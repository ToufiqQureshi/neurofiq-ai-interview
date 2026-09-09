package services

import (
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/config"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/models"
	"gorm.io/gorm/clause"
)

// Turning a board's answer into this directory's rows.
//
// The guards that matter live here: an empty read is not the same as not
// hiring, a talent-pool posting is not a vacancy, and a role outside India
// is not this directory's business.
// SyncJobsForCompany detects (if not already known) and pulls the company's
// current open roles from its ATS's public API, upserting them into the
// jobs table (deduped by company+url) and removing listings that have since
// closed.
func SyncJobsForCompany(company models.Company) (int, error) {
	atsType, atsSlug := company.ATSType, company.ATSSlug
	if atsType == "" {
		// Detection reads the careers page, so don't repeat it on every tick
		// for a company that just came back with nothing.
		//
		// The wait is short, because the long one hid every improvement we
		// made. A failed detection used to freeze a company for a full week,
		// which meant a fix shipped on Monday changed nothing visible until
		// the following Monday — and any company that failed during a bad
		// week stayed at zero roles long after the cause was gone. A
		// successful detection is what earns the week; a failure is worth
		// another look the same day.
		if company.ATSCheckedAt != nil && time.Since(*company.ATSCheckedAt) < atsRecheckIntervalFor(company) {
			return 0, nil
		}

		// The careers URL is often missing, or points at the homepage.
		// Recover it from the company's own domain before giving up —
		// otherwise these companies sit at zero jobs forever.
		if resolved := ResolveCareersURL(company); resolved != company.CareersURL {
			log.Printf("careers URL resolved for %s: %s", company.Name, resolved)
			company.CareersURL = resolved
			config.DB.Model(&models.Company{}).Where("id = ?", company.ID).
				Update("careers_url", resolved)
		}

		atsType, atsSlug = DetectATS(company)

		if atsType != "" {
			config.DB.Model(&models.Company{}).Where("id = ?", company.ID).Updates(
				map[string]interface{}{
					"ats_type": atsType, "ats_slug": atsSlug, "ats_checked_at": time.Now(),
				})
		} else {
			// Stamping ats_checked_at here is what starts the week-long
			// cooldown, so it is done only once the careers-page attempt
			// below has actually had its turn. It used to be written
			// unconditionally, before that attempt ran: a single Firecrawl
			// rate-limit error then cost the company seven days at zero
			// roles, and the next attempt a week later could lose the same
			// coin toss. Now a transient failure is retried on the next
			// tick, and only a clean "nothing here" starts the clock.
			n, err := syncJobsFromCareersPage(company)
			if err != nil {
				return 0, err
			}
			config.DB.Model(&models.Company{}).Where("id = ?", company.ID).
				Update("ats_checked_at", time.Now())
			return n, nil
		}
	}

	// Most companies run a custom careers portal rather than a supported
	// ATS. Without this fallback they'd show zero jobs while actively
	// hiring, which is the common case, not the edge case.
	if atsType == "" {
		return syncJobsFromCareersPage(company)
	}

	rows, err := FetchATSJobs(company.ID, atsType, atsSlug)
	if err != nil {
		return 0, err
	}
	return applySyncedJobs(company, rows)
}

// atsRecheckIntervalFor is how long to leave a company alone before looking
// for its board again: a week once we've found one, twelve hours while we
// still haven't.
func atsRecheckIntervalFor(company models.Company) time.Duration {
	if company.ATSType != "" {
		return atsRecheckInterval
	}
	return atsRetryInterval
}

// FetchATSJobs returns the open roles a provider's public API reports for one
// board slug, as Job rows belonging to companyID.
//
// Split out from SyncJobsForCompany so board discovery can look at a board's
// roles before deciding whether the company belongs in the directory at all,
// without a second copy of this switch drifting out of sync with it.
func FetchATSJobs(companyID, atsType, atsSlug string) ([]models.Job, error) {
	var rows []models.Job
	switch atsType {
	case "greenhouse":
		jobs, err := fetchGreenhouseJobs(atsSlug)
		if err != nil {
			return nil, err
		}
		for _, j := range jobs {
			dept := ""
			if len(j.Departments) > 0 {
				dept = j.Departments[0].Name
			}
			rows = append(rows, models.Job{
				CompanyID: companyID, Title: j.Title, Department: dept,
				Location: j.Location.Name, URL: j.AbsoluteURL, Source: "greenhouse",
			})
		}
	case "lever":
		jobs, err := fetchLeverJobs(atsSlug)
		if err != nil {
			return nil, err
		}
		for _, j := range jobs {
			rows = append(rows, models.Job{
				CompanyID: companyID, Title: j.Text, Department: j.Categories.Team,
				Location: j.Categories.Location, URL: j.HostedURL, Source: "lever",
			})
		}
	case "ashby":
		jobs, err := fetchAshbyJobs(atsSlug)
		if err != nil {
			return nil, err
		}
		for _, j := range jobs {
			rows = append(rows, models.Job{
				CompanyID: companyID, Title: j.Title, Department: j.Department,
				Location: j.Location, URL: j.JobURL, Source: "ashby",
			})
		}
	case "smartrecruiters":
		jobs, err := fetchSmartRecruitersJobs(atsSlug)
		if err != nil {
			return nil, err
		}
		for _, j := range jobs {
			// department is often empty; function is the usable fallback
			dept := j.Department.Label
			if dept == "" {
				dept = j.Function.Label
			}
			loc := j.Location.FullLocation
			if loc == "" {
				loc = j.Location.City
			}
			rows = append(rows, models.Job{
				CompanyID: companyID, Title: j.Name, Department: dept, Location: loc,
				// The API response has no public posting URL, so build the
				// canonical careers-site one from the slug + posting id.
				URL:    fmt.Sprintf("https://careers.smartrecruiters.com/%s/%s", atsSlug, j.ID),
				Source: "smartrecruiters",
			})
		}
	case "workable":
		jobs, err := fetchWorkableJobs(atsSlug)
		if err != nil {
			return nil, err
		}
		for _, j := range jobs {
			loc := strings.TrimSpace(strings.Trim(j.Location.City+", "+j.Location.Country, ", "))
			url := j.URL
			if url == "" && j.Shortcode != "" {
				url = fmt.Sprintf("https://apply.workable.com/%s/j/%s/", atsSlug, j.Shortcode)
			}
			rows = append(rows, models.Job{
				CompanyID: companyID, Title: j.Title, Department: j.Department,
				Location: loc, URL: url, Source: "workable",
			})
		}
	case "darwinbox":
		jobs, err := fetchDarwinboxJobs(atsSlug)
		if err != nil {
			return nil, err
		}
		for _, j := range jobs {
			loc := j.Locations
			if len(j.OfficeLocs) > 0 && j.OfficeLocs[0] != "" {
				loc = j.OfficeLocs[0]
			}
			rows = append(rows, models.Job{
				CompanyID: companyID, Title: j.Title, Department: j.DepartmentName,
				Location: strings.TrimSpace(loc),
				URL: fmt.Sprintf("https://%s.darwinbox.in/ms/candidatev2/main/careers/jobDetails/%s",
					atsSlug, j.ID),
				Source: "darwinbox",
			})
		}
	case "keka":
		jobs, err := fetchKekaJobs(atsSlug)
		if err != nil {
			return nil, err
		}
		for _, j := range jobs {
			var locs []string
			for _, l := range j.JobLocations {
				name := l.Name
				if name == "" {
					name = l.City
				}
				if name != "" {
					locs = append(locs, name)
				}
			}
			rows = append(rows, models.Job{
				CompanyID: companyID, Title: j.Title, Department: j.DepartmentName,
				Location: strings.Join(locs, ", "),
				URL:      fmt.Sprintf("https://%s.keka.com/careers/jobdetails/%d", atsSlug, j.ID),
				Source:   "keka",
			})
		}
	case "workday":
		jobs, err := fetchWorkdayJobs(atsSlug)
		if err != nil {
			return nil, err
		}
		parts := strings.Split(atsSlug, ":")
		if len(parts) != 3 {
			return nil, fmt.Errorf("bad workday slug %q", atsSlug)
		}
		tenant, region, site := parts[0], parts[1], parts[2]
		for _, j := range jobs {
			rows = append(rows, models.Job{
				CompanyID: companyID, Title: j.Title, Location: j.LocationsText,
				URL: fmt.Sprintf("https://%s.%s.myworkdayjobs.com/en-US/%s%s",
					tenant, region, site, j.ExternalPath),
				Source: "workday",
			})
		}
	case "recruitee":
		offers, err := fetchRecruiteeJobs(atsSlug)
		if err != nil {
			return nil, err
		}
		for _, o := range offers {
			loc := strings.TrimSpace(strings.Trim(o.City+", "+o.Country, ", "))
			rows = append(rows, models.Job{
				CompanyID: companyID, Title: o.Title, Department: o.Department,
				Location: loc, URL: o.CareersURL, Source: "recruitee",
			})
		}
	case "personio":
		positions, err := fetchPersonioJobs(atsSlug)
		if err != nil {
			return nil, err
		}
		for _, p := range positions {
			rows = append(rows, models.Job{
				CompanyID: companyID, Title: p.Name, Department: p.Department,
				Location: p.Office,
				URL:      fmt.Sprintf("https://%s/job/%s", atsSlug, p.ID),
				Source:   "personio",
			})
		}
	case "freshteam":
		// Already models.Job — fetchFreshteamJobs reads a rendered page
		// rather than a JSON/XML API, so it builds full rows itself instead
		// of a provider-shaped intermediate type only this switch would use.
		jobs, err := fetchFreshteamJobs(atsSlug)
		if err != nil {
			return nil, err
		}
		for _, j := range jobs {
			j.CompanyID = companyID
			rows = append(rows, j)
		}
	case "gem":
		postings, err := fetchGemJobs(atsSlug)
		if err != nil {
			return nil, err
		}
		for _, p := range postings {
			loc := ""
			if len(p.Locations) > 0 {
				loc = p.Locations[0].Name
			}
			rows = append(rows, models.Job{
				CompanyID: companyID, Title: p.Title, Department: p.Job.Department.Name,
				Location: loc,
				URL:      fmt.Sprintf("https://jobs.gem.com/%s/%s", atsSlug, p.ExtID),
				Source:   "gem",
			})
		}
	default:
		// Without this the switch falls through to `return rows, nil` with
		// rows still nil, and the caller cannot tell "this board has no open
		// roles" from "nobody here knows how to read this board" — so it
		// deletes every stored role for the company. An unreadable provider
		// is an error, not an empty board.
		return nil, fmt.Errorf("unknown ATS provider %q", atsType)
	}

	return rows, nil
}

// applySyncedJobs writes one sync's result to the jobs table, with a single
// guard: an empty result does not clear a company's listings the first time
// it happens.
//
// Every read feeding this can fail in a way that looks exactly like success.
// An ATS returns 200 with an empty array while it is being reconfigured; the
// careers-page link scan depends on markup and reads a redesign as zero. The
// old behaviour deleted every role on the strength of one such read, so a
// company that is plainly hiring showed as having nothing — and reappeared an
// hour later, which is worse than either state on its own.
//
// A second consecutive empty read is the company actually saying it has
// nothing open, and that is when the listings go.
func applySyncedJobs(company models.Company, rows []models.Job) (int, error) {
	// Before the empty-read check, not after: a board carrying 300 roles and
	// no Indian one is empty as far as this directory is concerned, and has
	// to reach the protection below rather than sail past it and then be
	// cleared downstream on the first read.
	rows = keepIndianRoles(rows)

	if len(rows) == 0 && company.EmptyJobReads == 0 {
		var existing int64
		config.DB.Model(&models.Job{}).Where("company_id = ?", company.ID).Count(&existing)
		if existing > 0 {
			config.DB.Model(&models.Company{}).Where("id = ?", company.ID).
				Update("empty_job_reads", 1)
			log.Printf("job sync: %s read empty but has %d stored roles — keeping them until a second empty read",
				company.Name, existing)
			return int(existing), nil
		}
	}

	n, err := replaceJobsForCompany(company.ID, rows)
	if err != nil {
		return 0, err
	}
	// Either a read succeeded, or the second empty read has just cleared the
	// company. Both leave the counter at zero.
	if company.EmptyJobReads != 0 {
		config.DB.Model(&models.Company{}).Where("id = ?", company.ID).
			Update("empty_job_reads", 0)
	}

	// area is stamped once at discovery and never revisited, so a row that
	// got it wrong stayed wrong: Speechify sat on the India map labelled
	// "Indianapolis, IN, USA". The roles just stored are all Indian by the
	// filter above, so the first of them is a better answer than whatever is
	// on the row — but only when what is on the row is not Indian already,
	// so a correct area is never churned by whichever posting sorted first.
	if len(rows) > 0 && !looksIndian(company.Area) {
		if area := firstIndianLocation(rows); area != "" {
			config.DB.Model(&models.Company{}).Where("id = ?", company.ID).
				Update("area", area)
			log.Printf("job sync: corrected %s area %q -> %q", company.Name, company.Area, area)
		}
	}
	return n, nil
}

// careersPageSource marks a role read off a company's own careers page rather
// than from a job board. job_service writes it on every row those tiers
// produce.
const careersPageSource = "careers-page"

// htmlFragmentRe catches a title that is really a piece of the page's markup.
// The link scan reads anchor text, and a page whose markup it misparsed once
// stored "Apply--> <!-- Now" as a job.
var htmlFragmentRe = regexp.MustCompile(`<!--|-->|</?[a-z][a-z0-9]*\s*/?>`)

// ctaTitles are the words on a button, not the name of a job.
//
// The careers-page link scan takes an anchor's text as the role, and a page
// that puts "View details" on every listing gives every listing that name.
// Matching is exact, deliberately: "Back Office Executive" begins with "back"
// and is a real job, so anything looser deletes real openings to remove
// cosmetic ones — the wrong trade for a directory whose product is the roles.
var ctaTitles = map[string]bool{
	"apply": true, "apply now": true, "view": true, "view details": true,
	"view jd": true, "view job": true, "view more": true, "details": true,
	"explore more": true, "explore": true, "read more": true, "learn more": true,
	"know more": true, "see more": true, "click here": true, "here": true,
	"more": true, "submit": true, "next": true, "back": true, "previous": true,
	"job description": true, "description": true, "open positions": true,
}

// looksLikeRoleTitle rejects the strings a page scan mistakes for a job title.
func looksLikeRoleTitle(title string) bool {
	t := strings.TrimSpace(title)
	if len(t) < 4 {
		// "TL" and "SSO" arrived this way. A real posting names the work.
		return false
	}
	if htmlFragmentRe.MatchString(t) {
		return false
	}
	return !ctaTitles[strings.ToLower(t)]
}

// keepIndianRoles drops the roles a board carries that are not in India.
//
// A board belongs to a company, not to a country: accepting a company because
// it hires here brought in every other posting it had anywhere. The directory
// counted 6,621 "open roles in India" of which 1,626 actually were — WPP Media
// contributed 1,074 rows with 90 Indian, OpenAI 769 with 10. A visitor
// clicking "View 769 roles" got 759 they cannot apply to: the Jogether failure
// in a new shape, a number counted correctly that means something false.
//
// The rule applies to board rows only, and the source is what decides. A
// company's own careers page is not a global feed — the company is already in
// this directory because it hires here — and those pages describe location
// loosely or not at all. Testbook writes "Not specified", Schoolnet India "As
// per requirement", RocketFrog "Hybrid", and 159 rows carry no location field
// at all. Judging those by the board rule deletes real openings at real Indian
// companies, which is a worse error than the one being fixed: the directory
// exists to show these roles.
//
// The cost is small and known. Two rows out of 1,918 name a foreign city on a
// careers page (UnPay's New York and London) and survive. Recognising those
// would need a list of every foreign place, which is the kind of list that is
// never finished — and being wrong about two rows is cheaper than inventing it.
func keepIndianRoles(rows []models.Job) []models.Job {
	kept := rows[:0]
	for _, r := range rows {
		if r.Source == careersPageSource || looksIndian(r.Location) {
			kept = append(kept, r)
		}
	}
	return kept
}

// replaceJobsForCompany makes the jobs table match `rows` exactly for one
// company: closed postings are deleted, existing ones updated, new ones
// inserted. Shared by the ATS-API and careers-page paths so both stay
// idempotent — re-running a sync must never duplicate rows.
func replaceJobsForCompany(companyID string, rows []models.Job) (int, error) {
	// The choke point every producer passes through — board discovery, the
	// ATS sync and the careers-page tiers all end here — so the India rule is
	// applied once, where it cannot be forgotten by a new caller.
	rows = keepIndianRoles(rows)

	// Drop anything missing the two fields we require, or whose title is not
	// the name of a job at all.
	valid := rows[:0]
	seen := map[string]bool{}
	for _, r := range rows {
		if r.Title == "" || r.URL == "" || seen[r.URL] {
			continue // also guards against a provider repeating a URL
		}
		if !looksLikeRoleTitle(r.Title) {
			continue
		}
		seen[r.URL] = true
		valid = append(valid, r)
	}
	rows = valid

	if len(rows) == 0 {
		// No open roles right now (or the board came back empty) — clear
		// any stale listings we previously had for this company.
		config.DB.Where("company_id = ?", companyID).Delete(&models.Job{})
		setOpenRoles(companyID, 0)
		return 0, nil
	}

	// The facet buckets are stamped here, from the same slice being written,
	// so a stored row and the classifier can never disagree. Doing it at the
	// choke point rather than in each producer is what stops a new caller
	// from writing rows the filters cannot see.
	classifyJobs(rows)

	currentURLs := make([]string, len(rows))
	for i, r := range rows {
		currentURLs[i] = r.URL
	}
	// Drop listings that closed since the last sync.
	config.DB.Where("company_id = ? AND url NOT IN ?", companyID, currentURLs).Delete(&models.Job{})

	if err := config.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "company_id"}, {Name: "url"}},
		DoUpdates: clause.AssignmentColumns([]string{"title", "department", "location", "field", "level"}),
	}).Create(&rows).Error; err != nil {
		return 0, err
	}

	setOpenRoles(companyID, len(rows))
	return len(rows), nil
}
