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

	// Field and Level are the facet buckets ClassifyField/ClassifyLevel derive
	// from the title, computed once when the row is written.
	//
	// They were computed per request instead: JobFacets pulled the title and
	// department of every job matching the visitor's filters into Go and
	// classified them on each page load. That is a full scan of the jobs table
	// per request, and the classification is pure — the same title gives the
	// same answer every time — so it was the same work repeated forever.
	// Stored, the facet counts become one GROUP BY on an indexed column.
	//
	// The classifiers stay the source of truth: these columns are written from
	// them, and BackfillJobFacets rewrites the column when the rules change,
	// which is the only way a stored derivation stays honest.
	Field string `gorm:"index" json:"field"`
	Level string `gorm:"index" json:"level"`

	// LastCheckedAt is when the dead-link prune last verified this URL.
	//
	// The prune used to fetch every job's URL on every run. At 4,000 roles
	// that is a five-minute job; at the 300,000 this pipeline is built to
	// reach it is 300,000 requests against other people's servers twice a
	// day, which is both unfinishable and exactly the kind of traffic that
	// gets a crawler blocked. Ordering by this column lets the prune do a
	// bounded slice per tick and still come round to everything.
	LastCheckedAt *time.Time `gorm:"index" json:"-"`
}
