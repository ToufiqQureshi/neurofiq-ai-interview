package models

import (
	"time"
)

type InterviewSession struct {
	ID            string  `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	UserID        string  `gorm:"type:uuid;not null;index" json:"user_id"`
	RepoFullName  string  `gorm:"not null" json:"repo_full_name"`
	QuestionsJSON string  `gorm:"type:text" json:"questions_json"`
	AnswersJSON   string  `gorm:"type:text" json:"answers_json"`
	OverallScore  float64 `json:"overall_score"`
	FeedbackJSON  string  `gorm:"type:text" json:"feedback_json"`
	InterviewType string  `gorm:"column:interview_type;not null;default:'code_interview'" json:"interview_type"`
	Mode          string  `gorm:"column:mode;not null;default:'text'" json:"mode"`

	// ShareSlug is the public identifier for this report. It is a pointer so
	// that the unique index tolerates the many rows that were never shared —
	// Postgres treats NULLs as distinct, but every unshared row holding the
	// same empty string would collide.
	ShareSlug *string `gorm:"uniqueIndex" json:"share_slug,omitempty"`
	// SharedAt records when the candidate turned sharing on. Revoking sets
	// both fields back so the old link 404s immediately.
	SharedAt *time.Time `json:"shared_at,omitempty"`

	// InviteID links this session to the recruiter invite it was taken under,
	// if any. Nil for a candidate practising on their own.
	InviteID *string `gorm:"type:uuid;index" json:"invite_id,omitempty"`

	CreatedAt time.Time `json:"created_at"`
}
