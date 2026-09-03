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
were an alphabetical list of professions off a career-advice article. Three
guards stand in that shape's way now, and they are not interchangeable:
`guidancePageRe` rejects the page before it is read, `departmentTitles` rejects
a link whose whole text is a section name ("Engineering" is a landing page, not
a role), and `maxCareersPageRoles` is the ceiling if both are wrong. Note what
`careersPageResultLooksReal` can no longer do on its own: every row the link
scan produces carries its own posting URL, and the guard counts that as
evidence — so it will not reject a linked list of professions. When adding a
source, ask what its garbage looks like and reject it explicitly.

**An empty read is not the same as "not hiring".** An ATS answers 200 with an
empty array while it is being reconfigured, and the link scan reads a
redesigned page as zero. Deleting a company's roles on the first empty read
made a hiring company look shut and flipped it back an hour later. Listings now
survive one empty read and clear on the second (`applySyncedJobs`,
`Company.EmptyJobReads`). For the same reason `FetchATSJobs` errors on a
provider it does not know instead of returning no rows — silence and zero must
not look alike.

**Verify before claiming done.** Query the running system and quote the number.
"Should work" is not a result. Two separate false readings in this project came
from a *test script* bug, not the code under test — when a number looks wrong,
suspect the measurement first.

**Free tiers only.** Keyless Jina reads every page first; Firecrawl (1000
pages/month, budget guard at 800) is the fallback, and a credit is counted
only once a call actually returns something. Search is metered the same way in
`scrape_usages` — Exa first, Tavily when Exa's month is spent, both guarded at
800 of their 1000 free calls. Free paths run before paid ones, and detection is
cached per company: a week once a board is found, twelve hours while none is.
The short retry is deliberate — a week-long freeze meant every improvement
reached the directory a week late. Do not remove a guard to make a run finish
faster.

**No single provider may be load-bearing.** `resolveCompanyWebsite` was
Exa-only, which made the Tavily fallback useless: once Exa's month was spent,
discovery still paid Tavily to find boards and then dropped every company for
having no website — a run that costs a search and stores nothing. Exa's
`category=company` is a quality win, not a requirement; `isAggregatorHost` is
what actually rejects a LinkedIn page, and it works on any provider's results.
The same lesson as Firecrawl: check what happens when the good provider is
gone.

**Search is the only metered step in discovery, so it is rationed.** One
search per rotation tick to find boards, plus — only when nothing free named
the company's site — one per company whose homepage is *looked up*.
`RunDiscoveryRotation` runs every 15 minutes, matching
`discoveryIntervalSeconds` — frequent enough that the shared monthly
allowance would be gone well before the month is out on its own, which is
exactly why the budget guard below exists.
`websiteFromBoardPage` and `guessCompanyWebsite` (the slug tried as a domain
label, accepted only when the candidate site links back to the same board)
both run before that lookup, so most companies never spend a search at all:
of 145 stored in board search's first two days, 92 sat on their own slug
under one of five TLDs. Count attempts, not saves: a candidate rejected
after its lookup (no site found, domain already held) has still spent a
search. `mayStartLookup` gates each lookup individually against
`maxNewCompaniesPerRun`, and the loop itself stops at `limit` companies
*stored* — the two are different caps now that a company can be stored for
free, so a full tick can save more than it ever looks up. Job syncing is free
and stays hourly on its own lease, so listings remain fresh while discovery
slows. A board skipped by the cap is not lost: the rotation comes back.

**Board slugs are also harvested for free, on their own schedule.**
`slug_harvest.go` reads board URLs out of Common Crawl's public index — the
same URLs discovery would otherwise have to search for — and admits them
through discovery's own rules (`boardRowIsAdmissible`, live board, Indian
role, not a duplicate). No metered call anywhere in that path. It runs off
`RunScheduledHarvest` every three hours, but Common Crawl only publishes an
index monthly, so nearly every tick reads the last-seen index id
(`HarvestState`) and returns after one request; the cadence controls how
promptly a new index is noticed, not how often 13,000+ boards are re-read.

A quarter of the monthly budget is reserved for the rotation, checked before
the board search *and* before every lookup — a manual run that passed the
check on entry must not spend through the reserve while its loop is running.
`POST /api/companies/discover` is still any-authenticated-user; the reserve
bounds what they can take, it does not decide who may ask.

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
Workday — all official public JSON APIs, and all ten hosts are in the
discovery search list. Keep those two lists in step: a board we can read but
never search for is only ever found by accident, and Keka and Darwinbox are
the platforms Indian employers use most.

Jina runs before Firecrawl everywhere. When Firecrawl went first, an unset key
or a spent budget took the whole path down and every company without a
supported ATS dropped to zero roles — the paid service was load-bearing for
the free product.

## Work queue

1. More boards: TalentRecruit (seen on Zepto), Gem (Hasura), Recruitee,
   Personio, Freshteam. Add each to `boardSearchDomains` as well as to the
   reader, or it will only ever be found from a careers page.
2. Board discovery stores no sector or stage, so those filters miss every
   company it finds. Enrich from the company's own site rather than guessing.
3. Re-enrich companies after first discovery; stage and description are frozen
   at first-seen values today. Jobs *are* re-synced.
4. `resolveCompanyWebsite` costs one search per newly-seen company. Fine at
   this volume, worth batching if discovery widens.

Known gaps, not blocking: filters are exact-match against a fixed dropdown;
`POST /api/companies/discover` is any-authenticated-user rather than
admin-only — it now has a bucket of its own (two, then one an hour) so one
account cannot spend the month in five minutes, but a limit is not an
authorisation check; `jobs` and `scrape_usages` have no migration file and
exist only via AutoMigrate.

## More detail

`SESSION_JOB_MAP_HANDOFF.md` — every bug, alternatives evaluated and rejected,
and why. `PROGRESS.md` — dated changelog.
