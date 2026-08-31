package services

import "testing"

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
