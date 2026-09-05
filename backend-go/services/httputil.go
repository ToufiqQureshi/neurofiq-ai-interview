package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"syscall"
	"time"
)

// Every outbound request in this package goes through one of the clients
// below. Go's http.DefaultClient has NO timeout: a single unresponsive host
// parks the calling goroutine forever. That has already cost this codebase
// one ~3-hour stall of the whole cron tick (see PROGRESS.md, 2026-08-29), so
// the default client is deliberately never used here.
var (
	// workerClient talks to our own Python worker. Each call contains an LLM
	// round trip, so its ceiling is far higher than a plain page fetch.
	workerClient = &http.Client{Timeout: 90 * time.Second}

	// searchClient (search_provider.go) covers the search side. It is a
	// plain HTTP client with a much shorter ceiling, because a search is one
	// request rather than an LLM round trip.

	// downloadClient pulls repository zipballs: large and slow, but bounded.
	downloadClient = &http.Client{Timeout: 3 * time.Minute}

	// githubClient handles the small GitHub REST calls (branch, commits).
	githubClient = &http.Client{Timeout: 15 * time.Second}
)

// safeDialer refuses to open a connection to a private or loopback address.
//
// The check lives in Control rather than in a URL string test because it runs
// *after* DNS resolution, on the address actually being dialled. That closes
// the two holes a string check leaves open: a public hostname whose A record
// points at 169.254.169.254 (DNS rebinding, the classic cloud-metadata SSRF),
// and a public URL that 302s to one. Both matter here because the URLs we
// fetch come from an LLM's web search, not from us.
var safeDialer = &net.Dialer{
	Timeout:   10 * time.Second,
	KeepAlive: 30 * time.Second,
	Control: func(network, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return fmt.Errorf("unparseable dial address %q", address)
		}
		ip := net.ParseIP(host)
		if ip == nil {
			return fmt.Errorf("unparseable dial ip %q", host)
		}
		if isBlockedIP(ip) {
			return fmt.Errorf("refusing to connect to non-public address %s", ip)
		}
		return nil
	},
}

// externalClient fetches third-party pages: company careers pages, ATS APIs.
// Every one of those URLs originates from web search or from HTML we scraped,
// so this is the client that needs the dialer guard.
var externalClient = &http.Client{
	Timeout: 20 * time.Second,
	Transport: &http.Transport{
		DialContext:           safeDialer.DialContext,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
	},
}

// isBlockedIP reports whether an address belongs to the machine, the private
// network, or one of the ranges cloud providers put their metadata service on.
func isBlockedIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
		return true
	}
	// 100.64.0.0/10 — carrier-grade NAT, routable inside many VPCs.
	if v4 := ip.To4(); v4 != nil && v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
		return true
	}
	return false
}

// AllowedPublicURL rejects a URL before we spend a request on it. The dialer
// above is the real guard; this one exists to fail fast and to give a caller a
// readable reason rather than a dial error.
func AllowedPublicURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("invalid url")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("unsupported url scheme %q", parsed.Scheme)
	}
	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("url has no host")
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") ||
		strings.HasSuffix(host, ".internal") || strings.HasSuffix(host, ".local") {
		return fmt.Errorf("blocked host %q", host)
	}
	if ip := net.ParseIP(host); ip != nil && isBlockedIP(ip) {
		return fmt.Errorf("blocked host %q", host)
	}
	return nil
}

// SafeExternalGet fetches a URL we did not author. Use it for anything whose
// address came from search results, scraped HTML, or an LLM.
func SafeExternalGet(rawURL string) (*http.Response, error) {
	return SafeExternalGetCtx(context.Background(), rawURL)
}

// SafeExternalGetCtx is SafeExternalGet with a caller-supplied deadline.
//
// Two things happen here that did not before. The request waits for its host's
// pacing slot (hostlimit.go), so eight workers on one provider queue instead
// of bursting; and the response is fed back into that host's gate, so a 429
// slows every worker rather than only the one that saw it.
//
// This is the single place every third-party fetch in the service passes
// through — the harvest, the hourly sync, ATS detection, careers-page reads
// and the dead-link prune — which is why the pacing belongs here rather than
// in each caller. A caller added later gets it without knowing it exists.
func SafeExternalGetCtx(ctx context.Context, rawURL string) (*http.Response, error) {
	if err := AllowedPublicURL(rawURL); err != nil {
		return nil, err
	}
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, err
	}
	host := strings.ToLower(parsed.Hostname())

	if err := awaitHostSlot(ctx, host); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "NeuroFIQ-JobMap/1.0 (+https://neurofiq.in)")
	resp, err := externalClient.Do(req)
	if err != nil {
		return nil, err
	}
	noteHostResponse(host, resp)
	return resp, nil
}

// SafeExternalDo is SafeExternalGetCtx for a request the caller built itself.
//
// Two ATS readers have to POST — Darwinbox takes a JSON filter body, Workday
// pages through one — so they cannot go through the GET helper, and before
// this they called externalClient.Do directly. That put the two providers most
// likely to sit behind a bot filter outside the only place that paces requests
// and notices a 429, which is exactly backwards.
func SafeExternalDo(ctx context.Context, req *http.Request) (*http.Response, error) {
	if req.URL == nil {
		return nil, fmt.Errorf("request has no url")
	}
	if err := AllowedPublicURL(req.URL.String()); err != nil {
		return nil, err
	}
	host := strings.ToLower(req.URL.Hostname())
	if err := awaitHostSlot(ctx, host); err != nil {
		return nil, err
	}
	resp, err := externalClient.Do(req.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	noteHostResponse(host, resp)
	return resp, nil
}


// ReadCapped reads at most max bytes and reports an error if the body was
// longer, so an oversized response is rejected rather than silently truncated
// into something that parses but is wrong.
func ReadCapped(r io.Reader, max int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("response exceeds %d byte limit", max)
	}
	return data, nil
}

// ErrWorkerUnavailable wraps every failure to get an answer out of the Python
// worker. Callers classify on this rather than on the error string: a reworded
// message should not silently change an HTTP status code.
var ErrWorkerUnavailable = errors.New("ai worker unavailable")

func internalSecret() string { return os.Getenv("INTERNAL_SECRET") }

func workerURL() string {
	if u := os.Getenv("PYTHON_WORKER_URL"); u != "" {
		return strings.TrimRight(u, "/")
	}
	return "http://localhost:8001"
}

// postToWorker is the single path from Go to the Python worker. Every caller
// used to hand-roll this same marshal / header / status-check sequence, and
// they drifted: one had a timeout, two did not.
func postToWorker(client *http.Client, path string, payload interface{}) ([]byte, error) {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to encode worker payload: %w", err)
	}
	req, err := http.NewRequest("POST", workerURL()+path, bytes.NewReader(jsonData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Secret", internalSecret())

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrWorkerUnavailable, err)
	}
	defer resp.Body.Close()

	body, err := ReadCapped(resp.Body, 8<<20) // 8 MB — far above any real response
	if err != nil {
		return nil, fmt.Errorf("%w: response unreadable: %v", ErrWorkerUnavailable, err)
	}
	if resp.StatusCode != http.StatusOK {
		// The worker's body can carry a provider error or a stack trace. It
		// belongs in our logs, never in a candidate's browser — the handler
		// logs this and answers with a fixed message.
		return nil, fmt.Errorf("%w: status %d: %s", ErrWorkerUnavailable, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

// ValidRepoFullName accepts only "owner/name" — no extra path segments, no
// traversal, nothing that could steer a GitHub API URL somewhere else.
func ValidRepoFullName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 200 || strings.Contains(name, "..") ||
		strings.ContainsAny(name, " \t\\?#%@:") {
		return false
	}
	parts := strings.Split(name, "/")
	if len(parts) != 2 {
		return false
	}
	for _, p := range parts {
		if p == "" || p == "." || p == ".." || strings.HasPrefix(p, "-") {
			return false
		}
		for _, r := range p {
			isOK := r == '-' || r == '_' || r == '.' ||
				(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
			if !isOK {
				return false
			}
		}
	}
	return true
}
