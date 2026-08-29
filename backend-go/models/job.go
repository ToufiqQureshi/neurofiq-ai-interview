package models

import (
	"time"
)

// Job is a single open role pulled from a company's applicant-tracking
// system (Greenhouse/Lever public API) via services.SyncJobsForCompany.
type Job struct {
	ID         string    `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	CompanyID  string    `gorm:"type:uuid;not null;uniqueIndex:idx_jobs_company_url" json:"company_id"`
	Title      string    `gorm:"not null" json:"title"`
	Department string    `json:"department"`
	Location   string    `json:"location"`
	URL        string    `gorm:"not null;uniqueIndex:idx_jobs_company_url" json:"url"`
	Source     string    `json:"source"`
	CreatedAt  time.Time `gorm:"default:now()" json:"created_at"`
}
