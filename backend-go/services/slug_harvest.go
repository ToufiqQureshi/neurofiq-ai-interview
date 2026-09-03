package services

import (
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm/clause"

	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/config"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/models"
)

// Slug harvesting — finding boards without paying a search for each one.
//
// Discovery searches board domains and stores what comes back, which works and
// is what the directory is built on. It has one property that caps it: a
// search is metered. Measured over September, the rotation spent 293 of Exa's
// 800 monthly calls to store 145 companies — 2.02 searches each — so the
// month's ceiling is around 400 companies however fast the cron runs.
//
// The boards themselves are not hidden. Every one of them is a public page
// that crawlers have already indexed, and the slug this pipeline needs is in
// the URL. So the list can be read from an index that already exists rather
// than rediscovered one paid query at a time. One Common Crawl index yielded
// 13,501 distinct slugs across the eight providers job_service can read, for
// about twenty HTTP requests and no metered call at all.
//
// What this does NOT change is admission. A harvested slug is a candidate and
// nothing more: it is the same shape as a search hit and goes through the same
// gate — the board's own API must answer, the roles must be in India, the name
// must survive boardRowIsAdmissible, and the company must not already be here.
// The evidence rule this directory rests on is unchanged; only the way
// candidates arrive is cheaper.

// slugCandidate is one board a source believes exists, plus whatever that
// source happens to know about the company behind it.
//
// Common Crawl knows only the provider and the slug. The startup register
// knows a great deal about the company and nothing about its board, which is
// why it carries the optional fields — they are the reason to run that source
// at all, since no board API reports a funding stage or a street address.
// Every optional field is advisory: the pipeline below prefers what it can
// verify and falls back to these only where verification has nothing to say.
type slugCandidate struct {
	Provider string
	Slug     string

	// Everything below is optional and source-supplied.
	Name    string
	Website string
	Sector  string
	Stage   string
	Area    string
	Lat     *float64
	Lng     *float64

	// Source is written to companies.source so a row can be traced back to
	// the thing that suggested it.
	Source string
}

// harvestConcurrency bounds the board API calls a harvest makes at once.
//
// Same number the job sync uses, and for the same reason: these are other
// people's public endpoints, and a harvest reads thousands of them in a row
// where the sync reads a few hundred. Politeness is the constraint here, not
// our own throughput.
const harvestConcurrency = 8

// harvestPoliteness is the pause each worker takes between board calls, so a
// run of thousands of slugs does not read as a burst to any one provider.
const harvestPoliteness = 250 * time.Millisecond

// HarvestStats reports what one harvest did, so a run can be judged without
// reading the log line by line.
type HarvestStats struct {
	Candidates int
	Skipped    int // rejected before any network call
	DeadBoard  int // provider answered, board is empty or gone
	NotIndian  int // live board, no Indian role
	Duplicate  int // already in the directory
	Attached   int // existing company gained a board
	Stored     int // new company
}

func (s HarvestStats) String() string {
	return strings.Join([]string{
		"candidates=" + itoa(s.Candidates),
		"skipped=" + itoa(s.Skipped),
		"dead=" + itoa(s.DeadBoard),
		"not-india=" + itoa(s.NotIndian),
		"duplicate=" + itoa(s.Duplicate),
		"attached=" + itoa(s.Attached),
		"stored=" + itoa(s.Stored),
	}, " ")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}

// directoryIndex is the whole companies table in the two shapes a harvest asks
// about, read once.
//
// findDuplicateCompany answers the same question, and correctly, but it loads
// every company row on each call. Discovery calls it at most five times a
// tick, where that is free. A harvest calls it once per candidate: at 13,501
// candidates against a table of a few thousand rows, the same query runs
// thirteen thousand times and the run turns into a table scan benchmark. The
// answer does not change during a harvest, so it is computed once.
type directoryIndex struct {
	mu sync.RWMutex
	// boards is provider+lowercased slug, so a board already stored is
	// rejected before its API is called.
	boards map[string]bool
	// names is normalizeCompanyName -> company, matching findDuplicateCompany.
	names map[string]*models.Company
	// domains is the unique key the companies table is actually built on.
	domains map[string]*models.Company
}

func newDirectoryIndex() (*directoryIndex, error) {
	var stored []models.Company
	if err := config.DB.Find(&stored).Error; err != nil {
		return nil, err
	}

	idx := &directoryIndex{
		boards:  make(map[string]bool, len(stored)),
		names:   make(map[string]*models.Company, len(stored)),
		domains: make(map[string]*models.Company, len(stored)),
	}
	for i := range stored {
		c := &stored[i]
		if c.ATSSlug != "" {
			idx.boards[boardKey(c.ATSType, c.ATSSlug)] = true
		}
		if key := normalizeCompanyName(c.Name); len(key) >= 4 {
			if _, seen := idx.names[key]; !seen {
				idx.names[key] = c
			}
		}
		if c.Domain != "" {
			idx.domains[strings.ToLower(c.Domain)] = c
		}
	}
	return idx, nil
}

func boardKey(provider, slug string) string {
	return strings.ToLower(provider) + ":" + strings.ToLower(slug)
}

func (idx *directoryIndex) hasBoard(provider, slug string) bool {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.boards[boardKey(provider, slug)]
}

// duplicate mirrors findDuplicateCompany: domain first, then normalized name.
func (idx *directoryIndex) duplicate(name, domain string) *models.Company {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	if domain != "" {
		if c := idx.domains[strings.ToLower(domain)]; c != nil {
			return c
		}
	}
	if key := normalizeCompanyName(name); len(key) >= 4 {
		return idx.names[key]
	}
	return nil
}

// remember records a company the harvest just stored, so two candidates for
// the same business inside one run cannot both be written.
func (idx *directoryIndex) remember(c models.Company) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if c.ATSSlug != "" {
		idx.boards[boardKey(c.ATSType, c.ATSSlug)] = true
	}
	if key := normalizeCompanyName(c.Name); len(key) >= 4 {
		if _, seen := idx.names[key]; !seen {
			idx.names[key] = &c
		}
	}
	if c.Domain != "" {
		idx.domains[strings.ToLower(c.Domain)] = &c
	}
}

// dedupeCandidates collapses the same board arriving twice.
//
// It arrives twice routinely: two Common Crawl indexes overlap heavily by
// design, and a company found in the register may also appear in the crawl.
// Whichever copy carries more information wins, because the register's copy
// knows a sector and a stage that the crawl's copy never will.
func dedupeCandidates(in []slugCandidate) []slugCandidate {
	best := make(map[string]int, len(in))
	out := make([]slugCandidate, 0, len(in))

	for _, c := range in {
		key := boardKey(c.Provider, c.Slug)
		at, seen := best[key]
		if !seen {
			best[key] = len(out)
			out = append(out, c)
			continue
		}
		if candidateDetail(c) > candidateDetail(out[at]) {
			out[at] = c
		}
	}
	return out
}

// candidateDetail counts how much a source told us, so the richer duplicate is
// the one kept.
func candidateDetail(c slugCandidate) int {
	n := 0
	for _, s := range []string{c.Name, c.Website, c.Sector, c.Stage, c.Area} {
		if strings.TrimSpace(s) != "" {
			n++
		}
	}
	if c.Lat != nil && c.Lng != nil {
		n++
	}
	return n
}

// HarvestSlugs runs candidates through the directory's admission rules and
// stores the ones that pass.
//
// limit caps how many companies one run will store; zero means no cap, which
// is what a one-time backfill wants and a scheduled run should never use.
func HarvestSlugs(candidates []slugCandidate, limit int) (HarvestStats, error) {
	stats := HarvestStats{}
	candidates = dedupeCandidates(candidates)
	stats.Candidates = len(candidates)
	if len(candidates) == 0 {
		return stats, nil
	}

	// A directory we could not read is not an empty directory. Swallowing this
	// returned zero-of-everything with a nil error, so an automated run
	// reported a clean harvest that had evaluated nothing — and every
	// candidate would have looked new to the dedupe that never happened.
	idx, err := newDirectoryIndex()
	if err != nil {
		return stats, fmt.Errorf("could not read the directory: %w", err)
	}

	var (
		mu   sync.Mutex
		wg   sync.WaitGroup
		sem  = make(chan struct{}, harvestConcurrency)
		done bool
	)

	for _, cand := range candidates {
		mu.Lock()
		full := done
		mu.Unlock()
		if full {
			break
		}

		wg.Add(1)
		sem <- struct{}{}
		go func(c slugCandidate) {
			defer wg.Done()
			defer func() { <-sem }()
			// Gin's Recovery() does not cover goroutines spawned here, and one
			// malformed board payload would otherwise take the process down.
			defer func() {
				if r := recover(); r != nil {
					log.Printf("PANIC harvesting %s/%s: %v", c.Provider, c.Slug, r)
				}
			}()

			outcome := admitCandidate(c, idx)
			time.Sleep(harvestPoliteness)

			mu.Lock()
			defer mu.Unlock()
			switch outcome {
			case outcomeSkipped:
				stats.Skipped++
			case outcomeDeadBoard:
				stats.DeadBoard++
			case outcomeNotIndian:
				stats.NotIndian++
			case outcomeDuplicate:
				stats.Duplicate++
			case outcomeAttached:
				stats.Attached++
			case outcomeStored:
				stats.Stored++
				if limit > 0 && stats.Stored >= limit {
					done = true
				}
			}
		}(cand)
	}
	wg.Wait()

	log.Printf("slug harvest: %s", stats)
	return stats, nil
}

type harvestOutcome int

const (
	outcomeSkipped harvestOutcome = iota
	outcomeDeadBoard
	outcomeNotIndian
	outcomeDuplicate
	outcomeAttached
	outcomeStored
)

// admitCandidate is the gate. Every rule here is one discovery already
// applies; the ordering is what keeps a harvest cheap.
//
// Free checks run before the board call, the board call runs before the
// website work, and the website work runs before anything is written. A
// candidate rejected at the first step costs nothing at all, which matters
// when the input is thirteen thousand slugs of which most are already stored,
// dead, or hiring nowhere near India.
func admitCandidate(c slugCandidate, idx *directoryIndex) harvestOutcome {
	provider := strings.ToLower(strings.TrimSpace(c.Provider))
	slug := strings.TrimSpace(c.Slug)
	if provider == "" || slug == "" || !validATSSlug(slug) || !boardSlugIsAdmissible(slug) {
		return outcomeSkipped
	}
	if idx.hasBoard(provider, slug) {
		return outcomeDuplicate
	}

	// A harvested slug carries no page title, so the name starts as whatever
	// the source offered and falls back to the slug — the same fallback
	// companyNameFromBoard uses when a title cannot be corroborated.
	name := strings.TrimSpace(c.Name)
	if name == "" || !nameAgreesWithSlug(name, slug) {
		name = slugDisplayName(slug)
	}
	if !boardRowIsAdmissible(name, slug) {
		return outcomeSkipped
	}
	if sharedBoardRe.MatchString(name) || sharedBoardRe.MatchString(slug) ||
		aggregatorBoardRe.MatchString(name) || aggregatorBoardRe.MatchString(slug) {
		return outcomeSkipped
	}

	// The board's own API. Free, and it answers both remaining questions at
	// once: is this a real live board, and does it hire here.
	jobs, err := FetchATSJobs("", provider, slug)
	if err != nil || len(jobs) == 0 {
		return outcomeDeadBoard
	}
	// A board whose only roles are talent-pool signups is not hiring.
	jobs = dropTalentPools(jobs)
	jobs = tidyLocations(jobs)
	if len(jobs) == 0 {
		return outcomeDeadBoard
	}
	if len(jobs) > maxBoardRoles {
		return outcomeSkipped
	}
	area := firstIndianLocation(jobs)
	if area == "" {
		return outcomeNotIndian
	}

	// Name-level duplicate, before spending anything on the website.
	if dup := idx.duplicate(name, ""); dup != nil {
		attachBoardTo(dup, boardHit{Provider: provider, Slug: slug, URL: boardURL(provider, slug)})
		return outcomeAttached
	}

	// The company's own site. The register hands one over; otherwise the free
	// guess from its board is tried. Neither costs a search, and a harvest
	// never falls through to resolveCompanyWebsite — a run of this size would
	// drain the month in an afternoon.
	website := strings.TrimSpace(c.Website)
	if website != "" && !strings.HasPrefix(website, "http") {
		website = "https://" + website
	}
	if website != "" && isAggregatorHost(extractDomain(website)) {
		website = ""
	}
	if website == "" {
		website = guessCompanyWebsite(provider, slug)
	}
	domain := extractDomain(website)
	if domain == "" {
		return outcomeSkipped
	}
	if dup := idx.duplicate(name, domain); dup != nil {
		attachBoardTo(dup, boardHit{Provider: provider, Slug: slug, URL: boardURL(provider, slug)})
		return outcomeAttached
	}

	// The register's area is a registered office, which is frequently a
	// founder's home. A location the board itself stated is where the work is,
	// so it wins whenever there is one.
	if area == "" {
		area = strings.TrimSpace(c.Area)
	}

	company := models.Company{
		Name:       name,
		Slug:       slugify(name),
		Website:    website,
		Domain:     domain,
		Sector:     strings.TrimSpace(c.Sector),
		Stage:      strings.TrimSpace(c.Stage),
		Area:       area,
		CareersURL: boardURL(provider, slug),
		ATSType:    provider,
		ATSSlug:    slug,
		Source:     c.Source,
	}
	now := time.Now()
	company.ATSCheckedAt = &now

	// Coordinates the register measured beat the ones fallbackCoordsForArea
	// derives from a hash of the company's name. Where the register has none,
	// geocoding the area is the same free Nominatim call discovery makes.
	if c.Lat != nil && c.Lng != nil {
		company.Lat, company.Lng = c.Lat, c.Lng
	} else if lat, lng, geoErr := geocodeArea(area); geoErr == nil {
		company.Lat, company.Lng = lat, lng
	}

	result := config.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "domain"}},
		DoNothing: true,
	}).Create(&company)
	if result.Error != nil {
		log.Printf("slug harvest: failed to save %q: %v", name, result.Error)
		return outcomeSkipped
	}
	if result.RowsAffected == 0 {
		return outcomeDuplicate
	}
	idx.remember(company)

	for i := range jobs {
		jobs[i].CompanyID = company.ID
	}
	if n, jerr := replaceJobsForCompany(company.ID, jobs); jerr != nil {
		log.Printf("slug harvest: failed to store roles for %q: %v", name, jerr)
	} else {
		log.Printf("slug harvest: %s (%s/%s) -> %d roles", name, provider, slug, n)
	}
	return outcomeStored
}

// HarvestOptions is what an operator chose on the command line.
type HarvestOptions struct {
	// CommonCrawl reads board slugs out of the published URL index. Cheap and
	// broad: about twenty requests for thousands of slugs.
	CommonCrawl bool
	// StartupRegister walks the accelerator portfolios for companies that
	// arrive with a sector, a stage and coordinates. Slow and narrow.
	StartupRegister bool
	// Indexes is how many recent Common Crawl indexes to read. More is worth
	// it: a second index added 204 Keka slugs and 745 Greenhouse ones the
	// first did not have, because boards open and close between crawls.
	Indexes int
	// Limit caps companies stored, and for the register also caps pages read.
	// Zero means no cap, which only a deliberate backfill should use.
	Limit int
}

// RunHarvest collects candidates from the chosen sources and admits them.
//
// It lives here rather than in main so the candidate type stays unexported:
// assembling candidates is this package's business, and a caller that could
// build one by hand could bypass the admission rules that make a board
// trustworthy.
func RunHarvest(opts HarvestOptions) (HarvestStats, error) {
	var candidates []slugCandidate

	if opts.CommonCrawl {
		n := opts.Indexes
		if n <= 0 {
			n = 1
		}
		ids, err := LatestCommonCrawlIndexes(n)
		if err != nil {
			return HarvestStats{}, err
		}
		log.Printf("harvest: reading Common Crawl indexes %v", ids)
		candidates = append(candidates, HarvestFromCommonCrawl(ids)...)
	}

	if opts.StartupRegister {
		log.Printf("harvest: reading the startup register's accelerator portfolios")
		candidates = append(candidates, HarvestFromStartupRegister(nil, opts.Limit)...)
	}

	log.Printf("harvest: %d candidates collected", len(candidates))
	return HarvestSlugs(candidates, opts.Limit)
}

// talentPoolRe matches the evergreen "send us your CV" posting most boards
// keep open alongside their real vacancies.
//
// It is a posting by every structural measure — its own id, its own apply URL,
// a location — so nothing upstream rejects it, and the first harvest surfaced
// one immediately: Affinidi's "Be Part of our Talent Community" arrived as that
// company's only Indian role, which would have entered the directory as a
// hiring company advertising a mailing list.
//
// Matched as a phrase rather than a whole title, because boards word it
// differently every time. The phrases are specific enough that no real vacancy
// contains them — unlike the single words in ctaTitles, which is why that list
// stayed exact-match. "Talent Acquisition Specialist" and "Application Security
// Engineer" both survive this.
//
// It lives here rather than in looksLikeRoleTitle because only the harvest has
// met it so far. If the ordinary sync starts seeing these too, that is the
// moment to move it into the shared guard rather than before.
var talentPoolRe = regexp.MustCompile(`(?i)talent (community|pool|network|bench)|general application|future opportunit|speculative application|open application|didn'?t find|don'?t see (a|the) role`)

// dropTalentPools removes the postings above from a board's roles.
func dropTalentPools(jobs []models.Job) []models.Job {
	kept := jobs[:0]
	for _, j := range jobs {
		if talentPoolRe.MatchString(j.Title) {
			continue
		}
		kept = append(kept, j)
	}
	return kept
}

// tidyLocations normalises the whitespace a board puts in a location string.
//
// Darwinbox returns "Bengaluru, Karnataka, India" — a literal carriage
// return inside the field, which reaches the card and the area column exactly
// as written. Every producer of a location ends up here, so the cleanup
// belongs here rather than in each reader.
func tidyLocations(jobs []models.Job) []models.Job {
	for i := range jobs {
		jobs[i].Location = tidyLocation(jobs[i].Location)
	}
	return jobs
}

// tidyLocation collapses internal whitespace and tidies the punctuation that
// collapsing can leave stranded (", ," and a trailing comma).
func tidyLocation(raw string) string {
	s := whitespaceRe.ReplaceAllString(strings.TrimSpace(raw), " ")
	s = strings.ReplaceAll(s, " ,", ",")
	for strings.Contains(s, ",,") {
		s = strings.ReplaceAll(s, ",,", ",")
	}
	return strings.Trim(s, " ,")
}
