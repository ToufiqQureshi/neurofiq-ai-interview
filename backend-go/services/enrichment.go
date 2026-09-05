package services

import (
	"fmt"
	"html"
	"log"
	"net/url"
	"regexp"
	"strings"
	"sync"

	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/config"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/models"
)

// Company enrichment: the description and sector a card needs, read from the
// company's own homepage.
//
// Board discovery stores a name, a domain and a board — everything needed to
// prove the company is hiring, and nothing that describes it. So the cards for
// the companies with the most open roles were the emptiest ones on the page,
// and the sector filter, which is exact-match against a fixed dropdown, missed
// them all: 89% of stored roles could not be reached through it, and "SaaS"
// returned nothing at all.
//
// No model is involved, and that is deliberate rather than frugal. The
// description is the one the company wrote about itself in its own meta tag —
// a model asked to write one would paraphrase a page it was handed and could
// only do worse. The sector is a keyword map, the same shape ClassifyField
// already uses on job titles. Both are wrong in ways a person reading the card
// can see, which is the property that matters for a field nobody verifies.
//
// Funding stage is deliberately NOT enriched. There is no free source for it,
// and a guess dressed as a "Series B" badge is exactly the kind of confident
// wrong answer this directory removed a model for producing. The field stays
// empty until something real can fill it.

// metaDescriptionRe and ogDescriptionRe pull the one-line summary nearly every
// site publishes for search engines and link previews.
var (
	ogSiteNameRe         = regexp.MustCompile(`(?is)<meta[^>]+property\s*=\s*["']og:site_name["'][^>]+content\s*=\s*["']([^"']{2,60})["']`)
	ogSiteNameAltRe      = regexp.MustCompile(`(?is)<meta[^>]+content\s*=\s*["']([^"']{2,60})["'][^>]+property\s*=\s*["']og:site_name["']`)
	ogDescriptionRe      = regexp.MustCompile(`(?is)<meta[^>]+property\s*=\s*["']og:description["'][^>]+content\s*=\s*["']([^"']{15,500})["']`)
	ogDescriptionAltRe   = regexp.MustCompile(`(?is)<meta[^>]+content\s*=\s*["']([^"']{15,500})["'][^>]+property\s*=\s*["']og:description["']`)
	metaDescriptionRe    = regexp.MustCompile(`(?is)<meta[^>]+name\s*=\s*["']description["'][^>]+content\s*=\s*["']([^"']{15,500})["']`)
	metaDescriptionAltRe = regexp.MustCompile(`(?is)<meta[^>]+content\s*=\s*["']([^"']{15,500})["'][^>]+name\s*=\s*["']description["']`)
	twitterDescRe        = regexp.MustCompile(`(?is)<meta[^>]+(?:name|property)\s*=\s*["']twitter:description["'][^>]+content\s*=\s*["']([^"']{15,500})["']`)
	twitterDescAltRe     = regexp.MustCompile(`(?is)<meta[^>]+content\s*=\s*["']([^"']{15,500})["'][^>]+(?:name|property)\s*=\s*["']twitter:description["']`)
)

// maxHomepageBytes caps the read. A meta tag lives in <head>; anything past
// this is the page body, which we do not want and should not pay to read.
const maxHomepageBytes = 512 * 1024

// enrichBatchSize bounds one pass. These are free fetches against other
// people's servers, so the run is small and comes back rather than sweeping
// the whole table at once.
const enrichBatchSize = 40

// sectorKeywords maps a company's own words onto the fixed vocabulary the
// filter dropdown offers. Order is the whole design: what a company DOES beats
// how it is built, so "AI-powered healthcare marketing" is Healthtech, not AI.
// AI and SaaS sit last among the real buckets because almost everything now
// claims both.
// sectorKeywords maps a company's own words onto the fixed vocabulary the
// filter dropdown offers.
//
// Two things decide the order, and both came from tests that caught the map
// getting it wrong. What a company DOES beats how it is built, so "AI-powered
// healthcare marketing" is Healthtech rather than AI, and AI and SaaS sit last
// because nearly everything now claims both. And the more specific technology
// wins: "warehouse robotics" is Deeptech, not Logistics, so Deeptech is tested
// first.
//
// Several keywords carry a leading space on purpose. Without it "erp" matches
// inside "enterprise" and files every enterprise product as SaaS, "dating"
// matches inside "updating", and "learning platform" swallows "machine
// learning platform" into Edtech. Substring matching is cheap and fine here
// as long as the substrings are chosen to be words.
var sectorKeywords = []struct {
	bucket   string
	keywords []string
}{
	{"Fintech", []string{"fintech", "payment", "lending", "loan", "credit card", "insurance", "insurtech",
		"banking", "neobank", "wealth management", "mutual fund", "stockbroking", "trading platform",
		"brokerage", "upi", "invoicing", "payroll", "accounting", "tax filing", "financial services"}},
	{"Healthtech", []string{"healthcare", "healthtech", "medical", "patient", "clinic", "hospital",
		"diagnostic", "pharma", "telemedicine", "mental health", "wellness", "doctor", "therapy"}},
	{"Edtech", []string{"edtech", "education", "online learning", "learning management", " lms ",
		"students", "exam prep", "test prep", "school", "university", "tutoring", "upskilling"}},
	{"Gaming", []string{"gaming", "video game", "esports", "game studio", "mobile games"}},
	{"Deeptech", []string{"robotics", "semiconductor", "satellite", "space tech", "spacecraft", "drone",
		"biotech", "quantum", "materials science", "chip design", "electric vehicle", "battery"}},
	{"Logistics", []string{"logistics", "supply chain", "warehouse", "freight", "shipping", "fleet",
		"last mile", "courier", "delivery network"}},
	// India's two largest white-collar employers by headcount were missing from
	// the vocabulary entirely, so the two biggest boards in the directory —
	// Brillio's "Enterprise Digital Transformation Solutions" and WPP's "global
	// media collective" — got a description and no sector, and the filter could
	// not reach either. Both sit above AI and SaaS: a consultancy that says
	// "machine learning consulting" is a consultancy.
	{"Media & Advertising", []string{"advertising", "adtech", "media agency", "media collective",
		"media buying", "creative agency", "marketing agency", "brand campaigns", "publisher",
		"digital media", "programmatic advertising"}},
	{"IT Services", []string{"it services", "digital transformation", "consulting services",
		"technology consulting", "system integrator", "systems integration", "managed services",
		"outsourcing", "staff augmentation", "software services", "engineering services",
		"global capability centre", "global capability center"}},
	{"D2C", []string{"d2c", "direct-to-consumer", "skincare", "apparel", "footwear", "nutrition brand",
		"beverage", "grocery", "personal care", "consumer brand"}},
	{"Consumer", []string{"consumer app", "social network", "dating app", "travel booking",
		"food delivery", "entertainment", "streaming", "community app"}},
	{"AI", []string{"artificial intelligence", "machine learning", "ai-powered", "ai powered",
		"large language model", "generative ai", "computer vision", "ai agents", "ai platform",
		"deep learning", "ai models"}},
	{"SaaS", []string{"saas", "b2b software", " crm", " erp", "workflow automation",
		"collaboration tool", "api platform", "developer platform", "analytics platform",
		"cloud platform", "software for teams", "business software"}},
}

// ClassifySector buckets a company from its own description. Returns "" when
// nothing matches — deliberately, rather than "Other". A keyword miss means we
// could not tell, and "Other" claims we looked and it genuinely is none of
// these. Leaving it empty also keeps a count of what the map cannot handle,
// which is the evidence that would justify reaching for a model later.
func ClassifySector(name, description string) string {
	hay := " " + strings.ToLower(name+" "+description) + " "
	for _, s := range sectorKeywords {
		for _, kw := range s.keywords {
			if strings.Contains(hay, kw) {
				return s.bucket
			}
		}
	}
	return ""
}

// siteMeta is what one read of a company's homepage yields.
type siteMeta struct {
	Description string
	SiteName    string
}


// fetchSiteMetaWithFallback attempts the provided website URL first, and if that
// yields no description or has a subpath, falls back to the clean root domain.
func fetchSiteMetaWithFallback(website, domain string) siteMeta {
	var candidates []string

	cleanWeb := strings.TrimSpace(website)
	if cleanWeb != "" {
		if !strings.HasPrefix(cleanWeb, "http://") && !strings.HasPrefix(cleanWeb, "https://") {
			cleanWeb = "https://" + cleanWeb
		}
		candidates = append(candidates, cleanWeb)
		if parsed, err := url.Parse(cleanWeb); err == nil && parsed.Host != "" {
			root := fmt.Sprintf("%s://%s", parsed.Scheme, parsed.Host)
			if root != cleanWeb && root != cleanWeb+"/" {
				candidates = append(candidates, root)
			}
		}
	}

	cleanDomain := strings.TrimSpace(domain)
	if cleanDomain != "" {
		domURL := "https://" + cleanDomain
		hasDom := false
		for _, c := range candidates {
			if strings.EqualFold(c, domURL) || strings.EqualFold(c, domURL+"/") {
				hasDom = true
				break
			}
		}
		if !hasDom {
			candidates = append(candidates, domURL)
		}
	}

	var out siteMeta
	for _, targetURL := range candidates {
		meta := readPageMeta(targetURL)
		if meta.Description != "" && out.Description == "" {
			out.Description = meta.Description
		}
		if meta.SiteName != "" && out.SiteName == "" {
			out.SiteName = meta.SiteName
		}
		if out.Description != "" {
			break
		}
	}
	return out
}

func readPageMeta(targetURL string) siteMeta {
	var out siteMeta
	resp, err := SafeExternalGet(targetURL)
	if err != nil {
		return out
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return out
	}
	body, err := ReadCapped(resp.Body, maxHomepageBytes)
	if err != nil {
		return out
	}
	page := string(body)

	// Scan og:description, meta description, and twitter:description
	for _, re := range []*regexp.Regexp{ogDescriptionRe, ogDescriptionAltRe, metaDescriptionRe, metaDescriptionAltRe, twitterDescRe, twitterDescAltRe} {
		if m := re.FindStringSubmatch(page); m != nil {
			if d := cleanDescription(m[1]); d != "" {
				out.Description = d
				break
			}
		}
	}
	for _, re := range []*regexp.Regexp{ogSiteNameRe, ogSiteNameAltRe} {
		if m := re.FindStringSubmatch(page); m != nil {
			out.SiteName = whitespaceRe.ReplaceAllString(strings.TrimSpace(html.UnescapeString(m[1])), " ")
			break
		}
	}
	return out
}

// cleanDescription normalises what came out of an attribute: entities decoded,
// whitespace collapsed, and anything that is plainly not a description
// rejected.
func cleanDescription(raw string) string {
	d := whitespaceRe.ReplaceAllString(strings.TrimSpace(html.UnescapeString(raw)), " ")
	if len(d) < 15 {
		return ""
	}
	if len(d) > 320 {
		d = strings.TrimSpace(d[:317]) + "..."
	}
	lower := strings.ToLower(d)
	for _, bad := range []string{"enable javascript", "cookies", "404", "page not found",
		"lorem ipsum", "under construction", "domain is for sale"} {
		if strings.Contains(lower, bad) {
			return ""
		}
	}
	return d
}

// EnrichCompany fills in whatever the company row is missing. Existing values
// are never overwritten: a description written by the earlier pipeline, or by
// a person, is better evidence than a meta tag read today.
func EnrichCompany(c models.Company) bool {
	updates := map[string]interface{}{}

	needsDescription := strings.TrimSpace(c.Description) == ""
	needsSector := strings.TrimSpace(c.Sector) == "" || c.Sector == "Unknown"

	description := c.Description
	var meta siteMeta
	if needsDescription || looksLikeSlugFallback(c.Name) {
		meta = fetchSiteMetaWithFallback(c.Website, c.Domain)
	}
	if needsDescription && meta.Description != "" {
		description = meta.Description
		updates["description"] = meta.Description
	}
	if needsSector {
		if s := ClassifySector(c.Name, description); s != "" {
			updates["sector"] = s
		}
	}

	if meta.SiteName != "" && looksLikeSlugFallback(c.Name) &&
		nameAgreesWithSlug(meta.SiteName, c.ATSSlug) && meta.SiteName != c.Name {
		updates["name"] = meta.SiteName
	}

	if len(updates) == 0 {
		return false
	}

	if err := config.DB.Model(&models.Company{}).Where("id = ?", c.ID).Updates(updates).Error; err != nil {
		log.Printf("enrichment: could not update %q: %v", c.Name, err)
		return false
	}
	return true
}

// looksLikeSlugFallback reports whether a stored name is the one board
// discovery derives from a slug when no title could be corroborated.
func looksLikeSlugFallback(name string) bool {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return true
	}
	return trimmed == strings.ToLower(trimmed)
}

// RunEnrichment fills description and sector for companies that have neither.
func RunEnrichment() {
	var pending []models.Company
	if err := config.DB.
		Where("(coalesce(description, '') = '' OR coalesce(sector, '') = '' OR sector = 'Unknown') AND (coalesce(website, '') <> '' OR coalesce(domain, '') <> '')").
		Limit(enrichBatchSize).
		Find(&pending).Error; err != nil {
		log.Printf("enrichment: could not load companies: %v", err)
		return
	}
	if len(pending) == 0 {
		return
	}

	var (
		mu      sync.Mutex
		updated int
		wg      sync.WaitGroup
		sem     = make(chan struct{}, 8)
	)
	for _, c := range pending {
		wg.Add(1)
		go func(company models.Company) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					log.Printf("enrichment: recovered while enriching %q: %v", company.Name, r)
				}
			}()
			sem <- struct{}{}
			defer func() { <-sem }()

			if EnrichCompany(company) {
				mu.Lock()
				updated++
				mu.Unlock()
			}
		}(c)
	}
	wg.Wait()

	log.Printf("enrichment: %d of %d companies gained a description or sector", updated, len(pending))
}

// EnrichAllPendingCompanies sweeps every company in the directory missing descriptions
// or sector classification using concurrent workers.
func EnrichAllPendingCompanies(concurrency int) (int, error) {
	if concurrency <= 0 {
		concurrency = 12
	}
	var pending []models.Company
	if err := config.DB.
		Where("(coalesce(description, '') = '' OR coalesce(sector, '') = '' OR sector = 'Unknown') AND (coalesce(website, '') <> '' OR coalesce(domain, '') <> '')").
		Find(&pending).Error; err != nil {
		return 0, err
	}
	if len(pending) == 0 {
		return 0, nil
	}

	var (
		mu      sync.Mutex
		updated int
		wg      sync.WaitGroup
		sem     = make(chan struct{}, concurrency)
	)

	for _, c := range pending {
		wg.Add(1)
		go func(company models.Company) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					log.Printf("enrichment: recovered while enriching %q: %v", company.Name, r)
				}
			}()
			sem <- struct{}{}
			defer func() { <-sem }()

			if EnrichCompany(company) {
				mu.Lock()
				updated++
				mu.Unlock()
			}
		}(c)
	}
	wg.Wait()

	return updated, nil
}
