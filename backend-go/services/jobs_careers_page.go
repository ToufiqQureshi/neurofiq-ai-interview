package services

import (
	"context"
	"html"
	"log"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/config"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/models"
)

// The fallback for companies with no board this pipeline can read.
//
// Their careers page must load its listings from somewhere public, because
// visitors are not logged in — so this reads the page, scans it for links
// that look like postings, and only pays for a rendered read or an LLM
// extraction when the free tiers come back empty.
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
			Source:     careersPageSource,
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
	SyncCompanyJobsBatch(context.Background(), jobSyncBatchSize)
}

// jobSyncBatchSize caps how many companies one sync tick refreshes.
//
// The sync used to load the whole directory and check all of it. That is a
// fixed hour of work against a growing table, and the tick that stops fitting
// inside its window does not fail — it overruns, and cron starts the next tick
// on top of it. A bounded batch ordered by staleness turns the same work into
// a rotation: every company is reached, the oldest first, and the tick always
// ends. At this size the directory turns over roughly daily however large it
// gets, which is what the listings actually need.
const jobSyncBatchSize = 1200

// jobSyncBudget is the wall clock one tick may spend, comfortably inside the
// hourly schedule. The batch size bounds the work; this bounds the time, and
// they are not the same thing — a batch full of throttled hosts takes far
// longer per company than one that answers.
const jobSyncBudget = 45 * time.Minute

// SyncCompanyJobsBatch refreshes the companies whose roles are stalest.
func SyncCompanyJobsBatch(ctx context.Context, batch int) {
	ctx, cancel := context.WithTimeout(ctx, jobSyncBudget)
	defer cancel()

	var companies []models.Company
	// NULLS FIRST so a company that has never been synced — one the harvest
	// stored a minute ago — is refreshed before one that was synced an hour
	// ago, rather than last.
	if err := config.DB.
		Order("last_synced_at ASC NULLS FIRST").
		Limit(batch).
		Find(&companies).Error; err != nil {
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
		if ctx.Err() != nil {
			// Out of budget. The companies not reached keep their old
			// last_synced_at, so they are first in line next tick — the
			// rotation continues rather than restarting.
			break
		}
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
				//
				// A transient failure deliberately does NOT stamp
				// last_synced_at: leaving it stale is what brings this
				// company back to the front of the next rotation instead of
				// sending it to the back of a queue it never got served in.
				if !IsTransientFetchError(err) {
					touchSynced(c.ID)
				}
				return
			}
			if n == 0 {
				// A clean read that found nothing still counts as reached.
				// replaceJobsForCompany stamps the column whenever it writes,
				// but a company skipped by its ATS re-check cooldown never
				// gets there, and without this it would be permanently first
				// in line and crowd out everything else.
				touchSynced(c.ID)
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
	for _, h := range ThrottledHosts() {
		log.Printf("job sync: %s is pacing us at %s after %d strikes", h.Host, h.Interval, h.Strikes)
	}
}

// touchSynced records that a company had its turn in the rotation, without
// claiming anything about what was found.
func touchSynced(companyID string) {
	config.DB.Model(&models.Company{}).
		Where("id = ?", companyID).
		Update("last_synced_at", time.Now())
}

// JobWithCompany joins a single Job with the parent Company metadata for global search and discovery feeds.
type JobWithCompany struct {
	models.Job
	CompanyName   string `json:"company_name"`
	CompanyDomain string `json:"company_domain"`
	CompanyLogo   string `json:"company_logo"`
	CompanySector string `json:"company_sector"`
	CompanyStage  string `json:"company_stage"`
	CompanyArea   string `json:"company_area"`
	ATSType       string `json:"ats_provider"`
	Field         string `json:"field"`
	Level         string `json:"level"`
}

// TechFields and NonTechFields split job_facets.go's FieldBuckets into the two
// groups the jobs portal's scope toggle filters on.
var (
	TechFields    = []string{"Engineering", "Data & AI", "Product", "Design"}
	NonTechFields = []string{"Sales & Marketing", "Operations", "Other"}
)

// ListGlobalJobs searches across all active jobs in the system with keyword, location, role field, level, work type, and tech/non-tech scope filters.
func ListGlobalJobs(q, location, field, level, workType, scope string, page, pageSize int) ([]JobWithCompany, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 30
	}

	db := config.DB.Table("jobs").
		Select("jobs.*, companies.name as company_name, companies.domain as company_domain, companies.sector as company_sector, companies.stage as company_stage, companies.area as company_area, companies.ats_type as ats_type").
		Joins("JOIN companies ON companies.id = jobs.company_id")

	if q != "" {
		trimmedQ := strings.TrimSpace(q)
		lowerQ := strings.ToLower(trimmedQ)
		switch lowerQ {
		case "golang", "go":
			db = db.Where("jobs.title ILIKE ? OR jobs.title ILIKE ? OR jobs.title ILIKE ?", "%golang%", "%backend%", "%engineer%")
		case "ai / ml", "ai", "ml":
			db = db.Where("jobs.title ILIKE ? OR jobs.title ILIKE ? OR jobs.title ILIKE ? OR jobs.department ILIKE ?", "%ai%", "%ml%", "%machine learning%", "%data%")
		default:
			db = db.Where("jobs.title ILIKE ? OR companies.name ILIKE ? OR jobs.department ILIKE ?", "%"+trimmedQ+"%", "%"+trimmedQ+"%", "%"+trimmedQ+"%")
		}
	}
	if location != "" {
		db = db.Where("jobs.location ILIKE ? OR companies.area ILIKE ?", "%"+location+"%", "%"+location+"%")
	}
	if field != "" && field != "All Tech Roles" {
		fieldTerm := strings.ToLower(field)
		if strings.Contains(fieldTerm, "ai") || strings.Contains(fieldTerm, "data") {
			db = db.Where("jobs.title ILIKE ? OR jobs.title ILIKE ? OR jobs.title ILIKE ? OR jobs.department ILIKE ?", "%data%", "%ai%", "%machine learning%", "%analytics%")
		} else if strings.Contains(fieldTerm, "backend") {
			db = db.Where("jobs.title ILIKE ? OR jobs.department ILIKE ?", "%backend%", "%backend%")
		} else if strings.Contains(fieldTerm, "frontend") || strings.Contains(fieldTerm, "fullstack") {
			db = db.Where("jobs.title ILIKE ? OR jobs.title ILIKE ? OR jobs.department ILIKE ?", "%frontend%", "%fullstack%", "%web%")
		} else if strings.Contains(fieldTerm, "devops") {
			db = db.Where("jobs.title ILIKE ? OR jobs.title ILIKE ? OR jobs.department ILIKE ?", "%devops%", "%sre%", "%infrastructure%")
		} else if strings.Contains(fieldTerm, "mobile") {
			db = db.Where("jobs.title ILIKE ? OR jobs.title ILIKE ? OR jobs.department ILIKE ?", "%mobile%", "%android%", "%ios%")
		} else {
			db = db.Where("jobs.title ILIKE ? OR jobs.department ILIKE ?", "%"+field+"%", "%"+field+"%")
		}
	}

	if scope == "tech" {
		db = db.Where("COALESCE(NULLIF(jobs.field, ''), 'Other') IN ?", TechFields)
	} else if scope == "non-tech" {
		db = db.Where("COALESCE(NULLIF(jobs.field, ''), 'Other') IN ?", NonTechFields)
	}
	if level != "" {
		db = db.Where("COALESCE(NULLIF(jobs.level, ''), 'Unspecified') = ?", level)
	}
	if workType == "Remote" {
		db = db.Where("jobs.location ILIKE ?", "%remote%")
	} else if workType == "On-site" {
		db = db.Where("jobs.location NOT ILIKE ? OR jobs.location IS NULL", "%remote%")
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var results []JobWithCompany
	offset := (page - 1) * pageSize
	if err := db.Order("jobs.created_at DESC").Limit(pageSize).Offset(offset).Scan(&results).Error; err != nil {
		return nil, 0, err
	}

	for i := range results {
		results[i].Field = ClassifyField(results[i].Title, results[i].Department)
		results[i].Level = ClassifyLevel(results[i].Title)
	}

	return results, total, nil
}
