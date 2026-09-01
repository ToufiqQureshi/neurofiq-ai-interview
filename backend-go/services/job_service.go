package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
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

	// Darwinbox is common across Indian employers and serves a plain JSON
	// board, but only to a request that looks like it came from the page:
	// see fetchDarwinboxJobs.
	darwinboxLinkRe = regexp.MustCompile(`([a-zA-Z0-9-]+)\.darwinbox\.(?:in|com)`)
)

// atsRecheckInterval is how long a company with a known board is left alone
// before its detection is revisited. Its roles are still re-synced every
// tick — this only governs re-detection.
const atsRecheckInterval = 7 * 24 * time.Hour

// atsRetryInterval is the shorter wait for a company we could not find a
// board for. Detection improves (a new provider, a better careers-page
// reader), and a week-long freeze meant those improvements reached the
// directory a week late.
const atsRetryInterval = 12 * time.Hour

// workdaySiteCandidates are the job-site ids Workday tenants commonly use.
// Detection probes these in order and keeps the first that returns jobs.
var workdaySiteCandidates = []string{"External", "External_Careers", "careers", "Careers_External", "External_Career_Site"}

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
	Text       string `json:"text"`
	HostedURL  string `json:"hostedUrl"`
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

// darwinboxJob is one row of Darwinbox's careers API. The payload carries
// both a coded and a display variant of most fields — department_name is
// "CRM - Operations (0014_JSL_CRM_L84)" while department_name_only is just
// "CRM - Operations" — so the display variants are the ones read here.
type darwinboxJob struct {
	ID             string   `json:"id"`
	Title          string   `json:"title"`
	DepartmentName string   `json:"department_name_only"`
	Locations      string   `json:"locations"`
	OfficeLocs     []string `json:"officelocations_without_area"`
}

type darwinboxResponse struct {
	Status string         `json:"status"`
	Data   []darwinboxJob `json:"data"`
}

type workableJob struct {
	Title      string `json:"title"`
	URL        string `json:"url"`
	Shortcode  string `json:"shortcode"`
	Department string `json:"department"`
	Location   struct {
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
		{"darwinbox", darwinboxLinkRe},
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
// Both tiers read the company's OWN careers page and look for the board link
// the company embedded there itself. That link is evidence, not inference —
// it is how the page renders its own listings — so a match is always the
// right company:
//
//  1. Plain HTTP fetch. Free and instant, works on server-rendered pages.
//  2. Hosted render (Jina first, Firecrawl only if Jina fails) for pages
//     that build their listings in the browser, where a plain fetch returns
//     an empty shell.
//
// What this deliberately does NOT do is guess a slug from the company name.
// That guess was wrong in a way that is worse than finding nothing: board
// slugs are not unique across companies, and the check only asked "did any
// jobs come back", never "are they this company's". jobs.lever.co/cred
// returns a full, healthy job list — for CreditVidya, not for CRED. The
// directory then showed one company's roles under another's name. A company
// with no roles is a gap; a company with someone else's roles is a lie.
//
// No LLM or search call anywhere — this is a lookup, not a judgment call.
func DetectATS(company models.Company) (atsType, atsSlug string) {
	pageURL := company.CareersURL
	if pageURL == "" {
		pageURL = company.Website
	}
	if pageURL == "" {
		return "", ""
	}

	// Tier 1 — free
	if page, err := fetchText(pageURL); err == nil {
		if t, s := scanForATS(page); t != "" {
			return t, s
		}
	}

	// Tier 2 — a rendered read of the same page. FetchRenderedPage prefers
	// the free provider, so this stays free in the common case.
	if content, provider, err := FetchRenderedPage(pageURL); err == nil {
		if t, s := scanForATS(content); t != "" {
			log.Printf("ATS detect: %s found via %s for %s", t, provider, company.Name)
			return t, s
		}
	}

	return "", ""
}

// fetchText downloads a page whose address we did not choose.
//
// Every URL that reaches here came from an LLM's web search, from a company
// record it produced, or from an href on a page we scraped. That makes this
// the one function in the codebase that will happily fetch whatever a third
// party puts in front of it, so it goes through SafeExternalGet: bounded
// timeout, and a dialer that refuses loopback, private, and cloud-metadata
// addresses even when a public hostname resolves to one.
func fetchText(url string) (string, error) {
	resp, err := SafeExternalGet(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// A 404 body is still a body, and a site's not-found template routinely
	// carries the word "careers" in its own navigation. Returning it made a
	// dead link look like a live careers page, which is how companies ended
	// up stored against a URL that had 404'd for months — re-rendered on
	// every sync, at the cost of a scrape credit each time, to extract
	// nothing.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("status %d for %s", resp.StatusCode, url)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 500_000)) // cap: don't pull huge pages
	return string(body), err
}

// atsGet calls one of the applicant-tracking APIs we support. The host is
// ours to choose, but the slug inside the URL came out of a regex over
// scraped HTML — so it is validated before it can steer the request
// somewhere else entirely.
func atsGet(url string) (*http.Response, error) {
	return SafeExternalGet(url)
}

// validATSSlug accepts only the shape a real board identifier takes. Without
// it a scraped slug of "evil.example.com/x?" turns "https://<slug>.keka.com/…"
// into a request to a host we never intended to contact.
func validATSSlug(slug string) bool {
	if slug == "" || len(slug) > 100 {
		return false
	}
	for _, r := range slug {
		ok := r == '-' || r == '_' || r == '.' || r == ':' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !ok {
			return false
		}
	}
	return !strings.Contains(slug, "..")
}

func fetchGreenhouseJobs(slug string) ([]greenhouseJob, error) {
	if !validATSSlug(slug) {
		return nil, fmt.Errorf("invalid greenhouse slug %q", slug)
	}
	resp, err := atsGet("https://boards-api.greenhouse.io/v1/boards/" + slug + "/jobs")
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
	if !validATSSlug(slug) {
		return nil, fmt.Errorf("invalid lever slug %q", slug)
	}
	resp, err := atsGet("https://api.lever.co/v0/postings/" + slug + "?mode=json")
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
	if !validATSSlug(slug) {
		return nil, fmt.Errorf("invalid ashby slug %q", slug)
	}
	resp, err := atsGet("https://api.ashbyhq.com/posting-api/job-board/" + slug)
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
	if !validATSSlug(slug) {
		return nil, fmt.Errorf("invalid smartrecruiters slug %q", slug)
	}
	// limit=100 is the API's max page size; without it you silently get
	// only the first 10 roles.
	resp, err := atsGet("https://api.smartrecruiters.com/v1/companies/" + slug + "/postings?limit=100")
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

// browserUserAgent is sent only where a board refuses a plain client. Our
// own identifying agent is the default everywhere else (see httputil.go);
// this is the exception, not the rule.
const browserUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

// maxATSResponseBytes caps a board's JSON so a misbehaving endpoint cannot
// stream us out of memory.
const maxATSResponseBytes = 8 << 20 // 8 MB

// fetchDarwinboxJobs reads a Darwinbox tenant's public board.
//
// Two things make this different from the other providers. It is a POST, not
// a GET — the board is a search endpoint and the filter goes in the body.
// And the tenant sits behind a bot check that answers a bare client with an
// HTML challenge page instead of JSON, so the request has to carry the
// headers a browser would send from the careers page. Those headers are the
// difference between this working on a free HTTP fetch and needing a
// rendered scrape that costs a credit.
//
// `limit` is set high enough to take the whole board in one request; the
// largest tenant seen while building this listed 134 roles.
//
// Not every tenant answers. A strictly-configured one refuses this client
// with a 403 that no set of headers gets past: the check is on the TLS and
// HTTP/2 fingerprint, which curl clears and Go's stack does not. Of five
// tenants tested, four returned their board (24, 77 and 54 roles, plus one
// genuinely empty) and the fifth 403'd. That is an ordinary provider error
// here -- the company finishes the sync with no roles instead of failing the
// run, and the rendered tiers can still reach it.
func fetchDarwinboxJobs(slug string) ([]darwinboxJob, error) {
	if !validATSSlug(slug) {
		return nil, fmt.Errorf("invalid darwinbox slug %q", slug)
	}

	base := "https://" + slug + ".darwinbox.in"
	endpoint := base + "/ms/candidateapi/job/alljobs?companyId=main"
	body := []byte(`{"companyId":"main","page":1,"sort_option":"new","limit":300}`)

	if err := AllowedPublicURL(endpoint); err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", browserUserAgent)
	req.Header.Set("Origin", base)
	req.Header.Set("Referer", base+"/ms/candidatev2/main/careers/allJobs")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := externalClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("darwinbox status %d", resp.StatusCode)
	}

	var parsed darwinboxResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxATSResponseBytes)).Decode(&parsed); err != nil {
		// A challenge page decodes as neither JSON nor an error we can act
		// on, so say which tenant it was.
		return nil, fmt.Errorf("darwinbox %s: %w", slug, err)
	}
	if parsed.Status != "success" {
		return nil, fmt.Errorf("darwinbox %s returned status %q", slug, parsed.Status)
	}
	return parsed.Data, nil
}

func fetchKekaJobs(slug string) ([]kekaJob, error) {
	if !validATSSlug(slug) {
		return nil, fmt.Errorf("invalid keka slug %q", slug)
	}
	// Keka's official developer API is partner-gated, but every Keka-hosted
	// careers portal exposes this endpoint publicly — it's what the page
	// itself calls to render its listings.
	resp, err := atsGet("https://" + slug + ".keka.com/careers/api/jobs/default/active")
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
	if !validATSSlug(slug) {
		return nil, fmt.Errorf("invalid workable slug %q", slug)
	}
	resp, err := atsGet("https://apply.workable.com/api/v1/widget/accounts/" + slug + "?details=true")
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
	if !validATSSlug(slug) {
		return nil, fmt.Errorf("invalid workday slug %q", slug)
	}
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

		resp, err := externalClient.Do(req)
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
		// Detection reads the careers page, so don't repeat it on every tick
		// for a company that just came back with nothing.
		//
		// The wait is short, because the long one hid every improvement we
		// made. A failed detection used to freeze a company for a full week,
		// which meant a fix shipped on Monday changed nothing visible until
		// the following Monday — and any company that failed during a bad
		// week stayed at zero roles long after the cause was gone. A
		// successful detection is what earns the week; a failure is worth
		// another look the same day.
		if company.ATSCheckedAt != nil && time.Since(*company.ATSCheckedAt) < atsRecheckIntervalFor(company) {
			return 0, nil
		}

		// The careers URL is often missing, or points at the homepage.
		// Recover it from the company's own domain before giving up —
		// otherwise these companies sit at zero jobs forever.
		if resolved := ResolveCareersURL(company); resolved != company.CareersURL {
			log.Printf("careers URL resolved for %s: %s", company.Name, resolved)
			company.CareersURL = resolved
			config.DB.Model(&models.Company{}).Where("id = ?", company.ID).
				Update("careers_url", resolved)
		}

		atsType, atsSlug = DetectATS(company)

		if atsType != "" {
			config.DB.Model(&models.Company{}).Where("id = ?", company.ID).Updates(
				map[string]interface{}{
					"ats_type": atsType, "ats_slug": atsSlug, "ats_checked_at": time.Now(),
				})
		} else {
			// Stamping ats_checked_at here is what starts the week-long
			// cooldown, so it is done only once the careers-page attempt
			// below has actually had its turn. It used to be written
			// unconditionally, before that attempt ran: a single Firecrawl
			// rate-limit error then cost the company seven days at zero
			// roles, and the next attempt a week later could lose the same
			// coin toss. Now a transient failure is retried on the next
			// tick, and only a clean "nothing here" starts the clock.
			n, err := syncJobsFromCareersPage(company)
			if err != nil {
				return 0, err
			}
			config.DB.Model(&models.Company{}).Where("id = ?", company.ID).
				Update("ats_checked_at", time.Now())
			return n, nil
		}
	}

	// Most companies run a custom careers portal rather than a supported
	// ATS. Without this fallback they'd show zero jobs while actively
	// hiring, which is the common case, not the edge case.
	if atsType == "" {
		return syncJobsFromCareersPage(company)
	}

	rows, err := FetchATSJobs(company.ID, atsType, atsSlug)
	if err != nil {
		return 0, err
	}
	return applySyncedJobs(company, rows)
}

// atsRecheckIntervalFor is how long to leave a company alone before looking
// for its board again: a week once we've found one, twelve hours while we
// still haven't.
func atsRecheckIntervalFor(company models.Company) time.Duration {
	if company.ATSType != "" {
		return atsRecheckInterval
	}
	return atsRetryInterval
}

// FetchATSJobs returns the open roles a provider's public API reports for one
// board slug, as Job rows belonging to companyID.
//
// Split out from SyncJobsForCompany so board discovery can look at a board's
// roles before deciding whether the company belongs in the directory at all,
// without a second copy of this switch drifting out of sync with it.
func FetchATSJobs(companyID, atsType, atsSlug string) ([]models.Job, error) {
	var rows []models.Job
	switch atsType {
	case "greenhouse":
		jobs, err := fetchGreenhouseJobs(atsSlug)
		if err != nil {
			return nil, err
		}
		for _, j := range jobs {
			dept := ""
			if len(j.Departments) > 0 {
				dept = j.Departments[0].Name
			}
			rows = append(rows, models.Job{
				CompanyID: companyID, Title: j.Title, Department: dept,
				Location: j.Location.Name, URL: j.AbsoluteURL, Source: "greenhouse",
			})
		}
	case "lever":
		jobs, err := fetchLeverJobs(atsSlug)
		if err != nil {
			return nil, err
		}
		for _, j := range jobs {
			rows = append(rows, models.Job{
				CompanyID: companyID, Title: j.Text, Department: j.Categories.Team,
				Location: j.Categories.Location, URL: j.HostedURL, Source: "lever",
			})
		}
	case "ashby":
		jobs, err := fetchAshbyJobs(atsSlug)
		if err != nil {
			return nil, err
		}
		for _, j := range jobs {
			rows = append(rows, models.Job{
				CompanyID: companyID, Title: j.Title, Department: j.Department,
				Location: j.Location, URL: j.JobURL, Source: "ashby",
			})
		}
	case "smartrecruiters":
		jobs, err := fetchSmartRecruitersJobs(atsSlug)
		if err != nil {
			return nil, err
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
				CompanyID: companyID, Title: j.Name, Department: dept, Location: loc,
				// The API response has no public posting URL, so build the
				// canonical careers-site one from the slug + posting id.
				URL:    fmt.Sprintf("https://careers.smartrecruiters.com/%s/%s", atsSlug, j.ID),
				Source: "smartrecruiters",
			})
		}
	case "workable":
		jobs, err := fetchWorkableJobs(atsSlug)
		if err != nil {
			return nil, err
		}
		for _, j := range jobs {
			loc := strings.TrimSpace(strings.Trim(j.Location.City+", "+j.Location.Country, ", "))
			url := j.URL
			if url == "" && j.Shortcode != "" {
				url = fmt.Sprintf("https://apply.workable.com/%s/j/%s/", atsSlug, j.Shortcode)
			}
			rows = append(rows, models.Job{
				CompanyID: companyID, Title: j.Title, Department: j.Department,
				Location: loc, URL: url, Source: "workable",
			})
		}
	case "darwinbox":
		jobs, err := fetchDarwinboxJobs(atsSlug)
		if err != nil {
			return nil, err
		}
		for _, j := range jobs {
			loc := j.Locations
			if len(j.OfficeLocs) > 0 && j.OfficeLocs[0] != "" {
				loc = j.OfficeLocs[0]
			}
			rows = append(rows, models.Job{
				CompanyID: companyID, Title: j.Title, Department: j.DepartmentName,
				Location: strings.TrimSpace(loc),
				URL: fmt.Sprintf("https://%s.darwinbox.in/ms/candidatev2/main/careers/jobDetails/%s",
					atsSlug, j.ID),
				Source: "darwinbox",
			})
		}
	case "keka":
		jobs, err := fetchKekaJobs(atsSlug)
		if err != nil {
			return nil, err
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
				CompanyID: companyID, Title: j.Title, Department: j.DepartmentName,
				Location: strings.Join(locs, ", "),
				URL:      fmt.Sprintf("https://%s.keka.com/careers/jobdetails/%d", atsSlug, j.ID),
				Source:   "keka",
			})
		}
	case "workday":
		jobs, err := fetchWorkdayJobs(atsSlug)
		if err != nil {
			return nil, err
		}
		parts := strings.Split(atsSlug, ":")
		if len(parts) != 3 {
			return nil, fmt.Errorf("bad workday slug %q", atsSlug)
		}
		tenant, region, site := parts[0], parts[1], parts[2]
		for _, j := range jobs {
			rows = append(rows, models.Job{
				CompanyID: companyID, Title: j.Title, Location: j.LocationsText,
				URL: fmt.Sprintf("https://%s.%s.myworkdayjobs.com/en-US/%s%s",
					tenant, region, site, j.ExternalPath),
				Source: "workday",
			})
		}
	default:
		// Without this the switch falls through to `return rows, nil` with
		// rows still nil, and the caller cannot tell "this board has no open
		// roles" from "nobody here knows how to read this board" — so it
		// deletes every stored role for the company. An unreadable provider
		// is an error, not an empty board.
		return nil, fmt.Errorf("unknown ATS provider %q", atsType)
	}

	return rows, nil
}

// applySyncedJobs writes one sync's result to the jobs table, with a single
// guard: an empty result does not clear a company's listings the first time
// it happens.
//
// Every read feeding this can fail in a way that looks exactly like success.
// An ATS returns 200 with an empty array while it is being reconfigured; the
// careers-page link scan depends on markup and reads a redesign as zero. The
// old behaviour deleted every role on the strength of one such read, so a
// company that is plainly hiring showed as having nothing — and reappeared an
// hour later, which is worse than either state on its own.
//
// A second consecutive empty read is the company actually saying it has
// nothing open, and that is when the listings go.
func applySyncedJobs(company models.Company, rows []models.Job) (int, error) {
	if len(rows) == 0 && company.EmptyJobReads == 0 {
		var existing int64
		config.DB.Model(&models.Job{}).Where("company_id = ?", company.ID).Count(&existing)
		if existing > 0 {
			config.DB.Model(&models.Company{}).Where("id = ?", company.ID).
				Update("empty_job_reads", 1)
			log.Printf("job sync: %s read empty but has %d stored roles — keeping them until a second empty read",
				company.Name, existing)
			return int(existing), nil
		}
	}

	n, err := replaceJobsForCompany(company.ID, rows)
	if err != nil {
		return 0, err
	}
	// Either a read succeeded, or the second empty read has just cleared the
	// company. Both leave the counter at zero.
	if company.EmptyJobReads != 0 {
		config.DB.Model(&models.Company{}).Where("id = ?", company.ID).
			Update("empty_job_reads", 0)
	}
	return n, nil
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
// extractRolesFreely reads a careers page without spending a scraper credit
// and returns whatever roles it can link to, plus the page they came from —
// which may not be the page we started on, because plenty of /careers pages
// are marketing pages whose only job is to link to the real listing.
//
// Three passes, stopping at the first that finds anything:
//
//  1. Plain HTTP fetch of the careers page.
//  2. The page it links to as its listing ("View open positions").
//  3. A rendered read (Jina) for pages that build their list in the browser.
func extractRolesFreely(company models.Company, pageURL string) ([]ExtractedJob, string) {
	page, err := fetchText(pageURL)
	if err == nil {
		if jobs := extractJobsFromPageText(page, pageURL); len(jobs) > 0 {
			return jobs, pageURL
		}
		if next := findJobsListingLink(page, pageURL); next != "" {
			if listing, lerr := fetchText(next); lerr == nil {
				if jobs := extractJobsFromPageText(listing, next); len(jobs) > 0 {
					log.Printf("careers page for %s: roles found on linked listing %s", company.Name, next)
					return jobs, next
				}
			}
		}
	}

	rendered, provider, rerr := FetchRenderedPage(pageURL)
	if rerr != nil {
		return nil, pageURL
	}
	if jobs := extractJobsFromPageText(rendered, pageURL); len(jobs) > 0 {
		log.Printf("careers page for %s: %d roles found via %s", company.Name, len(jobs), provider)
		return jobs, pageURL
	}

	// The same "View open positions" hop the plain path takes, on the
	// rendered copy. Without it a company whose careers page is BOTH
	// client-rendered AND a marketing page that links elsewhere — which is
	// the ordinary shape for a company big enough to have a marketing team —
	// falls through to the paid extraction, or to nothing. The link is only
	// visible after rendering, so the earlier hop never saw it.
	if next := findJobsListingLink(rendered, pageURL); next != "" {
		if listing, lerr := fetchText(next); lerr == nil {
			if jobs := extractJobsFromPageText(listing, next); len(jobs) > 0 {
				log.Printf("careers page for %s: roles found on listing %s linked from the %s render",
					company.Name, next, provider)
				return jobs, next
			}
		}
	}

	return nil, pageURL
}

func syncJobsFromCareersPage(company models.Company) (int, error) {
	pageURL := company.CareersURL
	if pageURL == "" {
		return 0, nil // nothing to read
	}

	// The page has to be a careers page, not a careers *article*. This is
	// where the 295 professions came from: a company linked "career options
	// after 12th" and the extraction read the whole alphabetical list. The
	// link scan below is, if anything, more willing to believe such a page,
	// because an article links every profession it names — so the check that
	// used to sit only on the links we follow now also sits on the page we
	// start from.
	if guidancePageRe.MatchString(pageURL) {
		log.Printf("careers page for %s: %s reads as a guidance article, not a listing — skipping",
			company.Name, pageURL)
		return 0, nil
	}

	// Free first, and in the order the page is cheapest to read.
	//
	// This used to open with the Firecrawl extraction, which made a paid
	// service the only way any company without a supported ATS could show a
	// single role. When the key was unset or the month's budget was spent,
	// this function returned an error for every such company — the majority
	// of the directory — and the Job Map read as empty even though the
	// pipeline was working. The paid call is now the last thing tried, not
	// the first.
	extracted, pageURL := extractRolesFreely(company, pageURL)

	// Last resort: Firecrawl's LLM extraction, for pages whose roles are not
	// expressed as links at all.
	if len(extracted) == 0 {
		var err error
		if extracted, err = ExtractJobsFromCareersPage(pageURL); err != nil {
			// Not fatal, and not worth an error either: the free passes above
			// already had their say. Log and leave the company at zero.
			log.Printf("careers page for %s: firecrawl extraction unavailable (%v)", company.Name, err)
			return 0, nil
		}
	}
	if len(extracted) > 0 {
		log.Printf("careers page for %s: %d roles", company.Name, len(extracted))
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

	if !careersPageResultLooksReal(rows, company.Name, pageURL) {
		return 0, nil
	}
	return applySyncedJobs(company, rows)
}

// jobLinkPathRe matches the URL shape of one job posting. Careers pages vary
// wildly in markup but converge on their links: a role's own page lives
// under /jobs/, /careers/, /openings/ or /positions/.
var jobLinkPathRe = regexp.MustCompile(`(?i)/(?:job|opening|position|role|vacanc|career|opportunit)[a-z]*[/-][^/\s]`)

// Links come in two shapes depending on how the page was read: Jina returns
// markdown, a plain fetch returns HTML.
var (
	markdownLinkRe = regexp.MustCompile(`\[([^\]\n]{2,120})\]\(\s*<?(https?://[^)>\s]+|/[^)>\s]+)>?\s*\)`)
	anchorRe       = regexp.MustCompile(`(?is)<a[^>]+href=["']([^"']+)["'][^>]*>(.*?)</a>`)
	htmlTagRe      = regexp.MustCompile(`(?s)<[^>]+>`)
	whitespaceRe   = regexp.MustCompile(`\s+`)
	hasLetterRe    = regexp.MustCompile(`\p{L}`)
)

// nonRoleTitles are link texts that sit on every careers page and are never
// a job. Matched as substrings of the lowercased title.
var nonRoleTitles = []string{
	"life at", "culture", "benefit", "perks", "about us", "our story", "our team",
	"blog", "press", "news", "privacy", "terms", "cookie", "login", "log in",
	"sign in", "sign up", "contact", "home", "read more", "learn more",
	"view all", "see all", "browse", "search", "filter", "back to", "faq",
	"diversity", "equal opportunity", "linkedin", "twitter", "instagram",
	"facebook", "youtube", "apply now", "join us", "job alert", "share this",
	"next page", "previous", "load more", "subscribe", "newsletter",
}

// extractJobsFromPageText pulls roles out of a careers page by following its
// own links, with no LLM and no scraper credit.
//
// The premise is the same one the rest of this pipeline rests on: a careers
// page has to link each role to that role's own page, because a visitor who
// is not logged in has to be able to click it. Those links are in the markup
// whether the page was read as HTML or rendered to markdown by Jina, so both
// shapes are scanned.
//
// This is deliberately a link scan and not a text scan. A page's prose
// mentions plenty of job titles that are not openings; only the links point
// at postings. That distinction is also the guard: rows produced here each
// carry a distinct posting URL, which is the evidence careersPageResultLooksReal
// weighs when deciding whether a result is a listing or an article.
// pageLink is one link found on a page, whichever markup the page arrived in.
type pageLink struct{ title, href string }

// linkCandidates pulls every link out of a page as (text, href) pairs.
//
// A page reaches us in one of two shapes and both carry links that matter: a
// plain fetch gives HTML anchors, and a rendered read through Jina gives
// markdown. Reading only one of them is how the rendered path came to be
// half-blind — it could find roles but not the "View open positions" link
// that leads to them, which is the shape most large careers pages take.
func linkCandidates(content string) []pageLink {
	var out []pageLink

	for _, m := range markdownLinkRe.FindAllStringSubmatch(content, -1) {
		out = append(out, pageLink{title: m[1], href: m[2]})
	}
	for _, m := range anchorRe.FindAllStringSubmatch(content, -1) {
		out = append(out, pageLink{title: htmlTagRe.ReplaceAllString(m[2], " "), href: m[1]})
	}

	// Both shapes carry HTML entities, and both matter. An href written as
	// ?dept=Sales&amp;loc=IN is stored verbatim otherwise, and the saved
	// posting URL then points at a page that does not exist. The link text
	// has the same problem: "Sales &amp; Marketing" is not a job title.
	for i := range out {
		out[i].title = html.UnescapeString(out[i].title)
		out[i].href = html.UnescapeString(out[i].href)
	}
	return out
}

func extractJobsFromPageText(content, pageURL string) []ExtractedJob {
	base, err := url.Parse(pageURL)
	if err != nil {
		return nil
	}

	candidates := linkCandidates(content)

	var out []ExtractedJob
	// Deduped on the posting URL alone, which is what identifies a job
	// everywhere else in this system: replaceJobsForCompany dedupes on it and
	// the jobs table is uniquely indexed on (company_id, url).
	//
	// It used to also drop a second role with the same title, which reads as
	// tidying and is actually data loss: "Software Engineer" in Bangalore and
	// "Software Engineer" in Pune are two openings with two postings, and a
	// board of any size lists several roles under one title. Only one of them
	// survived.
	seenURL := map[string]bool{}

	for _, c := range candidates {
		title := cleanRoleTitle(c.title)
		if title == "" {
			continue
		}

		href := strings.TrimSpace(c.href)
		if href == "" || strings.HasPrefix(href, "#") ||
			strings.HasPrefix(href, "mailto:") || strings.HasPrefix(href, "javascript:") {
			continue
		}
		ref, err := url.Parse(href)
		if err != nil {
			continue
		}
		abs := base.ResolveReference(ref)
		absStr := abs.String()

		// Only links that look like one posting, and never a link back to the
		// listing page itself.
		if !jobLinkPathRe.MatchString(abs.Path) || absStr == pageURL {
			continue
		}
		// The guidance-article guard from the LLM path applies here too: an
		// education site's "career options" article is reachable by exactly
		// this kind of link.
		if guidancePageRe.MatchString(absStr) {
			continue
		}
		if seenURL[absStr] {
			continue
		}
		seenURL[absStr] = true

		out = append(out, ExtractedJob{Title: title, URL: absStr})
	}

	return out
}

// departmentTitles are what a careers page calls its *sections*. As the whole
// text of a link they point at a department landing page, not at a role —
// "Engineering" links to /careers/engineering, which the posting-URL pattern
// happily matches.
//
// Matched on the complete title and never as a substring, because every one
// of these words also appears inside real job titles: "Engineering Manager"
// and "Head of Design" are roles, "Engineering" and "Design" are not.
var departmentTitles = map[string]bool{
	"engineering": true, "sales": true, "design": true, "marketing": true,
	"operations": true, "product": true, "finance": true, "legal": true,
	"people": true, "human resources": true, "hr": true, "data": true,
	"technology": true, "tech": true, "business": true, "corporate": true,
	"support": true, "customer success": true, "locations": true,
	"departments": true, "teams": true, "team": true, "students": true,
	"internships": true, "interns": true, "graduates": true, "leadership": true,
	"all jobs": true, "all roles": true, "open roles": true, "openings": true,
	"vacancies": true, "opportunities": true, "positions": true,
}

// cleanRoleTitle normalises a link's text and returns "" if it cannot be a
// job title.
func cleanRoleTitle(raw string) string {
	title := whitespaceRe.ReplaceAllString(strings.TrimSpace(raw), " ")
	title = strings.Trim(title, "*_#|-–— ")
	if len(title) < 3 || len(title) > 120 || !hasLetterRe.MatchString(title) {
		return ""
	}
	lower := strings.ToLower(title)
	if departmentTitles[lower] {
		return ""
	}
	for _, bad := range nonRoleTitles {
		if strings.Contains(lower, bad) {
			return ""
		}
	}
	return title
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
func careersPageResultLooksReal(rows []models.Job, companyName, pageURL string) bool {
	if len(rows) == 0 {
		return true // nothing to store, nothing to doubt
	}

	if len(rows) > maxCareersPageRoles {
		log.Printf("careers page for %s returned %d roles — above the sane ceiling, discarding",
			companyName, len(rows))
		return false
	}

	// A real listing leaves evidence per role: either metadata (a location or
	// a department) or a link to that role's own posting.
	//
	// The link counts because of what the 295-profession case actually looked
	// like — rows read out of an article's prose, every one of them falling
	// back to the listing URL with a fragment appended because there was no
	// posting to point at. A row with its own posting URL came from a link
	// the company put on the page, which is the same evidence the ATS
	// detection trusts.
	fallbackPrefix := pageURL + "#"
	withEvidence := 0
	for _, r := range rows {
		linked := r.URL != "" && !strings.HasPrefix(r.URL, fallbackPrefix)
		if r.Location != "" || r.Department != "" || linked {
			withEvidence++
		}
	}
	if len(rows) >= 5 && withEvidence == 0 {
		log.Printf("careers page for %s returned %d roles with no location, department or posting link at all — discarding",
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
	//
	// Anything else the agent hands us is checked before it is trusted. It
	// used to be taken as given, and the cost of that showed up downstream:
	// of the companies sitting at zero roles, a third pointed at a careers
	// page that 404s or no longer resolves at all. Every sync then rendered
	// that dead page and asked a model to find jobs on it — a scraping
	// credit and an LLM call spent to be told nothing is there.
	if current != "" && strings.TrimRight(current, "/") != strings.TrimRight(site, "/") {
		if !isAggregatorURL(current) && urlIsAlive(current) {
			return current
		}
		// Fall through to the guesses rather than returning a dead URL.
		current = ""
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
// careersAggregatorHosts are job boards and directories. A careers URL
// pointing at one is not the company's own page: the agent answered with
// where the company's jobs are *listed by someone else*, which we cannot
// parse and should not store as if it were their board.
var careersAggregatorHosts = []string{
	"naukri.com", "linkedin.com", "indeed.com", "glassdoor.co",
	"monsterindia.com", "shine.com", "timesjobs.com", "foundit.in",
	"wellfound.com", "angel.co", "internshala.com", "ambitionbox.com",
	"hirist.tech", "cutshort.io", "instahyre.com",
}

func isAggregatorURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	host := strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www.")
	for _, a := range careersAggregatorHosts {
		if host == a || strings.HasSuffix(host, "."+a) {
			return true
		}
	}
	return false
}

// urlIsAlive answers only whether the page exists, which is a different
// question from whether it advertises jobs and has to stay separate from it.
//
// Most careers pages worth keeping build their listings in the browser, so
// their served HTML contains no hiring words at all — judging them by
// content would throw away perfectly good URLs and send us guessing at
// /careers paths that are worse than the one the agent found. What we can
// decide from a plain fetch is whether the URL resolves. A 404 or a dead
// host is a fact; an empty-looking page is not.
func urlIsAlive(u string) bool {
	_, err := fetchText(u)
	return err == nil
}

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

// listingLinkTextRe matches the words a link uses when it leads to the actual
// job listings. Many /careers pages are marketing pages that link out to the
// real board — without following that link those companies look empty.
//
// Matched against a link's text rather than against raw HTML, so it works the
// same on a plain fetch and on Jina's markdown.
var listingLinkTextRe = regexp.MustCompile(`(?i)view\s+(?:all\s+)?(?:open|job|position|role)|current\s+opening|open\s+position|open\s+role|see\s+(?:all\s+)?job|explore\s+(?:open\s+)?role|browse\s+job|all\s+opening|job\s+opening`)

// guidancePageRe matches URLs that are career *advice* rather than career
// *openings* — a distinction that cost us 295 fake job rows once.
var guidancePageRe = regexp.MustCompile(`(?i)career-(option|guide|advice|counsel|path|choice)|/blog/|/article|after-12th|course`)

// findJobsListingLink looks for a link from a careers page to the page that
// actually lists roles. Returns an absolute URL, or "" if none found.
func findJobsListingLink(content, baseURL string) string {
	base, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}
	for _, link := range linkCandidates(content) {
		if len(link.title) > 120 || !listingLinkTextRe.MatchString(link.title) {
			continue
		}
		href := strings.TrimSpace(link.href)
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

	// Sync companies in parallel, but bounded.
	//
	// This loop used to be strictly sequential, with several network calls
	// per company. At 67 companies that finishes inside the hour; at a few
	// thousand it does not, and cron simply starts the next tick on top of
	// the one still running. A small pool keeps the tick well inside its
	// window without turning us into a thundering herd against the ATS APIs.
	const syncConcurrency = 8

	var (
		mu               sync.Mutex
		synced, detected int
		sem              = make(chan struct{}, syncConcurrency)
		wg               sync.WaitGroup
	)

	for _, c := range companies {
		wg.Add(1)
		sem <- struct{}{}
		go func(c models.Company) {
			defer wg.Done()
			defer func() { <-sem }()
			// Gin's Recovery() does not cover goroutines we spawn: a panic in
			// any one company's parse would otherwise kill the process.
			defer func() {
				if r := recover(); r != nil {
					log.Printf("PANIC syncing jobs for %s: %v", c.Name, r)
				}
			}()

			n, err := SyncJobsForCompany(c)
			if err != nil {
				// Non-fatal: one company's board being briefly unreachable
				// shouldn't stop the rest of the sync.
				return
			}
			if n > 0 {
				mu.Lock()
				synced += n
				if c.ATSType == "" {
					detected++ // had no ATS before this run
				}
				mu.Unlock()
			}
		}(c)
	}
	wg.Wait()
	log.Printf("job sync: %d companies checked, %d newly detected, %d open roles total | scrape usage this month: %v",
		len(companies), detected, synced, ScrapeUsageSummary())
}
