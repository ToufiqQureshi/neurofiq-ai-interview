package models

import (
	"time"
)

// GithubProfile represents the analysis cache for a specific repository.
type GithubProfile struct {
	ID           string    `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	UserID       string    `gorm:"type:uuid;not null;uniqueIndex:idx_github_profiles_user_repo" json:"user_id"`
	RepoFullName string    `gorm:"not null;uniqueIndex:idx_github_profiles_user_repo" json:"repo_full_name"`
	RepoSizeKb   int       `json:"repo_size_kb"`
	StrategyUsed string    `gorm:"not null;default:'pending'" json:"strategy_used"`
	AnalysisJSON string    `gorm:"type:jsonb" json:"analysis_json"` // Stores the JSON result from AI
	AnalyzedAt   time.Time `gorm:"default:now()" json:"analyzed_at"`
	ExpiresAt    time.Time `json:"expires_at"` // Set to AnalyzedAt + 7 days
}
