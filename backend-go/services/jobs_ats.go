package services

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/models"
)

// The ATS clients: how each board platform answers, and how a company's
// board is found in the first place.
//
// Every one of these is an official public JSON endpoint — the same one the
// company's own careers page calls to render itself — so reading a board
// costs nothing and needs no key. Split out of a single 2,076-line file;
// same package, so nothing here changed but the name on the door.

// Patterns for spotting an embedded ATS job board in a company's own
// careers page HTML. Companies embed these themselves — that link is how
// their careers page renders its listings — so finding it is far more
// reliable than guessing a board slug from the company name.
var (
	// Greenhouse serves regional boards too (e.g. job-boards.eu.greenhouse.io),
	// so allow an optional region segment before greenhouse.io.
	// The embed form is listed first because the alternation is ordered and the
	// plain path would otherwise match it, returning the literal segment
	// "embed" as the slug.
	//
	// A company that embeds its board rather than linking it serves
	// boards.greenhouse.io/embed/job_board?for=observeai — the company is in
	// the query string. Without this, every embedding company collapsed to the
	// same inadmissible slug, so DetectATS could not read an embedded board off
	// a careers page at all and the company was silently left at zero roles.
	// Observe.ai is one: its careers page embeds rather than links.
	greenhouseLinkRe      = regexp.MustCompile(`(?:boards|job-boards)\.(?:[a-z]{2}\.)?greenhouse\.io/(?:embed/job_board(?:/js)?\?for=([a-zA-Z0-9_-]+)|([a-zA-Z0-9_-]+))`)
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

	recruiteeLinkRe = regexp.MustCompile(`([a-zA-Z0-9-]+)\.recruitee\.com`)
	freshteamLinkRe = regexp.MustCompile(`([a-zA-Z0-9-]+)\.freshteam\.com`)
	// Personio hosts on both .de and .com, and which one a given tenant uses
	// is not guessable — so the whole host is captured and stored as the
	// slug (e.g. "gus-germany.jobs.personio.de"), not just the subdomain.
	personioLinkRe = regexp.MustCompile(`([a-zA-Z0-9-]+\.jobs\.personio\.(?:de|com))`)
	// Gem's board lives at a path, not a subdomain — jobs.gem.com/<boardId> —
	// so this also matches the platform's own reserved paths (jobs.gem.com/api/...).
	// scanForATS rejects those explicitly, the same way it rejects Workday
	// candidates that don't pan out.
	gemLinkRe = regexp.MustCompile(`jobs\.gem\.com/([a-zA-Z0-9_-]+)`)
)

// gemReservedPaths are jobs.gem.com paths that are not a company's board —
// caught here because, unlike every other provider here, Gem's board slug is
// a path segment rather than a subdomain, so it can collide with the
// platform's own routes.
var gemReservedPaths = map[string]bool{
	"api": true, "assets": true, "static": true, "login": true, "signup": true,
}

// atsRecheckInterval is how long a company with a known board is left alone
// before we scan it again for fresh roles.
const atsRecheckInterval = 1 * time.Hour

// atsRetryInterval is the shorter wait for a company we could not find a
// board for. The board may not exist, or our heuristics may have missed it.
const atsRetryInterval = 15 * time.Minute

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

type recruiteeOffer struct {
	Title      string `json:"title"`
	Department string `json:"department"`
	City       string `json:"city"`
	Country    string `json:"country"`
	// CareersURL is the offer's own public posting page — Recruitee includes
	// it directly, so there is nothing to construct.
	CareersURL string `json:"careers_url"`
}

type recruiteeResponse struct {
	Offers []recruiteeOffer `json:"offers"`
}

// personioPosition is one <position> in Personio's public XML feed
// (workzag-jobs/position — "workzag" predates the Personio rebrand and the
// feed schema was never renamed). No posting URL: it's built from the
// tenant host plus the id, same shape every live Personio board uses
// (https://{tenant}/job/{id}).
type personioPosition struct {
	ID         string `xml:"id"`
	Name       string `xml:"name"`
	Office     string `xml:"office"`
	Department string `xml:"department"`
}

type personioResponse struct {
	Positions []personioPosition `xml:"position"`
}

// gemJobPosting is one posting from Gem's public Job Board API — a GraphQL
// endpoint, not REST, so the shape here is the subset of JobBoardList's
// response this pipeline actually uses. See fetchGemJobs for the query.
type gemJobPosting struct {
	Title     string `json:"title"`
	ExtID     string `json:"extId"`
	Locations []struct {
		Name string `json:"name"`
	} `json:"locations"`
	Job struct {
		Department struct {
			Name string `json:"name"`
		} `json:"department"`
	} `json:"job"`
}

type gemGraphQLResponse struct {
	Data struct {
		OatsExternalJobPostings struct {
			JobPostings []gemJobPosting `json:"jobPostings"`
		} `json:"oatsExternalJobPostings"`
	} `json:"data"`
}

// scanForATS looks for an embedded ATS job-board link in page content and
// returns the provider and its board slug.
// firstGroup returns the first non-empty capture group of a match.
//
// A pattern that accepts more than one URL shape carries one group per shape
// and fills exactly one of them.
func firstGroup(m []string) string {
	for _, g := range m[1:] {
		if g != "" {
			return g
		}
	}
	return ""
}

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
		{"recruitee", recruiteeLinkRe},
		{"freshteam", freshteamLinkRe},
		{"personio", personioLinkRe},
	} {
		if m := p.re.FindStringSubmatch(content); m != nil {
			// First non-empty group, not group 1. A pattern that has to accept
			// two URL shapes carries two groups and only one of them fills:
			// Greenhouse's board is linked as /<slug> or embedded as
			// /embed/job_board?for=<slug>, and reading group 1 unconditionally
			// returned "" for whichever shape did not match.
			if slug := firstGroup(m); slug != "" {
				return p.name, slug
			}
		}
	}

	// Gem's board slug is a path segment (jobs.gem.com/<boardId>), not a
	// subdomain, so a match against the platform's own reserved paths is
	// rejected here rather than trusted like every other provider above.
	if m := gemLinkRe.FindStringSubmatch(content); m != nil {
		if slug := m[1]; slug != "" && !gemReservedPaths[strings.ToLower(slug)] {
			return "gem", slug
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
	if resp.StatusCode != http.StatusOK {
		return nil, &HTTPStatusError{Status: resp.StatusCode, URL: "greenhouse/" + slug}
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
	if resp.StatusCode != http.StatusOK {
		return nil, &HTTPStatusError{Status: resp.StatusCode, URL: "lever/" + slug}
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
	if resp.StatusCode != http.StatusOK {
		return nil, &HTTPStatusError{Status: resp.StatusCode, URL: "ashby/" + slug}
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
	if resp.StatusCode != http.StatusOK {
		return nil, &HTTPStatusError{Status: resp.StatusCode, URL: "smartrecruiters/" + slug}
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

	resp, err := SafeExternalDo(context.Background(), req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &HTTPStatusError{Status: resp.StatusCode, URL: "darwinbox/" + slug}
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
	if resp.StatusCode != http.StatusOK {
		return nil, &HTTPStatusError{Status: resp.StatusCode, URL: "keka/" + slug}
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
	if resp.StatusCode != http.StatusOK {
		return nil, &HTTPStatusError{Status: resp.StatusCode, URL: "workable/" + slug}
	}
	var parsed workableResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	return parsed.Jobs, nil
}

// fetchRecruiteeJobs reads a company's Careers Site API — public JSON, no
// key, documented at docs.recruitee.com/reference/intro-to-careers-site-api.
func fetchRecruiteeJobs(slug string) ([]recruiteeOffer, error) {
	if !validATSSlug(slug) {
		return nil, fmt.Errorf("invalid recruitee slug %q", slug)
	}
	resp, err := atsGet("https://" + slug + ".recruitee.com/api/offers/")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &HTTPStatusError{Status: resp.StatusCode, URL: "recruitee/" + slug}
	}
	var parsed recruiteeResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	return parsed.Offers, nil
}

// fetchPersonioJobs reads a tenant's public XML feed. slug is stored as the
// full host (e.g. "gus-germany.jobs.personio.de") — see personioLinkRe —
// because Personio splits tenants across .de and .com with no way to tell
// which a given one uses except by having already matched it.
func fetchPersonioJobs(slug string) ([]personioPosition, error) {
	if !validATSSlug(slug) {
		return nil, fmt.Errorf("invalid personio slug %q", slug)
	}
	resp, err := atsGet("https://" + slug + "/xml")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &HTTPStatusError{Status: resp.StatusCode, URL: "personio/" + slug}
	}
	var parsed personioResponse
	if err := xml.NewDecoder(io.LimitReader(resp.Body, maxATSResponseBytes)).Decode(&parsed); err != nil {
		return nil, err
	}
	return parsed.Positions, nil
}

// freshteamJobRe reads a job card straight off the server-rendered careers
// page — there is no API, public or otherwise (confirmed against a live
// board: the page ships every listing in the initial HTML, data-portal-*
// attributes and all, so a plain fetch sees exactly what a browser does).
var freshteamJobRe = regexp.MustCompile(`(?is)<a href="(/jobs/[a-zA-Z0-9_-]+/[a-zA-Z0-9_-]+)"[^>]*data-portal-location="([^"]*)"[^>]*>.*?<div class="job-title">([^<]*)</div>`)

// freshteamDeptRe finds each department section so its jobs can be
// attributed to it — the department itself is never on the job card, only
// on the <li> that groups its jobs.
//
// The gap before <h5> is `.*?`, not `\s*`: a live board carries an HTML
// comment between <div class="role-title"> and <h5> ("Do not remove
// data-portal-* attributes..."), and a strict whitespace-only gap matched
// the trimmed fixture this was built against but not one single real board —
// every department came back empty. Caught by the live check, not the unit
// test with the fixture that had quietly dropped the comment.
var freshteamDeptRe = regexp.MustCompile(`(?is)<li data-portal-role="[^"]*">.*?<h5>\s*([^<\n]+)`)

// fetchFreshteamJobs reads a company's server-rendered careers page directly.
func fetchFreshteamJobs(slug string) ([]models.Job, error) {
	if !validATSSlug(slug) {
		return nil, fmt.Errorf("invalid freshteam slug %q", slug)
	}
	base := "https://" + slug + ".freshteam.com"
	page, err := fetchText(base + "/jobs")
	if err != nil {
		return nil, err
	}

	// Split on department blocks so each job card can carry its section's
	// name — the same two-pass shape scanForATS uses for Workday, for the
	// same reason: one regex cannot carry both without look-around, which
	// Go's RE2 does not support.
	deptBlocks := regexp.MustCompile(`(?is)<li data-portal-role="[^"]*">`).Split(page, -1)
	deptNames := freshteamDeptRe.FindAllStringSubmatch(page, -1)

	var rows []models.Job
	di := 0
	for _, block := range deptBlocks[1:] { // [0] is everything before the first department
		dept := ""
		if di < len(deptNames) {
			dept = strings.TrimSpace(deptNames[di][1])
		}
		di++
		for _, m := range freshteamJobRe.FindAllStringSubmatch(block, -1) {
			rows = append(rows, models.Job{
				Title:      strings.TrimSpace(html.UnescapeString(m[3])),
				Department: dept,
				Location:   strings.TrimSpace(m[2]),
				URL:        base + m[1],
				Source:     "freshteam",
			})
		}
	}
	return rows, nil
}

// gemJobBoardListQuery is the GraphQL query the board page itself sends —
// captured from a live board page's own request rather than written against
// documentation, since the public API reference doesn't show the full query
// shape. boardId is the same slug the jobs.gem.com/<boardId> URL carries.
const gemJobBoardListQuery = `query JobBoardList($boardId: String!) {
  oatsExternalJobPostings(boardId: $boardId) {
    jobPostings {
      id
      extId
      title
      locations { id name city isoCountry isRemote extId __typename }
      job { id department { id name extId __typename } locationType employmentType __typename }
      __typename
    }
    __typename
  }
  __typename
}`

// fetchGemJobs calls Gem's public Job Board API — a GraphQL endpoint, not
// REST, but public and keyless the same as every other provider here.
func fetchGemJobs(slug string) ([]gemJobPosting, error) {
	if !validATSSlug(slug) {
		return nil, fmt.Errorf("invalid gem slug %q", slug)
	}
	body, err := json.Marshal([]map[string]interface{}{{
		"operationName": "JobBoardList",
		"variables":     map[string]string{"boardId": slug},
		"query":         gemJobBoardListQuery,
	}})
	if err != nil {
		return nil, err
	}
	endpoint := "https://jobs.gem.com/api/public/graphql/batch"
	if err := AllowedPublicURL(endpoint); err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := SafeExternalDo(context.Background(), req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &HTTPStatusError{Status: resp.StatusCode, URL: "gem/" + slug}
	}

	// The endpoint is a GraphQL *batch* — one response object per operation
	// in the request array, in the same order. This request sends one.
	var parsed []gemGraphQLResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxATSResponseBytes)).Decode(&parsed); err != nil {
		return nil, err
	}
	if len(parsed) == 0 {
		return nil, fmt.Errorf("gem %s: empty batch response", slug)
	}
	return parsed[0].Data.OatsExternalJobPostings.JobPostings, nil
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

		resp, err := SafeExternalDo(context.Background(), req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, &HTTPStatusError{Status: resp.StatusCode, URL: "workday/" + slug}
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
