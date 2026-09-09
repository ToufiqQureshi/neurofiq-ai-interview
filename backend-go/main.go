package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"github.com/robfig/cron/v3"
	"golang.org/x/time/rate"

	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/auth"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/config"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/controllers"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/models"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/services"
)

// visitor is one client's token bucket plus the time we last saw it, so the
// limiter can forget clients that have gone away.
type visitor struct {
	limiter *rate.Limiter
	// Unix nanoseconds, atomic: written by every request goroutine and read
	// by the housekeeping cron. sync.Map protects the map, not the value it
	// stores, and a time.Time is several words — a torn read could evict a
	// bucket that is in active use.
	lastSeen atomic.Int64
}

// ipLimiters holds one token bucket per client IP (5 req/sec, burst of 10).
//
// Entries are swept, not kept forever: an unbounded map keyed by remote
// address is a memory leak that a single attacker can drive, since every
// forged source address allocates a bucket nobody ever frees.
var (
	ipLimiters   sync.Map // map[string]*visitor
	writeLimiter sync.Map // map[userID]*visitor — the expensive endpoints
)

func getLimiter(store *sync.Map, key string, r rate.Limit, burst int) *rate.Limiter {
	now := time.Now().UnixNano()
	if v, ok := store.Load(key); ok {
		vis := v.(*visitor)
		vis.lastSeen.Store(now)
		return vis.limiter
	}
	vis := &visitor{limiter: rate.NewLimiter(r, burst)}
	vis.lastSeen.Store(now)
	actual, _ := store.LoadOrStore(key, vis)
	// Refresh on the losing side of the race too: otherwise a bucket only
	// ever reached through this path never has its timestamp touched and
	// ages out while it is still being used.
	existing := actual.(*visitor)
	existing.lastSeen.Store(now)
	return existing.limiter
}

// sweepLimiters drops buckets nobody has used recently. A bucket at rest is
// indistinguishable from a fresh one, so forgetting it costs nothing.
func sweepLimiters(store *sync.Map, idleFor time.Duration) {
	cutoff := time.Now().Add(-idleFor).UnixNano()
	store.Range(func(key, value interface{}) bool {
		if value.(*visitor).lastSeen.Load() < cutoff {
			store.Delete(key)
		}
		return true
	})
}

func main() {
	// 1. Load environment variables from the .env file.
	// We use "../.env" because main.go is in backend-go/, but .env is in the
	// repository root.
	if err := godotenv.Load("../.env"); err != nil {
		log.Println("Warning: No .env file found or error reading it. Assuming env vars are injected by host.")
	}

	isProduction := os.Getenv("APP_ENV") == "production"
	if isProduction {
		gin.SetMode(gin.ReleaseMode)
	}

	// 2. Connect to Postgres via GORM.
	config.ConnectDB()

	// AutoMigrate the schema models.
	if err := config.DB.AutoMigrate(
		&models.User{}, &models.GithubProfile{}, &models.Question{},
		&models.InterviewSession{}, &models.InterviewInvite{},
		&models.Company{}, &models.Job{}, &models.ScrapeUsage{},
		&models.CronLease{}, &models.FailedRequest{},
	); err != nil {
		log.Fatalf("Migration failed: %v", err)
	}

	// Any analysis still marked "pending" is one this process (or a previous
	// one) was running when it stopped. Nothing will ever finish it, so the
	// user would sit on a spinner forever and their free slot would stay
	// spent. Reclaim them at boot rather than making the user wait it out.
	services.ReclaimStaleAnalyses(30 * time.Minute)

	auth.InitOAuth()

	// 3. Initialize the Gin router with logger and recovery middleware.
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	// Gin trusts every proxy by default, which means any client can set
	// X-Forwarded-For and pick its own ClientIP — and the rate limiter below
	// keys on exactly that. Trust only the proxies we actually run behind.
	proxies := trustedProxies()
	if err := r.SetTrustedProxies(proxies); err != nil {
		log.Fatalf("Invalid TRUSTED_PROXIES: %v", err)
	}
	if isProduction && len(proxies) == 0 {
		log.Println("WARNING: TRUSTED_PROXIES is unset. If this instance runs behind a " +
			"load balancer, every request will report the balancer's address, the per-IP " +
			"rate limiter will put all users in one bucket, and a busy minute will 429 " +
			"everyone. Set TRUSTED_PROXIES to your balancer's CIDR.")
	}

	// Configure CORS. FRONTEND_URL may hold a comma-separated list so the
	// apex domain and a preview deployment can both reach the API.
	allowedOrigins := allowedOrigins()
	r.Use(cors.New(cors.Config{
		AllowOrigins:     allowedOrigins,
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	// A request body larger than this is refused before it is read into
	// memory. Every endpoint here takes a small JSON object.
	r.Use(limitRequestBody(1 << 20)) // 1 MB

	// Session middleware.
	//
	// Two keys, not one: the first signs the cookie, the second encrypts it.
	// This session carries the user's GitHub OAuth token, and a signed-only
	// cookie is fully readable by anyone holding it — including any browser
	// extension on the user's machine. Signing proves we issued it; it does
	// not keep the contents private.
	sessionSecret := os.Getenv("SESSION_SECRET")
	if sessionSecret == "" {
		if isProduction {
			log.Fatal("SESSION_SECRET is required in production")
		}
		sessionSecret = "default_secret_for_local_dev"
		log.Println("Warning: SESSION_SECRET unset; using a local-dev default")
	}
	blockKey := sha256.Sum256([]byte(sessionSecret + "|cookie-encryption"))
	store := cookie.NewStore([]byte(sessionSecret), blockKey[:])
	store.Options(sessions.Options{
		Path:     "/",
		MaxAge:   86400 * 7,
		HttpOnly: true,
		Secure:   isProduction,
		SameSite: http.SameSiteLaxMode,
	})
	r.Use(sessions.Sessions("neurofiq_session", store))

	// 4. Health check, registered BEFORE the rate limiter.
	//
	// The load balancer polls this constantly from a single address. Behind
	// the limiter it competes for the same bucket as real traffic, so a busy
	// minute answers the health probe with 429 and the balancer pulls a
	// perfectly healthy instance out of rotation — turning a traffic spike
	// into an outage.
	//
	// It pings the database, because an API that cannot reach Postgres is not
	// healthy and should be drained rather than left serving 500s.
	r.GET("/health", func(c *gin.Context) {
		sqlDB, err := config.DB.DB()
		if err != nil || sqlDB.Ping() != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unhealthy", "db": "down"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "healthy", "db": "connected"})
	})

	// 5. Rate limiter — one bucket per client IP, so a handful of legitimate
	// concurrent users can't exhaust a single shared bucket and lock everyone
	// else out.
	//
	// This is only true while ClientIP is really per-client. Behind a proxy
	// with TRUSTED_PROXIES unset, every request carries the balancer's
	// address, all users land in ONE bucket, and the limiter stops being a
	// per-abuser control and becomes a global cap. The startup check below
	// makes that misconfiguration loud instead of silent, and the ceiling is
	// tunable so an operator is never stuck with a number we guessed.
	// A zero or negative value here is not a lower limit, it's an outage —
	// rate.NewLimiter with burst 0 or a non-positive rate admits nothing, so
	// a typo'd env var (RATE_LIMIT_BURST=0) would silently 429 every request
	// instead of falling back to the default. Only a valid positive value
	// overrides the default; anything else is treated the same as unset.
	var ipRate float64 = 10
	if raw := os.Getenv("RATE_LIMIT_RPS"); raw != "" {
		if f, err := strconv.ParseFloat(raw, 64); err == nil && f > 0 {
			ipRate = f
		}
	}
	var ipBurst int = 30
	if raw := os.Getenv("RATE_LIMIT_BURST"); raw != "" {
		if i, err := strconv.Atoi(raw); err == nil && i > 0 {
			ipBurst = i
		}
	}
	r.Use(func(c *gin.Context) {
		limiter := getLimiter(&ipLimiters, c.ClientIP(), rate.Limit(ipRate), ipBurst)
		if !limiter.Allow() {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "Too many requests"})
			return
		}
		c.Next()
	})

	// 6. Auth routes.
	r.POST("/auth/register", controllers.HandleRegister)
	r.POST("/auth/login", controllers.HandleLogin)
	r.GET("/auth/github/login", auth.HandleGithubLogin)
	r.GET("/auth/github/callback", auth.HandleGithubCallback)
	r.GET("/auth/me", auth.HandleAuthMe)
	r.POST("/auth/logout", auth.HandleLogout)

	// Public company directory & job discovery ("Job Map" & "Find Jobs") — no auth required.
	r.GET("/api/companies", controllers.HandleGetCompanies)
	r.GET("/api/companies/stats", controllers.HandleGetDirectoryStats)
	r.GET("/api/jobs", controllers.HandleGetGlobalJobs)
	r.POST("/api/admin/reclassify-jobs", controllers.HandleReclassifyJobs)
	r.POST("/api/admin/enrich-companies", controllers.HandleEnrichCompanies)
	r.POST("/api/admin/import-boards", controllers.HandleImportBoards)
	r.GET("/api/admin/failures/export", controllers.HandleExportFailures)
	// Whether the pipeline behind that directory is actually working. Public
	// and read-only: it reports counts and timings the directory already
	// exposes, and being able to ask without credentials is the point — a
	// health check nobody can reach is one nobody checks.
	r.GET("/api/pipeline/health", controllers.HandleGetPipelineHealth)
	r.GET("/api/companies/:id", controllers.HandleGetCompanyByID)
	r.GET("/api/companies/:id/jobs", controllers.HandleGetCompanyJobs)

	// Public, link-only route. A shared report is authorised by holding an
	// unguessable URL, so it sits outside the session middleware on purpose.
	r.GET("/api/public/reports/:slug", controllers.HandleGetPublicReport)

	// WebSocket Gateway for live interview audio/text streaming.
	// This sits outside auth for now to allow testing, but should be secured in production.
	r.GET("/ws/interview", controllers.HandleInterviewWebSocket)

	// Public ATS Invites (Verification)
	r.GET("/api/invites/:token", controllers.VerifyInvite)

	// 7. Authenticated API routes.
	api := r.Group("/api")
	api.Use(auth.AuthMiddleware())
	{
		api.POST("/user/onboarding", controllers.HandleOnboarding)
		api.GET("/repos", controllers.HandleGetRepos)
		api.GET("/interviews/questions", controllers.HandleGetQuestions)
		api.GET("/reports", controllers.HandleGetReports)
		api.GET("/reports/:id", controllers.HandleGetReportByID)
		api.GET("/repos/analyze/status", controllers.HandleCheckAnalysisStatus)

		// ATS Invites
		api.POST("/invites", controllers.CreateInvite)

		// Everything below spends money — an LLM call, a scraper credit, or
		// a repository download. The per-IP limiter above does not cover
		// these: one account behind one IP can stay under 5 req/s and still
		// run up a bill all day. These are throttled per user instead.
		paid := api.Group("")
		paid.Use(perUserLimit(rate.Limit(0.5), 5)) // ~30/min sustained, burst 5
		{
			paid.POST("/repos/analyze", controllers.HandleAnalyzeRepo)
			paid.POST("/interviews/submit", controllers.HandleSubmitInterview)
		}

		// The user-triggered discovery endpoint and its own hard rate limit are
		// gone with the search that backed them. It existed to spend a metered
		// allowance the product could not buy more of, which is why it was
		// throttled to two calls an hour; nothing on this server spends that
		// allowance any more.

		api.POST("/reports/:id/share", controllers.HandleShareReport)
	}

	// 8. Background schedules.
	//
	// Finding boards is no longer one of them. Search moved out to
	// scripts/discover_companies.py, which queries a self-hosted SearXNG behind
	// a residential proxy — free, and deeper per query than the metered API this
	// used to pay for: one query walked four pages returns around fifty distinct
	// companies where an Exa call returned forty-five. It reports what it finds
	// to POST /api/admin/import-boards and the server admits it, so every guard
	// still runs here and nothing outside this process writes to the database.
	//
	// Blocking, before a single request can be served: the directory listing
	// filters and sorts on companies.open_roles, and that column is zero for
	// every row until this runs. A server that comes up first answers
	// "hiring=1" with an empty directory.
	services.RunBlockingStartupRepairs()

	// Derived columns first, before anything reads them. companies.open_roles
	// and jobs.field/level ship with data already behind them, so on the first
	// boot after this change they are empty — an empty-looking directory until
	// something fills them in. That something is this, not a command anyone
	// has to remember: a migration nobody runs is a migration that did not
	// happen.
	go safely("startup repairs", func() {
		services.RunStartupRepairs()
		// The first health line, so a deploy says whether it came up working
		// rather than only that it came up.
		services.LogPipelineHealth()
	})

	// The backfill follows the sync in the same goroutine rather than racing
	// it. Run in parallel, the sync held a company list read before the
	// backfill deleted one of them from under it, and wrote that company's
	// roles back with a company_id no longer in the table — 48 orphans, made
	// by the very pass that exists to remove them.
	go safely("startup sync and backfill", func() {
		services.SyncAllCompanyJobs()
		// A deploy is exactly when the rules have just changed, so this runs
		// now rather than up to twelve hours from now.
		services.ReapplyGuards()
		services.RunEnrichment()
	})

	scheduler := cron.New()
	// No discovery schedule here any more. The search that finds new boards
	// runs outside this process (see the note above section 8); what arrives
	// through the importer is admitted by the same guards either path always
	// used, so removing the schedule removed a caller, not a rule.
	//
	// Everything below is work on companies already stored — free, and
	// unaffected by where they were found.

	// mustSchedule registers one job or refuses to start. A schedule that
	// silently failed to register is a background task nobody notices is
	// missing until the data it maintains has already drifted.
	mustSchedule := func(spec, name string, job func()) {
		if _, err := scheduler.AddFunc(spec, func() { safely(name, job) }); err != nil {
			log.Fatalf("Failed to schedule %s: %v", name, err)
		}
	}

	// Job syncing stays hourly on its own schedule, so a closed posting drops
	// off within the hour even between discovery runs.
	mustSchedule("@every 5m", "job sync", services.RunJobSync)

	// Enrichment reads each company's own homepage for the description and
	// sector its card needs. One free GET per company, no metered search and
	// no model, so it keeps its own schedule instead of competing with
	// discovery for a budget. A bounded batch each hour works through the
	// backlog without sweeping the table at once.
	mustSchedule("@every 1h", "enrichment", services.RunEnrichment)

	// Says, on a schedule, whether roles are still arriving — and says it
	// loudly when they are not. Every failure this catches is a quiet one: a
	// throttled provider, a sync rotation falling behind.
	mustSchedule("@every 1h", "pipeline health", services.LogPipelineHealth)

	// Finishes any job classification the boot pass did not reach, and
	// reclassifies everything when the bucket rules change.
	mustSchedule("@every 12h", "job facet backfill", services.RunFacetBackfill)

	// Repairs the denormalised companies.open_roles from the jobs table. The
	// counter is written by every path that writes roles; this is what makes a
	// drift caused by a crash mid-write self-correcting rather than permanent.
	mustSchedule("@every 6h", "open-role recount", func() {
		if n, err := services.RecountOpenRoles(); err != nil {
			log.Printf("open-role recount failed: %v", err)
		} else if n > 0 {
			log.Printf("open-role recount: corrected %d companies", n)
		}
	})

	// Housekeeping: reclaim abandoned analyses and forget idle rate-limit
	// buckets. Cheap, and it keeps a long-running process from drifting.
	mustSchedule("@every 15m", "housekeeping", func() {
		services.ReclaimStaleAnalyses(30 * time.Minute)
		sweepLimiters(&ipLimiters, time.Hour)
		sweepLimiters(&writeLimiter, time.Hour)
	})

	// One entry, not two: the prune deletes rows and recounts, the guard pass
	// re-runs the admission rules over what is left. They share the same tick
	// deliberately so they run in that order rather than interleaving their
	// writes.
	mustSchedule("@every 12h", "prune and guard backfill", func() {
		safely("prune dead jobs", func() {
			services.PruneDeadJobs()
		})
		// Every guard in services runs at insert and never again, so a rule
		// added today leaves yesterday's violations in place — and clearing
		// those has meant hand-written DELETEs against production, which is
		// the one thing a self-maintaining directory must never need.
		safely("guard backfill", func() {
			services.ReapplyGuards()
		})
	})
	scheduler.Start()

	// 9. Serve, and shut down cleanly.
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		// Long enough for an interview submission, which waits on the LLM.
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		log.Printf("Starting Gin server on port %s (origins: %s)", port, strings.Join(allowedOrigins, ", "))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Server failed to start: %v", err)
		}
	}()

	// Without this, a deploy kills the process mid-request: in-flight
	// interviews lose their evaluation after the LLM has already been paid
	// for, and any analysis running in a background goroutine dies silently.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutdown signal received — draining connections...")

	// Stop taking new scheduled work and hand the sync lease back, so the next
	// instance can pick it up immediately instead of waiting out the remaining
	// TTL.
	<-scheduler.Stop().Done()
	services.ReleaseCronLease(services.JobSyncLeaseName)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("Forced shutdown: %v", err)
	}
	log.Println("Server stopped.")
}

// perUserLimit throttles the endpoints that cost money, keyed on the session
// user rather than the source address.
func perUserLimit(r rate.Limit, burst int) gin.HandlerFunc {
	return perUserLimitOn(&writeLimiter, r, burst)
}

// perUserLimitOn is perUserLimit against a named bucket store, so an endpoint
// whose cost is not measured in the same units as its neighbours can carry a
// limit of its own without loosening theirs.
func perUserLimitOn(store *sync.Map, r rate.Limit, burst int) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, _ := c.Get("user_id")
		key, _ := userID.(string)
		if key == "" {
			key = c.ClientIP()
		}
		if !getLimiter(store, key, r, burst).Allow() {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "You're going a bit fast. Wait a moment and try again.",
			})
			return
		}
		c.Next()
	}
}

// limitRequestBody refuses oversized payloads before they are buffered.
func limitRequestBody(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		c.Next()
	}
}

// safely runs a background job with its own recover().
//
// Gin's Recovery() middleware only covers the request goroutine. A panic in a
// scheduled job would otherwise take the whole process down, for every
// connected user, because of one malformed careers page.
func safely(name string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("PANIC in %s: %v", name, r)
		}
	}()
	fn()
}

// allowedOrigins reads the browser origins permitted to call this API.
func allowedOrigins() []string {
	return config.AllowedOrigins()
}

// trustedProxies returns the proxy addresses whose X-Forwarded-For we honour.
// nil means "trust nothing and use the direct peer address", which is the
// right default: it makes ClientIP unspoofable at the cost of showing the
// load balancer's address until TRUSTED_PROXIES is configured.
func trustedProxies() []string {
	raw := strings.TrimSpace(os.Getenv("TRUSTED_PROXIES"))
	if raw == "" {
		return nil
	}
	var proxies []string
	for _, p := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			proxies = append(proxies, trimmed)
		}
	}
	return proxies
}
