package models

import "time"

// CronLease is how multiple copies of the API agree on which one runs the
// scheduled work.
//
// The cron lives inside the API process, so every container that boots starts
// its own scheduler. With two containers that means two company-discovery
// runs an hour — double the LLM spend, double the scraper credits, and every
// job board fetched twice. A short lease with an expiry (rather than a
// long-lived lock) means a container that dies mid-tick releases it by itself.
type CronLease struct {
	Name      string    `gorm:"primaryKey" json:"name"`
	Holder    string    `gorm:"not null" json:"holder"`
	ExpiresAt time.Time `gorm:"not null" json:"expires_at"`
	UpdatedAt time.Time `gorm:"default:now()" json:"updated_at"`
}
