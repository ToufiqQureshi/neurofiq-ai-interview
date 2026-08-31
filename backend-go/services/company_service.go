package services

import (
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

	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/config"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type discoveredCompanyDTO struct {
	Name        string `json:"name"`
	Website     string `json:"website"`
	Description string `json:"description"`
	Sector      string `json:"sector"`
	Stage       string `json:"stage"`
	Area        string `json:"area"`
	CareersURL  string `json:"careers_url"`
}

type discoverCompaniesResponse struct {
	Companies []discoveredCompanyDTO `json:"companies"`
}

var nonSlugChars = regexp.MustCompile(`[^a-z0-9]+`)

// Noise words stripped when comparing company names for duplicates. The
// discovery agent returns the same company under different labels — e.g.
// "BYJU'S Exam Prep" and "BYJU'S Exam Prep (Gradeup)" arrived with
// different domains, so domain-only dedupe let both through.
var (
	parentheticalRe = regexp.MustCompile(`\([^)]*\)`)
	companyNoiseRe  = regexp.MustCompile(`\b(private|pvt|limited|ltd|llp|inc|incorporated|corp|corporation|technologies|technology|tech|solutions|systems|labs|software|services|india|global|group|company|co)\b`)
)

// normalizeCompanyName reduces a name to a comparable key: lowercased, with
// parentheticals, legal suffixes and punctuation removed.
//
//	"BYJU'S Exam Prep (Gradeup)"      -> "byjusexamprep"
//	"Edunext Technologies Pvt. Ltd."  -> "edunext"
func normalizeCompanyName(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = parentheticalRe.ReplaceAllString(s, " ")
	s = companyNoiseRe.ReplaceAllString(s, " ")
	return nonSlugChars.ReplaceAllString(s, "")
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

// Seed queries are generated as city × sector × phrasing rather than
// hand-listed, so coverage grows without maintaining a list by hand.
//
// The phrasings matter: asking the same question three different ways
// surfaces different companies, because the underlying web search returns
// different results for "AI startups in Bangalore" vs "AI companies hiring
// in Bangalore".
var (
	seedCities = []string{
		"Bangalore", "Mumbai", "Delhi NCR", "Pune", "Hyderabad", "Chennai",
		"Gurgaon", "Noida", "Ahmedabad", "Kolkata", "Jaipur", "Indore",
	}
	seedSectors = []string{
		"AI", "Fintech", "SaaS", "Healthtech", "Edtech", "D2C",
		"Logistics", "Deeptech", "Consumer", "Gaming",
	}
	seedPhrasings = []string{
		"%s startups in %s",
		"funded %s companies in %s",
		"%s startups hiring in %s",
	}

	seedQueries = buildSeedQueries()
)

// discoveryIntervalSeconds must match the cron schedule in main.go — the
// rotation cursor is derived from it, so a mismatch would skip or repeat
// queries. Hourly gets through all 360 seeds in ~15 days.
const discoveryIntervalSeconds = 3600

// buildSeedQueries expands the city/sector/phrasing lists into the full
// rotation. 12 cities × 10 sectors × 3 phrasings = 360 distinct queries.
func buildSeedQueries() []string {
	out := make([]string, 0, len(seedCities)*len(seedSectors)*len(seedPhrasings))
	for _, city := range seedCities {
		for _, sector := range seedSectors {
			for _, phrasing := range seedPhrasings {
				out = append(out, fmt.Sprintf(phrasing, sector, city))
			}
		}
	}
	return out
}

// DiscoverCompanies asks the ai-worker's Agno discovery agent to find real
// companies for the given query, then upserts them into the companies table
// (deduped by domain).
func DiscoverCompanies(query string, limit int) ([]models.Company, error) {
	dtos, err := callDiscoveryAgent(query, limit)
	if err != nil {
		return nil, err
	}

	var saved []models.Company
	for _, dto := range dtos {
		domain := extractDomain(dto.Website)
		if domain == "" {
			continue
		}

		company := models.Company{
			Name:        dto.Name,
			Slug:        slugify(dto.Name),
			Description: dto.Description,
			Website:     dto.Website,
			Domain:      domain,
			Sector:      dto.Sector,
			Stage:       dto.Stage,
			Area:        dto.Area,
			CareersURL:  dto.CareersURL,
			Source:      "agno-discovery",
		}

		// Skip anything we already have — same domain, or the same business
		// under a differently-worded name.
		if dup := findDuplicateCompany(dto.Name, domain); dup != nil {
			continue
		}

		// No careers page, no entry.
		//
		// This is a job map: a company that cannot be shown with roles is
		// noise in it. The agent's search reliably surfaces small D2C shops
		// alongside real startups — of the companies we had stored with no
		// careers page at all, 62% were D2C and 88% were pre-Series-A. They
		// have no careers page because they do not hire.
		//
		// The resolver is free (plain HTTP probes of /careers, /jobs and
		// friends), so this gate costs no scraping credits, only the probe.
		if company.CareersURL == "" {
			company.CareersURL = ResolveCareersURL(company)
		}
		if company.CareersURL == "" {
			log.Printf("company discovery: skipping %q — no careers page found", dto.Name)
			continue
		}

		if lat, lng, geoErr := geocodeArea(dto.Area); geoErr == nil {
			company.Lat = lat
			company.Lng = lng
		}

		result := config.DB.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "domain"}},
			DoNothing: true,
		}).Create(&company)
		if result.Error != nil {
			log.Printf("company discovery: failed to save %q: %v", dto.Name, result.Error)
			continue
		}
		if result.RowsAffected == 0 {
			// Domain already existed; ON CONFLICT DO NOTHING skipped the insert.
			continue
		}
		saved = append(saved, company)

		// Pull real open roles for this company right away, so it doesn't
		// sit with zero jobs until the next cron re-sync.
		if _, jobErr := SyncJobsForCompany(company); jobErr != nil {
			log.Printf("company discovery: job sync failed for %q: %v", company.Name, jobErr)
		}
	}

	return saved, nil
}

// CompanyWithJobCount is a Company row annotated with how many currently
// open roles we have for it, so the directory can show a "N open" badge
// without an N+1 query per card.
type CompanyWithJobCount struct {
	models.Company
	JobCount int64 `json:"job_count"`
}

// ListCompanies returns a filtered, paginated slice of the company directory.
// hiringOnly restricts it to companies with at least one open role — most
// companies aren't hiring at any given moment, so browsing the full list is
// only useful when you want the directory rather than the jobs.
func ListCompanies(sector, stage, area, q string, hiringOnly bool, page, pageSize int) ([]CompanyWithJobCount, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 500 {
		pageSize = 24
	}

	applyFilters := func() *gorm.DB {
		dbQuery := config.DB.Model(&models.Company{})
		if sector != "" {
			dbQuery = dbQuery.Where("sector = ?", sector)
		}
		if stage != "" {
			dbQuery = dbQuery.Where("stage = ?", stage)
		}
		if area != "" {
			dbQuery = dbQuery.Where("area ILIKE ?", "%"+area+"%")
		}
		if q != "" {
			dbQuery = dbQuery.Where("name ILIKE ? OR description ILIKE ?", "%"+q+"%", "%"+q+"%")
		}
		return dbQuery
	}

	base := func() *gorm.DB {
		q := applyFilters().
			Select("companies.*, COUNT(jobs.id) AS job_count").
			Joins("LEFT JOIN jobs ON jobs.company_id = companies.id").
			Group("companies.id")
		if hiringOnly {
			q = q.Having("COUNT(jobs.id) > 0")
		}
		return q
	}

	// Count the grouped rows, not the raw join, or companies with many jobs
	// would be counted once per job.
	var total int64
	if err := config.DB.Table("(?) AS grouped", base()).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var companies []CompanyWithJobCount
	err := base().
		// Hiring companies first, then most recently discovered.
		Order("COUNT(jobs.id) DESC, companies.created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&companies).Error

	return companies, total, err
}

// TotalOpenRoles returns the number of open roles matching the same filters,
// for the "N open roles across M companies" header.
func TotalOpenRoles(sector, stage, area, q string) (int64, error) {
	dbQuery := config.DB.Model(&models.Job{}).
		Joins("JOIN companies ON companies.id = jobs.company_id")
	if sector != "" {
		dbQuery = dbQuery.Where("companies.sector = ?", sector)
	}
	if stage != "" {
		dbQuery = dbQuery.Where("companies.stage = ?", stage)
	}
	if area != "" {
		dbQuery = dbQuery.Where("companies.area ILIKE ?", "%"+area+"%")
	}
	if q != "" {
		dbQuery = dbQuery.Where("companies.name ILIKE ? OR companies.description ILIKE ?", "%"+q+"%", "%"+q+"%")
	}

	var n int64
	err := dbQuery.Count(&n).Error
	return n, err
}

// RunDiscoveryRotation is invoked on a schedule (see main.go cron) and picks
// the next seed query deterministically from the current time, so the
// rotation survives restarts without needing extra state in the database.
// discoveryLeaseName is the key every API instance competes for before
// running the hourly rotation.
const DiscoveryLeaseName = "discovery-rotation"

func RunDiscoveryRotation() {
	if len(seedQueries) == 0 {
		return
	}

	// Only one instance runs this tick. The cron scheduler lives inside the
	// API process, so scaling to two containers would otherwise mean two
	// discovery runs an hour: double the LLM spend, double the scraper
	// credits, and every job board fetched twice. The lease is slightly
	// shorter than the interval so a crashed instance frees it before the
	// next tick is due.
	if !AcquireCronLease(DiscoveryLeaseName, 55*time.Minute) {
		log.Printf("company discovery rotation: another instance holds the lease, skipping")
		return
	}
	// Deliberately NOT released when the run finishes. Each process registers
	// its own "@every 1h" schedule, so instance ticks are not aligned: if A
	// finishes at :05 and B ticks at :25, B would take the freed lease and —
	// because the query index is derived from the current hour — run the
	// identical query again. That is exactly the duplicated LLM spend and
	// duplicated board fetching the lease exists to prevent. The 55-minute
	// TTL covers the rest of the interval; shutdown releases it explicitly.
	// The clock is the cursor: derive which query is next from the current
	// hour rather than storing a position. Restart-proof, redeploy-proof,
	// no cursor table to keep in sync. Must match the cron interval in
	// main.go, otherwise the rotation skips or repeats queries.
	idx := int((time.Now().Unix() / int64(discoveryIntervalSeconds)) % int64(len(seedQueries)))
	query := seedQueries[idx]

	// Discovery and job sync are deliberately independent. Discovery depends
	// on the AI worker and a live web search, so it's the flakier half — if
	// it fails we still want to refresh jobs for the companies we already
	// have, rather than skipping the whole tick.
	if saved, err := DiscoverCompanies(query, 10); err != nil {
		log.Printf("company discovery rotation failed for %q: %v", query, err)
	} else {
		log.Printf("company discovery rotation: %q -> %d new companies saved", query, len(saved))
	}

	// Refresh open roles for everything we already track, so closed
	// postings drop off and newly-posted ones appear.
	SyncAllCompanyJobs()
}

func callDiscoveryAgent(query string, limit int) ([]discoveredCompanyDTO, error) {
	// The discovery agent does a live web search plus an LLM call, so it's
	// slow — but it must never hang forever. Without a timeout a stuck
	// worker blocks the whole cron tick, including the job sync that runs
	// after it. discoveryClient carries the 3-minute ceiling.
	payload := map[string]interface{}{"query": query, "limit": limit}

	body, err := postToWorker(discoveryClient, "/internal/discover-companies", payload)
	if err != nil {
		return nil, err
	}

	var parsed discoverCompaniesResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse discovery JSON: %w", err)
	}

	return parsed.Companies, nil
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
