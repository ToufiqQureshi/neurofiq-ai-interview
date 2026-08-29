package main

import (
	"log"
	"net/http"
	"os"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/joho/godotenv"
	"github.com/robfig/cron/v3"
	"golang.org/x/time/rate"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/auth"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/config"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/controllers"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/models"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/services"
)

// ipLimiters holds one token bucket per client IP (5 req/sec, burst of 10).
var ipLimiters sync.Map // map[string]*rate.Limiter

func getIPLimiter(ip string) *rate.Limiter {
	if l, ok := ipLimiters.Load(ip); ok {
		return l.(*rate.Limiter)
	}
	limiter := rate.NewLimiter(rate.Limit(5), 10)
	actual, _ := ipLimiters.LoadOrStore(ip, limiter)
	return actual.(*rate.Limiter)
}

func main() {
	// 1. Load environment variables from .env file
	// We use "../.env" because main.go is in backend-go/, but .env is in the root directory.
	if err := godotenv.Load("../.env"); err != nil {
		log.Println("Warning: No .env file found or error reading it. Assuming env vars are injected by host.")
	}

	// 2. Connect to the Postgres database via GORM
	config.ConnectDB()

	// AutoMigrate the new schema models
	if err := config.DB.AutoMigrate(&models.User{}, &models.GithubProfile{}, &models.Question{}, &models.InterviewSession{}, &models.Company{}, &models.Job{}, &models.ScrapeUsage{}); err != nil {
		log.Fatalf("Migration failed: %v", err)
	}

	// Initialize OAuth config
	auth.InitOAuth()

	// 3. Initialize the Gin router with default middleware (logger and recovery)
	r := gin.Default()

	// Configure CORS
	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = "http://localhost:5173"
	}
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{frontendURL},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
	}))

	// Initialize session middleware
	sessionSecret := os.Getenv("SESSION_SECRET")
	if sessionSecret == "" {
		sessionSecret = "default_secret_for_local_dev"
	}
	store := cookie.NewStore([]byte(sessionSecret))
	store.Options(sessions.Options{
		Path:     "/",
		MaxAge:   86400 * 7,
		HttpOnly: true,
		Secure:   os.Getenv("APP_ENV") == "production",
		SameSite: http.SameSiteLaxMode,
	})
	r.Use(sessions.Sessions("neurofiq_session", store))

	// 4. Rate Limiter Middleware — one bucket per client IP, so a handful of
	// legitimate concurrent users can't exhaust a single shared bucket and
	// lock everyone else out.
	r.Use(func(c *gin.Context) {
		limiter := getIPLimiter(c.ClientIP())
		if !limiter.Allow() {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "Too many requests"})
			c.Abort()
			return
		}
		c.Next()
	})

	// 5. Setup a basic health check endpoint
	r.GET("/health", func(c *gin.Context) {
		// c.JSON automatically formats the response as JSON and sets the correct headers
		c.JSON(http.StatusOK, gin.H{
			"status": "healthy",
			"db":     "connected",
		})
	})

	// 6. Auth Routes
	r.GET("/auth/github/login", auth.HandleGithubLogin)
	r.GET("/auth/github/callback", auth.HandleGithubCallback)
	r.GET("/auth/me", auth.HandleAuthMe)

	// Public company directory ("Job Map") — no auth required
	r.GET("/api/companies", controllers.HandleGetCompanies)
	r.GET("/api/companies/:id", controllers.HandleGetCompanyByID)
	r.GET("/api/companies/:id/jobs", controllers.HandleGetCompanyJobs)

	// 6. API Routes
	api := r.Group("/api")
	api.Use(auth.AuthMiddleware())
	{
		api.GET("/repos", controllers.HandleGetRepos)
		api.POST("/repos/analyze", controllers.HandleAnalyzeRepo)
		api.GET("/repos/analyze/status", controllers.HandleCheckAnalysisStatus)
		api.GET("/interviews/questions", controllers.HandleGetQuestions)
		api.POST("/interviews/submit", controllers.HandleSubmitInterview)
		api.GET("/reports", controllers.HandleGetReports)
		api.GET("/reports/:id", controllers.HandleGetReportByID)
		api.POST("/companies/discover", controllers.HandleTriggerDiscovery)
	}

	// Automatic company discovery: rotates through seed queries so the Job
	// Map directory fills itself in without any manual scraping. Run once
	// immediately so a fresh deploy doesn't sit empty for up to 6h waiting
	// on the first scheduled tick.
	go services.RunDiscoveryRotation()
	discoveryCron := cron.New()
	// Hourly, matching services.discoveryIntervalSeconds — the rotation
	// cursor is derived from that interval, so the two must agree.
	discoveryCron.AddFunc("@every 1h", services.RunDiscoveryRotation)
	discoveryCron.Start()

	// 7. Determine port and start server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Starting Gin server on port %s", port)
	// r.Run() blocks forever, keeping the server alive
	if err := r.Run(":" + port); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}
