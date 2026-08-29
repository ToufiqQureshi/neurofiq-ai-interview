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

// HandleGetRepos fetches the user's GitHub repositories.
func HandleGetRepos(c *gin.Context) {
	// Extracted from session by AuthMiddleware
	userID := c.MustGet("user_id").(string)
	token := c.MustGet("github_token").(string)

	// 2. Fetch Repos from GitHub using the ETag Cache service
	repos, err := services.GetReposWithETag(userID, token)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to fetch repositories from GitHub"})
		return
	}

	// 3. Return the repos
	c.JSON(http.StatusOK, gin.H{
		"repos": repos,
	})
}

// HandleAnalyzeRepo triggers the repo analysis pipeline.
func HandleAnalyzeRepo(c *gin.Context) {
	userID := c.MustGet("user_id").(string)
	token := c.MustGet("github_token").(string)

	var reqBody struct {
		RepoFullName string `json:"repo_full_name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&reqBody); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	// The billing-limit check (has this repo already been analyzed / has the
	// user hit the 3-repo free-tier limit) and reserving a slot for this
	// analysis must happen atomically, otherwise two concurrent requests can
	// both read count < 3 before either has recorded a row. We serialize the
	// whole check-then-reserve section per-user with a Postgres advisory
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

	// Billing Limit Check 1: Has this specific repo already been analyzed
	// (or is analysis for it already in flight)?
	var existingProfile models.GithubProfile
	err := tx.Where("user_id = ? AND repo_full_name = ?", userID, reqBody.RepoFullName).First(&existingProfile).Error
	if err == nil {
		tx.Rollback() // read-only so far, nothing to persist
		if existingProfile.StrategyUsed == "pending" {
			c.JSON(http.StatusAccepted, gin.H{"message": "Analysis already in progress", "status": "processing"})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"message":  "Analysis already exists",
			"status":   "completed",
			"analysis": existingProfile.AnalysisJSON,
		})
		return
	}

	// Billing Limit Check 2: Has this user reached the 3 repo limit? Pending
	// (in-flight) rows count too, since they already occupy a slot.
	var count int64
	tx.Model(&models.GithubProfile{}).Where("user_id = ?", userID).Count(&count)
	if count >= 3 {
		tx.Rollback()
		c.JSON(http.StatusForbidden, gin.H{"error": "Free tier limit reached. You can only analyze up to 3 repositories."})
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

	// Run Async Analysis (Ponytail style: no Redis/Celery needed)
	go func(uid, repo, tok string) {
		defer func() {
			if r := recover(); r != nil {
				// Gin's Recovery() middleware only guards the request
				// goroutine, not this one — an unrecovered panic here would
				// crash the whole process for every connected user.
				log.Printf("PANIC in background analysis for repo %s: %v", repo, r)
				config.DB.Where("user_id = ? AND repo_full_name = ? AND strategy_used = ?", uid, repo, "pending").Delete(&models.GithubProfile{})
			}
		}()
		if _, err := services.AnalyzeAndExtract(uid, repo, tok); err != nil {
			log.Printf("Background analysis failed for repo %s: %v", repo, err)
			// Free the reserved slot so the user can retry and doesn't lose
			// quota to a failed attempt.
			config.DB.Where("user_id = ? AND repo_full_name = ? AND strategy_used = ?", uid, repo, "pending").Delete(&models.GithubProfile{})
		}
	}(userID, reqBody.RepoFullName, token)

	c.JSON(http.StatusAccepted, gin.H{
		"message": "Analysis started in background",
		"status":  "processing",
	})
}

// HandleCheckAnalysisStatus polls the database to check if background analysis is finished
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
		if existingProfile.StrategyUsed == "pending" {
			c.JSON(http.StatusOK, gin.H{"status": "processing"})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"status":   "completed",
			"analysis": existingProfile.AnalysisJSON,
		})
		return
	}

	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusOK, gin.H{"status": "processing"})
		return
	}

	// A real DB error (not just "no row yet") — report it instead of
	// silently telling the frontend to keep polling forever.
	log.Printf("HandleCheckAnalysisStatus: db error for user=%s repo=%s: %v", userID, repoFullName, err)
	c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check analysis status"})
}
