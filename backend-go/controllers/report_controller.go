package controllers

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/config"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/models"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/services"
)

type SubmitInterviewReq struct {
	RepoFullName string            `json:"repo_full_name" binding:"required"`
	QAList       []services.QAItem `json:"qa_list" binding:"required"`
	Mode         string            `json:"mode"`
}

func HandleSubmitInterview(c *gin.Context) {
	userID := c.MustGet("user_id").(string)

	var reqBody SubmitInterviewReq
	if err := c.ShouldBindJSON(&reqBody); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
		return
	}

	payload := services.EvaluatePayload{
		RepoFullName: reqBody.RepoFullName,
		QAList:       reqBody.QAList,
	}

	// 1. Call Python AI Worker to evaluate the answers
	feedbackJSON, score, err := services.CallPythonEvaluationWorker(payload)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	qaJSON, _ := json.Marshal(reqBody.QAList)

	mode := reqBody.Mode
	if mode != "voice" {
		mode = "text"
	}

	// 2. Save the Session Report to DB
	session := models.InterviewSession{
		UserID:        userID,
		RepoFullName:  reqBody.RepoFullName,
		QuestionsJSON: string(qaJSON), // Saving combined QA
		AnswersJSON:   string(qaJSON),
		OverallScore:  score,
		FeedbackJSON:  feedbackJSON,
		InterviewType: "code_interview",
		Mode:          mode,
	}

	if err := config.DB.Create(&session).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save session to DB"})
		return
	}

	// Return the ID of the new session so frontend can redirect to /report/:id
	c.JSON(http.StatusOK, gin.H{
		"session_id": session.ID,
	})
}

func HandleGetReportByID(c *gin.Context) {
	userID := c.MustGet("user_id").(string)

	sessionID := c.Param("id")
	var session models.InterviewSession

	if err := config.DB.Where("id = ? AND user_id = ?", sessionID, userID).First(&session).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Report not found"})
		return
	}

	c.JSON(http.StatusOK, session)
}

func HandleGetReports(c *gin.Context) {
	userID := c.MustGet("user_id").(string)

	var sessions []models.InterviewSession
	// Get all sessions for this user, newest first
	if err := config.DB.Where("user_id = ?", userID).Order("created_at desc").Find(&sessions).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch reports"})
		return
	}

	c.JSON(http.StatusOK, sessions)
}
