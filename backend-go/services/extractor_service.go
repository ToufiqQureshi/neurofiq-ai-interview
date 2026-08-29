package services

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/config"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/models"
)

type CodeSnippet struct {
	File      string `json:"file"`
	LineRange string `json:"line_range"`
	Content   string `json:"content"`
}

type StructureSummary struct {
	DirectoryTree string   `json:"directory_tree"`
	Languages     []string `json:"languages"`
}

type CommitStats struct {
	TotalCommits int `json:"total_commits"`
	Contributors int `json:"contributors"`
}

type AnalyzePayload struct {
	RepoFullName     string           `json:"repo_full_name"`
	StructureSummary StructureSummary `json:"structure_summary"`
	CodeSnippets     []CodeSnippet    `json:"code_snippets"`
	CommitStats      CommitStats      `json:"commit_stats"`
}

const (
	MaxTotalCharacters = 60000 // roughly 15k tokens limit
)

// AnalyzeAndExtract downloads the repo zip, extracts key files, and calls the Python worker.
func AnalyzeAndExtract(userID, repoFullName, token string) (string, error) {
	// 1. Get default branch
	defaultBranch, err := getDefaultBranch(repoFullName, token)
	if err != nil {
		return "", fmt.Errorf("failed to get branch: %w", err)
	}

	// 2. Download zip
	zipData, err := downloadZip(repoFullName, defaultBranch, token)
	if err != nil {
		return "", fmt.Errorf("failed to download zip: %w", err)
	}

	// 3. Extract and score files
	snippets, treeStr, err := processZip(zipData)
	if err != nil {
		return "", fmt.Errorf("failed to process zip: %w", err)
	}

	payload := AnalyzePayload{
		RepoFullName: repoFullName,
		StructureSummary: StructureSummary{
			DirectoryTree: treeStr,
			Languages:     []string{"Auto-detected from files"},
		},
		CodeSnippets: snippets,
		CommitStats: CommitStats{ // Dummy for MVP
			TotalCommits: 1,
			Contributors: 1,
		},
	}

	// 4. Call Python Worker for AI Analysis
	analysisJSON, err := callPythonWorker(payload)
	if err != nil {
		return "", fmt.Errorf("ai worker failed: %w", err)
	}

	// 5. Save the final JSON to Postgres DB (Caching).
	// The controller already reserved a "pending" placeholder row for
	// (userID, repoFullName) before starting this background job (see
	// HandleAnalyzeRepo), so this fills that row in rather than inserting a
	// new one — a blind insert here would violate the unique index.
	now := time.Now()
	result := config.DB.Model(&models.GithubProfile{}).
		Where("user_id = ? AND repo_full_name = ?", userID, repoFullName).
		Updates(map[string]interface{}{
			"repo_size_kb":  len(zipData) / 1024,
			"strategy_used": "full_scan",
			"analysis_json": analysisJSON,
			"analyzed_at":   now,
			"expires_at":    now.Add(7 * 24 * time.Hour),
		})
	if result.Error != nil {
		return "", fmt.Errorf("failed to save to db: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return "", fmt.Errorf("no reserved profile row found for %s/%s", userID, repoFullName)
	}

	return analysisJSON, nil
}

func getDefaultBranch(repoFullName, token string) (string, error) {
	req, _ := http.NewRequest("GET", "https://api.github.com/repos/"+repoFullName, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("github api status %d: %s", resp.StatusCode, string(body))
	}

	var data struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", err
	}
	return data.DefaultBranch, nil
}

func downloadZip(repoFullName, branch, token string) ([]byte, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/zipball/%s", repoFullName, branch)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("github api status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func processZip(zipData []byte) ([]CodeSnippet, string, error) {
	reader, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return nil, "", err
	}

	var allFiles []string
	type fileData struct {
		name    string
		content string
		score   int
	}
	var files []fileData

	for _, f := range reader.File {
		// Remove root folder from path (e.g. owner-repo-sha/...)
		parts := strings.SplitN(f.Name, "/", 2)
		if len(parts) < 2 {
			continue
		}
		name := parts[1]
		if name == "" {
			continue
		}
		allFiles = append(allFiles, name)

		if f.FileInfo().IsDir() || strings.Contains(name, "node_modules") || strings.Contains(name, ".git") || strings.Contains(name, "vendor") {
			continue
		}

		// Ponytail Rule: Keep scoring simple. Give points to entry/manifest files.
		score := 0
		if strings.HasSuffix(name, ".go") || strings.HasSuffix(name, ".py") || strings.HasSuffix(name, ".ts") || strings.HasSuffix(name, ".js") {
			score += 10
		}
		if name == "main.go" || name == "main.py" || name == "package.json" || name == "go.mod" {
			score += 50
		}

		if score > 0 {
			rc, _ := f.Open()
			content, _ := io.ReadAll(rc)
			rc.Close()
			files = append(files, fileData{name: name, content: string(content), score: score})
		}
	}

	// Sort files by score descending
	sort.Slice(files, func(i, j int) bool {
		return files[i].score > files[j].score
	})

	var snippets []CodeSnippet
	totalChars := 0
	for _, f := range files {
		// Hard budget enforcement!
		if totalChars+len(f.content) > MaxTotalCharacters {
			break
		}
		snippets = append(snippets, CodeSnippet{
			File:      f.name,
			LineRange: "all",
			Content:   f.content,
		})
		totalChars += len(f.content)
	}

	// Create directory tree string (max 2000 chars)
	treeStr := strings.Join(allFiles, "\n")
	if len(treeStr) > 2000 {
		treeStr = treeStr[:2000] + "\n... (truncated)"
	}

	return snippets, treeStr, nil
}

func callPythonWorker(payload AnalyzePayload) (string, error) {
	workerURL := os.Getenv("PYTHON_WORKER_URL")
	if workerURL == "" {
		workerURL = "http://localhost:8001"
	}
	
	jsonData, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", workerURL+"/internal/analyze-repo", bytes.NewBuffer(jsonData))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Secret", os.Getenv("INTERNAL_SECRET"))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("python worker error: %s", string(body))
	}
	
	return string(body), nil
}
