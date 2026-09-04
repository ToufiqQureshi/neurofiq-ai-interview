package controllers

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/config"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/models"
	"github.com/gin-gonic/gin"
)

// Send an email via Resend API
func sendResendEmail(to string, magicLink string, jobTitle string) {
	apiKey := os.Getenv("RESEND_API_KEY")
	if apiKey == "" {
		fmt.Println("No RESEND_API_KEY found, skipping email.")
		return
	}

	htmlContent := fmt.Sprintf(`
		<div style="font-family: sans-serif; padding: 20px;">
			<h2>You've been invited to interview for %s!</h2>
			<p>Click the secure link below to start your AI-proctored technical interview:</p>
			<a href="%s" style="display:inline-block; padding: 10px 20px; background-color: #5D5FEF; color: white; text-decoration: none; border-radius: 5px; font-weight: bold;">Start Interview</a>
			<p style="margin-top: 20px; font-size: 12px; color: #666;">This link expires in 72 hours. Please use a desktop browser with screen sharing enabled.</p>
		</div>
	`, jobTitle, magicLink)

	payload := map[string]interface{}{
		"from":    "NeuroFIQ <onboarding@resend.dev>",
		"to":      []string{to},
		"subject": "Interview Invitation: " + jobTitle,
		"html":    htmlContent,
	}
	bodyBytes, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST", "https://api.resend.com/emails", bytes.NewBuffer(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("Error sending email via Resend:", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		fmt.Println("Resend API returned status:", resp.StatusCode)
	} else {
		fmt.Println("Successfully sent invite email to", to)
	}
}

// Generate a random secure token for magic links
func generateMagicToken() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "fallback-token-" + time.Now().String()
	}
	return hex.EncodeToString(bytes)
}

// POST /api/invites
func CreateInvite(c *gin.Context) {
	// Need to check auth for recruiter, but for MVP we assume logged in user is the recruiter
	userVal, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	user := userVal.(*models.User)

	var req struct {
		CandidateEmail string  `json:"candidate_email"`
		RepoFullName   *string `json:"repo_full_name"`
		JobTitle       string  `json:"job_title"`
		JobDescription string  `json:"job_description"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var jobID *string
	if req.JobTitle != "" && req.JobDescription != "" {
		job := models.Job{
			Title:       req.JobTitle,
			Description: req.JobDescription,
			CompanyID:   user.ID, // For MVP, mapping recruiter ID as company ID
			URL:         "internal",
		}
		if err := config.DB.Create(&job).Error; err == nil {
			jobID = &job.ID
		}
	}

	token := generateMagicToken()

	invite := models.InterviewInvite{
		Token:          token,
		RecruiterID:    user.ID,
		CandidateEmail: req.CandidateEmail,
		RepoFullName:   req.RepoFullName,
		JobID:          jobID,
		ExpiresAt:      time.Now().Add(72 * time.Hour), // 3 days expiry
	}

	if err := config.DB.Create(&invite).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create invite"})
		return
	}

	// Assuming the frontend runs on localhost:5173 for local MVP or deployed URL
	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = "http://localhost:5173"
	}
	magicLink := fmt.Sprintf("%s/invite/%s", frontendURL, token)

	// Fire the email async so we don't block the API response
	go sendResendEmail(req.CandidateEmail, magicLink, req.JobTitle)

	c.JSON(http.StatusCreated, gin.H{
		"message":    "Invite created successfully",
		"invite_id":  invite.ID,
		"magic_link": magicLink,
	})
}

// GET /api/invites/:token
func VerifyInvite(c *gin.Context) {
	token := c.Param("token")

	var invite models.InterviewInvite
	if err := config.DB.Where("token = ?", token).First(&invite).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Invalid or expired invite"})
		return
	}

	if invite.ExpiresAt.Before(time.Now()) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "This invite has expired"})
		return
	}

	if invite.Status != "pending" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "This invite has already been used"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"invite": invite,
	})
}
