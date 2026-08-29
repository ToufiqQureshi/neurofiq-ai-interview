
# neurofiq.in — Full Product & Engineering Documentation

Status: Pre-build design docs, consolidated for implementation with Claude
Code / Antigravity.
Owner: Toufiq (solo founder/engineer)

## Product One-Liner

An AI-powered technical interview practice platform. Candidates connect
their GitHub account, pick a repo (usually one matching what's on their
CV), and an AI interviewer analyzes their actual code to ask personalized
technical questions — via text or voice — instead of generic mock-interview
questions.

## Tech Stack (locked)

- **Frontend**: React + TypeScript + Vite, deployed on Cloudflare Pages
- **Main Backend**: **Go** — orchestrator, owns DB, auth, all REST APIs,
  voice WebSocket handling. Deployed on AWS App Runner.
- **AI Worker**: **Python + FastAPI** — stateless AI brain. No DB access.
  Receives structured requests (prompt/context in), returns AI output
  (JSON out). Deployed on AWS App Runner as a separate service.
- **Database**: Supabase (Postgres) — accessed ONLY by the Go service
  (GORM or pgx driver). Also used for MVP-level caching.
- **AI**: Claude API (called only from the Python worker)
- **Voice**: Deepgram (STT) + ElevenLabs (TTS) — audio streaming handled by
  Go (WebSocket layer), AI turn logic handled by Python worker
- **Auth**: GitHub OAuth (primary) + Google OAuth (identity only, GitHub
  connect required separately for repo access) — handled by Go
- **Payments**: Razorpay — handled by Go
- **CDN/Edge/DDoS**: Cloudflare (in front of frontend and both backend
  services)

## Why Go + Python Split (StayChat Pattern)

This mirrors the architecture already proven in Toufiq's own StayChat
project:

```
Go        = Orchestrator & Database Manager (fast, concurrent, handles
            webhooks/API/WebSocket at scale via goroutines)
Python    = Stateless AI Worker (best AI/LLM ecosystem, no DB, purely
            processes prompt-in/response-out)
```

Go is used specifically where its concurrency model gives a real, defensible
technical advantage (WebSocket voice streaming, handling many simultaneous
API/webhook requests) — not applied everywhere for its own sake. Python
stays the AI layer because Claude SDKs, GitHub API tooling, and code-parsing
libraries are strongest there.

## Document Index

1. `01_ARCHITECTURE_OVERVIEW.md` — system diagram, Go/Python responsibilities
2. `02_DATABASE_SCHEMA.md` — full Postgres/Supabase schema (Go-owned)
3. `03_API_ENDPOINTS.md` — Go public REST API + internal Go↔Python contract
4. `04_REPO_ANALYSIS_PIPELINE.md` — GitHub analysis + large-codebase cost safety
5. `05_VOICE_INTERVIEW_SYSTEM.md` — Go WebSocket relay + Python AI turn logic
6. `06_CAMERA_PROCTORING_SYSTEM.md` — camera, recording, proctoring, compliance
7. `07_COST_CONTROLS_AND_CACHING.md` — AI token savings, caching layers, tiers
8. `08_SECURITY_RATE_LIMITING_DDOS.md` — security, abuse prevention, race conditions
9. `09_DEPLOYMENT_INFRA.md` — AWS (2 services) + Cloudflare + Supabase deployment
10. `10_UI_FLOW_SCREENS.md` — screen-by-screen UX flow
11. `11_MVP_ROADMAP.md` — phased build order, what's in/out of MVP
12. `12_PRODUCTION_RELIABILITY.md` — health checks, retries, correlation IDs,
    failure handling between Go and Python services
13. `13_WORKING_MODE_TEACH_WHILE_BUILDING.md` — **read this first, every
    session** — how the AI coding tool should explain decisions and
    concepts while building, since I'm learning Go/FastAPI alongside
    shipping this

## Detailed Tech Stack

```
Frontend:    React + TypeScript + Vite + Tailwind CSS + Axios
             → Cloudflare Pages

Go Backend:  Gin (web framework) + GORM (ORM) + gorilla/websocket
             (voice WebSocket) + golang-jwt or signed cookies (sessions)
             + golang.org/x/oauth2 (GitHub/Google OAuth)
             + log/slog or zerolog (structured logging)
             → AWS EC2 (Amazon Linux 2023), Docker

Python AI Worker: FastAPI + Uvicorn + Pydantic (validation) + anthropic
             (Claude SDK) + loguru (structured logging)
             → same EC2, separate Docker container

Database:    Supabase (Postgres)

Infra:       Docker + Docker Compose (local + prod) + Nginx (reverse
             proxy on EC2) + GitHub Actions (CI/CD, Phase 1.5+)

Monitoring:  Sentry (Go SDK + Python SDK)
```

## How to use these docs with Claude Code / Antigravity

- Feed one doc at a time per build session (e.g. "implement everything in
  02_DATABASE_SCHEMA.md as Go migrations using GORM").
- Treat `11_MVP_ROADMAP.md` as the source of truth for *build order* — don't
  let the AI jump ahead to Phase 2 features while Phase 1 is incomplete.
- Treat `12_PRODUCTION_RELIABILITY.md` as mandatory reading before any
  Go↔Python integration code is written — it exists because these are the
  exact failure modes that show up in production with this split.
- Open decisions are explicitly marked `[OPEN DECISION]` — resolve these
  before asking the AI to implement that section.
