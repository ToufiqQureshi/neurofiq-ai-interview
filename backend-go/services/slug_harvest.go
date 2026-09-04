package services

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strconv"
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
//
// Two halves, deliberately separate
//
// Collection reads an index and writes candidates to a queue. Admission takes
// due candidates off that queue and judges them. They run on their own
// schedules and neither can stall the other, which is the difference between
// a pipeline that keeps producing and one that produces in lumps.
//
// The reason is that they fail differently. Collection is one source being
// unavailable for an hour; admission is thirteen thousand third-party boards
// being slow, throttling, or gone. When these were one function, a run that
// could not finish admission also could not record what it had collected, so
// the next tick re-read the whole index and made twelve thousand board calls
// to re-learn what it already knew. See models.BoardCandidate.

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

// fromQueueRow rebuilds a candidate from its stored row, so admission works on
// the same value whichever source suggested it.
func fromQueueRow(r models.BoardCandidate) slugCandidate {
	return slugCandidate{
		Provider: r.Provider, Slug: r.Slug, Name: r.Name, Website: r.Website,
		Sector: r.Sector, Stage: r.Stage, Area: r.Area,
		Lat: r.Lat, Lng: r.Lng, Source: r.Source,
	}
}

// harvestConcurrency bounds the board API calls a harvest makes at once.
//
// This is now a bound on our own parallelism only. Politeness towards any one
// provider is no longer this number's job: every request waits for its host's
// pacing slot (hostlimit.go), so eight workers against one provider form a
// queue rather than a burst, and eight workers against ten providers really do
// run ten-way. That separation is what lets this be raised without becoming
// rude — the old value had to be small because it was doing both jobs badly.
const harvestConcurrency = 12

// HarvestStats reports what one admission pass did, so a run can be judged
// without reading the log line by line.
type HarvestStats struct {
	Candidates int
	Skipped    int // structurally not an employer; never retried
	DeadBoard  int // provider answered, board is empty or gone
	NotIndian  int // live board, no Indian role
	Duplicate  int // already in the directory
	Attached   int // existing company gained a board
	Stored     int // new company
	Deferred   int // host would not answer; queued for a retry
}

func (s HarvestStats) String() string {
	return strings.Join([]string{
		"candidates=" + strconv.Itoa(s.Candidates),
		"skipped=" + strconv.Itoa(s.Skipped),
		"dead=" + strconv.Itoa(s.DeadBoard),
		"not-india=" + strconv.Itoa(s.NotIndian),
		"duplicate=" + strconv.Itoa(s.Duplicate),
		"attached=" + strconv.Itoa(s.Attached),
		"stored=" + strconv.Itoa(s.Stored),
		"deferred=" + strconv.Itoa(s.Deferred),
	}, " ")
}

// directoryIndex is the whole companies table in the two shapes a harvest asks
// about, read once.
//
// findDuplicateCompany answers the same question, and correctly, but it loads
// every company row on each call. Discovery calls it at most five times a
// tick, where that is free. Admission calls it once per candidate: at a
// thousand candidates a pass against a table of tens of thousands of rows, the
// same query runs a thousand times and the pass turns into a table scan
// benchmark. The answer changes rarely, so it is computed once per pass.
type directoryIndex struct {
	mu sync.RWMutex
	// boards is provider+lowercased slug, so a board already stored is
	// rejected before its API is called.
	boards map[string]bool
	// names is normalizeCompanyName -> company, matching findDuplicateCompany.
	names map[string]*models.Company
	// domains is the unique key the companies table is actually built on.
	domains map[string]*models.Company
	// slugs is companies.slug, which carries its own unique constraint.
	// Without this a name collision reached the INSERT and came back as an
	// error rather than as a duplicate — see admitCandidate.
	slugs map[string]bool
}

func newDirectoryIndex() (*directoryIndex, error) {
	// Only the columns the index is built from. Selecting whole rows pulled
	// descriptions and careers URLs across the wire for a table this reads in
	// full, which is bytes spent to be discarded.
	var stored []models.Company
	if err := config.DB.
		Select("id", "name", "slug", "domain", "ats_type", "ats_slug", "careers_url").
		Find(&stored).Error; err != nil {
		return nil, err
	}

	idx := &directoryIndex{
		boards:  make(map[string]bool, len(stored)),
		names:   make(map[string]*models.Company, len(stored)),
		domains: make(map[string]*models.Company, len(stored)),
		slugs:   make(map[string]bool, len(stored)),
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
		if c.Slug != "" {
			idx.slugs[strings.ToLower(c.Slug)] = true
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

// freeSlug returns a companies.slug not already taken.
//
// companies.slug is unique, and slugify() collides for names the name-level
// duplicate check lets through — it strips legal suffixes and parentheticals,
// so "Acme (Beta)" and "Acme Beta" normalize differently but slugify the same.
// Reaching the INSERT with a taken slug returned an error, not a conflict:
// OnConflict names the domain column, so Postgres raised the slug violation
// instead of doing nothing. One such row failed the pass, and a failed pass
// used to mean the whole index went unrecorded and was re-walked every three
// hours forever. Suffixing here costs nothing and removes the whole class.
func (idx *directoryIndex) freeSlug(base string) string {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if idx.slugs == nil {
		idx.slugs = map[string]bool{}
	}
	if base == "" {
		base = "company"
	}
	candidate := base
	for n := 2; idx.slugs[strings.ToLower(candidate)]; n++ {
		candidate = base + "-" + strconv.Itoa(n)
		if n > 50 {
			// Fifty companies sharing a slug base is not a naming collision,
			// it is a bug upstream. Fail the candidate rather than loop.
			return ""
		}
	}
	idx.slugs[strings.ToLower(candidate)] = true
	return candidate
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
	if c.Slug != "" {
		if idx.slugs == nil {
			idx.slugs = map[string]bool{}
		}
		idx.slugs[strings.ToLower(c.Slug)] = true
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

// AdmitDueCandidates judges the candidates whose time has come.
//
// Bounded twice on purpose. limit caps how many rows are taken off the queue,
// and ctx caps how long the pass may run whatever it finds — because the work
// per candidate is not predictable from its count: a board behind a throttled
// host can take a hundred times longer than one that answers at once. Without
// the deadline a pass could still overrun its cron window, and the next tick
// would start on top of it.
//
// Neither bound loses anything. A candidate not reached this pass keeps its
// due time and is simply first in line next pass, which is the property the
// queue was introduced for.
func AdmitDueCandidates(ctx context.Context, limit int) (HarvestStats, error) {
	stats := HarvestStats{}

	rows, err := DueCandidates(limit)
	if err != nil {
		return stats, fmt.Errorf("reading the candidate queue: %w", err)
	}
	stats.Candidates = len(rows)
	if len(rows) == 0 {
		return stats, nil
	}
	rows = interleaveByProvider(rows)

	// A directory we could not read is not an empty directory. Swallowing this
	// returned zero-of-everything with a nil error, so an automated run
	// reported a clean harvest that had evaluated nothing — and every
	// candidate would have looked new to the dedupe that never happened.
	idx, idxErr := newDirectoryIndex()
	if idxErr != nil {
		return stats, fmt.Errorf("could not read the directory: %w", idxErr)
	}

	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		sem = make(chan struct{}, harvestConcurrency)
	)

	for _, row := range rows {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(row models.BoardCandidate) {
			defer wg.Done()
			defer func() { <-sem }()
			// Gin's Recovery() does not cover goroutines spawned here, and one
			// malformed board payload would otherwise take the process down.
			// A panicking candidate is deferred rather than left pending, so
			// it backs off instead of panicking again every pass.
			defer func() {
				if r := recover(); r != nil {
					log.Printf("PANIC harvesting %s/%s: %v", row.Provider, row.Slug, r)
					settleCandidate(row, models.CandidateDeferred, "",
						fmt.Errorf("panic: %v", r))
				}
			}()

			outcome, companyID, cause := admitCandidate(ctx, fromQueueRow(row), idx)

			if err := settleCandidate(row, statusFor(outcome), companyID, cause); err != nil {
				log.Printf("slug harvest: could not settle %s/%s: %v", row.Provider, row.Slug, err)
			}

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
			case outcomeDeferred:
				stats.Deferred++
			}
		}(row)
	}
	wg.Wait()

	log.Printf("slug harvest: %s", stats)
	// The line that tells a quiet pass from a blocked one. "stored=0" reads
	// the same either way; a throttled host named here is the difference.
	if throttled := ThrottledHosts(); len(throttled) > 0 {
		for _, h := range throttled {
			log.Printf("slug harvest: %s is pacing us at %s after %d strikes",
				h.Host, h.Interval, h.Strikes)
		}
	}
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
	// outcomeDeferred is not a judgement. The host would not answer, so the
	// pipeline learned nothing and the candidate must come back — the one
	// outcome that is about us rather than about the board.
	outcomeDeferred
)

// statusFor maps an outcome onto the queue status that decides when, or
// whether, this candidate is looked at again.
func statusFor(o harvestOutcome) string {
	switch o {
	case outcomeStored:
		return models.CandidateStored
	case outcomeAttached:
		return models.CandidateAttached
	case outcomeDuplicate:
		// The board is already in the directory under some company. Nothing
		// about this candidate will change that, and the company's own sync
		// keeps its roles current.
		return models.CandidateAttached
	case outcomeNotIndian:
		return models.CandidateForeign
	case outcomeDeadBoard:
		return models.CandidateDead
	case outcomeDeferred:
		return models.CandidateDeferred
	default:
		return models.CandidateRejected
	}
}

// admitCandidate is the gate. Every rule here is one discovery already
// applies; the ordering is what keeps a pass cheap.
//
// Free checks run before the board call, the board call runs before the
// website work, and the website work runs before anything is written. A
// candidate rejected at the first step costs nothing at all, which matters
// when most of the queue is already stored, dead, or hiring nowhere near
// India.
//
// It returns the outcome, the company the candidate ended up on where there is
// one, and — for a deferral only — the cause, which is recorded for the
// operator and never used to decide anything.
func admitCandidate(ctx context.Context, c slugCandidate, idx *directoryIndex) (harvestOutcome, string, error) {
	provider := strings.ToLower(strings.TrimSpace(c.Provider))
	slug := strings.TrimSpace(c.Slug)
	if provider == "" || slug == "" || !validATSSlug(slug) || !boardSlugIsAdmissible(slug) {
		return outcomeSkipped, "", nil
	}
	if idx.hasBoard(provider, slug) {
		return outcomeDuplicate, "", nil
	}

	// A harvested slug carries no page title, so the name starts as whatever
	// the source offered and falls back to the slug — the same fallback
	// companyNameFromBoard uses when a title cannot be corroborated.
	name := strings.TrimSpace(c.Name)
	if name == "" || !nameAgreesWithSlug(name, slug) {
		name = slugDisplayName(slug)
	}
	if !boardRowIsAdmissible(name, slug) {
		return outcomeSkipped, "", nil
	}
	if sharedBoardRe.MatchString(name) || sharedBoardRe.MatchString(slug) ||
		aggregatorBoardRe.MatchString(name) || aggregatorBoardRe.MatchString(slug) {
		return outcomeSkipped, "", nil
	}

	// A Workday slug arrives from the crawl without its job-site id, which is
	// not in the URL. Resolving it costs live API calls, so it happens here —
	// after the free checks above have already discarded the tenants we
	// recognise, and never during collection.
	if provider == "workday" {
		resolved, err := resolveWorkdaySlug(ctx, slug)
		if err != nil {
			return outcomeDeferred, "", err
		}
		if resolved == "" {
			return outcomeDeadBoard, "", nil
		}
		slug = resolved
		if idx.hasBoard(provider, slug) {
			return outcomeDuplicate, "", nil
		}
	}

	// The board's own API. Free, and it answers both remaining questions at
	// once: is this a real live board, and does it hire here.
	//
	// The error is classified rather than collapsed. A 404 is the board
	// saying it does not exist; a 429 is the host saying not now, and filing
	// the second as the first is how a rate-limited pass silently drops real
	// companies and reports a normal-looking dead count. This codebase
	// already holds that rule — "silence and zero must not look alike" — and
	// it is IsTransientFetchError that keeps it here.
	jobs, err := FetchATSJobs("", provider, slug)
	if err != nil {
		if IsTransientFetchError(err) {
			return outcomeDeferred, "", err
		}
		return outcomeDeadBoard, "", nil
	}
	if len(jobs) == 0 {
		return outcomeDeadBoard, "", nil
	}
	// A board whose only roles are talent-pool signups is not hiring.
	jobs = dropTalentPools(jobs)
	jobs = tidyLocations(jobs)
	if len(jobs) == 0 {
		return outcomeDeadBoard, "", nil
	}
	if len(jobs) > maxBoardRoles {
		return outcomeSkipped, "", nil
	}
	area := firstIndianLocation(jobs)
	if area == "" {
		return outcomeNotIndian, "", nil
	}

	// Name-level duplicate, before spending anything on the website.
	if dup := idx.duplicate(name, ""); dup != nil {
		if attachBoardTo(dup, boardHit{Provider: provider, Slug: slug, URL: boardURL(provider, slug)}) {
			return outcomeAttached, dup.ID, nil
		}
		// Lost the race, or the company already had a board by the time this
		// candidate reached it — either way it is a duplicate, not a company
		// this candidate changed.
		return outcomeDuplicate, dup.ID, nil
	}

	// The company's own site. The register hands one over; otherwise the free
	// guess from its board is tried. Neither costs a search, and a harvest
	// never falls through to resolveCompanyWebsite — a queue of this size
	// would drain the month in an afternoon.
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
		// No verifiable site. Not a judgement about the board, so it is left
		// to the monthly re-check rather than rejected outright: a company
		// whose homepage was down today may resolve next month.
		return outcomeDeadBoard, "", nil
	}
	if dup := idx.duplicate(name, domain); dup != nil {
		if attachBoardTo(dup, boardHit{Provider: provider, Slug: slug, URL: boardURL(provider, slug)}) {
			return outcomeAttached, dup.ID, nil
		}
		return outcomeDuplicate, dup.ID, nil
	}

	companySlug := idx.freeSlug(slugify(name))
	if companySlug == "" {
		return outcomeSkipped, "", nil
	}

	company := models.Company{
		Name:       name,
		Slug:       companySlug,
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

	// The pin has to agree with the area printed beside it, so it is geocoded
	// from the area actually stored — which firstIndianLocation guarantees is
	// a location the board itself stated. The register's own coordinates are
	// its registered office, frequently a founder's home in another city
	// entirely: taking those first put a Bengaluru company's pin on a Mumbai
	// address, and the card and the map then disagreed about where the work
	// is. They stay as the fallback for the case geocoding cannot answer.
	if lat, lng, geoErr := geocodeArea(area); geoErr == nil {
		company.Lat, company.Lng = lat, lng
	} else if c.Lat != nil && c.Lng != nil {
		company.Lat, company.Lng = c.Lat, c.Lng
	}

	// Both unique columns are named, so a collision on either is a no-op
	// rather than an error. freeSlug makes the slug case unlikely; this makes
	// it harmless when two workers pick the same free slug at the same moment.
	result := config.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "domain"}},
		DoNothing: true,
	}).Create(&company)
	if result.Error != nil {
		// The candidate was never actually judged, so it must not be recorded
		// as though it were. Deferring re-tries it on a backoff instead of
		// burying a database problem as a rejected company.
		log.Printf("slug harvest: failed to save %q: %v", name, result.Error)
		return outcomeDeferred, "", fmt.Errorf("saving %q: %w", name, result.Error)
	}
	if result.RowsAffected == 0 {
		return outcomeDuplicate, "", nil
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
	return outcomeStored, company.ID, nil
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

// resolveWorkdaySlug fills in the job-site id a crawled Workday URL does not
// carry, returning "" when no candidate site answers with postings.
//
// A Workday board is <tenant>.<region>.myworkdayjobs.com/<site>. The crawl
// gives the first two; the third has to be asked for, which is what
// scanForATS already does when it finds a Workday link on a careers page.
// This is the same probe, reached from the other direction.
//
// The error return is the point of the rewrite: a tenant that answered 429 to
// all five probes used to be indistinguishable from one that answered 404 to
// all five, and was recorded as dead. Now the caller can tell, and a throttled
// tenant is retried instead of written off.
//
// A slug that already carries a site id is returned untouched, so a candidate
// from any other source passes through.
func resolveWorkdaySlug(ctx context.Context, slug string) (string, error) {
	parts := strings.Split(slug, ":")
	if len(parts) != 3 {
		return "", nil
	}
	tenant, region, site := parts[0], parts[1], parts[2]
	if site != "" {
		return slug, nil
	}
	if tenant == "" || region == "" {
		return "", nil
	}

	var transient error
	for _, candidate := range workdaySiteCandidates {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		full := tenant + ":" + region + ":" + candidate
		jobs, err := fetchWorkdayJobs(full)
		if err == nil && len(jobs) > 0 {
			return full, nil
		}
		if IsTransientFetchError(err) {
			transient = err
		}
	}
	// Only after every probe has been tried: one site id answering 404 while
	// another was throttled is still a tenant we have not properly asked.
	return "", transient
}
