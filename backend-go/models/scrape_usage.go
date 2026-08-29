package models

import (
	"time"
)

// ScrapeUsage tracks how many paid-tier scrape calls we've made per month,
// so the pipeline can fall back to a free provider before burning through
// a monthly credit allowance. One row per (month, provider).
type ScrapeUsage struct {
	ID        string    `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	Month     string    `gorm:"not null;uniqueIndex:idx_scrape_usage_month_provider" json:"month"`    // "2026-08"
	Provider  string    `gorm:"not null;uniqueIndex:idx_scrape_usage_month_provider" json:"provider"` // "firecrawl"
	Count     int       `gorm:"not null;default:0" json:"count"`
	UpdatedAt time.Time `gorm:"default:now()" json:"updated_at"`
}
