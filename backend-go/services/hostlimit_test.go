package services

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
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
