package controllers

import (
	"fmt"
	"net/http"
	"strconv"
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
	hiringOnly := c.Query("hiring") == "1"

	cacheKey := fmt.Sprintf("%s|%s|%s|%s|%t|%d|%d", sector, stage, area, q, hiringOnly, page, pageSize)

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

	companies, total, err := services.ListCompanies(sector, stage, area, q, hiringOnly, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch companies"})
		return
	}

	var openRoles int64
	if q == "" {
		openRoles, _ = services.TotalOpenRolesFast(sector, stage, area)
	} else {
		openRoles, _ = services.TotalOpenRoles(sector, stage, area, q)
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

	jobs, total, err := services.ListGlobalJobs(q, location, field, level, page, pageSize)
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

type triggerDiscoveryRequest struct {
	Query string `json:"query" binding:"required"`
	Limit int    `json:"limit"`
}

// HandleTriggerDiscovery runs one discovery search on demand.
//
// The query is a plain job search — "backend engineer jobs in Pune, India" —
// because it is run against job-board domains, not against the open web. The
// hits are boards, and every board is a company that is currently hiring.
func HandleTriggerDiscovery(c *gin.Context) {
	var req triggerDiscoveryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "query is required"})
		return
	}
	// The same ceiling the service enforces, not a larger one: each stored
	// company costs a metered search of its own, and a handler that accepts
	// 25 while the service caps at 5 promises something it will not deliver.
	if req.Limit <= 0 || req.Limit > services.MaxNewCompaniesPerRun {
		req.Limit = services.MaxNewCompaniesPerRun
	}
	if len(req.Query) > 200 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "query is too long"})
		return
	}

	saved, err := services.DiscoverFromBoardsManual(req.Query, req.Limit)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"saved": len(saved), "companies": saved})
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

// HandleRunDiscovery runs Exa board discovery across target tech hub locations.
func HandleRunDiscovery(c *gin.Context) {
	go services.RunDiscoveryRotation()
	c.JSON(http.StatusOK, gin.H{
		"status":           "discovery_running",
		"message":          "Exa board discovery rotation triggered in background",
		"budget_remaining": services.SearchBudgetRemaining(),
	})
}

