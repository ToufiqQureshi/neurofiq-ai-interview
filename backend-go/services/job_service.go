package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/config"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/models"
	"gorm.io/gorm/clause"
)

// Patterns for spotting an embedded ATS job board in a company's own
// careers page HTML. Companies embed these themselves — that link is how
// their careers page renders its listings — so finding it is far more
// reliable than guessing a board slug from the company name.
var (
	// Greenhouse serves regional boards too (e.g. job-boards.eu.greenhouse.io),
	// so allow an optional region segment before greenhouse.io.
	greenhouseLinkRe      = regexp.MustCompile(`(?:boards|job-boards)\.(?:[a-z]{2}\.)?greenhouse\.io/([a-zA-Z0-9_-]+)`)
	leverLinkRe           = regexp.MustCompile(`jobs\.lever\.co/([a-zA-Z0-9_-]+)`)
	ashbyLinkRe           = regexp.MustCompile(`jobs\.ashbyhq\.com/([a-zA-Z0-9_.-]+)`)
	workableLinkRe        = regexp.MustCompile(`apply\.workable\.com/([a-zA-Z0-9_-]+)`)
	smartRecruitersLinkRe = regexp.MustCompile(`careers\.smartrecruiters\.com/([a-zA-Z0-9_-]+)`)
	kekaLinkRe            = regexp.MustCompile(`([a-zA-Z0-9-]+)\.keka\.com/careers`)
	// Workday boards live at <tenant>.<region>.myworkdayjobs.com and need a
	// third piece — the job-site id — which isn't in the URL, so it gets
	// probed at detection time. Slug is stored as "tenant:region:site".
	workdayLinkRe = regexp.MustCompile(`([a-zA-Z0-9-]+)\.(wd\d+)\.myworkdayjobs\.com`)
)

// atsRecheckInterval is how long to wait before retrying detection on a
// company that came back with no ATS. Keeps the scraper's monthly credit
// usage proportional to new companies, not to total directory size.
const atsRecheckInterval = 7 * 24 * time.Hour

// workdaySiteCandidates are the job-site ids Workday tenants commonly use.
// Detection probes these in order and keeps the first that returns jobs.
var workdaySiteCandidates = []string{"External", "External_Careers", "careers", "Careers_External", "External_Career_Site"}

// atsProviders is the detection order used when falling back to guessing a
// board slug from the company name. Greenhouse and Lever go first because
// they're the most common in this dataset.
var atsProviders = []string{"greenhouse", "lever", "smartrecruiters", "ashby", "workable"}

type greenhouseJob struct {
	Title       string `json:"title"`
	AbsoluteURL string `json:"absolute_url"`
	Location    struct {
		Name string `json:"name"`
	} `json:"location"`
	Departments []struct {
		Name string `json:"name"`
	} `json:"departments"`
}

type greenhouseResponse struct {
	Jobs []greenhouseJob `json:"jobs"`
}

type leverJob struct {
	Text      string `json:"text"`
	HostedURL string `json:"hostedUrl"`
	Categories struct {
		Team     string `json:"team"`
		Location string `json:"location"`
	} `json:"categories"`
}

type ashbyJob struct {
	Title      string `json:"title"`
	Department string `json:"department"`
	Location   string `json:"location"`
	JobURL     string `json:"jobUrl"`
}

type ashbyResponse struct {
	Jobs []ashbyJob `json:"jobs"`
}

type smartRecruitersJob struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Location struct {
		City         string `json:"city"`
		FullLocation string `json:"fullLocation"`
	} `json:"location"`
	Department struct {
		Label string `json:"label"`
	} `json:"department"`
	Function struct {
		Label string `json:"label"`
	} `json:"function"`
}

type smartRecruitersResponse struct {
	TotalFound int                  `json:"totalFound"`
	Content    []smartRecruitersJob `json:"content"`
}

type kekaJob struct {
	ID             int    `json:"id"`
	Title          string `json:"title"`
	DepartmentName string `json:"departmentName"`
	JobLocations   []struct {
		Name string `json:"name"`
		City string `json:"city"`
	} `json:"jobLocations"`
}

type workableJob struct {
	Title    string `json:"title"`
	URL      string `json:"url"`
	Shortcode string `json:"shortcode"`
	Department string `json:"department"`
	Location struct {
		City    string `json:"city"`
		Country string `json:"country"`
	} `json:"location"`
}

type workableResponse struct {
	Jobs []workableJob `json:"jobs"`
}

type workdayJob struct {
	Title         string `json:"title"`
	ExternalPath  string `json:"externalPath"`
	LocationsText string `json:"locationsText"`
}

type workdayResponse struct {
	Total       int          `json:"total"`
	JobPostings []workdayJob `json:"jobPostings"`
}

// scanForATS looks for an embedded ATS job-board link in page content and
// returns the provider and its board slug.
func scanForATS(content string) (atsType, atsSlug string) {
	// Ordered most-specific first; keka's pattern is a bare subdomain match
	// so it must not shadow the others.
	for _, p := range []struct {
		name string
		re   *regexp.Regexp
	}{
		{"greenhouse", greenhouseLinkRe},
		{"lever", leverLinkRe},
		{"ashby", ashbyLinkRe},
		{"smartrecruiters", smartRecruitersLinkRe},
		{"workable", workableLinkRe},
		{"keka", kekaLinkRe},
	} {
		if m := p.re.FindStringSubmatch(content); m != nil {
			return p.name, m[1]
		}
	}

	// Workday needs an extra step: the URL gives us the tenant and region,
	// but not the job-site id, so probe the common ones and keep whichever
	// actually returns postings.
	if m := workdayLinkRe.FindStringSubmatch(content); m != nil {
		tenant, region := m[1], m[2]
		for _, site := range workdaySiteCandidates {
			slug := fmt.Sprintf("%s:%s:%s", tenant, region, site)
			if jobs, err := fetchWorkdayJobs(slug); err == nil && len(jobs) > 0 {
				return "workday", slug
			}
		}
	}
	return "", ""
}

// DetectATS figures out which applicant-tracking system (if any) a company
// uses, so real open roles can be pulled from its public API rather than
// just linking out to the careers page.
//
// Three tiers, cheapest first — the expensive one only runs when the free
// ones come up empty:
//
//  1. Plain HTTP fetch of the careers page, scanned for an embedded board
//     link. Free and instant. Works whenever the page is server-rendered.
//  2. Hosted render (Firecrawl, falling back to Jina) for pages that build
//     their content in the browser, where a plain fetch returns an empty
//     shell. This is the only step that consumes a paid-tier credit.
//  3. Guess the board slug from the company name and verify it against each
//     provider's real API. Free.
//
// No LLM or search call anywhere — this is a lookup, not a judgment call.
func DetectATS(company models.Company) (atsType, atsSlug string) {
	pageURL := company.CareersURL
	if pageURL == "" {
		pageURL = company.Website
	}

	if pageURL != "" {
		// Tier 1 — free
		if html, err := fetchText(pageURL); err == nil {
			if t, s := scanForATS(html); t != "" {
				return t, s
			}
		}

		// Tier 2 — costs a credit, so only for client-rendered pages that
		// tier 1 couldn't read.
		if content, provider, err := FetchRenderedPage(pageURL); err == nil {
			if t, s := scanForATS(content); t != "" {
				log.Printf("ATS detect: %s found via %s for %s", t, provider, company.Name)
				return t, s
			}
		}
	}

	// Tier 3 — free. Guess the board slug from the company name and verify
	// against each provider's real API. Only counts as a match if actual
	// jobs come back — an empty board tells us nothing.
	guess := slugify(company.Name)
	if guess == "" {
		return "", ""
	}
	for _, provider := range atsProviders {
		if n, err := countJobsFor(provider, guess); err == nil && n > 0 {
			return provider, guess
		}
	}
	return "", ""
}

// countJobsFor returns how many open roles a provider reports for a slug.
// Used by DetectATS to verify a guessed slug actually belongs to a company.
func countJobsFor(provider, slug string) (int, error) {
	switch provider {
	case "greenhouse":
		jobs, err := fetchGreenhouseJobs(slug)
		return len(jobs), err
	case "lever":
		jobs, err := fetchLeverJobs(slug)
		return len(jobs), err
	case "ashby":
		jobs, err := fetchAshbyJobs(slug)
		return len(jobs), err
	case "smartrecruiters":
		jobs, err := fetchSmartRecruitersJobs(slug)
		return len(jobs), err
	case "workable":
		jobs, err := fetchWorkableJobs(slug)
		return len(jobs), err
	case "keka":
		jobs, err := fetchKekaJobs(slug)
		return len(jobs), err
	case "workday":
		jobs, err := fetchWorkdayJobs(slug)
		return len(jobs), err
	}
	return 0, fmt.Errorf("unknown provider %q", provider)
}

func fetchText(url string) (string, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; NeuroFIQ-JobMap/1.0)")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 500_000)) // cap: don't pull huge pages
	return string(body), err
}

func fetchGreenhouseJobs(slug string) ([]greenhouseJob, error) {
	resp, err := http.Get("https://boards-api.greenhouse.io/v1/boards/" + slug + "/jobs")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("greenhouse status %d", resp.StatusCode)
	}
	var parsed greenhouseResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	return parsed.Jobs, nil
}

func fetchLeverJobs(slug string) ([]leverJob, error) {
	resp, err := http.Get("https://api.lever.co/v0/postings/" + slug + "?mode=json")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("lever status %d", resp.StatusCode)
	}
	var parsed []leverJob
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	return parsed, nil
}

func fetchAshbyJobs(slug string) ([]ashbyJob, error) {
	resp, err := http.Get("https://api.ashbyhq.com/posting-api/job-board/" + slug)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("ashby status %d", resp.StatusCode)
	}
	var parsed ashbyResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	return parsed.Jobs, nil
}

func fetchSmartRecruitersJobs(slug string) ([]smartRecruitersJob, error) {
	// limit=100 is the API's max page size; without it you silently get
	// only the first 10 roles.
	resp, err := http.Get("https://api.smartrecruiters.com/v1/companies/" + slug + "/postings?limit=100")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("smartrecruiters status %d", resp.StatusCode)
	}
	var parsed smartRecruitersResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	return parsed.Content, nil
}

func fetchKekaJobs(slug string) ([]kekaJob, error) {
	// Keka's official developer API is partner-gated, but every Keka-hosted
	// careers portal exposes this endpoint publicly — it's what the page
	// itself calls to render its listings.
	resp, err := http.Get("https://" + slug + ".keka.com/careers/api/jobs/default/active")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("keka status %d", resp.StatusCode)
	}
	var parsed []kekaJob
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	return parsed, nil
}

func fetchWorkableJobs(slug string) ([]workableJob, error) {
	resp, err := http.Get("https://apply.workable.com/api/v1/widget/accounts/" + slug + "?details=true")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("workable status %d", resp.StatusCode)
	}
	var parsed workableResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	return parsed.Jobs, nil
}

// fetchWorkdayJobs pulls postings from Workday's public CXS job-board API.
// slug is "tenant:region:site" (see workdayLinkRe). The API pages at 20 per
// request, so this loops until it has everything.
func fetchWorkdayJobs(slug string) ([]workdayJob, error) {
	parts := strings.Split(slug, ":")
	if len(parts) != 3 {
		return nil, fmt.Errorf("bad workday slug %q", slug)
	}
	tenant, region, site := parts[0], parts[1], parts[2]
	endpoint := fmt.Sprintf("https://%s.%s.myworkdayjobs.com/wday/cxs/%s/%s/jobs", tenant, region, tenant, site)

	const pageSize = 20
	const maxPages = 25 // hard cap: 500 roles is plenty, and stops runaway loops

	var all []workdayJob
	for page := 0; page < maxPages; page++ {
		body, _ := json.Marshal(map[string]interface{}{
			"appliedFacets": map[string]interface{}{},
			"limit":         pageSize,
			"offset":        page * pageSize,
			"searchText":    "",
		})
		req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; NeuroFIQ-JobMap/1.0)")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != 200 {
			resp.Body.Close()
			return nil, fmt.Errorf("workday status %d", resp.StatusCode)
		}
		var parsed workdayResponse
		decErr := json.NewDecoder(resp.Body).Decode(&parsed)
		resp.Body.Close()
		if decErr != nil {
			return nil, decErr
		}

		all = append(all, parsed.JobPostings...)
		if len(parsed.JobPostings) < pageSize || len(all) >= parsed.Total {
			break
		}
	}
	return all, nil
}

// SyncJobsForCompany detects (if not already known) and pulls the company's
// current open roles from its ATS's public API, upserting them into the
// jobs table (deduped by company+url) and removing listings that have since
// closed.
func SyncJobsForCompany(company models.Company) (int, error) {
	atsType, atsSlug := company.ATSType, company.ATSSlug
	if atsType == "" {
		// Detection can cost a scraper credit, so don't retry a company that
		// recently came back with nothing. Support for new ATS platforms
		// gets added occasionally, not hourly — a weekly retry is enough to
		// pick those up without re-scraping the whole directory every tick.
		if company.ATSCheckedAt != nil && time.Since(*company.ATSCheckedAt) < atsRecheckInterval {
			return 0, nil
		}

		// The agent often omits the careers URL, or points it at the
		// homepage. Recover it from the company's own domain before giving
		// up — otherwise these companies sit at zero jobs forever.
		if resolved := ResolveCareersURL(company); resolved != company.CareersURL {
			log.Printf("careers URL resolved for %s: %s", company.Name, resolved)
			company.CareersURL = resolved
			config.DB.Model(&models.Company{}).Where("id = ?", company.ID).
				Update("careers_url", resolved)
		}

		atsType, atsSlug = DetectATS(company)

		now := time.Now()
		updates := map[string]interface{}{"ats_checked_at": now}
		if atsType != "" {
			updates["ats_type"] = atsType
			updates["ats_slug"] = atsSlug
		}
		config.DB.Model(&models.Company{}).Where("id = ?", company.ID).Updates(updates)
	}

	// Most companies run a custom careers portal rather than a supported
	// ATS. Without this fallback they'd show zero jobs while actively
	// hiring, which is the common case, not the edge case.
	if atsType == "" {
		return syncJobsFromCareersPage(company)
	}

	var rows []models.Job
	switch atsType {
	case "greenhouse":
		jobs, err := fetchGreenhouseJobs(atsSlug)
		if err != nil {
			return 0, err
		}
		for _, j := range jobs {
			dept := ""
			if len(j.Departments) > 0 {
				dept = j.Departments[0].Name
			}
			rows = append(rows, models.Job{
				CompanyID: company.ID, Title: j.Title, Department: dept,
				Location: j.Location.Name, URL: j.AbsoluteURL, Source: "greenhouse",
			})
		}
	case "lever":
		jobs, err := fetchLeverJobs(atsSlug)
		if err != nil {
			return 0, err
		}
		for _, j := range jobs {
			rows = append(rows, models.Job{
				CompanyID: company.ID, Title: j.Text, Department: j.Categories.Team,
				Location: j.Categories.Location, URL: j.HostedURL, Source: "lever",
			})
		}
	case "ashby":
		jobs, err := fetchAshbyJobs(atsSlug)
		if err != nil {
			return 0, err
		}
		for _, j := range jobs {
			rows = append(rows, models.Job{
				CompanyID: company.ID, Title: j.Title, Department: j.Department,
				Location: j.Location, URL: j.JobURL, Source: "ashby",
			})
		}
	case "smartrecruiters":
		jobs, err := fetchSmartRecruitersJobs(atsSlug)
		if err != nil {
			return 0, err
		}
		for _, j := range jobs {
			// department is often empty; function is the usable fallback
			dept := j.Department.Label
			if dept == "" {
				dept = j.Function.Label
			}
			loc := j.Location.FullLocation
			if loc == "" {
				loc = j.Location.City
			}
			rows = append(rows, models.Job{
				CompanyID: company.ID, Title: j.Name, Department: dept, Location: loc,
				// The API response has no public posting URL, so build the
				// canonical careers-site one from the slug + posting id.
				URL:    fmt.Sprintf("https://careers.smartrecruiters.com/%s/%s", atsSlug, j.ID),
				Source: "smartrecruiters",
			})
		}
	case "workable":
		jobs, err := fetchWorkableJobs(atsSlug)
		if err != nil {
			return 0, err
		}
		for _, j := range jobs {
			loc := strings.TrimSpace(strings.Trim(j.Location.City+", "+j.Location.Country, ", "))
			url := j.URL
			if url == "" && j.Shortcode != "" {
				url = fmt.Sprintf("https://apply.workable.com/%s/j/%s/", atsSlug, j.Shortcode)
			}
			rows = append(rows, models.Job{
				CompanyID: company.ID, Title: j.Title, Department: j.Department,
				Location: loc, URL: url, Source: "workable",
			})
		}
	case "keka":
		jobs, err := fetchKekaJobs(atsSlug)
		if err != nil {
			return 0, err
		}
		for _, j := range jobs {
			var locs []string
			for _, l := range j.JobLocations {
				name := l.Name
				if name == "" {
					name = l.City
				}
				if name != "" {
					locs = append(locs, name)
				}
			}
			rows = append(rows, models.Job{
				CompanyID: company.ID, Title: j.Title, Department: j.DepartmentName,
				Location: strings.Join(locs, ", "),
				URL:      fmt.Sprintf("https://%s.keka.com/careers/jobdetails/%d", atsSlug, j.ID),
				Source:   "keka",
			})
		}
	case "workday":
		jobs, err := fetchWorkdayJobs(atsSlug)
		if err != nil {
			return 0, err
		}
		parts := strings.Split(atsSlug, ":")
		if len(parts) != 3 {
			return 0, fmt.Errorf("bad workday slug %q", atsSlug)
		}
		tenant, region, site := parts[0], parts[1], parts[2]
		for _, j := range jobs {
			rows = append(rows, models.Job{
				CompanyID: company.ID, Title: j.Title, Location: j.LocationsText,
				URL: fmt.Sprintf("https://%s.%s.myworkdayjobs.com/en-US/%s%s",
					tenant, region, site, j.ExternalPath),
				Source: "workday",
			})
		}
	}

	return replaceJobsForCompany(company.ID, rows)
}

// replaceJobsForCompany makes the jobs table match `rows` exactly for one
// company: closed postings are deleted, existing ones updated, new ones
// inserted. Shared by the ATS-API and careers-page paths so both stay
// idempotent — re-running a sync must never duplicate rows.
func replaceJobsForCompany(companyID string, rows []models.Job) (int, error) {
	// Drop anything missing the two fields we require.
	valid := rows[:0]
	seen := map[string]bool{}
	for _, r := range rows {
		if r.Title == "" || r.URL == "" || seen[r.URL] {
			continue // also guards against a provider repeating a URL
		}
		seen[r.URL] = true
		valid = append(valid, r)
	}
	rows = valid

	if len(rows) == 0 {
		// No open roles right now (or the board came back empty) — clear
		// any stale listings we previously had for this company.
		config.DB.Where("company_id = ?", companyID).Delete(&models.Job{})
		return 0, nil
	}

	currentURLs := make([]string, len(rows))
	for i, r := range rows {
		currentURLs[i] = r.URL
	}
	// Drop listings that closed since the last sync.
	config.DB.Where("company_id = ? AND url NOT IN ?", companyID, currentURLs).Delete(&models.Job{})

	if err := config.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "company_id"}, {Name: "url"}},
		DoUpdates: clause.AssignmentColumns([]string{"title", "department", "location"}),
	}).Create(&rows).Error; err != nil {
		return 0, err
	}

	return len(rows), nil
}

// syncJobsFromCareersPage is the fallback for companies with no supported
// ATS: read their own careers page and store whatever roles are listed.
//
// These listings are lower fidelity than an ATS feed — often without a
// per-role apply link — so they're stored with source "careers-page" and can
// be told apart from API-sourced roles.
func syncJobsFromCareersPage(company models.Company) (int, error) {
	pageURL := company.CareersURL
	if pageURL == "" {
		return 0, nil // nothing to read
	}

	extracted, err := ExtractJobsFromCareersPage(pageURL)
	if err != nil {
		return 0, err
	}

	// A lot of /careers pages are marketing pages that link out to the real
	// listings ("View open positions"). If the first page yielded nothing,
	// follow that link once and try there.
	if len(extracted) == 0 {
		if html, ferr := fetchText(pageURL); ferr == nil {
			if next := findJobsListingLink(html, pageURL); next != "" {
				log.Printf("careers page for %s had no roles — following %s", company.Name, next)
				if more, merr := ExtractJobsFromCareersPage(next); merr == nil && len(more) > 0 {
					extracted = more
					pageURL = next // apply links should point at the real board
				}
			}
		}
	}

	var rows []models.Job
	for _, j := range extracted {
		title := strings.TrimSpace(j.Title)
		if title == "" {
			continue
		}
		// Many custom portals don't expose a per-role link. Fall back to the
		// careers page, made unique per role so the (company_id, url) index
		// doesn't collapse every job into one row.
		url := strings.TrimSpace(j.URL)
		if url == "" || !strings.HasPrefix(url, "http") {
			url = pageURL + "#" + slugify(title)
		}
		rows = append(rows, models.Job{
			CompanyID:  company.ID,
			Title:      title,
			Department: strings.TrimSpace(j.Department),
			Location:   strings.TrimSpace(j.Location),
			URL:        url,
			Source:     "careers-page",
		})
	}

	if !careersPageResultLooksReal(rows, company.Name) {
		return 0, nil
	}
	return replaceJobsForCompany(company.ID, rows)
}

// maxCareersPageRoles is a sanity ceiling for a single careers page. A
// company genuinely listing more than this runs a real ATS, which the
// earlier tiers would have found — so a huge result here means we read the
// wrong kind of list.
const maxCareersPageRoles = 60

// careersPageResultLooksReal rejects LLM extractions that clearly aren't job
// openings.
//
// This exists because of a real failure: an education platform's careers page
// linked to a "career options" guidance article, and the extraction happily
// returned 295 "jobs" — Actor, Actuary, Addiction Counselor, Aerospace
// Engineer… an alphabetical list of professions. Bad data is worse than none.
func careersPageResultLooksReal(rows []models.Job, companyName string) bool {
	if len(rows) == 0 {
		return true // nothing to store, nothing to doubt
	}

	if len(rows) > maxCareersPageRoles {
		log.Printf("careers page for %s returned %d roles — above the sane ceiling, discarding",
			companyName, len(rows))
		return false
	}

	// A real listing carries at least some location or department metadata.
	// A generic list of profession names carries neither.
	withMeta := 0
	for _, r := range rows {
		if r.Location != "" || r.Department != "" {
			withMeta++
		}
	}
	if len(rows) >= 5 && withMeta == 0 {
		log.Printf("careers page for %s returned %d roles with no location or department at all — discarding",
			companyName, len(rows))
		return false
	}

	return true
}

// careersPathGuesses are the conventional places a careers page lives.
// Ordered by how common they are.
var careersPathGuesses = []string{"/careers", "/careers/", "/jobs", "/careers/jobs", "/company/careers", "/about/careers"}

// ResolveCareersURL returns a usable careers page for a company. The
// discovery agent often omits it (or points at the homepage), which leaves
// the company permanently at zero jobs — so when it's missing or clearly
// wrong, probe the conventional paths on the company's own domain.
//
// Free: plain HTTP HEAD/GET only, no scraper credits, no search calls.
func ResolveCareersURL(company models.Company) string {
	current := strings.TrimSpace(company.CareersURL)
	site := strings.TrimSpace(company.Website)

	// Treat "careers URL == homepage" as missing — that's the agent
	// defaulting rather than actually finding a careers page.
	if current != "" && strings.TrimRight(current, "/") != strings.TrimRight(site, "/") {
		return current
	}
	if site == "" {
		return current
	}

	base := strings.TrimRight(site, "/")
	for _, path := range careersPathGuesses {
		candidate := base + path
		if pageLooksLikeCareers(candidate) {
			return candidate
		}
	}
	return current
}

// hiringMarkers are phrases that only appear on a page that is actually
// advertising jobs. Deliberately excludes weak ones like "join us" and
// "apply now" — a training company's course page says both, which is how a
// courses page got mistaken for a careers page.
var hiringMarkers = []string{
	"open position", "open role", "current opening", "job opening",
	"we're hiring", "we are hiring", "view job", "view opening",
	"join our team", "career opportunit", "job vacanc", "apply for this",
}

// pageLooksLikeCareers fetches a candidate URL and checks it's a real
// careers page rather than a 404, a soft-404, or a marketing page that
// happens to use similar words.
func pageLooksLikeCareers(url string) bool {
	body, err := fetchText(url)
	if err != nil || len(body) < 500 {
		return false
	}
	return containsHiringMarker(body)
}

func containsHiringMarker(body string) bool {
	lower := strings.ToLower(body)
	for _, marker := range hiringMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// jobsLinkRe finds anchors whose text suggests they lead to the actual job
// listings. Many /careers pages are marketing pages that link out to the
// real board — without following that link those companies look empty.
var jobsLinkRe = regexp.MustCompile(`(?is)<a[^>]+href=["']([^"']+)["'][^>]*>([^<]{0,80}?(?:view\s+(?:all\s+)?(?:open|job|position|role)|current\s+opening|open\s+position|open\s+role|see\s+(?:all\s+)?job|explore\s+(?:open\s+)?role|browse\s+job|all\s+opening|job\s+opening)[^<]{0,40})</a>`)

// guidancePageRe matches URLs that are career *advice* rather than career
// *openings* — a distinction that cost us 295 fake job rows once.
var guidancePageRe = regexp.MustCompile(`(?i)career-(option|guide|advice|counsel|path|choice)|/blog/|/article|after-12th|course`)

// findJobsListingLink looks for a link from a careers page to the page that
// actually lists roles. Returns an absolute URL, or "" if none found.
func findJobsListingLink(pageHTML, baseURL string) string {
	base, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}
	for _, m := range jobsLinkRe.FindAllStringSubmatch(pageHTML, -1) {
		href := strings.TrimSpace(m[1])
		if href == "" || strings.HasPrefix(href, "#") || strings.HasPrefix(href, "mailto:") || strings.HasPrefix(href, "javascript:") {
			continue
		}
		ref, err := url.Parse(href)
		if err != nil {
			continue
		}
		abs := base.ResolveReference(ref)
		if abs.String() == baseURL {
			continue // links back to itself
		}
		// Education and career-advice sites link to "career options" and
		// "career guide" articles from their careers page. Those are lists
		// of professions, not openings — following one produced 295 bogus
		// "jobs" once.
		if guidancePageRe.MatchString(abs.String()) {
			continue
		}
		return abs.String()
	}
	return ""
}

// ListJobsForCompany returns the currently-open roles for one company.
func ListJobsForCompany(companyID string) ([]models.Job, error) {
	var jobs []models.Job
	err := config.DB.Where("company_id = ?", companyID).Order("title").Find(&jobs).Error
	return jobs, err
}

// SyncAllCompanyJobs re-syncs job listings for every company on each cron
// tick. Two things happen here:
//
//   - Companies with a known ATS get refreshed, so closed postings drop off
//     and newly-posted ones appear.
//   - Companies with no ATS yet get re-detected. This matters because
//     detection improves over time: when support for a new ATS platform is
//     added, companies that previously came back empty need another look,
//     otherwise the new provider only ever applies to newly-discovered
//     companies.
func SyncAllCompanyJobs() {
	var companies []models.Company
	if err := config.DB.Find(&companies).Error; err != nil {
		// Without this check a failed query looks identical to "no companies
		// exist" — the sync would silently do nothing and log success.
		log.Printf("job sync: failed to load companies: %v", err)
		return
	}
	if len(companies) == 0 {
		log.Printf("job sync: no companies in directory yet")
		return
	}

	synced, detected := 0, 0
	for _, c := range companies {
		n, err := SyncJobsForCompany(c)
		if err != nil {
			// Non-fatal: one company's board being briefly unreachable
			// shouldn't stop the rest of the sync.
			continue
		}
		if n > 0 {
			synced += n
			if c.ATSType == "" {
				detected++ // had no ATS before this run
			}
		}
	}
	log.Printf("job sync: %d companies checked, %d newly detected, %d open roles total | scrape usage this month: %v",
		len(companies), detected, synced, ScrapeUsageSummary())
}
