package services

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/config"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/models"
)

// Every query this change rewrote, run once against a real Postgres.
//
// The rest of the suite is pure functions, which is the right shape for rules
// and schedules and useless for the thing that actually breaks here: SQL that
// compiles in Go and fails in the database. Two such faults shipped in this
// branch before this file existed — a SUM scanned into the wrong pointer
// depth, and a *gorm.DB reused across two aggregate queries — and neither was
// reachable by any test that did not connect.
//
// Skipped without DATABASE_URL, so `go test ./...` stays offline by default.
// Read-only apart from AutoMigrate and a single candidate row it inserts and
// deletes, so it is safe to point at the live directory: nothing here writes
// to companies or jobs.
func liveDB(t *testing.T) {
	t.Helper()
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("set DATABASE_URL to run the live database checks")
	}
	if config.DB == nil {
		config.ConnectDB()
	}
	if config.DB == nil {
		t.Fatal("no database connection")
	}
}

// AutoMigrate has to be able to create the queue table before anything can use
// it: a composite primary key across two string columns, plus the ordered
// index the due-work query depends on.
func TestLiveAutoMigrateCandidateQueue(t *testing.T) {
	liveDB(t)
	if err := config.DB.AutoMigrate(
		&models.Company{}, &models.Job{}, &models.BoardCandidate{}, &models.HarvestState{},
	); err != nil {
		t.Fatalf("AutoMigrate failed: %v", err)
	}
	if !config.DB.Migrator().HasTable(&models.BoardCandidate{}) {
		t.Fatal("board_candidates was not created")
	}
	for _, col := range []string{"provider", "slug", "status", "next_attempt_at", "attempts"} {
		if !config.DB.Migrator().HasColumn(&models.BoardCandidate{}, col) {
			t.Errorf("board_candidates.%s missing", col)
		}
	}
	for _, col := range []string{"open_roles", "last_synced_at"} {
		if !config.DB.Migrator().HasColumn(&models.Company{}, col) {
			t.Errorf("companies.%s missing", col)
		}
	}
	for _, col := range []string{"field", "level", "last_checked_at"} {
		if !config.DB.Migrator().HasColumn(&models.Job{}, col) {
			t.Errorf("jobs.%s missing", col)
		}
	}
}

// EnqueueCandidates upserts with CASE ... excluded.<col> expressions, which
// GORM has to render into a valid ON CONFLICT DO UPDATE. A re-enqueue must
// leave an existing row's status and schedule alone while filling in detail
// the first sighting did not have — collection must never reset the progress
// admission has made.
func TestLiveEnqueueUpsertsWithoutResettingProgress(t *testing.T) {
	liveDB(t)
	const provider, slug = "greenhouse", "zz-neurofiq-selftest"
	defer config.DB.Where("provider = ? AND slug = ?", provider, slug).
		Delete(&models.BoardCandidate{})

	if _, err := EnqueueCandidates([]slugCandidate{
		{Provider: provider, Slug: slug, Source: "probe"},
	}); err != nil {
		t.Fatalf("first enqueue failed: %v", err)
	}

	// Pretend admission judged it and put it on the monthly re-check.
	var row models.BoardCandidate
	if err := config.DB.Where("provider = ? AND slug = ?", provider, slug).
		First(&row).Error; err != nil {
		t.Fatalf("row was not written: %v", err)
	}
	if err := settleCandidate(row, models.CandidateDead, "", nil); err != nil {
		t.Fatalf("settle failed: %v", err)
	}

	// A later source sees the same board and knows more about it.
	if _, err := EnqueueCandidates([]slugCandidate{{
		Provider: provider, Slug: slug, Source: SourceStartupRegister,
		Name: "Selftest", Sector: "AI", Stage: "Seed",
	}}); err != nil {
		t.Fatalf("second enqueue failed: %v", err)
	}

	var after models.BoardCandidate
	if err := config.DB.Where("provider = ? AND slug = ?", provider, slug).
		First(&after).Error; err != nil {
		t.Fatalf("row vanished: %v", err)
	}
	if after.Status != models.CandidateDead {
		t.Errorf("status reset to %q — a re-collection must not undo admission's work", after.Status)
	}
	if after.NextAttemptAt.Before(time.Now().Add(20 * 24 * time.Hour)) {
		t.Error("the monthly re-check was reset; the board would be re-read immediately")
	}
	if after.Sector != "AI" || after.Name != "Selftest" {
		t.Errorf("detail was not filled in: name=%q sector=%q", after.Name, after.Sector)
	}
}

// The due-work query and the two aggregates the health check reads.
func TestLiveQueueQueries(t *testing.T) {
	liveDB(t)
	if _, err := DueCandidates(5); err != nil {
		t.Errorf("DueCandidates: %v", err)
	}
	if _, err := DueCandidateCount(); err != nil {
		t.Errorf("DueCandidateCount: %v", err)
	}
	if _, err := CandidateQueueDepth(); err != nil {
		t.Errorf("CandidateQueueDepth: %v", err)
	}
}

// The directory listing, in the shapes a visitor actually produces. This is
// the rewrite that dropped the LEFT JOIN, the GROUP BY, the HAVING and the
// ORDER BY over an aggregate, so every one of those paths needs to still
// return rows rather than an error.
func TestLiveListCompaniesAcrossFilters(t *testing.T) {
	liveDB(t)
	cases := []struct {
		name                   string
		sector, stage, area, q string
		hiring                 bool
	}{
		{name: "default"},
		{name: "hiring only", hiring: true},
		{name: "area", area: "Bengaluru"},
		{name: "text search", q: "ai"},
		{name: "unknown stage", stage: UnknownFacetValue},
		{name: "unknown sector", sector: UnknownFacetValue},
		{name: "matches nothing", sector: "NoSuchSectorXYZ"},
		{name: "combined", area: "Pune", stage: UnknownFacetValue, hiring: true},
	}
	for _, c := range cases {
		rows, total, err := ListCompanies(c.sector, c.stage, c.area, c.q, c.hiring, 1, 5)
		if err != nil {
			t.Errorf("ListCompanies(%s): %v", c.name, err)
			continue
		}
		t.Logf("ListCompanies(%s): %d rows, total %d", c.name, len(rows), total)
		for _, r := range rows {
			// The badge and the sort key are the same column now, so a row
			// whose count disagrees with its own field is a bug in the write
			// path rather than in the query.
			if r.JobCount != int64(r.OpenRoles) {
				t.Errorf("%s: job_count %d != open_roles %d", r.Name, r.JobCount, r.OpenRoles)
			}
			if r.Lat == nil || r.Lng == nil {
				t.Errorf("%s: listing returned no coordinates", r.Name)
			}
		}
	}
}

// The header counts, including the empty case that broke the first version:
// SUM over no matching rows is NULL.
func TestLiveTotalOpenRoles(t *testing.T) {
	liveDB(t)
	// The repair the server runs before it serves. Without it this compares a
	// column that was created moments ago, all zeroes, against the jobs table —
	// which is exactly the deploy-order fault this found the first time it ran,
	// and it belongs in the boot sequence rather than in the test's assumptions.
	RunBlockingStartupRepairs()

	fast, err := TotalOpenRolesFast("", "", "")
	if err != nil {
		t.Fatalf("TotalOpenRolesFast: %v", err)
	}
	joined, err := TotalOpenRoles("", "", "", "")
	if err != nil {
		t.Fatalf("TotalOpenRoles: %v", err)
	}
	t.Logf("open roles: counter=%d joined=%d", fast, joined)
	if fast != joined {
		t.Errorf("the counter says %d open roles and the jobs table says %d — "+
			"the directory header would disagree with the listings under it", fast, joined)
	}

	// The narrowing case: a filter nothing matches.
	empty, err := TotalOpenRolesFast("NoSuchSectorXYZ", "", "")
	if err != nil {
		t.Fatalf("TotalOpenRolesFast on an empty match: %v", err)
	}
	if empty != 0 {
		t.Errorf("an unmatched filter returned %d roles", empty)
	}
}

// Facet counts now come from stored columns via GROUP BY rather than from
// classifying every title in Go, so the totals have to still add up.
func TestLiveJobFacets(t *testing.T) {
	liveDB(t)
	// Same reason: the buckets are stored, so they have to exist before the
	// counts mean anything. Unclassified rows all collapse into one bucket,
	// which is a filter that looks broken rather than one that errors.
	RunStartupRepairs()

	fields, levels, err := JobFacets("", "", "", "")
	if err != nil {
		t.Fatalf("JobFacets: %v", err)
	}
	var fieldTotal, levelTotal int
	for _, f := range fields {
		fieldTotal += f.Count
	}
	for _, l := range levels {
		levelTotal += l.Count
	}
	t.Logf("facets: %d field buckets (%d roles), %d level buckets (%d roles)",
		len(fields), fieldTotal, len(levels), levelTotal)

	// Every role lands in exactly one bucket of each kind, so the two totals
	// must match each other. They would not if the second query had inherited
	// the first's GROUP BY — the failure this file was written to catch.
	if fieldTotal != levelTotal {
		t.Errorf("field buckets total %d but level buckets total %d — "+
			"the two aggregates are not counting the same rows", fieldTotal, levelTotal)
	}

	var jobs int64
	config.DB.Model(&models.Job{}).Count(&jobs)
	if fieldTotal != int(jobs) {
		t.Errorf("facets cover %d roles but the table holds %d", fieldTotal, jobs)
	}
	// A single bucket holding everything is the signature of unclassified rows
	// reaching the COALESCE default — the filter renders, and is useless.
	if len(fields) < 2 || len(levels) < 2 {
		t.Errorf("only %d field and %d level buckets: the stored columns are not populated",
			len(fields), len(levels))
	}
}

func TestLiveCompanyFacets(t *testing.T) {
	liveDB(t)
	sectors, stages, err := CompanyFacets()
	if err != nil {
		t.Fatalf("CompanyFacets: %v", err)
	}
	t.Logf("facet options: sectors=%v stages=%v", sectors, stages)
	if len(sectors) == 0 || len(stages) == 0 {
		t.Error("a filter with no options renders as an empty dropdown")
	}
}

// The counter repair, and the health report that reads it.
func TestLiveRecountAndHealth(t *testing.T) {
	liveDB(t)
	corrected, err := RecountOpenRoles()
	if err != nil {
		t.Fatalf("RecountOpenRoles: %v", err)
	}
	t.Logf("open-role recount corrected %d companies", corrected)

	// Idempotent: a second pass immediately after must find nothing left.
	again, err := RecountOpenRoles()
	if err != nil {
		t.Fatalf("RecountOpenRoles (second pass): %v", err)
	}
	if again != 0 {
		t.Errorf("recount is not idempotent: second pass corrected %d more", again)
	}

	h := CheckPipelineHealth()
	for _, c := range h.Checks {
		mark := "ok"
		if !c.OK {
			mark = "FAIL"
		}
		t.Logf("health %-24s %-4s %s", c.Name, mark, c.Detail)
	}
}

// The sync rotation's ordering clause — NULLS FIRST is Postgres-specific and
// silently valid-looking in Go.
func TestLiveSyncRotationOrdering(t *testing.T) {
	liveDB(t)
	var companies []models.Company
	if err := config.DB.
		Order("last_synced_at ASC NULLS FIRST").
		Limit(5).
		Find(&companies).Error; err != nil {
		t.Fatalf("staleness ordering failed: %v", err)
	}
	t.Logf("rotation head: %d companies", len(companies))

	var jobs []models.Job
	if err := config.DB.
		Where("source = ?", careersPageSource).
		Order("last_checked_at ASC NULLS FIRST").
		Limit(5).
		Find(&jobs).Error; err != nil {
		t.Fatalf("prune ordering failed: %v", err)
	}
	t.Logf("prune head: %d careers-page jobs", len(jobs))
}

// One admission pass with a tiny budget, to prove the whole path runs against
// the real schema. It judges nothing unless there is queued work, and the
// budget stops it either way.
func TestLiveAdmissionPassRuns(t *testing.T) {
	liveDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	stats, err := AdmitDueCandidates(ctx, 3)
	if err != nil {
		t.Fatalf("AdmitDueCandidates: %v", err)
	}
	t.Logf("admission pass: %s", stats)
}

// The whole admission path, end to end, on a slug chosen because no such board
// exists: Greenhouse answers 404, which is a board saying it is not there. That
// is the one outcome that can be asserted without writing a company row, and it
// is the outcome the throttle bug used to counterfeit — a 429 arrived here as
// the same "dead", so proving 404 lands as dead is only half the point. The
// other half is that it lands as dead rather than as deferred, which is what
// separates a real answer from a retry.
func TestLiveAdmissionSettlesADeadBoard(t *testing.T) {
	liveDB(t)
	const provider, slug = "greenhouse", "zz-neurofiq-nonexistent-board"
	defer config.DB.Where("provider = ? AND slug = ?", provider, slug).
		Delete(&models.BoardCandidate{})

	if _, err := EnqueueCandidates([]slugCandidate{
		{Provider: provider, Slug: slug, Source: "probe"},
	}); err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	// A large batch, because the queue is ordered by due time and this row has
	// to actually be reached rather than sorted behind whatever else is due.
	if _, err := AdmitDueCandidates(ctx, 2000); err != nil {
		t.Fatalf("AdmitDueCandidates: %v", err)
	}

	var after models.BoardCandidate
	if err := config.DB.Where("provider = ? AND slug = ?", provider, slug).
		First(&after).Error; err != nil {
		t.Fatalf("candidate row vanished: %v", err)
	}
	t.Logf("settled as %q, next attempt %s, attempts=%d, err=%q",
		after.Status, after.NextAttemptAt.Format(time.RFC3339), after.Attempts, after.LastError)

	if after.Status == models.CandidatePending {
		t.Fatal("the candidate was never judged")
	}
	if after.Status != models.CandidateDead {
		t.Errorf("a 404 board settled as %q, want %q", after.Status, models.CandidateDead)
	}
	// And it must come back, or a company that opens this board later is
	// invisible forever.
	gap := time.Until(after.NextAttemptAt)
	if gap < 20*24*time.Hour || gap > 40*24*time.Hour {
		t.Errorf("re-check scheduled %s away, want roughly a month", gap)
	}
}
