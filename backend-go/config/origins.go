package config

import (
	"os"
	"strings"
)

// defaultOrigin is where the frontend runs in development.
const defaultOrigin = "http://localhost:5173"

// AllowedOrigins reads FRONTEND_URL into the list of origins this deployment
// serves — comma-separated, trailing slashes trimmed, empty entries dropped.
//
// It lives here because two packages need the same answer: main sets CORS from
// it, and the WebSocket upgrader in controllers decides whether to accept a
// connection by it. Each had its own copy of this parsing, which is a rule that
// has to be kept identical by hand in two places to stay correct — and the two
// places are the browser-facing gate and the socket-facing one, so they
// disagreeing means one of them is wrong about who may connect.
func AllowedOrigins() []string {
	raw := os.Getenv("FRONTEND_URL")
	if raw == "" {
		return []string{defaultOrigin}
	}
	var origins []string
	for _, o := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(strings.TrimRight(o, "/")); trimmed != "" {
			origins = append(origins, trimmed)
		}
	}
	if len(origins) == 0 {
		return []string{defaultOrigin}
	}
	return origins
}

// OriginAllowed reports whether one Origin header is in that list, comparing
// the same way AllowedOrigins normalises: trailing slash ignored.
func OriginAllowed(origin string) bool {
	origin = strings.TrimRight(origin, "/")
	for _, allowed := range AllowedOrigins() {
		if allowed == origin {
			return true
		}
	}
	return false
}
