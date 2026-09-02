package services

import (
	"strings"

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
	{"Lead", []string{"lead", "head of", "principal", "director", "vp ", "vice president", "chief", "cto", "cfo", "ceo", "founding"}},
	// "manager" used to sit in Senior, and it put every Account Manager and
	// Product Manager there: 983 roles carried the word with no seniority
	// marker at all, which is most of the gap between 2,608 Senior and 19
	// Mid. Managing people is a kind of job, not a rung — there are junior
	// managers and senior ones — so it decides the field, not the level.
	{"Senior", []string{"senior", "sr.", "sr ", "staff", "sde 3", "sde iii", " iii"}},
	{"Fresher", []string{"intern", "trainee", "fresher", "graduate", "entry level", "entry-level", "apprentice"}},
	{"Junior", []string{"junior", "jr.", "jr ", "associate", "sde 1", "sde i ", "analyst i"}},
	// Roman numerals are matched with their leading space, so "ii" finds
	// "Engineer II" without also firing on any title that merely contains the
	// letters. Senior is tested first, so "Engineer III" never lands here.
	{"Mid", []string{"sde 2", "sde ii", "mid-level", " ii"}},
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
	dbQuery := config.DB.Model(&models.Job{}).
		Select("jobs.title, jobs.department").
		Joins("JOIN companies ON companies.id = jobs.company_id")
	dbQuery = applyFacetFilter(dbQuery, "sector", sector, "companies")
	dbQuery = applyFacetFilter(dbQuery, "stage", stage, "companies")
	dbQuery = applyAreaFilter(dbQuery, area, "companies")
	if q != "" {
		dbQuery = dbQuery.Where("companies.name ILIKE ? OR companies.description ILIKE ?", "%"+q+"%", "%"+q+"%")
	}

	var rows []struct {
		Title      string
		Department string
	}
	if err := dbQuery.Scan(&rows).Error; err != nil {
		return nil, nil, err
	}

	fieldCounts := map[string]int{}
	levelCounts := map[string]int{}
	for _, r := range rows {
		fieldCounts[ClassifyField(r.Title, r.Department)]++
		levelCounts[ClassifyLevel(r.Title)]++
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
