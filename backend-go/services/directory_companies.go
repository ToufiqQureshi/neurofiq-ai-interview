package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/config"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/models"
)

var nonSlugChars = regexp.MustCompile(`[^a-z0-9]+`)

// Noise words stripped when comparing company names for duplicates. The
// discovery agent returns the same company under different labels — e.g.
// "BYJU'S Exam Prep" and "BYJU'S Exam Prep (Gradeup)" arrived with
// different domains, so domain-only dedupe let both through.
var (
	parentheticalRe = regexp.MustCompile(`\([^)]*\)`)
	companyNoiseRe  = regexp.MustCompile(`\b(private|pvt|limited|ltd|llp|inc|incorporated|corp|corporation|technologies|technology|tech|solutions|systems|labs|software|services|india|global|group|company|co)\b`)
)

// trailingCountRe is the number an ATS appends when a company takes a slug it
// already has — acme, acme2, brillio-2, adeebaeservicespvtltd3.
var trailingCountRe = regexp.MustCompile(`[0-9]{1,2}$`)

// minStemBeforeCount is how much name has to survive stripping that number
// before the strip is believable. Below it the digits are part of the name:
// "Web3" keeps its 3 because "web" alone is not the company, while "acme2"
// and "adeebaeservicespvtltd3" are second registrations of names already here.
//
// Four, not six: six read well against the long slug that prompted this and
// then failed the short ones, which is where the real duplicates were —
// asapp/asapp-2 and brillio/brillio-2 are both live on Lever right now.
const minStemBeforeCount = 4

// normalizeCompanyName reduces a name to a comparable key: lowercased, with
// parentheticals, legal suffixes, punctuation and a re-registration number
// removed.
//
//	"BYJU'S Exam Prep (Gradeup)"      -> "byjusexamprep"
//	"Edunext Technologies Pvt. Ltd."  -> "edunext"
//	"adeebaeservicespvtltd3"          -> "adeebaeservicespvtltd"
func normalizeCompanyName(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = parentheticalRe.ReplaceAllString(s, " ")
	s = companyNoiseRe.ReplaceAllString(s, " ")
	s = nonSlugChars.ReplaceAllString(s, "")
	if stem := trailingCountRe.ReplaceAllString(s, ""); len(stem) >= minStemBeforeCount {
		return stem
	}
	return s
}

// findDuplicateCompany returns an existing company that is the same business
// under a different name or domain, or nil if this looks genuinely new.
func findDuplicateCompany(name, domain string) *models.Company {
	if byDomain := (models.Company{}); config.DB.Where("domain = ?", domain).First(&byDomain).Error == nil {
		return &byDomain
	}

	key := normalizeCompanyName(name)
	if len(key) < 4 {
		return nil // too short to match safely — "AI", "Zoho" style names
	}

	// Compare against existing names in the same normalized form. Cheap at
	// this table size, and avoids needing a stored normalized column.
	var existing []models.Company
	if err := config.DB.Find(&existing).Error; err != nil {
		return nil
	}
	for i := range existing {
		if normalizeCompanyName(existing[i].Name) == key {
			return &existing[i]
		}
	}
	return nil
}

// CompanyWithJobCount is a Company row annotated with how many currently
// open roles we have for it, so the directory can show a "N open" badge
// without an N+1 query per card.
type CompanyWithJobCount struct {
	models.Company
	JobCount int64 `json:"job_count"`
}

// UnknownFacetValue is the option that reaches the companies the pipeline
// never filled in.
//
// Board discovery stores no sector or stage — it learns a company from its
// job board, which does not publish either — so 145 of 275 companies have an
// empty stage and 127 an empty sector. Every one of them vanished the moment
// a visitor touched a filter, which reads as "the directory has nothing" on
// what is actually its majority. It also matches the literal "Unknown" a
// couple of rows carry, because to someone filtering, an unrecorded stage and
// one recorded as unknown are the same thing.
const UnknownFacetValue = "Unknown"

// applyFacetFilter filters on an exact-match company column, and is the only
// place that knows how. ListCompanies, TotalOpenRoles and JobFacets each had
// their own copy of these two clauses, which is three chances for a filter to
// mean something different from the count printed beside it.
func applyFacetFilter(db *gorm.DB, column, value, prefix string) *gorm.DB {
	if value == "" {
		return db
	}
	condition, args := facetClause(column, value, prefix)
	return db.Where(condition, args...)
}

// facetClause is the condition itself, kept separate from the query so it can
// be read without a database.
func facetClause(column, value, prefix string) (string, []interface{}) {
	col := column
	if prefix != "" {
		// Unqualified, "stage = ?" is ambiguous on a query that joins
		// companies to jobs, and Postgres rejects the statement rather than
		// guessing — the filter would not narrow, it would 500.
		col = prefix + "." + column
	}
	if value == UnknownFacetValue {
		// Parenthesised deliberately. This clause is ANDed with the other
		// filters, and a bare OR chain would bind looser than the AND: one
		// unparenthesised OR turns "hiring in Pune AND stage unknown" into
		// "hiring in Pune, or anything at all with a blank stage", which
		// returns more rows the more the visitor filters.
		return "(" + col + " IS NULL OR " + col + " = '' OR " + col + " = ?)",
			[]interface{}{UnknownFacetValue}
	}
	return col + " = ?", []interface{}{value}
}

// CompanyFacets lists the sector and stage values the directory actually
// holds, so the filter dropdowns describe this table rather than a list
// someone typed into the frontend.
//
// The hardcoded lists they replace had drifted both ways: they offered
// Pre-seed, Gaming, Consumer and Other, which no company has, while Series C,
// Series H and Unknown existed in the data with no way to select them. Every
// option here is one that returns something, and every value in the data has
// an option.
//
// Deliberately not filtered by the visitor's current selection. These are the
// choices available, not the choices remaining — narrowing them as filters are
// applied is how a dropdown ends up with one entry and no way back.
func CompanyFacets() (sectors, stages []string, err error) {
	// Cached, because these are two SELECT DISTINCT scans of the whole
	// companies table and they ran on every single request to /api/companies.
	// The values only change when a company is stored or enriched, which is
	// minutes apart at best, so a short cache costs nothing in freshness and
	// takes both scans off the hot path entirely.
	facetCacheMu.RLock()
	fresh := facetCacheAt.Add(facetCacheTTL).After(time.Now())
	cachedSectors, cachedStages := facetCacheSectors, facetCacheStages
	facetCacheMu.RUnlock()
	if fresh {
		return cachedSectors, cachedStages, nil
	}

	if sectors, err = distinctCompanyValues("sector"); err != nil {
		return nil, nil, err
	}
	if stages, err = distinctCompanyValues("stage"); err != nil {
		return nil, nil, err
	}

	facetCacheMu.Lock()
	facetCacheSectors, facetCacheStages, facetCacheAt = sectors, stages, time.Now()
	facetCacheMu.Unlock()
	return sectors, stages, nil
}

// facetCacheTTL is short enough that a newly discovered sector appears on the
// filter within one refresh, and long enough that a burst of traffic does not
// scan the table once per visitor.
const facetCacheTTL = 5 * time.Minute

var (
	facetCacheMu      sync.RWMutex
	facetCacheSectors []string
	facetCacheStages  []string
	facetCacheAt      time.Time
)

// distinctCompanyValues reads one column's options, sorted, with a single
// Unknown entry standing for every row the pipeline left blank.
func distinctCompanyValues(column string) ([]string, error) {
	// COALESCE because the column is nullable and Pluck scans into a plain
	// string: one NULL row fails the whole scan, and the caller renders a
	// filter with no options at all. Every blank collapses into Unknown
	// immediately below, so a null and an empty string are already the same
	// answer here.
	// Ordered by the same expression it selects. Postgres rejects a SELECT
	// DISTINCT whose ORDER BY names something outside the select list
	// (SQLSTATE 42P10), so ordering by the bare column while plucking the
	// COALESCE failed every call — and HandleGetCompanies drops the error,
	// which would have rendered the filter with no options at all.
	selected := "COALESCE(" + column + ", '')"

	var raw []string
	if err := config.DB.Model(&models.Company{}).
		Distinct().
		Order(selected).
		Pluck(selected, &raw).Error; err != nil {
		return nil, err
	}

	return collapseFacetValues(raw), nil
}

// collapseFacetValues turns stored column values into filter options.
//
// Blank and the literal "Unknown" become the single Unknown option — the same
// collapse facetClause makes when that option is chosen, so every option
// offered returns rows and every row is reachable from some option. Unknown
// sorts last because it is the absence of an answer, not one of them.
func collapseFacetValues(raw []string) []string {
	out := make([]string, 0, len(raw)+1)
	unknown := false
	for _, v := range raw {
		v = strings.TrimSpace(v)
		if v == "" || v == UnknownFacetValue {
			unknown = true
			continue
		}
		out = append(out, v)
	}
	if unknown {
		out = append(out, UnknownFacetValue)
	}
	return out
}

// applyAreaFilter matches tech hubs and aliases across the area column.
func applyAreaFilter(db *gorm.DB, area string, prefix string) *gorm.DB {
	if area == "" {
		return db
	}
	col := "area"
	if prefix != "" {
		col = prefix + ".area"
	}
	norm := strings.ToLower(strings.TrimSpace(area))
	var keywords []string
	switch {
	case strings.Contains(norm, "delhi") || strings.Contains(norm, "ncr") || strings.Contains(norm, "noida") || strings.Contains(norm, "gurgaon") || strings.Contains(norm, "gurugram"):
		keywords = []string{"noida", "gurgaon", "gurugram", "delhi"}
	case strings.Contains(norm, "bengaluru") || strings.Contains(norm, "bangalore"):
		keywords = []string{"bengaluru", "bangalore", "hsr", "koramangala", "indiranagar", "whitefield"}
	case strings.Contains(norm, "mumbai"):
		keywords = []string{"mumbai", "navi mumbai", "thane"}
	case strings.Contains(norm, "hyderabad"):
		keywords = []string{"hyderabad", "hitec"}
	case strings.Contains(norm, "pune"):
		keywords = []string{"pune"}
	default:
		return db.Where(col+" ILIKE ?", "%"+area+"%")
	}

	var clauses []string
	var vals []interface{}
	for _, kw := range keywords {
		clauses = append(clauses, col+" ILIKE ?")
		vals = append(vals, "%"+kw+"%")
	}
	return db.Where(strings.Join(clauses, " OR "), vals...)
}

// cityCentroids is where a company goes on the map when geocodeArea could not
// place it. Most specific name first, since an area string naming a suburb
// usually names its city too ("Gurgaon, Delhi NCR" is Gurgaon).
var cityCentroids = []struct {
	match    string
	lat, lng float64
}{
	{"gurgaon", 28.4595, 77.0266},
	{"gurugram", 28.4595, 77.0266},
	{"noida", 28.5355, 77.3910},
	{"navi mumbai", 19.0330, 73.0297},
	{"thane", 19.2183, 72.9781},
	{"bengaluru", 12.9538, 77.6309},
	{"bangalore", 12.9538, 77.6309},
	{"mumbai", 19.0760, 72.8777},
	{"delhi", 28.6139, 77.2090},
	{"ncr", 28.6139, 77.2090},
	{"hyderabad", 17.4435, 78.3772},
	{"pune", 18.5204, 73.8567},
	{"chennai", 13.0827, 80.2707},
	{"kolkata", 22.5726, 88.3639},
	{"ahmedabad", 23.0225, 72.5714},
	{"jaipur", 26.9124, 75.7873},
	{"indore", 22.7196, 75.8577},
	{"kochi", 9.9312, 76.2673},
	{"coimbatore", 11.0168, 76.9558},
	{"chandigarh", 30.7333, 76.7794},
}

// fallbackCoordsForArea puts a pin on a company the geocoder could not place.
//
// This is a display fallback, not a measurement: it runs only where Lat/Lng are
// nil, i.e. where the pipeline has no real position. So it answers the only
// question it can honestly answer — which city — and scatters the pin across
// that city rather than claiming a street.
//
// It used to carry ~50 hand-typed sub-neighbourhood coordinates (HSR Layout,
// DLF Cyber City, Hinjawadi...) and pick one by hashing the company name. That
// is a guess wearing four decimal places: a company hashed onto Koramangala has
// no more to do with Koramangala than with Whitefield, and the precision only
// made the guess harder to spot. The city is the part that came from the data.
//
// The hash stays, because two things still depend on it: the same company must
// land on the same pin every request, and pins must not stack on one point.
// The offset is now city-scale (~±4km) instead of the old ~±300m, which is what
// "somewhere in this city" actually looks like.
func fallbackCoordsForArea(name, area string) (*float64, *float64) {
	norm := strings.ToLower(area)

	baseLat, baseLng := 12.9538, 77.6309 // Bengaluru, the directory's centre of mass
	for _, c := range cityCentroids {
		if strings.Contains(norm, c.match) {
			baseLat, baseLng = c.lat, c.lng
			break
		}
	}

	// Deterministic per company, so a pin does not move between requests.
	h := 0
	for i := 0; i < len(name); i++ {
		h = (h*31 + int(name[i])) % 10000
	}

	lat := baseLat + float64((h%80)-40)/1000.0
	lng := baseLng + float64(((h*7)%80)-40)/1000.0
	return &lat, &lng
}

// PruneDeadJobs checks active job links via fast concurrent HTTP HEAD/GET requests
// and removes any 404, 410, DNS failure, or expired postings.
// pruneBatchSize caps how many links one prune tick verifies.
//
// The prune used to fetch every job URL in the table on every run. At 4,000
// roles that is a five-minute job; at the 300,000 this pipeline is built to
// reach it is 300,000 requests against other people's servers twice a day —
// unfinishable inside its window, and exactly the traffic shape that gets a
// crawler blocked. A bounded slice ordered by last_checked_at makes it a
// rotation instead: every link is still reached, just not all in one tick.
const pruneBatchSize = 3000

// jobLinkIsGone reports whether a posting URL is definitively gone.
//
// It goes through SafeExternalGetCtx like every other third-party fetch in the
// package, so the prune queues behind a host's pacing gate and sees its 429s
// instead of hammering past both with a client of its own — a batch of 3,000
// links is exactly the traffic shape that gate exists for.
//
// That routing brings one new failure mode with it, and it decides the shape of
// this function: awaitHostSlot answers with ErrHostBusy or ErrHostThrottled
// when it will not let a request through. Those say nothing about the posting,
// and the old code's rule — any error means dead — would have read them as a
// closed role and deleted a live one. So only an answer we actually got back
// counts: a 404 or 410 from the server, or the one host we know serves a page
// for a posting that no longer exists. Anything else leaves the row alone for
// the next tick, the same way an empty ATS read does not clear a board.
func jobLinkIsGone(targetURL string) bool {
	if strings.Contains(targetURL, "wellfound.com") {
		return true
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// HEAD first: a posting page can be large and the status is all we want.
	// Plenty of boards reject HEAD outright, so a non-answer here is not an
	// answer about the posting — only a GET's verdict is trusted below.
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, targetURL, nil)
	if err != nil {
		return false // not a URL we can ask about; nothing to conclude
	}
	if resp, err := SafeExternalDo(ctx, req); err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
			return true
		}
		if resp.StatusCode < 400 {
			return false
		}
		// 4xx that is not 404/410, or a 5xx: fall through and ask properly.
	}

	resp, err := SafeExternalGetCtx(ctx, targetURL)
	if err != nil {
		return false // throttled, busy, or unreachable — not evidence
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone
}

func PruneDeadJobs() (int, error) {
	var jobs []models.Job
	// Board-sourced roles are deliberately excluded. replaceJobsForCompany
	// already makes the jobs table match the board exactly on every sync, so a
	// closed Greenhouse posting is deleted by the sync that saw it vanish;
	// re-fetching those links asks a question whose answer we already have.
	// What is left is careers-page roles, which have no authoritative feed
	// behind them and are the only ones that can rot without anyone noticing.
	if err := config.DB.
		Where("source = ?", careersPageSource).
		Order("last_checked_at ASC NULLS FIRST").
		Limit(pruneBatchSize).
		Find(&jobs).Error; err != nil {
		return 0, fmt.Errorf("failed to load jobs: %w", err)
	}

	if len(jobs) == 0 {
		return 0, nil
	}

	deadIDs := make([]string, 0)
	var mu sync.Mutex
	sem := make(chan struct{}, 15) // Max 15 concurrent health checks
	var wg sync.WaitGroup

	for _, j := range jobs {
		wg.Add(1)
		go func(job models.Job) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					log.Printf("prune: recovered while checking %q: %v", job.URL, r)
				}
			}()
			sem <- struct{}{}
			defer func() { <-sem }()

			if jobLinkIsGone(job.URL) {
				mu.Lock()
				deadIDs = append(deadIDs, job.ID)
				mu.Unlock()
			}
		}(j)
	}

	wg.Wait()

	// Stamped whether or not the link turned out to be dead, so the next tick
	// moves on to rows this one did not reach. Without it the same oldest
	// batch would be re-checked every tick and the rotation would never
	// advance past its first slice.
	for start := 0; start < len(jobs); start += 1000 {
		end := start + 1000
		if end > len(jobs) {
			end = len(jobs)
		}
		ids := make([]string, 0, end-start)
		for _, j := range jobs[start:end] {
			ids = append(ids, j.ID)
		}
		config.DB.Model(&models.Job{}).Where("id IN ?", ids).
			Update("last_checked_at", time.Now())
	}

	if len(deadIDs) > 0 {
		log.Printf("Pruning %d dead jobs from database...", len(deadIDs))
		if err := config.DB.Where("id IN ?", deadIDs).Delete(&models.Job{}).Error; err != nil {
			return 0, fmt.Errorf("failed to delete dead jobs: %w", err)
		}
		// The counter has to follow the deletion, or a company keeps a badge
		// advertising roles that were just removed. Only the companies this
		// batch actually touched are recounted.
		affected := map[string]bool{}
		for _, j := range jobs {
			affected[j.CompanyID] = true
		}
		for id := range affected {
			var n int64
			config.DB.Model(&models.Job{}).Where("company_id = ?", id).Count(&n)
			config.DB.Model(&models.Company{}).Where("id = ?", id).Update("open_roles", n)
		}
	}

	return len(deadIDs), nil
}

// listableHaving drops the companies the pipeline has no way to read.
//
// A company with no board AND no roles has no listings source at all: ATS
// detection ran against its careers page and found nothing, and the free
// careers-page tiers returned nothing either. Listing it states a fact the
// directory does not have — a visitor reads a card with no roles as "not
// hiring", when the truth is "we cannot see".
//
// This is deliberately NOT the same as hiding every company without open
// roles. A company whose board we can read and which has nothing open today
// is a real answer, and the directory is a map of the ecosystem rather than a
// jobs board — defaulting the whole list to hiring-only once hid the map
// itself. So the line is drawn at readable-or-not, not at hiring-or-not:
// an empty read is not the same as not hiring.
//
// Applied only to the default listing. hiringOnly is a stricter filter that
// already excludes these, and the stats strip counts them separately so the
// two never disagree.
const listableCondition = "open_roles > 0 OR COALESCE(ats_slug, '') <> ''"

// jobFacetCondition narrows a jobs query to one field and/or level bucket.
//
// The COALESCE/NULLIF defaults are the ones JobFacets counts by, and they have
// to stay identical: the chips show a count taken from that expression, so a
// filter written any other way would return a different number of roles than
// the chip the user clicked promised — and "Other"/"Unspecified", which are
// only ever produced by these defaults, would match nothing at all.
func jobFacetCondition(db *gorm.DB, field, level string) *gorm.DB {
	if field != "" {
		db = db.Where("COALESCE(NULLIF(jobs.field, ''), 'Other') = ?", field)
	}
	if level != "" {
		db = db.Where("COALESCE(NULLIF(jobs.level, ''), 'Unspecified') = ?", level)
	}
	return db
}

// ListCompanies returns a filtered, paginated slice of the company directory.
// hiringOnly restricts it to companies with at least one open role — most
// companies aren't hiring at any given moment, so browsing the full list is
// only useful when you want the directory rather than the jobs.
//
// field and level narrow the list to companies with at least one role in that
// bucket. They cost a correlated subquery, which is why they are opt-in: with
// both empty the query is exactly the single-table scan described below, and
// that is the common case.
func ListCompanies(sector, stage, area, q, field, level string, hiringOnly bool, page, pageSize int) ([]CompanyWithJobCount, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 500 {
		pageSize = 24
	}

	byFacet := field != "" || level != ""

	applyFilters := func() *gorm.DB {
		dbQuery := config.DB.Model(&models.Company{})
		dbQuery = applyFacetFilter(dbQuery, "sector", sector, "")
		dbQuery = applyFacetFilter(dbQuery, "stage", stage, "")
		dbQuery = applyAreaFilter(dbQuery, area, "")
		if q != "" {
			dbQuery = dbQuery.Where("name ILIKE ? OR description ILIKE ?", "%"+q+"%", "%"+q+"%")
		}
		if byFacet {
			// EXISTS rather than a join: a company with twelve matching roles
			// is still one row, and the planner can stop at the first match
			// instead of counting them to throw the count away.
			sub := jobFacetCondition(
				config.DB.Model(&models.Job{}).Select("1").Where("jobs.company_id = companies.id"),
				field, level)
			dbQuery = dbQuery.Where("EXISTS (?)", sub)
		}
		return dbQuery
	}

	// No join, no GROUP BY, no HAVING, and no ORDER BY over an aggregate.
	//
	// All four came from counting a company's roles at read time, which meant
	// aggregating the whole companies-to-jobs join before the LIMIT could
	// apply — twice per request, since the total was counted the same way.
	// Nothing can index an ORDER BY COUNT(), so that plan gets slower with
	// every company the harvest adds and there is no version of it that does
	// not. companies.open_roles holds the same number, maintained where the
	// roles are written and repaired on a schedule (directory_counters.go),
	// so all of it collapses into an indexed scan of one table.
	base := func() *gorm.DB {
		q := applyFilters()
		if hiringOnly {
			return q.Where("open_roles > 0")
		}
		return q.Where(listableCondition)
	}

	var total int64
	if err := base().Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// job_count is what the card's "N open" badge shows and what its button
	// offers to open, so under a facet it has to count that bucket. Left as
	// companies.open_roles it advertised 40 roles and then listed the 3 the
	// filter actually kept.
	listQuery := base()
	if byFacet {
		countSub := jobFacetCondition(
			config.DB.Model(&models.Job{}).Select("COUNT(*)").Where("jobs.company_id = companies.id"),
			field, level)
		listQuery = listQuery.Select("companies.*, (?) AS job_count", countSub)
	} else {
		listQuery = listQuery.Select("companies.*, companies.open_roles AS job_count")
	}

	var companies []CompanyWithJobCount
	err := listQuery.
		// Hiring companies first, then most recently discovered.
		Order("companies.open_roles DESC, companies.created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&companies).Error

	if err == nil {
		for i := range companies {
			// Only where the pipeline has no real position for this company.
			// Overwriting unconditionally discarded the coordinates
			// geocodeArea measured from the area the board itself stated, so
			// the grid and the map showed a hash of the company name jittered
			// around a hub while the detail endpoint showed the truth — the
			// card and the pin disagreed about which city the work is in.
			if companies[i].Lat == nil || companies[i].Lng == nil {
				lat, lng := fallbackCoordsForArea(companies[i].Name, companies[i].Area)
				companies[i].Lat = lat
				companies[i].Lng = lng
			}
		}
	}

	return companies, total, err
}

// TotalOpenRoles returns the number of open roles matching the same filters,
// for the "N open roles across M companies" header.
func TotalOpenRoles(sector, stage, area, q, field, level string) (int64, error) {
	dbQuery := config.DB.Model(&models.Job{}).
		Joins("JOIN companies ON companies.id = jobs.company_id")
	dbQuery = applyFacetFilter(dbQuery, "sector", sector, "companies")
	dbQuery = applyFacetFilter(dbQuery, "stage", stage, "companies")
	dbQuery = applyAreaFilter(dbQuery, area, "companies")
	if q != "" {
		dbQuery = dbQuery.Where("companies.name ILIKE ? OR companies.description ILIKE ?", "%"+q+"%", "%"+q+"%")
	}
	dbQuery = jobFacetCondition(dbQuery, field, level)

	var n int64
	err := dbQuery.Count(&n).Error
	return n, err
}

// TotalOpenRolesFast sums companies.open_roles under the company-level
// filters, reading one table instead of joining to jobs and counting rows.
//
// Used when there is no text search, which is the common case — a text search
// still has to reach the jobs table because it matches on company name and
// description, and the exact count there is worth the join.
func TotalOpenRolesFast(sector, stage, area string) (int64, error) {
	dbQuery := config.DB.Model(&models.Company{})
	dbQuery = applyFacetFilter(dbQuery, "sector", sector, "")
	dbQuery = applyFacetFilter(dbQuery, "stage", stage, "")
	dbQuery = applyAreaFilter(dbQuery, area, "")

	// sql.NullInt64 rather than a *int64, because SUM over no matching rows is
	// NULL and the destination has to be able to hold that. Scanning into a
	// **int64 is not a shape the driver accepts, and the header this feeds
	// would have failed for every filter that matches nothing — which is the
	// case a visitor reaches by narrowing, not an exotic one.
	var total sql.NullInt64
	if err := dbQuery.Select("COALESCE(SUM(open_roles), 0)").Scan(&total).Error; err != nil {
		return 0, err
	}
	return total.Int64, nil
}

func extractDomain(website string) string {
	if website == "" {
		return ""
	}
	if !strings.Contains(website, "://") {
		website = "https://" + website
	}
	parsed, err := url.Parse(website)
	if err != nil || parsed.Host == "" {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(parsed.Host), "www.")
}

func slugify(name string) string {
	slug := strings.ToLower(strings.TrimSpace(name))
	slug = nonSlugChars.ReplaceAllString(slug, "-")
	return strings.Trim(slug, "-")
}

type nominatimResult struct {
	Lat string `json:"lat"`
	Lon string `json:"lon"`
}

// Nominatim's usage policy caps clients at 1 request/second. Exceeding it
// gets your IP blocked, so every call goes through this limiter. It's a
// package-level mutex rather than a token bucket because the calls are
// infrequent and strictly serialised — simpler, and impossible to leak.
var (
	geocodeMu       sync.Mutex
	geocodeLastCall time.Time
)

const geocodeMinInterval = 1100 * time.Millisecond // 1s policy + headroom

// throttleGeocode blocks until at least geocodeMinInterval has passed since
// the previous Nominatim request.
func throttleGeocode() {
	geocodeMu.Lock()
	defer geocodeMu.Unlock()

	if wait := geocodeMinInterval - time.Since(geocodeLastCall); wait > 0 {
		time.Sleep(wait)
	}
	geocodeLastCall = time.Now()
}

// geocodeArea resolves a free-text city/locality string to coordinates via
// the free Nominatim (OpenStreetMap) API. Called at most once per new
// company since results are cached on the row.
func geocodeArea(area string) (*float64, *float64, error) {
	if area == "" {
		return nil, nil, fmt.Errorf("empty area")
	}

	// The discovery agent sometimes returns a compound area like
	// "Noida/Gurugram, Delhi NCR". Nominatim can't resolve that and returns
	// nothing, leaving the company with no map pin at all. Try progressively
	// simpler forms until one resolves — a slightly-off pin beats no pin.
	for _, candidate := range geocodeCandidates(area) {
		if lat, lng, err := geocodeOnce(candidate); err == nil {
			return lat, lng, nil
		}
	}
	return nil, nil, fmt.Errorf("no geocode result for %q", area)
}

// geocodeCandidates expands an area string into fallbacks, most specific
// first: the original, then without the "A/B" alternatives, then just the
// last comma-separated part (usually the city or region).
func geocodeCandidates(area string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}

	add(area)

	// "Noida/Gurugram, Delhi NCR" -> "Noida, Delhi NCR"
	if strings.Contains(area, "/") {
		parts := strings.SplitN(area, "/", 2)
		head := parts[0]
		if i := strings.Index(parts[1], ","); i >= 0 {
			head += parts[1][i:]
		}
		add(head)
	}

	// Fall back to the last comma-separated component: "Delhi NCR"
	if i := strings.LastIndex(area, ","); i >= 0 {
		add(area[i+1:])
	}

	return out
}

func geocodeOnce(area string) (*float64, *float64, error) {
	throttleGeocode() // respect Nominatim's 1 req/sec policy

	endpoint := "https://nominatim.openstreetmap.org/search?format=json&limit=1&q=" + url.QueryEscape(area)
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", "NeuroFIQ-JobMap/1.0 (contact: tech.revmerito@gmail.com)")

	resp, err := externalClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var results []nominatimResult
	if err := json.Unmarshal(body, &results); err != nil || len(results) == 0 {
		return nil, nil, fmt.Errorf("no geocode result for %q", area)
	}

	var lat, lng float64
	if _, err := fmt.Sscanf(results[0].Lat, "%f", &lat); err != nil {
		return nil, nil, err
	}
	if _, err := fmt.Sscanf(results[0].Lon, "%f", &lng); err != nil {
		return nil, nil, err
	}

	return &lat, &lng, nil
}

// DirectoryStats is the headline count strip on the Job Map: how big the
// directory is, and how much of it arrived recently.
//
// These are deliberately unfiltered — they describe the directory itself, not
// whatever the visitor has narrowed it to, so the numbers stay stable while
// someone clicks through sectors and stages.
type DirectoryStats struct {
	Companies       int64      `json:"companies"`
	HiringCompanies int64      `json:"hiring_companies"`
	Jobs            int64      `json:"jobs"`
	NewJobs24h      int64      `json:"new_jobs_24h"`
	NewJobs7d       int64      `json:"new_jobs_7d"`
	LastJobAt       *time.Time `json:"last_job_at"`
}

// GetDirectoryStats counts the whole directory in one place.
//
// "New" is measured against jobs.created_at, which survives a re-sync: the
// upsert in replaceJobsForCompany only overwrites title, department and
// location, so a role keeps the timestamp of the run that first saw it. A
// job that closes and is re-posted does count as new again, which is the
// honest answer — it is a fresh opening.
func GetDirectoryStats() (DirectoryStats, error) {
	var s DirectoryStats

	// Counted the same way the default listing filters, or the strip would
	// advertise more companies than the grid below it can show. Sharing the
	// one condition is what keeps that true — this count and ListCompanies
	// disagreeing is a bug a visitor sees directly.
	if err := config.DB.Model(&models.Company{}).
		Where(listableCondition).Count(&s.Companies).Error; err != nil {
		return s, err
	}
	if err := config.DB.Model(&models.Job{}).Count(&s.Jobs).Error; err != nil {
		return s, err
	}
	if err := config.DB.Model(&models.Job{}).
		Distinct("company_id").Count(&s.HiringCompanies).Error; err != nil {
		return s, err
	}

	now := time.Now()
	if err := config.DB.Model(&models.Job{}).
		Where("created_at >= ?", now.Add(-24*time.Hour)).Count(&s.NewJobs24h).Error; err != nil {
		return s, err
	}
	if err := config.DB.Model(&models.Job{}).
		Where("created_at >= ?", now.Add(-7*24*time.Hour)).Count(&s.NewJobs7d).Error; err != nil {
		return s, err
	}

	// When the newest job was stored — this is what tells a visitor the
	// pipeline is alive, rather than showing a confident zero from a sync
	// that has quietly not run for days.
	var newest models.Job
	if err := config.DB.Order("created_at DESC").First(&newest).Error; err == nil {
		s.LastJobAt = &newest.CreatedAt
	}

	return s, nil
}
