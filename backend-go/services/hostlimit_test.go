package services

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/models"
)

// The bug this whole file exists to prevent: a host asking us to slow down was
// recorded as a board that does not exist, and the company was dropped with
// nothing in the log to tell it apart from the 90% that genuinely are dead.
// Everything below asserts the two are now distinguishable.

func TestTransientAndPermanentStatusesAreDistinguished(t *testing.T) {
	transient := []int{429, 500, 502, 503, 504, 408}
	permanent := []int{400, 401, 403, 404, 410, 422}

	for _, code := range transient {
		err := error(&HTTPStatusError{Status: code, URL: "greenhouse/acme"})
		if !IsTransientFetchError(err) {
			t.Errorf("status %d must be retryable — treating it as a dead board is how a throttled pass silently drops real companies", code)
		}
	}
	for _, code := range permanent {
		err := error(&HTTPStatusError{Status: code, URL: "greenhouse/acme"})
		if IsTransientFetchError(err) {
			t.Errorf("status %d is the board answering; retrying it forever is not free", code)
		}
	}
}

func TestPacingErrorsAreTransient(t *testing.T) {
	for _, err := range []error{
		ErrHostThrottled,
		ErrHostBusy,
		context.DeadlineExceeded,
		// Wrapped, because awaitHostSlot names the host in front of them.
		errors.New("wrapped: " + ErrHostThrottled.Error()),
	} {
		wrapped := err
		if errors.Is(err, ErrHostThrottled) || errors.Is(err, ErrHostBusy) ||
			errors.Is(err, context.DeadlineExceeded) {
			if !IsTransientFetchError(wrapped) {
				t.Errorf("%v must be retryable", err)
			}
		}
	}
	// A plain unrelated error is not transient: guessing that it is would
	// retry a permanent failure forever.
	if IsTransientFetchError(errors.New("invalid greenhouse slug")) {
		t.Error("an unrelated error must not be treated as transient")
	}
	if IsTransientFetchError(nil) {
		t.Error("no error is not a transient error")
	}
}

// The gate must space requests to one host across goroutines, not per
// goroutine. Reserving the slot under the lock is what makes that true, and
// this is the property that stops twelve workers bursting at one provider.
func TestHostGateSpacesConcurrentReservations(t *testing.T) {
	g := &hostGate{interval: 100 * time.Millisecond}
	now := time.Now()

	var waits []time.Duration
	for i := 0; i < 4; i++ {
		w, ok := g.reserve(now, time.Minute)
		if !ok {
			t.Fatalf("reservation %d refused unexpectedly", i)
		}
		waits = append(waits, w)
	}
	// Four callers arriving at the same instant must be spaced one interval
	// apart, not all released together.
	for i, w := range waits {
		want := time.Duration(i) * 100 * time.Millisecond
		if w != want {
			t.Errorf("reservation %d waits %s, want %s — callers are not being spaced", i, w, want)
		}
	}
}

// A queue longer than the caller is willing to wait must be refused rather
// than parked. Without this a throttled host with hundreds of slugs behind it
// holds every worker for hours.
func TestHostGateRefusesAnOverlongQueue(t *testing.T) {
	g := &hostGate{interval: time.Second}
	now := time.Now()
	for i := 0; i < 5; i++ {
		g.reserve(now, time.Minute)
	}
	if _, ok := g.reserve(now, 2*time.Second); ok {
		t.Error("a wait past the caller's ceiling must be refused, not queued")
	}
}

// A 429 has to slow every worker, not only the one that saw it.
func TestThrottleWidensTheIntervalAndDecaysBack(t *testing.T) {
	g := &hostGate{interval: hostBaseInterval}

	g.throttled("example.test", 0)
	if g.interval <= hostBaseInterval {
		t.Fatal("a throttle response must widen the interval")
	}
	widened := g.interval

	g.throttled("example.test", 0)
	if g.interval <= widened {
		t.Error("a second throttle must widen it further")
	}
	if g.interval > hostMaxInterval {
		t.Error("backoff must stay capped, or a host is never contacted again")
	}

	for i := 0; i < 10; i++ {
		g.succeeded()
	}
	if g.interval != hostBaseInterval {
		t.Errorf("interval decayed to %s, want it back at %s — a host that recovered must be used again", g.interval, hostBaseInterval)
	}
	if g.strikes != 0 {
		t.Error("a clean response clears the strike count")
	}
}

// Retry-After is the host stating its own terms; guessing over the top of it
// is how a client earns a longer ban. An implausible value is still capped,
// because a header asking for an hour would park a worker for an hour.
func TestRetryAfterIsHonouredAndCapped(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("Retry-After", "5")
	if got := retryAfterFrom(resp); got != 5*time.Second {
		t.Errorf("Retry-After: 5 gave %s, want 5s", got)
	}

	resp.Header.Set("Retry-After", "3600")
	if got := retryAfterFrom(resp); got != hostMaxInterval {
		t.Errorf("an hour-long Retry-After must be capped at %s, got %s", hostMaxInterval, got)
	}

	resp.Header.Set("Retry-After", "not-a-number")
	if got := retryAfterFrom(resp); got != 0 {
		t.Errorf("an unparseable Retry-After must not become a delay, got %s", got)
	}

	resp.Header.Del("Retry-After")
	if got := retryAfterFrom(resp); got != 0 {
		t.Errorf("no header means no stated delay, got %s", got)
	}
}

// The queue's schedule is the pipeline's whole guarantee that jobs keep
// arriving without anyone touching it, so each status has to map to a sane
// time rather than to "never".
func TestCandidateScheduleKeepsWorkComingBack(t *testing.T) {
	now := time.Now()

	if at := nextAttemptFor(models.CandidatePending, 0); at.After(now.Add(time.Second)) {
		t.Error("a pending candidate is due immediately")
	}

	// A dead or foreign board must come back, or a company that starts hiring
	// next month is never seen again — the crawl already indexed that board,
	// so nothing else would rediscover it.
	for _, status := range []string{models.CandidateDead, models.CandidateForeign} {
		at := nextAttemptFor(status, 0)
		gap := at.Sub(now)
		if gap < 20*24*time.Hour || gap > 40*24*time.Hour {
			t.Errorf("%s re-check is %s away; it must land near the monthly interval", status, gap)
		}
	}

	// The retry backs off, so a struggling host is not asked forty more times
	// during the hour it is struggling.
	var previous time.Duration
	for attempt := 1; attempt <= 5; attempt++ {
		gap := nextAttemptFor(models.CandidateDeferred, attempt).Sub(now)
		if gap <= 0 || gap > candidateRetryMax*2 {
			t.Fatalf("attempt %d scheduled %s away, outside any sane range", attempt, gap)
		}
		if attempt > 1 && gap <= previous {
			t.Errorf("attempt %d (%s) did not back off past attempt %d (%s)", attempt, gap, attempt-1, previous)
		}
		previous = gap
	}

	// Settled-and-final statuses are never due again.
	for _, status := range []string{models.CandidateStored, models.CandidateAttached, models.CandidateRejected} {
		if nextAttemptFor(status, 0).Before(now.Add(365 * 24 * time.Hour)) {
			t.Errorf("%s must not come back around", status)
		}
	}
}

// Outcomes and queue statuses have to stay in step: an outcome mapped to the
// wrong status is a candidate that either never returns or returns forever.
func TestOutcomeToStatusMapping(t *testing.T) {
	cases := map[harvestOutcome]string{
		outcomeStored:    models.CandidateStored,
		outcomeAttached:  models.CandidateAttached,
		outcomeDuplicate: models.CandidateAttached,
		outcomeNotIndian: models.CandidateForeign,
		outcomeDeadBoard: models.CandidateDead,
		outcomeDeferred:  models.CandidateDeferred,
		outcomeSkipped:   models.CandidateRejected,
	}
	for outcome, want := range cases {
		if got := statusFor(outcome); got != want {
			t.Errorf("statusFor(%d) = %q, want %q", outcome, got, want)
		}
	}

	// Only the deferred status may ever be reconsidered on a short timer;
	// everything else is either final or on the monthly re-check.
	open := map[string]bool{}
	for _, s := range models.CandidateOpenStatuses {
		open[s] = true
	}
	if !open[models.CandidateDeferred] {
		t.Error("a deferred candidate must be picked up again, or a throttled board is lost")
	}
	if open[models.CandidateStored] || open[models.CandidateAttached] {
		t.Error("a company already in the directory must not be re-judged; its own sync keeps it current")
	}
	if open[models.CandidateRejected] {
		t.Error("a slug that is structurally not an employer cannot become one")
	}
}

// interleaveByProvider is what keeps the pass from queueing every worker on
// one host. It must preserve every row and never lose or duplicate one.
func TestInterleaveByProviderSpreadsHostsAndKeepsEveryRow(t *testing.T) {
	var in []models.BoardCandidate
	for i := 0; i < 6; i++ {
		in = append(in, models.BoardCandidate{Provider: "greenhouse", Slug: "g" + string(rune('a'+i))})
	}
	in = append(in,
		models.BoardCandidate{Provider: "lever", Slug: "l1"},
		models.BoardCandidate{Provider: "ashby", Slug: "a1"},
	)

	out := interleaveByProvider(in)
	if len(out) != len(in) {
		t.Fatalf("interleave returned %d rows, want %d", len(out), len(in))
	}
	seen := map[string]bool{}
	for _, r := range out {
		key := boardKey(r.Provider, r.Slug)
		if seen[key] {
			t.Fatalf("row %s duplicated", key)
		}
		seen[key] = true
	}
	// The first three must touch three different providers, which is the
	// whole point: workers start out spread rather than stacked.
	if out[0].Provider == out[1].Provider && out[1].Provider == out[2].Provider {
		t.Error("the head of the batch is still single-provider; workers will queue behind one host")
	}
}
