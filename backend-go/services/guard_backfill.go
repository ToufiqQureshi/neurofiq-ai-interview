package services

import (
	"log"

	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/config"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/models"
)

// Guard backfill.
//
// Every admission rule in this package runs at insert time and never again, so
// a rule written today leaves yesterday's violations sitting in the table. The
// day the aggregator and role-ceiling guards went in, Jogether's 4,440 roles
// and ten companies named after job postings were still stored, and clearing
// them meant a person typing DELETE statements against production. A directory
// whose whole claim is that it maintains itself cannot need that.
//
// So the guards run twice: once on the way in, and once on a schedule over
// what is already here. Any rule added later is backfilled by adding it to
// ReapplyGuards as well, and history obeys it without anyone being asked.
//
// The rules here are the ones whose violation is unambiguous — a row that
// today's pipeline would refuse to create. Anything needing judgement stays
// out: deleting is not reversible, and a guard that is only usually right does
// more damage running unattended than it repairs.

// ReapplyGuards removes stored rows the current rules would not accept.
// Returns how many jobs and companies it deleted.
func ReapplyGuards() (jobsRemoved, companiesRemoved int) {
	jobsRemoved += sweepForeignBoardRoles()

	jobsRemoved += sweepJunkTitles()

	removed, jobs := sweepAggregatorCompanies()
	companiesRemoved += removed
	jobsRemoved += jobs

	// Last, not first. Removing a company is itself a way to orphan roles, so
	// a sweep that runs before the other rules cleans up after the previous
	// run rather than this one. The first version ran it first and left 48
	// orphans behind on its own maiden run.
	jobsRemoved += sweepOrphanJobs()

	if jobsRemoved > 0 || companiesRemoved > 0 {
		log.Printf("guard backfill: removed %d companies and %d roles that current rules reject",
			companiesRemoved, jobsRemoved)
	}
	return jobsRemoved, companiesRemoved
}

// sweepOrphanJobs deletes roles whose company is gone.
//
// jobs has no foreign key to companies — the table is created by AutoMigrate
// and has no migration file — so deleting a company leaves its roles behind,
// counted by every total the directory reports. One cleanup left 6,396 of
// them, and the count of "open roles" stayed wrong until they were found by
// hand.
func sweepOrphanJobs() int {
	res := config.DB.
		Where("NOT EXISTS (SELECT 1 FROM companies WHERE companies.id = jobs.company_id)").
		Delete(&models.Job{})
	if res.Error != nil {
		log.Printf("guard backfill: orphan sweep failed: %v", res.Error)
		return 0
	}
	if res.RowsAffected > 0 {
		log.Printf("guard backfill: removed %d orphaned roles", res.RowsAffected)
	}
	return int(res.RowsAffected)
}

// sweepForeignBoardRoles deletes board roles located outside India.
//
// keepIndianRoles applies this on every sync, but only to companies that get
// synced: a company whose board has gone quiet keeps whatever was stored the
// last time the rule was different. Careers-page rows are left alone here for
// the same reason keepIndianRoles leaves them alone — that source describes
// location loosely, and judging it by the board rule deletes real openings.
func sweepForeignBoardRoles() int {
	var jobs []models.Job
	if err := config.DB.Where("source <> ?", careersPageSource).Find(&jobs).Error; err != nil {
		log.Printf("guard backfill: could not load board roles: %v", err)
		return 0
	}

	var doomed []string
	for _, j := range jobs {
		if !looksIndian(j.Location) {
			doomed = append(doomed, j.ID)
		}
	}
	if len(doomed) == 0 {
		return 0
	}

	res := config.DB.Where("id IN ?", doomed).Delete(&models.Job{})
	if res.Error != nil {
		log.Printf("guard backfill: foreign-role sweep failed: %v", res.Error)
		return 0
	}
	log.Printf("guard backfill: removed %d board roles located outside India", res.RowsAffected)
	return int(res.RowsAffected)
}

// sweepAggregatorCompanies removes companies whose board belongs to a job
// marketplace or staffing firm rather than to an employer.
//
// discoverFromBoards rejects these before storing anything, but that check was
// written after Jogether had already filed 4,440 of other employers' roles
// under one name — two thirds of every job the directory held.
func sweepAggregatorCompanies() (companies, jobs int) {
	var all []models.Company
	if err := config.DB.Find(&all).Error; err != nil {
		log.Printf("guard backfill: could not load companies: %v", err)
		return 0, 0
	}

	for _, c := range all {
		if !aggregatorBoardRe.MatchString(c.Name) && !aggregatorBoardRe.MatchString(c.ATSSlug) {
			continue
		}

		res := config.DB.Where("company_id = ?", c.ID).Delete(&models.Job{})
		if res.Error != nil {
			log.Printf("guard backfill: could not clear roles for %q: %v", c.Name, res.Error)
			continue
		}
		if err := config.DB.Delete(&models.Company{}, "id = ?", c.ID).Error; err != nil {
			log.Printf("guard backfill: could not remove %q: %v", c.Name, err)
			continue
		}

		log.Printf("guard backfill: removed %q (%s/%s) and its %d roles — reads as a marketplace board",
			c.Name, c.ATSType, c.ATSSlug, res.RowsAffected)
		companies++
		jobs += int(res.RowsAffected)
	}
	return companies, jobs
}

// sweepJunkTitles deletes roles whose title is a button rather than a job.
//
// The link scan takes an anchor's text as the role name, so a careers page
// that labels every listing "View details" stored that as thirty-five jobs
// across thirteen companies — one of them the literal string
// "Apply--> <!-- Now", a piece of the page's own markup.
func sweepJunkTitles() int {
	var jobs []models.Job
	if err := config.DB.Find(&jobs).Error; err != nil {
		log.Printf("guard backfill: could not load roles: %v", err)
		return 0
	}

	var doomed []string
	for _, j := range jobs {
		if !looksLikeRoleTitle(j.Title) {
			doomed = append(doomed, j.ID)
		}
	}
	if len(doomed) == 0 {
		return 0
	}

	res := config.DB.Where("id IN ?", doomed).Delete(&models.Job{})
	if res.Error != nil {
		log.Printf("guard backfill: junk-title sweep failed: %v", res.Error)
		return 0
	}
	log.Printf("guard backfill: removed %d roles whose title was not a job", res.RowsAffected)
	return int(res.RowsAffected)
}
