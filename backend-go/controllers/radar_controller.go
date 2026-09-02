package controllers

import (
	"log"
	"net/http"

	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/services"
	"github.com/gin-gonic/gin"
)

// RadarRequest represents the incoming JSON from the React frontend
type RadarRequest struct {
	ProfileURL string `json:"url" binding:"required"`
}

// HandleRadarAnalyze takes a profile URL, calls the AI worker to scrape and analyze it,
// and returns the structured ProfileRadarResult JSON to the frontend.
func HandleRadarAnalyze(c *gin.Context) {
	// 1. Parse request
	var req RadarRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body. 'url' is required."})
		return
	}

	// 2. Call the Python AI Worker
	analysisJSON, err := services.OptimizeProfileRadar(req.ProfileURL)
	if err != nil {
		log.Printf("HandleRadarAnalyze failed for %s: %v", req.ProfileURL, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to analyze profile URL. It might be blocked or invalid."})
		return
	}

	// 3. Return the raw JSON string directly from the Python worker
	c.Data(http.StatusOK, "application/json; charset=utf-8", []byte(analysisJSON))
}
