package models

import (
	"time"
)

// Question represents an interview question stored in the Question Engine.
type Question struct {
	ID             string `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	QuestionText   string `gorm:"not null" json:"question_text"`
	ExpectedAnswer string `gorm:"not null" json:"expected_answer"`
	Difficulty     string `gorm:"not null" json:"difficulty"`
	Category       string `gorm:"not null" json:"category"` // e.g., "Architecture", "Code Quality"

	// Language is the cache group. For repo-derived questions it holds the
	// repo's full name — the questions are only ever reusable for that one
	// repository, so there is nothing more general to group them by.
	Language string `gorm:"not null;index:idx_questions_cache,priority:1" json:"language"`

	// Fingerprint identifies the analysis these questions were generated
	// from. Without it the cache could never expire: re-analysing a
	// repository produces a new analysis, but the old questions would still
	// match on repo name alone and the candidate would be interviewed on code
	// that no longer exists.
	Fingerprint string `gorm:"index:idx_questions_cache,priority:2" json:"-"`

	// FileReference and CodeSnippet are the exact code the question is about,
	// so the interview UI can show it instead of sending the candidate off to
	// GitHub mid-answer. Empty for questions drawn from the commit history.
	FileReference string `json:"file_reference"`
	CodeSnippet   string `gorm:"type:text" json:"code_snippet"`

	Reusable  bool      `gorm:"default:false" json:"reusable"`
	CreatedAt time.Time `gorm:"default:now()" json:"created_at"`
}

// TableName overrides the default pluralized table name used by GORM.
func (Question) TableName() string {
	return "questions_bank"
}
