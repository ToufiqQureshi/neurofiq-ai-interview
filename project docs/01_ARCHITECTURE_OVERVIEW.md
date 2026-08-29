# Architecture Overview

## High-Level Flow

```JavaScript
Candidate → Auth (Google or GitHub) → Connect GitHub (if not already)
  → Select/Pin Repo(s) → Repo Size Check → Analysis Pipeline
  → Question Generation → Interview Session (text or voice, camera optional)
  → AI Evaluation → Report → Session History
```

## System Components (Go + Python Split)

```Python
┌─────────────┐      ┌──────────────────┐
│ React + TS  │◄────►│  Go Backend       │◄────► Supabase (Postgres)
│ (Cloudflare │      │  (AWS App Runner) │       - Go owns ALL DB access
│  Pages)     │      │  Port ~8080       │       - GORM/pgx driver
└─────────────┘      └────────┬──────────┘
                               │ internal REST call
                               │ (with correlation ID)
                               ▼
                      ┌──────────────────┐
                      │  Python AI Worker │──► Claude API
                      │  (FastAPI)        │──► GitHub API (for repo analysis)
                      │  (AWS App Runner) │
                      │  Port ~8000       │
                      │  STATELESS — no DB │
                      └──────────────────┘

Voice path (both Go and Python involved differently):
┌─────────────┐   WebSocket    ┌──────────┐  internal REST  ┌────────────┐
│  Candidate  │◄──────────────►│    Go    │◄───────────────►│   Python   │
│  (browser)  │  audio stream  │ (relay,  │  transcript in,  │ (Claude    │
│             │                │ Deepgram/│  AI response out │  turn      │
│             │                │ElevenLabs│                  │  logic)    │
│             │                │  calls)  │                  │            │
└─────────────┘                └──────────┘                  └────────────┘
```

## Component Responsibilities

### Go Backend (Main/Orchestrator)

- Owns ALL database access (Supabase/Postgres) via GORM or pgx
- GitHub OAuth + Google OAuth flows
- All public-facing REST API endpoints (see doc 03)
- Repo listing, pinning, usage/rate-limit tracking (single source of truth
  — see doc 08 for why this must be centralized in Go, not duplicated)
- Voice interview WebSocket connection handling (audio relay to/from
  Deepgram and ElevenLabs)
- Billing (Razorpay) integration
- Calls the Python worker internally for anything requiring Claude/AI
- Camera/recording chunk upload + storage orchestration

### Python AI Worker (Stateless)

- **No database connection, ever.** Receives JSON in, returns JSON out.
- Repo code analysis: takes a structured file/snippet payload from Go,
  runs the Claude call, returns architecture/complexity/questions
- Answer evaluation: takes question + candidate answer, returns score +
  feedback via Claude
- Voice turn processing: takes a transcript + session context from Go,
  returns the next AI response text (Go then sends this to ElevenLabs)
- Can be scaled horizontally (multiple instances) independently of Go when
  AI processing becomes the bottleneck — this statelessness is exactly why
  that scaling is easy (see StayChat's own scaling notes)

### Frontend (React + TypeScript + Vite)

- Talks ONLY to the Go backend's public API and the Go WebSocket endpoint.
  **Never talks to Python worker directly, never talks to GitHub/Claude/
  Deepgram/ElevenLabs directly.** All third-party API keys stay server-side
  in Go and Python only.

## Design Principles (apply to every feature built)

1. **Cost must never scale unpredictably with input size.** A 500-line repo
   and a 500,000-line repo must cost roughly the same to analyze. Hard caps
   everywhere (see `04_REPO_ANALYSIS_PIPELINE.md`).
2. **Deterministic work never goes through Claude, and stays in Go where
   possible.** Language detection, commit stats, file filtering, tab-switch
   detection — all rule-based, zero AI cost, and ideally done in Go before
   the Python worker is even called.
3. **Batch AI calls, never loop them.** One Claude call producing 7
   structured questions, not 7 separate calls.
4. **Expensive features are paid-tier gated.** Voice and camera/proctoring
   are premium because their AI/storage cost is 5-10x text-based flow.
5. **Rate limits and usage counters live in exactly one place: Go, backed
   by Postgres.** The Python worker must never independently decide "is
   this user allowed to do this" — it trusts Go's authorization, avoiding
   race conditions (see doc 08).
6. **Python worker stays stateless — no exceptions.** If a future feature
   seems to need Python to touch the database, that's a signal the
   feature should be re-scoped so Go owns that data access and passes
   Python only what it needs for the AI call.
7. **Every feature ships with its usage limit defined before code is
   written** — free tier caps, paid tier caps, and abuse-prevention rate
   limits are part of the spec, not an afterthought.

## Production-Grade Scalability & Reliability

This system is built for production launch traffic from day one, skipping traditional "MVP" shortcuts that would crumble under load.

1. **In-Memory Streaming over Disk I/O:** The Go backend downloads and processes large GitHub repositories entirely in memory (RAM). By skipping disk writes, we avoid I/O bottlenecks and EBS volume exhaustion during traffic spikes, allowing highly concurrent repo processing.
2. **Go Concurrency:** By using Go (`goroutines`), the API server can handle thousands of concurrent API requests with minimal memory footprint, making it incredibly resilient to the "Launch Day Hug of Death."
3. **ETag Caching:** We heavily utilize GitHub's ETag caching in our `github_service.go` to ensure we never hit API rate limits or spam GitHub, even if thousands of users try to analyze repos simultaneously.
4. **Pure Postgres (No BaaS Bottlenecks):** We use Supabase purely as a managed Postgres database without relying on intermediate BaaS layers (like Supabase Auth or Storage) that could introduce cold-start latency or vendor lock-in.

## Solo-Founder Process Note

No multi-person review chains, no sprint ceremonies, no canary deploys —
these solve coordination problems that don't exist for a solo builder. What
IS carried over from big-tech practice: write a short design note before
building anything non-trivial, name open decisions explicitly, ship small,
monitor both services with Sentry, keep tests passing before deploy.
