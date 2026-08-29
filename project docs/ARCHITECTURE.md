# neurofiq.in— Architecture Document

Status: Pre-build design doc. No code written yet.

## 1. High-Level Flow

```
Candidate → GitHub OAuth → Select Repo → Analyze Code → Generate Questions
    → Interview Session (Q&A) → AI Evaluation → Report → Session History
```

## 2. System Components

```JavaScript
┌─────────────┐      ┌──────────────┐      ┌─────────────┐
│  React + TS │◄────►│  FastAPI     │◄────►│  Supabase   │
│  Frontend   │      │  Backend     │      │  (Postgres) │
└─────────────┘      └──────┬───────┘      └─────────────┘
                             │
                 ┌───────────┼───────────┐
                 ▼           ▼           ▼
            GitHub API   Claude API   Redis/Cache
                                       (optional, MVP:
                                        Postgres table
                                        as cache)
```

- **Frontend**: React + TypeScript, talks only to FastAPI (never directly to GitHub or Claude).
- **Backend**: FastAPI — owns all GitHub API calls, all Claude API calls, all business logic.
- **Supabase**: Postgres for persistent data + built-in auth session table (candidate identity keyed off GitHub OAuth).
- **Claude API**: question generation + answer evaluation.
- **GitHub API**: repo listing, file tree, file contents, commit history.

## 3. Data Models (Supabase / Postgres)

### `users`

| column          | type           | notes                                                      |
| --------------- | -------------- | ---------------------------------------------------------- |
| id              | uuid, pk       |                                                            |
| github_id       | bigint, unique | GitHub numeric user id                                     |
| github_username | text           |                                                            |
| email           | text, nullable | from GitHub if public                                      |
| avatar_url      | text           |                                                            |
| role            | text           | `candidate` \| `company` (Phase 2 unlocks `company`) |
| created_at      | timestamptz    |                                                            |
| last_login_at   | timestamptz    |                                                            |

**GitHub OAuth token storage: OPEN DECISION — not finalized.**
Two options on the table, pick one before building auth:

1. **Session-only**: token lives only in server-side session (Redis or signed cookie), never hits DB. Re-auth needed each session. Safer, simpler compliance story.
2. **Encrypted DB storage with TTL**: store `access_token` encrypted (pgcrypto or app-level AES) in a separate `github_tokens` table with `expires_at`, background job purges expired rows. Allows background re-analysis without re-prompting user, but bigger security surface + GDPR data-minimization concern.

Until decided, backend should be built so the token lookup is behind a single interface (e.g. `get_github_token(user_id) -> str`) so swapping storage strategy later doesn't touch calling code.

### `github_profiles` (cache)

| column         | type              | notes                                                                                                                |
| -------------- | ----------------- | -------------------------------------------------------------------------------------------------------------------- |
| id             | uuid, pk          |                                                                                                                      |
| user_id        | uuid, fk → users |                                                                                                                      |
| repo_full_name | text              | e.g.`octocat/hello-world`                                                                                          |
| analysis_json  | jsonb             | languages, framework stack, project type, architecture patterns, complexity, db usage, dependencies, commit insights |
| analyzed_at    | timestamptz       |                                                                                                                      |
| expires_at     | timestamptz       | `analyzed_at + 7 days` (TTL cache)                                                                                 |

Unique constraint on `(user_id, repo_full_name)`.

### `questions_bank`

| column                   | type               | notes                                                                   |
| ------------------------ | ------------------ | ----------------------------------------------------------------------- |
| id                       | uuid, pk           |                                                                         |
| repo_full_name           | text, nullable     | null if generic/reusable question                                       |
| source_github_profile_id | uuid, fk, nullable | which analysis produced it                                              |
| question_text            | text               |                                                                         |
| category                 | text               | architecture\| performance \| security \| best_practices \| code_review |
| difficulty               | text               | easy\| medium \| hard                                                   |
| code_reference           | jsonb, nullable    | `{file, line_range, snippet}`                                         |
| reusable                 | boolean            | true if generic enough to reuse across candidates                       |
| created_at               | timestamptz        |                                                                         |

### `interview_sessions`

| column            | type                        | notes                                |
| ----------------- | --------------------------- | ------------------------------------ |
| id                | uuid, pk                    |                                      |
| user_id           | uuid, fk → users           |                                      |
| github_profile_id | uuid, fk → github_profiles |                                      |
| mode              | text                        | timed\| untimed \| by_difficulty     |
| status            | text                        | in_progress\| completed \| abandoned |
| started_at        | timestamptz                 |                                      |
| completed_at      | timestamptz, nullable       |                                      |

### `session_questions` (join: which questions were asked, in what order, answers)

| column              | type                           | notes                                    |
| ------------------- | ------------------------------ | ---------------------------------------- |
| id                  | uuid, pk                       |                                          |
| session_id          | uuid, fk → interview_sessions |                                          |
| question_id         | uuid, fk → questions_bank     |                                          |
| order_index         | int                            |                                          |
| candidate_answer    | text                           |                                          |
| ai_feedback         | text                           |                                          |
| score               | numeric                        | e.g. 0–10                               |
| follow_up_questions | jsonb, nullable                | AI-generated follow-ups + answers if any |
| answered_at         | timestamptz, nullable          |                                          |

### `candidate_reports`

| column                  | type                                   | notes                  |
| ----------------------- | -------------------------------------- | ---------------------- |
| id                      | uuid, pk                               |                        |
| session_id              | uuid, fk → interview_sessions, unique | one report per session |
| overall_score           | numeric                                |                        |
| strengths               | jsonb                                  | list of strings        |
| weaknesses              | jsonb                                  | list of strings        |
| improvement_suggestions | jsonb                                  | list of strings        |
| generated_at            | timestamptz                            |                        |

## 4. API Endpoints (FastAPI)

### Auth

- `GET /auth/github/login` → redirect to GitHub OAuth
- `GET /auth/github/callback` → exchange code, create/update `users` row, establish session
- `POST /auth/logout`

### Repos

- `GET /repos` → list candidate's GitHub repos (via GitHub API, using their token)
- `POST /repos/{owner}/{repo}/analyze` → trigger analysis; checks `github_profiles` cache first (7-day TTL), else runs analysis pipeline and stores result
- h

### Interview

- `POST /interviews` → body: `{repo_full_name, mode}` → creates session, batch-generates 5–10 questions via Claude, stores in `questions_bank` + `session_questions`
- `GET /interviews/{session_id}` → session state + questions so far
- `POST /interviews/{session_id}/answers` → body: `{question_id, answer_text}` → sends to Claude for scoring/feedback, may return follow-up question, updates `session_questions`
- `POST /interviews/{session_id}/complete` → finalizes session, triggers report generation
- `GET /interviews/{session_id}/report` → returns `candidate_reports` row

### History

- `GET /users/me/sessions` → list past sessions for candidate

## 5. Code Analysis Pipeline (backend)

1. Fetch repo file tree via GitHub API (respecting rate limits — use conditional requests / ETags).
2. Filter to relevant files (skip `node_modules`, lockfiles, binaries, vendored code).
3. Detect languages/frameworks from file extensions + manifest files (`package.json`, `requirements.txt`, `pyproject.toml`, `go.mod`, etc.) — **this step is cheap/deterministic, do NOT use Claude for it**.
4. Sample a bounded set of representative files (entry points, largest modules, config files) — needed because full repos won't fit in context economically.
5. Send structured summary (not raw full repo) to Claude for: architecture pattern detection, complexity rating, code quality signals.
6. Pull commit history via GitHub API (`/repos/{owner}/{repo}/commits`, `/stats/contributors`) for collaboration/commit-pattern insights — deterministic stats, no Claude needed.
7. Store combined result as `analysis_json` in `github_profiles`.

## 6. Question Generation

- Batch call: one Claude request produces 5–10 questions across difficulty/category in one structured JSON response (not one request per question) — controls cost.
- Prompt includes: analysis summary + a handful of actual code snippets (file path + line range + snippet), not the whole repo.
- Output schema enforced (JSON mode / structured output) so parsing is deterministic.

## 7. Answer Evaluation

- Each answer scored independently against a **rubric** passed in the prompt (this is currently undefined in the original plan — needs to be written before evaluation prompts are built). Suggested rubric axes: correctness, depth of reasoning, communication clarity, awareness of trade-offs.
- Follow-ups generated only when score is ambiguous/borderline, to control token spend — not on every answer.

## 8. Caching & Cost Controls

- `github_profiles.expires_at` — 7-day TTL, checked before re-running analysis.
- `questions_bank.reusable` — generic questions (not tied to a specific repo's exact code) can be served to other candidates with similar stacks, cutting Claude calls over time.
- Batch generation (5–10 questions/call) instead of per-question calls.
- Deterministic steps (language detection, commit stats, file filtering) never go through Claude.

## 9. Security Considerations

- GitHub token handling: see open decision in §3. Whichever is chosen, token must never reach the frontend.
- All GitHub/Claude API calls happen server-side only.
- Rate-limit `POST /interviews` and `POST /repos/.../analyze` per user to prevent cost abuse.
- Sanitize any code snippets before rendering in frontend (stored as text, rendered escaped — avoid XSS via candidate's own repo content, e.g. a repo containing `<script>` in a filename or README).
- Row-level security in Supabase: candidates can only read their own `interview_sessions`, `session_questions`, `candidate_reports`.

## 10. GDPR Considerations

- Data retention policy: not yet defined — need explicit deletion flow (`DELETE /users/me` cascades to all owned rows) and a stated retention period for `github_profiles` cache and session transcripts.
- EU users: need consent capture at OAuth step for storing analyzed code snippets.

## 11. MVP Build Order (Phase 1)

1. Supabase schema (tables above) + RLS policies
2. FastAPI: GitHub OAuth flow + token storage decision finalized
3. FastAPI: repo listing + analysis pipeline (deterministic parts first, Claude last)
4. FastAPI: batch question generation endpoint
5. FastAPI: answer submission + scoring endpoint
6. Frontend: OAuth login → repo picker → interview UI → report view
7. Session history list

## 12. Explicitly Deferred (Phase 2)

Video recording, in-browser code editor, company dashboard, analytics/trends, leaderboard.
