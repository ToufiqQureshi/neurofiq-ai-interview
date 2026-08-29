package controllers

import (
	"net/http"
	"strings"

	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/services"
	"github.com/gin-gonic/gin"
)

// HandleGetQuestions fetches or generates the questions for one interview.
func HandleGetQuestions(c *gin.Context) {
	userID := c.MustGet("user_id").(string)

	repoFullName := c.Query("repo_full_name")
	if repoFullName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "repo_full_name is required"})
		return
	}

	questions, err := services.GetOrGenerateQuestions(userID, repoFullName)
	if err != nil {
		// "Analyze the repo first" is the caller's problem; the AI worker
		// being unreachable is ours. Returning 400 for both left the frontend
		// unable to tell a user error from an outage.
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "ai worker") {
			status = http.StatusBadGateway
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}

	// Never ship expected_answer to the browser: it is the answer key, and it
	// is one Network-tab click away from the candidate being interviewed.
	public := make([]gin.H, 0, len(questions))
	for _, q := range questions {
		public = append(public, gin.H{
			"id":            q.ID,
			"question_text": q.QuestionText,
			"difficulty":    q.Difficulty,
			"category":      q.Category,
			// The code the question is about, so the candidate can read it
			// here instead of going to find it on GitHub mid-interview.
			"file_reference": q.FileReference,
			"code_snippet":   q.CodeSnippet,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"message":   "Questions retrieved successfully",
		"questions": public,
	})
}
