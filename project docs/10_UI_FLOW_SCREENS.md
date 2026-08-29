# UI Flow — Screen by Screen

Note: this doc is backend-agnostic — the frontend talks only to the Go
backend's public API (doc 03) regardless of what happens behind it, so this
flow is unaffected by the Go/Python split.

## Complete Screen Sequence

```
1. Landing Page
       ↓
2. Auth Choice
   "Continue with Google" | "Continue with GitHub"
       ↓
3. GitHub Connect (separate step, only if not already connected)
   - If logged in via Google → "Connect GitHub to analyze your code"
     (requests `repo` scope explicitly)
   - If logged in via GitHub directly → already connected, skip
       ↓
4. Repo Selection
   - Search/filter box
   - List of repos (forks hidden by default, sorted by recently updated)
   - Manual override: "Repo not showing? Paste URL"
   - "Pin" action (subject to free=2 / paid=10 limit)
       ↓
5. [If repo is large/monorepo] Scope Selection
   "This repo is large. Which part should the interview focus on?"
   [ ] Backend  [ ] Frontend  [ ] Full structural overview
       ↓
6. Analysis Progress
   Step indicator: "Fetching file structure..." → "Detecting stack..."
   → "Analyzing architecture..." → "Ready!"
       ↓
7. Analysis Summary
   - Detected stack (e.g. React + FastAPI + Postgres)
   - Complexity rating
   - Files analyzed (e.g. "38/230 files sampled")
   - "Start Interview" button (choice: Text or Voice, voice paid-gated)
       ↓
8. Interview Session
   Text mode: question card + text input, progress "Question 3/7"
   Voice mode: waveform animation, hold-to-speak mic button (doc 05)
   Camera preview: optional self-view box, consent required if recording (doc 06)
       ↓
9. Report
   - Overall score
   - Strengths / weaknesses / improvement suggestions
   - "Retry weak areas" (Phase 2)
       ↓
10. Session History (from dashboard, any time)
```

## Voice Interview Screen (detail)

```
┌────────────────────────────────────────┐
│     [Waveform animation - AI speaking]  │
│         "Tell me about..."              │
│      (subtitle/transcript toggle)       │
│  ─────────────────────────────────────  │
│         🎤 [Hold to Speak]              │
│    Question 3/7        [Skip] [Mute]    │
└────────────────────────────────────────┘
```

## Camera Preview (detail — Tier A, self-view only)

```
┌────────────────────────────────────────┐
│  [Candidate's camera feed - small box]  │  ← self-view, like Zoom mirror
│                                          │
│     [Waveform animation - AI speaking]  │
│         "Tell me about..."              │
│         🎤 [Hold to Speak]              │
│    Question 3/7        [Camera: ON/OFF] │
└────────────────────────────────────────┘
```

## Consent Screen (required before any recording, Tier B)

```
┌────────────────────────────────────────┐
│  Before we start recording:             │
│                                          │
│  ☐ I consent to video recording for     │
│    interview practice purposes. Video   │
│    will be stored for 30 days and can   │
│    be deleted anytime.                  │
│                                          │
│  [Continue without recording]  [Agree]  │
└────────────────────────────────────────┘
```

## Design Principles

- Minimal distraction — this is a focused practice tool, not a gamified
  app. No unnecessary animations/badges competing with the interview
  content.
- Every AI-processing step should show visible progress (never a bare
  spinner with no context) — candidates should always know what's
  happening (analyzing code, generating questions, scoring answer, etc.).
  This matters more in this architecture since a Go→Python round-trip adds
  a small but real delay the UI should account for gracefully.
- Free-tier limits should be visible but not naggy — a small persistent
  "3/5 interviews remaining this month" indicator, with an upgrade CTA only
  shown when the limit is actually hit.
