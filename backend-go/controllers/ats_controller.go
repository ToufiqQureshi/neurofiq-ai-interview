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
	userID := c.MustGet("user_id").(string)

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

	// A job posting belongs to a company, and companies.id is what the
	// public Job Map and Find Jobs pages join jobs against. An ad-hoc JD
	// pasted here has no company behind it, so it must never be written to
	// the shared jobs table — doing so left a permanently orphaned row with
	// company_id set to the recruiter's own user id and url "internal".
	// Nothing reads InterviewInvite.JobID today (VerifyInvite returns the
	// bare invite, and the candidate landing page only uses repo_full_name
	// and token), so there is nothing to carry it in until this needs a
	// dedicated field on the invite itself.
	token := generateMagicToken()

	invite := models.InterviewInvite{
		Token:          token,
		RecruiterID:    userID,
		CandidateEmail: req.CandidateEmail,
		RepoFullName:   req.RepoFullName,
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
