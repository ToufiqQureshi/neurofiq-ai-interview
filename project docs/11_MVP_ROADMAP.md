# MVP Roadmap — Build Order

This is the source of truth for sequencing. Claude Code / Antigravity
should not be asked to build Phase 2/3 items until the corresponding
Phase 1 items are working and deployed. Both the Go backend and Python
worker should be scaffolded early, even if the Python worker starts with
very little logic — this avoids retrofitting the split later.

## Phase 1 — MVP (core loop, validate the idea)

Goal: prove that "AI reads my actual GitHub code and asks smart, relevant
questions" is something candidates find valuable, before investing in
voice/camera polish.

```
1. Supabase schema (doc 02) + RLS policies

2. Go backend scaffold:
   - Project structure (mirroring StayChat's backend-go layout)
   - GORM models + migrations
   - Basic health check endpoint

3. Python AI worker scaffold:
   - FastAPI project structure (mirroring StayChat's backend/ layout)
   - /internal/health endpoint
   - Internal-secret auth middleware (doc 08) wired up from day one

4. Go: GitHub OAuth flow (doc 03) + token storage decision FINALIZED
   [OPEN DECISION — resolve before building: session-only vs encrypted DB]

5. Go: repo listing + deterministic analysis extraction
   (language detection, structure, commit stats — all in Go, doc 04)

6. Go → Python: /internal/analyze-repo wired up, with the hard 15k-token
   budget enforced on the Go side before the call is made (doc 04)

7. Go → Python: /internal/generate-questions (batch question generation,
   doc 04/07)

8. Go → Python: /internal/evaluate-answer (text-based scoring)
   - Evaluation rubric MUST be written and hardcoded into the Python
     worker's prompt before this ships: correctness, depth of reasoning,
     communication clarity, awareness of trade-offs — 0-10 each, weighted
     average as final score

9. Frontend: OAuth login → repo picker → text interview UI → report view

10. Session history list

11. Free tier usage limits enforced in Go, atomically (5 interviews/month,
    10 analyses/day — doc 07/08)

12. Camera Tier A (simple self-view preview, no recording, pure frontend)
    — cheap, ship alongside MVP for the "professional feel" boost

13. Go: Razorpay billing integration (upgrade to paid tier)

14. Basic rate limiting (Cloudflare rules + Go middleware, doc 08)

15. Deploy: Cloudflare Pages (frontend) + AWS App Runner ×2 (Go + Python,
    private networking between them) + Supabase (DB) — doc 09

16. Health checks + correlation ID logging wired up for BOTH services
    before calling this phase done (doc 12) — this is not optional
    polish, it's how you'll debug the split when something breaks
```

## Phase 1.5 — Once Phase 1 is validated with real users

```
17. Voice interview system (doc 05) — paid-tier gated
    - Go: WebSocket handling, Deepgram/ElevenLabs streaming
    - Python: /internal/voice-turn conversational logic
    - This is the highest-complexity integration point — build it only
      after Phase 1's Go↔Python pattern is proven stable in production
18. Session retry / "retry weak areas" feature
19. Email notifications (report ready)
20. Camera Tier B: recording + storage pipeline (Go-orchestrated, doc 06)
21. Consent + compliance layer (retention policy, delete, ToS/Privacy update)
```

## Phase 2 — Deferred until there's specific demand

```
22. Client-side proctoring (MediaPipe face/tab-switch detection → Go, doc 06)
23. Proctoring dashboard (only if pursuing B2B/recruiter angle)
24. Company dashboard (bulk candidate testing, B2B pivot)
25. Analytics/trends for candidates over time
26. Leaderboard system
27. In-browser code editor for live coding questions
28. Voice Activity Detection / interruption handling (beyond push-to-talk)
29. Hinglish voice support
30. Horizontal scaling of the Python worker independently of Go (only
    once AI processing volume, not request volume, is the bottleneck)
```

## Explicit Non-Goals for MVP (don't let scope creep back in)

- No ads on free tier (doc 07 — negligible revenue, hurts UX)
- No face-match-to-GitHub-avatar verification (unreliable, doc 06)
- No continuous server-side AI frame analysis for proctoring (cost-unsafe,
  doc 06 — use client-side MediaPipe only, and only in Phase 2)
- No unlimited repo analysis on free tier (hard daily caps always apply)
- No per-question Claude calls anywhere (always batched)
- No rate-limit or usage-check logic in the Python worker — Go is the only
  authorization gate, always (doc 08)
- No database credentials issued to the Python worker, ever

## Open Decisions to Resolve Before Coding Starts

1. **GitHub token storage** (doc 02): session-only vs encrypted DB.
   Recommendation: session-only for MVP.
2. **Evaluation rubric wording** (item 8 above): draft exact rubric text
   before building the scoring endpoint — this directly determines answer
   quality and consistency.
3. **Voice pricing threshold**: confirm ₹/$7-9/month is the launch price
   before wiring up Razorpay plans.
4. **Internal secret rotation policy** (doc 08): decide how the
   `X-Internal-Secret` shared between Go and Python gets stored/rotated
   (AWS Secrets Manager vs simple environment variable for MVP).
