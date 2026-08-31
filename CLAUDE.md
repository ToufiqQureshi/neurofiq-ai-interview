# NeuroFIQ — working notes for Claude

AI interviewer that reads a user's real GitHub repo and questions them on their
own architecture, plus a **Job Map**: a self-maintaining directory of startups
and their real open roles.

Built and maintained by **one person**. That constraint decides most calls below.

## Layout

```
backend-go/     Go orchestrator — Postgres, OAuth, billing, rate limiting,
                repo extraction, the Job Map pipeline. Owns all state.
ai-worker/      Python FastAPI + Agno. Talks to the LLM. Zero DB access.
frontend/       React 19 + Vite + Tailwind v4 SPA.
project docs/   Architecture, schema, API, security, roadmap.
```

## Running it

```bash
cd ai-worker  && uvicorn main:app --host 0.0.0.0 --port 8001 --env-file ../.env
cd backend-go && go run main.go          # :8080, AutoMigrates on boot
cd frontend   && npm run dev             # :5173
```

Port **8001**, not 8000 — 8000 collides with another local service on this
machine and the failure is silent.
The worker needs `--env-file`; without it the key loads as empty and every
call fails with an unhelpful error.

Check work with real data, not by reading code:

```bash
curl -s "localhost:8080/api/companies?hiring=1&page_size=1" \
  | python -c "import json,sys;d=json.load(sys.stdin);print(d['total'],'companies |',d['open_roles'],'roles')"
```

Before finishing: `cd backend-go && go build ./... && go vet ./...`

## Rules that came from being corrected

**Keep it small.** One person maintains this in production at 2am. A regex plus
a fetch function plus a switch case beats a framework. If a change needs a
diagram to explain, it is probably the wrong change.

**Never enter data by hand.** The Job Map's whole value is that it maintains
itself. Hand-seeding a company to make a screenshot look better is a lie about
what the system does. If the pipeline missed something, fix the pipeline.

**Bad data is worse than no data.** An extraction once returned 295 "jobs" that
were an alphabetical list of professions off a career-advice article. Guards in
`careersPageResultLooksReal` reject that shape now. When adding a source, ask
what its garbage looks like and reject it explicitly.

**Verify before claiming done.** Query the running system and quote the number.
"Should work" is not a result. Two separate false readings in this project came
from a *test script* bug, not the code under test — when a number looks wrong,
suspect the measurement first.

**Free tiers only.** Firecrawl is 1000 pages/month with a budget guard at 800;
past it, calls fall back to keyless Jina. Free paths run before paid ones,
detection is cached per company, and companies with no job board are not
re-checked for 7 days. Do not remove a guard to make a run finish faster.

**Never commit secrets.** `.env` holds live Supabase, GitHub OAuth, DeepSeek,
Firecrawl and Exa credentials. It is gitignored. Before any commit:
`git diff --cached --name-only | grep -E '(^|/)\.env$'` must return nothing.

## Architecture boundaries worth preserving

- **Python has no database access.** By rule, not accident. It stays a pure
  function `f(code) = analysis`, so scaling is just more containers.
- **Every background goroutine carries its own `recover()`.** Gin's
  `Recovery()` does not cover goroutines you spawn; one panic kills the process.
- **LLM output is always a Pydantic schema** via Agno's `output_schema`. Never
  parse markdown fences out of a model response.
- `discoveryIntervalSeconds` in `company_service.go` must stay equal to the
  cron schedule in `main.go` — the seed-query rotation index is derived from it.

## Job Map pipeline

Four tiers, cheapest first. The premise: a careers page must load its jobs from
somewhere public, because visitors are not logged in.

| Tier | What | Cost |
|---|---|---|
| 0 | Resolve careers URL (`/careers`, `/jobs`…) | free |
| 1 | Plain HTTP + regex for an embedded ATS board link | free |
| 2 | Hosted render (Firecrawl → Jina) for JS pages | 1 credit |
| 3 | Guess board slug from name, verify against real API | free |
| 4 | LLM extraction off the careers page | 1 credit |

Boards: Greenhouse, Lever, SmartRecruiters, Ashby, Workable, Keka, Workday —
all official public JSON APIs.

## Work queue

1. Retry failed extractions — Classplus, Physics Wallah, Extramarks have
   careers URLs but return 0 jobs, and we know they are hiring.
2. Direct Exa lookup for careers URLs from Go. Finding a careers page is a
   lookup, not a judgment call, so it should skip LLM tokens entirely. Slots
   in as a new tier in `ResolveCareersURL`.
3. More boards: Gem (seen on Hasura), Recruitee, Personio, Freshteam.
4. Tier 2 and Tier 4 fetch the same page twice — 2 credits where 1 would do.
   Worth fixing as usage approaches the budget.
5. Re-enrich companies after first discovery; stage and description are frozen
   at first-seen values today. Jobs *are* re-synced.

Known gaps, not blocking: filters are exact-match against a fixed dropdown and
the agent sometimes emits values outside it; `POST /api/companies/discover` is
any-authenticated-user rather than admin-only; `/health` returns a hardcoded
`{"db":"connected"}` without pinging anything; `jobs` and `scrape_usages` have
no migration file and exist only via AutoMigrate.

## More detail

`SESSION_JOB_MAP_HANDOFF.md` — every bug, alternatives evaluated and rejected,
and why. `PROGRESS.md` — dated changelog.
