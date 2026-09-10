package services

import (
	"encoding/xml"
	"strings"
	"testing"
)

// freshteamFixtureHTML is trimmed straight from a live board
// (stllr.freshteam.com/jobs, captured 2026-09-07) rather than hand-written,
// so the parser is proven against what a real careers page actually ships —
// two departments, one with a single job, one with two.
const freshteamFixtureHTML = `
<div data-portal-id="jobs_list">
  <div class="job-role-list" data-portal-id="job-role-list">
    <ul>
      <li data-portal-role="_role_6000125936">
        <div class="role-title">
        <!-- Do not remove data-portal-* attributes. Removing the same will result in breakages in filter behaviour -->
          <h5>
            Administration
            <span class="mobile-role-count">- <span data-portal-id="mobile-jobs-count"></span></span>
          </h5>
        </div>
        <div>
          <div class="job-list">
            <a href="/jobs/jlPAyS_q4I2_/senior-accountant-remote" class="heading" data-portal-title="senioraccountant(remote)" data-portal-location="Riyadh, Saudi Arabia" data-portal-job-type="2" data-portal-remote-location=true>
              <div class="row">
                <div class="job-list-info">
                  <div class="job-title">Senior Accountant (Remote)</div>
                </div>
              </div>
            </a>
          </div>
        </div>
      </li>
      <li data-portal-role="_role_6000136161">
        <div class="role-title">
        <!-- Do not remove data-portal-* attributes. Removing the same will result in breakages in filter behaviour -->
          <h5>
            Tech
            <span class="mobile-role-count">- <span data-portal-id="mobile-jobs-count"></span></span>
          </h5>
        </div>
        <div>
          <div class="job-list">
            <a href="/jobs/BWM-y7CB1TMw/ai-ml-intern-paid-remote" class="heading" data-portal-title="ai/mlintern(paidremote)" data-portal-location="Riyadh, Saudi Arabia" data-portal-job-type="3" data-portal-remote-location=true>
              <div class="row">
                <div class="job-list-info">
                  <div class="job-title">AI / ML Intern (Paid Remote)</div>
                </div>
              </div>
            </a>
            <a href="/jobs/xw5VWmv7bY87/mid-senior-fullstack-developer-remote" class="heading" data-portal-title="mid-seniorfullstackdeveloper(remote)" data-portal-location="Riyadh, Saudi Arabia" data-portal-job-type="2" data-portal-remote-location=true>
              <div class="row">
                <div class="job-list-info">
                  <div class="job-title">Mid-Senior Fullstack Developer (Remote)</div>
                </div>
              </div>
            </a>
          </div>
        </div>
      </li>
    </ul>
  </div>
</div>
`

func TestFreshteamJobRegexReadsRealBoardHTML(t *testing.T) {
	matches := freshteamJobRe.FindAllStringSubmatch(freshteamFixtureHTML, -1)
	if len(matches) != 3 {
		t.Fatalf("expected 3 job cards, got %d", len(matches))
	}
	wantTitles := []string{"Senior Accountant (Remote)", "AI / ML Intern (Paid Remote)", "Mid-Senior Fullstack Developer (Remote)"}
	for i, m := range matches {
		if m[3] != wantTitles[i] {
			t.Errorf("job %d: title = %q, want %q", i, m[3], wantTitles[i])
		}
		if m[2] != "Riyadh, Saudi Arabia" {
			t.Errorf("job %d: location = %q, want %q", i, m[2], "Riyadh, Saudi Arabia")
		}
	}
}

func TestFreshteamDeptRegexAttributesJobsToTheRightSection(t *testing.T) {
	depts := freshteamDeptRe.FindAllStringSubmatch(freshteamFixtureHTML, -1)
	if len(depts) != 2 {
		t.Fatalf("expected 2 department headings, got %d", len(depts))
	}
	if got := strings.TrimSpace(depts[0][1]); got != "Administration" {
		t.Errorf("first department = %q, want %q", got, "Administration")
	}
	if got := strings.TrimSpace(depts[1][1]); got != "Tech" {
		t.Errorf("second department = %q, want %q", got, "Tech")
	}
}

// personioFixtureXML is trimmed from a live tenant's public feed
// (gus-germany.jobs.personio.de/xml, captured 2026-09-07).
const personioFixtureXML = `<?xml version="1.0" encoding="UTF-8"?>
<workzag-jobs>
<position>
    <id>2523845</id>
    <office>Hamburg</office>
    <department>Academic Faculty</department>
    <name>An external freelance lecturer for Big Data</name>
</position>
<position>
    <id>2516231</id>
    <office>Hamburg</office>
    <department>Academic Faculty</department>
    <name>An external freelance lecturer for Digital Business</name>
</position>
</workzag-jobs>
`

func TestPersonioXMLUnmarshalsRealFeedShape(t *testing.T) {
	var parsed personioResponse
	if err := xml.Unmarshal([]byte(personioFixtureXML), &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(parsed.Positions) != 2 {
		t.Fatalf("expected 2 positions, got %d", len(parsed.Positions))
	}
	if parsed.Positions[0].ID != "2523845" || parsed.Positions[0].Office != "Hamburg" {
		t.Errorf("position 0 = %+v", parsed.Positions[0])
	}
}

// A board slug reaches the readers from a regex over scraped HTML, so a slug
// shaped to steer the request at another host has to be refused before
// anything is sent. Every provider shares one helper now; this is the check
// that the shared path still enforces what each hand-written copy used to.
func TestEveryATSReaderRefusesAHostileSlug(t *testing.T) {
	const hostile = "evil.example.com/x?"
	readers := map[string]func(string) error{
		"greenhouse":      func(s string) error { _, err := fetchGreenhouseJobs(s); return err },
		"lever":           func(s string) error { _, err := fetchLeverJobs(s); return err },
		"ashby":           func(s string) error { _, err := fetchAshbyJobs(s); return err },
		"smartrecruiters": func(s string) error { _, err := fetchSmartRecruitersJobs(s); return err },
		"keka":            func(s string) error { _, err := fetchKekaJobs(s); return err },
		"workable":        func(s string) error { _, err := fetchWorkableJobs(s); return err },
		"recruitee":       func(s string) error { _, err := fetchRecruiteeJobs(s); return err },
		"personio":        func(s string) error { _, err := fetchPersonioJobs(s); return err },
	}
	for name, read := range readers {
		err := read(hostile)
		if err == nil {
			t.Fatalf("%s accepted a hostile slug", name)
		}
		// Not just "an error": a network error would mean the request went
		// out and the guard did not stop it.
		if !strings.Contains(err.Error(), "invalid "+name+" slug") {
			t.Fatalf("%s rejected for the wrong reason: %v", name, err)
		}
	}
}

// A reader that fails must hand back no rows, not a half-filled slice the
// caller could mistake for a board with nothing on it.
func TestFailedATSReadReturnsNoRows(t *testing.T) {
	rows, err := fetchGreenhouseJobs("bad slug!")
	if err == nil {
		t.Fatal("expected an error")
	}
	if rows != nil {
		t.Fatalf("expected no rows, got %d", len(rows))
	}
}
