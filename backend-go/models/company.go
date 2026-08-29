package models

import (
	"time"
)

type Company struct {
	ID          string    `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	Name        string    `gorm:"not null" json:"name"`
	Slug        string    `gorm:"uniqueIndex;not null" json:"slug"`
	Description string    `json:"description"`
	Website     string    `json:"website"`
	Domain      string    `gorm:"uniqueIndex;not null" json:"domain"`
	Sector      string    `json:"sector"`
	Stage       string    `json:"stage"`
	Area        string    `json:"area"`
	CareersURL  string    `json:"careers_url"`
	Lat         *float64  `json:"lat"`
	Lng         *float64  `json:"lng"`
	// ATSType/ATSSlug identify the applicant-tracking system (if any) this
	// company's job board runs on ("greenhouse" or "lever"), detected by
	// services.DetectATS, so real open roles can be pulled from its public
	// API instead of just linking out to the careers page.
	ATSType string `json:"ats_type"`
	ATSSlug string `json:"ats_slug"`
	// ATSCheckedAt is when we last attempted detection. Companies that came
	// back with no ATS are retried only occasionally — without this, every
	// cron tick re-scrapes every ATS-less company and burns the scraper's
	// monthly credit allowance on companies that will never have a board.
	ATSCheckedAt *time.Time `json:"ats_checked_at"`
	Source    string    `gorm:"default:'agno-discovery'" json:"source"`
	CreatedAt time.Time `gorm:"default:now()" json:"created_at"`
}
