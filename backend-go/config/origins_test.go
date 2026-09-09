package config

import (
	"slices"
	"testing"
)

func TestAllowedOriginsParsesFrontendURL(t *testing.T) {
	cases := []struct {
		name string
		env  string
		want []string
	}{
		{"unset falls back to dev", "", []string{defaultOrigin}},
		{"single origin", "https://neurofiq.in", []string{"https://neurofiq.in"}},
		{"trailing slash trimmed", "https://neurofiq.in/", []string{"https://neurofiq.in"}},
		{"comma separated", "https://a.com,https://b.com", []string{"https://a.com", "https://b.com"}},
		{"spaces trimmed", " https://a.com , https://b.com ", []string{"https://a.com", "https://b.com"}},
		{"empty entries dropped", "https://a.com,,", []string{"https://a.com"}},
		{"all entries empty falls back", ",,", []string{defaultOrigin}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("FRONTEND_URL", tc.env)
			if got := AllowedOrigins(); !slices.Equal(got, tc.want) {
				t.Errorf("AllowedOrigins() = %v, want %v", got, tc.want)
			}
		})
	}
}

// The WebSocket upgrader and the CORS layer must accept exactly the same set.
// They used to reach that answer through two separate copies of the parsing
// above, so this holds the one implementation to answering consistently.
func TestOriginAllowedAgreesWithTheList(t *testing.T) {
	t.Setenv("FRONTEND_URL", "https://neurofiq.in,https://www.neurofiq.in")

	for _, ok := range []string{
		"https://neurofiq.in",
		"https://neurofiq.in/", // a browser may or may not send the slash
		"https://www.neurofiq.in",
	} {
		if !OriginAllowed(ok) {
			t.Errorf("OriginAllowed(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{
		"https://neurofiq.in.evil.com",
		"http://neurofiq.in", // scheme is part of an origin
		"https://evil.com",
		"",
	} {
		if OriginAllowed(bad) {
			t.Errorf("OriginAllowed(%q) = true, want false", bad)
		}
	}
}
