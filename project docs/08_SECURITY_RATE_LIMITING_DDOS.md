 

# Security, Rate Limiting & DDoS Protection

## DDoS Protection (Layered)

```
Layer 1: Cloudflare (Frontend + proxy in front of Go backend)
├── Automatic DDoS protection (Layer 3/4) — free tier
├── Bot detection
├── Rate limiting rules (dashboard-configured, no code)
└── WAF (Web Application Firewall) — cheap paid add-on

Layer 2: AWS
├── AWS Shield Standard — free, automatic on all AWS resources
└── Security Groups — firewall rules on both App Runner services

Layer 3: Application-level
├── Go: rate limiting middleware per user/IP (public-facing)
├── Go↔Python internal calls: NOT exposed publicly at all — see below
├── CAPTCHA on signup (hCaptcha, free)
└── Request size limits
```

Cloudflare dashboard rules (no code needed):

```
Rule 1: Rate limit /api/* to 20 requests/10 seconds per IP
Rule 2: Challenge (CAPTCHA) if request rate > 50/min
Rule 3: Block known bad bots (Cloudflare threat intelligence)
```

## Securing the Internal Go↔Python Boundary

The Python worker must NEVER be reachable from the public internet:

- Deploy both Go and Python App Runner services inside the same VPC with
  private networking; Python worker has no public ingress.
- Every internal call carries `X-Internal-Secret` (shared secret, rotated
  periodically, stored in each service's environment config — never in
  code) — Python worker rejects any request missing/mismatching this.
- Every internal call carries `X-Correlation-ID` for cross-service tracing
  (see doc 12) — not a security measure itself, but should be treated as
  mandatory alongside the secret header.

```python
# Python worker: reject unauthenticated internal calls
@app.middleware("http")
async def verify_internal_secret(request: Request, call_next):
    if request.headers.get("X-Internal-Secret") != INTERNAL_SECRET:
        return JSONResponse(status_code=403, content={"error": "forbidden"})
    return await call_next(request)
```

## Rate Limiting — 3 Layers

```
Layer 1: Cloudflare (network level)     → 100 req/min per IP globally
Layer 2: Go middleware (app level)      → 30 req/min per authenticated user
Layer 3: Business logic (feature level) → per-endpoint limits (see doc 03
                                            and doc 07 for exact numbers)
```

```go
func RateLimitMiddleware() gin.HandlerFunc {
    requestCounts := make(map[string][]time.Time)
    var mu sync.Mutex

    return func(c *gin.Context) {
        ip := c.ClientIP()
        now := time.Now()

        mu.Lock()
        filtered := []time.Time{}
        for _, t := range requestCounts[ip] {
            if now.Sub(t) < time.Minute {
                filtered = append(filtered, t)
            }
        }
        requestCounts[ip] = filtered

        if len(requestCounts[ip]) >= 30 {
            mu.Unlock()
            c.AbortWithStatusJSON(429, gin.H{"error": "too many requests"})
            return
        }
        requestCounts[ip] = append(requestCounts[ip], now)
        mu.Unlock()

        c.Next()
    }
}
```

Note: this in-memory approach works for a single Go App Runner instance. If
you scale to multiple Go instances, move counters to Postgres (atomic
increment, as shown in doc 07) so limits are shared correctly across
instances.

## Critical: Race Condition Prevention (the Go/Python-specific risk)

**The problem:** if usage/rate-limit checks were duplicated in both Go and
Python (each independently deciding "is this allowed?"), two concurrent
requests could both pass the check before either updates the counter —
silently exceeding the free-tier cap or the daily analysis limit.

**The fix — enforced everywhere in this architecture:**

1. **Go is the only service that checks and increments usage counters.**
   The Python worker performs zero authorization/limit logic — it purely
   executes the AI task it's asked to do and trusts Go's gate-keeping.
2. **Counter increments are atomic Postgres operations** (`ON CONFLICT DO UPDATE ... SET count = count + 1`), not "read count, check, then write"
   in separate steps — see the transaction pattern in doc 07.
3. Go checks the limit, and ONLY on success does it call the Python worker.
   If the Python call fails downstream, the counter should already reflect
   the attempt (accept the small risk of an undercount on failure rather
   than the race-condition risk of checking after the fact).

## Feature-Specific Abuse Prevention

```go
func CheckAnalyzeLimit(userID string) error {
    today := time.Now().Format("2006-01-02")
    var usage AnalyzeUsage
    db.Where("user_id = ? AND date = ?", userID, today).First(&usage)

    if usage.Count >= 10 {
        return ErrDailyLimitReached
    }
    return nil
}
```

## Application Security Checklist

- **GitHub tokens never reach the frontend, and never reach the Python
  worker.** Only Go holds them, per the open decision in doc 02. All
  GitHub/Claude/Deepgram/ElevenLabs API calls happen server-side —
  GitHub calls from Go, Claude/Deepgram/ElevenLabs calls split between Go
  (audio streaming) and Python (Claude conversation logic) as specified in
  docs 04/05.
- **Sanitize code snippets before rendering.** Stored as text, rendered
  escaped — a candidate's own repo could contain `<script>` in a filename,
  README, or code comment; avoid XSS via that vector.
- **Row-Level Security in Supabase** — candidates can only read their own
  `interview_sessions`, `session_questions`, `candidate_reports`,
  `interview_recordings` (see full RLS pattern in doc 02). Go connects
  with a service role for admin operations but should still respect
  per-user scoping in application logic.
- **Webhook verification** — Razorpay webhook signature must be verified
  in Go before trusting payment confirmation payloads.
- **Consent gating** — camera/recording endpoints reject requests without a
  prior `POST /interviews/{id}/consent` call (doc 06).

## GDPR / DPDP Considerations

- Explicit deletion flow: `DELETE /users/me` cascades to all owned rows
  (Go-orchestrated, single transaction where possible).
- Defined retention period for `github_profiles` cache and session
  transcripts — state this in the privacy policy.
- EU users: consent capture at OAuth step for storing analyzed code
  snippets.
- India: DPDP Act 2023 compliance for candidate data, especially video/
  biometric data from the camera/proctoring feature (doc 06).
