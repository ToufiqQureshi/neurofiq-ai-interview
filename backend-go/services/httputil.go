package services

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

func internalSecret() string {
	return os.Getenv("INTERNAL_SECRET")
}

func workerURL() string {
	if u := os.Getenv("PYTHON_WORKER_URL"); u != "" {
		return strings.TrimRight(u, "/")
	}
	return "http://localhost:8001"
}

func workerHTTPClient() *http.Client {
	return &http.Client{Timeout: 90 * time.Second}
}

func newWorkerRequest(method, path string, body []byte) (*http.Request, error) {
	req, err := http.NewRequest(method, workerURL()+path, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Secret", os.Getenv("INTERNAL_SECRET"))
	return req, nil
}

// ValidRepoFullName accepts only "owner/name" with no extra path segments.
func ValidRepoFullName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || strings.Contains(name, "..") || strings.ContainsAny(name, " \\?#") {
		return false
	}
	parts := strings.Split(name, "/")
	if len(parts) != 2 {
		return false
	}
	for _, p := range parts {
		if p == "" || p == "." || p == ".." {
			return false
		}
	}
	return true
}

func isPrivateOrLocalHost(host string) bool {
	host = strings.TrimSuffix(host, ".")
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		h = host
	}
	h = strings.Trim(h, "[]")
	if h == "localhost" || strings.HasSuffix(h, ".localhost") || h == "0.0.0.0" {
		return true
	}
	ip := net.ParseIP(h)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}

func allowedPublicURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid url")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("unsupported url scheme")
	}
	if parsed.Host == "" || isPrivateOrLocalHost(parsed.Host) {
		return fmt.Errorf("blocked host")
	}
	return nil
}
