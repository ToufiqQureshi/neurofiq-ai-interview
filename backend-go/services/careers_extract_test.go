package services

import (
	"strings"
	"testing"

	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/models"
)

func titlesOf(jobs []ExtractedJob) []string {
	out := make([]string, len(jobs))
	for i, j := range jobs {
		out[i] = j.Title
	}
	return out
}

func TestExtractJobsFromHTMLCareersPage(t *testing.T) {
	html := `
	<nav><a href="/about">About us</a><a href="/blog">Blog</a></nav>
	<h2>Open roles</h2>
	<ul>
	  <li><a href="/careers/jobs/senior-backend-engineer">Senior Backend Engineer</a></li>
	  <li><a href="https://acme.com/jobs/product-designer">Product Designer</a></li>
	  <li><a href="/openings/data-analyst">Data Analyst</a></li>
	</ul>
	<a href="/privacy">Privacy policy</a>
	<a href="mailto:jobs@acme.com">Email us</a>`

	jobs := extractJobsFromPageText(html, "https://acme.com/careers")
	if len(jobs) != 3 {
		t.Fatalf("expected 3 roles, got %d: %v", len(jobs), titlesOf(jobs))
	}

	for _, j := range jobs {
		if !strings.HasPrefix(j.URL, "https://acme.com/") {
			t.Errorf("expected an absolute company URL, got %q", j.URL)
		}
	}
	if jobs[0].Title != "Senior Backend Engineer" {
		t.Errorf("unexpected first role %q", jobs[0].Title)
	}
}

// Jina renders a page to markdown, so the same page arrives in a different
// shape. Both have to work, because which one we get depends only on whether
// the page needed a browser.
func TestExtractJobsFromMarkdownCareersPage(t *testing.T) {
	md := `
## Current openings

- [**Staff Engineer, Platform**](https://acme.com/careers/jobs/staff-engineer)
- [Engineering Manager](/careers/job-1234)
- [Life at Acme](/life-at-acme)
- [View all jobs](/careers)
`

	jobs := extractJobsFromPageText(md, "https://acme.com/careers")
	if len(jobs) != 2 {
		t.Fatalf("expected 2 roles, got %d: %v", len(jobs), titlesOf(jobs))
	}
	if jobs[0].Title != "Staff Engineer, Platform" {
		t.Errorf("markdown emphasis was not stripped: %q", jobs[0].Title)
	}
}

// HTML encodes & as &amp; inside an href. Stored verbatim, the posting URL
// points at a page that does not exist — and the title reads "Sales &amp;
// Marketing".
func TestExtractJobsDecodesHTMLEntities(t *testing.T) {
	html := `<a href="/jobs/apply?dept=Sales&amp;loc=IN">Manager, Sales &amp; Marketing</a>`

	jobs := extractJobsFromPageText(html, "https://acme.com/careers")
	if len(jobs) != 1 {
		t.Fatalf("expected 1 role, got %d: %v", len(jobs), titlesOf(jobs))
	}
	if jobs[0].Title != "Manager, Sales & Marketing" {
		t.Errorf("title was not decoded: %q", jobs[0].Title)
	}
	if jobs[0].URL != "https://acme.com/jobs/apply?dept=Sales&loc=IN" {
		t.Errorf("href was not decoded: %q", jobs[0].URL)
	}
}

// A careers page that is both client-rendered and a marketing page linking
// elsewhere is the ordinary shape for a company big enough to have a
// marketing team. The listing link only exists after rendering, so the
// plain-fetch hop never sees it — the rendered read has to take the same hop
// or the company falls through to the paid extraction for nothing.
func TestListingLinkIsFoundInBothPageShapes(t *testing.T) {
	const base = "https://acme.com/careers"
	const want = "https://acme.com/careers/openings"

	// Jina's markdown — the shape the rendered path actually receives.
	rendered := `
	# Careers at Acme
	Fetching open roles...
	[View all open positions](/careers/openings)`
	if got := findJobsListingLink(rendered, base); got != want {
		t.Errorf("markdown: got %q, want %q", got, want)
	}

	// A plain fetch's HTML, which used to be the only shape understood.
	htmlPage := `<div><a class="btn" href="/careers/openings">View all open positions</a></div>`
	if got := findJobsListingLink(htmlPage, base); got != want {
		t.Errorf("html: got %q, want %q", got, want)
	}

	// An ordinary link is not a listing link.
	if got := findJobsListingLink(`<a href="/about">About us</a>`, base); got != "" {
		t.Errorf("expected no listing link, got %q", got)
	}

	// A career-advice article is still refused, in both shapes.
	if got := findJobsListingLink(`[View all open positions](/blog/career-options)`, base); got != "" {
		t.Errorf("expected a guidance article to be refused, got %q", got)
	}
}

func TestExtractJobsSkipsNavigationAndGuidanceLinks(t *testing.T) {
	html := `
	<a href="/careers/jobs/1">Software Engineer</a>
	<a href="/careers/login">Login</a>
	<a href="/blog/career-options-after-12th">Career options after 12th</a>
	<a href="/career-guide/actuary">Actuary</a>
	<a href="/careers">Back to careers</a>`

	jobs := extractJobsFromPageText(html, "https://school.example/careers")
	if len(jobs) != 1 || jobs[0].Title != "Software Engineer" {
		t.Fatalf("expected only the real role, got %v", titlesOf(jobs))
	}
}

// The posting URL is what identifies a job here, so the same link twice is one
// row — but two links are two openings even when they share a title. A board
// of any size lists several roles under one title ("Software Engineer" in two
// cities), and collapsing those loses real jobs.
func TestExtractJobsDeduplicatesOnURLNotTitle(t *testing.T) {
	html := `
	<a href="/jobs/abc">Backend Engineer</a>
	<a href="/jobs/abc">Backend Engineer</a>
	<a href="/jobs/def">Backend Engineer</a>`

	jobs := extractJobsFromPageText(html, "https://acme.com/careers")
	if len(jobs) != 2 {
		t.Fatalf("expected the repeated link to collapse and the distinct posting to survive, got %d: %v",
			len(jobs), titlesOf(jobs))
	}

	seen := map[string]bool{}
	for _, j := range jobs {
		if seen[j.URL] {
			t.Errorf("duplicate posting URL %q survived", j.URL)
		}
		seen[j.URL] = true
	}
	if !seen["https://acme.com/jobs/abc"] || !seen["https://acme.com/jobs/def"] {
		t.Errorf("expected both distinct postings, got %v", seen)
	}
}

// The 295-profession case: rows read out of an article's prose, none of them
// linked to a posting. That result must still be rejected.
func TestCareersPageResultRejectsUnlinkedBulk(t *testing.T) {
	page := "https://school.example/careers"
	var rows []models.Job
	for _, title := range []string{"Actor", "Actuary", "Addiction Counselor", "Aerospace Engineer", "Agronomist", "Animator"} {
		rows = append(rows, models.Job{Title: title, URL: page + "#" + slugify(title)})
	}

	if careersPageResultLooksReal(rows, "Example School", page) {
		t.Error("expected a list of professions with no postings behind it to be rejected")
	}
}

// A real listing read by link has no location or department, but every row
// points at its own posting — which is the evidence the old check was missing.
func TestCareersPageResultAcceptsLinkedRoles(t *testing.T) {
	page := "https://acme.com/careers"
	rows := []models.Job{
		{Title: "Backend Engineer", URL: "https://acme.com/careers/jobs/1"},
		{Title: "Frontend Engineer", URL: "https://acme.com/careers/jobs/2"},
		{Title: "Product Designer", URL: "https://acme.com/careers/jobs/3"},
		{Title: "Data Analyst", URL: "https://acme.com/careers/jobs/4"},
		{Title: "Engineering Manager", URL: "https://acme.com/careers/jobs/5"},
	}

	if !careersPageResultLooksReal(rows, "Acme", page) {
		t.Error("expected roles with their own posting links to be accepted")
	}
}

func TestCareersPageResultRejectsAboveCeiling(t *testing.T) {
	page := "https://acme.com/careers"
	rows := make([]models.Job, maxCareersPageRoles+1)
	for i := range rows {
		rows[i] = models.Job{Title: "Role", URL: page + "/jobs/x"}
	}

	if careersPageResultLooksReal(rows, "Acme", page) {
		t.Error("expected a result above the sane ceiling to be rejected")
	}
}

func TestATSRecheckIntervalIsShorterAfterAFailure(t *testing.T) {
	found := models.Company{ATSType: "greenhouse"}
	if atsRecheckIntervalFor(found) != atsRecheckInterval {
		t.Error("a company with a known board should keep the long interval")
	}

	missing := models.Company{}
	if got := atsRecheckIntervalFor(missing); got != atsRetryInterval {
		t.Errorf("a company with no board should be retried after %v, got %v", atsRetryInterval, got)
	}
	if atsRetryInterval >= atsRecheckInterval {
		t.Error("the retry wait must be shorter than the recheck wait, or a fix stays invisible for a week")
	}
}
