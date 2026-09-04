package models

import "time"

// BoardCandidate is one board slug the pipeline knows about but has not
// finished judging — the harvest's work queue, kept in the database rather
// than in a slice that dies with the process.
//
// # Why this table exists
//
// The harvest used to hold its whole candidate list in memory for one run and
// remember only which Common Crawl index it had read. That left three holes,
// and all three are the same hole: a decision made about a slug was thrown
// away the moment the run ended.
//
//   - A run capped at its store limit could not record the index as read
//     without losing everything past the cap, so it recorded nothing and the
//     next tick re-walked all 13,501 slugs — 12,000 live board calls to
//     re-learn what the last tick already knew.
//   - A board that answered 429 was filed as dead and dropped. Nothing
//     retried it, and nothing in the log told it apart from the 90% of
//     candidates that genuinely are dead.
//   - A board that was empty in August and hiring in October was never
//     looked at again, because the index it came from had been marked read.
//
// With the decision stored, each of those becomes ordinary: the queue is the
// progress, a transient failure is a row with a retry time, and a dead board
// is a row that comes back around next month. No run has to finish for work
// to be kept, and no work is repeated because a run was interrupted.
type BoardCandidate struct {
	// Provider and Slug together identify the board, and are the natural key —
	// the same pair boardKey() builds and FetchATSJobs switches on.
	Provider string `gorm:"primaryKey" json:"provider"`
	Slug     string `gorm:"primaryKey" json:"slug"`

	// Status is what the pipeline last concluded. See the CandidateStatus
	// constants for the meaning and the re-check policy of each.
	Status string `gorm:"index:idx_candidate_due,priority:1;not null;default:'pending'" json:"status"`

	// NextAttemptAt is when this row is due for another look. It is the whole
	// scheduler: every status maps to a delay, "pending" means now, and the
	// admission pass simply takes the rows whose time has come, oldest first.
	// A single ordered index serves that query at any table size.
	NextAttemptAt time.Time `gorm:"index:idx_candidate_due,priority:2;not null" json:"next_attempt_at"`

	// Attempts counts consecutive transient failures, and is what makes the
	// retry back off rather than hammer a host that is already struggling.
	// Reset whenever the candidate reaches a real conclusion.
	Attempts int `json:"attempts"`

	// LastError is the most recent transient failure, for the operator. It is
	// never used to make a decision — Status is.
	LastError string `json:"last_error"`

	// CompanyID is set once the candidate became, or joined, a company. It is
	// what lets an operator answer "where did this company come from" without
	// reading the logs.
	//
	// Text, not uuid, although it holds one. A candidate has no company for
	// most of its life, and Postgres rejects the empty string as a uuid — so
	// typed as uuid this column failed every insert of a new candidate, which
	// is every insert collection makes. A pointer would be the other answer;
	// text is the smaller one, and this is a breadcrumb for a person rather
	// than a foreign key anything joins on.
	CompanyID string `json:"company_id"`

	// Source names the harvester that suggested this board.
	Source string `json:"source"`

	// Everything below is optional detail the suggesting source happened to
	// know. Common Crawl knows none of it; the startup register knows a
	// sector, a stage and coordinates, which no board API reports. Stored
	// here so a candidate deferred today is still rich when it is retried
	// next week — in memory that detail was lost with the run.
	Name    string   `json:"name"`
	Website string   `json:"website"`
	Sector  string   `json:"sector"`
	Stage   string   `json:"stage"`
	Area    string   `json:"area"`
	Lat     *float64 `json:"lat"`
	Lng     *float64 `json:"lng"`

	FirstSeenAt time.Time `gorm:"default:now()" json:"first_seen_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Candidate statuses.
//
// The split that matters is between a conclusion and a failure to reach one.
// CandidateDead, CandidateForeign and CandidateRejected are answers: this
// board is empty, this board hires elsewhere, this slug is not an employer.
// CandidateDeferred is the absence of an answer — the host would not talk to
// us — and must never be stored as one of the others, because a "no" we
// invented is indistinguishable from a "no" the board gave us.
const (
	// CandidatePending has never been judged.
	CandidatePending = "pending"
	// CandidateDeferred hit a transient failure: a 429, a timeout, a 5xx.
	// Retried with an exponential backoff.
	CandidateDeferred = "deferred"
	// CandidateDead means the provider answered and the board is empty or
	// gone. Re-checked monthly: a company that was not hiring in August may
	// be hiring in October, and nothing else would ever notice.
	CandidateDead = "dead"
	// CandidateForeign means the board is live but carries no Indian role.
	// Re-checked monthly, for the same reason.
	CandidateForeign = "foreign"
	// CandidateRejected means the slug is structurally not an employer — a
	// vendor demo tenant, a section name, an aggregator. Nothing about that
	// changes with time, so it is never retried; the row is kept so the same
	// slug is not re-judged every time a crawl surfaces it again.
	CandidateRejected = "rejected"
	// CandidateStored became a new company.
	CandidateStored = "stored"
	// CandidateAttached joined a company already in the directory.
	CandidateAttached = "attached"
)

// CandidateOpenStatuses are the ones the admission pass will pick up again.
// Stored, attached and rejected are final — the first two because the company
// row now owns the board and the hourly job sync keeps it current, the third
// because it cannot change.
var CandidateOpenStatuses = []string{
	CandidatePending, CandidateDeferred, CandidateDead, CandidateForeign,
}
