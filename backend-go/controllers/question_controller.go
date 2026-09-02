package controllers

import (
	"errors"
	"log"
	"net/http"

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

	// Optional: the Job Map opening — or company — this interview is practice
	// for. Both are resolved against the database in the service, so an
	// unknown id costs the framing and nothing else.
	questions, err := services.GetOrGenerateQuestions(
		userID, repoFullName, c.Query("job_id"), c.Query("company_id"))
	if err != nil {
		// "Analyze the repo first" is the caller's problem and its text is
		// written for them. The worker being unreachable is ours: that error
		// carries the worker's own response body, which may hold a provider
		// message or a stack trace, so it is logged and never returned.
		if errors.Is(err, services.ErrWorkerUnavailable) {
			log.Printf("questions: worker failure for user=%s repo=%s: %v", userID, repoFullName, err)
			c.JSON(http.StatusBadGateway, gin.H{
				"error": "The question service is unavailable right now. Please try again in a moment.",
			})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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
