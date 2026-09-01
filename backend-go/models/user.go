package models

import (
	"time"
)

// User represents a candidate or admin in the system.
// It maps directly to the "users" table in our Supabase schema.
type User struct {
	ID               string    `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	GithubID         *int64    `gorm:"unique" json:"github_id,omitempty"`
	GithubUsername   string    `json:"github_username"`
	GoogleID         *string   `gorm:"unique" json:"google_id,omitempty"`
	Email            string    `gorm:"unique" json:"email"`
	PasswordHash     string    `json:"-"` // Never exposed in JSON responses
	FullName         string    `json:"full_name"`
	AvatarURL        string    `json:"avatar_url"`
	PlanType         string    `gorm:"default:'free'" json:"plan_type"`
	Role             string    `gorm:"default:'candidate'" json:"role"`
	GithubConnected  bool      `gorm:"default:false" json:"github_connected"`
	IsOnboarded      bool      `gorm:"default:false" json:"is_onboarded"`
	ExperienceLevel  string    `json:"experience_level"` // fresher, mid, senior
	TargetRole       string    `json:"target_role"`      // backend, frontend, fullstack, ai_ml, devops
	TechStack        string    `json:"tech_stack"`       // comma-separated tags e.g. "Go, Python, React"
	LinkedInURL      string    `gorm:"column:linkedin_url" json:"linkedin_url"`
	CollegeOrCompany string    `json:"college_or_company"`
	InterviewGoal    string    `json:"interview_goal"`
	CreatedAt        time.Time `gorm:"default:now()" json:"created_at"`
	LastLoginAt      time.Time `json:"last_login_at"`
}
