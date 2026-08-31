# Job Map — Session Handoff

**Sessions:** 2026-08-27 → 2026-08-29
**For:** whoever (human or AI agent) picks this up next.

`PROGRESS.md` has the dated one-liners. This file has the full picture: what
was built, why it's built that way, what's verified, what's broken, and what
to do next.

**Current state:** 67 companies · 19 hiring · **213 open roles**

---

## 1. What this feature is

An automated startup + **real open jobs** directory at `/directory`, reachable
from the sidebar under "Discover".

Inspired by bangalorestartupmap.com, but **no data is scraped from that site** —
their curated dataset is their own IP. Everything here is sourced independently.

### Pipeline

```
Go cron (@every 1h, plus one run at server startup)   [main.go:134-139]
  │
  ├─▶ DiscoverCompanies()                    ── finds NEW companies
  │     └─▶ POST ai-worker /internal/discover-companies
  │           └─▶ Agno discovery_agent
  │                 DeepSeek + ExaTools(category="company")
  │                 + DuckDuckGoTools as keyless fallback
  │     └─▶ dedupe by domain AND normalized name  (see §5)
  │     └─▶ geocode `area` via Nominatim, rate-limited to 1 req/1.1s
  │
  └─▶ SyncAllCompanyJobs()                   ── refreshes jobs for ALL companies
        └─▶ per company: resolve careers URL → detect ATS → fetch jobs → upsert

GET /api/companies (public) ──▶ frontend /directory (grid + Leaflet map)
```

**The two halves are deliberately independent.** Discovery depends on a live
web search and the AI worker, so it's the flakier half. If it fails, job sync
still runs. (This was a bug once — see §6.3.)

---

## 2. How jobs are found — four tiers, cheapest first

**No LLM is used to *find* the ATS.** It's a lookup, not a judgment call.

The insight the whole feature rests on:

> A careers page has to load its jobs from somewhere, and that somewhere must
> be **public** — because visitors aren't logged in.

### The tiers (`services/job_service.go`)

| Tier | What | Cost |
|---|---|---|
| **0** | `ResolveCareersURL` — if the agent gave no careers URL (or gave the homepage), probe `/careers`, `/jobs`, `/careers/jobs`… and verify the page contains careers vocabulary | **Free** |
| **1** | Plain HTTP fetch → regex for an embedded ATS board link | **Free** |
| **2** | Hosted render (Firecrawl → Jina fallback) → same regex | 1 credit |
| **3** | Slugify the company name, verify against each provider's real API | **Free** |
| **4** | **No ATS at all** → Firecrawl LLM extraction straight off the careers page | 1 credit |

Result is cached on the company row (`ats_type`, `ats_slug`), so detection is
**once per company**, not per sync.

**Tier ordering matters:** Razorpay's Greenhouse slug is
`razorpaysoftwareprivatelimited`, not `razorpay`. Slug-guessing alone would
have missed it. That's why the HTML scan is primary.

**Tier 4 is where most jobs now come from.** 10 of 16 hiring companies use a
custom portal, not a supported ATS. Without tier 4 they'd all show zero.

### Supported ATS platforms (7)

| Platform | Endpoint | Notes |
|---|---|---|
| Greenhouse | `boards-api.greenhouse.io/v1/boards/<slug>/jobs` | Regex allows regional boards (`job-boards.eu.greenhouse.io`) |
| Lever | `api.lever.co/v0/postings/<slug>?mode=json` | |
| SmartRecruiters | `api.smartrecruiters.com/v1/companies/<slug>/postings?limit=100` | **`limit=100` required** — without it you silently get 10 |
| Ashby | `api.ashbyhq.com/posting-api/job-board/<slug>` | Has `jobUrl` directly |
| Keka | `<slug>.keka.com/careers/api/jobs/default/active` | See below |
| Workday | `<tenant>.<region>.myworkdayjobs.com/wday/cxs/<tenant>/<site>/jobs` (POST) | Slug stored as `tenant:region:site`; site id is probed |
| Workable | `apply.workable.com/api/v1/widget/accounts/<slug>?details=true` | ⚠️ unverified — see §7 |

**The Keka find:** Keka's official developer API is partner-gated. But every
Keka-hosted careers portal exposes the endpoint above publicly with no auth —
found by watching the network tab on a real Keka careers page. The gated API
is for HR admins *writing* data; the read path is necessarily open.

**Workday quirk:** the URL gives tenant + region but not the job-site id, so
detection probes `External`, `External_Careers`, `careers`, etc.

---

## 3. Scraping providers (Firecrawl + Jina)

`services/scrape_service.go`. We do **not** run headless Chrome ourselves —
both providers render on their own infrastructure, so no RAM/hosting cost here.

| Provider | Cost | Role |
|---|---|---|
| **Firecrawl** | 1,000 pages/month free | Primary. Better extraction, and does LLM job extraction (tier 4). |
| **Jina Reader** (`r.jina.ai`) | Free, **no API key** | Fallback. Absorbs overflow. |

### Credit protection (four guards)

1. **Plain HTTP first** — anything resolvable free never touches a credit.
2. **Once per company** — result cached on the row.
3. **`ats_checked_at` + 7-day recheck** (`atsRecheckInterval`) — a company that
   came back with no ATS isn't re-scraped for a week. **Without this, every
   hourly tick re-scraped every ATS-less company: ~2,400 credits/month against
   a 1,000 free tier.**
4. **Budget switch** — at `FIRECRAWL_MONTHLY_BUDGET` (default 800), switches to
   Jina automatically. Also switches on any Firecrawl error.

Usage is tracked in `scrape_usages` (one row per month+provider) and logged on
every sync tick. Currently ~130/800 used.

**Env vars:** `FIRECRAWL_API_KEY` (set), `FIRECRAWL_MONTHLY_BUDGET` (optional),
`JINA_API_KEY` (optional, raises rate limit).

---

## 3b. Search: Exa (primary) + DuckDuckGo (fallback)

`ai-worker/main.py` → `_discovery_tools()`.

DuckDuckGo was the weakest link in the whole pipeline: it returned blog posts
and listicles *about* companies rather than the companies themselves, which is
why ~13 companies arrived with no usable careers URL at all.

**Exa's `category="company"` filter** returns company homepages directly.

```python
if os.getenv("EXA_API_KEY"):
    tools.append(ExaTools(category="company", num_results=10, text_length_limit=2000))
tools.append(DuckDuckGoTools())   # always present as keyless fallback
```

The agent also got explicit instructions: the careers URL is the single most
valuable field, return the company's own domain (never an aggregator or news
article), and skip anything unverifiable.

**Measured on one query** ("funded SaaS companies in Bangalore"): 5 of 6
companies came back with a careers URL, and the companies were real funded
businesses (Perfios, Plum, Jodo) rather than the random small ones DuckDuckGo
was surfacing. The first full run added **10 new companies in one cycle**,
against 1-3 before.

**DuckDuckGo is deliberately kept.** If the Exa key is missing or its monthly
credits run out, discovery degrades rather than stopping.

**Not added yet:** Tavily and Apify. Both were researched (§8) and both have
useful free tiers — Apify in particular has `compass/crawler-google-places`,
which gives Google Maps business data *without* needing a Google Cloud card.
Deliberately deferred: adding three search providers at once makes it
impossible to tell which one actually helped. Add them only if Exa proves
insufficient.

---

## 4. The UI

Matches the reference site's pattern, verified against it directly:

| | bangalorestartupmap | Ours |
|---|---|---|
| Header | "991 open roles across 95 companies" | "213 open roles across 19 companies" |
| Hiring filter | `?hiring=1` button | **"Hiring only" toggle, default ON** |
| Role facets | FIELD + LEVEL chips with counts | **same** — see below |
| Hiring ratio | 95/1045 = **9%** | 19/67 = **28%** |

**"Hiring only" defaults to ON.** Most companies aren't hiring at any moment,
so a directory full of empty cards is useless — browsing everything is opt-out,
not the default. Toggle flips to "All companies".

Companies are sorted **most-jobs-first**, then newest.

### Role facets (`services/job_facets.go`)

FIELD and LEVEL chips with live counts, derived from the job title and
department we already store — **no extra data, no LLM call, pure Go**.

```
FIELD  Data & AI 11 · Engineering 42 · Product 5 · Design 4 ·
       Sales & Marketing 47 · Operations 39 · Other 65
LEVEL  Fresher 8 · Junior 14 · Mid 1 · Senior 59 · Lead 23 · Unspecified 108
```

Clicking a chip filters the roles shown *inside* each company card and map
popup (`GET /api/companies/:id/jobs?field=…&level=…`), not the company list.

Two deliberate choices:

- **Bucket order matters.** First match wins, so `Data & AI` is checked before
  `Engineering` — otherwise "Data Engineer" would land in Engineering.
- **"Unspecified" is a real bucket, not a fallback bug.** Most job titles say
  nothing about seniority. Guessing "Mid" would be wrong more often than
  admitting we don't know — hence 108 of 213.

Other UI: grid ⇄ Leaflet map toggle, marker clustering (companies in the same
city share a geocoded point and would otherwise stack invisibly), filters,
click-to-expand job lists in both card and map popup, "N open" badges. Logos
use Google's public favicon endpoint — no hosting.

---

## 5. Deduplication

Two layers, because domain alone wasn't enough:

1. **By domain** — `ON CONFLICT (domain) DO NOTHING`.
2. **By normalized name** — `normalizeCompanyName()` strips parentheticals,
   legal suffixes and punctuation:

   ```
   "BYJU'S Exam Prep (Gradeup)"      -> "byjusexamprep"
   "Edunext Technologies Pvt. Ltd."  -> "edunext"
   ```

   The agent returned BYJU'S twice under different domains
   (`byjus.com` and `byjusexamprep.com`), so domain-only dedupe let both
   through. Names shorter than 4 normalized chars are skipped to avoid false
   merges on short names.

Jobs are deduped on `(company_id, url)`. For careers-page jobs without a
per-role link, the URL is `careersURL#<slugified-title>` so the unique index
doesn't collapse every job into one row.

---

## 6. Bugs found and fixed — they share a pattern

**Almost all were "silently does nothing" bugs.** Code looked correct, logs
looked fine, feature quietly did nothing.

| # | File | Issue → Fix |
|---|---|---|
| 6.1 | `job_service.go` | Greenhouse regex missed regional boards (`job-boards.**eu**.greenhouse.io`). Added optional region segment. |
| 6.2 | `job_service.go` | Periodic sync queried only `WHERE ats_type != ''`, so adding new ATS providers would have had **zero effect on existing companies**. Renamed to `SyncAllCompanyJobs`, now re-detects. |
| 6.3 | `company_service.go` | `RunDiscoveryRotation` did `return` on discovery error → `SyncAllCompanyJobs` never ran. One flaky search meant zero job refresh for the whole tick. Now independent. |
| 6.4 | `ai-worker/main.py` | `'str' object has no attribute 'model_dump'` — with tools enabled, Agno doesn't always return a parsed model. Raw JSON and markdown-fenced JSON are both normalised now. |
| 6.5 | `job_service.go` | `config.DB.Find()` error ignored → a failed query produced an empty slice and logged `"0 companies checked"` as if normal. |
| 6.6 | `company_service.go` | No timeout on the ai-worker call. A stuck worker held the startup sync for **~3 hours**. Now a 3-minute client timeout. |
| 6.7 | `repo_controller.go` | A pending-placeholder row wrote `""` into a `jsonb` column. Postgres rejects that — **every** analyze request would have failed. Fixed to `"null"`. |
| 6.8 | `CompanyMap.tsx` | Google's favicon endpoint returns a 16×16 globe **with a 404 status but a valid PNG body**, so `onError` never fired. Now also checks `naturalWidth < 32`. |
| 6.9 | `CompanyMap.tsx` | Map showed one pin — per-city geocoding gave every Delhi company identical coordinates. Fixed with clustering + auto-fit bounds. |
| 6.10 | `company_service.go` | Nominatim had **no rate limit** (their policy is 1 req/sec) — real IP-ban risk. Now a mutex-based 1.1s throttle. |
| 6.11 | `company_service.go` | Compound areas like `"Noida/Gurugram, Delhi NCR"` failed to geocode → no map pin. Now falls back to simpler forms. |

---

## 7. Verified vs unverified

### Verified against live boards
Swiggy 75 (SmartRecruiters) · Freshworks 100 (SmartRecruiters) · Ramp 138
(Ashby) · Meesho 48 (Lever) · Groww 5 (Greenhouse) · BrowserStack 32 (Workday)
· Keka multi-location parsing · Doceree 19 (careers-page LLM extraction).
Re-sync idempotent on all — no duplicate rows.

### Verified in the live directory
```
job sync: 46 companies checked, 3 newly detected, 125 open roles
scrape usage this month: map[firecrawl:90 jina:31]
```
Zypp Electric's Keka board was found **only by Firecrawl** — plain HTTP missed it.

### ⚠️ NOT verified
- **Workable.** Endpoint responds 200 and returns a well-formed *empty* array;
  the v3 POST endpoint returns `{"total":0,"results":[]}`. None of ~20 sampled
  slugs had active postings, so the parse path has never seen real data.
  Implemented per the documented shape — treat as unproven.
- **Careers-page LLM extraction is not hallucination-proof.** The prompt says
  "never invent one" and results are tagged `source: "careers-page"`, but
  nothing verifies each title exists on the page.

---

## 8. Alternatives evaluated and rejected

Documented so nobody re-researches these:

| Option | Verdict |
|---|---|
| **webclaw** (Rust, 2.3k★, Go SDK) | ❌ README: *"Set WEBCLAW_API_KEY to handle bot-protected and JavaScript-rendered pages"* — the self-hosted version doesn't render JS, which is exactly our need. |
| **Lightpanda** (Zig browser, 34k★) | ❌ Tested in Docker: **3MB idle RAM** (vs Chrome ~300MB) — impressive. But on our real test page it returned 28KB and **missed the Keka link that Firecrawl found in 87KB**. Incomplete web-API coverage. Releases still "nightly". Re-check in ~6 months. |
| **Crawl4AI** (78k★) | ❌ Runs on Playwright — same Chrome, same RAM. "Free" but expensive to host. |
| **TheirStack** | ❌ Free tier = **200 jobs/month**. Razorpay alone is 21. |
| **Darwinbox / Keka admin APIs** | ❌ Partner/customer-gated. (Keka's *public careers* endpoint works though — see §2.) |
| **Google Maps (Agno `GoogleMapTools`)** | ⏸️ Good for verification + exact lat/lng, but gives **no careers URL** — which is the actual bottleneck. Needs a card. Revisit later. |
| **Indeed / LinkedIn / Naukri APIs** | ❌ Indeed's Publisher API shut down 2023; LinkedIn is partner-gated; Naukri has no public API. |

**Checked what bangalorestartupmap actually uses:** ClickPost → Keka,
Ctruh → Keka, Bureau → Ashby. **Same ATS public APIs we use.** No Naukri, no
Wellfound, no Cutshort. Our approach is right; the gap is scale, not method.

---

## 9. What's next — priority order

1. **Retry failed extractions** (~1h). Classplus, Physics Wallah, Extramarks
   have careers URLs but returned 0 jobs. Worth a second pass with a tweaked
   prompt or longer wait — these are companies we *know* are hiring.
2. ~~**Follow "View Openings" links.**~~ **Shipped.** `findJobsListingLink`
   in `job_service.go` follows a careers page's link to the real listing page
   when the first page yields nothing. Note the guard that came with it: an
   education site linked to a *career-advice* article and the extraction
   returned 295 professions as "jobs", so `guidancePageRe` skips advice URLs
   and `careersPageResultLooksReal` rejects extractions that are too large or
   carry no location/department metadata at all.
3. **Direct Exa lookup for careers URLs from Go** (~45 min). Finding a careers
   page is a *lookup*, not a judgment call — a direct Exa call
   (`site:company.com careers`) skips the LLM tokens entirely. Cheaper than
   routing it through the agent, and would slot in as a new tier in
   `ResolveCareersURL`.
4. **More ATS platforms.** Gem (`jobs.gem.com`, seen on Hasura), Recruitee,
   Personio, Freshteam. Same pattern: one regex + one fetch function + one
   switch case.
5. **Tier 2/4 fetch the same page twice** — ~2 credits where 1 would do.
   Worth fixing when credit usage gets closer to budget (currently ~160/800).
6. **Tavily / Apify**, only if Exa proves insufficient. Apify's
   `compass/crawler-google-places` is the interesting one — Google Maps data
   without a Google Cloud card.

### Known gaps (not blocking)
- Companies are never re-enriched after first discovery — stage/description
  stay frozen at first-seen values. Jobs *are* re-synced.
- Filters are exact-match against a fixed dropdown; the agent occasionally
  emits values outside it (e.g. "Series D").
- `POST /api/companies/discover` is any-authenticated-user, not admin-only.
- `/health` returns a hardcoded `{"db":"connected"}` without pinging anything.
- Migrations: `002_companies.sql` documents the schema, but the live schema
  comes from GORM AutoMigrate. `jobs` and `scrape_usages` have no migration file.

---

## 10. Files, endpoints, dependencies

**Added:** `models/company.go`, `models/job.go`, `models/scrape_usage.go`,
`services/company_service.go`, `services/job_service.go`,
`services/scrape_service.go`, `services/job_facets.go`,
`controllers/company_controller.go`,
`supabase/migrations/002_companies.sql`, `frontend/src/pages/CompanyMap.tsx`

**Modified:** `main.go` (AutoMigrate + routes + cron), `ai-worker/main.py`,
`frontend/src/App.tsx`, `frontend/src/components/DashboardLayout.tsx`

| Method | Path | Auth | Notes |
|---|---|---|---|
| GET | `/api/companies` | public | filters `sector`/`stage`/`area`/`q`/**`hiring=1`**; returns `job_count` per company, `open_roles` total, and `facets` |
| GET | `/api/companies/:id` | public | |
| GET | `/api/companies/:id/jobs` | public | optional `field` / `level` facet filters |
| POST | `/api/companies/discover` | authenticated | manual trigger |

**Deps:** Go `robfig/cron/v3`; frontend `leaflet`, `react-leaflet`,
`react-leaflet-cluster`, `@types/leaflet`; Python `duckduckgo-search`, `exa-py`.

**Env vars:** `FIRECRAWL_API_KEY`, `EXA_API_KEY` (both set),
`FIRECRAWL_MONTHLY_BUDGET` and `JINA_API_KEY` (optional).

---

## 11. Running it locally

```bash
# ai-worker — MUST be 8001 (8000 collides with a Dockerized GlitchTip)
cd ai-worker && uvicorn main:app --host 0.0.0.0 --port 8001 --env-file ../.env

# backend — loads ../.env; AutoMigrate creates all tables on boot
cd backend-go && go run main.go

# frontend
cd frontend && npm run dev
```

```bash
curl localhost:8080/health
curl "localhost:8080/api/companies?hiring=1"       # only companies with roles
curl localhost:8080/api/companies/<id>/jobs
grep "job sync:" backend-go/_run.log               # last sync summary
```

**Timing note:** a full sync of ~50 companies takes several minutes — each
undetected company does an HTTP fetch, possibly a render, and up to 7 API
probes. The startup sync runs in a goroutine, so the server is up immediately.
If a sync appears to hang, check `_run.log` before assuming it's stuck.

**Gotcha for one-off scripts:** if you reset `ats_checked_at` to force
re-detection, do it **before** loading the company rows — otherwise the
in-memory structs still carry the old timestamp and every company skips. This
cost a full debugging cycle.

---

## 12. Related docs

- `PROGRESS.md` — dated changelog
- `project docs/11_MVP_ROADMAP.md` — overall build order and phases
- `project docs/CAMPAIGN_PART1_BUILD_TO_LAUNCH.md` — Day 1-30 build-in-public posts
- `project docs/CAMPAIGN_PART2_POST_LAUNCH.md` — Day 31-46 post-launch posts

The campaign files mark ✅ shipped vs ⚠️ not built, so they double as a feature
checklist — each planned day maps to a roadmap item.
