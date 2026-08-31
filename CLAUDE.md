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

**Free tiers only.** Keyless Jina reads every page first; Firecrawl (1000
pages/month, budget guard at 800) is the fallback, and a credit is counted
only once a call actually returns something. Free paths run before paid ones,
and detection is cached per company: a week once a board is found, twelve
hours while none is. The short retry is deliberate — a week-long freeze meant
every improvement reached the directory a week late. Do not remove a guard to
make a run finish faster.

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
- `discoveryIntervalSeconds` in `board_discovery.go` must stay equal to the
  cron schedule in `main.go` — the seed-query rotation index is derived from it.
- **A board is only ever accepted with evidence.** Either the company linked to
  it from its own careers page, or the board URL came back from a search. Never
  from guessing a slug off the company name: slugs are not unique, and
  `jobs.lever.co/cred` returns a healthy job list belonging to CreditVidya, not
  to CRED. One company's roles under another's name is worse than no roles.

## Job Map pipeline

**Companies come from job boards, not from a model.** Every hiring company puts
its roles on a board, and those boards are public pages a search engine has
already indexed. `board_discovery.go` searches Exa restricted to the board
domains; a hit like `jobs.lever.co/Sprinto` carries the same slug the board's
public API takes, so one search yields a company that is provably hiring with
its roles one free call away. No LLM anywhere in this path.

The reverse — asking a model which companies exist, then hunting for a careers
page on each answer — stacked two guesses and mostly returned 2-person shops
that do not hire. That agent is gone.

Roles, cheapest first. The premise: a careers page must load its jobs from
somewhere public, because visitors are not logged in.

| Step | What | Cost |
|---|---|---|
| 0 | Board already known from discovery → its public JSON API | free |
| 1 | Resolve careers URL (`/careers`, `/jobs`…) | free |
| 2 | Plain HTTP + regex for an embedded ATS board link | free |
| 3 | Link-scan that page for per-role postings | free |
| 4 | Rendered read (Jina, then Firecrawl) → repeat 2 and 3 | free / 1 credit |
| 5 | LLM extraction off the careers page | 1 credit |

Boards: Greenhouse, Lever, SmartRecruiters, Ashby, Workable, Keka, Darwinbox,
Workday — all official public JSON APIs.

Jina runs before Firecrawl everywhere. When Firecrawl went first, an unset key
or a spent budget took the whole path down and every company without a
supported ATS dropped to zero roles — the paid service was load-bearing for
the free product.

## Work queue

1. More boards: TalentRecruit (seen on Zepto), Gem (Hasura), Recruitee,
   Personio, Freshteam.
2. Board discovery stores no sector or stage, so those filters miss every
   company it finds. Enrich from the company's own site rather than guessing.
3. Re-enrich companies after first discovery; stage and description are frozen
   at first-seen values today. Jobs *are* re-synced.
4. `resolveCompanyWebsite` costs one Exa search per newly-seen company. Fine at
   this volume, worth batching if discovery widens.

Known gaps, not blocking: filters are exact-match against a fixed dropdown;
`POST /api/companies/discover` is any-authenticated-user rather than
admin-only; `jobs` and `scrape_usages` have no migration file and exist only
via AutoMigrate.

## More detail

`SESSION_JOB_MAP_HANDOFF.md` — every bug, alternatives evaluated and rejected,
and why. `PROGRESS.md` — dated changelog.
