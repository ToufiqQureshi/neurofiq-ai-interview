package services

import (
	"log"

	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/config"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/models"
)

// LogFailedRequest saves an API or scrape failure to the database.
// It is fire-and-forget: if this insert fails, it only logs the error rather
// than propagating it, because failing to log a failure shouldn't crash the pipeline.
func LogFailedRequest(provider, query, reason string) {
	if config.DB == nil {
		return
	}

	failure := models.FailedRequest{
		Provider: provider,
		Query:    query,
		Reason:   reason,
	}

	if err := config.DB.Create(&failure).Error; err != nil {
		log.Printf("error: failed to write failed_request to db for %s: %v", provider, err)
	}
}
