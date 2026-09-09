# Progress Log

---

## 2026-09-09 — Discovery moved out of Go; Job Map roughly doubled

**602 → 1,189 companies. 8,387 → 15,704 jobs.** Two threads ran through the day:
the Job Map directory page, and where companies actually come from.

### The directory page: four bugs, all found by driving it

Audited `/directory` in a real browser rather than by reading the code.

Working already: Grid/2D/3D toggle, all six Tech Hub pills, search, sector and
stage dropdowns, hiring-only toggle, Load more, map clustering, stats strip.

Broken, and fixed:

1. **FIELD and LEVEL chips filtered nothing.** Clicking "Engineering 3210" lit
   the chip and left all 630 companies on screen. `field`/`level` never reached
   `buildURL()`, were missing from the effect's dependencies, and
   `/api/companies` did not accept them at all. Added them end to end: an
   `EXISTS` subquery in `ListCompanies` (opt-in, so the common no-facet path is
   still the single-table scan it was designed to be), matching
   `COALESCE(NULLIF(jobs.field,''),'Other')` exactly as `JobFacets` counts — the
   "Other" and "Unspecified" buckets exist only as that default, so any other
   spelling would match nothing while the chip advertised thousands. Verified:
   630 → 504 companies for Engineering, 360 for Engineering+Senior.

2. **An expanded card could not be closed.** The footer carrying the toggle got
   `hidden` when the card opened, so the only control that would collapse it
   disappeared — along with the Website link. Footer now stays; the button reads
   "Hide roles" with a rotated chevron.

3. **The badge disagreed with the list.** A card said "40 open" and then listed
   3, because the roles inside were filtered by the chips while the badge came
   from `companies.open_roles`. Under a facet the badge is now a correlated
   count of that bucket.

4. **404px of horizontal overflow on a phone.** `main` is `flex-1`, which
   defaults to `min-width:auto` — its content's intrinsic width. The tech-hub
   pill row is wider than 375px, so the whole page scrolled sideways instead of
   that one row scrolling inside its own `overflow-x-auto`. `min-w-0` on `main`
   fixed it; overflow went 404px → 0, desktop unchanged.

### Why discovery was slow — three findings, in the order they mattered

**Quoted phrases were halving Exa's yield.** The ATS-dorking query shape
(`site:jobs.lever.co "Bengaluru" "Software Engineer" India`) is a *Google*
technique. Measured on one slot: the dork form returned 47 results and 23
distinct companies; the same slot as a plain sentence returned 100 and 45,
including 28 companies the dork form never surfaced. Dropping `site:` alone
changed nothing — the quotes are what did the damage. Exa and the free keyword
engines now each get the syntax they actually read.

**Every "domain-specific" query was searching all 15 domains.** Exa's own docs
say not to put `site:` in the query and to use `includeDomains` instead. The
code did both — `site:X` in the text, and the full `boardSearchDomains` list in
the parameter. So the rotation's domain axis did nothing for Exa, and the same
well-linked companies dominated every tick. Scoped `includeDomains` to the one
host the slot targets; confirmed at the wire with a temporary log:
`includeDomains=[keka.com]`, not fifteen.

**The daily cap, not the cadence, was the throttle.** The directory went 743 →
744 in an hour with the cron at 4 minutes. The logs showed only the free engines
ticking: `tavily … today: 31/30` — both paid sources had met their daily target
by early afternoon and returned silently for the rest of the day. Raised, and
the rotation resumed immediately (`today: 74/400`). Also removed a fixed
five-company-per-tick storage cap that discarded results already paid for, and
raised `boardResultsPerQuery` 25 → 100 (Exa's own ceiling; the same one credit
buys either).

### Search left Go

Tested SearXNG self-hosted on Railway behind a residential proxy against the
paid API: **51 distinct companies from one query walked five pages, versus 45
for one Exa call.** Free, and deeper. The catch is pacing — five pages back to
back put every upstream engine into CAPTCHA within ~13 requests and the instance
returned nothing for minutes; the same five pages 6–10s apart tripped nothing.

So discovery is now `scripts/discover_companies.py`, and Go no longer searches.

Removed from Go: `RunDiscoveryRotation`, `RunFreeDiscoveryRotation`,
`runRotationSource`, `DiscoverFromBoards`, `DiscoverFromBoardsManual`,
`discoverFromBoards`, `boardHitsFor`, `RunMultiCityDiscovery`, the seed
query/city/role tables, daily targets, the scheduler reserve, `mayStartLookup`,
both cron schedules, the startup discovery run, `POST /api/admin/run-discovery`,
`POST /api/companies/discover` and its rate limiter, and eight tests.
`board_discovery.go` went 1,688 → ~1,000 lines.

Kept in Go, deliberately — this is the half that was working:

- **`admitBoard`**, extracted from the old rotation loop so one function is the
  only place a company is judged. Fund and marketplace names, a board we already
  hold, the board's own API answering at all, the 2,000-role ceiling, hiring in
  India, duplicate detection by name and by domain.
- **`ImportBoards`** and `POST /api/admin/import-boards`, the way boards found
  anywhere else get in. No metered lookup is allowed on this path, so an import
  of hundreds of boards is free; a company whose site cannot be named for free
  is reported as `unnamed` and left alone.
- Hourly job sync, which is what actually produces the jobs — 1,189 companies'
  boards re-read on a rotation, and the reason the role count moved as much as
  it did.

The split: **Python finds companies, Go decides which are real and keeps their
jobs fresh.**

### Two guards added after looking at what came in

Checked the companies the new path stored rather than assuming they were fine.
Real ones were arriving (MongoDB, Altisource, QAD, Xoxoday, Softobiz) — and so
were these:

- **Staffing firms that never say so.** "APAC Talent Attraction" is Cielo, an
  RPO, stored as an employer with 98 roles belonging to its clients. The guard
  had `staffing|recruitment|placements` and no word for this. Added the
  business-model shapes: `talent(attraction|acquisition|partners|network|pool)`,
  `rpo`, `bpo`, `outsourc`, `workforce`, `headhunt`. Deliberately *not*
  "consulting" or "solutions" — Capco, Deloitte and QAD are consultancies and
  are exactly who this directory is for; a test pins those ten names as allowed.
- **Re-registration numbers defeated dedupe.** `adeebaeservicespvtltd` and
  `adeebaeservicespvtltd3` were two rows for one business under two domains. An
  ATS appends a number when a slug is taken, so `normalizeCompanyName` now
  strips a trailing 1–2 digits when ≥4 characters survive. The threshold started
  at 6, which read well against the long slug that prompted it and then failed
  `acme2` and `asapp-2` — where the real duplicates were. The test caught that,
  not a re-read.

### Also today

- `.seen_boards.json` removed. A local memory of boards already sent had grown
  to 1,827 entries and was skipping all of them permanently — including ones the
  directory had only *temporarily* turned away. Go already knows what it holds
  and says so in `rejected`; a second memory can only disagree, and when it does
  it hides boards.
- Exa removed from the script entirely. SearXNG is the only source now.
- Import is batched at 25. One 110-board push stored its companies and *still*
  reported a dropped connection, because the work outran the server's 120s
  `WriteTimeout` — indistinguishable from a failure.
- `.claude/skills/verify/SKILL.md` added: how to build, launch and drive this
  service, including the AutoMigrate wait and the rotation-index gotcha.
- This file rewritten. It had 2,364 interleaved NUL bytes (git treated it as
  binary) and duplicate sections — 2026-08-29 appeared five times.

### Known gaps, not fixed

- **Wrong websites on some companies**: `webleetechnologies` → `jobdials.com`,
  `nreach` → `xoxoday.com`. The name/domain disagreement is visible and unchecked.
- **Several companies sit at exactly 100 roles**, which looks like a board API
  page limit being stored as a true count rather than a real number.
- 82% of companies have `sector = Unknown` and 89% `stage = Unknown`, so those
  two dropdowns work but reach very little.
- 1,769 Firecrawl and 1,084 Jina failures logged, mostly rate limits on the
  careers-page rendering tier.

### Current pipeline

```
every 2 min (Windows scheduled task "NeuroFIQ-Discovery")
  └─ discover_companies.py
       ├─ slot from the clock: provider × city × role (960 slots, ~32h to repeat)
       ├─ SearXNG (self-hosted, residential proxy) — 4 pages, 6–10s apart
       ├─ slugs out of the result URLs
       └─ POST /api/admin/import-boards  (batches of 25)
            └─ Go admitBoard() — every guard
                 └─ company + its roles stored
                      └─ hourly job sync keeps the roles current
```

---

## Before 2026-09-09 — condensed

The detail below was a dated log; it is summarised by theme because the dates
had duplicated and reordered themselves.

### The product

An AI interviewer that reads a candidate's real GitHub repository and questions
them on their own architecture, plus the **Job Map** — a self-maintaining
directory of companies and their real open roles. Go orchestrator (Postgres,
OAuth, billing, rate limiting, repo extraction), Python FastAPI + Agno for the
LLM with no database access by rule, React 19 + Vite + Tailwind v4 front end.

### Job Map, how it got here

- Started as an LLM asking which companies exist, then hunting for a careers
  page on each answer. Two stacked guesses; mostly returned 2-person shops that
  do not hire. That agent is gone.
- Replaced by **board-first discovery**: every hiring company puts roles on a
  public ATS board, and the slug in `jobs.lever.co/Sprinto` is the same slug the
  board's API takes. One search yields a company provably hiring with its roles
  one free call away, and no model anywhere in the path.
- **Ten ATS readers** on official public JSON APIs: Greenhouse, Lever,
  SmartRecruiters, Ashby, Workable, Keka, Darwinbox, Workday, Recruitee,
  Freshteam, Personio, Gem. Keka and Darwinbox matter most for Indian employers.
  Darwinbox needs browser headers or it answers with a bot-check page.
- **Roles, cheapest first**: known board → its JSON API; else resolve
  `/careers`; else regex the page for an embedded board link; else link-scan for
  per-role postings; else a rendered read (Jina, then Firecrawl); else LLM
  extraction. Jina runs before Firecrawl everywhere — when the paid one went
  first, an unset key took the whole free path down with it.
- **Common Crawl harvest** was built and later removed: one index yielded 13,501
  slugs across eight providers for ~20 requests and no metered call. Removed in
  favour of board search, though the arithmetic it recorded is what proved the
  point later — 2.02 searches per company stored caps a month at ~400 companies
  however fast the cron runs.

### Guards, each one bought with a real failure

- An extraction once returned 295 "jobs" that were an alphabetical list of
  professions from a careers-advice article. Three separate guards now stand in
  that shape's way, and they are not interchangeable.
- Jobgether, a marketplace on a Lever board, put 4,440 roles into the directory
  under one company name — two thirds of everything stored. Those roles are
  real; they belong to several hundred other employers.
- `maxBoardRoles` was 400 and threw out Paytm Payments at 840 and WPP Media at
  1,074. Between ~500 and 1,500 a role count does not separate an aggregator
  from a large employer, so the ceiling sits above any real employer instead.
- **An empty read is not "not hiring".** An ATS answers 200 with an empty array
  while it is being reconfigured. Deleting a company's roles on the first empty
  read made a hiring company look shut and flipped it back an hour later.
  Listings now survive one empty read and clear on the second.
- **A board is only accepted with evidence** — the company linked to it, or a
  search returned it. Never from guessing a slug off a name: `jobs.lever.co/cred`
  is CreditVidya, not CRED.
- Vendor demo tenants (`salesdemo.keka.com`, 82 postings, one titled "HR Manager
  (Sumit)") and talent-pool postings ("Be Part of our Talent Community") both
  look exactly like real listings and needed their own rules.
- `looksIndian` learned all 36 states and UTs — boards write "Mumbai MH" — plus
  a foreign-marker rule, because "Bangalore, Mexico" is a real row.

### The interview product

- **The extractor was blind to most languages.** `processZip` scored files by
  suffix on `.go/.py/.ts/.js` — and `.tsx` does not end in `.ts`. A React +
  TypeScript repo contributed zero source files and the interview was generated
  from `package.json` alone; Java, Rust, Ruby, C#, Kotlin, Swift, PHP and C++
  were dropped entirely. The product's central claim was silently failing for
  most of GitHub. Replaced with a 50-extension table plus manifest/entrypoint
  handling. A `break` that should have been `continue` was also exiting the
  budget loop with zero snippets whenever one large file sorted first.
- Live-coding interview studio: Monaco editor, tab-switch monitor, PiP camera.
- Audio moved off the browser's `SpeechRecognition` onto a Go WebSocket gateway
  streaming WebM to Deepgram Nova-2, with a Go ↔ Python gRPC pipeline behind it.
- LLM output is always a Pydantic schema via Agno's `output_schema` — never
  markdown fences parsed out of a response.

### Security and reliability

- Ten-finding audit, all fixed: OAuth CSRF state was a hardcoded literal, now
  crypto/rand and session-bound; `INTERNAL_SECRET` fell back to a source-visible
  default, now fails closed; a TOCTOU race let users exceed the 3-repo limit,
  now a per-user Postgres advisory lock verified with a 6-way concurrency test;
  a background goroutine had no `recover()`, so one bad repo could kill the
  process for every user.
- Every background goroutine carries its own `recover()` — Gin's `Recovery()`
  does not cover goroutines you spawn.
- Per-IP rate limiting, `HttpOnly`/`Secure`/`SameSite=Lax` cookies, and
  environment-variable URLs throughout.

### Directory and front end

- Grid / 2D Leaflet / 3D MapLibre views, tech-hub switcher, sub-hub geocoding
  across Indian tech clusters, marker clustering, company drawer.
- Job seniority classification rebuilt on the Radford/Levels.fyi IC framework;
  reclassified 7,459 jobs and cut "Unspecified" from 2,812 to 1,517.
- Two-tier in-memory cache with `Cache-Control` headers: repeat directory
  requests drop from ~300ms to 2.4ms with zero database queries.
- `companies.open_roles` is a maintained counter, so the listing sorts and
  filters without aggregating a join on every request.

### Known gaps carried forward

- Filters are exact-match against a fixed dropdown.
- `jobs` and `scrape_usages` have no migration file and exist only via
  AutoMigrate.
- Board discovery stores no sector or stage, and companies are never re-enriched
  after first discovery — stage and description stay frozen at first-seen values.
