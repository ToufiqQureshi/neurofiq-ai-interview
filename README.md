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
                                snippets, shown beside the code they came
                                from. One of the five comes from your commit
                                history. Text or voice ($0 — browser Web
                                Speech API, no streaming vendor).
5. Report                       0-10 on correctness, depth, clarity and
                                trade-off awareness — plus, per question,
                                what a strong answer would have covered.
6. Share                        One click mints a public link. Score and
                                assessment travel; your answers don't.
```

**The questions your history writes.** The archive says what the code is now.
The commit log is the only record of what it used to be — so one question comes
from there: work that got revisited, a decision the messages show being
reversed. "You rewrote this auth middleware three times in one week last
March — what did the first two get wrong?" is not a question another product
can ask you.

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

## Sharing a report

The candidate owns the result. Finishing an interview produces a private
report; turning sharing on mints an unguessable link the candidate can send
to whoever they want, and turning it off makes that link 404 immediately.

The shared page carries the questions, the scores and the assessment. The
candidate's own answers stay private — a recruiter sees how the reasoning was
judged, not the raw transcript.

---

## The Job Map

A self-maintaining directory of startups and their real open roles. No manual
data entry.

### Finding companies — through their job boards, not through a model

The insight the whole feature rests on:

> Every hiring company publishes its roles on a job board, and those boards
> are public pages a search engine has already indexed.

So discovery searches the board domains directly. A hit like
`jobs.lever.co/Sprinto` carries the same slug the board's public API takes, so
one search yields a company that is **provably hiring**, with its open roles
one free call away. There is nothing to verify afterwards, because the board
is the verification. No LLM anywhere in this path.

The reverse — asking a model which companies exist, then hunting for a careers
page on each answer — stacked two guesses and mostly returned shops that do
not hire. That agent is gone.

A three-hourly cron rotates through 120 generated queries (12 cities × 10
roles); job syncing runs hourly on a schedule of its own, because it is free.

### Finding the roles — cheapest first

| Step | What | Cost |
|---|---|---|
| 0 | Board already known from discovery → its public JSON API | free |
| 1 | Resolve the careers URL (`/careers`, `/jobs`…) | free |
| 2 | Plain HTTP fetch → regex for an embedded ATS board link | free |
| 3 | Link-scan that page for per-role postings | free |
| 4 | Rendered read (Jina, then Firecrawl) → repeat 2 and 3 | free / 1 credit |
| 5 | LLM extraction straight off the careers page | 1 credit |

A board is only ever accepted **with evidence**: either the company linked to
it from its own careers page, or a search returned it. Never by guessing a
slug from the company name — slugs are not unique, and `jobs.lever.co/cred`
returns a healthy job list belonging to CreditVidya, not to CRED. One
company's roles under another's name is worse than no roles at all.

Detection is cached per company: a week once a board is found, twelve hours
while none is.

**Supported job boards (8):** Greenhouse · Lever · SmartRecruiters · Ashby ·
Workable · Keka · Darwinbox · Workday — all via their official public JSON
APIs. No scraping, no keys, no hallucination.

**Search:** Exa first, Tavily when Exa's month is spent. A provider is only
usable here if it can restrict results to a domain list — without that, a
search for "backend engineer jobs in Pune" answers with blog posts instead of
boards.

### Cost control

Free tiers only. The guards that keep it that way:

1. Free paths run first — Jina reads every page before Firecrawl is asked
2. Detection is cached per company
3. Search is rationed: one query per three-hourly tick, at most 5 new
   companies per run, so a tick costs at most 6 metered calls
4. A monthly budget (800 of each 1,000 free tier) stops a provider before the
   wall, and the next one takes over
5. A credit is counted only once a call actually returns something
6. An empty read does not clear a company's roles the first time — an ATS
   under maintenance and a redesigned careers page both read as zero

Usage is tracked per month per provider, in one table, and logged on every
sync.

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

### Tests

```bash
(cd backend-go && gofmt -l . && go build ./... && go vet ./... && go test -race ./...)
(cd frontend  && npm ci && npm run lint && npm run build)
(cd ai-worker && python -m compileall -q main.py)
```

Each line runs in a subshell, so they all start from the repository root.

CI runs the same three groups on pull requests and on pushes to `main`.

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

- **`.tsx` does not end in `.ts`.** The file scorer matched source files by
  suffix against `.go/.py/.ts/.js`, so a React codebase — including this repo's
  own frontend — contributed zero files and the interview was built from
  `package.json` alone. Java, Rust, Ruby, C# and the rest scored zero too. One
  four-line predicate, and the product's central claim was quietly false for
  most of GitHub.

- **`break` where `continue` belonged.** Files are packed into the prompt
  highest-score-first, and the budget check exited the loop instead of skipping
  the file. A single oversized `main.go` sorts first, blows the budget, and
  leaves the model with nothing at all.

- **A URL blocklist doesn't stop SSRF; a dialer does.** Every URL the Job Map
  fetches comes from an LLM's web search, and a string check on the hostname
  loses to both a 302 and an A record pointing at `169.254.169.254`. The guard
  runs in `net.Dialer.Control`, after resolution, on the address actually being
  dialled.

- **A signed cookie is not a private one.** The session carries a GitHub OAuth
  token. Signing proves we issued the cookie; it does nothing to stop whoever
  holds it from reading the contents. Two keys, not one.

---

## Documentation

| File | What |
|---|---|
| `PROGRESS.md` | Dated changelog |
| `SESSION_JOB_MAP_HANDOFF.md` | Full Job Map handoff — architecture, every bug, alternatives evaluated and rejected, prioritised next steps |
| `project docs/` | Architecture, schema, API, security, roadmap |

---

## Status

Actively built in public. The core interview loop, shareable reports and the
Job Map are working. Billing, deployment and the gRPC migration are in
progress.

Issues and PRs welcome.
