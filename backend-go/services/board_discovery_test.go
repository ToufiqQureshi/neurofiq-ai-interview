package services

import (
	"strings"
	"testing"
	"time"
)

// Board discovery reads a slug straight out of a search result's URL, so the
// URL shapes a search actually returns are what these cover — including the
// ones that are not a company at all.
func TestScanForATSReadsSlugFromBoardURLs(t *testing.T) {
	cases := []struct {
		url      string
		provider string
		slug     string
	}{
		{"https://jobs.lever.co/Sprinto", "lever", "Sprinto"},
		{"https://jobs.ashbyhq.com/cartesia", "ashby", "cartesia"},
		{"https://boards.greenhouse.io/udemy/jobs/640973", "greenhouse", "udemy"},
		{"https://boards.greenhouse.io/chime?gh_src=Blind", "greenhouse", "chime"},
		{"https://job-boards.greenhouse.io/zeptonow", "greenhouse", "zeptonow"},
		{"https://apply.workable.com/some-company/", "workable", "some-company"},
		{"https://careers.smartrecruiters.com/Example", "smartrecruiters", "Example"},
		{"https://example.com/careers", "", ""},
	}

	for _, tc := range cases {
		provider, slug := scanForATS(tc.url)
		if provider != tc.provider || slug != tc.slug {
			t.Errorf("scanForATS(%q) = (%q, %q), want (%q, %q)",
				tc.url, provider, slug, tc.provider, tc.slug)
		}
	}
}

func TestCompanyNameFromBoard(t *testing.T) {
	cases := []struct{ title, slug, want string }{
		{"Sprinto - Lever", "Sprinto", "Sprinto"},
		{"Cartesia - Jobs", "cartesia", "Cartesia"},
		{"Careers at DigiCert", "digicert", "DigiCert"},
		{"Altimate.ai - Jobs", "altimate", "Altimate.ai"},
		// An unusable title falls back to the slug, which is url-safe and so
		// reads as a name once separators are spaces.
		{"", "hinge-health", "hinge health"},
		{"Jobs", "zeptonow", "zeptonow"},

		// Every one of these is a title this directory actually stored as a
		// company name. A discovery search matches postings, not board front
		// pages, so a posting's headline is the normal case here — not the
		// edge case. The slug is what says which part of it, if any, is the
		// company.
		{"Senior Software Engineer at Gitlab", "gitlab", "Gitlab"},
		{"Frontend Engineer, Vision at Sarvam", "sarvam", "Sarvam"},
		{"Forward Deployed Engineer I at Handshake", "handshake", "Handshake"},
		{"Staff Software Engineer, Backend at Kantiv", "kantiv", "Kantiv"},
		{"Job Application for Backend Developer at Piston Technologies",
			"pistontechnologies", "Piston Technologies"},
		{"Software Engineer I (Bangalore, India) at Aiprise", "aiprise", "Aiprise"},
		{"Jobgether - Software Engineer", "jobgether", "Jobgether"},

		// Two rows the directory actually stored this way: the raw title's
		// diff against the slug (4 characters, "Lead"/"IC4") sat inside
		// nameAgreesWithSlug's near-length tolerance, so it passed and won
		// before titleCompanyHeadRe's own "Zeta"/"Protolabs" ever got a turn.
		// Same failure shape as Jobgether above, just short enough on the
		// trailing fragment to slip past the guard meant to catch it.
		{"Zeta - Lead", "zeta", "Zeta"},
		{"Protolabs - IC4", "protolabs", "Protolabs"},

		// The title names a role and a city and never names the company at
		// all, so nothing in it agrees with the slug. Taking it verbatim is
		// what produced a row whose name was a job title, whose ATS slug was
		// speechify, and whose website search then resolved to an unrelated
		// firm's domain.
		{"Software Engineer, Platform - Kolkata, India", "speechify", "speechify"},

		// A title that names a different company than the board it sits on is
		// the CRED/CreditVidya failure in another shape: reject it, keep the
		// slug, because the slug is the thing the board's API answered to.
		{"Careers at Acme Corp", "zeptonow", "zeptonow"},

		// Workday slugs are "tenant:region:site"; only the tenant is a name.
		{"", "acme:wd3:careers", "acme"},
	}

	for _, tc := range cases {
		if got := companyNameFromBoard(tc.title, tc.slug); got != tc.want {
			t.Errorf("companyNameFromBoard(%q, %q) = %q, want %q", tc.title, tc.slug, got, tc.want)
		}
	}
}

// A venture fund's board carries dozens of other companies' roles. Storing it
// as one company would file every one of those roles under the fund's name,
// which is the shape of bad data this guard exists to stop.
func TestSharedBoardRejection(t *testing.T) {
	shared := []string{"Pear VC", "Accel Partners", "Sequoia Capital", "a16z Talent Network", "YC Portfolio"}
	for _, name := range shared {
		if !sharedBoardRe.MatchString(name) {
			t.Errorf("expected %q to be treated as a shared board", name)
		}
	}

	real := []string{"Sprinto", "Cartesia", "Zepto", "Razorpay", "Notion", "Capillary Technologies"}
	for _, name := range real {
		if sharedBoardRe.MatchString(name) {
			t.Errorf("expected %q to be treated as a real company", name)
		}
	}
}

func TestIsAggregatorHost(t *testing.T) {
	blocked := []string{
		"linkedin.com", "in.linkedin.com", "crunchbase.com", "wellfound.com",
		// A board host is not a company's own site either: storing one would
		// point the careers-page resolver at the ATS's own domain.
		"jobs.lever.co", "boards.greenhouse.io",
	}
	for _, host := range blocked {
		if !isAggregatorHost(host) {
			t.Errorf("expected %q to be rejected as an aggregator", host)
		}
	}

	allowed := []string{"setu.co", "razorpay.com", "cred.club", "sprinto.com", "notion.so"}
	for _, host := range allowed {
		if isAggregatorHost(host) {
			t.Errorf("expected %q to be accepted as a company site", host)
		}
	}
}

func TestLooksIndian(t *testing.T) {
	yes := []string{"Bengaluru", "Bangalore, India", "Remote (India)", "Gurugram, Haryana", "mumbai"}
	for _, loc := range yes {
		if !looksIndian(loc) {
			t.Errorf("expected %q to count as an Indian location", loc)
		}
	}

	no := []string{"San Francisco Bay Area", "London", "Remote - US", "Berlin, Germany", ""}
	for _, loc := range no {
		if looksIndian(loc) {
			t.Errorf("expected %q not to count as an Indian location", loc)
		}
	}
}

// A lease shorter than its schedule lets a second instance re-run the tick
// this one just ran — the same metered searches, the same boards fetched
// twice. That is the whole reason the lease exists, so the two must be tied.
func TestCronLeasesCoverTheirFullInterval(t *testing.T) {
	if discoveryLeaseTTL < time.Duration(discoveryIntervalSeconds)*time.Second {
		t.Errorf("discovery lease %v is shorter than its %ds interval — another instance could repeat the tick",
			discoveryLeaseTTL, discoveryIntervalSeconds)
	}
	if jobSyncLeaseTTL < time.Duration(jobSyncIntervalSeconds)*time.Second {
		t.Errorf("job sync lease %v is shorter than its %ds interval",
			jobSyncLeaseTTL, jobSyncIntervalSeconds)
	}
}

// Every provider a search can return must resolve to a usable careers URL,
// either canonically or by falling back to the URL the search gave us.
func TestBoardURLCoversEverySearchableProvider(t *testing.T) {
	cases := map[string]string{
		"greenhouse":      "acme",
		"lever":           "acme",
		"ashby":           "acme",
		"workable":        "acme",
		"smartrecruiters": "acme",
		"keka":            "acme",
		"darwinbox":       "acme",
		"workday":         "acme:wd5:External",
	}
	for provider, slug := range cases {
		if got := boardURL(provider, slug); got == "" {
			t.Errorf("boardURL(%q, %q) returned nothing", provider, slug)
		}
	}

	// An unknown provider returns "" on purpose — boardHitsFor falls back to
	// the search result's own URL rather than storing an empty careers page.
	if got := boardURL("someday-ats", "acme"); got != "" {
		t.Errorf("expected an unknown provider to return \"\", got %q", got)
	}
	// A malformed Workday slug cannot be rebuilt, and must not produce a
	// half-formed URL.
	if got := boardURL("workday", "acme"); got != "" {
		t.Errorf("expected a malformed workday slug to return \"\", got %q", got)
	}
}

// The manual endpoint is open to any signed-in user and the budget is one
// shared pot, so a per-user rate limit does not bound what several accounts
// spend together. The reserve is what keeps the scheduled rotation running
// when they have spent everything else.
func TestManualDiscoveryLeavesTheSchedulerAReserve(t *testing.T) {
	t.Setenv("EXA_API_KEY", "test-key")
	t.Setenv("EXA_MONTHLY_BUDGET", "800")
	t.Setenv("TAVILY_API_KEY", "")

	reserved := schedulerReserve()
	if reserved <= 0 {
		t.Fatal("no budget is reserved for the scheduler — several accounts could stop discovery for the month")
	}
	if reserved >= 800 {
		t.Errorf("reserve of %d leaves nothing for manual runs", reserved)
	}
	if want := 800 / schedulerReserveFraction; reserved != want {
		t.Errorf("reserved %d, want %d", reserved, want)
	}
}

// A provider with no key contributes no budget, so its absence must not
// inflate the reserve into blocking every manual run.
func TestManualDiscoveryBudgetCountsOnlyConfiguredProviders(t *testing.T) {
	t.Setenv("EXA_API_KEY", "")
	t.Setenv("TAVILY_API_KEY", "")

	if reserved := schedulerReserve(); reserved != 0 {
		t.Errorf("with no provider configured the reserve should be 0, got %d", reserved)
	}
}

// The run has to stop on lookups started, not on companies saved. A candidate
// rejected *after* its website lookup — no site found, or a domain already
// held — has spent a search all the same, so counting saves let a "5 company"
// run spend one search per board hit.
func TestLookupsAreCappedByAttemptNotBySave(t *testing.T) {
	const limit, plenty, noFloor = 5, 1000, 0

	for lookups := 0; lookups < limit; lookups++ {
		if !mayStartLookup(lookups, limit, plenty, noFloor) {
			t.Errorf("lookup %d of %d should be allowed", lookups+1, limit)
		}
	}
	if mayStartLookup(limit, limit, plenty, noFloor) {
		t.Error("a run must stop once it has started `limit` lookups, however few were saved")
	}
}

// A manual run passes the reserve check on entry; it must not then spend
// through the reserve while the loop is running.
func TestLookupsStopAtTheReserve(t *testing.T) {
	const limit, floor = 5, 200

	if !mayStartLookup(0, limit, floor+1, floor) {
		t.Error("a lookup with budget above the reserve should be allowed")
	}
	if mayStartLookup(0, limit, floor, floor) {
		t.Error("a lookup that would take the budget to the reserve must not start")
	}
	if mayStartLookup(0, limit, floor-1, floor) {
		t.Error("a lookup below the reserve must not start")
	}
}

func TestBoardSeedQueriesAreDistinct(t *testing.T) {
	if len(boardSeedQueries) == 0 {
		t.Fatal("no seed queries built")
	}
	seen := map[string]bool{}
	for _, q := range boardSeedQueries {
		if seen[q] {
			t.Fatalf("duplicate seed query %q — the rotation would repeat it", q)
		}
		seen[q] = true
	}
}

// Jobgether is a job marketplace with a Lever board. It is not a fund and does
// not call itself a network, so sharedBoardRe never saw it — and its board
// alone put 4440 roles into the directory under one company name, two thirds
// of every job stored. Those roles belong to several hundred employers.
func TestAggregatorBoardRejection(t *testing.T) {
	blocked := []string{
		"Jobgether", "jobgether", "Hirist", "Instahyre", "Naukri",
		"ABC Staffing Solutions", "TeamLease", "Vertex Recruitment",
	}
	for _, name := range blocked {
		if !aggregatorBoardRe.MatchString(name) {
			t.Errorf("expected %q to be rejected as a marketplace or staffing board", name)
		}
	}

	// Employers, including ones whose names sit near the words above.
	real := []string{
		"Sprinto", "Cartesia", "Zepto", "Razorpay", "Sarvam", "Handshake",
		"Piston Technologies", "Speechify", "Darwinbox",
	}
	for _, name := range real {
		if aggregatorBoardRe.MatchString(name) {
			t.Errorf("expected %q to be treated as a real employer", name)
		}
	}
}

// maxBoardRoles has to clear every real employer and still stop a board that
// cannot belong to one.
//
// The lower bound is not a guess. At 400 this guard rejected Paytm Payments
// (840 roles) and WPP Media (1074) on a live run, and Zscaler cleared it by
// only 48 — real companies, all three, and exactly what the directory is for.
// A role count carries no signal in that band, so the ceiling belongs above it
// and the name guard does the real work.
func TestMaxBoardRolesClearsRealEmployers(t *testing.T) {
	for _, seen := range []struct {
		name  string
		roles int
	}{
		{"Zscaler", 352},
		{"Paytm Payments", 840},
		{"WPP Media", 1074},
	} {
		if maxBoardRoles <= seen.roles {
			t.Errorf("maxBoardRoles = %d rejects %s, a real employer seen at %d roles",
				maxBoardRoles, seen.name, seen.roles)
		}
	}

	// Jogether, the board that put 4440 roles under one name.
	if maxBoardRoles >= 4440 {
		t.Errorf("maxBoardRoles = %d would have admitted the 4440-role aggregator board", maxBoardRoles)
	}
}

// The company's own site is on its board page, put there by the company. That
// is the same standard this file applies to boards themselves — and it is
// free, where the search it replaces is the one metered call in the loop.
func TestPickCompanyLink(t *testing.T) {
	page := `
	  <a href="https://jobs.lever.co/gokwik">Back to jobs</a>
	  <a href="https://www.linkedin.com/company/gokwik">LinkedIn</a>
	  <a href="https://twitter.com/gokwik">Twitter</a>
	  <a href="https://www.gokwik.co/">Visit our website</a>
	  <a href="https://someblog.com/gokwik-raises">Press</a>`
	if got := pickCompanyLink(matchLinks(boardOutboundLinkRe, page), "gokwik"); got != "https://www.gokwik.co/" {
		t.Errorf("pickCompanyLink() = %q, want the corroborated company site", got)
	}

	// A slug-corroborated domain wins even when another usable link came
	// first, because agreeing with the slug is proof and position is not.
	page = `
	  <a href="https://press.example.com/story">In the news</a>
	  <a href="https://nutrabay.com">Home</a>`
	if got := pickCompanyLink(matchLinks(boardOutboundLinkRe, page), "nutrabay"); got != "https://nutrabay.com" {
		t.Errorf("pickCompanyLink() = %q, want the slug-corroborated link", got)
	}

	// Nothing but the ATS and the social networks: fall through to the search.
	page = `
	  <a href="https://jobs.ashbyhq.com/ema">Jobs</a>
	  <a href="https://www.linkedin.com/company/ema">LinkedIn</a>`
	if got := pickCompanyLink(matchLinks(boardOutboundLinkRe, page), "ema"); got != "" {
		t.Errorf("pickCompanyLink() = %q, want empty so the caller falls back", got)
	}
}

// The rotation”'s order matters as much as its weights.
//
// It used to loop city-outer, so all ten of a city”'s queries ran back to back:
// a day of discovery was one or two cities and nothing else, and a report on
// "which city has the most companies" measured the cursor instead of the
// country. Weighting alone would have made that worse.
func TestSeedRotationInterleavesCities(t *testing.T) {
	// A spelling is not a city. "Bengaluru" and "Bangalore" are the same
	// place, and counting them apart is how two consecutive ticks on Bengaluru
	// once looked like two different cities.
	canonical := map[string]string{}
	for _, c := range boardSeedCities {
		for _, sp := range c.spellings {
			canonical[sp] = c.Name()
		}
	}
	cityOf := func(q string) string {
		i := strings.Index(q, " in ")
		if i < 0 {
			t.Fatalf("unexpected query shape: %q", q)
		}
		spelling := strings.TrimSuffix(q[i+4:], ", India")
		name, ok := canonical[spelling]
		if !ok {
			t.Fatalf("query names a city not in boardSeedCities: %q", spelling)
		}
		return name
	}

	for i := 1; i < len(boardSeedQueries); i++ {
		if a, b := cityOf(boardSeedQueries[i-1]), cityOf(boardSeedQueries[i]); a == b {
			t.Fatalf("ticks %d and %d are both %s — the rotation is clustering again", i-1, i, a)
		}
	}

	// Weights track the published ecosystem sizes, so the order of the big
	// three has to come out right, and Hyderabad has to outrank Kolkata.
	share := map[string]int{}
	for _, q := range boardSeedQueries {
		share[cityOf(q)]++
	}
	if !(share["Bengaluru"] >= share["Mumbai"] && share["Mumbai"] > share["Pune"]) {
		t.Errorf("share order Bengaluru %d, Mumbai %d, Pune %d", share["Bengaluru"], share["Mumbai"], share["Pune"])
	}
	ncr := share["Gurgaon"] + share["Noida"] + share["Delhi"]
	if ncr <= share["Pune"] {
		t.Errorf("Delhi NCR %d should outweigh Pune %d", ncr, share["Pune"])
	}
	if share["Hyderabad"] <= share["Kolkata"] {
		t.Errorf("Hyderabad %d <= Kolkata %d; Hyderabad is the fourth-largest hub and Kolkata is outside the top ten",
			share["Hyderabad"], share["Kolkata"])
	}
}
