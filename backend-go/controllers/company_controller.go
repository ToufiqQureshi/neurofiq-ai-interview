package controllers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/config"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/models"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/services"
	"github.com/gin-gonic/gin"
)

type cachedDirectoryResponse struct {
	data      gin.H
	expiresAt time.Time
}

var (
	dirCacheMu sync.RWMutex
	dirCache   = make(map[string]cachedDirectoryResponse)

	statsCacheMu   sync.RWMutex
	statsCacheData *services.DirectoryStats
	statsCacheAt   time.Time
)

const (
	dirCacheTTL   = 60 * time.Second
	statsCacheTTL = 30 * time.Second
)

// InvalidateDirectoryCache purges the cached company results when new data is written.
func InvalidateDirectoryCache() {
	dirCacheMu.Lock()
	dirCache = make(map[string]cachedDirectoryResponse)
	dirCacheMu.Unlock()

	statsCacheMu.Lock()
	statsCacheData = nil
	statsCacheMu.Unlock()
}

func HandleGetCompanies(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "24"))

	sector, stage, area, q := c.Query("sector"), c.Query("stage"), c.Query("area"), c.Query("q")
	// The role-bucket chips. They filter the company list itself, not just the
	// roles inside an expanded card: the chip shows a count, and a count the
	// list does not respond to reads as a broken filter.
	field, level := c.Query("field"), c.Query("level")
	hiringOnly := c.Query("hiring") == "1"

	cacheKey := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%t|%d|%d",
		sector, stage, area, q, field, level, hiringOnly, page, pageSize)

	// Check RAM cache first to avoid hitting database on frequent refreshes
	dirCacheMu.RLock()
	cached, ok := dirCache[cacheKey]
	fresh := ok && time.Now().Before(cached.expiresAt)
	dirCacheMu.RUnlock()

	if fresh {
		c.Header("Cache-Control", "public, max-age=60, stale-while-revalidate=120")
		c.Header("X-Cache", "HIT")
		c.JSON(http.StatusOK, cached.data)
		return
	}

	companies, total, err := services.ListCompanies(sector, stage, area, q, field, level, hiringOnly, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch companies"})
		return
	}

	var openRoles int64
	// The fast path sums companies.open_roles, which counts every role a
	// company has — right up until a role bucket is selected, when the answer
	// has to come from the roles themselves.
	if q == "" && field == "" && level == "" {
		openRoles, _ = services.TotalOpenRolesFast(sector, stage, area)
	} else {
		openRoles, _ = services.TotalOpenRoles(sector, stage, area, q, field, level)
	}
	fields, levels, _ := services.JobFacets(sector, stage, area, q)
	sectors, stages, _ := services.CompanyFacets()

	resp := gin.H{
		"companies":  companies,
		"total":      total,
		"open_roles": openRoles,
		"facets": gin.H{
			"field":  fields,
			"level":  levels,
			"sector": sectors,
			"stage":  stages,
		},
	}

	// Store in RAM cache
	dirCacheMu.Lock()
	dirCache[cacheKey] = cachedDirectoryResponse{
		data:      resp,
		expiresAt: time.Now().Add(dirCacheTTL),
	}
	dirCacheMu.Unlock()

	c.Header("Cache-Control", "public, max-age=60, stale-while-revalidate=120")
	c.Header("X-Cache", "MISS")
	c.JSON(http.StatusOK, resp)
}

// HandleGetPipelineHealth reports whether roles are still arriving.
func HandleGetPipelineHealth(c *gin.Context) {
	health := services.CheckPipelineHealth()
	status := http.StatusOK
	if !health.Healthy {
		status = http.StatusServiceUnavailable
	}
	c.JSON(status, health)
}

// HandleGetDirectoryStats backs the count strip above the Job Map grid.
func HandleGetDirectoryStats(c *gin.Context) {
	statsCacheMu.RLock()
	cached := statsCacheData
	fresh := cached != nil && statsCacheAt.Add(statsCacheTTL).After(time.Now())
	statsCacheMu.RUnlock()

	if fresh {
		c.Header("Cache-Control", "public, max-age=30")
		c.Header("X-Cache", "HIT")
		c.JSON(http.StatusOK, cached)
		return
	}

	stats, err := services.GetDirectoryStats()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch directory stats"})
		return
	}

	statsCacheMu.Lock()
	statsCacheData = &stats
	statsCacheAt = time.Now()
	statsCacheMu.Unlock()

	c.Header("Cache-Control", "public, max-age=30")
	c.Header("X-Cache", "MISS")
	c.JSON(http.StatusOK, stats)
}

// HandleReclassifyJobs triggers a full reclassification pass across all existing jobs.
func HandleReclassifyJobs(c *gin.Context) {
	n, err := services.ReclassifyAllJobs(2000)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	InvalidateDirectoryCache()
	c.JSON(http.StatusOK, gin.H{"status": "reclassified", "count": n})
}

func HandleGetCompanyByID(c *gin.Context) {
	var company models.Company
	if err := config.DB.First(&company, "id = ?", c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "company not found"})
		return
	}
	c.JSON(http.StatusOK, company)
}

func HandleGetCompanyJobs(c *gin.Context) {
	jobs, err := services.ListJobsForCompany(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch jobs"})
		return
	}
	jobs = services.FilterJobsByFacet(jobs, c.Query("field"), c.Query("level"))
	c.JSON(http.StatusOK, gin.H{"jobs": jobs, "total": len(jobs)})
}

// HandleGetGlobalJobs provides paginated, filtered discovery across all company job postings.
func HandleGetGlobalJobs(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "30"))
	q := c.Query("q")
	location := c.Query("location")
	if location == "" {
		location = c.Query("hub")
	}
	field := c.Query("field")
	level := c.Query("level")
	workType := c.Query("work_type")
	scope := c.Query("scope")

	jobs, total, err := services.ListGlobalJobs(q, location, field, level, workType, scope, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch jobs"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"jobs":      jobs,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// HandleEnrichCompanies triggers batch metadata and description enrichment across all companies.
func HandleEnrichCompanies(c *gin.Context) {
	updated, err := services.EnrichAllPendingCompanies(12)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	InvalidateDirectoryCache()
	c.JSON(http.StatusOK, gin.H{
		"status":  "enriched",
		"updated": updated,
	})
}

// maxImportBoards bounds one import to what fits inside the server's
// WriteTimeout, which main.go sets to 120s.
//
// Admission reads each candidate's board — someone else's API, measured at
// roughly 2.7s per board once the guards and the free website resolution are
// counted. A batch of 110 took about five minutes: the work completed and the
// companies were stored, but the connection had been closed for minutes and
// the caller saw only "remote end closed connection", with no way to tell a
// successful import from a failed one. Twenty-five leaves comfortable headroom
// at that rate; a caller with more sends more batches.
const maxImportBoards = 25

// HandleImportBoards admits boards found outside the rotation.
//
// The search rotation spends about two searches per company stored, so its
// month has a ceiling around 400 companies no matter how often the cron runs.
// A script that sweeps one provider does not, and this is how what it finds
// gets in. It runs the same admission guards — no metered lookup is allowed,
// so the import is free and a company that cannot be named without one is
// reported as unnamed and left for the rotation.
func HandleImportBoards(c *gin.Context) {
	var req struct {
		Source string              `json:"source"`
		Boards []services.BoardRef `json:"boards"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "expected {\"boards\":[{\"provider\":\"lever\",\"slug\":\"acme\"}]}"})
		return
	}
	if len(req.Boards) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no boards supplied"})
		return
	}
	if len(req.Boards) > maxImportBoards {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("too many boards: %d (max %d per request)", len(req.Boards), maxImportBoards),
		})
		return
	}

	result := services.ImportBoards(req.Boards, req.Source)
	InvalidateDirectoryCache()
	c.JSON(http.StatusOK, result)
}

// HandleExportFailures exports the failed_requests table as a CSV file.
func HandleExportFailures(c *gin.Context) {
	var failures []models.FailedRequest
	if err := config.DB.Order("created_at desc").Find(&failures).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load failed requests"})
		return
	}

	c.Writer.Header().Set("Content-Type", "text/csv")
	c.Writer.Header().Set("Content-Disposition", "attachment;filename=failed_requests.csv")

	c.Writer.Write([]byte("ID,Provider,Query,Reason,CreatedAt\n"))
	for _, f := range failures {
		// Escape quotes and commas for CSV
		query := strings.ReplaceAll(f.Query, "\"", "\"\"")
		reason := strings.ReplaceAll(f.Reason, "\"", "\"\"")

		record := fmt.Sprintf("%d,\"%s\",\"%s\",\"%s\",\"%s\"\n",
			f.ID, f.Provider, query, reason, f.CreatedAt.Format(time.RFC3339))
		c.Writer.Write([]byte(record))
	}
}
