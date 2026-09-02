package services

import (
	"strings"
	"testing"
)

// Every fixture below is a row that was actually in the directory when this
// rule was written. Invented examples would have agreed with whatever the code
// happened to do; these disagreed with it, which is why the rule exists.

// The five companies discovery stored under a job posting's headline. All were
// found by one seed query — "devops engineer" — so this is not five stray rows
// but one title shape walking the whole pipeline.
func TestPostingHeadlinesAreNotCompanyNames(t *testing.T) {
	for _, row := range []struct{ name, slug string }{
		{"Sr. Engineer - DevOps 4B - Myworkdayjobs.com", "genpact:wd108:External_Careers"},
		{"Kobie Marketing - Lead DevOps Engineer", "kobie"},
		{"Job Application for Senior DevOps Engineer at Bottomline", "bottomlinetechnologies"},
		{"Senior DevOps Engineer @ AHL - Saaf AI", "ahl-saafai"},
		{"DevOps Engineer at Outmarket", "outmarket"},
	} {
		if boardRowIsAdmissible(row.name, row.slug) {
			t.Errorf("%q would still be stored against slug %q", row.name, row.slug)
		}
	}
}

// The other side of the same rule: the names the directory is actually built
// from must survive it. A sweep that deletes these is worse than the rows it
// was written to remove.
func TestStoredBoardCompaniesSurviveTheNameRule(t *testing.T) {
	for _, row := range []struct{ name, slug string }{
		{"paytm", "paytm"},
		{"Capco", "capco"},
		{"Databricks", "databricks"},
		{"Zscaler", "zscaler"},
		{"meesho", "meesho"},
		{"Sarvam", "sarvam"},
		{"Tekion", "tekion"},
		{"Speechify", "speechify"},
		// A board slug carries a disambiguating suffix the name does not.
		{"Brillio", "brillio-2"},
		// The name spaces a word the slug ran together.
		{"WPP Media", "wppmedia"},
		{"Hevo Data", "hevodata"},
		// Nothing usable in the title, so the slug became the name. That is
		// companyNameFromBoard's own fallback and must read as admissible.
		{"vmlenterprisesolutions", "vmlenterprisesolutions"},
		{"weloglobal", "weloglobal"},
	} {
		if !boardRowIsAdmissible(row.name, row.slug) {
			t.Errorf("%q (%s) is a real stored company the rule would delete", row.name, row.slug)
		}
	}
}

// The sweep must never be widened past board-search rows, and this is the row
// that says why. Razorpay's board is registered under its legal name, so its
// slug and its name disagree by 22 characters. It is correct, and it fails.
func TestAgentDiscoveredNamesWouldFailARuleTheyNeverSat(t *testing.T) {
	if boardRowIsAdmissible("Razorpay", "razorpaysoftwareprivatelimited") {
		t.Skip("the slug rule now accepts legal-name boards; the source scoping may be relaxed")
	}
	// Deliberately asserting the failure: if this ever passes, someone has
	// changed nameAgreesWithSlug, and the sweep's `source = 'board-search'`
	// filter should be revisited rather than left as unexplained caution.
}

// Vendor demo tenants serve a real board full of real-looking JSON, which is
// why nothing else catches them. salesdemo.keka.com answered with 82 postings,
// several duplicated, one titled "HR Manager (Sumit)".
func TestVendorDemoTenantsAreNotEmployers(t *testing.T) {
	for _, slug := range []string{"salesdemo", "demo", "SANDBOX", "test", "staging"} {
		if boardSlugIsAdmissible(slug) {
			t.Errorf("%q reads as an employer's board", slug)
		}
	}
}

// The demo filter is exact-match for a reason: real companies contain those
// words. Testbook is a company; a substring check would delete it.
func TestDemoFilterDoesNotEatRealNames(t *testing.T) {
	for _, slug := range []string{"testbook", "demandbase", "democratance", "testsigma", "stagingpoint"} {
		if !boardSlugIsAdmissible(slug) {
			t.Errorf("%q is rejected as a demo tenant", slug)
		}
	}
}

// Workday slugs are stored as "tenant:region:site". Only the tenant names the
// company, and the guessed website is built from it.
func TestBoardSlugLabelReadsTheWorkdayTenant(t *testing.T) {
	if got := boardSlugLabel("genpact:wd108:External_Careers"); got != "genpact" {
		t.Errorf("boardSlugLabel = %q, want %q", got, "genpact")
	}
	if got := boardSlugLabel("sprinto"); got != "sprinto" {
		t.Errorf("boardSlugLabel = %q, want %q", got, "sprinto")
	}
}

// A guessed domain is accepted only when the page links back to the SAME
// board. Provider alone would accept any company using Greenhouse, which is
// most of them — and that is exactly the slug-guessing this pipeline removed.
func TestGuessedSiteNeedsTheSameBoardNotJustTheSameProvider(t *testing.T) {
	page := `<a href="https://boards.greenhouse.io/databricks">Careers</a>`

	if !boardLinkMatches(page, "greenhouse", "databricks") {
		t.Error("a page linking the exact board does not corroborate it")
	}
	if boardLinkMatches(page, "greenhouse", "capco") {
		t.Error("a different company's Greenhouse board corroborates this one")
	}
	if boardLinkMatches(page, "lever", "databricks") {
		t.Error("the same slug on another provider corroborates this one")
	}
	if boardLinkMatches("<p>we are hiring</p>", "greenhouse", "databricks") {
		t.Error("a page with no board link corroborates a board")
	}
}

// Slug casing differs between the URL a search returns and the one a company
// prints on its own site; the corroboration must not turn on it.
func TestCorroborationIgnoresSlugCase(t *testing.T) {
	if !boardLinkMatches(`<a href="https://jobs.lever.co/Sprinto">Jobs</a>`, "lever", "sprinto") {
		t.Error("a differently-cased slug fails to corroborate its own board")
	}
}

// The guess is skipped outright for slugs that could not name a domain, so a
// run never spends fetches on "www.com" or on a vendor's demo tenant.
func TestWebsiteGuessSkipsSlugsThatNameNothing(t *testing.T) {
	for _, slug := range []string{"", "j", "www", "salesdemo", "ab"} {
		if got := guessCompanyWebsite("lever", slug); got != "" {
			t.Errorf("guessCompanyWebsite(%q) = %q, want no attempt", slug, got)
		}
	}
}

// Every TLD tried must be one extractDomain can read back, since the guessed
// URL becomes the company's stored domain and that domain is the dedupe key.
func TestGuessedTLDsProduceReadableDomains(t *testing.T) {
	for _, tld := range websiteGuessTLDs {
		if !strings.HasPrefix(tld, ".") {
			t.Errorf("%q is not a TLD suffix", tld)
		}
		if got := extractDomain("https://sprinto" + tld); got != "sprinto"+tld {
			t.Errorf("extractDomain for %q = %q", tld, got)
		}
	}
}
