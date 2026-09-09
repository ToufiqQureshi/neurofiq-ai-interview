---
name: verify
description: Build/launch/drive recipe for verifying backend-go changes against the live app
---

# Verifying backend-go changes

## Build
```bash
cd backend-go && go build ./... && go vet ./...
```

## Launch (dev)
```bash
cd backend-go && go run main.go   # :8080, AutoMigrate runs on boot
```
AutoMigrate against the remote Supabase Postgres takes ~20-40s (many
`SLOW SQL >= 200ms` lines checking every table/column/constraint) before the
server actually starts listening. Poll instead of a fixed sleep:
```bash
until curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/api/companies/stats --max-time 2 | grep -q 200; do sleep 2; done
```
To restart after a code change (no hot reload): find the PID on 8080 and kill
it before re-running.
```bash
netstat -ano | grep ":8080" | grep LISTENING   # last column is the PID
taskkill //F //PID <pid>
```

## Drive: the Job Map / board discovery pipeline
Real data check (matches CLAUDE.md's own verification pattern):
```bash
curl -s "localhost:8080/api/companies?hiring=1&page_size=1" \
  | python -c "import json,sys;d=json.load(sys.stdin);print(d['total'],'companies |',d['open_roles'],'roles')"
```
Trigger one discovery tick on demand (no auth, runs in background — the
scheduled rotation otherwise only fires every 3h):
```bash
curl -s -X POST "http://localhost:8080/api/admin/run-discovery"
```
Then poll the companies count above until it changes (or ~60s passes) to see
what that tick actually stored. `go run`'s stdout carries the real evidence —
`board discovery rotation: <source> "<query>" -> N new companies | ...` lines
and, for a search-provider-parameter question, a temporary `log.Printf` right
before the outgoing API call (removed again after capturing the evidence) is
the fastest way to confirm what was actually sent, since Exa/Tavily request
bodies aren't logged by default.

## Gotchas
- Restarting the dev server kills whatever terminal/process was running it —
  say so before doing it if the user may have it open elsewhere.
- The scheduled rotation index (`idx := time.Now().Unix()/discoveryIntervalSeconds % len(boardSeedQueries)`)
  doesn't change for the whole interval (3h), so multiple manual triggers
  within a few minutes hit the *same* seed query — later ones legitimately
  return 0 new companies (already deduped), not a bug.
- `go test`/`go vet` prove the code compiles and unit logic holds; they are
  not a substitute for the curl-and-observe steps above.
