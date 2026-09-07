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
