package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/config"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Firecrawl's free tier allows 1,000 pages/month. We stop well short of it
// and fall back to Jina, so we never hit a hard failure mid-month and never
// accidentally spill into paid usage. Override with FIRECRAWL_MONTHLY_BUDGET.
const defaultFirecrawlBudget = 800

func firecrawlBudget() int {
	if v := os.Getenv("FIRECRAWL_MONTHLY_BUDGET"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
	}
	return defaultFirecrawlBudget
}

func currentMonth() string { return time.Now().UTC().Format("2006-01") }

// scrapeUsageThisMonth returns how many calls we've made to a provider in
// the current month.
func scrapeUsageThisMonth(provider string) int {
	var row models.ScrapeUsage
	err := config.DB.Where("month = ? AND provider = ?", currentMonth(), provider).First(&row).Error
	if err != nil {
		return 0 // no row yet (or a read error) — treat as unused
	}
	return row.Count
}

// recordScrapeUsage increments the monthly counter for a provider.
func recordScrapeUsage(provider string) {
	month := currentMonth()
	err := config.DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "month"}, {Name: "provider"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"count":      gorm.Expr("scrape_usages.count + 1"),
			"updated_at": time.Now(),
		}),
	}).Create(&models.ScrapeUsage{
		Month: month, Provider: provider, Count: 1, UpdatedAt: time.Now(),
	}).Error
	if err != nil {
		log.Printf("scrape usage: failed to record %s: %v", provider, err)
	}
}

// FetchRenderedPage returns the text of a page that needs JavaScript to
// render, using a hosted service so we never run a headless browser (and
// pay for the RAM) ourselves.
//
// Jina goes first because it is free, keyless and unmetered. Firecrawl reads
// the page better, but it is metered, and paying a credit to learn something
// the free provider would also have told us is how a month's budget
// disappears without a single extra job in the directory.
//
// That ordering has a second, larger effect: when Firecrawl ran first, an
// unset key or an exhausted budget took this whole path down with it, and
// every company without a supported ATS dropped to zero roles. With Jina in
// front, the free path is the one that carries the directory and Firecrawl
// is an upgrade, not a dependency.
//
// Callers should try a plain HTTP fetch first; this is the escalation path
// for pages that come back empty because they're client-rendered.
func FetchRenderedPage(url string) (string, string, error) {
	text, err := fetchViaJina(url)
	if err == nil {
		recordScrapeUsage("jina")
		return text, "jina", nil
	}
	jinaErr := err
	log.Printf("jina failed for %s (%v) — trying firecrawl", url, err)

	key := os.Getenv("FIRECRAWL_API_KEY")
	if key == "" {
		return "", "", jinaErr
	}
	if used, budget := scrapeUsageThisMonth("firecrawl"), firecrawlBudget(); used >= budget {
		log.Printf("firecrawl monthly budget reached (%d/%d) — giving up on %s", used, budget, url)
		return "", "", jinaErr
	}

	text, err = fetchViaFirecrawl(url, key)
	if err != nil {
		return "", "", jinaErr
	}
	recordScrapeUsage("firecrawl")
	return text, "firecrawl", nil
}

type firecrawlResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Markdown string `json:"markdown"`
		HTML     string `json:"html"`
		RawHTML  string `json:"rawHtml"`
	} `json:"data"`
	Error string `json:"error"`
}

func fetchViaFirecrawl(pageURL, apiKey string) (string, error) {
	// rawHtml is what we actually want: ATS board links live in href/src
	// attributes, which markdown conversion can strip.
	payload, _ := json.Marshal(map[string]interface{}{
		"url":     pageURL,
		"formats": []string{"rawHtml", "markdown"},
	})

	req, err := http.NewRequest("POST", "https://api.firecrawl.dev/v2/scrape", bytes.NewBuffer(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 90 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 3_000_000))
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("firecrawl status %d: %.200s", resp.StatusCode, string(body))
	}

	var parsed firecrawlResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", err
	}
	if !parsed.Success {
		return "", fmt.Errorf("firecrawl error: %s", parsed.Error)
	}

	// Prefer raw HTML, fall back through the other formats.
	for _, s := range []string{parsed.Data.RawHTML, parsed.Data.HTML, parsed.Data.Markdown} {
		if s != "" {
			return s, nil
		}
	}
	return "", fmt.Errorf("firecrawl returned no content")
}

// fetchViaJina uses Jina Reader (r.jina.ai), which renders the page on
// their infrastructure and returns readable text. Free and keyless; an
// optional JINA_API_KEY raises the rate limit.
func fetchViaJina(pageURL string) (string, error) {
	req, err := http.NewRequest("GET", "https://r.jina.ai/"+pageURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "NeuroFIQ-JobMap/1.0")
	// Ask Jina to keep links, otherwise ATS board URLs get flattened away.
	req.Header.Set("X-Retain-Images", "none")
	req.Header.Set("X-With-Links-Summary", "true")
	if key := os.Getenv("JINA_API_KEY"); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}

	client := &http.Client{Timeout: 90 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 3_000_000))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("jina status %d", resp.StatusCode)
	}
	return string(body), nil
}

// ExtractedJob is a job listing pulled straight off a company's own careers
// page, for companies that don't use a supported ATS.
type ExtractedJob struct {
	Title      string `json:"title"`
	Department string `json:"department"`
	Location   string `json:"location"`
	URL        string `json:"url"`
}

type firecrawlExtractResponse struct {
	Success bool `json:"success"`
	Data    struct {
		// The model picks its own top-level key, so accept the common
		// spellings rather than forcing a schema (which came back empty).
		JSON struct {
			Jobs        []ExtractedJob `json:"jobs"`
			JobOpenings []ExtractedJob `json:"jobOpenings"`
			Openings    []ExtractedJob `json:"openings"`
			Positions   []ExtractedJob `json:"positions"`
		} `json:"json"`
	} `json:"data"`
	Error string `json:"error"`
}

// ExtractJobsFromCareersPage reads a company's own careers page and pulls out
// whatever roles are listed. This is the fallback for the majority of
// companies that run a custom portal instead of a supported ATS — without it
// they'd show zero jobs even while actively hiring.
//
// Costs one Firecrawl credit per call, so callers should rate-limit how often
// they re-run it per company (see atsRecheckInterval).
func ExtractJobsFromCareersPage(pageURL string) ([]ExtractedJob, error) {
	apiKey := os.Getenv("FIRECRAWL_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("FIRECRAWL_API_KEY not set")
	}
	if used, budget := scrapeUsageThisMonth("firecrawl"), firecrawlBudget(); used >= budget {
		return nil, fmt.Errorf("firecrawl monthly budget reached (%d/%d)", used, budget)
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"url": pageURL,
		"formats": []map[string]interface{}{{
			"type": "json",
			"prompt": "Extract every open job opening listed on this page, with its " +
				"title, department or team, location, and the direct link to apply " +
				"if one is shown. Only include roles actually listed on the page — " +
				"never invent one. If no roles are listed, return an empty list.",
		}},
	})

	req, err := http.NewRequest("POST", "https://api.firecrawl.dev/v2/scrape", bytes.NewBuffer(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 3 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 3_000_000))
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("firecrawl status %d: %.200s", resp.StatusCode, string(body))
	}

	var parsed firecrawlExtractResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	if !parsed.Success {
		return nil, fmt.Errorf("firecrawl error: %s", parsed.Error)
	}
	// Counted only once the call actually produced an extraction. Counting
	// before this check billed us for every failure too, so the month's
	// budget could run out having returned no jobs at all — and the budget
	// guard then blocked the calls that would have worked.
	recordScrapeUsage("firecrawl")

	for _, list := range [][]ExtractedJob{
		parsed.Data.JSON.Jobs,
		parsed.Data.JSON.JobOpenings,
		parsed.Data.JSON.Openings,
		parsed.Data.JSON.Positions,
	} {
		if len(list) > 0 {
			return list, nil
		}
	}
	return nil, nil
}

// ScrapeUsageSummary reports this month's usage per provider, for logging
// and for a future admin view.
func ScrapeUsageSummary() map[string]int {
	var rows []models.ScrapeUsage
	config.DB.Where("month = ?", currentMonth()).Find(&rows)
	out := map[string]int{}
	for _, r := range rows {
		out[r.Provider] = r.Count
	}
	return out
}
