package services

import (
	"fmt"
	"log"
	"os"
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
// board page's text actually contains — a Bangalore posting says "Bangalore",
// it does not say "Series A fintech".
var (
	// boardSeedCities carries a role budget per city, because the cities are
	// not the same size and an equal share sent the rotation to the wrong
	// places.
	//
	// Every city used to get all ten roles, so Kolkata drew as much of the
	// budget as Hyderabad. The directory ended up holding 21 Kolkata companies
	// and 4 Hyderabad ones — while Hyderabad is India's fourth-largest startup
	// hub with ~5,000 startups and the fastest-growing funding of any Indian
	// city, and Kolkata is outside the top ten. Delhi NCR meanwhile drew three
	// shares by accident, because it is spelled as three cities, and came to
	// hold 70 companies against Bengaluru's 28 — with Bengaluru the larger
	// ecosystem, 12,000 startups to NCR's 10,000.
	//
	// roles is how many of boardSeedRoles that city gets, taken from the front
	// of the list. Weighting by repeating a query instead would have spent a
	// metered search to fetch results already seen; a bigger city earns a
	// broader sweep of roles, which is both distinct and more useful — a large
	// ecosystem really does hire designers and data scientists, a small one
	// mostly hires engineers.
	//
	// Sizes follow published counts (Inc42, Tracxn, StartupBlink, 2025-26):
	// Bengaluru 12k, Delhi NCR 10k, Mumbai 8k, Hyderabad 5k, Pune 4k, Chennai
	// 3.5k, Ahmedabad 2.5k, Kochi 1.8k, Jaipur 1.5k, Chandigarh 1.2k. Bengaluru
	// carries both spellings because boards use both and they return different
	// pages.
	boardSeedCities = []seedCity{
		// spellings, not separate cities: boards write both "Bengaluru" and
		// "Bangalore" and the two return different pages, so the rotation uses
		// them in turn. Listing them as two entries put the same city on two
		// consecutive ticks while pretending they were different places.
		{spellings: []string{"Bengaluru", "Bangalore"}, roles: 10},
		{spellings: []string{"Mumbai"}, roles: 10},
		{spellings: []string{"Gurgaon", "Gurugram"}, roles: 8},
		{spellings: []string{"Hyderabad"}, roles: 8},
		{spellings: []string{"Noida"}, roles: 7},
		{spellings: []string{"Pune"}, roles: 5},
		{spellings: []string{"Chennai"}, roles: 5},
		{spellings: []string{"Delhi", "New Delhi"}, roles: 5},
		{spellings: []string{"India remote"}, roles: 4},
		{spellings: []string{"Ahmedabad"}, roles: 3},
		{spellings: []string{"Kochi"}, roles: 2},
		{spellings: []string{"Jaipur"}, roles: 2},
		{spellings: []string{"Chandigarh"}, roles: 2},
		{spellings: []string{"Indore"}, roles: 2},
		{spellings: []string{"Coimbatore"}, roles: 2},
		// Kolkata is outside the published top ten and its budget says so, but
		// it is not nothing either: the directory already holds 21 companies
		// hiring there. Cutting a city to zero stops the rotation ever looking
		// again, which is a stronger claim than the data supports.
		{spellings: []string{"Kolkata"}, roles: 2},
	}

	// boardSeedRoles are ordered most general first, because a city with a
	// small budget takes them from the front and should spend it on the roles
	// most likely to exist anywhere.
	boardSeedRoles = []string{
		"software engineer", "backend engineer", "sales", "marketing",
		"data scientist", "frontend engineer", "product manager",
		"designer", "devops engineer", "machine learning engineer",
	}

	boardSeedQueries = buildBoardSeedQueries()
)

// seedCity is one place the rotation searches, and how much of the role list
// it is worth spending there.
type seedCity struct {
	// spellings are the ways boards write this city. They are alternated
	// across the city's queries rather than listed as separate cities.
	spellings []string
	// roles is how many of boardSeedRoles this city gets, from the front.
	roles int
}

// Name is the city as the directory refers to it.
func (c seedCity) Name() string { return c.spellings[0] }

// buildBoardSeedQueries lays the rotation out so consecutive ticks land in
// different cities.
//
// The order is the whole point. This used to loop city-outer, role-inner,
// which put all ten of a city's queries next to each other: a day of discovery
// was one or two cities and nothing else, and a report on "which city has the
// most companies" measured the cursor rather than the country — 20 of the 34
// companies found in one window came from the two cities the rotation happened
// to be sitting on.
//
// Roles run on the outside now and cities on the inside, so every city is
// visited once before any city is visited twice. A city with a bigger budget
// survives into more of the later role passes, and the two heaviest keep a
// full budget so the tail of the rotation still alternates rather than ending
// on one city repeated.
func buildBoardSeedQueries() []string {
	out := make([]string, 0, len(boardSeedCities)*len(boardSeedRoles))
	for r, role := range boardSeedRoles {
		for _, city := range boardSeedCities {
			if r >= city.roles {
				continue // this city's budget is spent
			}
			spelling := city.spellings[r%len(city.spellings)]
			out = append(out, fmt.Sprintf("%s jobs in %s, India", role, spelling))
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
const discoveryIntervalSeconds = 15 * 60 // front-loaded; see main.go

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

// vendorDemoSlugs are the ATS vendors' own demonstration tenants. They are
// real boards serving real-looking JSON, which is exactly why nothing else
// here catches them: salesdemo.keka.com answered with 82 postings, several
// duplicated and one titled "HR Manager (Sumit)", and the directory filed
// them as a hiring company. A vendor's showroom is not an employer, and the
// slug is the only place that shows.
var vendorDemoSlugs = map[string]bool{
	"demo": true, "salesdemo": true, "democompany": true, "test": true,
	"testing": true, "sandbox": true, "staging": true, "example": true,
}

// boardSearchSource labels the companies this file stores. Written out at
// every use before, which meant a sweep filtering on it could disagree with
// the writer by a typo and silently judge nothing.
const boardSearchSource = "board-search"

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
	// The asset CDNs those boards serve from, and the vendors' own marketing
	// sites. A board page links to its stylesheets, its icons and a "powered
	// by" badge far more often than to the employer: without these, a scan of
	// GitLab's Greenhouse board returned greenhouse.com as GitLab's website.
	"ashbyprd.com", "greenhouse-cdn.com", "leverstatic.com", "smartrecruiters.io",
	"greenhouse.com", "lever.com", "ashby.hq", "workable.co", "keka.io",
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

// aggregatorBoardRe matches boards belonging to a job marketplace or a
// staffing firm rather than to an employer. sharedBoardRe covers funds and
// talent networks, which name themselves as such; these do not — and one of
// them, Jobgether, put 4440 roles into this directory under a single company,
// two thirds of every job it held. Those roles are real. They just belong to
// several hundred other employers.
var aggregatorBoardRe = regexp.MustCompile(`(?i)(jobgether|jobsora|jooble|remotive|weworkremotely|remoteok|weekday|hirist|instahyre|cutshort|foundit|monsterindia|timesjobs|naukri|indeed|glassdoor|ziprecruiter|simplyhired|adzuna|careerbuilder|staffing|manpower|randstad|adecco|teamlease|quesscorp|recruit(er|ers|ment|ing)|placements?|jobboard|jobsite)`)

// maxBoardRoles rejects a board so large it cannot belong to one employer.
//
// It was 400, and live discovery showed that to be wrong: it threw out
// Paytm Payments at 840 roles and WPP Media at 1074, both of which are exactly
// the employers this directory exists to list. Between roughly 500 and 1500 a
// role count does not separate an aggregator from a large employer at all, so
// a ceiling in that band costs real companies and buys nothing.
//
// What actually catches the case this guard was written for is
// aggregatorBoardRe: Jobgether is rejected by name, before a metered search is
// spent. The ceiling is only the backstop for a board no name list anticipated,
// so it sits above any real employer and below the 4440 roles Jobgether had.
//
// The signal that would separate the middle band is dispersion, not count — an
// aggregator’s roles are scattered across the world while an employer’s cluster
// in a few offices — but that is more machinery than this has earned.
const maxBoardRoles = 2000

// indiaLocationHints decide whether a board has roles worth listing. The
// directory is for people looking for work in India; a company headquartered
// anywhere is welcome, as long as it is hiring here.
var indiaLocationHints = []string{
	"india", "bengaluru", "bangalore", "mumbai", "delhi", "gurgaon",
	"gurugram", "noida", "hyderabad", "pune", "chennai", "kolkata",
	"ahmedabad", "jaipur", "indore", "chandigarh", "kochi", "coimbatore",
	"trivandrum", "thiruvananthapuram", "bhubaneswar", "nagpur", "surat",
}

// indiaStateHints are the states and union territories, which boards name far
// more often than the city list above anticipated.
//
// A board writes a location however its HR team typed it, and the two forms
// this list exists for are ordinary: "Mumbai MH" and "Bengaluru, Karnataka"
// carry a city the hints already know, but "Kutch - Gujarat" and "Nimbehera -
// Rajasthan" carry only a state, and were read as not-Indian. The cost of
// missing them is not a cosmetic gap: a board whose only Indian roles are
// stated that way returns "" from firstIndianLocation and the whole company is
// rejected at discovery.
//
// Measured against 30,000 real board postings before this was added, the city
// list alone missed 0.1% of Indian rows — small, and the rows it missed were
// exactly this shape.
var indiaStateHints = []string{
	"andhra pradesh", "arunachal pradesh", "assam", "bihar", "chhattisgarh",
	"goa", "gujarat", "haryana", "himachal pradesh", "jharkhand", "karnataka",
	"kerala", "madhya pradesh", "maharashtra", "manipur", "meghalaya", "mizoram",
	"nagaland", "odisha", "punjab", "rajasthan", "sikkim", "tamil nadu",
	"tamilnadu", "telangana", "tripura", "uttar pradesh", "uttarakhand",
	"west bengal", "andaman and nicobar", "dadra and nagar haveli", "daman and diu",
	"jammu and kashmir", "ladakh", "lakshadweep", "puducherry",
}

// indiaLocationRe matches the hints as whole words.
//
// Substring matching was wrong in a way that took a US company into an India
// directory: "india" is inside "Indianapolis" and "Indiana", so Speechify's
// board in Indianapolis read as Indian, the company was accepted, and its
// area was stamped "Indianapolis, IN, USA" on a map of Indian startups. A
// word boundary costs nothing and ends the whole class — "Remote - Indiana,
// USA" no longer reads as Bengaluru.
var indiaLocationRe = regexp.MustCompile(`(?i)\b(` +
	strings.Join(append(append([]string{}, indiaLocationHints...), indiaStateHints...), "|") + `)\b`)

// foreignLocationRe names a country that is not India.
//
// It exists for one shape the city list cannot judge on its own: a location
// that names an Indian city AND a foreign country, where the foreign country
// is the real one. "Bangalore, Mexico" is in the sample of 30,000 postings
// this was measured against, and so is "Bengaluru, Karnataka / Romania -
// Remote". The first is a mislabelled foreign role; the second is a genuine
// Indian role that also lists Romania.
//
// The rule below is what separates them, and it is deliberately conservative:
// a foreign country only disqualifies a location that names no Indian state or
// the word India. That keeps every multi-region req whose Indian half is
// explicit, and drops only the ones where the Indian word is a city name
// standing alone against a foreign country.
//
// Measured cost of the rule: 0.37% of matching postings carry a foreign
// marker, and most of those name India as well, so they survive.
var foreignLocationRe = regexp.MustCompile(`(?i)\b(` + strings.Join([]string{
	"japan", "thailand", "canada", "ontario", "united states", "u\\.s(?:\\.a?)?", "usa",
	"united kingdom", "england", "scotland", "singapore", "malaysia", "australia",
	"germany", "france", "netherlands", "ireland", "poland", "brazil", "mexico",
	"philippines", "vietnam", "indonesia", "china", "hong kong", "taiwan",
	"south korea", "spain", "italy", "sweden", "norway", "denmark", "finland",
	"switzerland", "austria", "belgium", "portugal", "romania", "czechia",
	"hungary", "israel", "turkey", "egypt", "kenya", "nigeria", "south africa",
	"new zealand", "argentina", "chile", "colombia", "peru", "costa rica",
	"united arab emirates", "uae", "dubai", "saudi arabia", "qatar", "bahrain",
	"oman", "bangladesh", "sri lanka", "nepal", "pakistan",
}, "|") + `)\b`)

// crossBorderStateHints are Indian state names that another country also uses
// for a province of its own. They still say "somewhere in India" when nothing
// contradicts them, but they cannot outweigh a named foreign country — see
// indiaWordRe.
var crossBorderStateHints = map[string]bool{"punjab": true}

// indiaWordRe is the unambiguous evidence that a location really is in India:
// the country itself, or one of its states. A bare city name is not enough,
// because cities collide across countries — there is a Kochi in Japan and a
// Surat in Thailand.
//
// Cross-border state names are left out of THIS list, though they stay in
// indiaLocationRe. Punjab is a province of Pakistan as well as a state of
// India, so including it here let "Lahore, Punjab, Pakistan" name a foreign
// country, be caught by the foreign check, and then be rescued from it — the
// directory stored a Pakistani city as an Indian company's area. A location
// that really is in Indian Punjab still passes, either because it names no
// foreign country at all or because it also says India.
var indiaWordRe = regexp.MustCompile(`(?i)\b(india|` + strings.Join(unambiguousIndiaStates(), "|") + `)\b`)

func unambiguousIndiaStates() []string {
	out := make([]string, 0, len(indiaStateHints))
	for _, state := range indiaStateHints {
		if !crossBorderStateHints[state] {
			out = append(out, state)
		}
	}
	return out
}

// looksIndian reports whether a role's stated location is in India.
//
// Two questions, in order: does it name an Indian place at all, and if it also
// names a foreign country, does it still name India or an Indian state? A
// location that names Mexico and only the word "Bangalore" fails the second —
// which is the whole point, because "Bangalore, Mexico" is a real row.
func looksIndian(location string) bool {
	if !indiaLocationRe.MatchString(location) {
		return false
	}
	if foreignLocationRe.MatchString(location) && !indiaWordRe.MatchString(location) {
		return false
	}
	return true
}

// boardTitleCleanupRe strips the suffixes boards append to their page titles,
// so "Sprinto - Lever" and "Cartesia - Jobs" both come back as the company.
var boardTitleCleanupRe = regexp.MustCompile(`(?i)\s*[-–—|]\s*(lever|greenhouse|ashby|workable|smartrecruiters|jobs|careers|job board|open positions|hiring)\s*$`)

var titlePrefixRe = regexp.MustCompile(`(?i)^\s*(careers at|jobs at|work at|current openings at|open positions at|job application for)\s+`)

// genericBoardTitles are what is left when a board page's title says nothing
// about the company. Left alone, a page titled "Jobs" would enter the
// directory as a company called Jobs.
var genericBoardTitles = map[string]bool{
	"jobs": true, "careers": true, "job board": true, "open positions": true,
	"openings": true, "current openings": true, "hiring": true, "home": true,
	"lever": true, "greenhouse": true, "ashby": true, "workable": true,
	"smartrecruiters": true, "job search": true, "search jobs": true,
}

// A discovery search finds POSTINGS, not board front pages. The seed queries
// say "software engineer jobs in Bangalore, India", and that is a posting's
// own text — a board index does not read like that. boardHitsFor already
// canonicalises the URL back to the board root for exactly this reason, but
// it passes the title through untouched, and taking that title as the company
// name is how the directory ended up with companies called "Senior Software
// Engineer at Gitlab" and "Job Application for Backend Developer at Piston
// Technologies". The bad name then poisoned the next step too, because
// resolveCompanyWebsite searches it: one of those rows resolved to an
// unrelated firm's domain and filed a different company's whole board there.
//
// So a title is no longer trusted on its own. These pull out the part of a
// posting title that could be the company — the tail of "<role> at <Company>",
// the head of "<Company> - <role>" — and the slug decides which, if either,
// is real.
var (
	titleCompanyTailRe = regexp.MustCompile(`(?i)^.*\S\s+at\s+(\S.*)$`)
	titleCompanyHeadRe = regexp.MustCompile(`^(.*\S)\s*[-–—|]\s*\S.*$`)
	alnumOnlyRe        = regexp.MustCompile(`[^a-z0-9]+`)
)

// nameAgreesWithSlug reports whether a candidate name pulled out of a page
// title is corroborated by the slug in the board URL.
//
// The slug is the identity worth trusting: it came out of the URL the board
// itself serves, and the board's public API answered to it. A title is taken
// only when it agrees, in which case it supplies the casing and spacing the
// slug lost ("pistontechnologies" -> "Piston Technologies"). That is the rule
// the rest of this file already follows for boards — accept with evidence,
// never from a guess — applied to the name as well as to the board.
func nameAgreesWithSlug(name, slug string) bool {
	a := alnumOnlyRe.ReplaceAllString(strings.ToLower(name), "")
	b := alnumOnlyRe.ReplaceAllString(strings.ToLower(slug), "")
	if len(a) < 3 || len(b) < 3 {
		return false
	}
	if a == b {
		return true
	}
	// A slug and a company's written name disagree at the edges more often
	// than in the middle: "altimate" for "Altimate.ai", "sprintohq" for
	// "Sprinto". So a prefix counts — but only a near-length one. Unbounded,
	// the prefix rule accepts the whole of "Jobgether - Software Engineer"
	// against the slug "jobgether", which is the exact bad name it is here
	// to reject: every posting title starts with something.
	if !strings.HasPrefix(a, b) && !strings.HasPrefix(b, a) {
		return false
	}
	diff := len(a) - len(b)
	if diff < 0 {
		diff = -diff
	}
	return diff <= 4
}

// titleCandidates are the parts of a title that could name the company.
func titleCandidates(name string) []string {
	// Extracted candidates first, the untouched title last. companyNameFromBoard
	// returns the first candidate nameAgreesWithSlug accepts, and that
	// tolerance is loose enough — a near-length prefix match — that a raw
	// title with a short trailing role fragment can pass on its own: "Zeta -
	// Lead" against slug "zeta" differs by only four characters, the same
	// tolerance meant for "Sprinto" against "sprintohq". Checked first, that
	// let the raw title win before titleCompanyHeadRe's own "Zeta" ever got a
	// turn — two board-search rows shipped exactly that way. The extracted
	// candidates are always the more specific answer when they exist, so they
	// go first; the untouched title stays as the fallback for a title that
	// carries no "at" or "-" separator to extract from at all.
	var out []string
	if m := titleCompanyTailRe.FindStringSubmatch(name); m != nil {
		out = append(out, strings.TrimSpace(m[1]))
	}
	if m := titleCompanyHeadRe.FindStringSubmatch(name); m != nil {
		out = append(out, strings.TrimSpace(m[1]))
	}
	out = append(out, name)
	return out
}

// boardSlugLabel is the part of a slug that names the company. Workday slugs
// are stored as "tenant:region:site", and only the tenant does — the whole
// triple would have entered the directory as "acme:wd3:careers".
func boardSlugLabel(slug string) string {
	if tenant, _, found := strings.Cut(slug, ":"); found {
		slug = tenant
	}
	return strings.TrimSpace(slug)
}

// slugDisplayName reads a board slug as a name.
func slugDisplayName(slug string) string {
	slug = boardSlugLabel(slug)
	slug = strings.ReplaceAll(strings.ReplaceAll(slug, "-", " "), "_", " ")
	return strings.TrimSpace(whitespaceRe.ReplaceAllString(slug, " "))
}

// boardSlugIsAdmissible reports whether a slug names an employer at all.
func boardSlugIsAdmissible(slug string) bool {
	label := strings.ToLower(boardSlugLabel(slug))
	return label != "" && !nonSlugSegments[label] && !vendorDemoSlugs[label]
}

// boardRowIsAdmissible reports whether discoverFromBoards would store this
// company today: a slug it would accept, under a name companyNameFromBoard
// could have returned for that slug.
//
// companyNameFromBoard has exactly two outcomes — a title the slug
// corroborates, or the slug read as a name — so those are the only two shapes
// a legitimately stored name can have. Anything else predates the rule.
func boardRowIsAdmissible(name, slug string) bool {
	if !boardSlugIsAdmissible(slug) {
		return false
	}
	name = strings.TrimSpace(name)
	return nameAgreesWithSlug(name, slug) || strings.EqualFold(name, slugDisplayName(slug))
}

// companyNameFromBoard turns a board page's title into a company name.
//
// The slug is both the referee and the fallback: a title is used only when it
// names something the slug agrees with, and anything else — a role, a posting
// headline, a title that says nothing — resolves to the slug. A slug-derived
// name reads worse than a good title, but it is always the right company, and
// that trade is the whole point.
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

	for _, candidate := range titleCandidates(name) {
		if len(candidate) > 80 || genericBoardTitles[strings.ToLower(candidate)] {
			continue
		}
		if nameAgreesWithSlug(candidate, slug) {
			return candidate
		}
	}
	return slugDisplayName(slug)
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
		if provider == "" || slug == "" || !boardSlugIsAdmissible(slug) {
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

// boardOutboundLinkRe finds the absolute links an HTML board page carries.
//
// Anchors only. Matching every href took <link rel="icon"> with it, and an
// Ashby board's favicon lives on cdn.ashbyprd.com — which is how a live check
// of this function returned a CDN asset as two companies' websites.
var boardOutboundLinkRe = regexp.MustCompile(`(?is)<a\b[^>]*?href\s*=\s*["'](https?://[^"'#?\s]+)`)

// jinaLinkRe finds the links in what Jina Reader returns, which is markdown
// with a links summary rather than HTML.
var jinaLinkRe = regexp.MustCompile(`(?i)[\(<\s](https?://[^\s\)>"']+)`)

// assetLinkRe matches a link to a file rather than to a site.
var assetLinkRe = regexp.MustCompile(`(?i)\.(svg|png|jpe?g|gif|webp|ico|css|js|woff2?|ttf|pdf|xml|json)$`)

// websiteFromBoardPage reads the company's own site off its board page.
//
// This runs before resolveCompanyWebsite because it is free and it is
// evidence. A board page is published by the company, and the link back to its
// site is one the company put there — the same standard this file applies to
// boards themselves. The search is a guess by comparison, and the expensive
// one: it was the only way a company's domain was found, so a run bounded at
// five companies spent five metered searches here alone, and a candidate
// rejected after its lookup spent one for nothing.
//
// It is also the more accurate of the two. Searching the name "Ema" for an
// Indian AI startup returned ema.europa.eu, and the directory stored the
// European Medicines Agency's homepage, then read its description off that
// page — a metered call spent to get the company wrong.
//
// Two attempts, cheapest first. A plain fetch reads a server-rendered board
// such as Greenhouse. Lever, Ashby and Workable render their boards in the
// browser, so their HTML carries no links at all and the plain read returns
// nothing; Jina renders those, and is free and keyless. Firecrawl is
// deliberately not in this path — it is paid, and a company website is a
// nicety next to the roles, which are already in hand by this point.
//
// Returns "" when neither attempt names anything usable, and the caller falls
// back to the search.
func websiteFromBoardPage(boardPageURL, slug string) string {
	if boardPageURL == "" {
		return ""
	}

	if page := plainFetchBoardPage(boardPageURL); page != "" {
		if link := pickCompanyLink(matchLinks(boardOutboundLinkRe, page), slug); link != "" {
			return link
		}
	}

	rendered, err := fetchViaJina(boardPageURL)
	if err != nil {
		return ""
	}
	recordScrapeUsage("jina")
	return pickCompanyLink(matchLinks(jinaLinkRe, rendered), slug)
}

// plainFetchBoardPage reads the board page over plain HTTP, or returns "".
func plainFetchBoardPage(boardPageURL string) string {
	resp, err := SafeExternalGet(boardPageURL)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return ""
	}
	body, err := ReadCapped(resp.Body, maxHomepageBytes)
	if err != nil {
		return ""
	}
	return string(body)
}

// matchLinks pulls the capture group out of every match, capped so a huge page
// cannot turn into a huge slice.
func matchLinks(re *regexp.Regexp, page string) []string {
	matches := re.FindAllStringSubmatch(page, 300)
	links := make([]string, 0, len(matches))
	for _, m := range matches {
		links = append(links, m[1])
	}
	return links
}

// pickCompanyLink chooses the company's own site from the links on its board
// page. Split out from the fetching so the judgement is testable without a
// network, and shared by the plain and rendered reads.
func pickCompanyLink(links []string, slug string) string {
	var firstUsable string
	for _, link := range links {
		if assetLinkRe.MatchString(link) {
			continue // an image or stylesheet, not a company
		}
		domain := extractDomain(link)
		// isAggregatorHost already rejects the ATS domains and their CDNs
		// along with LinkedIn, the social networks and the job aggregators,
		// which is most of what a board page links to besides the company.
		if domain == "" || isAggregatorHost(domain) {
			continue
		}
		// A domain the slug agrees with is the company beyond doubt:
		// jobs.lever.co/gokwik linking to gokwik.com. Take it immediately.
		if nameAgreesWithSlug(strings.SplitN(domain, ".", 2)[0], slug) {
			return link
		}
		if firstUsable == "" {
			firstUsable = link
		}
	}
	// No corroborated link, but the company put this one on its own board
	// page, which is still better evidence than a search result.
	return firstUsable
}

// resolveCompanyWebsite finds a company's own homepage.
//
// The board tells us a company is hiring but not where it lives, and the
// companies table is keyed on domain. One search per newly-seen company, and
// never for one we already have.
// websiteGuessTLDs are tried, in order, against a board slug before a search
// is paid for. Of the 145 companies the board search stored in its first two
// days, 92 sit on their slug under one of these five, and the sixth candidate
// would add two requests per company for a case that has not come up yet.
var websiteGuessTLDs = []string{".com", ".ai", ".io", ".in", ".co"}

// guessCompanyWebsite finds a company's own site without spending a search.
//
// resolveCompanyWebsite below is the last metered step in discovery — one
// search per newly-seen company, and 145 of them in two days. Most are paying
// to be told what the slug already said: sprinto, meesho, tekion and paytm are
// all their own domain label.
//
// But a guess is the thing this file spent a rewrite removing, so it is not
// trusted on its own. A candidate site is accepted only when its own pages
// link back to the SAME board the company was found on — DetectATS's evidence
// rule, run in the other direction. A parked domain, a squatter, or an
// unrelated firm sitting on the same word cannot produce that link. Nor could
// devopscompany.nl, which is what a paid search returned for a Genpact board.
//
// Every request here is plain HTTP. When nothing corroborates, we fall through
// and buy the search exactly as before, so the worst case in money is the
// current cost plus some free fetches.
//
// The worst case in time is ten requests per company — five TLDs, two pages
// each — which at externalClient's 20s ceiling is 200s, and 1000s for a full
// five-company run. That is inside the three-hour lease the rotation holds,
// and nowhere near it in practice: a TLD the company does not own fails at
// DNS in milliseconds, and the loop stops at the first board it can confirm.
// Worth re-measuring if the TLD list grows.
func guessCompanyWebsite(provider, slug string) string {
	label := strings.ToLower(boardSlugLabel(slug))
	if len(label) < 3 || !boardSlugIsAdmissible(slug) {
		return ""
	}

	for _, tld := range websiteGuessTLDs {
		site := "https://" + label + tld

		body, err := fetchText(site)
		if err != nil {
			// Does not resolve, or does not answer. Nothing on this domain
			// can corroborate anything, so do not spend a second request on
			// its careers path.
			continue
		}
		if boardLinkMatches(body, provider, slug) {
			log.Printf("board discovery: %s links its own %s board — website resolved without a search", site, provider)
			return site
		}

		// Homepages link "Careers", not the board itself. The page behind
		// that word is where the board link almost always lives.
		if careers, err := fetchText(site + "/careers"); err == nil && boardLinkMatches(careers, provider, slug) {
			log.Printf("board discovery: %s/careers links its own %s board — website resolved without a search", site, provider)
			return site
		}
	}
	return ""
}

// boardLinkMatches reports whether a page links the exact board given.
//
// Same provider AND same slug. Provider alone would accept any company that
// happens to use Greenhouse, which is most of them.
//
// This looks for the board's own address rather than asking scanForATS what
// the page links, because scanForATS answers with the FIRST board it
// recognises, in a fixed provider order. A Lever company whose page also
// mentions a Greenhouse board anywhere — a partner, an investor's careers
// link, a "we also hire through" note — would have that Greenhouse hit
// returned instead, and the corroboration would fail on a page that does link
// the right board. The cost of that is a search we did not need to buy.
func boardLinkMatches(page, provider, slug string) bool {
	target := boardURL(provider, slug)
	if target == "" {
		return false
	}

	// Matched without the scheme: the same board is linked as https, as http,
	// and protocol-relative, and all three are the same evidence.
	needle := strings.ToLower(strings.TrimPrefix(strings.TrimPrefix(target, "https://"), "http://"))
	haystack := strings.ToLower(page)

	for from := 0; from < len(haystack); {
		at := strings.Index(haystack[from:], needle)
		if at < 0 {
			return false
		}
		start := from + at
		end := start + len(needle)

		// Both edges have to be clean, and for different reasons.
		//
		// After the slug: without it "jobs.lever.co/cred" matches a link to
		// jobs.lever.co/creditvidya — the exact confusion this pipeline
		// removed slug guessing over.
		//
		// Before the host: without it "notjobs.lever.co/acme" contains
		// "jobs.lever.co/acme", so any domain ending in the board's host
		// would corroborate a guess and file a company under the wrong one.
		afterIsClean := end >= len(haystack) || !isSlugByte(haystack[end])
		beforeIsClean := start == 0 || !isHostByte(haystack[start-1])
		if afterIsClean && beforeIsClean {
			return true
		}
		from += at + 1
	}
	return false
}

// isHostByte reports whether a byte could be part of the hostname to the left
// of a match — the label separator "." included, since that is what makes
// "notjobs.lever.co" and "jobs.lever.co" different hosts.
func isHostByte(b byte) bool {
	return b == '-' || b == '.' ||
		(b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// isSlugByte reports whether a byte could be part of a board slug, and so
// whether a match that ends before it is really a match.
func isSlugByte(b byte) bool {
	return b == '-' || b == '_' || b == '.' || b == ':' ||
		(b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

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
// schedulerReserveFraction is the share of the monthly search budget that only
// the scheduled rotation may spend.
//
// The manual endpoint is open to any signed-in user, and a per-user rate limit
// does not bound what several accounts spend together: the budget is one
// shared pot, so enough of them draining it would stop the rotation for the
// rest of the month — the directory quietly stops growing, and nothing in the
// product says why. A reserve makes that impossible without needing an
// authorisation system: manual runs get the first three quarters, the
// scheduler always has the last.
//
// This is not a substitute for admin-only, which remains the real fix and is
// still listed as a gap. It is the part that can be done without one.
const schedulerReserveFraction = 4

// schedulerReserve is the number of searches held back for the rotation. A
// policy number derived from configuration alone — no usage lookup — so it
// stays testable and cannot fail closed on a database hiccup.
func schedulerReserve() int {
	total := 0
	for _, p := range searchProviders {
		if os.Getenv(p.envKey) != "" {
			total += providerBudget(p)
		}
	}
	return total / schedulerReserveFraction
}

// ManualDiscoveryBudget reports how many searches a user-triggered run may
// still spend, and how many are held back for the scheduler.
func ManualDiscoveryBudget() (spendable int, reserved int) {
	reserved = schedulerReserve()
	return SearchBudgetRemaining() - reserved, reserved
}

// DiscoverFromBoardsManual is the user-triggered entry point. It refuses to
// spend into the scheduler's reserve; the rotation calls DiscoverFromBoards
// directly and is not subject to it.
func DiscoverFromBoardsManual(query string, limit int) ([]models.Company, error) {
	if spendable, reserved := ManualDiscoveryBudget(); spendable <= limit {
		return nil, fmt.Errorf(
			"manual discovery is paused: %d searches left this month and %d of them are reserved for the scheduled rotation",
			SearchBudgetRemaining(), reserved)
	}
	return discoverFromBoards(query, limit, schedulerReserve())
}

// mayStartLookup decides whether another company-website search may begin.
//
// Two separate ceilings, and the run stops at whichever comes first:
//
//   - lookups against limit. This is the one that was wrong: the loop used to
//     stop on companies *saved*, but a candidate that is rejected after its
//     website lookup — no site found, or a domain we already hold — has spent
//     a search all the same. With 25 board hits a "5 company" run could spend
//     26 searches, five times what the schedule was budgeted for.
//   - the remaining budget against floor, so a manual run cannot eat into the
//     scheduler's reserve part-way through, having passed the check on entry.
func mayStartLookup(lookups, limit, remaining, floor int) bool {
	return lookups < limit && remaining > floor
}

// DiscoverFromBoards is the scheduled entry point: it may spend the whole
// remaining budget, because the rotation is what the budget is for.
func DiscoverFromBoards(query string, limit int) ([]models.Company, error) {
	return discoverFromBoards(query, limit, 0)
}

// discoverFromBoards runs one search and stores the companies behind the
// boards it finds. floor is the budget level it will not spend past — zero
// for the rotation, the scheduler's reserve for a manual run.
func discoverFromBoards(query string, limit, floor int) ([]models.Company, error) {
	if limit <= 0 || limit > maxNewCompaniesPerRun {
		limit = maxNewCompaniesPerRun
	}
	// One search to find boards. Everything after it may well be free — a
	// company whose board page or own domain names its website costs nothing
	// — so this asks only for the board search, not for the whole lookup
	// allowance on top of it.
	//
	// It used to require limit+floor up front, which was right when every
	// company cost a search and wrong the moment they stopped: with a handful
	// of credits left, a run that would have spent one and resolved five
	// companies for free refused to start at all.
	//
	// The lookups keep their own guard. mayStartLookup is checked immediately
	// before each metered one, which is the only place that can know whether
	// it is actually needed.
	if remaining := SearchBudgetRemaining(); remaining <= floor {
		return nil, fmt.Errorf("search budget nearly spent (%d left, %d reserved) — skipping discovery",
			remaining, floor)
	}

	hits := boardHitsFor(query, boardResultsPerQuery)
	if len(hits) == 0 {
		return nil, nil
	}

	var saved []models.Company
	lookups := 0
	for _, hit := range hits {
		if len(saved) >= limit {
			break
		}

		name := companyNameFromBoard(hit.Title, hit.Slug)
		if sharedBoardRe.MatchString(name) || sharedBoardRe.MatchString(hit.Slug) {
			log.Printf("board discovery: skipping %q — looks like a fund or talent-network board", name)
			continue
		}
		if aggregatorBoardRe.MatchString(name) || aggregatorBoardRe.MatchString(hit.Slug) {
			log.Printf("board discovery: skipping %q — looks like a job marketplace or staffing board", name)
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
		if len(jobs) > maxBoardRoles {
			log.Printf("board discovery: skipping %q — %d roles on one board is a marketplace, not an employer",
				name, len(jobs))
			continue
		}
		area := firstIndianLocation(jobs)
		if area == "" {
			continue // hiring, but not here
		}

		if dup := findDuplicateCompany(name, ""); dup != nil {
			// Same business, found earlier without its board.
			attachBoardTo(dup, hit)
			continue
		}

		// Two free ways to name the company's site, in the order this pipeline
		// takes everywhere: the board page the company published, then its own
		// domain when the slug is also its domain label. Only when neither
		// answers does this reach the search, the one metered call in the loop.
		website := websiteFromBoardPage(hit.URL, hit.Slug)
		if website == "" {
			website = guessCompanyWebsite(hit.Provider, hit.Slug)
		}
		if website == "" {
			// The metered step. Everything above this line is free, so the
			// count that matters is of lookups started — not of companies
			// stored, and not of hits examined. The budget check sits here
			// rather than at the top of the iteration for the same reason: a
			// run that has spent its lookups can still store every company it
			// can name for nothing.
			if !mayStartLookup(lookups, limit, SearchBudgetRemaining(), floor) {
				log.Printf("board discovery: stopping after %d website lookups (limit %d, budget %d, reserved %d)",
					lookups, limit, SearchBudgetRemaining(), floor)
				break
			}
			lookups++
			website = resolveCompanyWebsite(name)
		}
		domain := extractDomain(website)
		if domain == "" {
			log.Printf("board discovery: skipping %q — no company website found", name)
			continue
		}
		if dup := findDuplicateCompany(name, domain); dup != nil {
			// Reached only when the name did not match but the domain does —
			// the company was stored earlier under a different name. This
			// used to just skip, which threw away the board it had just paid
			// a search to find, so the row stayed boardless and every later
			// rotation repeated the same wasted lookup. 73 of the directory's
			// companies sat at zero roles in exactly this state.
			attachBoardTo(dup, hit)
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
			Source:     boardSearchSource,
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

// attachBoardTo gives an existing company the board just discovered for it.
// Both duplicate checks end here: whichever one matched, the stored row is the
// same business, and its board is new information about it.
//
// A company already carrying a board is left alone. Some companies run two —
// Level AI is on both Lever and Ashby — and overwriting on every rotation made
// its board flap between them, replacing all of its roles each time. The first
// board found wins; a second one adds nothing the directory can show.
// attachBoardTo reports whether it actually attached the board, so a caller
// that runs many of these concurrently — the slug harvest does, one goroutine
// per candidate — can tell a real attach from losing a race for the same
// company.
//
// The in-memory guard this replaced (checking dup.ATSSlug before writing) was
// not enough for that caller: two goroutines can both read the same *Company
// with an empty slug, both pass the check, and both fire an UPDATE — the
// second silently overwriting the first's board, and the harvest's own
// Attached counter double-counting one company. The WHERE clause below is
// what actually serialises this: only the update that finds the row still
// unattached does anything, so the loser's RowsAffected is 0.
func attachBoardTo(dup *models.Company, hit boardHit) bool {
	if strings.TrimSpace(dup.ATSSlug) != "" {
		return false
	}
	result := config.DB.Model(&models.Company{}).
		Where("id = ? AND (ats_slug IS NULL OR ats_slug = '')", dup.ID).
		Updates(map[string]interface{}{
			"ats_type": hit.Provider, "ats_slug": hit.Slug,
			"careers_url": hit.URL, "ats_checked_at": time.Now(),
		})
	if result.Error != nil {
		log.Printf("board discovery: failed to attach board %q to %q: %v", hit.Slug, dup.Name, result.Error)
		return false
	}
	if result.RowsAffected == 0 {
		// Another goroutine attached a board to this company first.
		return false
	}
	// dup.ATSSlug is deliberately left untouched here. The slug harvest calls
	// this from several goroutines that can hold the very same *Company
	// pointer (idx.duplicate hands out the one stored in its maps), and every
	// other reference to the field is a read with no synchronisation of its
	// own — a write here would race with those reads. The WHERE clause above
	// is what actually prevents a double attach; this field is just the
	// in-memory mirror the next full index rebuild will refresh.
	log.Printf("board discovery: attached %s board %q to existing company %q",
		hit.Provider, hit.Slug, dup.Name)
	return true
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

// RunMultiCityDiscovery triggers discovery queries across major Indian tech hub cities
// (Bengaluru, Mumbai, Gurgaon/Delhi, Hyderabad, Pune, Chennai, Noida).
// It allows an operator or admin to actively sweep locations on demand while respecting
// remaining search budget guards.
func RunMultiCityDiscovery(cities []string, limitPerCity int) (map[string]int, error) {
	if limitPerCity <= 0 || limitPerCity > maxNewCompaniesPerRun {
		limitPerCity = maxNewCompaniesPerRun
	}
	if len(cities) == 0 {
		cities = []string{"Bengaluru", "Mumbai", "Gurgaon", "Hyderabad", "Pune", "Chennai", "Noida", "Delhi"}
	}

	results := make(map[string]int)
	for _, city := range cities {
		if SearchBudgetRemaining() <= schedulerReserve() {
			log.Printf("multi-city discovery: stopping sweep early, search budget floor reached (%d remaining)", SearchBudgetRemaining())
			break
		}
		query := fmt.Sprintf("software engineer jobs in %s, India", city)
		saved, err := DiscoverFromBoards(query, limitPerCity)
		if err != nil {
			log.Printf("multi-city discovery: failed for city %q: %v", city, err)
			results[city] = 0
			continue
		}
		results[city] = len(saved)
		log.Printf("multi-city discovery: %s -> %d new companies discovered", city, len(saved))
		time.Sleep(1 * time.Second)
	}

	return results, nil
}
