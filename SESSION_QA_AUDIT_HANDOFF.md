# SESSION_QA_AUDIT_HANDOFF.md

Pre-launch QA audit of NeuroFIQ, run 4 September 2026 with Playwright against the
live Supabase database. **31 findings, 8 of them launch blockers.**

**Update, 5 September:** a follow-up pass fixed and verified live 10 of the 31 —
**B2, B3, B5, B6, B7, B8, H1, H5, H8, M3** — in commits `0d3b009` and `afa871f`.
Each has a ✅ marker below with the commit and what was verified. Two items were
deliberately *not* done, with the reason recorded in place: **M5** (needs a
`main.go` edit that collided with concurrent work from another session/tool —
see the note at the top of that finding) and **M9** (the report's suggested fix
turns out to be wrong: live testing showed the app genuinely reads private repos,
and classic GitHub OAuth has no read-only-private scope — this needs a GitHub App
migration, not a scope change). **B1 was already fixed by a concurrent session**
before this pass started (`/api/jobs` is registered in `main.go`) — verify it's
still there, don't re-add it.

Everything else — B4, H2, H3, H4, H6 (fixed only in Repositories.tsx; CompanyMap.tsx
and ReportsList.tsx still turn a failed fetch into "0 open roles" / "No reports
found"), H7, M1, M2, M4, M6, M7, M8, and all six Notes — is still open exactly as
described below.

Read `CLAUDE.md` first. Several findings below are the product breaking rules that
file already states; those are called out explicitly, because the fix is usually
"apply the rule you already wrote."

---

## 0. Read this before you run anything

### The trap that cost an hour

A `docker compose` stack for this project may be running and holding ports **8080**
and **5173**. Its backend connects to the **empty local Postgres container**, not
Supabase. Symptom: `/api/companies` returns `total: 0` while the database has
hundreds of companies. On Windows this is worse than it sounds — `curl localhost`
may resolve to IPv6 and hit your `go run`, while the browser resolves to IPv4 and
hits the container, so the two disagree and you chase a phantom bug.

**Always check first:**

```bash
docker ps --format "{{.Names}}\t{{.Ports}}"
netstat -ano | grep LISTENING | grep -E ":8080|:5173"
```

If `neurofiqaiinterview-backend-1` is up, either stop it or run on other ports.

### State this session left the machine in

- `neurofiqaiinterview-backend-1` and `neurofiqaiinterview-frontend-1` were
  **stopped** (`docker stop`) so GitHub OAuth could use 8080/5173.
  Restore with `docker start neurofiqaiinterview-backend-1 neurofiqaiinterview-frontend-1`.
- `neurofiqaiinterview-db-1` was left running and untouched.
- A `go run main.go` on :8080 and a `vite --port 5173` may still be running.
- ~24 screenshots sit in the repo root (`01-landing.png` … `24-interview.png`).
  They are gitignored by `.gitignore:51` (`/*.png`). Delete freely.
- No source file was modified. `git status` was clean apart from the
  pre-existing `OpenPostings` submodule change.

### Working setup

GitHub OAuth only works on **8080 + 5173**, because `.env` pins
`OAUTH_REDIRECT_URL=http://localhost:8080/auth/github/callback` and `FRONTEND_URL`
is unset (so `allowedOrigins()` falls back to `http://localhost:5173`). For
anything that does not need OAuth, use spare ports and leave the containers alone:

```bash
cd backend-go && PORT=8081 FRONTEND_URL=http://localhost:5174 go run main.go
cd frontend   && VITE_API_URL=http://localhost:8081 npx vite --port 5174 --strictPort
cd ai-worker  && uvicorn main:app --host 0.0.0.0 --port 8001 --env-file ../.env
```

Note cookies ignore ports: a session set by the :8081 backend is also sent to the
:8080 one, because both are host `localhost`. Log out between stacks or you will
think you are anonymous when you are not.

### Test account

`qa.tester.neurofiq@example.com` / `QaTest!2026pw` — email+password, onboarded,
no GitHub, no repos, no reports. Disposable; delete when done.

### Live data at time of audit

470 companies, 6,300 jobs, 8 users, 1 interview session.
Verify with `curl -s "localhost:8080/api/companies?page_size=1"`.

---

## 1. Blockers — must fix before launch

### B1 · `/api/jobs` was never registered → Find Jobs is dead

- `frontend/src/pages/JobsPortal.tsx:115` fetches `/api/jobs`.
- `backend-go/controllers/company_controller.go:106` has a complete
  `HandleGetGlobalJobs`. **Nothing routes to it.** `git log -S '"/api/jobs"'`
  returns nothing — it has never worked.

```
GET /api/jobs?page_size=50  → 404 page not found
DB at that moment           → 6,242 jobs / 462 hiring companies
Page shows                  → "0 Live Openings" / "No matching openings found"
```

**Fix:** add `r.GET("/api/jobs", controllers.HandleGetGlobalJobs)` in `main.go`
beside the other public company routes (near line 241). One line.

---

### B2 · ATS magic-link invite returns 401 for everyone, always

- `backend-go/auth/middleware.go:22` sets `c.Set("user_id", userID)` — a **string**.
- `backend-go/controllers/ats_controller.go:74` reads `c.Get("user")` and casts to
  `*models.User`. Nothing sets that key, so `exists` is always false.

Every other controller uses `c.MustGet("user_id").(string)` — this one file drifted.
The UI surfaces it as a native `alert("Unauthorized")`. This is the headline
feature of commit `adb6120` "B2B Enterprise Pivot".

**Fix:** read `user_id`, load the user, match the other ten controllers.

**While you are in that file:** line 98 does
`CompanyID: user.ID // For MVP, mapping recruiter ID as company ID`.
That writes a **user id into `jobs.company_id`**, the column the public Job Map
joins on. Shipping B2's fix without fixing line 98 will corrupt directory counts
the first time a recruiter creates an invite. `URL: "internal"` on the same struct
will also be seen by the dead-link pruner.

---

### B3 · Email/password users can never connect GitHub → core product unreachable

The only link to `/auth/github/login` is `frontend/src/pages/AuthChoice.tsx:77`,
which is unreachable once logged in. There is no "Connect GitHub" control anywhere
in the authenticated app. `github_connected` exists on the `User` type
(`context/AuthContext.tsx:18`) and is never rendered.

Email is the **default tab** on `/auth`. Such a user lands on `/repositories` and
sees only:

```
"No repositories found. Ensure you have granted GitHub access."
```

No button, no link, no next step.

**Fix:** add a Connect GitHub action to that empty state and to account settings.

---

### B4 · `docker-compose.yml` would deploy a broken, insecure stack

Supplies 3 of the ~22 env vars the backend reads, and one of those three
(`AI_GRPC_SERVER`) is read by no Go code.

| Problem | Consequence |
|---|---|
| `DATABASE_URL` → local `db` container | Deploy starts at 0 companies / 0 jobs (reproduced) |
| `APP_ENV` unset → `isProduction=false` | Cookie loses `Secure`; the "SESSION_SECRET is required in production" guard at `main.go:175` never fires, so sessions are signed with `"default_secret_for_local_dev"` — **forgeable** |
| No `GITHUB_CLIENT_ID` / `_SECRET` | OAuth cannot work |
| No `PYTHON_WORKER_URL` | `workerURL()` (`services/httputil.go:229`) defaults to `localhost:8001`, which inside the backend container is itself; the ai-worker service does not publish 8001 at all → every AI call fails |
| `POSTGRES_PASSWORD: password123` committed | secret in git |
| `5432:5432` and `50051:50051` published | Postgres and internal gRPC exposed on a server deploy |

Full env surface the backend reads (`grep -rhoE 'os\.Getenv\("[A-Z_]+"\)'` plus the
`envInt`/`envFloat` helpers and the search-key constants):

```
APP_ENV  DATABASE_URL  DEEPGRAM_API_KEY  EXA_API_KEY  EXA_MONTHLY_BUDGET
FIRECRAWL_API_KEY  FIRECRAWL_MONTHLY_BUDGET  FRONTEND_URL  GITHUB_CLIENT_ID
GITHUB_CLIENT_SECRET  INTERNAL_SECRET  JINA_API_KEY  OAUTH_REDIRECT_URL  PORT
PYTHON_WORKER_URL  RESEND_API_KEY  SESSION_SECRET  TAVILY_API_KEY
TAVILY_MONTHLY_BUDGET  TRUSTED_PROXIES  DB_MAX_OPEN_CONNS  DB_MAX_IDLE_CONNS
RATE_LIMIT_RPS  RATE_LIMIT_BURST
```

**Fix order:** `APP_ENV=production` and a real `SESSION_SECRET` first — everything
else is downtime, that one is a security hole. Then `DATABASE_URL`, the OAuth pair,
`PYTHON_WORKER_URL`, drop the published DB port, move secrets to the host.
Ship a `.env.example` listing all of the above; never real keys.

---

### B5 · No 404 route → unknown URLs render a blank white page

`frontend/src/App.tsx` has no catch-all. `/this-page-does-not-exist` returns an
empty `<body>` — no header, no nav, no message.

**Fix:** add `<Route path="*">`. The pattern already exists and is good:
`/r/:slug` with a bad slug shows "Report unavailable" plus a "Go to NeuroFIQ" link
(`pages/PublicReport.tsx`). Copy it.

---

### B6 · Repositories page overflows at every screen size

`frontend/src/pages/Repositories.tsx:137` renders one `w-6` (24 px) pill per free
analysis slot in a `flex gap-1` with no wrap and no overflow container. The
component defaults to `limit = 3` (line 29) but `/api/repos` returns
`analyses_limit: 100`.

```
quota strip width   2,796 px
1366 px viewport    185 px horizontal overflow
768  px viewport    527 px
390  px viewport    905 px
symptom             "Start Interview" clipped off-screen, page scrolls sideways
```

**Fix:** the pill metaphor only reads at single digits. Use a progress bar with a
count, or cap the pills and show "+N". See also N4 — 100 free analyses is itself a
decision worth revisiting.

---

### B7 · `/ws/interview` is unauthenticated and proxies straight to Deepgram

Both halves are marked in the code as temporary:

- `main.go:255` — `// This sits outside auth for now to allow testing, but should be secured in production.`
- `controllers/ws_controller.go:24` — `return true // Allow all for dev`

Verified live:

```
handshake, no cookie, Origin: evil-site  → 403   (CORS blocks browsers)
handshake, no cookie, no Origin header   → 101 Switching Protocols
handshake, no cookie, Origin: localhost  → 101 Switching Protocols
```

The browser attack path is closed by the CORS middleware — accidentally, not by
design. Any non-browser client (script, bot, anything that reaches the host) opens
a socket and streams audio you pay Deepgram to transcribe.

**Fix:** move the route inside the `api` group (or apply `AuthMiddleware` to it),
and make `CheckOrigin` compare against the same `allowedOrigins()` list CORS uses.

---

### B8 · Email signup + GitHub login collide with no linking path

`backend-go/auth/oauth.go:139` looks the user up by `github_id` only. No email
lookup, no account linking. `public.users` has `UNIQUE INDEX uni_users_email (email)`.

```
registered by email, then "Continue with GitHub"
  → github_id miss → config.DB.Create(&user) with the same email
  → unique violation → 500 "Failed to create user", no explanation

GitHub first, then register with that email
  → 409 "An account with this email already exists"   (verified live)
```

Both buttons are on the same screen, so users will try both. One direction gives a
clear message; the other gives a 500 and — because of B3 — no other way to attach
GitHub to that account.

**Fix:** when `github_id` misses, look up by the **verified** GitHub primary email
and attach the identity to that row instead of creating a second one.

---

## 2. High

### H1 · Identity read only from `github_username`

- `components/DashboardLayout.tsx:53` — `user?.github_username || 'Candidate'`
- `pages/Dashboard.tsx:44` — `Welcome back, {user?.github_username || 'there'}`

`full_name` is collected at signup, stored, never displayed. Same layout wraps the
**public** `/jobs` and `/directory` routes, so logged-out visitors get a fabricated
account too.

```
signed in as "QA Tester"  → sidebar "Candidate", header "Welcome back, there."
logged out on /jobs       → "Candidate / No email on file" + a "Sign out" button
                          → Dashboard link silently bounces to /auth
```

GitHub users are unaffected (real name and avatar render correctly) — this hits
every email signup.

**Fix:** fall back `full_name → github_username → 'there'`, and give
`DashboardLayout` a signed-out state with a Sign in CTA instead of a fake profile.

---

### H2 · Job Map unusable on a phone

At 390 px `/directory` scrolls sideways by 360 px. Tech-hub pills, the sector and
stage dropdowns and the FIELD/LEVEL facet rows all run past the right edge with
labels cut mid-word. Clean at 768 px, so it is phone-specific.

```
390 px:  /directory     scrollWidth 735 px  (+360)
         /repositories  scrollWidth 1280 px (+905)   ← B6
         everything else clean
```

**Fix:** give each filter row its own `overflow-x: auto` track; let the dropdowns
wrap. The page body must never scroll sideways.

---

### H3 · Password guessing limited only by the generic IP throttle

`/auth/login` sits behind the same 10 rps / burst 30 per-IP bucket as everything
else (`main.go:221`). No per-account counter, no lockout, no backoff.

```
200 parallel POST /auth/login with wrong passwords
  157 → 401   (reached the password check)
   43 → 429
```

Login itself correctly returns an identical message for a wrong password and an
unknown account — but `/auth/register` returns 409 "An account with this email
already exists", which hands an attacker the valid-email list.

**Fix:** per-account failure counter with backoff.

---

### H4 · The whole company directory is sent to Google on every page view

Each company card requests its logo from Google's favicon service with the
company's domain in the query string. Loading `/directory` hands a third party the
complete proprietary list the Job Map pipeline exists to build, and produces 25+
console 404s.

```
GET https://t1.gstatic.com/faviconV2?...&url=http://gokwik.co&size=128
GET https://t3.gstatic.com/faviconV2?...&url=http://berryworks.ai&size=128
… one per visible company
```

**Fix:** fetch and cache icons server-side, or fall back to the initial-letter
`Avatar` the app already has (`DashboardLayout.tsx:6`). Also removes a hard
third-party dependency from the main directory page.

---

### H5 · Job Radar scores a profile it never read

No client-side URL validation. Garbage input is accepted, spends a **metered
scrape** (the endpoint is in the `paid` group), and on failure the page still
renders the full result layout with a large red **0 / 100 — "Low Visibility"**
gauge. A user reads that as a score, not a failure.

```
input "not a url at all"
  → scrape spent
  → "Scraping Failed" banner
  → alongside: ATS OPTIMIZATION SCORE 0/100 "Low Visibility"
```

**Fix:** validate before submitting; on a failed fetch suppress the gauge entirely.
A score is a claim about the profile — you do not have one.

---

### H6 · A failed API call renders as a confident zero, not an error

Simulated the API being unreachable (deploy restart, DB blip):

```
/directory     → "0 open roles across 0 startups in India"
                 "0 Startups Plotted"          no error shown
/reports       → "No reports found."           no error shown
/repositories  → "Failed to fetch" (raw browser string) plus
                 "No repositories found. Ensure you have granted GitHub access."
                 ← wrong cause
```

This is the frontend making exactly the mistake `CLAUDE.md` says not to make:
*"An empty read is not the same as 'not hiring'."* `applySyncedJobs` keeps listings
alive through one empty read for that reason. The same discipline is not applied to
the user, so a thirty-second restart shows visitors an empty job board.

**Fix:** distinguish "request failed" from "response was empty" in every fetch;
render a retry state for the first.

---

### H7 · Freshness counters measure your sync, not the job market

```
total rows              6,300
created last 24h        2,564  (41%)   → shown as "+2564 fresh postings"
created last 7d         6,290  (99.8%) → shown as "6290 added this week"
oldest row in table     2026-08-27     (8 days)
distinct creation days  8
```

`jobs.created_at` records when the sync wrote the row. No board gains 41% new roles
in a day. The page also promises "Zero reposts, zero ghost listings" while 199 rows
(3.2%) are the same `(company_id, title, location)` under different URLs.

**Fix:** store the posting date the ATS reports and derive freshness from it. Where
a board gives none, show "first seen" and label it as such — the honest version is
still a good signal.

---

### H8 · Scraped text injected as raw HTML on the 3D map

`frontend/src/components/MapLibreCompanyMap.tsx` builds pins and popups with
`innerHTML` and interpolates unescaped values, including into an attribute and an
`href`:

```
line 150   alt="${c.name}"                ← attribute, unescaped
line 246   ${j.title} ${j.department}     ← element HTML, unescaped
line 256   href="${j.url}"                ← unescaped URL
```

Every one of those arrives from a third party: ATS APIs, careers pages, Common
Crawl, LLM extraction.

Live data today: 24 job titles and 1 company name contain quote characters
("Founder's Office", "BYJU'S", a title beginning with `"`). **No HTML tags present**
— so this is latent, not exploited. But creating a Greenhouse or Lever board is
free and the discovery pipeline is built to find exactly those; a job title is all
it takes. This is the only file in the app that bypasses React's escaping —
everything else is safe.

**Fix:** escape on the way in, or build these nodes with `createElement` +
`textContent`. Reject non-`http(s)` values before they reach an `href`.

---

## 3. Medium

| # | Finding | Where |
|---|---|---|
| **M1** | Directory data quality. Of 465 listable companies: **218 (47%)** show a raw ATS slug as the display name (`wppproduction`, `globalhealthcareexchangeinc`); **383 (82%)** no sector; **407 (88%)** no stage; **336 (72%)** no description; 24 have a deep-link website. Worst rows: `wppproduction → https://maps.app.goo.gl/JVxLC…` (35 roles, top listing), `Shield AI → http://bit.ly/shieldai_lever_homepage`, `globalhealthcareexchangeinc → a privacy-notice URL`, and **`Balbix → https://orbailix.com/`** — the wrong company's site, the exact failure `CLAUDE.md` warns about. `isAggregatorHost` rejects neither `maps.app.goo.gl` nor shorteners, and nothing rejects a URL with a path. Raw-slug naming also makes legitimately distinct entities look like duplicates (`paytm` and `paytmpayments` sit at #1 and #2). | `board_discovery.go`, `company_service.go` |
| **M2** | Find Jobs hero claims "24,000+ company ATS hiring boards"; the counter says "across 200 verified startups", hardcoded. Real figure: 470 companies. Three numbers on one screen, none agreeing. `/api/companies/stats` already returns the truth. | `pages/JobsPortal.tsx` |
| **M3** | "Select at least 2" technologies is enforced nowhere. The Continue handler validates only `step === 1 && !fullName.trim()`. Verified: advancing to Step 3 with **zero** selected, and `POST /api/user/onboarding {"tech_stack":""}` → 200. That field feeds question tailoring. | `pages/Onboarding.tsx:360`, `controllers` onboarding |
| **M4** | Six native `alert()` dialogs used for errors, including mic-denied mid-interview. | `Dashboard.tsx:152,172,175` · `InterviewSession.tsx:258` · `AuthChoice.tsx:74` · `UnifiedSearchCapsule.tsx:55` |
| **M5** | No security response headers at all — no HSTS, `X-Content-Type-Options`, `X-Frame-Options`, `Referrer-Policy`, CSP. Public `/r/:slug` report pages can be framed by anyone. | `main.go`, beside CORS and the body limit |
| **M6** | One 1.59 MB JS bundle (428 KB gzip), no code splitting. The landing page downloads MapLibre, Leaflet, the clustering plugin and Monaco. Mobile-first Indian audience. | `vite.config.ts`, route-level `React.lazy` |
| **M7** | Form inputs have no programmatic labels anywhere — labels are sibling `<div>`s, not `<label for>`. `/auth` has no `<h1>` and its password field lacks `autocomplete="current-password"` (Chrome warns). Several pages carry two `<h1>`s. Images all have alt text. | all form pages |
| **M8** | Mobile nav drawer is mouse-only: hamburger has **no accessible name**, Escape does not close, focus never moves into the drawer and is not trapped, no `aria-expanded`, no `role`/`aria-modal`. This is the primary navigation on phones. | `components/DashboardLayout.tsx:61-83` |
| **M9** | GitHub sign-in requests scope **`repo`** — full read *and write* control of every private repository, shown on the consent screen as "Full control of private repositories". The product only reads code. Hurts conversion and makes a breach far more costly. | `auth/oauth.go:30` |

---

## 4. Notes

- **N1** `linkedin_url` stored unvalidated — `javascript:alert(document.cookie)` is
  accepted and returned by `/auth/me`. Nothing renders it as a link today, so not
  exploitable; LinkedIn Optimizer would. Reject non-`http(s)` on write.
- **N2** Error states with no way forward: `"Error: Report not found"`,
  `"Error: invalid repository name"` as bare red text, no link back. The public
  report and invite pages do this well — apply the same pattern inside the app.
- **N3** Three unrelated visual themes: light paper app, near-black fuchsia/indigo
  Job Radar, dark-navy invite landing. A candidate arriving on an invite link
  crosses two in two clicks.
- **N4** `analyses_limit: 100` per free account is a lot of uncontrolled LLM spend
  for a codebase this cost-conscious elsewhere — and it is what breaks B6. Cold
  start is slow: `/api/companies/stats` **11.57 s** cold vs 0.019 s warm;
  `/api/companies` 3.25 s cold vs 0.053 s. Warm it on boot.
- **N5** Eight `target="_blank"` links without `rel="noopener noreferrer"`, all
  pointing at scraped URLs. Modern browsers imply `noopener`, so this is cheap
  insurance rather than a live risk. `CompanyDrawer.tsx:109,119` ·
  `CompanyJobList.tsx:104` · `JobListingCard.tsx:146` · `LeafletCompanyMap.tsx:166`
  · `CompanyMap.tsx:220,595,614`
- **N6** After leaving the Job Map — one tab, on `/interview` —
  `/api/companies/stats` kept being fetched **twice a minute indefinitely**, ~1 s of
  DB work each. `CompanyMap.tsx:330` *does* clear its interval on unmount, so this
  may be a dev-only artifact of StrictMode double-mount plus Vite HMR (which would
  also explain the doubling). **Confirm against a production build before treating
  it as a leak.**

---

## 5. Verified working — do not re-test

- **SQL injection** — parameterized. Injection through `q`, `sector`, `stage`
  returned zero rows, table intact.
- **Endpoint auth** — all ten protected routes 401 unauthenticated. No bypass.
- **Report access control** — reading *and* sharing another user's session both
  return 404, which also avoids confirming existence. No IDOR.
- **Session cookie** — HttpOnly, SameSite=Lax, encrypted store, `Secure` correctly
  gated on `isProduction`, `SESSION_SECRET` required in production. Flipping a
  single character → 401.
- **Sign out** — invalidates server-side, redirects home, cookie unreadable from JS.
- **Deep-link return** — `/reports` while logged out remembers the target and lands
  back on it after sign-in, not `/dashboard`. The `ProtectedRoute` StrictMode
  guard works.
- **Pagination clamping** — `page_size` 501 / 99999 / −5 / `abc` all clamp to 24.
- **Registration validation** — enforced client *and* server side; 8-char minimum
  and email format rejected at the API when the form is bypassed.
- **Filter correctness** — an unknown sector returns 0, not everything.
  "Series H / Unacademy" checked out as real data, not a parse artifact.
- **GitHub OAuth** — end to end with a real account. CSRF `state` is crypto-random,
  session-stored, verified on callback, deleted after use (`oauth.go:37,57,73`).
  Repos, languages, descriptions, PRIVATE badges all render.
- **Repo analysis** — returns real detected patterns ("Monorepo with kits",
  "Kit-based modularization") and a complexity rating.
- **LLM outage handling** — the best error path in the product: 502, a log line
  carrying user + repo, and a plain-language UI message. Do not "improve" it.
- **Job Map grid view** — 470 companies, 6,300 roles, working facets, clustering,
  hub filters, real logos.
- **Cross-origin WebSocket** — blocked by CORS (closes B7's browser path only).
- **Build health** — `go build ./...`, `go vet ./...` and `npm run build` all clean.
  No secrets tracked; `.env` is gitignored.

---

## 6. Still untested

- **Interview loop end to end** — questions, answering, scoring, report generation.
  Blocked here: the LLM key in `.env` is dead.

  ```
  GET /api/interviews/questions?repo_full_name=ToufiqQureshi/AgentKit → 502
  worker: {"detail":"Authentication Fails, Your api key: ****d50f is invalid"}
  ```

  Everything up to that point works. Put a valid key in `.env`, restart the worker
  (`--env-file ../.env`, port **8001** not 8000), and resume from
  `/analyze/ToufiqQureshi%2FAgentKit`.
- **Voice mode / Deepgram** transcription.
- **Camera proctoring.**
- **Transactional email** (Resend).
- **Billing / plan upgrades.**
- **Cross-browser** — Chromium only.

---

## 7. Suggested order

1. **B4 security half first** — `APP_ENV=production` + a real `SESSION_SECRET`.
   Everything else on this list is downtime; that one is a hole.
2. **B1, B2, B5** — three near-one-line fixes that revive two features and stop the
   blank-page 404. Fix `ats_controller.go:98` in the same pass as B2.
3. **B3 + B8 together** — they are the same story: an email signup can neither
   reach GitHub nor be linked to it.
4. **B7**, then **H8** — the two security items with an attacker-controlled input.
5. **B6, H2** — layout, both visible in any screenshot.
6. **H6, H7** — honesty about failure and freshness. Both are `CLAUDE.md` rules the
   frontend has not been held to.
7. The rest.

Fix the pipeline, never the row — M1's bad websites are a guard problem in
`isAggregatorHost`, not something to clean up by hand.
