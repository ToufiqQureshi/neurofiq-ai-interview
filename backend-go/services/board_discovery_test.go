package services

import (
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
