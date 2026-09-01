package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"
)

// Web search, behind one interface with a metered fallback.
//
// Discovery needs exactly one thing from a search engine that a scraper
// cannot give it: an index of pages we have never seen, filtered to the job
// board hosts. Every provider here is judged on that one feature — restrict
// results to a set of domains — because without it a search for "backend
// engineer jobs in Pune" answers with blog posts and listicles instead of
// boards, and the whole approach collapses back into guessing.
//
// Providers are tried in order, and a provider is skipped when its key is
// missing or its monthly free allowance is spent. Usage is counted in the
// same scrape_usages table as Firecrawl, so one place answers "what have we
// spent this month".

// searchResult is the only shape discovery needs: what the page is called
// and where it lives.
type searchResult struct {
	Title string
	URL   string
}

// searchProvider is one search backend.
type searchProvider struct {
	name string
	// envKey is the variable holding this provider's API key. A provider
	// with no key configured is skipped, not failed.
	envKey string
	// budgetEnv overrides the monthly call ceiling.
	budgetEnv     string
	defaultBudget int
	// search performs one query. includeDomains is never optional in
	// practice — a provider that cannot honour it is not usable here.
	search func(apiKey, query string, includeDomains []string, numResults int) ([]searchResult, error)
}

// searchProviders in preference order. Exa first because its index is the
// one this pipeline was built against; Tavily second because its
// include_domains filter behaves the same way, so a fallback does not change
// what discovery finds — only who serves it.
//
// Adding a third means writing one function, not a new abstraction. Resist
// adding providers whose API cannot restrict results to a domain list: the
// results would be unusable and the fallback would quietly poison the
// directory rather than pause it.
var searchProviders = []searchProvider{
	{
		name:          "exa",
		envKey:        "EXA_API_KEY",
		budgetEnv:     "EXA_MONTHLY_BUDGET",
		defaultBudget: 800, // free tier is 1000/month; stop short of the wall
		search:        exaSearchCall,
	},
	{
		name:          "tavily",
		envKey:        "TAVILY_API_KEY",
		budgetEnv:     "TAVILY_MONTHLY_BUDGET",
		defaultBudget: 800,
		search:        tavilySearchCall,
	},
}

// searchClient has a short ceiling: a search is one request, not an LLM round
// trip, and it must never hold a cron tick for minutes.
var searchClient = &http.Client{Timeout: 30 * time.Second}

func providerBudget(p searchProvider) int {
	if v := os.Getenv(p.budgetEnv); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
	}
	return p.defaultBudget
}

// SearchBudgetRemaining reports how many searches are left this month across
// all configured providers, for logging and for the discovery loop's own
// decision to stop early.
func SearchBudgetRemaining() int {
	remaining := 0
	for _, p := range searchProviders {
		if os.Getenv(p.envKey) == "" {
			continue
		}
		if left := providerBudget(p) - scrapeUsageThisMonth(p.name); left > 0 {
			remaining += left
		}
	}
	return remaining
}

// WebSearch runs one query against the first provider that has a key and
// budget left.
//
// A provider that errors is not retried by the next one. A failed search is
// almost always the query or the network, and falling through would spend a
// second provider's allowance to receive the same answer. The fallback exists
// for an exhausted allowance, which is a state we can see without paying to
// discover it.
func WebSearch(query string, includeDomains []string, numResults int) ([]searchResult, error) {
	var skipped []string

	for _, p := range searchProviders {
		apiKey := os.Getenv(p.envKey)
		if apiKey == "" {
			skipped = append(skipped, p.name+" (no key)")
			continue
		}
		used, budget := scrapeUsageThisMonth(p.name), providerBudget(p)
		if used >= budget {
			skipped = append(skipped, fmt.Sprintf("%s (%d/%d used)", p.name, used, budget))
			continue
		}

		results, err := p.search(apiKey, query, includeDomains, numResults)
		// Counted whether or not the call found anything: the provider served
		// the request either way, and an uncounted call is how a budget
		// silently overruns.
		recordScrapeUsage(p.name)
		if err != nil {
			return nil, fmt.Errorf("%s search failed: %w", p.name, err)
		}
		if len(skipped) > 0 {
			log.Printf("search: served by %s (skipped %v)", p.name, skipped)
		}
		return results, nil
	}

	return nil, fmt.Errorf("no search provider available (%v)", skipped)
}

// ---- Exa ----

type exaSearchResponse struct {
	Results []struct {
		Title string `json:"title"`
		URL   string `json:"url"`
	} `json:"results"`
}

func exaSearchCall(apiKey, query string, includeDomains []string, numResults int) ([]searchResult, error) {
	body := map[string]interface{}{
		"query":      query,
		"numResults": numResults,
		"type":       "auto",
	}
	if len(includeDomains) > 0 {
		body["includeDomains"] = includeDomains
	}

	raw, err := postSearchJSON("https://api.exa.ai/search", body, map[string]string{"x-api-key": apiKey})
	if err != nil {
		return nil, err
	}

	var parsed exaSearchResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse exa response: %w", err)
	}

	out := make([]searchResult, 0, len(parsed.Results))
	for _, r := range parsed.Results {
		out = append(out, searchResult{Title: r.Title, URL: r.URL})
	}
	return out, nil
}

// exaCompanySearch finds a company's own homepage. Exa's company category
// returns company sites rather than articles about them, which is the whole
// reason this lookup is worth a call; Tavily has no equivalent, so this one
// is Exa-only and simply returns nothing when Exa is unavailable.
func exaCompanySearch(query string, numResults int) ([]searchResult, error) {
	apiKey := os.Getenv("EXA_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("EXA_API_KEY not set")
	}
	used, budget := scrapeUsageThisMonth("exa"), providerBudget(searchProviders[0])
	if used >= budget {
		return nil, fmt.Errorf("exa monthly budget reached (%d/%d)", used, budget)
	}

	body := map[string]interface{}{
		"query":      query,
		"numResults": numResults,
		"type":       "auto",
		"category":   "company",
	}
	raw, err := postSearchJSON("https://api.exa.ai/search", body, map[string]string{"x-api-key": apiKey})
	recordScrapeUsage("exa")
	if err != nil {
		return nil, err
	}

	var parsed exaSearchResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse exa response: %w", err)
	}

	out := make([]searchResult, 0, len(parsed.Results))
	for _, r := range parsed.Results {
		out = append(out, searchResult{Title: r.Title, URL: r.URL})
	}
	return out, nil
}

// ---- Tavily ----

type tavilySearchResponse struct {
	Results []struct {
		Title string `json:"title"`
		URL   string `json:"url"`
	} `json:"results"`
}

func tavilySearchCall(apiKey, query string, includeDomains []string, numResults int) ([]searchResult, error) {
	// The key goes in the Authorization header only. Tavily's body is for
	// search parameters, and a credential duplicated into a JSON body is a
	// credential in one more log and one more error message than it needs
	// to be.
	body := map[string]interface{}{
		"query":       query,
		"max_results": numResults,
	}
	if len(includeDomains) > 0 {
		body["include_domains"] = includeDomains
	}

	raw, err := postSearchJSON("https://api.tavily.com/search", body, map[string]string{
		"Authorization": "Bearer " + apiKey,
	})
	if err != nil {
		return nil, err
	}

	var parsed tavilySearchResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse tavily response: %w", err)
	}

	out := make([]searchResult, 0, len(parsed.Results))
	for _, r := range parsed.Results {
		out = append(out, searchResult{Title: r.Title, URL: r.URL})
	}
	return out, nil
}

// postSearchJSON is the shared request half of every provider: same method,
// same encoding, same size cap, same error text.
func postSearchJSON(endpoint string, body map[string]interface{}, headers map[string]string) ([]byte, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := searchClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := ReadCapped(resp.Body, 4<<20)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %.200s", resp.StatusCode, string(raw))
	}
	return raw, nil
}
