# Deployment & Infrastructure

## Architecture

```
┌──────────────────────────────────────────────────┐
│  Cloudflare Pages (Frontend — React + TS + Vite)  │
│  - Global CDN, auto SSL, DDoS protection          │
│  - Domain: app.neurofiq.in                        │
└──────────────────┬─────────────────────────────────┘
                    │ HTTPS API calls + WebSocket
                    ▼
┌──────────────────────────────────────────────────┐
│  AWS App Runner — Go Backend (Main/Orchestrator)  │
│  - Domain: api.neurofiq.in                        │
│  - Auto-scaling, git-push-to-deploy                │
│  - Owns: auth, DB, billing, voice WebSocket        │
└──────────────────┬─────────────────────────────────┘
        │ internal REST (VPC-private, no public DNS)
        ▼                                    │
┌──────────────────────────────┐             │ Postgres connection
│  AWS App Runner —             │             ▼
│  Python AI Worker (FastAPI)   │   ┌──────────────────────────┐
│  - No public ingress          │   │  Supabase                 │
│  - Stateless, no DB            │   │  (DB + Auth + Storage)    │
│  - Scales independently        │   │  - Managed Postgres        │
│    when AI load is the        │   │  - PgBouncer pooling       │
│    bottleneck                  │   │  - RLS                     │
└──────────────────────────────┘   └──────────────────────────┘
```

## Backend Hosting Decision: AWS App Runner for BOTH services

| Option | Trade-off |
|--------|-----------|
| EC2 | Full control, but manual patching/scaling for TWO services — high time cost for a solo founder |
| Lambda + API Gateway | Cold starts hurt voice-interview latency requirements (doc 05) badly, especially on the Python worker |
| **App Runner (chosen, both services)** | Docker push → AWS handles scaling for each; consistent operational model across Go and Python; simplest solo-founder setup for a 2-service architecture |

```yaml
# apprunner-go.yaml
version: 1.0
runtime: docker
build:
  commands:
    build:
      - go build -o server .
run:
  command: ./server
  network:
    port: 8080
```

```yaml
# apprunner-python.yaml
version: 1.0
runtime: python3.11
build:
  commands:
    build:
      - pip install -r requirements.txt
run:
  command: uvicorn main:app --host 0.0.0.0 --port 8000
  network:
    port: 8000
```

## Domain / Networking Setup

```
neurofiq.in         → Marketing/landing page
app.neurofiq.in      → Frontend application (Cloudflare Pages)
api.neurofiq.in       → Go backend (AWS App Runner, DNS via Cloudflare,
                        PUBLIC)
(no public domain)   → Python AI worker (AWS App Runner, PRIVATE —
                        reachable only from Go within the VPC)
```

Both App Runner services should be placed in the same VPC with private
networking enabled so Go can reach Python over an internal address without
that traffic ever touching the public internet.

## Local Development Setup (matches StayChat's pattern)

```bash
# Terminal 1: Go backend
cd backend-go && go run .

# Terminal 2: Python AI worker
cd ai-worker && source venv/bin/activate && uvicorn main:app --reload

# Terminal 3: Frontend
cd frontend && npm run dev
```

For parity with production, use Docker Compose locally so environment
variables and internal networking (`http://ai-worker:8000` instead of
`localhost:8000`) match what App Runner's VPC setup will look like — this
avoids the "works locally, breaks in prod because of hardcoded localhost
URLs" issue flagged in doc 12.

## Speed Optimization

**Frontend (Cloudflare Pages):**
- Automatic global CDN edge caching
- Image optimization (Cloudflare Images if needed later)
- Code splitting / lazy loading (React)
- Minification via Vite build process

**Go Backend:**
- Goroutines handle concurrent requests natively — this is the core
  reason Go was chosen for this layer (see doc 01).
- Connection pooling to Supabase via PgBouncer — reuse connections, don't
  create new ones per request.
- Response compression middleware (gzip).
- Background goroutines for non-critical work (report generation trigger,
  video merging) so the HTTP response isn't blocked.

```go
func CompleteInterview(c *gin.Context) {
    sessionID := c.Param("session_id")
    go generateReportAsync(sessionID) // non-blocking
    c.JSON(200, gin.H{"status": "completing"})
}
```

**Python AI Worker:**
- Stays stateless and lightweight — no DB connection overhead at all.
- Can be scaled to multiple instances independently of Go when AI
  processing volume (not request volume) is the bottleneck — this is the
  direct payoff of keeping it stateless, matching the scaling note in
  StayChat's own docs.

- Database query optimization (Go side) — indexed lookups, avoid N+1
  queries.
- Cloudflare cache rules for cacheable GET responses (e.g. `GET /repos`
  cached 5 min per user, subject to correctness needs).

## Rough Monthly Infra Cost Estimate (early stage, ~100 users)

```
AWS App Runner (Go backend):       ~$25-30
AWS App Runner (Python worker):    ~$15-25 (min instance cost even at low traffic)
Supabase:                          $0 (free tier) → $25 when scaling
Cloudflare Pages:                  $0 (free tier covers this scale)
S3/Storage (if camera Tier B active): ~$1-5
Email (SendGrid or similar):       ~$0-19 (depending on volume)
Monitoring (Sentry, both services): ~$0-29 (free tier likely sufficient early)
─────────────────────────────────────────────────────────
TOTAL:                             ~$45-90/month before AI costs
```

This is ~$20-30/month more than a single-service setup would cost — a
deliberate trade-off made for the Go/Python polyglot portfolio value (see
prior discussion) rather than pure cost minimization. AI costs (Claude,
Deepgram, ElevenLabs) are covered separately in doc 07 — they scale with
usage, not with infra, and remain the dominant variable cost regardless of
this architecture choice.
