package services

import (
	"time"

	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/config"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/models"
)

// The derived columns, and the repairs that keep them honest.
//
// Two things are stored here that could be computed: companies.open_roles and
// jobs.field/level. Both were computed per request, and both were the same
// answer recomputed forever — an aggregation over the whole jobs table on
// every page load, and a classification of every job title in Go on every page
// load. That is affordable at three hundred companies and not at the scale the
// harvest exists to reach.
//
// A derived column is a promise that it matches what it derives from, and the
// only way to keep such a promise is to re-derive it on a schedule. Both
// functions below do exactly that, cheaply and in bulk, so a drift from a
// crash mid-write — or from a change to the classification rules — corrects
// itself without anyone noticing it happened.

// RecountOpenRoles rewrites companies.open_roles from the jobs table and
// reports how many rows were wrong.
//
// One statement, no row loading: the correlated subquery is the same work
// Postgres would have done for the old GROUP BY, done once every six hours
// instead of twice per page load.
func RecountOpenRoles() (int64, error) {
	res := config.DB.Exec(`
		UPDATE companies c
		SET open_roles = sub.n
		FROM (
			SELECT co.id, COUNT(j.id) AS n
			FROM companies co
			LEFT JOIN jobs j ON j.company_id = co.id
			GROUP BY co.id
		) AS sub
		WHERE c.id = sub.id AND c.open_roles IS DISTINCT FROM sub.n
	`)
	return res.RowsAffected, res.Error
}

// setOpenRoles keeps the counter in step with a write that just happened.
//
// Called from replaceJobsForCompany, which is the single choke point every
// producer of roles passes through — the ATS sync, the careers-page tiers and
// the harvest all end there — so there is one place to forget rather than
// four.
func setOpenRoles(companyID string, n int) {
	config.DB.Model(&models.Company{}).
		Where("id = ?", companyID).
		Updates(map[string]interface{}{
			"open_roles":     n,
			"last_synced_at": time.Now(),
		})
}

// BackfillJobFacets fills jobs.field and jobs.level for rows written before
// the columns existed, or classified under older rules.
//
// Done in bounded batches: the jobs table is the largest in the schema, and a
// single UPDATE over all of it holds locks for as long as it takes. Returning
// the count lets the caller loop until there is nothing left.
func BackfillJobFacets(batch int) (int, error) {
	if batch <= 0 {
		batch = 2000
	}
	var rows []models.Job
	if err := config.DB.
		Select("id", "title", "department").
		Where("field IS NULL OR field = '' OR level IS NULL OR level = ''").
		Limit(batch).
		Find(&rows).Error; err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}

	// Grouped by the pair rather than updated row by row: a few hundred
	// distinct (field, level) combinations exist across any number of jobs,
	// so this is a handful of statements instead of thousands.
	type bucket struct{ field, level string }
	byBucket := map[bucket][]string{}
	for _, j := range rows {
		b := bucket{ClassifyField(j.Title, j.Department), ClassifyLevel(j.Title)}
		byBucket[b] = append(byBucket[b], j.ID)
	}
	for b, ids := range byBucket {
		if err := config.DB.Model(&models.Job{}).
			Where("id IN ?", ids).
			Updates(map[string]interface{}{"field": b.field, "level": b.level}).Error; err != nil {
			return 0, err
		}
	}
	return len(rows), nil
}

// classifyJobs stamps the facet columns on rows about to be written, so the
// stored value and the classifier can never disagree for a new row.
func classifyJobs(rows []models.Job) {
	for i := range rows {
		rows[i].Field = ClassifyField(rows[i].Title, rows[i].Department)
		rows[i].Level = ClassifyLevel(rows[i].Title)
	}
}
