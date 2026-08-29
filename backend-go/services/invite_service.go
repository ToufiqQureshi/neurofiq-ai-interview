package services

import (
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/config"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/models"
)

const (
	defaultInviteTTL = 30 * 24 * time.Hour
	maxInviteTTL     = 180 * 24 * time.Hour
	maxInviteUses    = 500
)

// CreateInvite issues a recruiter's interview link.
func CreateInvite(recruiterID, roleTitle, note, candidateEmail string, maxUses int, ttl time.Duration) (*models.InterviewInvite, error) {
	roleTitle = strings.TrimSpace(roleTitle)
	if roleTitle == "" {
		return nil, fmt.Errorf("a role title is required")
	}
	if len(roleTitle) > 120 {
		return nil, fmt.Errorf("role title is too long")
	}
	if len(note) > 1000 {
		return nil, fmt.Errorf("note is too long")
	}
	if maxUses < 0 || maxUses > maxInviteUses {
		return nil, fmt.Errorf("max_uses must be between 0 (unlimited) and %d", maxInviteUses)
	}
	if ttl <= 0 {
		ttl = defaultInviteTTL
	}
	if ttl > maxInviteTTL {
		ttl = maxInviteTTL
	}

	token, err := newInviteToken()
	if err != nil {
		return nil, err
	}

	invite := models.InterviewInvite{
		RecruiterID:    recruiterID,
		Token:          token,
		RoleTitle:      roleTitle,
		Note:           strings.TrimSpace(note),
		CandidateEmail: strings.TrimSpace(candidateEmail),
		MaxUses:        maxUses,
		ExpiresAt:      time.Now().Add(ttl),
	}
	if err := config.DB.Create(&invite).Error; err != nil {
		return nil, fmt.Errorf("could not create the invite: %w", err)
	}
	return &invite, nil
}

// LookupInvite reads an invite without consuming a use, so a candidate can see
// what they were invited to before starting.
func LookupInvite(token string) (*models.InterviewInvite, error) {
	if len(token) < 16 || len(token) > 64 {
		return nil, fmt.Errorf("this invite link is not valid")
	}
	var invite models.InterviewInvite
	if err := config.DB.Where("token = ?", token).First(&invite).Error; err != nil {
		return nil, fmt.Errorf("this invite link is not valid")
	}
	if !invite.Redeemable(time.Now()) {
		return nil, fmt.Errorf("this invite has expired or has already been used")
	}
	return &invite, nil
}

// RedeemInvite consumes one use of an invite.
//
// The whole check-and-increment is a single conditional UPDATE rather than a
// read followed by a write: two candidates submitting a single-use invite at
// the same moment would otherwise both pass the read and both get in. This is
// the same race the analysis quota had, and the fix is the same shape — let
// the database do the comparison.
func RedeemInvite(token string) (*models.InterviewInvite, error) {
	if len(token) < 16 || len(token) > 64 {
		return nil, fmt.Errorf("this invite link is not valid")
	}

	result := config.DB.Model(&models.InterviewInvite{}).
		Where("token = ? AND revoked_at IS NULL AND expires_at > ? AND (max_uses = 0 OR uses < max_uses)",
			token, time.Now()).
		UpdateColumn("uses", config.DB.Raw("uses + 1"))
	if result.Error != nil {
		return nil, fmt.Errorf("could not redeem this invite")
	}
	if result.RowsAffected == 0 {
		return nil, fmt.Errorf("this invite has expired or has already been used")
	}

	var invite models.InterviewInvite
	if err := config.DB.Where("token = ?", token).First(&invite).Error; err != nil {
		return nil, fmt.Errorf("could not redeem this invite")
	}
	return &invite, nil
}

// RevokeInvite kills a link without deleting the interviews taken under it.
func RevokeInvite(recruiterID, inviteID string) error {
	now := time.Now()
	result := config.DB.Model(&models.InterviewInvite{}).
		Where("id = ? AND recruiter_id = ?", inviteID, recruiterID).
		Update("revoked_at", now)
	if result.Error != nil {
		return fmt.Errorf("could not revoke this invite")
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("invite not found")
	}
	return nil
}

// ListInvites returns a recruiter's invites, newest first.
func ListInvites(recruiterID string) ([]models.InterviewInvite, error) {
	var invites []models.InterviewInvite
	err := config.DB.Where("recruiter_id = ?", recruiterID).
		Order("created_at desc").Limit(200).Find(&invites).Error
	return invites, err
}

// RankedCandidate is one completed interview as a recruiter reads it: who,
// which repository, and how they scored.
type RankedCandidate struct {
	SessionID      string    `json:"session_id"`
	InviteID       string    `json:"invite_id"`
	RoleTitle      string    `json:"role_title"`
	GithubUsername string    `json:"github_username"`
	AvatarURL      string    `json:"avatar_url"`
	RepoFullName   string    `json:"repo_full_name"`
	OverallScore   float64   `json:"overall_score"`
	Mode           string    `json:"mode"`
	CompletedAt    time.Time `json:"completed_at"`
}

// RankedCandidates lists everyone who has completed one of this recruiter's
// invites, best score first.
//
// This is the whole recruiter product in one query: the interview loop, the
// analysis and the rubric are identical to the candidate-facing flow. The only
// thing that changes is who started it — which is also the difference between
// a free practice tool and something a company pays for.
func RankedCandidates(recruiterID, inviteID string) ([]RankedCandidate, error) {
	query := config.DB.Table("interview_sessions AS s").
		Select(`s.id AS session_id, s.invite_id, i.role_title,
		        u.github_username, u.avatar_url,
		        s.repo_full_name, s.overall_score, s.mode, s.created_at AS completed_at`).
		Joins("JOIN interview_invites i ON i.id = s.invite_id").
		Joins("JOIN users u ON u.id = s.user_id").
		Where("i.recruiter_id = ?", recruiterID)

	if inviteID != "" {
		query = query.Where("s.invite_id = ?", inviteID)
	}

	var rows []RankedCandidate
	err := query.Order("s.overall_score desc, s.created_at desc").Limit(500).Scan(&rows).Error
	return rows, err
}

// newInviteToken returns the unguessable half of an invite link. Holding the
// token is what authorises redemption, so it has to be random, not derived.
func newInviteToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf)), nil
}

// ReleaseInviteUse gives back a use that was consumed for an interview that
// then failed to save. Without it, a database hiccup silently costs the
// candidate their single-use link with nothing to show for it.
func ReleaseInviteUse(token string) {
	err := config.DB.Model(&models.InterviewInvite{}).
		Where("token = ? AND uses > 0", token).
		UpdateColumn("uses", config.DB.Raw("uses - 1")).Error
	if err != nil {
		log.Printf("invite: failed to release a use for a failed interview: %v", err)
	}
}
