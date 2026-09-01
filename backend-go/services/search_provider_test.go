package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// The one feature discovery cannot work without is restricting a search to a
// domain list. A provider added without it would return blog posts instead of
// boards, and the fallback would quietly poison the directory rather than
// pause it — so every provider must declare a key, a budget and a search.
func TestEverySearchProviderIsUsable(t *testing.T) {
	if len(searchProviders) == 0 {
		t.Fatal("no search providers configured")
	}

	seen := map[string]bool{}
	for _, p := range searchProviders {
		if p.name == "" || p.envKey == "" || p.search == nil {
			t.Errorf("provider %+v is missing a name, key or search function", p)
		}
		if p.defaultBudget <= 0 {
			t.Errorf("provider %q has no default budget — its free tier would be overrun", p.name)
		}
		if seen[p.name] {
			t.Errorf("duplicate provider name %q — usage counters would collide", p.name)
		}
		seen[p.name] = true
	}
}

func TestProviderBudgetReadsEnvOverride(t *testing.T) {
	p := searchProvider{budgetEnv: "TEST_SEARCH_BUDGET", defaultBudget: 800}

	if got := providerBudget(p); got != 800 {
		t.Errorf("with no override, want the default 800, got %d", got)
	}

	t.Setenv("TEST_SEARCH_BUDGET", "50")
	if got := providerBudget(p); got != 50 {
		t.Errorf("want the override 50, got %d", got)
	}

	// A zero budget is a legitimate way to switch a provider off entirely.
	t.Setenv("TEST_SEARCH_BUDGET", "0")
	if got := providerBudget(p); got != 0 {
		t.Errorf("want 0 to disable the provider, got %d", got)
	}

	// Garbage falls back to the default rather than disabling search.
	t.Setenv("TEST_SEARCH_BUDGET", "not-a-number")
	if got := providerBudget(p); got != 800 {
		t.Errorf("want the default on an unparseable value, got %d", got)
	}
}

// Tavily authenticates with an Authorization header. The key must not also
// be copied into the request body, where it would reach one more log and one
// more error message than it needs to.
func TestTavilyBodyCarriesNoCredential(t *testing.T) {
	var captured map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		if got := r.Header.Get("Authorization"); got != "Bearer secret-key" {
			t.Errorf("expected the key in the Authorization header, got %q", got)
		}
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer srv.Close()

	raw, err := postSearchJSON(srv.URL, map[string]interface{}{
		"query":       "backend engineer jobs in Pune, India",
		"max_results": 10,
	}, map[string]string{"Authorization": "Bearer secret-key"})
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if string(raw) == "" {
		t.Fatal("no response body")
	}

	for key, value := range captured {
		if s, ok := value.(string); ok && strings.Contains(s, "secret-key") {
			t.Errorf("request body field %q carries the API key", key)
		}
	}
	if _, present := captured["api_key"]; present {
		t.Error("request body still carries an api_key field")
	}
}

// With no keys configured at all, a search must fail loudly rather than
// return an empty result that reads like "no boards exist".
func TestWebSearchFailsWithNoProviderConfigured(t *testing.T) {
	for _, p := range searchProviders {
		if os.Getenv(p.envKey) != "" {
			t.Skipf("%s is configured in this environment", p.envKey)
		}
		t.Setenv(p.envKey, "")
	}

	if _, err := WebSearch("backend engineer jobs in Pune, India", boardSearchDomains, 10); err == nil {
		t.Error("expected an error when no search provider has a key")
	}
}

// Every host the search is restricted to must be one the slug reader
// recognises. A domain in one list and not the other is a search that spends
// budget on results nothing can read.
func TestSearchDomainsAreAllReadable(t *testing.T) {
	samples := map[string]string{
		"boards.greenhouse.io":        "https://boards.greenhouse.io/acme",
		"job-boards.greenhouse.io":    "https://job-boards.greenhouse.io/acme",
		"jobs.lever.co":               "https://jobs.lever.co/acme",
		"jobs.ashbyhq.com":            "https://jobs.ashbyhq.com/acme",
		"apply.workable.com":          "https://apply.workable.com/acme",
		"careers.smartrecruiters.com": "https://careers.smartrecruiters.com/acme",
		"keka.com":                    "https://acme.keka.com/careers",
		"darwinbox.in":                "https://acme.darwinbox.in/ms/candidate/careers",
		"darwinbox.com":               "https://acme.darwinbox.com/ms/candidate/careers",
	}

	for _, domain := range boardSearchDomains {
		sample, ok := samples[domain]
		if !ok {
			// Workday's slug needs a live probe for its job-site id, so it
			// has no offline sample; it is covered by DetectATS instead.
			if domain == "myworkdayjobs.com" {
				continue
			}
			t.Errorf("no sample URL for search domain %q — is it readable at all?", domain)
			continue
		}
		if provider, slug := scanForATS(sample); provider == "" || slug == "" {
			t.Errorf("search includes %q but scanForATS cannot read %q", domain, sample)
		}
	}
}
