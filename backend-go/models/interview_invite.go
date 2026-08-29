package models

import (
	"time"
)

// InterviewInvite is a recruiter asking a specific person to take the
// repo-based interview.
//
// The interview loop itself is unchanged — the same analysis, the same five
// questions, the same rubric. All that differs is who started it, which is
// what makes this the side of the market that pays: a candidate practises,
// a company screens.
type InterviewInvite struct {
	ID          string `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	RecruiterID string `gorm:"type:uuid;not null;index" json:"recruiter_id"`

	// Token is what goes in the link. Random and unguessable, because holding
	// it is what authorises a candidate to redeem the invite.
	Token string `gorm:"uniqueIndex;not null" json:"token"`

	RoleTitle string  `gorm:"not null" json:"role_title"`
	CompanyID *string `gorm:"type:uuid;index" json:"company_id,omitempty"`
	Note      string  `json:"note"`

	// CandidateEmail is optional: an invite can be a single-use link sent to
	// one person, or a shareable link posted in a job ad.
	CandidateEmail string `json:"candidate_email"`

	// MaxUses of 0 means unlimited. Uses is incremented on redemption.
	MaxUses int `gorm:"not null;default:1" json:"max_uses"`
	Uses    int `gorm:"not null;default:0" json:"uses"`

	ExpiresAt time.Time  `json:"expires_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
	CreatedAt time.Time  `gorm:"default:now()" json:"created_at"`
}

// Redeemable reports whether the invite can still start an interview.
func (i InterviewInvite) Redeemable(now time.Time) bool {
	if i.RevokedAt != nil {
		return false
	}
	if !i.ExpiresAt.IsZero() && now.After(i.ExpiresAt) {
		return false
	}
	if i.MaxUses > 0 && i.Uses >= i.MaxUses {
		return false
	}
	return true
}
