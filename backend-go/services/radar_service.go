package services

import (
	"fmt"
)

type ProfileRadarPayload struct {
	ProfileURL string `json:"profile_url"`
}

// OptimizeProfileRadar calls the Python AI worker to scrape the profile URL and parse its requirements.
func OptimizeProfileRadar(profileURL string) (string, error) {
	payload := ProfileRadarPayload{
		ProfileURL: profileURL,
	}

	body, err := postToWorker(workerClient, "/internal/optimize-profile", payload)
	if err != nil {
		return "", fmt.Errorf("ai worker failed to optimize profile radar: %w", err)
	}

	return string(body), nil
}
