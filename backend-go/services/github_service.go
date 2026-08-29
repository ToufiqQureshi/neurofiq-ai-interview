package services

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

type Repo struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	FullName    string `json:"full_name"`
	Private     bool   `json:"private"`
	Size        int    `json:"size"`
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

var repoCache = make(map[string]CachedRepoList)
var cacheMutex = sync.RWMutex{}

func GetReposWithETag(userID, token string) ([]Repo, error) {
	cacheMutex.RLock()
	cached, exists := repoCache[userID]
	cacheMutex.RUnlock()

	req, err := http.NewRequest("GET", "https://api.github.com/user/repos?sort=updated&per_page=100", nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	if exists && cached.ETag != "" {
		req.Header.Set("If-None-Match", cached.ETag)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch repos from github: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return cached.Repos, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github api error: status %d", resp.StatusCode)
	}

	var repos []Repo
	if err := json.NewDecoder(resp.Body).Decode(&repos); err != nil {
		return nil, fmt.Errorf("failed to parse github response: %w", err)
	}

	cacheMutex.Lock()
	repoCache[userID] = CachedRepoList{
		ETag:     resp.Header.Get("ETag"),
		Repos:    repos,
		CachedAt: time.Now(),
	}
	cacheMutex.Unlock()

	return repos, nil
}
