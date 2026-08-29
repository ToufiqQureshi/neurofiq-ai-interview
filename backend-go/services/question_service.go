package services

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"

	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/config"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/models"
)

type GenerateQuestionsPayload struct {
	RepoFullName   string `json:"repo_full_name"`
	AnalysisData   string `json:"analysis_data"`
	HistorySummary string `json:"history_summary"`
}

// questionsPerInterview is how many questions one interview runs on. It is
// also the cache quorum: a partial set is treated as a miss.
const questionsPerInterview = 5

// GetOrGenerateQuestions returns the interview questions for one repository,
// generating them only when we have none for the current analysis.
//
// The cache matters more than it looks: without it every page load of the
// interview screen — including a refresh, and including React's development
// double-mount — is a fresh LLM call and five more rows in questions_bank.
func GetOrGenerateQuestions(userID string, repoFullName string) ([]models.Question, error) {
	if !ValidRepoFullName(repoFullName) {
		return nil, fmt.Errorf("invalid repository name")
	}

	// 1. The analysis has to exist, belong to this user, and be finished.
	var profile models.GithubProfile
	if err := config.DB.Where("user_id = ? AND repo_full_name = ?", userID, repoFullName).First(&profile).Error; err != nil {
		return nil, fmt.Errorf("repo analysis not found for user")
	}
	switch {
	case profile.StrategyUsed == "pending":
		return nil, fmt.Errorf("analysis is still running")
	case profile.StrategyUsed == "failed", profile.AnalysisJSON == "", profile.AnalysisJSON == "null":
		return nil, fmt.Errorf("analysis is not ready — retry analyzing this repository")
	}

	fingerprint := analysisFingerprint(profile.AnalysisJSON)

	// 2. Reuse the questions generated from *this* analysis if we have them.
	var cached []models.Question
	config.DB.Where("reusable = ? AND language = ? AND fingerprint = ?", true, repoFullName, fingerprint).
		Order("created_at asc").
		Limit(questionsPerInterview).
		Find(&cached)
	if len(cached) >= questionsPerInterview {
		return cached, nil
	}

	// 3. Miss: generate a fresh set.
	//
	// The generation itself is deliberately not serialized. An advisory lock
	// is transaction-scoped, so holding one across this call would pin a
	// pooled database connection for the length of an LLM round trip — up to
	// 90 seconds — and every concurrent request for the same repository would
	// queue behind it holding one too. A pool of 25 is exhausted long before
	// the worker answers, which turns a duplicated call into an outage. The
	// write is a different matter: see the short lock below.
	//
	// A duplicated call is cheap and rare by comparison: the interview page
	// requests once per repository and the paid endpoints are rate-limited per
	// user. The write below takes a short lock on the cache key, so a set that
	// loses the race is discarded rather than stored alongside the winner.
	payload := GenerateQuestionsPayload{
		RepoFullName:   repoFullName,
		AnalysisData:   profile.AnalysisJSON,
		HistorySummary: historySummaryFromAnalysis(profile.AnalysisJSON),
	}

	newQuestions, err := callPythonQuestionGenerator(payload)
	if err != nil {
		return nil, err
	}
	if len(newQuestions) == 0 {
		return nil, fmt.Errorf("the question generator returned nothing for this repository")
	}
	// A short set must never be cached. The read above treats fewer than five
	// rows as a miss, so caching four would regenerate and insert four more on
	// every single load — the table grows without bound and the five questions
	// returned come from two different generations.
	if len(newQuestions) < questionsPerInterview {
		return newQuestions, nil
	}

	// Cache exactly one interview's worth, so what is written back matches
	// what the read above asks for.
	newQuestions = newQuestions[:questionsPerInterview]

	// 4. Store them against this analysis so the next load is free.
	//
	// A partial write is worse than none: the next request would see fewer
	// than five rows, treat it as a miss, and generate (and store) five more.
	// One transaction keeps the set atomic.
	tx := config.DB.Begin()
	if tx.Error != nil {
		return newQuestions, nil // usable now; just not cached
	}

	// Lock the cache key for the recheck and the insert.
	//
	// The worker call is already done, so this holds a pooled connection for
	// the length of two local statements rather than an LLM round trip — the
	// reason the earlier version of this lock had to go. Without it the
	// recheck below is useless: under READ COMMITTED each request reads in its
	// own transaction and neither sees the other's uncommitted rows, so two
	// concurrent misses both find nothing and both insert, leaving ten rows
	// for one analysis. The key is built outside the call so nothing is
	// concatenated into the statement itself.
	lockKey := "questions:" + repoFullName + ":" + fingerprint
	if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", lockKey).Error; err != nil {
		tx.Rollback()
		return newQuestions, nil // usable now; just not cached
	}

	// Another request may have finished generating while we waited on the
	// worker. Take its set rather than writing a second one.
	var raced []models.Question
	tx.Where("reusable = ? AND language = ? AND fingerprint = ?", true, repoFullName, fingerprint).
		Order("created_at asc").Limit(questionsPerInterview).Find(&raced)
	if len(raced) >= questionsPerInterview {
		tx.Rollback()
		return raced, nil
	}

	for i := range newQuestions {
		newQuestions[i].Reusable = true
		newQuestions[i].Language = repoFullName
		newQuestions[i].Fingerprint = fingerprint
		if err := tx.Create(&newQuestions[i]).Error; err != nil {
			tx.Rollback()
			// The questions are still good — hand them back rather than
			// failing an interview over a cache write.
			return newQuestions, nil
		}
	}
	if err := tx.Commit().Error; err != nil {
		// Nothing was cached; the interview still runs on this set.
		log.Printf("questions: could not cache the set for %s: %v", repoFullName, err)
	}

	return newQuestions, nil
}

// analysisFingerprint is a short, stable id for one analysis result.
func analysisFingerprint(analysisJSON string) string {
	sum := sha256.Sum256([]byte(analysisJSON))
	return hex.EncodeToString(sum[:8])
}

// historySummaryFromAnalysis pulls the commit-history observations back out of
// the stored analysis so the question agent can spend one question on them.
func historySummaryFromAnalysis(analysisJSON string) string {
	var parsed struct {
		HistoryObservations []string `json:"history_observations"`
	}
	if err := json.Unmarshal([]byte(analysisJSON), &parsed); err != nil {
		return ""
	}
	summary := ""
	for _, obs := range parsed.HistoryObservations {
		summary += "- " + obs + "\n"
	}
	return summary
}

func callPythonQuestionGenerator(payload GenerateQuestionsPayload) ([]models.Question, error) {
	body, err := postToWorker(workerClient, "/internal/generate-questions", payload)
	if err != nil {
		return nil, err
	}

	var questions []models.Question
	if err := json.Unmarshal(body, &questions); err != nil {
		return nil, fmt.Errorf("failed to parse questions from the ai worker: %w", err)
	}
	return questions, nil
}
