package services

import (
	"encoding/json"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/config"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/models"
)

// The startup register as a source of company *information*.
//
// This path exists for a different reason from the Common Crawl one, and the
// difference is worth being precise about, because measuring it is what
// decided the design.
//
// A sample of 50 companies taken at random from the register found websites on
// all 50, of which half no longer resolved, and an applicant-tracking system
// on none of them. That is not a surprise in hindsight: the register is every
// company that applied for startup recognition, and most of them are one and
// two person firms that will never run a hiring board. Crawling all 270,000 of
// those pages would be roughly 810,000 requests for approximately nothing, and
// it is the same mistake as the discovery agent that was deleted — find a
// company first, then hope it hires.
//
// The funded subset behaves differently. Of 85 companies backed by Y
// Combinator, Techstars, Antler or Plug and Play, 90% published a website, 80%
// of those still resolved, and 4.7% had a readable board: Bolna on Ashby,
// Observe.ai on Greenhouse, BharatX on Workable, rePurpose Global on Lever.
// Small, but real, and they arrive carrying things no board API reports —
// a sector, a funding signal, and coordinates somebody actually measured.
//
// So this source is bounded to companies with a funding signal, and its value
// is the information rather than the volume. The Common Crawl source is where
// jobs come from.
//
// Two further things the sample showed, both of which shape the code below.
// The registered name is usually not the brand: Voxlabs is Bolna, RazorSharp
// Technologies is Observe.ai, Aurorax is BharatX. Nothing that matches on name
// alone would connect those, so the domain is what this path keys on. And the
// address on file is a registered office, frequently a founder's home — which
// is why a location the board itself states always wins over it.
//
// robots.txt on the site permits crawling company pages and disallows /api/;
// this reads only the public pages, one at a time, and identifies itself.

// SourceStartupRegister is written to companies.source for rows this stored.
const SourceStartupRegister = "startup-register"

// registerBase is the published mirror of the government register this reads.
const registerBase = "https://indianstartupmap.com"

// registerPoliteness is the pause between page reads. The site is one
// publisher's small server, not a board API built for volume, and this path
// walks thousands of its pages.
const registerPoliteness = 1100 * time.Millisecond

// registerAccelerators are the attested funding signals — a company named on
// one of these portfolios was vouched for by somebody other than itself.
//
// The site is explicit that most of its 47,744 funding signals are
// self-declared: "the company ticked a box on its own Startup India profile
// saying it had raised money, and nobody checked." The 4.7% board rate was
// measured on the attested slice, so that is the slice this defaults to.
//
// The site lists 658 portfolio pages in total, and the other 600+ were
// deliberately left out. They are almost entirely university and government
// incubation cells (Atal Incubation Centres, college TBIs) that fund
// idea-stage student projects — a different population from the four above,
// which back companies expected to scale and hire. Walking all 658 would be
// roughly 25x the requests for a population even less likely than the 95%
// dead rate already measured on the attested slice. What is listed below is
// the same kind of signal as the original four: a corporate innovation
// program, a professional accelerator, or a VC platform whose portfolio
// companies plausibly run engineering teams and post roles on a real ATS.
var registerAccelerators = []string{
	"y-combinator", "techstars", "antler", "plug-and-play",
	"techstars-startup-weekend", "google-for-startups-accelerator-india",
	"nvidia-inception-for-startups", "amazon-launchpad", "cisco-launchpad",
	"oracle-for-startups", "netapp-excellerator", "dell-technologies",
	"bosch", "pitney-bowes", "philips-healthworks", "novartis-biome",
	"jiogennext", "airtel-startup-accelerator", "jsw-mg-motor-india",
	"bharat-petroleum", "sterlite-technologies-ltd", "comviva-technologies-ltd",
	"manipal-technologies-limited", "max-life-innovation-labs",
	"t-hub", "t-hub-foundation", "brigade-reap", "village-capital",
	"zone-startups-india", "startupbootcamp-india",
	"startupbootcamp-smart-cities-dubai", "angellist-india",
	"tie-hyderabad", "tie-pune", "e-cell-iit-madras", "upaya-social-ventures",
	"hexgn", "padup-ventures", "mindspace-ventures", "fundenable",
	"india-accelerator", "88mph", "rocketfuel-accelerator", "ghv-accelerator",
	"gomassive", "gruhas-aspire", "caret-accelerator", "alpha-labs-accelerator",
	"maha-accelerator", "yes-fintech",
	"catalyst-societe-generale-startup-accelerator-program",
	"toilet-board-coalition", "marico-innovation-foundation",
}

var (
	registerCompanyHrefRe = regexp.MustCompile(`href="/company/([a-z0-9-]+)"`)
	registerJSONLDRe      = regexp.MustCompile(`(?s)<script[^>]*application/ld\+json[^>]*>(.*?)</script>`)
)

// registerOrganization is the schema.org block each company page publishes.
// The site emits it for machines, which is why this reads that rather than
// scraping the rendered markup.
type registerOrganization struct {
	Type        string          `json:"@type"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	SameAs      json.RawMessage `json:"sameAs"`
	Address     struct {
		Locality string `json:"addressLocality"`
		Region   string `json:"addressRegion"`
		Country  string `json:"addressCountry"`
	} `json:"address"`
	Geo struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
	} `json:"geo"`
}

// website pulls the company's own domain out of sameAs, which the site emits
// either as a bare string or as a list.
func (o registerOrganization) website() string {
	if len(o.SameAs) == 0 {
		return ""
	}
	var one string
	if err := json.Unmarshal(o.SameAs, &one); err == nil {
		return strings.TrimSpace(one)
	}
	var many []string
	if err := json.Unmarshal(o.SameAs, &many); err == nil && len(many) > 0 {
		return strings.TrimSpace(many[0])
	}
	return ""
}

// registerCursorSource keys the HarvestState row that remembers how far a
// scheduled pass has walked through the flattened portfolio list.
//
// Every collection tick used to call registerPortfolioSlugs and start reading
// from index 0, so a limit of 25 against a list of thousands meant the same
// first 25 companies were re-read every three minutes forever — the register
// scanned Y Combinator's earliest portfolio entries in perpetuity and never
// reached Techstars, let alone anything after it. The cursor is what turns
// "read 25 companies" into "read the next 25", so a scheduled pass makes
// forward progress across ticks instead of resampling the same prefix.
//
// A one-time backfill (limit <= 0, run from the CLI) ignores the cursor and
// reads the list once from the start, because it means to see everything in
// one pass rather than resume a schedule.
const registerCursorSource = "startup-register-cursor"

// loadRegisterCursor returns where the last scheduled pass left off, clamped
// to the current list length so a shrunk or regenerated list cannot index out
// of range.
func loadRegisterCursor(total int) int {
	if total <= 0 {
		return 0
	}
	var state models.HarvestState
	if err := config.DB.Where("source = ?", registerCursorSource).First(&state).Error; err != nil {
		return 0
	}
	n, err := strconv.Atoi(state.LastIndex)
	if err != nil || n < 0 {
		return 0
	}
	return n % total
}

// saveRegisterCursor records the position the next pass should resume from.
func saveRegisterCursor(next int) {
	if err := config.DB.Save(&models.HarvestState{
		Source:    registerCursorSource,
		LastIndex: strconv.Itoa(next),
		LastRunAt: time.Now(),
	}).Error; err != nil {
		log.Printf("startup register: could not save cursor: %v", err)
	}
}

// HarvestFromStartupRegister reads the accelerator portfolios and returns a
// candidate for every company whose site advertises a board it can read.
//
// Nothing is written here and no board API is called: this returns candidates
// for HarvestSlugs to judge, exactly as the Common Crawl source does. The
// difference is that these carry a sector, a stage and coordinates.
//
// limit caps how many company pages one run reads, because the full funded set
// is 47,744 pages at roughly a second each and a scheduled job has no business
// spending thirteen hours on that. Zero means read them all, which is for a
// deliberate one-time backfill; a positive limit is the scheduled case and
// resumes from where the previous pass's cursor left off (see
// registerCursorSource), wrapping back to the start once the list is
// exhausted.
func HarvestFromStartupRegister(accelerators []string, limit int) []slugCandidate {
	if len(accelerators) == 0 {
		accelerators = registerAccelerators
	}

	slugs := registerPortfolioSlugs(accelerators)
	log.Printf("startup register: %d companies across %d portfolios", len(slugs), len(accelerators))
	if len(slugs) == 0 {
		return nil
	}

	start := 0
	scheduled := limit > 0
	if scheduled {
		start = loadRegisterCursor(len(slugs))
	}

	var out []slugCandidate
	read := 0
	pos := start
	for {
		if scheduled && read >= limit {
			break
		}
		if read >= len(slugs) {
			break
		}
		slug := slugs[pos]
		pos = (pos + 1) % len(slugs)
		read++

		org, ok := registerCompany(slug)
		time.Sleep(registerPoliteness)
		if !ok {
			continue
		}

		site := org.website()
		if site == "" {
			continue
		}
		if !strings.HasPrefix(site, "http") {
			site = "https://" + site
		}
		domain := extractDomain(site)
		if domain == "" || isAggregatorHost(domain) {
			continue
		}

		// The board, read off the company's own pages. This is the same
		// evidence DetectATS trusts — a link the company put there itself —
		// and it is what turns a register entry into a hiring company.
		provider, boardSlug := registerDetectBoard(site)
		if provider == "" {
			continue
		}

		cand := slugCandidate{
			Provider: provider,
			Slug:     boardSlug,
			Name:     registerDisplayName(org.Name),
			Website:  site,
			Sector:   ClassifySector(org.Name, org.Description),
			Stage:    "Accelerator-backed",
			Area:     registerArea(org),
			Source:   SourceStartupRegister,
		}
		if org.Geo.Latitude != 0 && org.Geo.Longitude != 0 {
			lat, lng := org.Geo.Latitude, org.Geo.Longitude
			cand.Lat, cand.Lng = &lat, &lng
		}
		out = append(out, cand)
		log.Printf("startup register: %s -> %s (%s/%s)", cand.Name, domain, provider, boardSlug)
	}
	if scheduled {
		saveRegisterCursor(pos)
	}
	return out
}

// registerPortfolioSlugs collects the company page slugs listed on each
// accelerator's page.
func registerPortfolioSlugs(accelerators []string) []string {
	seen := map[string]bool{}
	var out []string

	for _, acc := range accelerators {
		page, err := fetchText(registerBase + "/organisations/" + acc)
		time.Sleep(registerPoliteness)
		if err != nil {
			log.Printf("startup register: portfolio %s unreadable: %v", acc, err)
			continue
		}
		for _, m := range registerCompanyHrefRe.FindAllStringSubmatch(page, -1) {
			if seen[m[1]] {
				continue
			}
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	return out
}

// registerCompany reads one company page and returns its schema.org block.
func registerCompany(slug string) (registerOrganization, bool) {
	var org registerOrganization

	page, err := fetchText(registerBase + "/company/" + slug)
	if err != nil {
		return org, false
	}
	for _, m := range registerJSONLDRe.FindAllStringSubmatch(page, -1) {
		var parsed registerOrganization
		if err := json.Unmarshal([]byte(m[1]), &parsed); err != nil {
			continue
		}
		// Each page carries a BreadcrumbList alongside the Organization.
		if parsed.Type == "Organization" && parsed.Name != "" {
			return parsed, true
		}
	}
	return org, false
}

// registerDetectBoard looks for a readable board on the company's own site.
//
// Homepage first, then the conventional careers paths — the same order and the
// same regexes DetectATS uses, minus the rendered tiers. A harvest reads
// thousands of sites, and a rendered read apiece is a scraper budget this path
// has no claim on. A company whose board only appears after JavaScript runs is
// left for the ordinary sync to find later.
func registerDetectBoard(site string) (string, string) {
	base := strings.TrimRight(site, "/")
	for _, path := range []string{"", "/careers", "/jobs"} {
		page, err := fetchText(base + path)
		if err != nil {
			continue
		}
		if provider, slug := scanForATS(page); provider != "" && slug != "" {
			return provider, slug
		}
	}
	return "", ""
}

// registerDisplayName turns "VOXLABS PRIVATE LIMITED" into "Voxlabs".
//
// The register stores the legal name in capitals with its suffix attached,
// which is not what a card should read. The suffix strip reuses the same
// vocabulary normalizeCompanyName already knows.
func registerDisplayName(raw string) string {
	name := whitespaceRe.ReplaceAllString(strings.TrimSpace(raw), " ")
	if name == "" {
		return ""
	}

	lower := strings.ToLower(name)
	for _, suffix := range []string{
		" private limited", " pvt ltd", " pvt. ltd.", " limited", " ltd",
		" llp", " inc", " incorporated", " corporation", " corp",
	} {
		if strings.HasSuffix(lower, suffix) {
			name = strings.TrimSpace(name[:len(name)-len(suffix)])
			lower = strings.ToLower(name)
		}
	}

	// ALL CAPS reads as shouting on a card; anything with existing mixed case
	// is left exactly as the company wrote it.
	if name == strings.ToUpper(name) {
		return strings.Title(strings.ToLower(name)) //nolint:staticcheck // ASCII company names only
	}
	return name
}

// registerArea renders the registered office as the directory's area string.
//
// Kept only as a fallback: admitCandidate prefers the location a board stated,
// because a registered office is frequently a founder's home rather than
// anywhere work happens. The site says as much on every company page.
func registerArea(org registerOrganization) string {
	parts := make([]string, 0, 2)
	if l := strings.TrimSpace(org.Address.Locality); l != "" {
		parts = append(parts, l)
	}
	if r := strings.TrimSpace(org.Address.Region); r != "" {
		parts = append(parts, r)
	}
	return strings.Join(parts, ", ")
}
