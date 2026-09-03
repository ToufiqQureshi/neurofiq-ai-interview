# Slug Harvest — Session Handoff

**Session:** 2026-09-03
**Branch:** `slug-harvest` (one commit, `da1e124`, on top of PR #5's branch `claude/supabase-jobs-data-check-9gv2v6`)
**For:** whoever picks this up next.

`PROGRESS.md` has the dated one-liners. This file has the reasoning: what was
measured, what was wrong, what got built, and what is still open.

---

## 1. The question this session started from

The session began as a review of **OpenPostings** (`github.com/imperiex26/OpenPostings`),
an unrelated open-source Indian job aggregator that had been cloned into the
repo root. It holds ~24,000 companies and ~336,000 postings against our ~330
companies and ~4,000 jobs, and the obvious question was what to take from it.

The answer turned out not to be its code.

### What OpenPostings actually has

| | NeuroFIQ | OpenPostings |
|---|---|---|
| Companies with a board | 157 | 24,281 |
| Jobs | 2,969 | ~336,000 |
| **Jobs per company** | **~17** | **~14** |

Jobs-per-company is the same. The entire difference is how many board slugs
each side holds. And of their 24,281 slugs, **87.1% sit on the four ATSes we
already read** (Greenhouse, Workday, Lever, Ashby). Adding ATS readers would
have bought almost nothing.

Cross-referenced directly against our directory:

- 93 of our 270 companies also appear in their list, but 92 of those are on
  providers we already read. Exactly **one** (Piston Technologies, iCIMS) is on
  an ATS we cannot read.
- Of our 118 boardless companies, **one** matched their list — and that one is
  a false positive (our `dice.tech` against their Dice.com job board).
- All 77 of our zero-job boardless companies came from the **deleted LLM
  discovery agent** (`source <> 'board-search'`), not from the current pipeline.

So their advantage is a *slug list*, not better readers. Their list cannot be
copied — the repo ships **no LICENSE file**, and `package.json` declares none,
which is all-rights-reserved by default. That is the same reasoning that kept
bangalorestartupmap's dataset out of this project.

---

## 2. What was built, and why

### The constraint being removed

Discovery pays a metered search per company. Measured from `scrape_usages` and
`companies.created_at` for September:

```
293 Exa searches  ->  145 companies stored   =  2.02 searches/company
```

At an 800/month free tier that caps the month near 400 companies however fast
the cron runs — and 293 of the 800 were already gone within two days.

### The insight

Boards are not hidden. Every one is a public page a crawler has already
indexed, and the slug this pipeline needs is in the URL. So the list can be
**read** rather than rediscovered one paid query at a time.

**Common Crawl** publishes a URL index monthly, free, keyless. One index
yields, measured:

```
job-boards.greenhouse.io   3,954     *.myworkdayjobs.com   1,166
apply.workable.com         3,157     careers.smartrecruiters 415
jobs.ashbyhq.com           2,770     *.keka.com              206
boards.greenhouse.io       1,781     *.darwinbox.in/.com      51
                                     jobs.lever.co             1
                                     ------------------------------
                                     13,501 slugs, ~20 requests, ₹0
```

Indexes are not copies of each other. Taking 2026-30 alongside 2026-34 added
**204 Keka slugs and 745 Greenhouse ones** the newer index did not have —
boards open and close between crawls, so reading several indexes compounds.
127 indexes exist, back to 2008, published roughly monthly.

### Files

```
backend-go/services/slug_harvest.go              candidate type, dedupe,
                                                 directoryIndex, admission gate,
                                                 RunHarvest orchestrator
backend-go/services/slug_source_commoncrawl.go   CDX reader          (jobs)
backend-go/services/slug_source_register.go      startup register    (company info)
backend-go/services/slug_harvest_test.go         tests
backend-go/services/board_discovery.go   +77     India filter strengthened
backend-go/main.go                       +56     -harvest-crawl / -harvest-register
```

### The shape

```
Common Crawl ──┐
               ├──► slugCandidate ──► dedupeCandidates ──► admitCandidate ──► store
Startup reg ───┘                      (richer copy wins)
```

**Two sources, one gate.** No guard logic is duplicated. `admitCandidate`
orders its checks so free ones run first, which is what keeps a run of
thousands cheap:

```
validATSSlug → boardSlugIsAdmissible → already have this board?   0 requests
boardRowIsAdmissible → sharedBoardRe / aggregatorBoardRe          0 requests
FetchATSJobs                                                      1 free board call
dropTalentPools → maxBoardRoles → firstIndianLocation             0 requests
duplicate by name → attachBoardTo                                 0 requests
guessCompanyWebsite (from PR #5)                                  free, no search
duplicate by domain → store
```

A harvest **never** falls through to `resolveCompanyWebsite`. A run this size
through that path would drain the year's budget.

### Running it

```bash
cd backend-go
go run . -harvest-crawl -indexes 1 -limit 50   # smoke test
go run . -harvest-crawl -indexes 4             # full backfill
go run . -harvest-register -limit 200          # company info path
```

It exits instead of serving, so it cannot collide with a running instance.
Cadence: **monthly**, matching Common Crawl's publish cycle. Running it daily
returns the same data. It is not what keeps jobs fresh — the existing hourly
`RunJobSync` does that; the harvest only adds new companies.

---

## 3. Bugs found and fixed — all three came from running it

**Almost nothing here was visible before a live run.** That is the pattern
worth carrying forward: each of these looked correct in review.

### 3.1 The page walk stopped on the first CDX error

Requesting pages back to back had Common Crawl answer 504, and a 504 was
treated as "no more pages":

```
job-boards.greenhouse.io    748 slugs   (a paced walk finds 3,954)
boards.greenhouse.io          0 slugs   (its *first* page 504'd)
```

A 400 genuinely is the end of the walk; a 5xx is the index saying it is busy.
Now separated, with three retries, backoff, and a 2s gap between pages.

### 3.2 A failed page still discarded every page after it

Even with retries, the loop `break`ed on a page that exhausted them. Ashby
collected **345 slugs where its two pages hold 2,955** — verified by fetching
both pages directly and running the exact Go regex and filters over them.
A page failure now logs and `continue`s.

The summary line now reports **rows and failed pages**, not just the slug
count. A host returning fewer slugs than usual is either genuinely smaller or
was half-read, and the slug count alone cannot tell those apart — which is
precisely the ambiguity that hid this.

### 3.3 `looksIndian` was wrong in both directions

Measured against 30,000 real board postings:

- **Too narrow.** Boards write `"Mumbai MH"` and `"Kutch - Gujarat"`. Those
  read as not-Indian, and when they are a company's only Indian roles,
  `firstIndianLocation` returns `""` and the **whole company is rejected**.
  Fixed by adding the 36 states and UTs (0.1% of rows were affected).
- **Too wide.** `"Bangalore, Mexico"` is a real row and passed the city test.
  Fixed with a foreign-country rule that disqualifies a location **only when
  nothing in it names India or an Indian state** — so
  `"Bengaluru, Karnataka, India; Pleasanton, United States"` still counts
  (0.37% of matching rows carry a foreign marker; most name India too).

### 3.4 Talent-pool postings are not vacancies

The first run surfaced Affinidi's **"Be Part of our Talent Community"** as that
company's only Indian role — so the company would have entered the directory
advertising a mailing list. It is a posting by every structural measure: its
own id, its own apply URL, a location. Nothing upstream rejected it.

`dropTalentPools` matches the phrase, narrowly enough that "Talent Acquisition
Specialist" and "Application Security Engineer" survive. It lives in
`slug_harvest.go` rather than `looksLikeRoleTitle` because only the harvest has
met these so far.

### 3.5 `findDuplicateCompany` does not scale to a harvest

It reads the **whole companies table on every call**. Free at five calls a
tick; a table scan per candidate at 13,501. `directoryIndex` builds the same
answer once and updates it as the run goes, so two candidates for one business
inside a single run cannot both be written.

### 3.6 Darwinbox puts a carriage return inside the location

`"Bengaluru, Karnataka, India"` reached the company card exactly as written.
`tidyLocations` collapses internal whitespace and repairs the punctuation that
collapsing strands. Every producer of a location passes through the harvest, so
the cleanup belongs there rather than in each reader.

### 3.7 Greenhouse embed URLs collapse every company to the slug "embed"

A company that *embeds* its board rather than linking it serves
`boards.greenhouse.io/embed/job_board?for=observeai` — the company is in the
query string, and the path segment is the literal word `embed`.
`greenhouseLinkRe` reads the path, so it returns `"embed"` for every such URL.

`nonSlugSegments` rejects it, so nothing wrong is ever stored — the symptom is
a company that simply is never found. The register run made it visible:

```
startup register: Razorsharp Technologies -> observe.ai (greenhouse/embed)
```

Fixed for the harvest via `ccGreenhouseSlug`, which prefers the query
parameter and falls back to the path.

> **⚠ The same gap exists in `scanForATS` (`job_service.go`), which means
> `DetectATS` cannot read an embedded Greenhouse board off a company's own
> careers page either.** That is a live bug in the main discovery path, not
> just the harvest. It was deliberately not fixed on this branch because
> `job_service.go` carries unrelated in-progress work (`ListGlobalJobs`) that
> should not be dragged into this PR. **Fix it separately** — one alternation
> added to `greenhouseLinkRe`, plus a test.
---

## 4. Verified against the live directory

Two runs against production Supabase.

```
run 1 (before the paging fix)
candidates=6436  skipped=88   dead=265   not-india=979   duplicate=18  attached=0  stored=21

run 2 (after)
candidates=7340  skipped=453  dead=3647  not-india=3087  duplicate=63  attached=3  stored=87
```

| | before | after |
|---|---|---|
| Companies | 328 | **441** (+113) |
| Jobs | 3,953 | **5,048** (+1,095) |
| Searches spent | 331 | 334 |

The +3 searches came from the ordinary cron running in parallel. **The harvest
spent zero.** `attached=3` means three already-stored boardless companies
gained a board for free.

**India filtering verified.** Every stored role was checked; 14 rows looked
foreign to an ad-hoc regex and all 14 were Goa, Puducherry, Surat and Nagpur —
the verification regex was wrong, not the filter. **Zero leaks.**

---

## 5. Alternatives evaluated and rejected

Documented so nobody re-researches these.

| Option | Verdict |
|---|---|
| **OpenPostings' `companies.csv`** (24,281 slugs) | ❌ No LICENSE file, no `license` field — all rights reserved. Same reasoning that kept bangalorestartupmap out. |
| **OpenPostings' 9 extra ATS readers** | ❌ Measured: would unlock exactly **1** of our 270 companies. Their 78% "extra ATS" share is their own universe (Kotak 9,633 roles, Tata Capital 5,037 — BFSI bulk our `maxBoardRoles=2000` rejects anyway). |
| **indianstartupmap.com as a slug source** | ❌ Measured: 50 random DPIIT companies → 100% had a website, **50% resolved**, **0 had an ATS**. 270k pages ≈ 810,000 requests for ~nothing. Same mistake as the deleted agent: find a company first, then hope it hires. |
| **indianstartupmap funded subset** | ⏸️ Better — 85 YC/Techstars/Antler/Plug-and-Play companies → **4.7% had a board** (Bolna/Ashby, Observe.ai/Greenhouse, BharatX/Workable, rePurpose/Lever). Implemented as `-harvest-register`. Value is the *information*, not the volume. |
| **Expanding `indiaLocationHints` with more cities** | ❌ Measured 0.1% gain over 27,760 Indian postings. Not worth it. (The states *were* worth it — different failure.) |
| **OpenPostings' 73,473-row industry lookup** | ❌ Too big for the "keep it small" constraint; the keyword map covers what we show. |

### On indianstartupmap specifically

Its data is **Indian government open data** (DPIIT / Startup India register,
iStart Rajasthan, Kerala Startup Mission, Seed Fund Scheme) — not a private
curated dataset. `robots.txt` allows `/company/*` and disallows `/api/`, and
states the dataset is "republishable with attribution". Its licence page says
there is **no bulk download and no public API**.

Each company page publishes clean schema.org JSON-LD:

```json
name, description, sector
address: { locality, region, country }
geo:     { latitude, longitude }          ← 54-58% of pages, REAL coordinates
identifier: [ CIN, DPIIT number, incorporated year ]
sameAs:  [ website ]
```

Two things it gives that no board API can:

1. **Real coordinates.** `fallbackCoordsForArea()` currently *invents* them
   from a hash of the company name plus jitter. That is the weakest data in the
   product.
2. **Legal name → brand domain.** Voxlabs *is* Bolna, RazorSharp Technologies
   *is* Observe.ai, Aurorax *is* BharatX. Nothing matching on name alone
   connects those.

Caveat the site states itself: most of its 47,744 funding signals are
**self-declared** — "the company ticked a box on its own Startup India profile
saying it had raised money, and nobody checked." The 4.7% board rate was
measured on the *attested* slice (named on an accelerator portfolio), which is
why `registerAccelerators` defaults to that slice.

---

## 6. Still open

1. **Collection still under-reaches its own baseline.** Run 2 collected 7,340
   candidates where direct measurement of the same index finds 13,501. §3.2's
   fix should close most of it but **has not been verified by a full re-run**.
   Check the new `(N rows, M pages failed)` log line against the table in §2.
2. **Lever is absent from Common Crawl** — one block in the whole index, vs six
   for Greenhouse. Lever boards must keep coming from discovery's search. Do
   not remove that path.
3. **Workday is deliberately not harvested.** Its slug is `tenant:region:site`
   and the site id is not in the URL — `DetectATS` probes the live API up to
   five candidates deep. Doing that for 1,166 crawled tenants is a different
   kind of run and belongs behind its own switch.
4. **`-harvest-register` verification.** Implemented and building; live-run
   result should be recorded here.
5. **DPIIT stage enrichment is a separate task.** `enrichment.go` says "Funding
   stage is deliberately NOT enriched. There is no free source for it." The
   Seed Fund Scheme is a free source for the Indian-registered subset. But
   matching our companies to that register is unreliable through the site —
   slug guessing 404'd on 6 of 8 tried, and search matches only from the *start
   of the registered legal name*. Go to the government source directly rather
   than the site.
6. **Harvested names are slug-derived** (`amtechsoftware`, `apexit`). The
   existing enrichment pass (`looksLikeSlugFallback` + `og:site_name`) is
   designed to fix exactly this and will, on its own schedule.

---

## 7. Process notes for the next session

- **The push could not be completed from the agent.** `git ls-remote` (read)
  works instantly; `git push` hangs with no output even with
  `GIT_TERMINAL_PROMPT=0` and the credential helper disabled. Git Credential
  Manager appears to want a GUI interaction. The push has to be run by a human:
  ```
  git push origin slug-harvest:claude/supabase-jobs-data-check-9gv2v6
  ```
- **PR #5's branch is `gofmt`-unclean locally, but only because of CRLF.** The
  repo stores LF and `core.autocrlf=true` converts on checkout, so `gofmt -l .`
  flags everything on Windows while CI on Linux is fine. Do not "fix" it.
- **`job_service.go` was deliberately left untouched.** It carries in-progress
  work (`ListGlobalJobs`, the `/api/jobs` portal) that is not part of this
  branch. The talent-pool guard was moved into `slug_harvest.go` specifically to
  avoid mixing the two.
- **A `git stash -u` was run mid-session while uncommitted work was in the
  tree** (`JobsPortal.tsx`, `AiMatchSummaryCard.tsx`, `kimi_suggested.md`). The
  pop was clean and nothing was lost, but it was careless — a conflict would
  have stranded that work in a stash. Do not stash someone else's tree.
- **`PROGRESS.md` in this commit carries changelog entries for uncommitted
  work** (the `/jobs` portal), because those entries were already in the file.
  The changelog is therefore slightly ahead of the code.
- **Do not start the backend casually.** `main.go` runs `RunDiscoveryRotation`
  at boot, which spends real metered searches. The harvest flags exit before
  the server starts, which is why they are safe.
