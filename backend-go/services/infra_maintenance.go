package services

import (
	"log"
	"time"

	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/config"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/models"
)

// ReclaimStaleAnalyses marks abandoned analyses as failed.
//
// An analysis runs in a background goroutine, so a deploy, a crash, or an OOM
// kill leaves its row stuck on "pending" with nothing left to finish it. From
// the user's side that is a spinner that never resolves and a free-tier slot
// they cannot get back. Anything older than the grace period is not running
// any more, on this instance or any other — a real analysis takes under a
// minute.
func ReclaimStaleAnalyses(olderThan time.Duration) {
	cutoff := time.Now().Add(-olderThan)

	result := config.DB.Model(&models.GithubProfile{}).
		Where("strategy_used = ? AND analyzed_at < ?", "pending", cutoff).
		Update("strategy_used", "failed")

	if result.Error != nil {
		log.Printf("maintenance: could not reclaim stale analyses: %v", result.Error)
		return
	}
	if result.RowsAffected > 0 {
		log.Printf("maintenance: reclaimed %d abandoned analysis job(s) — users can retry them", result.RowsAffected)
	}
}
