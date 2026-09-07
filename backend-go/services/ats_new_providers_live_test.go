package services

import (
	"os"
	"testing"
)

// Skipped without LIVE_ATS_CHECK, same convention as live_db_test.go — these
// hit real, unauthenticated third-party boards over the network, so `go test
// ./...` stays offline by default. Useful when one of these four providers
// changes its response shape and a sync starts erroring: run this first to
// see which one broke before touching the parsing code.
func liveATSCheckEnabled(t *testing.T) {
	if os.Getenv("LIVE_ATS_CHECK") == "" {
		t.Skip("set LIVE_ATS_CHECK=1 to run live checks against real ATS boards")
	}
}

func TestLiveRecruiteeBoardIsReadable(t *testing.T) {
	liveATSCheckEnabled(t)
	offers, err := fetchRecruiteeJobs("yource")
	if err != nil {
		t.Fatalf("fetchRecruiteeJobs: %v", err)
	}
	if len(offers) == 0 {
		t.Fatal("expected at least one offer from a known-live board")
	}
	if offers[0].Title == "" || offers[0].CareersURL == "" {
		t.Errorf("offer missing expected fields: %+v", offers[0])
	}
}

func TestLivePersonioBoardIsReadable(t *testing.T) {
	liveATSCheckEnabled(t)
	positions, err := fetchPersonioJobs("gus-germany.jobs.personio.de")
	if err != nil {
		t.Fatalf("fetchPersonioJobs: %v", err)
	}
	if len(positions) == 0 {
		t.Fatal("expected at least one position from a known-live board")
	}
	if positions[0].ID == "" || positions[0].Name == "" {
		t.Errorf("position missing expected fields: %+v", positions[0])
	}
}

func TestLiveFreshteamBoardIsReadable(t *testing.T) {
	liveATSCheckEnabled(t)
	jobs, err := fetchFreshteamJobs("stllr")
	if err != nil {
		t.Fatalf("fetchFreshteamJobs: %v", err)
	}
	if len(jobs) != 3 {
		t.Fatalf("expected 3 jobs from the known-live board, got %d", len(jobs))
	}
	wantDepts := map[string]string{
		"Senior Accountant (Remote)":              "Administration",
		"AI / ML Intern (Paid Remote)":            "Tech",
		"Mid-Senior Fullstack Developer (Remote)": "Tech",
	}
	for _, j := range jobs {
		if want := wantDepts[j.Title]; want != "" && j.Department != want {
			t.Errorf("%q: department = %q, want %q", j.Title, j.Department, want)
		}
	}
}

func TestLiveGemBoardIsReadable(t *testing.T) {
	liveATSCheckEnabled(t)
	postings, err := fetchGemJobs("modular")
	if err != nil {
		t.Fatalf("fetchGemJobs: %v", err)
	}
	if len(postings) == 0 {
		t.Fatal("expected at least one posting from a known-live board")
	}
	if postings[0].Title == "" || postings[0].ExtID == "" {
		t.Errorf("posting missing expected fields: %+v", postings[0])
	}
}
