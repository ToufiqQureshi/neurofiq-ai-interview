# Backend-Go Services Architecture & Feature Breakdown

Is folder (`backend-go/services/`) me saare core business logic, automated background pipelines, ATS integrations, and AI interview evaluations rehte hain.
Taki kisi ko bhi files ka purpose samajhne me confusion na ho, in 38 files ko 5 distinct feature modules me classify kiya gaya hai:

---

## 1. Automated Discovery & Ingestion Pipeline (Zero-Cost Engine)
Ye module bina kisi costly paid search ke, zero-cost Indian startup registries and company boards se active hiring companies discover karke queue me daalta hai.

- `candidate_queue.go`:
  - **Kya karta hai**: Database candidate admission queue manage karta hai (`board_candidates`).
  - **Kyu zaroori hai**: Companies ko ek saath DB me dump karne ke bajay admission queue me rakhta hai taaki unhe politely rate-limited batches me admit kiya ja sake.
- `slug_harvest_schedule.go`:
  - **Kya karta hai**: Background cron leases schedule karta hai (`@every 3m` Startup Register collection aur `@every 2m` Candidate Admission).
  - **Kyu zaroori hai**: Continuous ingestion ensure karta hai taaki har 2-3 minute me nayi Indian tech firms platform pe live hoti rahein.
- `slug_source_register.go`:
  - **Kya karta hai**: `indianstartupmap.com` ke high-signal accelerator portfolios (Y Combinator, Techstars, Antler, Plug & Play) ko parse karta hai.
  - **Kyu zaroori hai**: Zero-cost, 100% verified Indian tech startups provide karta hai jinke paas genuine domain, location aur sector data hota hai.
- `slug_harvest.go`:
  - **Kya karta hai**: Candidate deduplication aur ATS slug admission logic (`admitCandidate`, `dedupeCandidates`).
  - **Kyu zaroori hai**: Duplicate companies ko rokt hai aur verify karta hai ki ATS board valid hai ya nahi.
- `cron_lease.go`:
  - **Kya karta hai**: Distributed cron locking using Supabase `cron_leases` table.
  - **Kyu zaroori hai**: Multiple backend instances me ek hi cron task ko duplicate run hone se rokt hai (race condition prevention).
- `pipeline_health.go`:
  - **Kya karta hai**: Discovery pipeline ki health monitor karta hai (`/api/pipeline/health`).
  - **Kyu zaroori hai**: Stale rotations, rate limit throttles aur empty candidate queues ko detect karta hai.
- `guard_backfill.go`:
  - **Kya karta hai**: Existing database companies ko audit karke foreign/dead/unverified candidates ko clean up karta hai.

---

## 2. Jobs & ATS Integrations (Hiring Detection)
Ye module actual applicant tracking systems (Ashby, Greenhouse, Lever, Workable, SmartRecruiters, Keka, Darwinbox, Workday) se real-time job openings fetch karta hai.

- `job_service.go`:
  - **Kya karta hai**: Main entry point for syncing jobs across all supported ATS providers (`FetchATSJobs`, `SyncCompanyJobs`).
  - **Kyu zaroori hai**: External ATS APIs se jobs fetch karke hamare normalized `jobs` schema me convert karta hai.
- `job_facets.go`:
  - **Kya karta hai**: Job roles ko auto-categorize karta hai (Engineering, Product, Design, Sales, Marketing, etc.) aur experience level filter karta hai.
- `directory_counters.go`:
  - **Kya karta hai**: Companies table ke `open_roles` denormalized counter ko aggregate job count ke sath perfectly in-sync rakhta hai.
- `search_provider.go`:
  - **Kya karta hai**: Exa/Google fallback search query abstraction (used only when direct ATS probe needs extra company URL context).
- `board_discovery.go`:
  - **Kya karta hai**: Company career pages se direct ATS board detection logic (`DetectATS`).

---

## 3. Companies & Public Directory
Ye module public tech hiring directory aur interactive map view ko serve karta hai.

- `company_service.go`:
  - **Kya karta hai**: Company listing, filtering, pagination, search aur map coordinates endpoints (`GetCompanies`, `GetCompanyBySlug`).
- `enrichment.go`:
  - **Kya karta hai**: High-signal Indian tech companies ke missing metadata (funding stage, logo, industry, coordinates) ko enrich karta hai.
- `scrape_service.go`:
  - **Kya karta hai**: Public careers page parser jo custom career pages se job listings extract karta hai.
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
  - **Kya karta hai**: Per-host rate limiter (tokens per second) taaki kisi bhi ATS (jaise Workable ya Lever) ko excessive requests na bheji jayein aur 429 rate limit na lage.
- `maintenance.go`:
  - **Kya karta hai**: Periodic DB hygiene checks and orphaned records cleanup.
