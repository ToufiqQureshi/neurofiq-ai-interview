# State — what is actually true today

Short by design. If a line here is stale, it is a lie. Update it when work lands,
delete it when it stops being true.

_Last updated: 2026-09-04_

## Shipped and working

- **Repo interview** — GitHub OAuth, repo picker, extraction in Go, analysis in
  the Python worker, report + shareable public report.
- **Job Map** — 212 verified hiring companies, 3,550+ active roles, all from
  board discovery and the Common Crawl slug harvest. No hand-entered rows.
- **Jobs portal** (`/jobs`) — global search over live roles, company drawer,
  1-click "practice this role" into the mock interview.
- **Interview studio** — 2-panel layout, Monaco editor, PiP camera,
  tab-switch monitor.

## Known drift (design)

The token layer in `frontend/src/index.css` is already the taste we want:
`--color-paper #fbfbfa` (warm off-white), one accent (`--color-accent #15803d`),
a real serif stack (`--font-serif: Newsreader`). Two things ignore it:

1. `h1, h2, h3` in the base layer are bound to `--font-display` (Inter), not
   `--font-serif`. So "serif headings" is a token nobody uses — only
   `JobsPortal.tsx` reaches for `font-serif` by hand.
2. 20 of ~30 page/component files hard-code dark surfaces
   (`bg-black`, `bg-slate-9*`, `from-slate-9*`) instead of `bg-paper` /
   `bg-surface`. The B2B revamp introduced this; it was never reconciled.

Not fixed here — it is a separate, scoped piece of work. See `decisions.md`.

## Open, not started

Everything in the **Work queue** section of `CLAUDE.md` (more board readers,
sector/stage enrichment, re-enrichment after first discovery).
