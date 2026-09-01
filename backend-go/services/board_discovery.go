package services

import (
	"fmt"
	"log"
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
//
// Keka, Darwinbox and Workday are here even though their boards live on a
// per-tenant subdomain (acme.keka.com), because the filter matches the parent
// domain. Leaving them out was a real gap for this directory in particular:
// they are the two platforms Indian employers use most, and without them the
// search only ever surfaced companies on the US-favoured platforms.
var boardSearchDomains = []string{
	"boards.greenhouse.io",
	"job-boards.greenhouse.io",
	"jobs.lever.co",
	"jobs.ashbyhq.com",
	"apply.workable.com",
	"careers.smartrecruiters.com",
	"keka.com",
	"darwinbox.in",
	"darwinbox.com",
	"myworkdayjobs.com",
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
// queries.
//
// Three-hourly, not hourly. Discovery is the only part of this pipeline that
// costs a metered call, and the free search allowances are ~1000/month: at
// one search an hour the rotation alone would spend 720 of them before a
// single company was looked up. Job syncing still runs hourly, so listings
// stay just as fresh; it is finding *new* boards that slows down, and a seed
// query that waits three hours costs nothing.
const discoveryIntervalSeconds = 3 * 3600

// DiscoveryLeaseName is the cron lease that keeps two instances from running
// the same discovery tick.
const DiscoveryLeaseName = "discovery-rotation"

// discoveryLeaseTTL spans a full rotation interval, so no second instance can
// repeat the tick this one just ran.
const discoveryLeaseTTL = discoveryIntervalSeconds * time.Second

// jobSyncIntervalSeconds must match the job-sync cron schedule in main.go.
const jobSyncIntervalSeconds = 3600

// jobSyncLeaseTTL spans a full sync interval, for the same reason.
const jobSyncLeaseTTL = jobSyncIntervalSeconds * time.Second

// boardResultsPerQuery is how many search hits one query asks for. Most
// resolve to a handful of distinct boards once duplicates collapse.
const boardResultsPerQuery = 25

// MaxNewCompaniesPerRun is exported so the API can advertise the same ceiling
// it actually enforces. A handler that accepts 25 and returns 5 is a contract
// that lies about itself.
const MaxNewCompaniesPerRun = maxNewCompaniesPerRun

// maxNewCompaniesPerRun caps how many companies one run will store.
//
// Each new company costs a second search to find its website, so an
// uncapped run against a fruitful query could spend 25 searches in one tick
// and a good chunk of the month in an afternoon. The boards this run skips
// are not lost — the rotation comes back around, and a board that exists
// today still exists next week.
const maxNewCompaniesPerRun = 5

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
	// The ATS vendors are themselves on the domains we search, so their own
	// pages come back too: keka.com returns www.keka.com/careers, which
	// scans as the board of a company called "www". And "j" is Workable's
	// per-job share URL (apply.workable.com/j/<code>), which carries no
	// account slug at all.
	"www": true, "j": true, "careers": true, "company": true, "companies": true,
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
	results, err := WebSearch(query, boardSearchDomains, numResults)
	if err != nil {
		log.Printf("board discovery: search failed for %q: %v", query, err)
		return nil
	}

	// scanForATS is free for every provider but Workday, whose job-site id is
	// not in the URL: it probes the live API for up to five candidate ids,
	// and each probe can paginate. One search returns many postings from the
	// same employer, so without memoising, twenty results from one tenant
	// mean twenty identical probes and a cron tick that runs for minutes.
	workdayProbe := map[string]string{}
	scan := func(u string) (string, string) {
		m := workdayLinkRe.FindStringSubmatch(u)
		if m == nil {
			return scanForATS(u)
		}
		key := m[1] + ":" + m[2]
		slug, done := workdayProbe[key]
		if !done {
			_, slug = scanForATS(u)
			workdayProbe[key] = slug
		}
		if slug == "" {
			return "", ""
		}
		return "workday", slug
	}

	seen := map[string]bool{}
	var hits []boardHit
	for _, r := range results {
		// The same regexes that read a board link out of a careers page read
		// it out of a search result, because both are just the URL.
		provider, slug := scan(r.URL)
		if provider == "" || slug == "" || nonSlugSegments[strings.ToLower(slug)] {
			continue
		}
		key := provider + ":" + strings.ToLower(slug)
		if seen[key] {
			continue
		}
		seen[key] = true

		// The canonical board address is the better careers URL — it is the
		// board's front page rather than whichever posting the search
		// happened to return. But a provider we have no canonical form for
		// must not lose the URL entirely: the search result is already a
		// verified page on that board.
		url := boardURL(provider, slug)
		if url == "" {
			url = r.URL
		}
		hits = append(hits, boardHit{
			Provider: provider,
			Slug:     slug,
			Title:    r.Title,
			URL:      url,
		})
	}
	return hits
}

// boardURL is the public, human-facing address of a board — used as the
// company's careers URL, since for these companies it is exactly that.
//
// Returns "" for a provider with no single canonical address; callers fall
// back to the URL the search returned.
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
	case "workday":
		// Stored as "tenant:region:site" — the same three parts the job
		// URLs are built from.
		parts := strings.Split(slug, ":")
		if len(parts) != 3 {
			return ""
		}
		return fmt.Sprintf("https://%s.%s.myworkdayjobs.com/en-US/%s", parts[0], parts[1], parts[2])
	}
	return ""
}

// resolveCompanyWebsite finds a company's own homepage.
//
// The board tells us a company is hiring but not where it lives, and the
// companies table is keyed on domain. One search per newly-seen company, and
// never for one we already have.
func resolveCompanyWebsite(name string) string {
	query := name + " official company website"

	// Exa's company category returns company sites rather than articles about
	// them, so it is the better answer whenever we can have it.
	//
	// But it must not be the ONLY answer. This lookup used to be Exa-only,
	// which quietly made the Tavily fallback useless: the moment Exa's month
	// was spent, discovery still paid Tavily to find boards and then dropped
	// every single company here for having no website. A run that costs a
	// search and stores nothing is worse than a run that does not happen.
	//
	// category=company is a quality win, not a requirement — isAggregatorHost
	// below is what actually rejects a LinkedIn or Crunchbase page, and it
	// works the same on any provider's results.
	var (
		results []searchResult
		err     error
	)
	if exaIsAvailable() {
		results, err = exaCompanySearch(query, 5)
	} else {
		results, err = WebSearch(query, nil, 5)
	}
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
	if limit <= 0 || limit > maxNewCompaniesPerRun {
		limit = maxNewCompaniesPerRun
	}
	// One search to find boards, then one per company we end up storing.
	// Stopping before the search is cheaper than discovering mid-run that we
	// cannot afford the lookups.
	if remaining := SearchBudgetRemaining(); remaining <= limit {
		return nil, fmt.Errorf("search budget nearly spent (%d left) — skipping discovery", remaining)
	}

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
	// discovery runs per interval: double the metered searches, and every
	// job board fetched twice.
	//
	// The lease covers the whole interval rather than part of it, and is not
	// released when the run finishes. Instance ticks are not aligned: if A
	// runs at :00 and B's schedule fires at :55, a lease that expired at :55
	// would let B run the identical query — the rotation index is derived
	// from the clock, so B lands on the same seed. A TTL this long does not
	// lock A out of its own next tick, because AcquireCronLease re-takes a
	// lease the same holder already has. Shutdown releases it explicitly so
	// a redeploy does not idle the next tick.
	if !AcquireCronLease(DiscoveryLeaseName, discoveryLeaseTTL) {
		log.Printf("board discovery rotation: another instance holds the lease, skipping")
		return
	}

	idx := int((time.Now().Unix() / int64(discoveryIntervalSeconds)) % int64(len(boardSeedQueries)))
	query := boardSeedQueries[idx]

	if saved, err := DiscoverFromBoards(query, maxNewCompaniesPerRun); err != nil {
		log.Printf("board discovery rotation failed for %q: %v", query, err)
	} else {
		log.Printf("board discovery rotation: %q -> %d new companies saved | search budget left: %d",
			query, len(saved), SearchBudgetRemaining())
	}
}

// JobSyncLeaseName is the cron lease for the hourly role refresh.
const JobSyncLeaseName = "job-sync"

// RunJobSync refreshes every stored company's roles.
//
// It runs on its own hourly schedule rather than on the back of discovery,
// because the two now tick at different rates: discovery is metered and runs
// three-hourly, while syncing is free and should keep listings current in
// between. Its own lease is what stops the two schedules — or two instances —
// from syncing the same directory at the same time and fetching every board
// twice.
func RunJobSync() {
	if !AcquireCronLease(JobSyncLeaseName, jobSyncLeaseTTL) {
		log.Printf("job sync: another instance holds the lease, skipping")
		return
	}
	SyncAllCompanyJobs()
}
