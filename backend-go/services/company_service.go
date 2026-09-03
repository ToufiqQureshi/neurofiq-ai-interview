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
	if sectors, err = distinctCompanyValues("sector"); err != nil {
		return nil, nil, err
	}
	if stages, err = distinctCompanyValues("stage"); err != nil {
		return nil, nil, err
	}
	return sectors, stages, nil
}

// distinctCompanyValues reads one column's options, sorted, with a single
// Unknown entry standing for every row the pipeline left blank.
func distinctCompanyValues(column string) ([]string, error) {
	// COALESCE because the column is nullable and Pluck scans into a plain
	// string: one NULL row fails the whole scan, and the caller renders a
	// filter with no options at all. Every blank collapses into Unknown
	// immediately below, so a null and an empty string are already the same
	// answer here.
	var raw []string
	if err := config.DB.Model(&models.Company{}).
		Distinct().
		Order(column).
		Pluck("COALESCE("+column+", '')", &raw).Error; err != nil {
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
const listableHaving = "COUNT(jobs.id) > 0 OR COALESCE(companies.ats_slug, '') <> ''"

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
		dbQuery = applyFacetFilter(dbQuery, "sector", sector, "")
		dbQuery = applyFacetFilter(dbQuery, "stage", stage, "")
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
		} else {
			q = q.Having(listableHaving)
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
	dbQuery = applyFacetFilter(dbQuery, "sector", sector, "companies")
	dbQuery = applyFacetFilter(dbQuery, "stage", stage, "companies")
	dbQuery = applyAreaFilter(dbQuery, area, "companies")
	if q != "" {
		dbQuery = dbQuery.Where("companies.name ILIKE ? OR companies.description ILIKE ?", "%"+q+"%", "%"+q+"%")
	}

	var n int64
	err := dbQuery.Count(&n).Error
	return n, err
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
	// advertise more companies than the grid below it can show.
	if err := config.DB.Model(&models.Company{}).
		Joins("LEFT JOIN jobs ON jobs.company_id = companies.id").
		Group("companies.id").
		Having(listableHaving).
		Distinct("companies.id").Count(&s.Companies).Error; err != nil {
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
