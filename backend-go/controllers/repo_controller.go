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
	userID := c.MustGet("user_id").(string)
	token := c.MustGet("github_token").(string)

	repos, err := services.GetReposWithETag(userID, token)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to fetch repositories from GitHub"})
		return
	}

	var profiles []models.GithubProfile
	config.DB.Select("repo_full_name", "strategy_used").Where("user_id = ?", userID).Find(&profiles)
	analyzed := map[string]string{}
	used := 0
	for _, p := range profiles {
		analyzed[p.RepoFullName] = p.StrategyUsed
		if p.StrategyUsed != "failed" {
			used++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"repos":           repos,
		"analysis_status": analyzed,
		"analyses_used":   used,
		"analyses_limit":  3,
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
	if !services.ValidRepoFullName(reqBody.RepoFullName) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "repo_full_name must be owner/name"})
		return
	}

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

	var existingProfile models.GithubProfile
	err := tx.Where("user_id = ? AND repo_full_name = ?", userID, reqBody.RepoFullName).First(&existingProfile).Error
	if err == nil {
		if existingProfile.StrategyUsed == "pending" {
			tx.Rollback()
			c.JSON(http.StatusAccepted, gin.H{"message": "Analysis already in progress", "status": "processing"})
			return
		}
		if existingProfile.StrategyUsed == "failed" {
			if err := tx.Model(&existingProfile).Updates(map[string]interface{}{
				"strategy_used": "pending",
				"analysis_json": "null",
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
		}
		tx.Rollback()
		c.JSON(http.StatusOK, gin.H{
			"message":  "Analysis already exists",
			"status":   "completed",
			"analysis": existingProfile.AnalysisJSON,
		})
		return
	}

	var count int64
	tx.Model(&models.GithubProfile{}).Where("user_id = ? AND strategy_used <> ?", userID, "failed").Count(&count)
	if count >= 3 {
		tx.Rollback()
		c.JSON(http.StatusForbidden, gin.H{"error": "Free tier limit reached. You can only analyze up to 3 repositories."})
		return
	}

	placeholder := models.GithubProfile{
		UserID:       userID,
		RepoFullName: reqBody.RepoFullName,
		StrategyUsed: "pending",
		AnalysisJSON: "null",
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
		if existingProfile.StrategyUsed == "failed" {
			c.JSON(http.StatusOK, gin.H{"status": "failed", "error": "Analysis failed. You can retry this repository."})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"status":   "completed",
			"analysis": existingProfile.AnalysisJSON,
		})
		return
	}

	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusOK, gin.H{"status": "not_found", "error": "No analysis job found for this repository."})
		return
	}

	log.Printf("HandleCheckAnalysisStatus: db error for user=%s repo=%s: %v", userID, repoFullName, err)
	c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check analysis status"})
}

func runBackgroundAnalysis(uid, repo, tok string) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("PANIC in background analysis for repo %s: %v", repo, r)
			markAnalysisFailed(uid, repo)
		}
	}()
	if _, err := services.AnalyzeAndExtract(uid, repo, tok); err != nil {
		log.Printf("Background analysis failed for repo %s: %v", repo, err)
		markAnalysisFailed(uid, repo)
	}
}

func markAnalysisFailed(uid, repo string) {
	config.DB.Model(&models.GithubProfile{}).
		Where("user_id = ? AND repo_full_name = ? AND strategy_used = ?", uid, repo, "pending").
		Update("strategy_used", "failed")
}
