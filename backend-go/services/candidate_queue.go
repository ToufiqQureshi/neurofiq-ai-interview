package services

import (
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/config"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/models"
)

// The candidate queue: reading and writing models.BoardCandidate.
//
// Collection puts slugs in, admission takes due slugs out, and the row that
// survives in between is the only thing that remembers what the pipeline
// concluded. See models.BoardCandidate for why that memory is the point.

// Re-check policy.
//
// The two numbers that matter are how soon a transient failure is retried and
// how often a settled answer is questioned again. Both are deliberately
// finite: a board that would not talk to us is not a board that does not
// exist, and a company that was not hiring in August is not a company that
// will never hire.
const (
	// candidateRetryBase is the first backoff after a transient failure, and
	// candidateRetryFactor how fast it grows: 5m, 20m, 80m, 5h20, then capped.
	// Fast enough that a brief 429 costs one tick, slow enough that a host
	// having a bad day is not asked forty more times during it.
	candidateRetryBase   = 5 * time.Minute
	candidateRetryFactor = 4
	candidateRetryMax    = 12 * time.Hour

	// candidateRecheckInterval is how long a settled "dead" or "foreign"
	// answer stands before the board is looked at again.
	//
	// This is the single line that keeps the directory alive without anyone
	// touching it. Common Crawl publishes monthly, so without a re-check a
	// board seen once and empty that day would never be read again — a
	// company that starts hiring in October would wait for a crawl to
	// rediscover it, which it may never do because the board was already
	// indexed. Thirty days means every known board is re-read about once a
	// month for free, from the queue, with no new crawl needed at all.
	candidateRecheckInterval = 30 * 24 * time.Hour

	// candidateGiveUpAttempts is when repeated transient failures stop being
	// treated as transient. A board that has failed this many times in a row
	// is more likely gone than busy, so it drops to the monthly re-check
	// rather than holding a retry slot forever.
	candidateGiveUpAttempts = 6
)

// nextAttemptFor is the whole scheduler: a status and an attempt count decide
// when this row is next due. Jittered so that thirteen thousand candidates
// enqueued in one minute do not all come due in the same minute a month later.
func nextAttemptFor(status string, attempts int) time.Time {
	now := time.Now()
	switch status {
	case models.CandidatePending:
		return now
	case models.CandidateDeferred:
		delay := candidateRetryBase
		for i := 1; i < attempts && delay < candidateRetryMax; i++ {
			delay *= candidateRetryFactor
		}
		if delay > candidateRetryMax {
			delay = candidateRetryMax
		}
		return now.Add(jitter(delay, 0.2))
	case models.CandidateDead, models.CandidateForeign:
		return now.Add(jitter(candidateRecheckInterval, 0.25))
	default:
		// Stored, attached and rejected are never due again; the status
		// filter excludes them, and a far-future time makes that true twice.
		return now.Add(100 * 365 * 24 * time.Hour)
	}
}

// EnqueueCandidates writes newly-collected slugs into the queue.
//
// A slug already in the queue keeps its status and its schedule — collection
// must never reset the progress admission has made, or every crawl would put
// thirteen thousand settled rows back at the front of the line, which is the
// re-walk this table exists to end.
//
// What a repeat sighting may do is fill in detail. Common Crawl knows only a
// provider and a slug; the startup register knows a sector, a stage and
// coordinates that no board API reports. If the crawl saw a slug first, the
// register's richer copy should still land, so each optional column is
// overwritten only where the stored row left it blank.
func EnqueueCandidates(candidates []slugCandidate) (int, error) {
	candidates = dedupeCandidates(candidates)
	if len(candidates) == 0 {
		return 0, nil
	}

	rows := make([]models.BoardCandidate, 0, len(candidates))
	now := time.Now()
	for _, c := range candidates {
		rows = append(rows, models.BoardCandidate{
			Provider:      c.Provider,
			Slug:          c.Slug,
			Status:        models.CandidatePending,
			NextAttemptAt: now,
			Source:        c.Source,
			Name:          c.Name,
			Website:       c.Website,
			Sector:        c.Sector,
			Stage:         c.Stage,
			Area:          c.Area,
			Lat:           c.Lat,
			Lng:           c.Lng,
			FirstSeenAt:   now,
			UpdatedAt:     now,
		})
	}

	// keepOrFill leaves a stored value alone unless it is blank.
	keepOrFill := func(col string) clause.Expr {
		return gorm.Expr(fmt.Sprintf(
			"CASE WHEN COALESCE(board_candidates.%s, '') = '' THEN excluded.%s ELSE board_candidates.%s END",
			col, col, col))
	}
	keepOrFillNum := func(col string) clause.Expr {
		return gorm.Expr(fmt.Sprintf(
			"COALESCE(board_candidates.%s, excluded.%s)", col, col))
	}

	var added int64
	// Chunked because a single INSERT of thirteen thousand rows with an
	// ON CONFLICT clause builds a statement large enough to be its own
	// problem, and a failure in it loses the whole collection rather than one
	// batch of it.
	const chunk = 500
	for start := 0; start < len(rows); start += chunk {
		end := start + chunk
		if end > len(rows) {
			end = len(rows)
		}
		res := config.DB.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "provider"}, {Name: "slug"}},
			DoUpdates: clause.Assignments(map[string]interface{}{
				"name":       keepOrFill("name"),
				"website":    keepOrFill("website"),
				"sector":     keepOrFill("sector"),
				"stage":      keepOrFill("stage"),
				"area":       keepOrFill("area"),
				"lat":        keepOrFillNum("lat"),
				"lng":        keepOrFillNum("lng"),
				"updated_at": time.Now(),
			}),
		}).Create(rows[start:end])
		if res.Error != nil {
			return int(added), fmt.Errorf("enqueueing candidates: %w", res.Error)
		}
		added += res.RowsAffected
	}
	return int(added), nil
}

// DueCandidates returns the rows whose time has come, longest-waiting first.
//
// One ordered index (status, next_attempt_at) serves this at any table size,
// which is what makes the queue indifferent to how large it grows. Pending
// rows carry a next_attempt_at of "now", so new discoveries naturally sort
// ahead of a monthly re-check without needing a priority column.
func DueCandidates(limit int) ([]models.BoardCandidate, error) {
	if limit <= 0 {
		return nil, nil
	}
	var rows []models.BoardCandidate
	err := config.DB.
		Where("status IN ?", models.CandidateOpenStatuses).
		Where("next_attempt_at <= ?", time.Now()).
		Order("next_attempt_at ASC").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}

// interleaveByProvider reorders a batch so consecutive candidates rarely share
// a provider.
//
// Requests are paced per host (hostlimit.go), so a batch that arrives sorted
// by provider — which is exactly what an index scan produces — puts every
// worker in the same queue while nine other providers sit idle. Dealing the
// batch round-robin across providers means the workers spread out and the
// pass finishes in roughly the time its widest provider needs, instead of the
// sum of all of them.
func interleaveByProvider(rows []models.BoardCandidate) []models.BoardCandidate {
	if len(rows) < 2 {
		return rows
	}
	buckets := map[string][]models.BoardCandidate{}
	var order []string
	for _, r := range rows {
		if _, seen := buckets[r.Provider]; !seen {
			order = append(order, r.Provider)
		}
		buckets[r.Provider] = append(buckets[r.Provider], r)
	}

	out := make([]models.BoardCandidate, 0, len(rows))
	for len(out) < len(rows) {
		for _, p := range order {
			if len(buckets[p]) == 0 {
				continue
			}
			out = append(out, buckets[p][0])
			buckets[p] = buckets[p][1:]
		}
	}
	return out
}

// settleCandidate records what the pipeline concluded about one candidate and
// when it should be looked at again.
//
// attempts is reset by any real conclusion and incremented only by a deferral,
// so the backoff measures consecutive failures rather than lifetime ones — a
// board that failed five times last year and answered today starts clean.
func settleCandidate(c models.BoardCandidate, status string, companyID string, cause error) error {
	attempts := 0
	lastErr := ""
	if status == models.CandidateDeferred {
		attempts = c.Attempts + 1
		if cause != nil {
			lastErr = truncate(cause.Error(), 300)
		}
		// A board that will not answer after this many tries is more likely
		// gone than busy. Dropping it to the monthly re-check frees the retry
		// slot without discarding the candidate — the difference between
		// giving up on a run and giving up on a company.
		if attempts >= candidateGiveUpAttempts {
			status = models.CandidateDead
			attempts = 0
		}
	}

	updates := map[string]interface{}{
		"status":          status,
		"attempts":        attempts,
		"last_error":      lastErr,
		"next_attempt_at": nextAttemptFor(status, attempts),
		"updated_at":      time.Now(),
	}
	if companyID != "" {
		updates["company_id"] = companyID
	}
	return config.DB.Model(&models.BoardCandidate{}).
		Where("provider = ? AND slug = ?", c.Provider, c.Slug).
		Updates(updates).Error
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// CandidateQueueDepth counts what is waiting, by status.
//
// The health signal this pipeline lacked. "Nothing was stored this tick" reads
// identically whether the queue is empty because everything has been judged or
// because every candidate is deferred behind a host that is refusing us, and
// those need opposite responses. One GROUP BY answers it.
func CandidateQueueDepth() (map[string]int64, error) {
	var rows []struct {
		Status string
		N      int64
	}
	err := config.DB.Model(&models.BoardCandidate{}).
		Select("status, COUNT(*) AS n").
		Group("status").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(rows))
	for _, r := range rows {
		out[r.Status] = r.N
	}
	return out, nil
}

// DueCandidateCount is how much work is ready right now, which is what says
// whether the admission pass is keeping up with collection.
func DueCandidateCount() (int64, error) {
	var n int64
	err := config.DB.Model(&models.BoardCandidate{}).
		Where("status IN ?", models.CandidateOpenStatuses).
		Where("next_attempt_at <= ?", time.Now()).
		Count(&n).Error
	return n, err
}
