# NeuroFIQ

**An AI interviewer that reads your actual GitHub repository and interviews you on your own code.**

Not LeetCode. Not trivia. It downloads your repo, understands how you built it,
and asks the questions a Principal Engineer would ask about *your* architecture
decisions — then scores the answers and tells you what a stronger one looks like.

Plus a **Job Map**: an automatically-maintained directory of startups and their
real open roles, pulled live from company job boards.

---

## Why

Tech hiring has a measurement problem. We say we want engineers who can design
systems and reason about trade-offs — then test them on whiteboard puzzles they
will never write again.

The best signal already exists. It's sitting in the candidate's GitHub: the
architecture decisions, the trade-offs, the shortcuts taken at 2am.

**Example question it generated from a real repo:**

> "In the OpenWA repo, you implement a multi-engine adapter pattern supporting
> both Baileys and whatsapp-web.js. How do you abstract the differences between
> these libraries to maintain a consistent internal API, and what breaking
> changes have you hit during version upgrades?"

You can't bluff that. It's your code.

---

## How it works

```
1. Sign in with GitHub          OAuth, CSRF-protected, session cookies
2. Pick a repository            ETag-cached repo list (304s cost zero API quota)
3. Analysis                     Repo streamed into memory — never touches disk.
                                A scoring pass keeps only the architecturally
                                significant files, capped at 60k characters.
4. Interview                    5 questions built from your actual code
                                snippets. Text or voice ($0 — browser Web
                                Speech API, no streaming vendor).
5. Report                       0-10 on correctness, depth, clarity and
                                trade-off awareness — plus, per question,
                                what a strong answer would have covered.
```

---

## Architecture

Two backend services, split on a deliberate boundary.

```
┌─────────────┐      ┌──────────────────┐      ┌──────────────────┐
│  React SPA  │─────▶│   Go orchestrator │─────▶│  Python AI worker │
│ Vite + TW   │      │                   │      │                   │
└─────────────┘      │ • Postgres        │ JSON │ • Agno + DeepSeek │
                     │ • OAuth/sessions  │ ────▶│ • structured out  │
                     │ • billing limits  │      │ • STATELESS       │
                     │ • rate limiting   │      │   (zero DB access)│
                     │ • repo extraction │      └──────────────────┘
                     └──────────────────┘
                              │
                         ┌────▼────┐
                         │ Postgres│  (Supabase — managed PG only,
                         └─────────┘   no BaaS SDKs, fully portable)
```

**Go** owns everything stateful. Goroutines absorb concurrent repo downloads
and extraction without a thread pool to tune.

**Python** owns exactly one thing: talking to the LLM. It has **zero database
access** — by rule, not by accident. That makes it a pure function
(`f(code) = analysis`), so scaling it is just raising the container count.

**Why the split:** Python is bad at high-concurrency I/O; Go has no AI
ecosystem. Forcing either to do both bakes a permanent weakness into your
hottest path.

---

## The Job Map

A self-maintaining directory of startups and their real open roles. No manual
data entry — an hourly cron rotates through 360 generated search queries
(12 cities × 10 sectors × 3 phrasings).

### Finding real jobs — four tiers, cheapest first

The insight the whole feature rests on:

> A careers page has to load its jobs from **somewhere public** — because
> visitors aren't logged in. So the structured data already exists.

| Tier | What | Cost |
|---|---|---|
| 0 | Resolve the careers URL if the agent didn't give one (`/careers`, `/jobs`…) | free |
| 1 | Plain HTTP fetch → regex for an embedded ATS board link | free |
| 2 | Hosted render (Firecrawl → Jina fallback) for JS-built pages | 1 credit |
| 3 | Guess the board slug from the company name, verify against the real API | free |
| 4 | No ATS at all → LLM extraction straight off the careers page | 1 credit |

Detection result is cached per company, so it runs **once**, not per sync.

**Supported job boards (7):** Greenhouse · Lever · SmartRecruiters · Ashby ·
Workable · Keka · Workday — all via their official public JSON APIs. No
scraping, no keys, no hallucination.

**Search:** Exa (`category="company"`) with DuckDuckGo as a keyless fallback,
so discovery degrades rather than stopping if credits run out.

### Cost control

Free tiers only. Four guards keep it that way:

1. Free paths run first — anything resolvable without a credit never spends one
2. Detection is cached per company
3. Companies with no board aren't re-checked for 7 days
4. A monthly budget switches to the free provider before the paid tier runs out

Usage is tracked per month per provider and logged on every sync.

---

## Stack

| Layer | Choice | Why |
|---|---|---|
| Orchestrator | **Go** (Gin, GORM) | Goroutines; single static binary |
| AI worker | **Python** (FastAPI, Agno) | The AI ecosystem lives here |
| Model | **DeepSeek** | GPT-4-class coding output at a fraction of the cost |
| Database | **Postgres** (Supabase) | Managed PG only — no BaaS lock-in |
| Frontend | **React + Vite + Tailwind v4** | Authenticated SPA; no SSR to pay for |
| Maps | **Leaflet + OpenStreetMap** | $0 in Maps API fees |

---

## Running locally

**Prerequisites:** Go 1.21+, Python 3.11+, Node 18+, a Postgres database.

```bash
cp .env.example .env      # fill in the values
```

```bash
# AI worker — port 8001 (8000 commonly collides with other local services)
cd ai-worker
pip install -r requirements.txt
uvicorn main:app --host 0.0.0.0 --port 8001 --env-file ../.env

# Go backend — creates its tables on boot
cd backend-go
go run main.go

# Frontend
cd frontend
npm install && npm run dev
```

Open http://localhost:5173

```bash
curl localhost:8080/health
curl "localhost:8080/api/companies?hiring=1"   # public — companies with open roles
```

---

## Engineering notes

Some decisions worth knowing about, and the bugs behind them:

- **The repo never touches disk.** ZIPs stream straight into memory via
  `archive/zip` + `bytes.Reader`. No temp files, no IOPS ceiling, nothing
  orphaned when a process dies mid-run.

- **ETag caching on the GitHub API.** A `304 Not Modified` costs zero rate-limit
  quota, so returning users get instant loads and the quota stays intact.

- **A TOCTOU race gave away free LLM compute.** The free-tier repo limit read a
  count, then inserted later — ten parallel requests all passed the check. Fixed
  with `pg_advisory_xact_lock`, verified by firing 6 concurrent requests and
  confirming exactly 3 got through.

- **Gin's `Recovery()` does not cover goroutines you spawn.** A panic in a
  background job takes down the whole process. Every background goroutine here
  carries its own `recover()`.

- **Structured LLM output, always.** Pydantic schemas via Agno mean the Go side
  gets valid JSON every time — no regex-stripping markdown fences at 2am.

- **Sometimes "success" is the bug.** Google's favicon endpoint returns a 16×16
  placeholder *with a 404 status but a valid PNG body* — it decodes fine, so
  `onError` never fires. Detection is by `naturalWidth`, not by error.

---

## Documentation

| File | What |
|---|---|
| `PROGRESS.md` | Dated changelog |
| `SESSION_JOB_MAP_HANDOFF.md` | Full Job Map handoff — architecture, every bug, alternatives evaluated and rejected, prioritised next steps |
| `project docs/` | Architecture, schema, API, security, roadmap |

---

## Status

Actively built in public. Core interview loop and Job Map are working;
billing, deployment and gRPC migration are in progress.

Issues and PRs welcome.
