package models

import (
	"time"
)

// Question represents an interview question stored in the Question Engine.
type Question struct {
	ID             string    `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	QuestionText   string    `gorm:"not null" json:"question_text"`
	ExpectedAnswer string    `gorm:"not null" json:"expected_answer"`
	Difficulty     string    `gorm:"not null" json:"difficulty"`
	Category       string    `gorm:"not null" json:"category"` // e.g., "Architecture", "Code Quality"
	Language       string    `gorm:"not null;index" json:"language"`
	Reusable       bool      `gorm:"default:false" json:"reusable"`
	CreatedAt      time.Time `gorm:"default:now()" json:"created_at"`
}

// TableName overrides the default pluralized table name used by GORM.
func (Question) TableName() string {
	return "questions_bank"
}
