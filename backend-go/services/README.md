# Backend-Go Services Architecture & Feature Breakdown

Is folder (`backend-go/services/`) me saare core business logic, automated discovery pipelines, ATS integrations, and AI interview evaluations rehte hain.
Ab architecture ko clean karke sirf **Exa / Tavily** (for targeted search) aur **Firecrawl** (for dynamic page rendering/scraping) par consolidate kiya gaya hai:

---

## 1. Targeted Board Discovery & Search Engine (Exa + Tavily)
Ye module targeted search queries run karke ATS board domains (Greenhouse, Lever, Ashby, Workable, SmartRecruiters, Keka, Darwinbox, Workday) se directly active Indian hiring companies aur ATS slugs discover karta hai.

- `discovery_search.go`:
  - **Kya karta hai**: Exa API (primary) aur Tavily API (fallback) ko unified `WebSearch` interface ke piche manage karta hai with monthly budget capping and usage tracking (`scrape_usages`).
  - **Kyu zaroori hai**: Discovery ko domain filtering (`includeDomains`) deta hai taaki blog posts aur noisy links filter out ho sakein.
- `discovery_boards.go`:
  - **Kya karta hai**: City aur role based rotation queries (`boardSeedQueries`) run karta hai, ATS URLs se slug extract karta hai, Indian job locations verify karta hai, aur real companies store karta hai.
- `infra_cron.go`:
  - **Kya karta hai**: Distributed cron locking using Supabase `cron_leases` table taaki multi-instance deploys me race conditions na ho.
- `directory_health.go`:
  - **Kya karta hai**: Discovery pipeline aur search/scrape budgets ki health monitor karta hai (`/api/pipeline/health`).
- `jobs_guards.go`:
  - **Kya karta hai**: Existing database companies ko audit karke foreign/dead/unverified entries ko clean up karta hai.

---

## 2. Jobs & ATS Integrations (Hiring Detection)
Ye module actual applicant tracking systems (Ashby, Greenhouse, Lever, Workable, SmartRecruiters, Keka, Darwinbox, Workday) se real-time job openings fetch karta hai.

- `jobs_ats.go`:
  - **Kya karta hai**: The twelve ATS clients — each provider's response shape and its fetcher — plus `scanForATS`/`DetectATS`, which find a company's board in the first place.
  - **Kyu zaroori hai**: Har ek official public JSON endpoint hai, wahi jo company ke apne careers page ko render karta hai — isliye board padhna free hai aur koi key nahi chahiye.
- `jobs_sync.go`:
  - **Kya karta hai**: `SyncJobsForCompany`, `FetchATSJobs`, `applySyncedJobs`, `replaceJobsForCompany` — a board's answer turned into rows.
  - **Kyu zaroori hai**: Yahin wo guards hain jo maayne rakhte hain — ek khaali read "not hiring" nahi hai, talent-pool posting vacancy nahi hai, aur India ke bahar ka role is directory ka kaam nahi hai.
- `jobs_careers_page.go`:
  - **Kya karta hai**: `syncJobsFromCareersPage` and the link/text scanning behind it, for companies with no readable board.
  - **Kyu zaroori hai**: Careers page ko apni listings kahin public jagah se load karni hi padti hai (visitor logged in nahi hota) — rendered read ya LLM extraction tabhi kharch hota hai jab free tiers khaali aayein.
- `jobs_facets.go`:
  - **Kya karta hai**: Job roles ko auto-categorize karta hai (Engineering, Product, Design, Sales, Marketing, etc.) aur experience level filter karta hai.
- `directory_counters.go`:
  - **Kya karta hai**: Companies table ke `open_roles` denormalized counter ko aggregate job count ke sath in-sync rakhta hai.

---

## 3. Web Scraping & Company Enrichment (Firecrawl)
Ye module public tech hiring directory aur interactive map view ko enrich karta hai.

- `discovery_scrape.go`:
  - **Kya karta hai**: Dynamic JavaScript-heavy careers pages ko render aur scrape karne ke liye **Firecrawl** API use karta hai with monthly quota enforcement.
- `directory_companies.go`:
  - **Kya karta hai**: Company listing, filtering, pagination, search aur map coordinates endpoints (`GetCompanies`, `GetCompanyBySlug`).
- `discovery_enrich.go`:
  - **Kya karta hai**: Indian tech companies ke missing metadata (funding stage, logo, industry, coordinates) ko enrich karta hai.
- `discovery_extract.go`:
  - **Kya karta hai**: Raw HTML/JSON responses se clean text aur job details extract karne ke helpers.

---

## 4. AI Interviews & Candidate Evaluation
Ye module candidate interview sessions, questions, speech processing aur AI evaluations ko drive karta hai.

- `interview_questions.go`:
  - **Kya karta hai**: Job description aur candidate profile ke basis pe dynamic, contextual interview questions generate karta hai.
- `interview_evaluation.go`:
  - **Kya karta hai**: Candidate ke interview answers ko score karta hai (technical accuracy, communication, confidence).
- `interview_github.go` & `interview_github_commits.go`:
  - **Kya karta hai**: Candidate ke public GitHub repositories aur commits ko analyze karke real technical contribution metrics nikalta hai.

---

## 5. Infrastructure & Networking Utilities
Low-level robust networking, rate limiting aur testing utilities.

- `infra_http.go`:
  - **Kya karta hai**: Resilient HTTP client with timeouts, custom User-Agents, and graceful error classification (`IsTransientFetchError`).
- `infra_hostlimit.go`:
  - **Kya karta hai**: Per-host rate limiter (tokens per second) taaki kisi bhi ATS ko excessive requests na bheji jayein aur 429 rate limit na lage.
- `infra_maintenance.go`:
  - **Kya karta hai**: Periodic DB hygiene checks and orphaned records cleanup.
