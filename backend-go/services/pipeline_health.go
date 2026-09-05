package services

import (
	"fmt"
	"gorm.io/gorm"
	"log"
	"time"

	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/config"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/models"
)

// Whether the pipeline is actually working, answered by the pipeline itself.
//
// Everything this file checks was previously answerable only by reading the
// log at the right moment, which is not a monitoring strategy — it is a person
// remembering to look. And the failures that matter here are all quiet ones:
// a throttled provider, a queue that stopped draining, a sync rotation falling
// behind. None of them error, none of them page anyone, and every one of them
// ends with the directory going stale while the service reports itself up.
//
// So the checks run on a schedule and say so in one line, and the same answer
// is available on an endpoint for anyone who would rather ask than wait.

// HealthCheck is one question and its answer.
type HealthCheck struct {
	Name string `json:"name"`
	OK   bool   `json:"ok"`
	// Detail is written for a person, and states the number the verdict was
	// reached on rather than only the verdict.
	Detail string `json:"detail"`
}

// PipelineHealth is the whole report.
type PipelineHealth struct {
	Healthy   bool          `json:"healthy"`
	CheckedAt time.Time     `json:"checked_at"`
	Checks    []HealthCheck `json:"checks"`
}

// processStartedAt is when this instance came up.
//
// Two checks need it, for the same reason: on a fresh deploy the pipeline has
// not run yet, and "has not run yet" must not read as "is broken". Reporting
// unhealthy at boot is worse than useless when the endpoint is wired to a
// platform health check — the deploy is rolled back for failing a test of work
// it has not had time to do, and the rollback restores a build that fails it
// the same way.
var processStartedAt = time.Now()

// startupGrace is how long after boot those checks stay quiet.
//
// Longer than the intervals they describe: collection runs six-hourly and the
// sync rotation is sized to turn the directory over daily, so a verdict before
// then is a verdict about the clock rather than about the pipeline.
const startupGrace = 26 * time.Hour

const (
	// admissionStallWindow is how long the queue may go without a single
	// candidate being settled before that counts as stalled. Admission ticks
	// every fifteen minutes, so an hour is four missed passes — long enough
	// that a slow pass is not an alarm, short enough to catch a stuck one.
	admissionStallWindow = time.Hour

	// syncStaleWindow is how far behind the job-sync rotation may fall. The
	// rotation is sized to turn the directory over daily; two days means it
	// is not keeping up and listings are going stale.
	syncStaleWindow = 48 * time.Hour
)

// CheckPipelineHealth answers, in one pass, whether roles are still arriving.
func CheckPipelineHealth() PipelineHealth {
	h := PipelineHealth{Healthy: true, CheckedAt: time.Now()}

	add := func(name string, ok bool, format string, args ...interface{}) {
		h.Checks = append(h.Checks, HealthCheck{
			Name: name, OK: ok, Detail: fmt.Sprintf(format, args...),
		})
		if !ok {
			h.Healthy = false
		}
	}

	// 1. Is there a directory at all.
	var companies, hiring, jobs int64
	config.DB.Model(&models.Company{}).Count(&companies)
	config.DB.Model(&models.Company{}).Where("open_roles > 0").Count(&hiring)
	config.DB.Model(&models.Job{}).Count(&jobs)
	add("directory", companies > 0 && jobs > 0,
		"%d companies, %d hiring, %d open roles", companies, hiring, jobs)

	// 2. Is the queue draining.
	//
	// The check that matters most, and the one with a deliberately careful
	// verdict: a queue with nothing due is healthy — it means admission has
	// caught up — while a queue with thousands due and nothing settled in an
	// hour is the pipeline being refused. "stored=0" reads identically in
	// both cases, which is exactly why it was never a usable signal.
	depth, err := CandidateQueueDepth()
	if err != nil {
		add("candidate queue", false, "could not be read: %v", err)
	} else {
		due, _ := DueCandidateCount()
		var settledRecently int64
		config.DB.Model(&models.BoardCandidate{}).
			Where("updated_at > ?", time.Now().Add(-admissionStallWindow)).
			Count(&settledRecently)

		stalled := due > 0 && settledRecently == 0
		add("candidate queue", !stalled,
			"%d due, %d settled in the last hour | pending=%d deferred=%d dead=%d foreign=%d stored=%d attached=%d",
			due, settledRecently,
			depth[models.CandidatePending], depth[models.CandidateDeferred],
			depth[models.CandidateDead], depth[models.CandidateForeign],
			depth[models.CandidateStored], depth[models.CandidateAttached])

		// A deferred pile that dwarfs everything else means hosts are
		// refusing us, which is a different problem from a slow queue and
		// needs saying separately — it is the failure that used to be
		// invisible.
		deferred := depth[models.CandidateDeferred]
		add("providers accepting us", deferred < 500,
			"%d candidates waiting on a retry", deferred)
	}

	// 3. Are we being throttled right now.
	throttled := ThrottledHosts()
	detail := "no host is pacing us"
	if len(throttled) > 0 {
		detail = ""
		for i, t := range throttled {
			if i > 0 {
				detail += "; "
			}
			detail += fmt.Sprintf("%s at %s (%d strikes)", t.Host, t.Interval, t.Strikes)
		}
	}
	// Not a failure on its own: backing off when asked is the system working.
	// It is reported so a quiet harvest has a visible cause.
	add("host pacing", true, "%s", detail)

	// 4. Is the sync rotation keeping up.
	//
	// Within the startup grace this reports without judging. The column is
	// populated by the rotation itself, so on the first boot after it was
	// introduced every company reads as never-synced — which is a fact about
	// the column's age, not about the directory going stale.
	young := time.Since(processStartedAt) < startupGrace
	var stale int64
	config.DB.Model(&models.Company{}).
		Where("last_synced_at IS NULL OR last_synced_at < ?", time.Now().Add(-syncStaleWindow)).
		Count(&stale)
	// Allowed to be non-zero right after a big harvest: those companies were
	// synced at the moment they were stored. The threshold is a share of the
	// directory rather than a count, so it means the same thing at any size.
	tolerance := companies / 4
	if young {
		add("sync rotation", true,
			"%d of %d companies awaiting their first turn (within %s of startup)",
			stale, companies, startupGrace)
	} else {
		add("sync rotation", stale <= tolerance,
			"%d of %d companies not synced in %s", stale, companies, syncStaleWindow)
	}

	// 5. Has collection ever run.
	var state models.HarvestState
	if err := config.DB.Where("source = ?", SourceStartupRegister).First(&state).Error; err != nil {
		add("collection", young, "no startup register collection has been run yet%s",
			map[bool]string{true: " (within startup grace)", false: ""}[young])
	} else {
		add("collection", true, "last read %s at %s",
			state.LastIndex, state.LastRunAt.Format(time.RFC3339))
	}

	// 6. Does the denormalised counter still match the rows it counts.
	//
	// open_roles is what the directory listing sorts and filters on, so a
	// drift here is not a statistic — it is companies appearing in the wrong
	// order, or not appearing. RecountOpenRoles repairs it; this notices.
	var drift int64
	config.DB.Raw(`
		SELECT COUNT(*) FROM companies c
		WHERE c.open_roles IS DISTINCT FROM (
			SELECT COUNT(*) FROM jobs j WHERE j.company_id = c.id
		)
	`).Scan(&drift)
	add("open-role counter", drift == 0, "%d companies out of step with the jobs table", drift)

	return h
}

// LogPipelineHealth runs the checks and writes them out, loudly when something
// is wrong.
//
// The point of the prefix is that a failing line is greppable and a passing one
// is ignorable, so nobody has to watch a terminal to know which it was.
func LogPipelineHealth() {
	h := CheckPipelineHealth()
	if h.Healthy {
		log.Printf("pipeline health: OK")
		for _, c := range h.Checks {
			log.Printf("pipeline health:   %s — %s", c.Name, c.Detail)
		}
		return
	}
	log.Printf("PIPELINE UNHEALTHY — roles may have stopped arriving")
	for _, c := range h.Checks {
		mark := "ok  "
		if !c.OK {
			mark = "FAIL"
		}
		log.Printf("PIPELINE %s %s — %s", mark, c.Name, c.Detail)
	}
}

// RunStartupRepairs brings the derived columns into line after a deploy.
//
// Both were introduced with data already in the tables behind them, so on the
// first boot after this ships companies.open_roles is zero everywhere and
// jobs.field/level are empty — which would mean an empty-looking directory and
// facet counts that undercount, until something filled them in. Something has
// to be this function, running on boot, rather than an operator remembering a
// command: a migration nobody runs is a migration that did not happen.
//
// Cheap and idempotent on every later boot: the recount touches only rows that
// disagree, and the backfill finds nothing once the columns are populated.
// RunBlockingStartupRepairs is the part that must complete before the server
// answers its first request.
//
// companies.open_roles is what the directory listing filters, sorts and badges
// on. Immediately after the migration that adds it, it is zero for every row —
// so a server that starts serving before the recount finishes answers
// "hiring=1" with nothing and orders everything else at random. Measured
// against the live directory: 535 companies, 6,189 roles, every counter zero
// until this ran, and the repair itself took 204ms. There is no reason to race
// it, and every reason not to.
//
// Idempotent and cheap on every later boot: one UPDATE that touches only the
// rows that disagree.
func RunBlockingStartupRepairs() {
	if n, err := RecountOpenRoles(); err != nil {
		log.Printf("startup repair: could not recount open roles: %v", err)
	} else if n > 0 {
		log.Printf("startup repair: corrected open_roles on %d companies", n)
	}

	// last_synced_at is written by the rotation, so on the first boot after it
	// was added every company reads as never-synced and all of them crowd the
	// front of the queue at once. They are not actually unsynced: the hourly
	// job sync this replaces was refreshing all of them, it simply had nowhere
	// to record that. Seeding from created_at keeps the ordering meaningful —
	// the oldest companies still go first — without claiming a sync happened
	// at a moment we cannot know.
	if config.DB.Migrator().HasColumn(&models.Company{}, "last_synced_at") {
		res := config.DB.Model(&models.Company{}).
			Where("last_synced_at IS NULL").
			Update("last_synced_at", gorm.Expr("created_at"))
		if res.Error != nil {
			log.Printf("startup repair: could not seed last_synced_at: %v", res.Error)
		} else if res.RowsAffected > 0 {
			log.Printf("startup repair: seeded last_synced_at on %d companies", res.RowsAffected)
		}
	}
}

// RunStartupRepairs finishes the work that can happen while the server is
// already answering.
//
// The facet columns degrade rather than break: a job with no stored bucket
// counts as "Other"/"Unspecified" until it is classified, which is a wrong
// filter count for a few seconds rather than a missing directory. Time-bounded
// so a large jobs table cannot hold the boot goroutine indefinitely; the
// scheduled pass finishes whatever is left.
func RunStartupRepairs() {
	deadline := time.Now().Add(2 * time.Minute)
	total := 0
	for time.Now().Before(deadline) {
		n, err := BackfillJobFacets(2000)
		if err != nil {
			log.Printf("startup repair: job facet backfill failed: %v", err)
			break
		}
		if n == 0 {
			break
		}
		total += n
	}
	if total > 0 {
		log.Printf("startup repair: classified %d jobs into field/level buckets", total)
	}
}

// RunFacetBackfill finishes any classification the boot pass did not reach,
// and reclassifies rows whenever the bucket rules change.
func RunFacetBackfill() {
	total := 0
	for pass := 0; pass < 25; pass++ {
		n, err := BackfillJobFacets(2000)
		if err != nil {
			log.Printf("job facet backfill failed: %v", err)
			return
		}
		if n == 0 {
			break
		}
		total += n
	}
	if total > 0 {
		log.Printf("job facet backfill: classified %d jobs", total)
	}
}
