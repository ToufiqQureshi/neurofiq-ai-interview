# Production Reliability — Go ↔ Python Integration

This doc exists because a two-service split introduces specific failure
modes that don't exist in a monolith. Read this before writing any code
that calls between Go and Python. Treat everything in this doc as
mandatory for Phase 1, not "nice to have polish."

## 1. Inter-Service Communication Failures

**Problem:** Go's internal call to the Python worker can fail for reasons
that have nothing to do with your business logic — the worker restarting,
a slow Claude API response blowing past Go's timeout, a transient network
hiccup inside the VPC.

**Required handling:**
- Explicit timeout on every Go→Python call (recommend 10s for
  analysis/question-gen calls, 3s for voice-turn calls given the latency
  budget in doc 05).
- Retry with backoff for idempotent calls (analysis, question generation)
  — NOT for calls that might have side effects without idempotency keys.
- On exhausted retries, return a clear, candidate-facing error ("We're
  having trouble analyzing this right now, please try again") — never let
  the frontend hang silently.

```go
func callPythonWorker(path string, payload any, correlationID string) (map[string]any, error) {
    var lastErr error
    for attempt := 0; attempt < 3; attempt++ {
        ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
        defer cancel()

        req, _ := http.NewRequestWithContext(ctx, "POST", pythonWorkerURL+path, toJSON(payload))
        req.Header.Set("X-Internal-Secret", internalSecret)
        req.Header.Set("X-Correlation-ID", correlationID)

        resp, err := httpClient.Do(req)
        if err == nil && resp.StatusCode == 200 {
            return parseResponse(resp), nil
        }
        lastErr = err
        time.Sleep(time.Duration(attempt+1) * 200 * time.Millisecond) // backoff
    }
    return nil, fmt.Errorf("python worker unreachable after retries: %w", lastErr)
}
```

## 2. Independent Service Crashes (Silent Partial Outage)

**Problem:** The Python worker can crash (unhandled Claude API exception,
memory issue) while Go stays completely healthy. The frontend sees "the
site is up" but every AI feature is dead — a confusing failure mode for
both the candidate and for you when debugging.

**Required handling:**
- Separate health check endpoints: Go's own `/health` AND
  `/admin/health` which explicitly pings the Python worker's
  `/internal/health` and reports both statuses.
- Separate Sentry projects (or at minimum separate tags) for Go and
  Python so an alert clearly tells you which service is the problem.
- Go should degrade gracefully where possible — e.g. if the Python worker
  is down, `/repos/.../analyze` should return a clear "AI analysis
  temporarily unavailable" rather than a generic 500.

```go
func AdminHealthCheck(c *gin.Context) {
    goStatus := "healthy"
    pythonStatus := "unreachable"

    resp, err := httpClient.Get(pythonWorkerURL + "/internal/health")
    if err == nil && resp.StatusCode == 200 {
        pythonStatus = "healthy"
    }

    c.JSON(200, gin.H{"go": goStatus, "python_worker": pythonStatus})
}
```

## 3. Deployment Ordering / API Contract Drift

**Problem:** Go gets deployed with a new field in its internal request
payload, but the Python worker is still running the previous version and
doesn't expect that field (or vice versa) — a breaking change that passed
local testing because both services were updated together locally.

**Required handling:**
- Treat the internal API contract (doc 03) as a versioned contract, not an
  implicit agreement. New fields should be additive and optional on the
  receiving side (Python should ignore unknown fields, not error on them;
  Go should have sane defaults for fields the worker might not yet return).
- Deploy backward-compatible changes first, on both sides, before deploying
  anything that assumes the new shape is always present.
- Document the internal contract explicitly in doc 03 and keep it updated
  — it's your interface boundary even though there's no external consumer.

## 4. Voice WebSocket Specific Issues

**Connection drops:** Candidate's internet flickers, WebSocket disconnects
mid-interview. Without reconnection handling, the session is corrupted/
incomplete.
- MVP: on disconnect, mark the session `abandoned` in Postgres and allow
  the candidate to see "resume" as a new session referencing the same
  repo analysis (don't re-run analysis, it's already cached).
- Phase 2: actual mid-session reconnect with state restoration.

**Concurrent connections / worker bottleneck:** Go's goroutines can handle
many simultaneous WebSocket connections easily, but if the Python worker
processes turns sequentially or has too few instances, it becomes the
bottleneck even though Go looks fine.
- Monitor Python worker response times specifically for `/internal/voice-
  turn`, not just Go's overall latency.
- Since the Python worker is stateless, scale it horizontally (multiple
  App Runner instances) if this becomes the bottleneck — this is the
  specific advantage of keeping it stateless (see doc 01, doc 09).

**Memory leaks in long-lived connections:** Ensure explicit cleanup on
WebSocket disconnect (Go side) — close Deepgram/ElevenLabs streams, remove
session state from any in-memory maps.

## 5. Rate-Limit / Cost-Cap Race Conditions

**Problem:** If both Go and Python independently checked "is this user
allowed to do this," two concurrent requests could both pass the check
before either updates the usage counter — silently exceeding the free-tier
cap or blowing past the 15k-token analysis budget.

**Required handling (restated from doc 08, critical enough to repeat
here):**
- The Python worker performs **zero** authorization or rate-limit logic.
  It trusts that Go already checked.
- Go's usage-counter increments are atomic Postgres operations (`ON
  CONFLICT DO UPDATE SET count = count + 1`), never a separate
  read-then-write.
- Go checks the limit and only on success calls the Python worker.

## 6. Database Connection Handling

**Problem:** Go owns all DB access — if its connection pool to Supabase
gets exhausted under concurrent load, new requests fail even though the
database itself is fine.

**Required handling:**
- Set explicit connection pool limits in Go's GORM/pgx config, sized
  appropriately for Supabase's PgBouncer settings (don't let Go try to
  open more connections than PgBouncer allows).
- Reconfirm discipline: the Python worker NEVER gets a database connection
  string, ever, even for a "quick" future feature — if a future feature
  seems to need this, re-scope it so Go fetches the data and passes it to
  Python instead.

## 7. Local Dev vs Production Parity

**Problem:** Locally you run `go run .` and `uvicorn main:app --reload`
directly, addressing each other via `localhost`. In production, App
Runner's internal VPC networking uses different addressing — a URL
hardcoded to `localhost:8000` will silently break in production.

**Required handling:**
- Use Docker Compose locally with named services (`ai-worker`,
  `go-backend`) so internal URLs are environment-variable-driven
  (`PYTHON_WORKER_URL=http://ai-worker:8000` locally,
  `PYTHON_WORKER_URL=<internal App Runner address>` in production) —
  never hardcode `localhost` anywhere in committed code.

## 8. Monitoring Blind Spots — Correlation IDs

**Problem:** When something fails, cross-referencing "what happened in Go"
with "what happened in Python for the same request" is painful without a
shared identifier.

**Required handling — mandatory from Phase 1, not an optional add-on:**
- Go generates a `correlation_id` (UUID) at the start of every request
  that will touch the Python worker.
- This ID is passed via `X-Correlation-ID` header on the internal call.
- Both services log this ID with every log line related to that request
  (structured logging — use a logging library that supports fields, not
  plain `fmt.Println`/`print`).
- Both services' Sentry configuration should tag events with this ID where
  available, so a single search finds the full cross-service trace of one
  failed request.

```python
# Python: pull correlation ID from header, attach to all logs for this request
@app.middleware("http")
async def add_correlation_id(request: Request, call_next):
    correlation_id = request.headers.get("X-Correlation-ID", "unknown")
    with logger.contextualize(correlation_id=correlation_id):
        response = await call_next(request)
    return response
```

## Priority — What Must Exist Before Launch vs What Can Wait

```
MUST HAVE before Phase 1 launch:
1. Health checks + independent monitoring for both services (§2)
2. Centralized, atomic rate-limit checking in Go only (§5, doc 08)
3. Timeout + retry logic on every Go→Python call (§1)
4. Correlation IDs on every cross-service request (§8)
5. Docker Compose local setup matching production addressing (§7)

CAN DEFER to Phase 1.5 (still needed before voice ships):
6. WebSocket reconnection logic (§4) — voice interview specifically
7. Python worker horizontal scaling setup (§4) — only if load requires it

CAN DEFER further (Phase 2, only if issues actually arise):
8. Full circuit breakers beyond basic retry (§1)
9. Mid-session WebSocket state restoration (§4)
```
