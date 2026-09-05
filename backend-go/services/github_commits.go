package services

import (
	"encoding/json"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"
)

// githubGet performs one authenticated GitHub REST call with a bounded body
// read and a real timeout.
func githubGet(url, token string) ([]byte, int, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := githubClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	body, err := ReadCapped(resp.Body, 4<<20)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}

func truncateForError(body []byte) string {
	s := strings.TrimSpace(string(body))
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}

// noiseCommitPrefixes are messages that describe the process rather than the
// work: they say nothing an interview question can be built on.
var noiseCommitPrefixes = []string{
	"merge pull request", "merge branch", "merge remote", "initial commit",
	"update readme", "bump", "chore(deps)", "wip", "fixup",
	"revert \"", "version bump", "release v", "typo", "formatting",
	"lint", "prettier", "gitignore", "add license",
}

// collectCommitStats reads the repository's history so a question can ask
// about *how* the code got here, not only what it currently says.
//
// Two calls, both cheap against the same rate limit the zipball already uses:
// /contributors gives an exact commit total (the sum of per-author counts)
// without paginating the whole log, and /commits gives the recent messages.
// Everything here is best-effort — a repository whose history is unreadable
// still produces a perfectly good code interview, so nothing returns an error.
func collectCommitStats(repoFullName, token string, meta repoMeta) CommitStats {
	stats := CommitStats{
		FirstCommitAt: meta.CreatedAt,
		LastCommitAt:  meta.PushedAt,
	}

	// Contributors, with anon=1 so commits from addresses that never linked a
	// GitHub account still count toward the total.
	body, status, err := githubGet(
		"https://api.github.com/repos/"+repoFullName+"/contributors?per_page=100&anon=1", token)
	if err != nil {
		log.Printf("commit stats: contributors call failed for %s: %v", repoFullName, err)
	} else if status == http.StatusOK {
		var contributors []struct {
			Login         string `json:"login"`
			Contributions int    `json:"contributions"`
		}
		if err := json.Unmarshal(body, &contributors); err == nil {
			stats.Contributors = len(contributors)
			for _, c := range contributors {
				stats.TotalCommits += c.Contributions
			}
		}
	}

	// Recent history.
	body, status, err = githubGet(
		"https://api.github.com/repos/"+repoFullName+"/commits?per_page=100", token)
	if err != nil {
		log.Printf("commit stats: commits call failed for %s: %v", repoFullName, err)
		return stats
	}
	if status != http.StatusOK {
		return stats
	}

	var commits []struct {
		Commit struct {
			Message string `json:"message"`
			Author  struct {
				Name string `json:"name"`
				Date string `json:"date"`
			} `json:"author"`
		} `json:"commit"`
	}
	if err := json.Unmarshal(body, &commits); err != nil {
		return stats
	}

	// Track which pieces of work get revisited: a subject line that recurs is
	// the strongest "you changed your mind here" signal available without
	// fetching every diff.
	subjectCount := map[string]int{}
	var candidates []NotableCommit

	for _, c := range commits {
		subject := strings.TrimSpace(strings.SplitN(c.Commit.Message, "\n", 2)[0])
		if subject == "" || len(subject) < 12 || isNoiseCommit(subject) {
			continue
		}
		if len(subject) > 160 {
			subject = subject[:160] + "…"
		}
		subjectCount[normalizeSubject(subject)]++
		candidates = append(candidates, NotableCommit{
			Message: subject,
			Date:    shortDate(c.Commit.Author.Date),
			Author:  c.Commit.Author.Name,
		})
	}

	// Prefer commits whose subject recurs — those are the rewrites — then
	// fall back to the most recent substantive work.
	sort.SliceStable(candidates, func(i, j int) bool {
		return subjectCount[normalizeSubject(candidates[i].Message)] >
			subjectCount[normalizeSubject(candidates[j].Message)]
	})

	if len(candidates) > 25 {
		candidates = candidates[:25]
	}
	stats.NotableCommits = candidates

	if stats.TotalCommits == 0 {
		stats.TotalCommits = len(commits)
	}
	if stats.Contributors == 0 && len(commits) > 0 {
		stats.Contributors = 1
	}
	return stats
}

func isNoiseCommit(subject string) bool {
	lower := strings.ToLower(subject)
	for _, prefix := range noiseCommitPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

// normalizeSubject strips the parts of a message that change between two
// passes at the same problem, so "fix auth middleware" and "fix auth
// middleware again" collapse to one key.
func normalizeSubject(subject string) string {
	lower := strings.ToLower(subject)
	for _, filler := range []string{" again", " properly", " for real", " v2", " take 2", " once more"} {
		lower = strings.ReplaceAll(lower, filler, "")
	}
	return strings.Join(strings.Fields(lower), " ")
}

func shortDate(raw string) string {
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return raw
	}
	return t.Format("2006-01-02")
}


