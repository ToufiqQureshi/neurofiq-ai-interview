# Backend-Go Services Architecture & Feature Breakdown

Is folder (`backend-go/services/`) me saare core business logic, automated discovery pipelines, ATS integrations, and AI interview evaluations rehte hain.
Ab architecture ko clean karke sirf **Exa / Tavily** (for targeted search) aur **Firecrawl** (for dynamic page rendering/scraping) par consolidate kiya gaya hai:

---

## 1. Targeted Board Discovery & Search Engine (Exa + Tavily)
Ye module targeted search queries run karke ATS board domains (Greenhouse, Lever, Ashby, Workable, SmartRecruiters, Keka, Darwinbox, Workday) se directly active Indian hiring companies aur ATS slugs discover karta hai.

- `search_provider.go`:
  - **Kya karta hai**: Exa API (primary) aur Tavily API (fallback) ko unified `WebSearch` interface ke piche manage karta hai with monthly budget capping and usage tracking (`scrape_usages`).
  - **Kyu zaroori hai**: Discovery ko domain filtering (`includeDomains`) deta hai taaki blog posts aur noisy links filter out ho sakein.
- `board_discovery.go`:
  - **Kya karta hai**: City aur role based rotation queries (`boardSeedQueries`) run karta hai, ATS URLs se slug extract karta hai, Indian job locations verify karta hai, aur real companies store karta hai.
- `cron_lease.go`:
  - **Kya karta hai**: Distributed cron locking using Supabase `cron_leases` table taaki multi-instance deploys me race conditions na ho.
- `pipeline_health.go`:
  - **Kya karta hai**: Discovery pipeline aur search/scrape budgets ki health monitor karta hai (`/api/pipeline/health`).
- `guard_backfill.go`:
  - **Kya karta hai**: Existing database companies ko audit karke foreign/dead/unverified entries ko clean up karta hai.

---

## 2. Jobs & ATS Integrations (Hiring Detection)
Ye module actual applicant tracking systems (Ashby, Greenhouse, Lever, Workable, SmartRecruiters, Keka, Darwinbox, Workday) se real-time job openings fetch karta hai.

- `job_service.go`:
  - **Kya karta hai**: Main entry point for syncing jobs across all supported ATS providers (`FetchATSJobs`, `SyncCompanyJobs`).
  - **Kyu zaroori hai**: External ATS APIs se jobs fetch karke hamare normalized `jobs` schema me convert karta hai.
- `job_facets.go`:
  - **Kya karta hai**: Job roles ko auto-categorize karta hai (Engineering, Product, Design, Sales, Marketing, etc.) aur experience level filter karta hai.
- `directory_counters.go`:
  - **Kya karta hai**: Companies table ke `open_roles` denormalized counter ko aggregate job count ke sath in-sync rakhta hai.

---

## 3. Web Scraping & Company Enrichment (Firecrawl)
Ye module public tech hiring directory aur interactive map view ko enrich karta hai.

- `scrape_service.go`:
  - **Kya karta hai**: Dynamic JavaScript-heavy careers pages ko render aur scrape karne ke liye **Firecrawl** API use karta hai with monthly quota enforcement.
- `company_service.go`:
  - **Kya karta hai**: Company listing, filtering, pagination, search aur map coordinates endpoints (`GetCompanies`, `GetCompanyBySlug`).
- `enrichment.go`:
  - **Kya karta hai**: Indian tech companies ke missing metadata (funding stage, logo, industry, coordinates) ko enrich karta hai.
- `extractor_service.go`:
  - **Kya karta hai**: Raw HTML/JSON responses se clean text aur job details extract karne ke helpers.

---

## 4. AI Interviews & Candidate Evaluation
Ye module candidate interview sessions, questions, speech processing aur AI evaluations ko drive karta hai.

- `question_service.go`:
  - **Kya karta hai**: Job description aur candidate profile ke basis pe dynamic, contextual interview questions generate karta hai.
- `evaluation_service.go`:
  - **Kya karta hai**: Candidate ke interview answers ko score karta hai (technical accuracy, communication, confidence).
- `radar_service.go`:
  - **Kya karta hai**: Candidate skill matrix (Radar chart) calculations provide karta hai.
- `github_service.go` & `github_commits.go`:
  - **Kya karta hai**: Candidate ke public GitHub repositories aur commits ko analyze karke real technical contribution metrics nikalta hai.

---

## 5. Infrastructure & Networking Utilities
Low-level robust networking, rate limiting aur testing utilities.

- `httputil.go`:
  - **Kya karta hai**: Resilient HTTP client with timeouts, custom User-Agents, and graceful error classification (`IsTransientFetchError`).
- `hostlimit.go`:
  - **Kya karta hai**: Per-host rate limiter (tokens per second) taaki kisi bhi ATS ko excessive requests na bheji jayein aur 429 rate limit na lage.
- `maintenance.go`:
  - **Kya karta hai**: Periodic DB hygiene checks and orphaned records cleanup.
