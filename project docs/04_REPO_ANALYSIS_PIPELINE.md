# Repo Analysis Pipeline — Cost-Safe Design

**Core rule: AI cost must NEVER scale with repo size.** A repo with 500
lines and a repo with 500,000 lines must cost roughly the same to analyze.
This is enforced with hard caps, not best-effort limits.

**Ownership split:** Go does everything deterministic and all GitHub API
calls. Go calls the Python worker's `/internal/analyze-repo` exactly ONCE
per analysis, with an already-trimmed, budget-capped payload. The Python
worker itself does not fetch anything from GitHub — it only calls Claude
with what Go gives it.

## Step-by-Step Pipeline

1. **Repo size check** (Go, GitHub API metadata call, free, no Claude):

   ```
   GET https://api.github.com/repos/{owner}/{repo}  → repo["size"] (KB)
   ```
2. **Go picks a strategy based on size:**

   | Repo size | Strategy            | Max files sent to Claude            |
   | --------- | ------------------- | ----------------------------------- |
   | < 5 MB    | `full_scan`       | 60                                  |
   | 5–50 MB  | `smart_sample`    | 40                                  |
   | > 50 MB   | `structural_only` | 20 (structure + tiny snippets only) |
3. **Deterministic extraction — done entirely in Go, FREE, no Claude call:**

   - Language/framework detection from file extensions + manifest files
     (`package.json`, `requirements.txt`, `pyproject.toml`, `go.mod`, etc.)
   - Directory structure (tree, depth-limited to 3)
   - Commit stats via `/repos/{owner}/{repo}/commits` and
     `/stats/contributors`
   - File filtering: skip `node_modules`, lockfiles, binaries, vendored/
     generated code, `dist/`, `build/`
4. **Smart file selection (scored, not random, hard-capped) — Go:**
   Priority scoring — entry points (`main.py`, `app.py`, `index.ts`,
   `main.go`) score highest, then config/manifest files, then
   recently-modified files, then moderate-size files (20–500 lines
   preferred). Sort by score, take top N per the table above.
5. **Token budget enforcement (hard cap, non-negotiable) — Go, before the
   internal call is even made:**

   ```go
   const (
       MaxFilesToAnalyze    = 40
       MaxLinesPerFile      = 300
       MaxTotalInputTokens  = 15000 // hard stop
   )
   ```

   Even a 10-lakh-line repo will never send more than ~15k input tokens'
   worth of payload to the Python worker (and therefore to Claude). This is
   the single most important cost-control mechanism in the whole product,
   and it lives in Go — the Python worker should not need its own
   size-limiting logic because Go never sends it more than the budget.
6. **One internal call, Go → Python (`/internal/analyze-repo`)**, combining
   structure summary + curated snippets. Python worker makes exactly ONE
   batched Claude call → returns architecture patterns, complexity rating,
   code quality signals as structured JSON.
7. **Go caches the result** in `github_profiles.analysis_json` with 7-day
   TTL.

## Candidate-Controlled Scoping (for large monorepos)

If a repo is very large (e.g. a monorepo like Staybooker), let the
candidate scope the analysis instead of forcing `structural_only` blindly:

```
UI: "This repo is large (1200+ files). Which part should the interview
     focus on?"
[ ] Backend (backend/)
[ ] Frontend (frontend/)
[ ] Full structural overview only
```

This keeps cost predictable AND gives the candidate a better, more relevant
interview. This choice is handled entirely in Go before the file-selection
step runs.

## Repo List & Repo-Level Caching (Go, not analysis — just listing repos)

- Frontend fetches repo list once per session, cached in React state; a
  manual "Refresh" button re-fetches.
- Go caches the repo list with a 1-hour TTL and uses GitHub's ETag support
  (`If-None-Match` header) so unchanged data costs zero GitHub rate-limit
  budget (304 Not Modified response).
- Repo *analysis* (the expensive part) is cached for 7 days as described
  above, separate from the repo *list* cache.

```go
func GetReposWithETag(userID, token string) ([]Repo, error) {
    cached := getCachedRepos(userID)
    req, _ := http.NewRequest("GET", "https://api.github.com/user/repos", nil)
    req.Header.Set("Authorization", "token "+token)
    if cached != nil && cached.ETag != "" {
        req.Header.Set("If-None-Match", cached.ETag)
    }

    resp, err := httpClient.Do(req)
    if err != nil {
        return nil, err
    }
    if resp.StatusCode == http.StatusNotModified {
        return cached.Data, nil
    }

    var repos []Repo
    json.NewDecoder(resp.Body).Decode(&repos)
    saveReposCache(userID, repos, resp.Header.Get("ETag"))
    return repos, nil
}
```

## Repo Selection UX (avoiding "which of my 50 repos?" problem)

- Default filter: hide forks, sort by "recently updated."
- Manual override: "Repo not showing? Paste URL" — covers org-owned repos,
  older/less-active repos, or repos matching a specific CV bullet point.
- "Pin" concept: candidate pins 1–3 repos as "My main projects" so they
  don't have to re-search every session. Pin limits: free tier = 2, paid
  tier = 10 (enforced in Go via `pinned_repos` table, see doc 02).

## Internal Contract: Go → Python `/internal/analyze-repo`

```json
// Request (Go → Python)
{
  "repo_full_name": "owner/repo",
  "structure_summary": { "directory_tree": "...", "languages": [...] },
  "code_snippets": [
    {"file": "app/main.py", "line_range": "1-45", "content": "..."}
  ],
  "commit_stats": { "total_commits": 340, "contributors": 1 }
}

// Response (Python → Go)
{
  "architecture_patterns": ["MVC", "Repository pattern"],
  "complexity": "intermediate",
  "quality_signals": ["consistent naming", "some duplication in handlers"],
  "db_usage": "PostgreSQL via SQLAlchemy",
  "dependencies": ["fastapi", "supabase-py"]
}
```

Python worker performs NO file selection, NO token budgeting, NO GitHub
calls — it trusts that Go already enforced the 15k-token budget before this
request was made.

## Expected Cost Per Analysis

Regardless of repo size: **~$0.05–0.08 per analysis**, because input tokens
are hard-capped at 15,000 and it's a single Claude call inside a single
Python worker request.
