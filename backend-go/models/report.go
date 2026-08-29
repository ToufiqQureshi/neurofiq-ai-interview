package models

import (
	"time"
)

type InterviewSession struct {
	ID            string    `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	UserID        string    `gorm:"type:uuid;not null" json:"user_id"`
	RepoFullName  string    `gorm:"not null" json:"repo_full_name"`
	QuestionsJSON string    `gorm:"type:text" json:"questions_json"`
	AnswersJSON   string    `gorm:"type:text" json:"answers_json"`
	OverallScore  float64   `json:"overall_score"`
	FeedbackJSON  string    `gorm:"type:text" json:"feedback_json"`
	InterviewType string    `gorm:"column:interview_type;not null;default:'code_interview'" json:"interview_type"`
	Mode          string    `gorm:"column:mode;not null;default:'text'" json:"mode"`
	CreatedAt     time.Time `json:"created_at"`
}
