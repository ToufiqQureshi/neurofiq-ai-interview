package controllers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/services"
)

func HandleGetQuestions(c *gin.Context) {
	userID := c.MustGet("user_id").(string)

	repoFullName := c.Query("repo_full_name")
	if repoFullName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing repo_full_name query parameter"})
		return
	}

	questions, err := services.GetOrGenerateQuestions(userID, repoFullName)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	public := make([]gin.H, 0, len(questions))
	for _, q := range questions {
		public = append(public, gin.H{
			"id":            q.ID,
			"question_text": q.QuestionText,
			"difficulty":    q.Difficulty,
			"category":      q.Category,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"message":   "Questions retrieved successfully",
		"questions": public,
	})
}
