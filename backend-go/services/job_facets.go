package services

import (
	"strings"

	"gorm.io/gorm"

	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/config"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/models"
)

// Job facets are derived from the title and department we already store —
// no extra data, no LLM call. They let someone filter 200+ roles down to
// "engineering, fresher" the way a job board should.

// FieldBuckets in display order. First match wins, so more specific buckets
// must come before broader ones (a "Data Engineer" is Data & AI, not
// Engineering).
var FieldBuckets = []string{"Data & AI", "Engineering", "Product", "Design", "Sales & Marketing", "Operations", "Other"}

// LevelBuckets in display order.
var LevelBuckets = []string{"Fresher", "Junior", "Mid", "Senior", "Lead", "Unspecified"}

var fieldKeywords = []struct {
	bucket   string
	keywords []string
}{
	{"Data & AI", []string{"data scien", "data analy", "machine learning", " ml ", "ai engineer", "ai/ml", "nlp", "computer vision", "data engineer", "analytics", "research scien", "applied scien"}},
	{"Engineering", []string{"engineer", "developer", "sde", "software", "backend", "frontend", "full stack", "fullstack", "devops", "sre", "qa", "test", "architect", "android", "ios", "mobile", "security", "infrastructure", "platform", "technical"}},
	{"Product", []string{"product manager", "product owner", "product manag", "program manag", "project manag", "scrum"}},
	{"Design", []string{"design", "ux", "ui ", "creative", "graphic", "content writer", "copywriter"}},
	{"Sales & Marketing", []string{"sales", "marketing", "business development", "bd ", "account executive", "account manager", "growth", "partnership", "customer success", "revenue", "brand", "seo", "demand gen", "inside sales", "territory"}},
	{"Operations", []string{"operations", "ops", "supply chain", "logistics", "warehouse", "finance", "account", "hr", "human resource", "recruit", "talent", "legal", "compliance", "admin", "support", "procurement"}},
}

var levelKeywords = []struct {
	bucket   string
	keywords []string
}{
	// 1. Lead / Executive: Organizational & functional leadership
	{"Lead", []string{"lead", "head of", "head", "principal", "director", "vp ", "vice president", "chief", "cto", "cfo", "ceo", "founding", "group product manager", "managing partner", "general partner"}},
	// 2. Senior: Advanced IC roles & enterprise ownership
	{"Senior", []string{"senior", "sr.", "sr ", "staff", "architect", "enterprise", "sde 3", "sde iii", " iii"}},
	// 3. Fresher: Explicit early-career & internship programs
	{"Fresher", []string{"intern", "trainee", "fresher", "graduate", "entry level", "entry-level", "apprentice", "campus"}},
	// 4. Junior: Entry-level corporate roles & early ICs (1-2 yrs)
	{"Junior", []string{"junior", "jr.", "jr ", "associate", "assistant", "sde 1", "sde i ", "analyst", "coordinator", "representative", " bdr ", " sdr "}},
	// 5. Mid: Core professional IC roles (Levels.fyi / Radford IC2 standard, 2-4 yrs)
	// When a company posts "Software Engineer", "Backend Developer", "Product Manager",
	// or "UI/UX Designer" without a prefix, by industry taxonomy it is a Mid-level role.
	{"Mid", []string{"sde 2", "sde ii", "mid-level", " ii", "engineer", "developer", "designer", "product manager", "specialist", "generalist", "consultant"}},
}


// ClassifyField buckets a job by what kind of work it is.
func ClassifyField(title, department string) string {
	hay := " " + strings.ToLower(title+" "+department) + " "
	for _, f := range fieldKeywords {
		for _, kw := range f.keywords {
			if strings.Contains(hay, kw) {
				return f.bucket
			}
		}
	}
	return "Other"
}

// ClassifyLevel buckets a job by seniority. Titles often say nothing about
// level, so "Unspecified" is a legitimate and common answer — better than
// guessing "Mid" and being wrong.
func ClassifyLevel(title string) string {
	hay := " " + strings.ToLower(title) + " "
	for _, l := range levelKeywords {
		for _, kw := range l.keywords {
			if strings.Contains(hay, kw) {
				return l.bucket
			}
		}
	}
	return "Unspecified"
}

// FacetCount is one bucket and how many roles fall in it.
type FacetCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// JobFacets returns field and level counts across all roles matching the
// given company filters, for the filter chips in the UI.
func JobFacets(sector, stage, area, q string) (fields, levels []FacetCount, err error) {
	// Built fresh for each of the two counts rather than shared between them.
	//
	// A *gorm.DB accumulates clauses, and reusing one across two finished
	// queries relies on session semantics to decide whether the second sees
	// the first's SELECT and GROUP BY. That is a subtle thing to be right
	// about and an ugly thing to be wrong about — the level counts would come
	// back grouped by field — so the query is simply constructed twice. It is
	// a few pointer allocations against a database round trip.
	filtered := func() *gorm.DB {
		db := config.DB.Model(&models.Job{}).
			Joins("JOIN companies ON companies.id = jobs.company_id")
		db = applyFacetFilter(db, "sector", sector, "companies")
		db = applyFacetFilter(db, "stage", stage, "companies")
		db = applyAreaFilter(db, area, "companies")
		if q != "" {
			db = db.Where("companies.name ILIKE ? OR companies.description ILIKE ?", "%"+q+"%", "%"+q+"%")
		}
		return db
	}

	// Counted in the database from the stored buckets rather than by pulling
	// every matching title into Go and classifying it again on each request.
	//
	// The classification is pure — the same title always gives the same bucket
	// — so recomputing it per page load was the same work repeated forever,
	// over a full scan of the filtered jobs table. jobs.field and jobs.level
	// hold the answer; two GROUP BYs replace the scan. classifyJobs writes
	// those columns at the choke point every producer passes through, and
	// BackfillJobFacets rewrites them when the rules change, which is what
	// stops a stored derivation from drifting away from its classifier.
	//
	// COALESCE/NULLIF cover rows written before the columns existed: they read
	// as the same default bucket the classifier would have given them, so the
	// counts stay complete while the backfill works through the table.
	countByBucket := func(column, fallback string) (map[string]int, error) {
		var rows []struct {
			Bucket string
			N      int
		}
		err := filtered().
			Select("COALESCE(NULLIF(jobs."+column+", ''), ?) AS bucket, COUNT(*) AS n", fallback).
			Group("bucket").
			Scan(&rows).Error
		if err != nil {
			return nil, err
		}
		out := make(map[string]int, len(rows))
		for _, r := range rows {
			out[r.Bucket] += r.N
		}
		return out, nil
	}

	fieldCounts, err := countByBucket("field", "Other")
	if err != nil {
		return nil, nil, err
	}
	levelCounts, err := countByBucket("level", "Unspecified")
	if err != nil {
		return nil, nil, err
	}

	// Emit in display order, skipping empty buckets so the UI stays clean.
	for _, b := range FieldBuckets {
		if n := fieldCounts[b]; n > 0 {
			fields = append(fields, FacetCount{Name: b, Count: n})
		}
	}
	for _, b := range LevelBuckets {
		if n := levelCounts[b]; n > 0 {
			levels = append(levels, FacetCount{Name: b, Count: n})
		}
	}
	return fields, levels, nil
}

// FilterJobsByFacet narrows a job list to one field and/or level bucket.
func FilterJobsByFacet(jobs []models.Job, field, level string) []models.Job {
	if field == "" && level == "" {
		return jobs
	}
	out := jobs[:0]
	for _, j := range jobs {
		if field != "" && ClassifyField(j.Title, j.Department) != field {
			continue
		}
		if level != "" && ClassifyLevel(j.Title) != level {
			continue
		}
		out = append(out, j)
	}
	return out
}
