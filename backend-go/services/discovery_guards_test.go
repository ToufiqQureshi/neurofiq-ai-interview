package services

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/models"
)

// The ATS vendors sit on the domains discovery searches, so their own pages
// come back alongside their customers' boards. Every one of these scans as a
// board belonging to a company called "www" (or "j"), and each costs a live
// API call to disprove.
func TestVendorOwnPagesAreNotCompanies(t *testing.T) {
	for _, u := range []string{
		"https://www.keka.com/careers",
		"https://www.darwinbox.in/blog/hiring-trends",
		"https://www.darwinbox.com/careers",
		"https://apply.workable.com/j/ABC123",
	} {
		_, slug := scanForATS(u)
		if slug == "" {
			continue // never read as a board in the first place
		}
		if !nonSlugSegments[strings.ToLower(slug)] {
			t.Errorf("%s reads as slug %q, which discovery would store as a company", u, slug)
		}
	}
}

// A real company slug must still survive the filter above.
func TestRealSlugsAreNotFiltered(t *testing.T) {
	for _, u := range []string{
		"https://jobs.lever.co/sprinto",
		"https://boards.greenhouse.io/cartesia",
		"https://apply.workable.com/acme/j/ABC123",
		"https://acme.keka.com/careers",
	} {
		provider, slug := scanForATS(u)
		if provider == "" || slug == "" {
			t.Errorf("%s no longer reads as a board", u)
			continue
		}
		if nonSlugSegments[strings.ToLower(slug)] {
			t.Errorf("%s reads as slug %q, which the junk filter now rejects", u, slug)
		}
	}
}

// A careers page's nav links match the posting-URL pattern exactly as well as
// its roles do. "Engineering" points at /careers/engineering, which is a
// department landing page, not a job.
func TestDepartmentLinksAreNotRoles(t *testing.T) {
	page := `<a href="/careers/engineering">Engineering</a>
	         <a href="/careers/design">Design</a>
	         <a href="/careers/locations">Locations</a>
	         <a href="/careers/internships">Internships</a>
	         <a href="/jobs/senior-backend-engineer">Senior Backend Engineer</a>
	         <a href="/jobs/engineering-manager">Engineering Manager</a>`

	got := extractJobsFromPageText(page, "https://acme.com/careers")
	titles := map[string]bool{}
	for _, j := range got {
		titles[j.Title] = true
	}

	for _, want := range []string{"Senior Backend Engineer", "Engineering Manager"} {
		if !titles[want] {
			t.Errorf("real role %q was dropped", want)
		}
	}
	for _, bad := range []string{"Engineering", "Design", "Locations", "Internships"} {
		if titles[bad] {
			t.Errorf("department landing page %q was stored as a job", bad)
		}
	}
}

// The 295-profession article was reached by a link from a careers page. The
// link scan is happy to read one, because an article links every profession
// it names — so the page we start from has to be checked too, not only the
// links we follow.
func TestGuidanceArticlesAreRejectedAsSourcePages(t *testing.T) {
	for _, u := range []string{
		"https://example.edu/career-options-after-12th",
		"https://example.com/blog/best-careers-2026",
		"https://example.com/article/career-guide",
	} {
		if !guidancePageRe.MatchString(u) {
			t.Errorf("%s should read as a guidance article", u)
		}
	}
	if guidancePageRe.MatchString("https://acme.com/careers") {
		t.Error("a plain /careers page must not read as a guidance article")
	}
}

// An unreadable provider must not look like an empty board: the caller
// deletes every stored role when a board legitimately returns none.
func TestUnknownProviderIsAnErrorNotAnEmptyBoard(t *testing.T) {
	rows, err := FetchATSJobs("company-123", "recruitee", "acme")
	if err == nil {
		t.Fatalf("unknown provider returned no error (rows=%v) — the caller would clear every stored role", rows)
	}
	if len(rows) != 0 {
		t.Errorf("expected no rows alongside the error, got %d", len(rows))
	}
}

// Discovery searches board domains, so the boards it can find must be boards
// it can also read. A search hit whose canonical URL we cannot build falls
// back to the search result, but one we cannot even scan is wasted budget.
func TestEverySearchDomainYieldsAReadableSlug(t *testing.T) {
	samples := map[string]string{
		"boards.greenhouse.io":        "https://boards.greenhouse.io/acme",
		"job-boards.greenhouse.io":    "https://job-boards.greenhouse.io/acme",
		"jobs.lever.co":               "https://jobs.lever.co/acme",
		"jobs.ashbyhq.com":            "https://jobs.ashbyhq.com/acme",
		"apply.workable.com":          "https://apply.workable.com/acme",
		"careers.smartrecruiters.com": "https://careers.smartrecruiters.com/acme",
		"keka.com":                    "https://acme.keka.com/careers",
		"darwinbox.in":                "https://acme.darwinbox.in/ms/candidate/careers",
		"darwinbox.com":               "https://acme.darwinbox.com/ms/candidate/careers",
	}
	for _, domain := range boardSearchDomains {
		sample, ok := samples[domain]
		if !ok {
			if domain == "myworkdayjobs.com" {
				continue // needs a live probe for its job-site id
			}
			t.Errorf("no sample URL for search domain %q", domain)
			continue
		}
		if _, slug := scanForATS(sample); slug == "" {
			t.Errorf("search includes %q but %q yields no slug", domain, sample)
		}
	}
}

// A linked list of professions still gets through the evidence guard: every
// row carries its own URL, which is what the guard counts as evidence. This
// test records that limit rather than asserting it away — the source-page
// check above is what actually stops the case, and the ceiling below is the
// last line if it ever gets past both.
func TestBulkLinkedResultsStillRelyOnTheCeiling(t *testing.T) {
	var b strings.Builder
	for i := 0; i < maxCareersPageRoles+5; i++ {
		fmt.Fprintf(&b, `<a href="/jobs/profession-%d">Profession %d</a>`, i, i)
	}
	got := extractJobsFromPageText(b.String(), "https://example.com/careers")
	if len(got) <= maxCareersPageRoles {
		t.Skipf("only %d rows extracted — ceiling not exercised", len(got))
	}
	rows := jobRowsFrom(got)
	if careersPageResultLooksReal(rows, "Example", "https://example.com/careers") {
		t.Errorf("%d rows is above the %d ceiling and should have been discarded",
			len(rows), maxCareersPageRoles)
	}
}

func jobRowsFrom(extracted []ExtractedJob) []models.Job {
	rows := make([]models.Job, 0, len(extracted))
	for _, j := range extracted {
		rows = append(rows, models.Job{Title: j.Title, URL: j.URL, Location: j.Location, Department: j.Department})
	}
	return rows
}
