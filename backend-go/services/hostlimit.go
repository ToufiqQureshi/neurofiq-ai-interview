package services

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Per-host pacing, for every outbound request this service makes.
//
// The concurrency limits elsewhere in this package bound how many requests are
// in flight, which is not the same thing as how hard we lean on any one
// server. The harvest reads 13,501 boards with eight workers, and 5,735 of
// those slugs are Greenhouse: in practice all eight workers sit on one host
// for half an hour. From Greenhouse's side that is a sustained burst from a
// single client, and the polite pause between calls does nothing about it,
// because it paces a worker rather than a host.
//
// So pacing moves to where it belongs. Every request through SafeExternalGet
// takes a slot from its host's gate, and a host with a thousand slugs queued
// is served at the same rate as a host with one. Eight workers against ten
// providers is then genuinely eight-way parallel; eight workers against one
// provider is a queue.
//
// The second half is what happens when a host says no anyway. A 429 or a 503
// is the server asking for room, and the only useful response is to give it —
// globally for that host, not just on the goroutine that saw it, because the
// other seven are about to make the same request. Every 429 doubles that
// host's interval and opens a cooling-off window that all workers observe.

// hostBaseInterval is the steady-state spacing between two requests to the
// same host: four a second.
//
// Chosen against the slowest thing it gates. Greenhouse is the widest host in
// the harvest at 5,735 slugs, which at this rate is a 24-minute pass — well
// inside the run's budget, and a rate no public API would consider abuse.
const hostBaseInterval = 250 * time.Millisecond

// hostMaxInterval caps the exponential backoff. Beyond this a host is better
// described as unavailable than as slow, and the callers give up and retry on
// a later tick rather than holding a worker.
const hostMaxInterval = 30 * time.Second

// hostMaxWait is the longest a caller will queue for a slot before being told
// the host is busy.
//
// This is the difference between a slow harvest and a stuck one. Without it,
// a host under a 30-second backoff with 400 slugs queued behind it parks eight
// workers for three hours. ErrHostBusy hands that decision back to the caller,
// which records the candidate as unread and moves on — the queue will return
// to it, so nothing is lost by refusing to wait.
const hostMaxWait = 45 * time.Second

// hostRecoveryFactor is how fast a host's interval decays back to normal after
// a clean response. Halving means a host that hit one 429 is back to full rate
// after a single success, while one that hit ten is let back gradually.
const hostRecoveryFactor = 2

var (
	// ErrHostThrottled is returned when a host has asked us to back off and
	// the wait would exceed hostMaxWait.
	ErrHostThrottled = errors.New("host is asking us to slow down")
	// ErrHostBusy is returned when the queue for a host is longer than
	// hostMaxWait, without the host having complained.
	ErrHostBusy = errors.New("too many requests already queued for this host")
)

// hostGate is one host's pacing state.
type hostGate struct {
	mu sync.Mutex
	// nextSlot is the earliest moment the next request to this host may
	// start. Reserving it under the lock is what makes the spacing hold
	// across goroutines rather than per goroutine.
	nextSlot time.Time
	// interval is the current spacing, grown by throttling and decayed by
	// success.
	interval time.Duration
	// strikes counts consecutive throttle responses, for the log line. A
	// host that is throttling us persistently is worth seeing.
	strikes int
}

var (
	hostGatesMu sync.Mutex
	hostGates   = map[string]*hostGate{}
)

func gateFor(host string) *hostGate {
	hostGatesMu.Lock()
	defer hostGatesMu.Unlock()
	g := hostGates[host]
	if g == nil {
		g = &hostGate{interval: hostBaseInterval}
		hostGates[host] = g
	}
	return g
}

// reserve claims this host's next slot and reports how long the caller must
// wait for it, or false if that wait is longer than hostMaxWait.
//
// The slot is reserved before the wait rather than after, so N goroutines
// arriving at once are spaced N intervals apart instead of all sleeping the
// same interval and then firing together.
func (g *hostGate) reserve(now time.Time, max time.Duration) (time.Duration, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()

	start := g.nextSlot
	if start.Before(now) {
		start = now
	}
	wait := start.Sub(now)
	if wait > max {
		return 0, false
	}
	g.nextSlot = start.Add(g.interval)
	return wait, true
}

// throttled widens this host's interval after it asked us to slow down, and
// pushes its next slot past the cooling-off window.
//
// retryAfter is the server's own answer when it gave one; a Retry-After header
// is the host stating its terms, and guessing over the top of it is how a
// client earns a longer ban.
func (g *hostGate) throttled(host string, retryAfter time.Duration) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.strikes++
	g.interval *= 2
	if g.interval > hostMaxInterval {
		g.interval = hostMaxInterval
	}

	pause := retryAfter
	if pause < g.interval {
		pause = g.interval
	}
	if until := time.Now().Add(pause); until.After(g.nextSlot) {
		g.nextSlot = until
	}
	log.Printf("host pacing: %s asked us to slow down (strike %d) — interval now %s, pausing %s",
		host, g.strikes, g.interval, pause.Round(time.Millisecond))
}

// succeeded decays the interval back towards normal after a clean response.
func (g *hostGate) succeeded() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.interval <= hostBaseInterval {
		g.strikes = 0
		return
	}
	g.interval /= hostRecoveryFactor
	if g.interval < hostBaseInterval {
		g.interval = hostBaseInterval
	}
	g.strikes = 0
}

// awaitHostSlot blocks until this host may be contacted again.
func awaitHostSlot(ctx context.Context, host string) error {
	if host == "" {
		return nil
	}
	g := gateFor(host)

	max := hostMaxWait
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining < max {
			max = remaining
		}
	}
	if max <= 0 {
		return context.DeadlineExceeded
	}

	wait, ok := g.reserve(time.Now(), max)
	if !ok {
		g.mu.Lock()
		throttling := g.interval > hostBaseInterval
		g.mu.Unlock()
		if throttling {
			return fmt.Errorf("%s: %w", host, ErrHostThrottled)
		}
		return fmt.Errorf("%s: %w", host, ErrHostBusy)
	}
	if wait <= 0 {
		return nil
	}

	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// noteHostResponse feeds a response back into its host's gate.
func noteHostResponse(host string, resp *http.Response) {
	if host == "" || resp == nil {
		return
	}
	g := gateFor(host)
	switch {
	case resp.StatusCode == http.StatusTooManyRequests,
		resp.StatusCode == http.StatusServiceUnavailable:
		g.throttled(host, retryAfterFrom(resp))
	case resp.StatusCode < 500:
		// Any answer the server actually formed is evidence it is coping.
		// A 404 counts: it means we are being served, just not something
		// that exists.
		g.succeeded()
	}
}

// retryAfterFrom reads the Retry-After header in either form the RFC allows.
func retryAfterFrom(resp *http.Response) time.Duration {
	raw := resp.Header.Get("Retry-After")
	if raw == "" {
		return 0
	}
	if secs, err := strconv.Atoi(raw); err == nil {
		if secs < 0 {
			return 0
		}
		return capRetryAfter(time.Duration(secs) * time.Second)
	}
	if when, err := http.ParseTime(raw); err == nil {
		return capRetryAfter(time.Until(when))
	}
	return 0
}

// capRetryAfter refuses to honour an implausible Retry-After. A header asking
// for an hour would otherwise park a worker for an hour; the run is better
// served by giving up on that host for this tick.
func capRetryAfter(d time.Duration) time.Duration {
	if d < 0 {
		return 0
	}
	if d > hostMaxInterval {
		return hostMaxInterval
	}
	return d
}

// HostPacingSnapshot describes one host's current state, for the health log.
type HostPacingSnapshot struct {
	Host     string
	Interval time.Duration
	Strikes  int
}

// ThrottledHosts lists the hosts currently pacing slower than normal.
//
// This is the signal that separates "these boards are dead" from "we are being
// rate-limited and calling it dead", which is the failure this whole file
// exists to prevent. It is logged at the end of every harvest rather than
// left for someone to notice.
func ThrottledHosts() []HostPacingSnapshot {
	hostGatesMu.Lock()
	gates := make(map[string]*hostGate, len(hostGates))
	for h, g := range hostGates {
		gates[h] = g
	}
	hostGatesMu.Unlock()

	var out []HostPacingSnapshot
	for host, g := range gates {
		g.mu.Lock()
		interval, strikes := g.interval, g.strikes
		g.mu.Unlock()
		if interval > hostBaseInterval {
			out = append(out, HostPacingSnapshot{Host: host, Interval: interval, Strikes: strikes})
		}
	}
	return out
}

// HTTPStatusError is a non-2xx answer, carrying the status so callers can tell
// "this board does not exist" from "this board would not talk to us".
//
// Those two were the same value before: every fetcher returned
// fmt.Errorf("greenhouse status %d"), the harvest tested err != nil, and a 429
// was filed as a dead board — the company dropped, no retry, nothing in the
// log to distinguish it from the 90% of candidates that genuinely are dead.
// This codebase already has the rule ("silence and zero must not look alike");
// this type is what lets the callers keep it.
type HTTPStatusError struct {
	Status int
	URL    string
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("http %d from %s", e.Status, e.URL)
}

// Transient reports whether asking again later could plausibly succeed.
func (e *HTTPStatusError) Transient() bool {
	return e.Status == http.StatusTooManyRequests ||
		e.Status == http.StatusRequestTimeout ||
		e.Status >= 500
}

// IsTransientFetchError reports whether a failed fetch is worth retrying.
//
// The harvest and the sync both need this answer and must not disagree about
// it: one treating a timeout as permanent while the other retries is how a
// company ends up present in one path and absent from the other.
func IsTransientFetchError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrHostThrottled) || errors.Is(err, ErrHostBusy) {
		return true
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	var status *HTTPStatusError
	if errors.As(err, &status) {
		return status.Transient()
	}
	// net.Error covers dial timeouts and response-header timeouts, which are
	// the shape a struggling host most often takes before it starts answering
	// 503s at all.
	var netErr interface{ Timeout() bool }
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return false
}

// jitter spreads a fixed delay so that N workers released at once do not all
// return at the same instant. Used by the candidate queue's retry schedule.
func jitter(d time.Duration, fraction float64) time.Duration {
	if d <= 0 {
		return 0
	}
	spread := float64(d) * fraction
	// Deterministic enough for spreading load and free of a rand dependency:
	// the nanosecond clock is the entropy, and collisions do not matter.
	offset := math.Mod(float64(time.Now().UnixNano()), spread*2) - spread
	out := time.Duration(float64(d) + offset)
	if out < 0 {
		return 0
	}
	return out
}
