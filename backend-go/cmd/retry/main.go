package main

import (
	"fmt"
	"log"

	"github.com/joho/godotenv"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/config"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/models"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/services"
)

// Retry job extraction for every company that has a careers URL but no jobs,
// now that we follow "view openings" links.
func main() {
	if err := godotenv.Load("../.env"); err != nil {
		log.Println("warn:", err)
	}
	config.ConnectDB()

	config.DB.Model(&models.Company{}).Where("ats_type = ''").Update("ats_checked_at", nil)

	var companies []models.Company
	config.DB.Raw(`
		SELECT c.* FROM companies c
		LEFT JOIN jobs j ON j.company_id = c.id
		WHERE c.ats_type = '' AND c.careers_url != ''
		GROUP BY c.id HAVING COUNT(j.id) = 0
	`).Scan(&companies)

	fmt.Printf("retrying %d companies\n", len(companies))
	fmt.Printf("usage before: %v\n\n", services.ScrapeUsageSummary())

	found := 0
	for _, c := range companies {
		n, err := services.SyncJobsForCompany(c)
		if err != nil {
			continue
		}
		if n > 0 {
			fmt.Printf("  OK  %-26s %3d jobs\n", trunc(c.Name, 26), n)
			found++
		}
	}
	fmt.Printf("\n%d of %d unblocked\n", found, len(companies))
	fmt.Printf("usage after: %v\n", services.ScrapeUsageSummary())
}

func trunc(s string, n int) string { if len(s) > n { return s[:n] }; return s }
