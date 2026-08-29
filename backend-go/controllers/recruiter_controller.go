package controllers

import (
	"net/http"
	"time"

	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/config"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/models"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/services"
	"github.com/gin-gonic/gin"
)

// RecruiterMiddleware gates the hiring-side endpoints.
//
// The role lives on the user row rather than in the session, so revoking
// somebody's recruiter access takes effect on their next request instead of
// whenever their cookie happens to expire.
func RecruiterMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.MustGet("user_id").(string)

		var user models.User
		if err := config.DB.Select("role").Where("id = ?", userID).First(&user).Error; err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Not authenticated"})
			return
		}
		if user.Role != "recruiter" && user.Role != "admin" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "This area is for hiring accounts. Ask us to enable recruiter access on your account.",
			})
			return
		}
		c.Next()
	}
}

type createInviteRequest struct {
	RoleTitle      string `json:"role_title" binding:"required"`
	Note           string `json:"note"`
	CandidateEmail string `json:"candidate_email"`
	MaxUses        int    `json:"max_uses"`
	ExpiresInDays  int    `json:"expires_in_days"`
}

// HandleCreateInvite issues a link a recruiter can send to candidates.
func HandleCreateInvite(c *gin.Context) {
	recruiterID := c.MustGet("user_id").(string)

	var req createInviteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "A role title is required."})
		return
	}
	if req.MaxUses == 0 {
		req.MaxUses = 1
	}

	ttl := time.Duration(req.ExpiresInDays) * 24 * time.Hour
	invite, err := services.CreateInvite(recruiterID, req.RoleTitle, req.Note, req.CandidateEmail, req.MaxUses, ttl)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"invite": invite})
}

// HandleListInvites returns the recruiter's own invites.
func HandleListInvites(c *gin.Context) {
	recruiterID := c.MustGet("user_id").(string)

	invites, err := services.ListInvites(recruiterID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not load invites"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"invites": invites})
}

// HandleRevokeInvite kills a link. Interviews already taken under it stay.
func HandleRevokeInvite(c *gin.Context) {
	recruiterID := c.MustGet("user_id").(string)

	if err := services.RevokeInvite(recruiterID, c.Param("id")); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"revoked": true})
}

// HandleRankedCandidates is the recruiter's actual product: everyone who
// completed one of their invites, ranked by how they scored on their own code.
func HandleRankedCandidates(c *gin.Context) {
	recruiterID := c.MustGet("user_id").(string)

	rows, err := services.RankedCandidates(recruiterID, c.Query("invite_id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not load candidates"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"candidates": rows, "total": len(rows)})
}

// HandleGetRecruiterReport lets a recruiter open the full report for an
// interview taken under one of their own invites — and only those.
func HandleGetRecruiterReport(c *gin.Context) {
	recruiterID := c.MustGet("user_id").(string)

	var session models.InterviewSession
	err := config.DB.Table("interview_sessions AS s").
		Select("s.*").
		Joins("JOIN interview_invites i ON i.id = s.invite_id").
		Where("s.id = ? AND i.recruiter_id = ?", c.Param("id"), recruiterID).
		First(&session).Error
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Report not found"})
		return
	}

	c.JSON(http.StatusOK, session)
}

// HandleLookupInvite is the candidate's side: read an invite without spending
// one of its uses, so the "you've been invited to interview for X" screen can
// render before they commit.
func HandleLookupInvite(c *gin.Context) {
	invite, err := services.LookupInvite(c.Param("token"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	var recruiter models.User
	config.DB.Select("github_username", "avatar_url").Where("id = ?", invite.RecruiterID).First(&recruiter)

	// The token is already in the URL bar; everything else about the invite
	// (who else it went to, how many uses are left) is the recruiter's.
	c.JSON(http.StatusOK, gin.H{
		"role_title": invite.RoleTitle,
		"note":       invite.Note,
		"expires_at": invite.ExpiresAt,
		"recruiter": gin.H{
			"github_username": recruiter.GithubUsername,
			"avatar_url":      recruiter.AvatarURL,
		},
	})
}
