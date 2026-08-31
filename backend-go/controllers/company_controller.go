package controllers

import (
	"net/http"
	"strconv"

	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/config"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/models"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/services"
	"github.com/gin-gonic/gin"
)

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

	openRoles, _ := services.TotalOpenRoles(sector, stage, area, q)
	fields, levels, _ := services.JobFacets(sector, stage, area, q)

	c.JSON(http.StatusOK, gin.H{
		"companies":  companies,
		"total":      total,
		"open_roles": openRoles,
		"facets":     gin.H{"field": fields, "level": levels},
	})
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
	if req.Limit <= 0 || req.Limit > 25 {
		req.Limit = 25
	}
	if len(req.Query) > 200 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "query is too long"})
		return
	}

	saved, err := services.DiscoverFromBoards(req.Query, req.Limit)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"saved": len(saved), "companies": saved})
}
