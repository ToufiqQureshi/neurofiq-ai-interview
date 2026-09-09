package services

import (
	"net"
	"strings"
	"testing"
)

func TestValidRepoFullNameAccepts(t *testing.T) {
	for _, name := range []string{"torvalds/linux", "Toufiq-Qureshi/neurofiq_ai.v2", "a/b"} {
		if !ValidRepoFullName(name) {
			t.Errorf("expected %q to be valid", name)
		}
	}
}

func TestValidRepoFullNameRejects(t *testing.T) {
	// Each of these would otherwise be interpolated straight into a GitHub
	// API URL.
	bad := []string{
		"", "owner", "owner/name/extra", "../../etc/passwd",
		"owner/../secrets", "owner /name", "owner/na?me", "owner/na#me",
		"https://evil.example.com/x", "owner/name%2F..", "-owner/name",
		strings.Repeat("a", 210) + "/b",
	}
	for _, name := range bad {
		if ValidRepoFullName(name) {
			t.Errorf("expected %q to be rejected", name)
		}
	}
}

func TestAllowedPublicURLBlocksInternalTargets(t *testing.T) {
	blocked := []string{
		"http://localhost:8080/admin",
		"http://127.0.0.1/",
		"http://169.254.169.254/latest/meta-data/",
		"http://10.0.0.5/internal",
		"http://192.168.1.1/",
		"http://[::1]:9000/",
		"file:///etc/passwd",
		"gopher://example.com/",
		"http://db.internal/",
	}
	for _, raw := range blocked {
		if err := AllowedPublicURL(raw); err == nil {
			t.Errorf("expected %q to be blocked", raw)
		}
	}
}

func TestAllowedPublicURLAllowsRealSites(t *testing.T) {
	for _, raw := range []string{
		"https://boards-api.greenhouse.io/v1/boards/acme/jobs",
		"https://acme.com/careers",
	} {
		if err := AllowedPublicURL(raw); err != nil {
			t.Errorf("expected %q to be allowed, got %v", raw, err)
		}
	}
}

// The dialer guard is what actually stops a public hostname that resolves to
// a private address, so the IP predicate behind it is worth pinning down.
func TestIsBlockedIP(t *testing.T) {
	blocked := []string{"127.0.0.1", "10.1.2.3", "172.16.0.1", "192.168.0.1",
		"169.254.169.254", "::1", "fd00::1", "0.0.0.0", "100.64.0.1"}
	for _, raw := range blocked {
		if !isBlockedIP(net.ParseIP(raw)) {
			t.Errorf("expected %s to be blocked", raw)
		}
	}
	for _, raw := range []string{"8.8.8.8", "1.1.1.1", "2606:4700::1111"} {
		if isBlockedIP(net.ParseIP(raw)) {
			t.Errorf("expected %s to be allowed", raw)
		}
	}
}

func TestValidATSSlug(t *testing.T) {
	for _, slug := range []string{"acme", "acme-corp", "tenant:wd3:External", "acme_2"} {
		if !validATSSlug(slug) {
			t.Errorf("expected %q to be a valid slug", slug)
		}
	}
	// A slug is interpolated into "https://<slug>.keka.com/..." — these would
	// send the request to a host we never intended.
	for _, slug := range []string{"", "evil.com/x?", "a/../b", "acme#", "acme@evil.com", strings.Repeat("a", 120)} {
		if validATSSlug(slug) {
			t.Errorf("expected %q to be rejected", slug)
		}
	}
}

func TestReadCappedRejectsOversizedBodies(t *testing.T) {
	if _, err := ReadCapped(strings.NewReader("0123456789"), 5); err == nil {
		t.Error("expected an oversized body to be rejected")
	}
	got, err := ReadCapped(strings.NewReader("hello"), 5)
	if err != nil || string(got) != "hello" {
		t.Errorf("expected an exact-size body to pass, got %q / %v", got, err)
	}
}
