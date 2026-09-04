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
	// SlugCollectionLeaseName covers reading a source into the queue.
	SlugCollectionLeaseName = "slug-collection"
	// CandidateAdmissionLeaseName covers judging queued candidates.
	CandidateAdmissionLeaseName = "candidate-admission"
)

const (
	// slugCollectionLeaseTTL spans a full collection tick. Reading ten hosts
	// across eight pages at a two-second pace takes about twenty minutes.
	slugCollectionLeaseTTL = 90 * time.Minute

	// candidateAdmissionLeaseTTL spans one admission tick with slack, so a
	// pass that overruns its budget slightly is not lapped by the next one.
	candidateAdmissionLeaseTTL = 14 * time.Minute
	// candidateAdmissionBudget is the wall clock one pass may spend. Shorter
	// than the lease, because the point of the budget is that the pass ends
	// before the lease does rather than at the same moment.
	candidateAdmissionBudget = 11 * time.Minute
	// candidateAdmissionBatch caps how many rows one pass takes off the
	// queue. Sized so a full Common Crawl index — about 13,500 candidates —
	// is worked through in roughly four hours of ordinary ticks, while any
	// single pass stays small enough to finish inside its budget.
	candidateAdmissionBatch = 800
)

// RunSlugCollection tops the candidate queue up from Common Crawl.
//
// Cheap to call often and expensive only when there is something new. Common
// Crawl publishes about one index a month; a tick that finds the index it
// already read logs one line and returns, having spent a single request. The
// cadence therefore decides how promptly a new index is noticed, not how often
// ten thousand URLs are re-read.
func RunSlugCollection() {
	if !AcquireCronLease(SlugCollectionLeaseName, slugCollectionLeaseTTL) {
		log.Printf("slug collection: another instance holds the lease, skipping")
		return
	}

	indexes, err := LatestCommonCrawlIndexes(1)
	if err != nil || len(indexes) == 0 {
		log.Printf("slug collection: could not read the Common Crawl index list: %v", err)
		return
	}
	newest := indexes[0]

	var state models.HarvestState
	if err := config.DB.Where("source = ?", SourceCommonCrawl).First(&state).Error; err == nil &&
		state.LastIndex == newest {
		// Nothing new to collect. This is not an idle pipeline: the queue
		// still holds every board this index ever produced, and admission
		// re-reads each of them monthly, so companies that start hiring keep
		// arriving between crawls without a new index existing at all.
		log.Printf("slug collection: %s already read (last run %s) — the queue continues on its own",
			newest, state.LastRunAt.Format(time.RFC3339))
		return
	}

	log.Printf("slug collection: %s is new, reading", newest)
	candidates := HarvestFromCommonCrawl([]string{newest})
	if len(candidates) == 0 {
		// A crawl index that yields nothing is a failed read, not an empty
		// internet. Recording it would skip the index permanently.
		log.Printf("slug collection: %s produced no candidates — not recording it as read", newest)
		return
	}

	added, err := EnqueueCandidates(candidates)
	if err != nil {
		log.Printf("slug collection: %s not enqueued: %v", newest, err)
		return
	}
	log.Printf("slug collection: %s -> %d candidates seen, %d new rows queued",
		newest, len(candidates), added)

	if err := config.DB.Save(&models.HarvestState{
		Source:    SourceCommonCrawl,
		LastIndex: newest,
		LastRunAt: time.Now(),
		Stored:    added,
	}).Error; err != nil {
		log.Printf("slug collection: could not record %s as read: %v", newest, err)
	}
}

// RunCandidateAdmission judges a bounded slice of the queue.
//
// This is the tick that actually produces companies and roles, and it is
// deliberately small and frequent. A pass that takes 800 candidates every
// fifteen minutes works through a whole Common Crawl index in about four
// hours, and — more importantly — always finishes, so it can never be running
// when the next tick fires.
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
	// health check. stored=0 with an empty due count is a pipeline that has
	// caught up; stored=0 with thousands due and a deferred pile is a
	// pipeline being refused, and those need opposite responses.
	if due, derr := DueCandidateCount(); derr == nil {
		log.Printf("candidate admission: %s | %d still due", stats, due)
	} else {
		log.Printf("candidate admission: %s", stats)
	}
}

// HarvestOptions is what an operator chose on the command line.
type HarvestOptions struct {
	// CommonCrawl reads board slugs out of the published URL index. Cheap and
	// broad: about twenty requests for thousands of slugs.
	CommonCrawl bool
	// StartupRegister walks the accelerator portfolios for companies that
	// arrive with a sector, a stage and coordinates. Slow and narrow.
	StartupRegister bool
	// Indexes is how many recent Common Crawl indexes to read. More is worth
	// it: a second index added 204 Keka slugs and 745 Greenhouse ones the
	// first did not have, because boards open and close between crawls.
	Indexes int
	// Limit caps companies stored, and for the register also caps pages read.
	// Zero means no cap, which only a deliberate backfill should use.
	Limit int
	// Admit runs admission passes after collecting, until the queue has no
	// due rows left. Off by default: collecting is minutes, draining a fresh
	// index is hours, and an operator should choose to spend them.
	Admit bool
}

// RunHarvest is the manual backfill: collect from the chosen sources into the
// queue, and optionally drain it.
//
// It lives here rather than in main so the candidate type stays unexported:
// assembling candidates is this package's business, and a caller that could
// build one by hand could bypass the admission rules that make a board
// trustworthy.
func RunHarvest(opts HarvestOptions) (HarvestStats, error) {
	var candidates []slugCandidate

	if opts.CommonCrawl {
		n := opts.Indexes
		if n <= 0 {
			n = 1
		}
		ids, err := LatestCommonCrawlIndexes(n)
		if err != nil {
			return HarvestStats{}, err
		}
		log.Printf("harvest: reading Common Crawl indexes %v", ids)
		candidates = append(candidates, HarvestFromCommonCrawl(ids)...)
	}

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
