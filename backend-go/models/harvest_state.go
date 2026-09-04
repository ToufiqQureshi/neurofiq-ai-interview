package models

import "time"

// HarvestState remembers which Common Crawl index a slug harvest has already
// read.
//
// The harvest is scheduled, but its source is not: Common Crawl publishes one
// index a month. A tick that re-reads the index it read last time collects the
// identical slug list and spends a board API call on every one of them to
// learn they are all duplicates — the run that produced this note ended
// `duplicate=227 stored=28`, which is the shape of a tick doing nothing
// expensively. At eight ticks a day that is tens of thousands of requests
// against other people's boards for no new companies.
//
// So the schedule can be as frequent as anyone likes, and the work still
// happens once per published index. Storing the index id is what makes the
// difference between "run every three hours" and "check every three hours".
type HarvestState struct {
	// Source is the harvester this row belongs to ("common-crawl"), so a
	// second source can keep its own place without a second table.
	Source string `gorm:"primaryKey" json:"source"`
	// LastIndex is the last Common Crawl index id read to completion, e.g.
	// "CC-MAIN-2026-34".
	LastIndex string `json:"last_index"`
	// LastRunAt and Stored are for the operator, not for the decision: they
	// answer "did this ever run, and did it find anything" without reading
	// the log.
	LastRunAt time.Time `json:"last_run_at"`
	Stored    int       `json:"stored"`
}
