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

func TestExtractJobsDeduplicates(t *testing.T) {
	html := `
	<a href="/jobs/abc">Backend Engineer</a>
	<a href="/jobs/abc">Backend Engineer</a>
	<a href="/jobs/def">Backend Engineer</a>`

	jobs := extractJobsFromPageText(html, "https://acme.com/careers")
	if len(jobs) != 1 {
		t.Fatalf("expected duplicates to collapse, got %d: %v", len(jobs), titlesOf(jobs))
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
