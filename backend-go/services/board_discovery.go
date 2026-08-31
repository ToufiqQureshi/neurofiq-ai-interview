package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm/clause"

	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/config"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/models"
)

// Board-first discovery.
//
// The old approach asked an LLM which companies exist ("AI startups in
// Bangalore"), then went looking for a careers page on each answer. That is
// two guesses stacked: whether the company is real, and whether it hires.
// Most answers failed the second one — they were small shops with no careers
// page — so the tokens bought names the directory could never use.
//
// This runs the search the other way round. Every hiring company puts its
// roles on a job board, and those boards are public pages a search engine has
// already indexed. Searching the board domains directly returns URLs like
// jobs.lever.co/Sprinto, and the slug in that URL is the same one the board's
// public API takes. So one search yields companies that are provably hiring,
// with their open roles one free API call away — no model in the loop, and
// nothing to verify afterwards, because the board *is* the verification.

// boardSearchDomains are the ATS hosts a discovery search is restricted to.
// Every URL returned from one of these is a board, so a hit is a company.
var boardSearchDomains = []string{
	"boards.greenhouse.io",
	"job-boards.greenhouse.io",
	"jobs.lever.co",
	"jobs.ashbyhq.com",
	"apply.workable.com",
	"careers.smartrecruiters.com",
}

// boardSeedQueries rotate the search so the directory keeps widening instead
// of re-reading the same boards. Cities and roles, because that is what a
// board page's text actually contains — a Bangalore posting says
// "Bangalore", it does not say "Series A fintech".
var (
	boardSeedCities = []string{
		"Bangalore", "Bengaluru", "Mumbai", "Delhi", "Gurgaon", "Noida",
		"Pune", "Hyderabad", "Chennai", "Ahmedabad", "Kolkata", "India remote",
	}
	boardSeedRoles = []string{
		"software engineer", "backend engineer", "frontend engineer",
		"data scientist", "product manager", "designer",
		"machine learning engineer", "devops engineer", "sales", "marketing",
	}

	boardSeedQueries = buildBoardSeedQueries()
)

func buildBoardSeedQueries() []string {
	out := make([]string, 0, len(boardSeedCities)*len(boardSeedRoles))
	for _, city := range boardSeedCities {
		for _, role := range boardSeedRoles {
			out = append(out, fmt.Sprintf("%s jobs in %s, India", role, city))
		}
	}
	return out
}

// discoveryIntervalSeconds must match the cron schedule in main.go — the
// rotation cursor is derived from it, so a mismatch would skip or repeat
// queries. Hourly gets through all 120 seeds in ~5 days.
const discoveryIntervalSeconds = 3600

// DiscoveryLeaseName is the cron lease that keeps two instances from running
// the same discovery tick.
const DiscoveryLeaseName = "discovery-rotation"

// boardResultsPerQuery is how many search hits one query asks for. Most
// resolve to a handful of distinct boards once duplicates collapse.
const boardResultsPerQuery = 25

// exaClient has its own ceiling: a search is a single request, not an LLM
// round trip, so it should never hold a cron tick for minutes.
var exaClient = &http.Client{Timeout: 30 * time.Second}

type exaResult struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

type exaSearchResponse struct {
	Results []exaResult `json:"results"`
}

// exaSearch runs one search, optionally restricted to a set of hosts.
//
// Exa rather than a scraper because this is the one step that genuinely needs
// an index of the whole web: we are looking for board pages we have never
// seen, and no amount of fetching pages we already know about will surface
// them.
func exaSearch(query string, includeDomains []string, numResults int, category string) ([]exaResult, error) {
	apiKey := os.Getenv("EXA_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("EXA_API_KEY not set")
	}

	body := map[string]interface{}{
		"query":      query,
		"numResults": numResults,
		"type":       "auto",
	}
	if len(includeDomains) > 0 {
		body["includeDomains"] = includeDomains
	}
	if category != "" {
		body["category"] = category
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", "https://api.exa.ai/search", bytes.NewBuffer(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)

	resp, err := exaClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := ReadCapped(resp.Body, 4<<20)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("exa status %d: %.200s", resp.StatusCode, string(raw))
	}

	var parsed exaSearchResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse exa response: %w", err)
	}
	return parsed.Results, nil
}

// boardHit is one distinct board found by a search.
type boardHit struct {
	Provider string
	Slug     string
	Title    string
	URL      string
}

// nonSlugSegments are path segments that match the board-URL patterns but are
// not a company's slug.
var nonSlugSegments = map[string]bool{
	"embed": true, "jobs": true, "job": true, "search": true,
	"api": true, "static": true, "assets": true,
}

// aggregatorHosts are never a company's own site. A "website" on one of these
// is a page *about* the company, and storing it would point the careers-page
// resolver at a job board's own domain.
var aggregatorHosts = []string{
	"linkedin.com", "crunchbase.com", "tracxn.com", "indeed.com",
	"glassdoor.co", "glassdoor.com", "naukri.com", "wellfound.com",
	"angel.co", "ambitionbox.com", "zaubacorp.com", "wikipedia.org",
	"youtube.com", "facebook.com", "instagram.com", "twitter.com", "x.com",
	"medium.com", "substack.com", "github.io", "notion.site",
	"greenhouse.io", "lever.co", "ashbyhq.com", "workable.com",
	"smartrecruiters.com", "keka.com", "darwinbox.in", "myworkdayjobs.com",
}

func isAggregatorHost(host string) bool {
	host = strings.ToLower(strings.TrimPrefix(host, "www."))
	for _, bad := range aggregatorHosts {
		if host == bad || strings.HasSuffix(host, "."+bad) {
			return true
		}
	}
	return false
}

// sharedBoardRe matches the names venture funds and talent collectives use for
// the boards they run on behalf of *other* companies. One of those boards
// carries dozens of employers' roles, so storing it as a company would file
// every one of those roles under the fund's name.
var sharedBoardRe = regexp.MustCompile(`(?i)\b(vc|ventures?|capital|partners|fund|portfolio|talent|network|community|collective|accelerator|incubator)\b`)

// indiaLocationHints decide whether a board has roles worth listing. The
// directory is for people looking for work in India; a company headquartered
// anywhere is welcome, as long as it is hiring here.
var indiaLocationHints = []string{
	"india", "bengaluru", "bangalore", "mumbai", "delhi", "gurgaon",
	"gurugram", "noida", "hyderabad", "pune", "chennai", "kolkata",
	"ahmedabad", "jaipur", "indore", "chandigarh", "kochi", "coimbatore",
	"trivandrum", "thiruvananthapuram", "bhubaneswar", "nagpur", "surat",
}

func looksIndian(location string) bool {
	lower := strings.ToLower(location)
	for _, hint := range indiaLocationHints {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	return false
}

// boardTitleCleanupRe strips the suffixes boards append to their page titles,
// so "Sprinto - Lever" and "Cartesia - Jobs" both come back as the company.
var boardTitleCleanupRe = regexp.MustCompile(`(?i)\s*[-–—|]\s*(lever|greenhouse|ashby|workable|smartrecruiters|jobs|careers|job board|open positions|hiring)\s*$`)

var titlePrefixRe = regexp.MustCompile(`(?i)^\s*(careers at|jobs at|work at|current openings at|open positions at)\s+`)

// genericBoardTitles are what is left when a board page's title says nothing
// about the company. Left alone, a page titled "Jobs" would enter the
// directory as a company called Jobs.
var genericBoardTitles = map[string]bool{
	"jobs": true, "careers": true, "job board": true, "open positions": true,
	"openings": true, "current openings": true, "hiring": true, "home": true,
	"lever": true, "greenhouse": true, "ashby": true, "workable": true,
	"smartrecruiters": true, "job search": true, "search jobs": true,
}

// companyNameFromBoard turns a board page's title into a company name,
// falling back to the slug when the title is unusable.
func companyNameFromBoard(title, slug string) string {
	name := strings.TrimSpace(title)
	// Titles carry several suffixes at once ("Sprinto - Jobs - Lever").
	for i := 0; i < 3; i++ {
		trimmed := boardTitleCleanupRe.ReplaceAllString(name, "")
		if trimmed == name {
			break
		}
		name = strings.TrimSpace(trimmed)
	}
	name = strings.TrimSpace(titlePrefixRe.ReplaceAllString(name, ""))
	name = whitespaceRe.ReplaceAllString(name, " ")

	if len(name) < 2 || len(name) > 80 || genericBoardTitles[strings.ToLower(name)] {
		// The slug is url-safe, so it reads as a name once separators go.
		name = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(slug, "-", " "), "_", " "))
	}
	return name
}

// boardHitsFor runs one search and returns the distinct boards it found.
func boardHitsFor(query string, numResults int) []boardHit {
	results, err := exaSearch(query, boardSearchDomains, numResults, "")
	if err != nil {
		log.Printf("board discovery: search failed for %q: %v", query, err)
		return nil
	}

	seen := map[string]bool{}
	var hits []boardHit
	for _, r := range results {
		// The same regexes that read a board link out of a careers page read
		// it out of a search result, because both are just the URL.
		provider, slug := scanForATS(r.URL)
		if provider == "" || slug == "" || nonSlugSegments[strings.ToLower(slug)] {
			continue
		}
		key := provider + ":" + strings.ToLower(slug)
		if seen[key] {
			continue
		}
		seen[key] = true
		hits = append(hits, boardHit{
			Provider: provider,
			Slug:     slug,
			Title:    r.Title,
			URL:      boardURL(provider, slug),
		})
	}
	return hits
}

// boardURL is the public, human-facing address of a board — used as the
// company's careers URL, since for these companies it is exactly that.
func boardURL(provider, slug string) string {
	switch provider {
	case "greenhouse":
		return "https://boards.greenhouse.io/" + slug
	case "lever":
		return "https://jobs.lever.co/" + slug
	case "ashby":
		return "https://jobs.ashbyhq.com/" + slug
	case "workable":
		return "https://apply.workable.com/" + slug
	case "smartrecruiters":
		return "https://careers.smartrecruiters.com/" + slug
	case "keka":
		return "https://" + slug + ".keka.com/careers"
	case "darwinbox":
		return "https://" + slug + ".darwinbox.in/ms/candidate/careers"
	}
	return ""
}

// resolveCompanyWebsite finds a company's own homepage.
//
// The board tells us a company is hiring but not where it lives, and the
// companies table is keyed on domain. One search per newly-seen company, and
// never for one we already have.
func resolveCompanyWebsite(name string) string {
	results, err := exaSearch(name+" official company website", nil, 5, "company")
	if err != nil {
		log.Printf("board discovery: website lookup failed for %q: %v", name, err)
		return ""
	}
	for _, r := range results {
		domain := extractDomain(r.URL)
		if domain == "" || isAggregatorHost(domain) {
			continue
		}
		return r.URL
	}
	return ""
}

// DiscoverFromBoards runs one board search and stores the companies behind
// the boards it finds, along with their open roles.
func DiscoverFromBoards(query string, limit int) ([]models.Company, error) {
	hits := boardHitsFor(query, boardResultsPerQuery)
	if len(hits) == 0 {
		return nil, nil
	}

	var saved []models.Company
	for _, hit := range hits {
		if len(saved) >= limit {
			break
		}

		name := companyNameFromBoard(hit.Title, hit.Slug)
		if sharedBoardRe.MatchString(name) || sharedBoardRe.MatchString(hit.Slug) {
			log.Printf("board discovery: skipping %q — looks like a fund or talent-network board", name)
			continue
		}

		// Cheapest disqualifier first: a board we already have.
		var existing int64
		config.DB.Model(&models.Company{}).
			Where("ats_type = ? AND lower(ats_slug) = lower(?)", hit.Provider, hit.Slug).
			Count(&existing)
		if existing > 0 {
			continue
		}

		// Then the board's own roles, which are free to read and settle both
		// remaining questions: is it live, and does it hire in India.
		jobs, err := FetchATSJobs("", hit.Provider, hit.Slug)
		if err != nil || len(jobs) == 0 {
			continue
		}
		area := firstIndianLocation(jobs)
		if area == "" {
			continue // hiring, but not here
		}

		if dup := findDuplicateCompany(name, ""); dup != nil {
			// Same business, found earlier without its board. Attach the
			// board rather than storing a second row for it.
			config.DB.Model(&models.Company{}).Where("id = ?", dup.ID).Updates(map[string]interface{}{
				"ats_type": hit.Provider, "ats_slug": hit.Slug,
				"careers_url": hit.URL, "ats_checked_at": time.Now(),
			})
			log.Printf("board discovery: attached %s board %q to existing company %q",
				hit.Provider, hit.Slug, dup.Name)
			continue
		}

		website := resolveCompanyWebsite(name)
		domain := extractDomain(website)
		if domain == "" {
			log.Printf("board discovery: skipping %q — no company website found", name)
			continue
		}
		if dup := findDuplicateCompany(name, domain); dup != nil {
			continue
		}

		company := models.Company{
			Name:       name,
			Slug:       slugify(name),
			Website:    website,
			Domain:     domain,
			Area:       area,
			CareersURL: hit.URL,
			ATSType:    hit.Provider,
			ATSSlug:    hit.Slug,
			Source:     "board-search",
		}
		now := time.Now()
		company.ATSCheckedAt = &now

		if lat, lng, geoErr := geocodeArea(area); geoErr == nil {
			company.Lat = lat
			company.Lng = lng
		}

		result := config.DB.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "domain"}},
			DoNothing: true,
		}).Create(&company)
		if result.Error != nil {
			log.Printf("board discovery: failed to save %q: %v", name, result.Error)
			continue
		}
		if result.RowsAffected == 0 {
			continue // domain already present
		}
		saved = append(saved, company)

		// The roles are already in hand, so store them now rather than
		// leaving the company at zero until the next tick.
		for i := range jobs {
			jobs[i].CompanyID = company.ID
		}
		if n, jerr := replaceJobsForCompany(company.ID, jobs); jerr != nil {
			log.Printf("board discovery: failed to store roles for %q: %v", name, jerr)
		} else {
			log.Printf("board discovery: %s (%s/%s) -> %d roles", name, hit.Provider, hit.Slug, n)
		}
	}

	return saved, nil
}

// firstIndianLocation returns the first Indian location among a board's
// roles, which doubles as the company's area for the directory's filters.
func firstIndianLocation(jobs []models.Job) string {
	for _, j := range jobs {
		if looksIndian(j.Location) {
			return j.Location
		}
	}
	return ""
}

// RunDiscoveryRotation is invoked on a schedule and works through the seed
// queries one per tick, then re-syncs the roles of everything already stored.
func RunDiscoveryRotation() {
	if len(boardSeedQueries) == 0 {
		return
	}

	// Only one instance runs this tick. The cron scheduler lives inside the
	// API process, so scaling to two containers would otherwise mean two
	// discovery runs an hour and every job board fetched twice. The lease is
	// slightly shorter than the interval so a crashed instance frees it
	// before the next tick is due.
	if !AcquireCronLease(DiscoveryLeaseName, 55*time.Minute) {
		log.Printf("board discovery rotation: another instance holds the lease, skipping")
		return
	}
	// Deliberately NOT released when the run finishes. Each process registers
	// its own "@every 1h" schedule, so instance ticks are not aligned: if A
	// finishes at :05 and B ticks at :25, B would take the freed lease and —
	// because the query index is derived from the current hour — run the
	// identical query again. The 55-minute TTL covers the rest of the
	// interval; shutdown releases it explicitly.
	idx := int((time.Now().Unix() / int64(discoveryIntervalSeconds)) % int64(len(boardSeedQueries)))
	query := boardSeedQueries[idx]

	// Discovery and job sync are deliberately independent. Discovery depends
	// on a live search, so it's the flakier half — if it fails we still want
	// to refresh roles for the companies we already have.
	if saved, err := DiscoverFromBoards(query, boardResultsPerQuery); err != nil {
		log.Printf("board discovery rotation failed for %q: %v", query, err)
	} else {
		log.Printf("board discovery rotation: %q -> %d new companies saved", query, len(saved))
	}

	SyncAllCompanyJobs()
}
