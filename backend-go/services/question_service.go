package services

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/config"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/models"
)

type GenerateQuestionsPayload struct {
	RepoFullName   string `json:"repo_full_name"`
	AnalysisData   string `json:"analysis_data"`
	HistorySummary string `json:"history_summary"`

	// TargetRole is the opening the candidate pressed "Practice" on, as
	// "<title> at <company>". Empty for an ordinary interview, and the worker
	// treats it as optional framing — the questions stay grounded in the
	// candidate's own code either way, because that is the only thing we can
	// ask about with authority.
	TargetRole string `json:"target_role,omitempty"`
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
//
// jobID and companyID are optional: what the candidate pressed "Practice" on
// in the Job Map, a specific opening or a whole company. They only ever frame
// the questions — the analysis is still what they are asked about — and an id
// that names nothing is ignored rather than refused, because a role filled
// overnight must not be able to block an interview.
func GetOrGenerateQuestions(userID, repoFullName, jobID, companyID string) ([]models.Question, error) {
	if !ValidRepoFullName(repoFullName) {
		return nil, fmt.Errorf("invalid repository name")
	}

	targetRole := interviewTarget(jobID, companyID)

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

	// The role is part of the cache key, not just the prompt. A set generated
	// for "Backend Engineer at Razorpay" answers a different question from the
	// plain set, and keying both on the analysis alone would serve whichever
	// was generated first to everyone — the role would appear to work once and
	// then silently stop.
	fingerprint := analysisFingerprint(profile.AnalysisJSON + "\x00" + targetRole)

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
		TargetRole:     targetRole,
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

// interviewTarget turns what the candidate pressed "Practice" on into the one
// line the question generator needs — "<title> at <company>" from a role, or
// just the company when they started from a company rather than a listing.
//
// Both ids are resolved against the database rather than the label being
// posted from the browser. The Job Map's whole claim is that its roles are
// real ones it found, and an interview framed by a title a client typed would
// quietly break it: the transcript would name a company that never posted the
// role. This way only something the pipeline actually stored can frame an
// interview.
//
// Every failure returns "" and the interview runs unframed. A missing job is
// the ordinary case, not an error — listings are pruned when a board drops
// them, so a bookmarked link outlives the opening — and losing the framing is
// a far smaller harm than refusing to interview.
func interviewTarget(jobID, companyID string) string {
	if config.DB == nil {
		return ""
	}

	if id := strings.TrimSpace(jobID); id != "" {
		var job models.Job
		if err := config.DB.Where("id = ?", id).First(&job).Error; err == nil {
			if title := strings.TrimSpace(job.Title); title != "" {
				if name := companyName(job.CompanyID); name != "" {
					return title + " at " + name
				}
				// The role outlived its company, which sweepOrphanJobs exists
				// to clean up. Still usable framing on its own.
				return title
			}
		}
		// A job id that names nothing falls through to the company, if one
		// was sent: a stale listing should not also lose the company context
		// the candidate could still practise against.
	}

	if name := companyName(strings.TrimSpace(companyID)); name != "" {
		return "an opening at " + name
	}
	return ""
}

func companyName(companyID string) string {
	if companyID == "" || config.DB == nil {
		return ""
	}
	var company models.Company
	if err := config.DB.Where("id = ?", companyID).First(&company).Error; err != nil {
		return ""
	}
	return strings.TrimSpace(company.Name)
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
