package models

import (
	"time"
)

// User represents a candidate or admin in the system.
// It maps directly to the "users" table in our Supabase schema.
type User struct {
	ID              string    `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	GithubID        int64     `gorm:"unique" json:"github_id"`
	GithubUsername  string    `json:"github_username"`
	GoogleID        *string   `gorm:"unique" json:"google_id,omitempty"`
	Email           string    `json:"email"`
	AvatarURL       string    `json:"avatar_url"`
	PlanType        string    `gorm:"default:'free'" json:"plan_type"`
	Role            string    `gorm:"default:'candidate'" json:"role"`
	GithubConnected bool      `gorm:"default:false" json:"github_connected"`
	CreatedAt       time.Time `gorm:"default:now()" json:"created_at"`
	LastLoginAt     time.Time `json:"last_login_at"`
}
