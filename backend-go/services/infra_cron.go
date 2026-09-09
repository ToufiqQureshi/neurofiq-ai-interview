package services

import (
	"log"
	"os"
	"time"

	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/config"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/models"
	"gorm.io/gorm/clause"
)

// instanceID identifies this process in the cron lease table. Container
// platforms set HOSTNAME to the container id, which is exactly what we want
// in the logs when two instances are competing.
var instanceID = func() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return "unknown"
}()

// AcquireCronLease tries to become the instance that runs a scheduled job.
//
// The whole decision is one conditional upsert, so two instances racing at the
// same second cannot both win: Postgres applies the WHERE on the conflicting
// row, and only one UPDATE reports a row affected.
func AcquireCronLease(name string, ttl time.Duration) bool {
	lease := models.CronLease{
		Name:      name,
		Holder:    instanceID,
		ExpiresAt: time.Now().Add(ttl),
		UpdatedAt: time.Now(),
	}

	result := config.DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "name"}},
		// Take the lease over only if the current one has expired — or if we
		// already hold it, so a restarted instance is not locked out by its
		// own stale entry.
		Where: clause.Where{Exprs: []clause.Expression{
			clause.Or(
				clause.Lt{Column: "cron_leases.expires_at", Value: time.Now()},
				clause.Eq{Column: "cron_leases.holder", Value: instanceID},
			),
		}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"holder":     instanceID,
			"expires_at": lease.ExpiresAt,
			"updated_at": time.Now(),
		}),
	}).Create(&lease)

	if result.Error != nil {
		// Failing closed is right here: a database we cannot reach is not a
		// good reason to start a job that costs money.
		log.Printf("cron lease %q: could not acquire (%v) — skipping this tick", name, result.Error)
		return false
	}
	return result.RowsAffected > 0
}

// ReleaseCronLease hands the lease back early so a redeploy does not leave the
// next tick waiting for the TTL to run out.
func ReleaseCronLease(name string) {
	config.DB.Model(&models.CronLease{}).
		Where("name = ? AND holder = ?", name, instanceID).
		Update("expires_at", time.Now().Add(-time.Second))
}
