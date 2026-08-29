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

var ipLimiters sync.Map

func getIPLimiter(ip string) *rate.Limiter {
	if l, ok := ipLimiters.Load(ip); ok {
		return l.(*rate.Limiter)
	}
	limiter := rate.NewLimiter(rate.Limit(5), 10)
	actual, _ := ipLimiters.LoadOrStore(ip, limiter)
	return actual.(*rate.Limiter)
}

func main() {
	if err := godotenv.Load("../.env"); err != nil {
		log.Println("Warning: No .env file found or error reading it. Assuming env vars are injected by host.")
	}

	config.ConnectDB()

	if err := config.DB.AutoMigrate(&models.User{}, &models.GithubProfile{}, &models.Question{}, &models.InterviewSession{}, &models.Company{}, &models.Job{}, &models.ScrapeUsage{}); err != nil {
		log.Fatalf("Migration failed: %v", err)
	}

	auth.InitOAuth()

	r := gin.Default()

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

	sessionSecret := os.Getenv("SESSION_SECRET")
	if sessionSecret == "" {
		if os.Getenv("APP_ENV") == "production" {
			log.Fatal("SESSION_SECRET is required in production")
		}
		sessionSecret = "default_secret_for_local_dev"
		log.Println("Warning: SESSION_SECRET unset; using a local-dev default")
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

	r.Use(func(c *gin.Context) {
		limiter := getIPLimiter(c.ClientIP())
		if !limiter.Allow() {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "Too many requests"})
			c.Abort()
			return
		}
		c.Next()
	})

	r.GET("/health", func(c *gin.Context) {
		sqlDB, err := config.DB.DB()
		if err != nil || sqlDB.Ping() != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unhealthy", "db": "down"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "healthy", "db": "connected"})
	})

	r.GET("/auth/github/login", auth.HandleGithubLogin)
	r.GET("/auth/github/callback", auth.HandleGithubCallback)
	r.GET("/auth/me", auth.HandleAuthMe)
	r.POST("/auth/logout", auth.HandleLogout)

	r.GET("/api/companies", controllers.HandleGetCompanies)
	r.GET("/api/companies/:id", controllers.HandleGetCompanyByID)
	r.GET("/api/companies/:id/jobs", controllers.HandleGetCompanyJobs)

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

	go services.RunDiscoveryRotation()
	discoveryCron := cron.New()
	discoveryCron.AddFunc("@every 1h", services.RunDiscoveryRotation)
	discoveryCron.Start()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Starting Gin server on port %s", port)
	if err := r.Run(":" + port); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}
