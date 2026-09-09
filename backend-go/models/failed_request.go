package models

import "time"

// FailedRequest tracks API or discovery failures (e.g. timeout, rate limit)
// so that the admin can export them to CSV to monitor waste or budget issues.
type FailedRequest struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	Provider  string    `gorm:"index" json:"provider"` // e.g. exa, firecrawl, tavily
	Query     string    `json:"query"`                 // the search term or URL that failed
	Reason    string    `json:"reason"`                // the error message
	CreatedAt time.Time `gorm:"index" json:"created_at"`
}
