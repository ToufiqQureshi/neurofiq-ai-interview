# Decisions

Append-only. Each entry: what was decided, and what was *rejected* — the
rejected option is the part worth keeping, because it is the one that comes
back next month wearing a new name.

---

## 2026-09-04 — Every feature starts as five lines, not as code

**Decided.** Before any feature work: PROBLEM (the user's actual difficulty,
not the feature name), SCOPE (v1 in, and an explicit cut list), FLOW
(screen → action → result), SCREEN (one primary button, everything else
secondary), RISK (where the user gets confused, what breaks).

**Rejected:** starting from the code and writing the rationale afterwards.
One person maintains this; a feature whose cut list was never written has no
edge, and every session re-negotiates it from scratch.

**Standing rule:** if scope grows mid-build, stop and say so. Do not build it
and mention it later.

---

## 2026-09-04 — Design direction is the token layer, not a per-page choice

**Decided.** `frontend/src/index.css` is the single source: warm off-white
paper ground, serif headings, generous spacing, minimal borders, one accent
(`#15803d`). New surfaces use `bg-paper` / `bg-surface` / `text-ink` /
`border-line`. Never raw Tailwind defaults (`bg-gray-50`, `bg-slate-900`,
`text-blue-600`) — those are how a product ends up looking like a template.

**Rejected:** per-page palettes. The B2B revamp shipped a dark glassmorphic
landing page against a light token set; the result is 20 files that do not
agree with the theme they import.

**Not done yet, deliberately:** reconciling those 20 files, and pointing
`h1,h2,h3` at `--font-serif`. It is a real change with real visual risk across
every screen, so it gets its own scoped pass — not a drive-by inside a docs
commit.

---

## 2026-09-04 — `state.md` says what is true; `decisions.md` says why

**Decided.** Two files, different jobs. `state.md` is overwritten to match
reality and stays short. `decisions.md` is appended to and never edited.
`PROGRESS.md` stays the dated changelog; `SESSION_JOB_MAP_HANDOFF.md` stays the
deep pipeline post-mortem.

**Rejected:** one combined doc. It becomes a changelog nobody reads, and the
"why" gets buried under the "what".
