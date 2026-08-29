package services

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Repo represents a GitHub repository as the repo picker needs it.
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

const (
	// repoCacheTTL bounds how long a user's entry survives without being
	// touched. The cache is only an ETag holder — expiring an entry costs one
	// conditional request, not correctness.
	repoCacheTTL = 30 * time.Minute

	// maxCachedUsers is the hard ceiling. This map used to grow forever: one
	// entry per user who ever logged in, each holding up to 100 repo records.
	// At a few thousand signups that is a slow memory leak with no ceiling
	// and no way to reclaim it short of a restart.
	maxCachedUsers = 5000
)

// In-memory ETag cache. Deliberately not in Postgres: the only thing lost on
// a restart is one conditional request per user.
var repoCache = make(map[string]CachedRepoList)
var cacheMutex = sync.RWMutex{}

// GetReposWithETag fetches the user's repositories from GitHub.
//
// It sends the previously stored ETag, so an unchanged list comes back as a
// 304 — which GitHub does not charge against the rate limit. Returning users
// therefore get an instant list and the quota stays intact for the analysis
// calls that actually need it.
func GetReposWithETag(userID, token string) ([]Repo, error) {
	// 1. Look for a cached ETag for this user.
	cacheMutex.RLock()
	cached, exists := repoCache[userID]
	cacheMutex.RUnlock()

	// 2. Ask GitHub, newest-pushed first so the most relevant repos lead.
	req, err := http.NewRequest("GET", "https://api.github.com/user/repos?sort=updated&per_page=100", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	// 3. Attach the ETag if we have one.
	if exists && cached.ETag != "" {
		req.Header.Set("If-None-Match", cached.ETag)
	}

	resp, err := githubClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch repos from github: %w", err)
	}
	defer resp.Body.Close()

	// 4. Not Modified — the cached list is still current and this cost zero
	//    rate-limit quota.
	if resp.StatusCode == http.StatusNotModified {
		return cached.Repos, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github api error: status %d", resp.StatusCode)
	}

	var repos []Repo
	body, err := ReadCapped(resp.Body, 8<<20)
	if err != nil {
		return nil, fmt.Errorf("github response too large: %w", err)
	}
	if err := json.Unmarshal(body, &repos); err != nil {
		return nil, fmt.Errorf("failed to parse github response: %w", err)
	}

	// 5. Store the new list and its ETag.
	cacheMutex.Lock()
	repoCache[userID] = CachedRepoList{
		ETag:     resp.Header.Get("ETag"),
		Repos:    repos,
		CachedAt: time.Now(),
	}
	evictStaleRepoCacheLocked()
	cacheMutex.Unlock()

	return repos, nil
}

// InvalidateRepoCache drops a user's cached repo list. Called on logout so a
// signed-out user's repository names do not sit in memory indefinitely.
func InvalidateRepoCache(userID string) {
	cacheMutex.Lock()
	delete(repoCache, userID)
	cacheMutex.Unlock()
}

// evictStaleRepoCacheLocked keeps the cache bounded. The caller must hold the
// write lock.
func evictStaleRepoCacheLocked() {
	if len(repoCache) <= maxCachedUsers {
		// Cheap pass: drop anything past its TTL only when the map is
		// actually growing, so the common path stays a single map write.
		if len(repoCache)%256 != 0 {
			return
		}
	}
	cutoff := time.Now().Add(-repoCacheTTL)
	for id, entry := range repoCache {
		if entry.CachedAt.Before(cutoff) {
			delete(repoCache, id)
		}
	}
	// Still over the ceiling after expiry (a burst of active users): drop the
	// oldest entries until we are back under it.
	for len(repoCache) > maxCachedUsers {
		oldestID, oldest := "", time.Now()
		for id, entry := range repoCache {
			if entry.CachedAt.Before(oldest) {
				oldestID, oldest = id, entry.CachedAt
			}
		}
		if oldestID == "" {
			break
		}
		delete(repoCache, oldestID)
	}
}
