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

// HandleGetDirectoryStats backs the count strip above the Job Map grid.
// Separate from HandleGetCompanies because those counts follow the visitor's
// filters, and these describe the whole directory.
func HandleGetDirectoryStats(c *gin.Context) {
	stats, err := services.GetDirectoryStats()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch directory stats"})
		return
	}
	c.JSON(http.StatusOK, stats)
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

func HandleTriggerDiscovery(c *gin.Context) {
	var req triggerDiscoveryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "query is required"})
		return
	}
	if req.Limit <= 0 {
		req.Limit = 10
	}
	if req.Limit > 10 {
		req.Limit = 10
	}
	if len(req.Query) > 200 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "query is too long"})
		return
	}

	saved, err := services.DiscoverCompanies(req.Query, req.Limit)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"saved": len(saved), "companies": saved})
}

// HandlePruneDeadJobs runs live health checks across all jobs in the database
// and deletes any 404, 410 or expired postings.
func HandlePruneDeadJobs(c *gin.Context) {
	pruned, err := services.PruneDeadJobs()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to prune dead jobs", "details": err.Error()})
		return
	}

	stats, _ := services.GetDirectoryStats()
	c.JSON(http.StatusOK, gin.H{
		"message": "Dead jobs pruned successfully",
		"pruned_count": pruned,
		"active_jobs": stats.Jobs,
		"hiring_companies": stats.HiringCompanies,
	})
}

