package controllers

import (
	"net/http"
	"strconv"

	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/config"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/models"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/services"
	"github.com/gin-gonic/gin"
)

// HandleGetCompanies is a public endpoint (no auth) listing the discovered
// company directory, filterable by sector/stage/area/search text.
func HandleGetCompanies(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "24"))

	sector, stage, area, q := c.Query("sector"), c.Query("stage"), c.Query("area"), c.Query("q")
	hiringOnly := c.Query("hiring") == "1"

	companies, total, err := services.ListCompanies(sector, stage, area, q, hiringOnly, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch companies"})
		return
	}

	// Total roles matching the same filters, for the "N open roles across
	// M companies" header. Non-fatal if it fails — the list still renders.
	openRoles, _ := services.TotalOpenRoles(sector, stage, area, q)
	fields, levels, _ := services.JobFacets(sector, stage, area, q)

	c.JSON(http.StatusOK, gin.H{
		"companies":  companies,
		"total":      total,
		"open_roles": openRoles,
		"facets":     gin.H{"field": fields, "level": levels},
	})
}

// HandleGetCompanyByID is a public endpoint returning a single company.
func HandleGetCompanyByID(c *gin.Context) {
	var company models.Company
	if err := config.DB.First(&company, "id = ?", c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "company not found"})
		return
	}
	c.JSON(http.StatusOK, company)
}

// HandleGetCompanyJobs is a public endpoint returning the open roles we've
// synced from a company's job board. Used when a user opens a company on
// the Job Map to see actual listings rather than just a careers link.
func HandleGetCompanyJobs(c *gin.Context) {
	jobs, err := services.ListJobsForCompany(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch jobs"})
		return
	}
	// Keep the expanded job list consistent with any active facet chips.
	jobs = services.FilterJobsByFacet(jobs, c.Query("field"), c.Query("level"))
	c.JSON(http.StatusOK, gin.H{"jobs": jobs, "total": len(jobs)})
}

type triggerDiscoveryRequest struct {
	Query string `json:"query" binding:"required"`
	Limit int    `json:"limit"`
}

// HandleTriggerDiscovery is an authenticated manual escape hatch alongside
// the automatic cron rotation in services.RunDiscoveryRotation.
func HandleTriggerDiscovery(c *gin.Context) {
	var req triggerDiscoveryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "query is required"})
		return
	}
	if req.Limit <= 0 {
		req.Limit = 10
	}

	saved, err := services.DiscoverCompanies(req.Query, req.Limit)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"saved": len(saved), "companies": saved})
}
