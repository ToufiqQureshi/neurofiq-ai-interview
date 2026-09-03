package services

import (
	"testing"

	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/models"
)

// The India rule is the one a harvest leans on hardest. Discovery reads a
// handful of boards a tick and a bad row is visible; a harvest reads thousands
// and nobody will look. These cases are all real location strings, taken from
// a sample of 30,000 board postings.
func TestLooksIndianAcceptsRealIndianLocations(t *testing.T) {
	for _, loc := range []string{
		"Bengaluru, India",
		"Gurgaon",
		"Remote - India",
		"Pune, Maharashtra, India",
		"NOIDA",
		"Hyderabad, Telangana",
		// State-only, which the city list alone used to miss.
		"Mumbai MH",
		"Bangalore IN",
		"Kutch - Gujarat, India",
		"Nimbehera - Rajasthan, India",
		"Bengaluru, Karnataka",
		"IND Bangalore Electronic City- S1",
		// Names India alongside a foreign country: still an Indian role.
		"Bengaluru, Karnataka, India; Pleasanton, California, United States",
		"Bengaluru, Karnataka / Romania - Remote",
	} {
		if !looksIndian(loc) {
			t.Errorf("looksIndian(%q) = false, want true", loc)
		}
	}
}

func TestLooksIndianRejectsForeignLocations(t *testing.T) {
	for _, loc := range []string{
		// The original bug: "india" inside a US place name.
		"Indianapolis, IN",
		"Indianapolis, IN, USA",
		"Fort Wayne, Indiana Area",
		"Remote - Indiana, USA",
		// Plain foreign.
		"San Francisco",
		"London, UK",
		"Berlin, Germany",
		"Remote",
		"",
		"Not specified",
		// An Indian city name against a foreign country, with nothing that
		// says India. This row exists.
		"Bangalore, Mexico",
	} {
		if looksIndian(loc) {
			t.Errorf("looksIndian(%q) = true, want false", loc)
		}
	}
}

// A foreign marker must not disqualify a location that also names India, or
// every multi-region requisition with a real Indian office would be dropped.
func TestForeignMarkerNeedsIndiaToBeAbsent(t *testing.T) {
	if !looksIndian("Mumbai, India; Singapore, Singapore") {
		t.Error("a role listing India and Singapore is still an Indian role")
	}
	if looksIndian("Kochi, Japan") {
		t.Error("Kochi is a Japanese prefecture too; without an India word this must not pass")
	}
	if looksIndian("Surat Thani, Thailand") {
		t.Error("Surat Thani is in Thailand")
	}
}

// dedupeCandidates has to keep the copy that knows more, because the register
// carries a sector and coordinates the crawl never will.
func TestDedupeCandidatesKeepsTheRicherCopy(t *testing.T) {
	lat, lng := 12.97, 77.59
	in := []slugCandidate{
		{Provider: "ashby", Slug: "bolna", Source: SourceCommonCrawl},
		{
			Provider: "ashby", Slug: "bolna", Source: SourceStartupRegister,
			Name: "Bolna", Website: "https://bolna.ai", Sector: "AI",
			Stage: "Accelerator-backed", Area: "Bengaluru, Karnataka",
			Lat: &lat, Lng: &lng,
		},
		// Same board, different letter case: still one board.
		{Provider: "Ashby", Slug: "Bolna", Source: SourceCommonCrawl},
	}

	out := dedupeCandidates(in)
	if len(out) != 1 {
		t.Fatalf("expected one candidate, got %d", len(out))
	}
	if out[0].Source != SourceStartupRegister {
		t.Errorf("kept the %s copy; the register copy carries more", out[0].Source)
	}
	if out[0].Lat == nil {
		t.Error("dropped the measured coordinates")
	}
}

func TestDedupeCandidatesKeepsDistinctBoards(t *testing.T) {
	out := dedupeCandidates([]slugCandidate{
		{Provider: "lever", Slug: "sprinto"},
		{Provider: "ashby", Slug: "sprinto"},  // same name, different provider
		{Provider: "lever", Slug: "cartesia"}, // same provider, different slug
	})
	if len(out) != 3 {
		t.Fatalf("expected 3 distinct boards, got %d", len(out))
	}
}

// The directory index is the harvest's answer to findDuplicateCompany, which
// reads the whole companies table on every call — fine at five calls a tick,
// a table scan per candidate at thirteen thousand. It has to agree with the
// function it replaces: domain first, then the normalized name.
func TestDirectoryIndexMatchesDomainThenName(t *testing.T) {
	idx := &directoryIndex{
		boards:  map[string]bool{},
		names:   map[string]*models.Company{},
		domains: map[string]*models.Company{},
	}
	idx.remember(models.Company{
		Name: "BYJU'S Exam Prep (Gradeup)", Domain: "byjus.com",
		ATSType: "keka", ATSSlug: "byjus",
	})

	if !idx.hasBoard("keka", "BYJUS") {
		t.Error("board lookup must ignore case, as the SQL it replaces does")
	}
	if idx.hasBoard("lever", "byjus") {
		t.Error("same slug on a different provider is a different board")
	}
	if idx.duplicate("Anything", "byjus.com") == nil {
		t.Error("domain is the companies table's unique key and must match")
	}
	// normalizeCompanyName strips parentheticals and legal suffixes, so the
	// same business under a different label still collides.
	if idx.duplicate("BYJU'S Exam Prep", "") == nil {
		t.Error("normalized name must match the way findDuplicateCompany does")
	}
	if idx.duplicate("Sprinto", "sprinto.com") != nil {
		t.Error("an unrelated company must not match")
	}
}

// Two candidates for the same business inside one run must not both be
// written. The index is updated as the run goes, not just read at the start.
func TestDirectoryIndexRemembersWithinARun(t *testing.T) {
	idx := &directoryIndex{
		boards:  map[string]bool{},
		names:   map[string]*models.Company{},
		domains: map[string]*models.Company{},
	}
	if idx.hasBoard("ashby", "bolna") {
		t.Fatal("index should start empty")
	}
	idx.remember(models.Company{Name: "Bolna", Domain: "bolna.ai", ATSType: "ashby", ATSSlug: "bolna"})
	if !idx.hasBoard("ashby", "bolna") {
		t.Error("a board stored during the run must be seen by later candidates")
	}
	if idx.duplicate("Bolna", "") == nil {
		t.Error("a name stored during the run must be seen by later candidates")
	}
}

// ccSubdomainSlug reads a tenant off a per-tenant host, and must refuse a host
// that only looks similar.
func TestCCSubdomainSlug(t *testing.T) {
	keka := ccSubdomainSlug("keka.com")
	cases := map[string]string{
		"https://adda247.keka.com/careers":            "adda247",
		"https://fynd.keka.com/careers/jobdetails/12": "fynd",
		"https://www.keka.com/careers":                "www",
		"https://keka.com/careers":                    "", // no tenant at all
		"https://notkeka.com/careers":                 "",
		"https://acme.kekaX.com/careers":              "",
	}
	for in, want := range cases {
		if got := keka(in); got != want {
			t.Errorf("ccSubdomainSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

// Every host the crawl source queries must yield a slug the board readers
// accept, or the harvest spends requests collecting strings nothing can read.
// This is the same discipline TestSearchDomainsAreAllReadable enforces for the
// search path.
func TestCommonCrawlHostsYieldReadableSlugs(t *testing.T) {
	samples := map[string]string{
		"job-boards.greenhouse.io/*":    "https://job-boards.greenhouse.io/acme/jobs/123",
		"boards.greenhouse.io/*":        "https://boards.greenhouse.io/acme",
		"jobs.lever.co/*":               "https://jobs.lever.co/acme/abc-123",
		"jobs.ashbyhq.com/*":            "https://jobs.ashbyhq.com/acme",
		"apply.workable.com/*":          "https://apply.workable.com/acme/",
		"careers.smartrecruiters.com/*": "https://careers.smartrecruiters.com/acme",
		"*.keka.com":                    "https://acme.keka.com/careers",
		"*.darwinbox.in":                "https://acme.darwinbox.in/ms/candidate/careers",
		"*.darwinbox.com":               "https://acme.darwinbox.com/ms/candidate/careers",
	}

	for _, host := range commonCrawlHosts {
		sample, ok := samples[host.query]
		if !ok {
			t.Errorf("no sample URL for crawl host %q", host.query)
			continue
		}
		slug := host.slugFrom(sample)
		if slug == "" {
			t.Errorf("host %q could not read a slug from %q", host.query, sample)
			continue
		}
		if !validATSSlug(slug) {
			t.Errorf("host %q produced %q, which validATSSlug rejects", host.query, slug)
		}
		// The provider named must be one FetchATSJobs actually switches on.
		if _, err := FetchATSJobs("", host.provider, ""); err != nil &&
			err.Error() == "unknown ATS provider \""+host.provider+"\"" {
			t.Errorf("host %q names provider %q, which FetchATSJobs cannot read", host.query, host.provider)
		}
	}
}

// The register stores legal names in capitals with the suffix attached. A card
// should not shout, and should not carry "PRIVATE LIMITED".
func TestRegisterDisplayName(t *testing.T) {
	cases := map[string]string{
		"VOXLABS PRIVATE LIMITED":                 "Voxlabs",
		"RAZORSHARP TECHNOLOGIES PRIVATE LIMITED": "Razorsharp Technologies",
		"AURORAX PRIVATE LIMITED":                 "Aurorax",
		// Already mixed case: the company wrote it that way, leave it alone.
		"rePurpose Global": "rePurpose Global",
		"Bolna":            "Bolna",
	}
	for in, want := range cases {
		if got := registerDisplayName(in); got != want {
			t.Errorf("registerDisplayName(%q) = %q, want %q", in, got, want)
		}
	}
}

// A board's evergreen "send us your CV" posting is a mailing list, not a
// vacancy. The first harvest surfaced one — Affinidi's "Be Part of our Talent
// Community" was that company's only Indian role, so the company would have
// entered the directory advertising nothing real.
func TestDropTalentPools(t *testing.T) {
	jobs := []models.Job{
		{Title: "Be Part of our Talent Community"},
		{Title: "Talent Community"},
		{Title: "Join our Talent Pool"},
		{Title: "Talent Network - India"},
		{Title: "General Application"},
		{Title: "Future Opportunities"},
		{Title: "Speculative Application"},
		{Title: "Didn't find the role you're looking for?"},
		// The phrases are specific on purpose: real vacancies that merely
		// mention talent, community or applications must survive.
		{Title: "Talent Acquisition Specialist"},
		{Title: "Head of Talent"},
		{Title: "Community Manager"},
		{Title: "Senior Talent Partner"},
		{Title: "Application Security Engineer"},
		{Title: "Application Support Analyst"},
	}

	kept := dropTalentPools(append([]models.Job(nil), jobs...))
	if len(kept) != 6 {
		t.Fatalf("kept %d roles, want the 6 real ones: %+v", len(kept), kept)
	}
	for _, j := range kept {
		if talentPoolRe.MatchString(j.Title) {
			t.Errorf("talent-pool posting %q survived", j.Title)
		}
	}
}

// Darwinbox returns "Bengaluru, Karnataka\r, India" — a literal carriage
// return inside the field, which reached the company card exactly as written.
func TestTidyLocation(t *testing.T) {
	cases := map[string]string{
		"Bengaluru, Karnataka\r, India":    "Bengaluru, Karnataka, India",
		"Neemuch, Madhya Pradesh\r, India": "Neemuch, Madhya Pradesh, India",
		"  Bangalore  ":                    "Bangalore",
		"Pune,\n\tMaharashtra":             "Pune, Maharashtra",
		"Mumbai,":                          "Mumbai",
		"Gurgaon, Noida":                   "Gurgaon, Noida",
		"India":                            "India",
	}
	for in, want := range cases {
		if got := tidyLocation(in); got != want {
			t.Errorf("tidyLocation(%q) = %q, want %q", in, got, want)
		}
	}

	// Tidying must not change whether a location reads as Indian.
	if !looksIndian(tidyLocation("Bengaluru, Karnataka\r, India")) {
		t.Error("tidying broke the India check")
	}
}

// A company that embeds its Greenhouse board rather than linking it serves
// boards.greenhouse.io/embed/job_board?for=observeai — the company is in the
// query string and the path segment is the literal word "embed".
// greenhouseLinkRe reads the path, so every embedding company collapses to one
// useless slug. The register run showed it: Observe.ai resolved to
// "greenhouse/embed" and was dropped.
func TestCCGreenhouseSlugReadsEmbedForm(t *testing.T) {
	cases := map[string]string{
		"https://boards.greenhouse.io/embed/job_board?for=observeai": "observeai",
		"https://boards.greenhouse.io/embed/job_board?for=acme&b=1":  "acme",
		// The ordinary forms must keep working.
		"https://boards.greenhouse.io/acme":              "acme",
		"https://job-boards.greenhouse.io/acme/jobs/123": "acme",
		"https://job-boards.eu.greenhouse.io/acme":       "acme",
		"https://example.com/careers":                    "",
	}
	for in, want := range cases {
		if got := ccGreenhouseSlug(in); got != want {
			t.Errorf("ccGreenhouseSlug(%q) = %q, want %q", in, got, want)
		}
	}

	// The bad slug this replaces would have been rejected anyway, which is why
	// the symptom was a missing company rather than a wrong one.
	if boardSlugIsAdmissible("embed") {
		t.Error(`"embed" must stay inadmissible as a slug`)
	}
}

// scanForATS must read an embedded Greenhouse board as well as a linked one.
//
// A company that embeds serves boards.greenhouse.io/embed/job_board?for=X,
// where the company is in the query string. Reading the path returned the
// literal "embed", which nonSlugSegments rejects — so DetectATS could not read
// an embedded board off a careers page at all, and the company sat at zero
// roles with nothing logged. Observe.ai's careers page is one such.
func TestScanForATSReadsEmbeddedGreenhouse(t *testing.T) {
	cases := []struct{ page, provider, slug string }{
		{`<iframe src="https://boards.greenhouse.io/embed/job_board?for=observeai">`, "greenhouse", "observeai"},
		{`<script src="https://boards.greenhouse.io/embed/job_board/js?for=acme">`, "greenhouse", "acme"},
		// Every shape that already worked must keep working.
		{`<a href="https://boards.greenhouse.io/sprinto">Careers</a>`, "greenhouse", "sprinto"},
		{`<a href="https://job-boards.greenhouse.io/zeptonow/jobs/9">x</a>`, "greenhouse", "zeptonow"},
		{`<a href="https://job-boards.eu.greenhouse.io/groww">x</a>`, "greenhouse", "groww"},
		{`<a href="https://jobs.lever.co/Sprinto">x</a>`, "lever", "Sprinto"},
		{`<a href="https://jobs.ashbyhq.com/cartesia">x</a>`, "ashby", "cartesia"},
		{`<a href="https://acme.keka.com/careers">x</a>`, "keka", "acme"},
		{`<p>no board here</p>`, "", ""},
	}
	for _, c := range cases {
		provider, slug := scanForATS(c.page)
		if provider != c.provider || slug != c.slug {
			t.Errorf("scanForATS(%.60s) = (%q, %q), want (%q, %q)",
				c.page, provider, slug, c.provider, c.slug)
		}
	}
}

func TestFirstGroup(t *testing.T) {
	if got := firstGroup([]string{"whole", "", "second"}); got != "second" {
		t.Errorf("firstGroup skipped the empty group: %q", got)
	}
	if got := firstGroup([]string{"whole", "first", ""}); got != "first" {
		t.Errorf("firstGroup = %q, want first", got)
	}
	if got := firstGroup([]string{"whole", "", ""}); got != "" {
		t.Errorf("firstGroup with nothing filled = %q, want empty", got)
	}
	if got := firstGroup([]string{"whole"}); got != "" {
		t.Errorf("firstGroup with no groups = %q, want empty", got)
	}
}
