package controllers

import (
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/config"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/models"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/services"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// freeTierAnalyses is how many repositories a free account may analyze. The
// user picks which ones — the repo list itself is never truncated.
const freeTierAnalyses = 3

// HandleGetRepos fetches the user's GitHub repositories, annotated with what
// we've already analyzed so the picker can show "retry" or "continue" rather
// than offering every repo as if it were untouched.
func HandleGetRepos(c *gin.Context) {
	// Extracted from the session by AuthMiddleware
	userID := c.MustGet("user_id").(string)
	rawToken, exists := c.Get("github_token")
	if !exists || rawToken == nil {
		c.JSON(http.StatusOK, gin.H{
			"repos":           []interface{}{},
			"analysis_status": map[string]string{},
			"analyses_used":   0,
			"analyses_limit":  freeTierAnalyses,
		})
		return
	}
	token := rawToken.(string)

	// Fetch repos from GitHub through the ETag cache service.
	repos, err := services.GetReposWithETag(userID, token)
	if err != nil {
		log.Printf("repos: github fetch failed for user=%s: %v", userID, err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to fetch repositories from GitHub"})
		return
	}

	var profiles []models.GithubProfile
	if err := config.DB.Select("repo_full_name", "strategy_used").
		Where("user_id = ?", userID).Find(&profiles).Error; err != nil {
		log.Printf("repos: failed to load analysis status for user=%s: %v", userID, err)
	}
	analyzed := map[string]string{}
	used := 0
	for _, p := range profiles {
		analyzed[p.RepoFullName] = p.StrategyUsed
		// A failed analysis costs the user nothing — it frees its slot so the
		// attempt can be retried.
		if p.StrategyUsed != "failed" {
			used++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"repos":           repos,
		"analysis_status": analyzed,
		"analyses_used":   used,
		"analyses_limit":  freeTierAnalyses,
	})
}

// HandleAnalyzeRepo triggers the repo analysis pipeline.
func HandleAnalyzeRepo(c *gin.Context) {
	userID := c.MustGet("user_id").(string)
	rawToken, exists := c.Get("github_token")
	if !exists || rawToken == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Please connect your GitHub account to analyze repositories"})
		return
	}
	token := rawToken.(string)

	var reqBody struct {
		RepoFullName string `json:"repo_full_name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&reqBody); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}
	if !services.ValidRepoFullName(reqBody.RepoFullName) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "repo_full_name must be owner/name"})
		return
	}

	// The billing-limit check (has this repo already been analyzed / has the
	// user hit the free-tier limit) and reserving a slot for this analysis
	// must happen atomically, otherwise two concurrent requests can both read
	// count < 3 before either has recorded a row. We serialize the whole
	// check-then-reserve section per-user with a Postgres advisory
	// transaction lock (auto-released on commit/rollback).
	tx := config.DB.Begin()
	if tx.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start analysis"})
		return
	}
	if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", userID).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start analysis"})
		return
	}

	// Count the slots already spoken for. Pending (in-flight) rows count —
	// they occupy a slot — while failed ones do not, because the user got
	// nothing for them.
	//
	// This has to happen before the branches below, not inside the "new repo"
	// branch only. Counting after the retry branch is how the limit gets
	// bypassed: three good analyses plus one failure lets the failure be
	// retried into a fourth live slot, and every further failure raises the
	// ceiling again.
	var used int64
	if err := tx.Model(&models.GithubProfile{}).
		// COALESCE, because strategy_used is nullable: SQL drops NULL rows
		// from `strategy_used <> 'failed'` entirely, while HandleGetRepos
		// counts them in Go as used. Without this the two disagree and the
		// quota check is the looser of the pair.
		Where("COALESCE(strategy_used, '') <> ?", "failed").
		Where("user_id = ?", userID).
		Count(&used).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start analysis"})
		return
	}

	// Has this specific repo already been analyzed, or is analysis for it
	// already in flight?
	var existingProfile models.GithubProfile
	err := tx.Where("user_id = ? AND repo_full_name = ?", userID, reqBody.RepoFullName).First(&existingProfile).Error

	switch {
	case err == nil && existingProfile.StrategyUsed == "pending":
		tx.Rollback() // read-only so far, nothing to persist
		c.JSON(http.StatusAccepted, gin.H{"message": "Analysis already in progress", "status": "processing"})
		return

	case err == nil && existingProfile.StrategyUsed == "failed":
		// Retrying a failure re-occupies a slot, so it goes through the same
		// limit as a brand-new analysis.
		if used >= freeTierAnalyses {
			tx.Rollback()
			c.JSON(http.StatusForbidden, gin.H{"error": freeTierMessage()})
			return
		}
		if err := tx.Model(&models.GithubProfile{}).
			Where("id = ?", existingProfile.ID).
			Updates(map[string]interface{}{
				"strategy_used": "pending",
				"analysis_json": "null",
				// Reset the clock too, or the stale-job sweeper sees an old
				// timestamp on a job that just started and reclaims it
				// straight back to "failed".
				"analyzed_at": time.Now(),
			}).Error; err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retry analysis"})
			return
		}
		if err := tx.Commit().Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retry analysis"})
			return
		}
		go runBackgroundAnalysis(userID, reqBody.RepoFullName, token)
		c.JSON(http.StatusAccepted, gin.H{"message": "Analysis started in background", "status": "processing"})
		return

	case err == nil:
		tx.Rollback()
		c.JSON(http.StatusOK, gin.H{
			"message":  "Analysis already exists",
			"status":   "completed",
			"analysis": existingProfile.AnalysisJSON,
		})
		return

	case !errors.Is(err, gorm.ErrRecordNotFound):
		tx.Rollback()
		log.Printf("analyze: lookup failed for user=%s repo=%s: %v", userID, reqBody.RepoFullName, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start analysis"})
		return
	}

	// New repository for this user.
	if used >= freeTierAnalyses {
		tx.Rollback()
		c.JSON(http.StatusForbidden, gin.H{"error": freeTierMessage()})
		return
	}

	// Reserve the slot now, inside the same locked transaction, so a
	// concurrent request's count check above is guaranteed to see it once we
	// commit. services.AnalyzeAndExtract fills this row in when it finishes.
	placeholder := models.GithubProfile{
		UserID:       userID,
		RepoFullName: reqBody.RepoFullName,
		StrategyUsed: "pending",
		AnalysisJSON: "null", // column is jsonb; "" is not valid JSON
		ExpiresAt:    time.Now().Add(7 * 24 * time.Hour),
	}
	if err := tx.Create(&placeholder).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start analysis"})
		return
	}
	if err := tx.Commit().Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start analysis"})
		return
	}

	go runBackgroundAnalysis(userID, reqBody.RepoFullName, token)

	c.JSON(http.StatusAccepted, gin.H{
		"message": "Analysis started in background",
		"status":  "processing",
	})
}

func freeTierMessage() string {
	return "Free tier limit reached. You can analyze up to 3 repositories — delete one or upgrade to add more."
}

// HandleCheckAnalysisStatus polls the database to check whether the
// background analysis has finished.
func HandleCheckAnalysisStatus(c *gin.Context) {
	userID := c.MustGet("user_id").(string)
	repoFullName := c.Query("repo")
	if repoFullName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "repo parameter is required"})
		return
	}

	var existingProfile models.GithubProfile
	err := config.DB.Where("user_id = ? AND repo_full_name = ?", userID, repoFullName).First(&existingProfile).Error
	if err == nil {
		switch existingProfile.StrategyUsed {
		case "pending":
			c.JSON(http.StatusOK, gin.H{"status": "processing"})
		case "failed":
			c.JSON(http.StatusOK, gin.H{"status": "failed", "error": "Analysis failed. You can retry this repository."})
		default:
			c.JSON(http.StatusOK, gin.H{"status": "completed", "analysis": existingProfile.AnalysisJSON})
		}
		return
	}

	// No row at all means nothing was ever started for this repo — say so
	// rather than telling the frontend to poll forever.
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusOK, gin.H{"status": "not_found", "error": "No analysis job found for this repository."})
		return
	}

	// A real DB error — report it instead of silently looking like progress.
	log.Printf("HandleCheckAnalysisStatus: db error for user=%s repo=%s: %v", userID, repoFullName, err)
	c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check analysis status"})
}

// runBackgroundAnalysis owns the whole background job, including its own
// panic recovery.
//
// Gin's Recovery() middleware only guards the request goroutine, not one we
// spawn ourselves — an unrecovered panic here would take the entire process
// down, for every connected user, not just this request.
func runBackgroundAnalysis(uid, repo, tok string) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("PANIC in background analysis for repo %s: %v", repo, r)
			MarkAnalysisFailed(uid, repo)
		}
	}()
	if _, err := services.AnalyzeAndExtract(uid, repo, tok); err != nil {
		log.Printf("Background analysis failed for repo %s: %v", repo, err)
		MarkAnalysisFailed(uid, repo)
	}
}

// MarkAnalysisFailed flips a reserved row to "failed" rather than deleting
// it, so the status endpoint can tell "this failed, retry it" apart from
// "nothing was ever started".
func MarkAnalysisFailed(uid, repo string) {
	if err := config.DB.Model(&models.GithubProfile{}).
		Where("user_id = ? AND repo_full_name = ? AND strategy_used = ?", uid, repo, "pending").
		Update("strategy_used", "failed").Error; err != nil {
		log.Printf("failed to mark analysis failed for %s: %v", repo, err)
	}
}
