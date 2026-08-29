package services

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Repo represents a GitHub repository
type Repo struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	FullName    string `json:"full_name"`
	Private     bool   `json:"private"`
	Size        int    `json:"size"` // in KB
	HTMLURL     string `json:"html_url"`
	PushedAt    string `json:"pushed_at"`
	Description string `json:"description"`
	Language    string `json:"language"`
}

type CachedRepoList struct {
	ETag     string
	Repos    []Repo
	CachedAt time.Time
}

// In-memory cache for ETag caching (Simplest MVP solution, no need for DB table yet)
var repoCache = make(map[string]CachedRepoList)
var cacheMutex = sync.RWMutex{}

// GetReposWithETag fetches repositories for the user from GitHub API.
// It uses ETags to prevent rate limiting (returns HTTP 304 if data hasn't changed).
func GetReposWithETag(userID, token string) ([]Repo, error) {
	// 1. Check if we have a cached ETag for this user
	cacheMutex.RLock()
	cached, exists := repoCache[userID]
	cacheMutex.RUnlock()

	// 2. Prepare GitHub API request (sort by updated to get most relevant first)
	req, err := http.NewRequest("GET", "https://api.github.com/user/repos?sort=updated&per_page=100", nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	
	// 3. Attach ETag if it exists
	if exists && cached.ETag != "" {
		req.Header.Set("If-None-Match", cached.ETag)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch repos from github: %w", err)
	}
	defer resp.Body.Close()

	// 4. Handle Not Modified (304)
	if resp.StatusCode == http.StatusNotModified {
		return cached.Repos, nil
	}

	// 5. Handle Errors
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github api error: status %d", resp.StatusCode)
	}

	// 6. Parse new data
	var repos []Repo
	if err := json.NewDecoder(resp.Body).Decode(&repos); err != nil {
		return nil, fmt.Errorf("failed to parse github response: %w", err)
	}

	// 7. Restrict the list to a maximum of 3 repos
	if len(repos) > 3 {
		repos = repos[:3]
	}

	// 8. Save new data and new ETag to cache
	cacheMutex.Lock()
	repoCache[userID] = CachedRepoList{
		ETag:     resp.Header.Get("ETag"),
		Repos:    repos,
		CachedAt: time.Now(),
	}
	cacheMutex.Unlock()

	return repos, nil
}
