package models

import (
	"time"
)

type Company struct {
	ID          string   `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	Name        string   `gorm:"not null" json:"name"`
	Slug        string   `gorm:"uniqueIndex;not null" json:"slug"`
	Description string   `json:"description"`
	Website     string   `json:"website"`
	Domain      string   `gorm:"uniqueIndex;not null" json:"domain"`
	Sector      string   `json:"sector"`
	Stage       string   `json:"stage"`
	Area        string   `json:"area"`
	CareersURL  string   `json:"careers_url"`
	Lat         *float64 `json:"lat"`
	Lng         *float64 `json:"lng"`
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
	// EmptyJobReads counts consecutive syncs that came back with no roles.
	// One empty read is not evidence a company stopped hiring: an ATS can
	// answer 200 with an empty array during maintenance, and the careers-page
	// link scan reads a redesigned page the same way. Clearing on the first
	// one makes a hiring company look shut and lets the directory flip
	// between the two states on alternate ticks, so listings survive one
	// empty read and are cleared on the second. Reset the moment a read finds
	// anything.
	EmptyJobReads int    `json:"-"`
	Source        string `gorm:"default:'agno-discovery'" json:"source"`

	// OpenRoles is how many roles the jobs table currently holds for this
	// company, maintained by whatever last wrote them.
	//
	// It is denormalised on purpose. The directory listing sorts hiring
	// companies first, which meant every page load ran
	// `LEFT JOIN jobs ... GROUP BY companies.id ... ORDER BY COUNT(jobs.id)` —
	// an aggregation over the whole join before the LIMIT could apply, twice
	// per request because the total is counted the same way. No index can
	// serve an ORDER BY over an aggregate, so that cost grows with the
	// directory and there is no version of it that gets faster. At 300
	// companies it is invisible; the harvest exists to make that number
	// 25,000.
	//
	// The risk of a counter is that it drifts from the rows it counts. Two
	// things hold it: every write goes through replaceJobsForCompany, which
	// sets it from the same slice it just stored, and RecountOpenRoles
	// repairs the whole column from the jobs table on a schedule, so a drift
	// caused by a crash mid-write is corrected without anyone noticing it.
	OpenRoles int `gorm:"index;not null;default:0" json:"open_roles"`

	// LastSyncedAt is when this company's roles were last refreshed.
	//
	// The hourly sync used to load every company and check them all. That is
	// a fixed hour's work against a growing directory, and the tick that
	// stops fitting inside its window does not fail — it overruns, and the
	// next tick starts on top of it. Ordering by this column and taking a
	// bounded batch turns the sync into a rotation: every company is reached,
	// the tick always finishes, and the ones waiting longest go first.
	LastSyncedAt *time.Time `gorm:"index" json:"last_synced_at"`

	CreatedAt time.Time `gorm:"default:now()" json:"created_at"`
}
