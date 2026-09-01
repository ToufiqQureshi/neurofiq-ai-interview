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
// queries. At one minute the full 360-seed rotation completes in ~90
// minutes rather than 15 days.
const discoveryIntervalSeconds = 60

// queriesPerTick is how many seed queries each tick runs, in parallel.
// Together with the interval this sets the discovery rate; the ceiling is
// the search and LLM quota behind the agent, not anything in this process,
// so it is small and tunable rather than as high as the machine allows.
const queriesPerTick = 4

// buildSeedQueries expands the city/sector/phrasing lists into the full
// rotation. 12 cities × 10 sectors × 3 phrasings = 360 distinct queries.
//
// City is the INNERMOST loop on purpose. With city outermost the cursor
// spent 30 consecutive ticks in one city before moving on, so the directory
// filled up city-block by city-block: after four days it held 22 Noida and
// 15 Kolkata companies and not one from Bangalore, which sat at indices
// 0-29 and was days away from being asked about. Iterating city innermost
// means every consecutive tick asks a different city, so coverage spreads
// evenly from the first hour instead of arriving in lumps.
func buildSeedQueries() []string {
	out := make([]string, 0, len(seedCities)*len(seedSectors)*len(seedPhrasings))
	for _, sector := range seedSectors {
		for _, phrasing := range seedPhrasings {
			for _, city := range seedCities {
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
	switch {
	case strings.Contains(norm, "delhi") || strings.Contains(norm, "ncr") || strings.Contains(norm, "noida") || strings.Contains(norm, "gurgaon") || strings.Contains(norm, "gurugram"):
		return db.Where(col+" ILIKE ? OR "+col+" ILIKE ? OR "+col+" ILIKE ? OR "+col+" ILIKE ?", "%noida%", "%gurgaon%", "%gurugram%", "%delhi%")
	case strings.Contains(norm, "bengaluru") || strings.Contains(norm, "bangalore"):
		return db.Where(col+" ILIKE ? OR "+col+" ILIKE ? OR "+col+" ILIKE ? OR "+col+" ILIKE ? OR "+col+" ILIKE ? OR "+col+" ILIKE ?", "%bengaluru%", "%bangalore%", "%hsr%", "%koramangala%", "%indiranagar%", "%whitefield%")
	case strings.Contains(norm, "mumbai"):
		return db.Where(col+" ILIKE ? OR "+col+" ILIKE ? OR "+col+" ILIKE ?", "%mumbai%", "%navi mumbai%", "%thane%")
	case strings.Contains(norm, "hyderabad"):
		return db.Where(col+" ILIKE ? OR "+col+" ILIKE ?", "%hyderabad%", "%hitec%")
	case strings.Contains(norm, "pune"):
		return db.Where(col+" ILIKE ?", "%pune%")
	default:
		return db.Where(col+" ILIKE ?", "%"+area+"%")
	}
}

// techSubHub represents a real, defined startup corridor in an Indian tech city
type techSubHub struct {
	Name string
	Lat  float64
	Lng  float64
}

var bangaloreTechHubs = []techSubHub{
	{Name: "HSR Layout (Startup Corridor)", Lat: 12.9121, Lng: 77.6446},
	{Name: "Koramangala (VC & Unicorn Hub)", Lat: 12.9352, Lng: 77.6245},
	{Name: "Indiranagar (100ft / 12th Main)", Lat: 12.9784, Lng: 77.6408},
	{Name: "Outer Ring Road (Bellandur / Ecospace)", Lat: 12.9260, Lng: 77.6762},
	{Name: "Domlur (Embassy GolfLinks EGL)", Lat: 12.9610, Lng: 77.6387},
	{Name: "Whitefield (ITPL & EPIP Zone)", Lat: 12.9698, Lng: 77.7500},
	{Name: "CBD (MG Road / Church Street)", Lat: 12.9756, Lng: 77.6066},
	{Name: "JP Nagar & Jayanagar", Lat: 12.9063, Lng: 77.5857},
	{Name: "Electronic City Phase 1", Lat: 12.8452, Lng: 77.6602},
	{Name: "Hebbal (Manyata Tech Park)", Lat: 13.0458, Lng: 77.6200},
}

var delhiNCRTechHubs = []techSubHub{
	{Name: "DLF Cyber City / Cyber Hub Gurgaon", Lat: 28.4907, Lng: 77.0898},
	{Name: "Golf Course Road Gurgaon", Lat: 28.4414, Lng: 77.1065},
	{Name: "Udyog Vihar Phase 1-5 Gurgaon", Lat: 28.5085, Lng: 77.0817},
	{Name: "Sohna Road Gurgaon", Lat: 28.4125, Lng: 77.0425},
	{Name: "Sector 62 Institutional Area Noida", Lat: 28.6279, Lng: 77.3749},
	{Name: "Noida Expressway (Sector 125/142)", Lat: 28.5448, Lng: 77.3331},
	{Name: "Sector 16/18 Film City Noida", Lat: 28.5708, Lng: 77.3160},
	{Name: "Okhla Phase 3 / South Delhi", Lat: 28.5355, Lng: 77.2718},
	{Name: "Connaught Place / Central Delhi", Lat: 28.6315, Lng: 77.2167},
}

var mumbaiTechHubs = []techSubHub{
	{Name: "BKC (Bandra Kurla Complex)", Lat: 19.0657, Lng: 72.8687},
	{Name: "Andheri East (MIDC & Chakala)", Lat: 19.1136, Lng: 72.8697},
	{Name: "Lower Parel & Worli Corporate Hub", Lat: 18.9986, Lng: 72.8278},
	{Name: "Powai (Hiranandani & IIT Bombay)", Lat: 19.1176, Lng: 72.9060},
	{Name: "Navi Mumbai (Airoli & Mahape)", Lat: 19.0330, Lng: 73.0297},
	{Name: "Thane West IT Parks", Lat: 19.2183, Lng: 72.9781},
}

var puneTechHubs = []techSubHub{
	{Name: "Hinjawadi Phase 1 & 2 Rajiv Gandhi Tech Park", Lat: 18.5912, Lng: 73.7389},
	{Name: "Kharadi (EON Free Zone & WTC)", Lat: 18.5516, Lng: 73.9520},
	{Name: "Baner & Balewadi High Street", Lat: 18.5590, Lng: 73.7868},
	{Name: "Kalyani Nagar & Koregaon Park", Lat: 18.5529, Lng: 73.9014},
	{Name: "Magarpatta Cybercity Hadapsar", Lat: 18.5158, Lng: 73.9272},
}

var hyderabadTechHubs = []techSubHub{
	{Name: "HITEC City & Cyber Towers", Lat: 17.4504, Lng: 78.3808},
	{Name: "Gachibowli & Financial District", Lat: 17.4401, Lng: 78.3489},
	{Name: "Madhapur Tech Corridor", Lat: 17.4483, Lng: 78.3915},
	{Name: "Kondapur", Lat: 17.4646, Lng: 78.3582},
	{Name: "Jubilee Hills & Banjara Hills", Lat: 17.4319, Lng: 78.4073},
}

// fallbackCoordsForArea provides realistic, high-precision tech-subhub coordinates
// modeled after BangaloreStartupMap and Delhi/Mumbai tech ecosystems.
func fallbackCoordsForArea(name, area string) (*float64, *float64) {
	norm := strings.ToLower(area)

	// Deterministic hash based on company name
	h := 0
	for i := 0; i < len(name); i++ {
		h = (h*31 + int(name[i])) % 10000
	}

	var baseLat, baseLng float64

	switch {
	// Specific Bengaluru sub-neighborhoods
	case strings.Contains(norm, "hsr"):
		baseLat, baseLng = 12.9121, 77.6446
	case strings.Contains(norm, "koramangala"):
		baseLat, baseLng = 12.9352, 77.6245
	case strings.Contains(norm, "indiranagar"):
		baseLat, baseLng = 12.9784, 77.6408
	case strings.Contains(norm, "whitefield"):
		baseLat, baseLng = 12.9698, 77.7500
	case strings.Contains(norm, "bellandur") || strings.Contains(norm, "outer ring"):
		baseLat, baseLng = 12.9260, 77.6762
	case strings.Contains(norm, "domlur") || strings.Contains(norm, "egl"):
		baseLat, baseLng = 12.9610, 77.6387
	case strings.Contains(norm, "electronic city"):
		baseLat, baseLng = 12.8452, 77.6602
	case strings.Contains(norm, "bengaluru") || strings.Contains(norm, "bangalore"):
		// Distribute across authentic Bangalore startup corridors
		hub := bangaloreTechHubs[h%len(bangaloreTechHubs)]
		baseLat, baseLng = hub.Lat, hub.Lng

	// Specific Delhi NCR sub-neighborhoods
	case strings.Contains(norm, "cyber city") || strings.Contains(norm, "dlf"):
		baseLat, baseLng = 28.4907, 77.0898
	case strings.Contains(norm, "golf course"):
		baseLat, baseLng = 28.4414, 77.1065
	case strings.Contains(norm, "udyog vihar"):
		baseLat, baseLng = 28.5085, 77.0817
	case strings.Contains(norm, "sohna"):
		baseLat, baseLng = 28.4125, 77.0425
	case strings.Contains(norm, "noida 62") || strings.Contains(norm, "sector 62"):
		baseLat, baseLng = 28.6279, 77.3749
	case strings.Contains(norm, "noida 125") || strings.Contains(norm, "expressway"):
		baseLat, baseLng = 28.5448, 77.3331
	case strings.Contains(norm, "noida 16") || strings.Contains(norm, "sector 16") || strings.Contains(norm, "sector 18"):
		baseLat, baseLng = 28.5708, 77.3160
	case strings.Contains(norm, "noida"):
		noidaHubs := []techSubHub{
			{Name: "Sector 62", Lat: 28.6279, Lng: 77.3749},
			{Name: "Expressway Sector 125", Lat: 28.5448, Lng: 77.3331},
			{Name: "Sector 16/18 Film City", Lat: 28.5708, Lng: 77.3160},
			{Name: "Sector 142 Advant Navis", Lat: 28.5042, Lng: 77.4147},
		}
		hub := noidaHubs[h%len(noidaHubs)]
		baseLat, baseLng = hub.Lat, hub.Lng
	case strings.Contains(norm, "gurgaon") || strings.Contains(norm, "gurugram"):
		gurgaonHubs := []techSubHub{
			{Name: "DLF Cyber City", Lat: 28.4907, Lng: 77.0898},
			{Name: "Golf Course Road", Lat: 28.4414, Lng: 77.1065},
			{Name: "Udyog Vihar", Lat: 28.5085, Lng: 77.0817},
			{Name: "Sohna Road", Lat: 28.4125, Lng: 77.0425},
		}
		hub := gurgaonHubs[h%len(gurgaonHubs)]
		baseLat, baseLng = hub.Lat, hub.Lng
	case strings.Contains(norm, "delhi") || strings.Contains(norm, "ncr"):
		hub := delhiNCRTechHubs[h%len(delhiNCRTechHubs)]
		baseLat, baseLng = hub.Lat, hub.Lng

	// Specific Mumbai sub-neighborhoods
	case strings.Contains(norm, "bkc"):
		baseLat, baseLng = 19.0657, 72.8687
	case strings.Contains(norm, "andheri"):
		baseLat, baseLng = 19.1136, 72.8697
	case strings.Contains(norm, "lower parel") || strings.Contains(norm, "worli"):
		baseLat, baseLng = 18.9986, 72.8278
	case strings.Contains(norm, "powai"):
		baseLat, baseLng = 19.1176, 72.9060
	case strings.Contains(norm, "navi mumbai"):
		baseLat, baseLng = 19.0330, 73.0297
	case strings.Contains(norm, "thane"):
		baseLat, baseLng = 19.2183, 72.9781
	case strings.Contains(norm, "mumbai"):
		hub := mumbaiTechHubs[h%len(mumbaiTechHubs)]
		baseLat, baseLng = hub.Lat, hub.Lng

	// Specific Pune sub-neighborhoods
	case strings.Contains(norm, "hinjawadi") || strings.Contains(norm, "hinjewadi"):
		baseLat, baseLng = 18.5912, 73.7389
	case strings.Contains(norm, "kharadi"):
		baseLat, baseLng = 18.5516, 73.9520
	case strings.Contains(norm, "baner"):
		baseLat, baseLng = 18.5590, 73.7868
	case strings.Contains(norm, "magarpatta"):
		baseLat, baseLng = 18.5158, 73.9272
	case strings.Contains(norm, "pune"):
		hub := puneTechHubs[h%len(puneTechHubs)]
		baseLat, baseLng = hub.Lat, hub.Lng

	// Specific Hyderabad sub-neighborhoods
	case strings.Contains(norm, "hitec") || strings.Contains(norm, "cyberabad"):
		baseLat, baseLng = 17.4504, 78.3808
	case strings.Contains(norm, "gachibowli"):
		baseLat, baseLng = 17.4401, 78.3489
	case strings.Contains(norm, "madhapur"):
		baseLat, baseLng = 17.4483, 78.3915
	case strings.Contains(norm, "hyderabad"):
		hub := hyderabadTechHubs[h%len(hyderabadTechHubs)]
		baseLat, baseLng = hub.Lat, hub.Lng

	case strings.Contains(norm, "chennai"):
		baseLat, baseLng = 13.0827, 80.2707
	default:
		hub := bangaloreTechHubs[h%len(bangaloreTechHubs)]
		baseLat, baseLng = hub.Lat, hub.Lng
	}

	// Office park micro-jitter (~150-300 meters) so pins within the same tech park do not overlap
	jitterLat := (float64((h%40)-20) / 7000.0)
	jitterLng := (float64(((h*7)%40)-20) / 7000.0)

	lat := baseLat + jitterLat
	lng := baseLng + jitterLng
	return &lat, &lng
}

// PruneDeadJobs checks active job links via fast concurrent HTTP HEAD/GET requests
// and removes any 404, 410, DNS failure, or expired postings.
func PruneDeadJobs() (int, error) {
	var jobs []models.Job
	if err := config.DB.Find(&jobs).Error; err != nil {
		return 0, fmt.Errorf("failed to load jobs: %w", err)
	}

	if len(jobs) == 0 {
		return 0, nil
	}

	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}

	deadIDs := make([]string, 0)
	var mu sync.Mutex
	sem := make(chan struct{}, 15) // Max 15 concurrent health checks
	var wg sync.WaitGroup

	for _, j := range jobs {
		wg.Add(1)
		go func(job models.Job) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			isDead := false
			targetURL := job.URL

			if strings.Contains(targetURL, "wellfound.com") {
				isDead = true
			} else {
				req, err := http.NewRequest("HEAD", targetURL, nil)
				if err == nil {
					req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
					resp, err := client.Do(req)
					if err != nil || resp.StatusCode == 404 || resp.StatusCode == 410 || resp.StatusCode == 400 {
						// Double-check with GET if HEAD was rejected
						reqGet, errGet := http.NewRequest("GET", targetURL, nil)
						if errGet == nil {
							reqGet.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
							respGet, errGetDo := client.Do(reqGet)
							if errGetDo != nil || respGet.StatusCode == 404 || respGet.StatusCode == 410 {
								isDead = true
							}
							if respGet != nil {
								respGet.Body.Close()
							}
						} else {
							isDead = true
						}
					}
					if resp != nil {
						resp.Body.Close()
					}
				}
			}

			if isDead {
				mu.Lock()
				deadIDs = append(deadIDs, job.ID)
				mu.Unlock()
			}
		}(j)
	}

	wg.Wait()

	if len(deadIDs) > 0 {
		log.Printf("Pruning %d dead jobs from database...", len(deadIDs))
		if err := config.DB.Where("id IN ?", deadIDs).Delete(&models.Job{}).Error; err != nil {
			return 0, fmt.Errorf("failed to delete dead jobs: %w", err)
		}
	}

	return len(deadIDs), nil
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
		dbQuery = applyAreaFilter(dbQuery, area, "")
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

	if err == nil {
		for i := range companies {
			lat, lng := fallbackCoordsForArea(companies[i].Name, companies[i].Area)
			companies[i].Lat = lat
			companies[i].Lng = lng
		}
	}

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
	dbQuery = applyAreaFilter(dbQuery, area, "companies")
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
	// discovery runs per tick: double the LLM spend, double the search
	// quota, and every job board fetched twice. The lease is slightly
	// shorter than the interval so a crashed instance frees it before the
	// next tick is due.
	if !AcquireCronLease(DiscoveryLeaseName, discoveryLeaseTTL) {
		return
	}
	// Deliberately NOT released when the run finishes. Each process
	// registers its own schedule, so instance ticks are not aligned: if A
	// finishes and B ticks a moment later, B would take the freed lease and
	// — because the query index is derived from the clock — run the
	// identical query again. The TTL covers the rest of the interval;
	// shutdown releases it explicitly.

	// The clock is the cursor: derive the position from the current time
	// rather than storing one. Restart-proof, redeploy-proof, no cursor
	// table to keep in sync. Must match the cron interval in main.go.
	base := int((time.Now().Unix() / int64(discoveryIntervalSeconds)) % int64(len(seedQueries)))

	// Run several seeds per tick. They are independent web searches, so they
	// overlap cleanly, and the agent call is the slow part — running them
	// one after another would leave the tick mostly idle.
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		saved int
	)
	for i := 0; i < queriesPerTick; i++ {
		query := seedQueries[(base+i*queryStride)%len(seedQueries)]
		wg.Add(1)
		go func(q string) {
			defer wg.Done()
			// Gin's Recovery() does not cover goroutines we spawn.
			defer func() {
				if r := recover(); r != nil {
					log.Printf("PANIC in discovery for %q: %v", q, r)
				}
			}()
			found, err := DiscoverCompanies(q, 10)
			if err != nil {
				log.Printf("discovery failed for %q: %v", q, err)
				return
			}
			mu.Lock()
			saved += len(found)
			mu.Unlock()
		}(query)
	}
	wg.Wait()

	if saved > 0 {
		log.Printf("discovery tick: %d queries -> %d new companies", queriesPerTick, saved)
	}
}

// discoveryLeaseTTL is set from how long a tick actually takes, not from the
// interval between ticks. A measured discovery call runs about 80 seconds
// (Exa search plus the model reading the results), so a tick of four runs
// well past the one-minute schedule. Held for only the interval, the lease
// would expire mid-run and the next tick would start on top of this one —
// each pile-up making every call slower until they all hit the worker
// timeout, which is exactly how this failed before. Holding it longer means
// the cron fires every minute but a tick only starts once the last has had
// time to finish, so the schedule self-throttles instead of collapsing.
const discoveryLeaseTTL = 2 * time.Minute

// queryStride spaces the seeds run within one tick so they land on
// different sectors as well as different cities. It is coprime with the
// rotation length, so stepping by it still visits every seed.
const queryStride = 7

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

	if err := config.DB.Model(&models.Company{}).Count(&s.Companies).Error; err != nil {
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
