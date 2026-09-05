package services

import (
	"context"
	"log"
	"time"

	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/config"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/models"
)

// Scheduling the two halves of the harvest.
//
// Collection tops the queue up from a source; admission works the queue down.
// They hold separate leases and neither waits on the other, which is what
// makes the pipeline continuous rather than lumpy: roles now appear every
// fifteen minutes as candidates are judged, instead of in a burst when a
// three-hourly run happened to get far enough.
//
// It also removes the failure that shaped the old design. Collection used to
// be unable to record what it had read unless admission had finished judging
// all of it, so a pass stopped by its own store limit recorded nothing and the
// next tick re-read the whole index — twelve thousand board calls to re-learn
// what it already knew. Recording collection when collection finishes is
// correct precisely because the queue, not the index marker, is now the
// progress.

const (
	// StartupRegisterCollectionLeaseName covers reading high-signal Indian startups into the queue.
	StartupRegisterCollectionLeaseName = "startup-register-collection"
	// CandidateAdmissionLeaseName covers judging queued candidates.
	CandidateAdmissionLeaseName = "candidate-admission"
)

const (
	// startupRegisterCollectionLeaseTTL covers a rapid register discovery pass.
	startupRegisterCollectionLeaseTTL = 3 * time.Minute

	// candidateAdmissionLeaseTTL spans one admission tick with slack.
	candidateAdmissionLeaseTTL = 3 * time.Minute
	// candidateAdmissionBudget is the wall clock one pass may spend.
	candidateAdmissionBudget = 2 * time.Minute
	// candidateAdmissionBatch caps how many rows one pass takes off the queue.
	// Running every 2 minutes allows rapid ingestion of newly admitted boards.
	candidateAdmissionBatch = 400
)

// RunStartupRegisterCollection tops the candidate queue up from verified Indian startup portfolios.
func RunStartupRegisterCollection() {
	if !AcquireCronLease(StartupRegisterCollectionLeaseName, startupRegisterCollectionLeaseTTL) {
		log.Printf("startup register collection: another instance holds the lease, skipping")
		return
	}

	// Read high-signal portfolio batches (25 companies per cycle for responsive 3m pacing)
	candidates := HarvestFromStartupRegister(nil, 25)
	if len(candidates) == 0 {
		log.Printf("startup register collection: no new candidates found in this pass")
		return
	}

	added, err := EnqueueCandidates(candidates)
	if err != nil {
		log.Printf("startup register collection: %v", err)
		return
	}
	log.Printf("startup register collection: %d candidates inspected, %d new rows queued",
		len(candidates), added)

	if err := config.DB.Save(&models.HarvestState{
		Source:    SourceStartupRegister,
		LastIndex: time.Now().Format("2006-01-02-15-04"),
		LastRunAt: time.Now(),
		Stored:    added,
	}).Error; err != nil {
		log.Printf("startup register collection: could not record state: %v", err)
	}
}

// RunCandidateAdmission judges a bounded slice of the queue.
//
// This is the tick that actually produces companies and roles, and it is
// deliberately small and frequent. A pass that takes candidates every 2 minutes
// admits companies swiftly and smoothly into production.
func RunCandidateAdmission() {
	if !AcquireCronLease(CandidateAdmissionLeaseName, candidateAdmissionLeaseTTL) {
		log.Printf("candidate admission: another instance holds the lease, skipping")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), candidateAdmissionBudget)
	defer cancel()

	stats, err := AdmitDueCandidates(ctx, candidateAdmissionBatch)
	if err != nil {
		log.Printf("candidate admission: %v", err)
		return
	}

	// Queue depth beside the pass result, because the two together are the
	// health check.
	if due, derr := DueCandidateCount(); derr == nil {
		log.Printf("candidate admission: %s | %d still due", stats, due)
	} else {
		log.Printf("candidate admission: %s", stats)
	}
}

// HarvestOptions is what an operator chose on the command line.
type HarvestOptions struct {
	// StartupRegister walks the accelerator portfolios for companies that
	// arrive with a sector, a stage and coordinates.
	StartupRegister bool
	// Limit caps companies stored, and for the register also caps pages read.
	// Zero means no cap, which only a deliberate backfill should use.
	Limit int
	// Admit runs admission passes after collecting, until the queue has no
	// due rows left.
	Admit bool
}

// RunHarvest is the manual backfill: collect from the chosen sources into the
// queue, and optionally drain it.
func RunHarvest(opts HarvestOptions) (HarvestStats, error) {
	var candidates []slugCandidate

	if opts.StartupRegister {
		log.Printf("harvest: reading the startup register's accelerator portfolios")
		candidates = append(candidates, HarvestFromStartupRegister(nil, opts.Limit)...)
	}

	if len(candidates) > 0 {
		added, err := EnqueueCandidates(candidates)
		if err != nil {
			return HarvestStats{}, err
		}
		log.Printf("harvest: %d candidates collected, %d new rows queued", len(candidates), added)
	}

	if !opts.Admit {
		due, _ := DueCandidateCount()
		log.Printf("harvest: %d candidates now due — the scheduled admission will work through them, "+
			"or re-run with -admit to drain the queue now", due)
		return HarvestStats{Candidates: len(candidates)}, nil
	}

	// Drain in passes rather than one enormous one, so a backfill of thirteen
	// thousand candidates checkpoints its progress into the queue as it goes
	// and an interrupted run keeps everything it had judged.
	var total HarvestStats
	for pass := 1; ; pass++ {
		batch := candidateAdmissionBatch
		if opts.Limit > 0 && total.Stored >= opts.Limit {
			log.Printf("harvest: reached the -limit of %d stored companies", opts.Limit)
			break
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		stats, err := AdmitDueCandidates(ctx, batch)
		cancel()
		if err != nil {
			return total, err
		}
		if stats.Candidates == 0 {
			log.Printf("harvest: queue drained after %d passes", pass-1)
			break
		}
		total.Candidates += stats.Candidates
		total.Skipped += stats.Skipped
		total.DeadBoard += stats.DeadBoard
		total.NotIndian += stats.NotIndian
		total.Duplicate += stats.Duplicate
		total.Attached += stats.Attached
		total.Stored += stats.Stored
		total.Deferred += stats.Deferred
		log.Printf("harvest: pass %d done — running total %s", pass, total)
	}
	return total, nil
}
