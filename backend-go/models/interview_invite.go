package models

import (
	"time"
)

// InterviewInvite represents an invitation sent by a recruiter to a candidate.
type InterviewInvite struct {
	ID             string    `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	Token          string    `gorm:"uniqueIndex;not null" json:"token"` // The magic link token
	RecruiterID    string    `gorm:"type:uuid;not null;index" json:"recruiter_id"`
	CandidateEmail string    `gorm:"not null;index" json:"candidate_email"`
	JobID          *string   `gorm:"type:uuid" json:"job_id"`         // Optional job description attached to the invite
	RepoFullName   *string   `json:"repo_full_name"`                  // Optional specific repo to test
	Status         string    `gorm:"default:'pending'" json:"status"` // pending, completed, expired
	ExpiresAt      time.Time `json:"expires_at"`
	CreatedAt      time.Time `gorm:"default:now()" json:"created_at"`
}
