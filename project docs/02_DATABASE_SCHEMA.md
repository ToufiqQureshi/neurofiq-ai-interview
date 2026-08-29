# Database Schema (Supabase / Postgres)

**Owned exclusively by the Go backend.** The Python AI worker has no
database credentials and never connects to Postgres directly — it only
ever receives/returns JSON over the internal Go↔Python API (doc 03).

All tables use `uuid` primary keys via `gen_random_uuid()`. All timestamps
are `timestamptz`. Row-Level Security (RLS) must be enabled on every table
containing user data — candidates can only read/write their own rows.

Go side: use GORM (as in StayChat) or pgx for queries/migrations.

## `users`

| column           | type                   | notes                                                  |
| ---------------- | ---------------------- | ------------------------------------------------------ |
| id               | uuid, pk               |                                                        |
| github_id        | bigint, unique         | nullable if user only did Google auth so far           |
| github_username  | text, nullable         |                                                        |
| google_id        | text, unique, nullable |                                                        |
| email            | text, nullable         |                                                        |
| avatar_url       | text                   |                                                        |
| plan_type        | text                   | `free` \| `paid`, default `free`                 |
| role             | text                   | `candidate` \| `company` (Phase 2 unlocks company) |
| github_connected | boolean                | false until GitHub OAuth scope granted                 |
| created_at       | timestamptz            |                                                        |
| last_login_at    | timestamptz            |                                                        |

**[DECISION CLOSED] GitHub OAuth token storage:** We will use **Supabase Vault** (`pgsodium`).

- Tokens will be stored encrypted at rest via Supabase Vault's transparent column encryption or `vault.secrets` table.
- This allows background re-analysis without re-prompting the user, while keeping encryption/decryption logic out of the Go application code and maximizing security.

Build the token lookup behind one Go interface —
`GetGithubToken(userID string) (string, error)` — so the exact Vault integration details are abstracted from the calling code.

## `github_profiles` (repo analysis cache)

| column         | type              | notes                                                                                                           |
| -------------- | ----------------- | --------------------------------------------------------------------------------------------------------------- |
| id             | uuid, pk          |                                                                                                                 |
| user_id        | uuid, fk → users |                                                                                                                 |
| repo_full_name | text              | e.g.`octocat/hello-world`                                                                                     |
| repo_size_kb   | integer           | from GitHub API, used to pick analysis strategy                                                                 |
| strategy_used  | text              | `full_scan` \| `smart_sample` \| `structural_only`                                                        |
| analysis_json  | jsonb             | languages, frameworks, project type, architecture patterns, complexity, db usage, dependencies, commit insights |
| analyzed_at    | timestamptz       |                                                                                                                 |
| expires_at     | timestamptz       | `analyzed_at + 7 days`                                                                                        |

Unique constraint on `(user_id, repo_full_name)`.

## `pinned_repos`

| column         | type              | notes |
| -------------- | ----------------- | ----- |
| id             | uuid, pk          |       |
| user_id        | uuid, fk → users |       |
| repo_full_name | text              |       |
| pinned_at      | timestamptz       |       |

Unique on `(user_id, repo_full_name)`. Enforce max-pin-count at the Go
application level: free tier = 2, paid tier = 10.

## `questions_bank`

| column                   | type               | notes                                                                                |
| ------------------------ | ------------------ | ------------------------------------------------------------------------------------ |
| id                       | uuid, pk           |                                                                                      |
| repo_full_name           | text, nullable     | null if generic/reusable                                                             |
| source_github_profile_id | uuid, fk, nullable | which analysis produced it                                                           |
| question_text            | text               |                                                                                      |
| category                 | text               | `architecture`\|`performance`\|`security`\|`best_practices`\|`code_review` |
| difficulty               | text               | `easy`\|`medium`\|`hard`                                                       |
| code_reference           | jsonb, nullable    | `{file, line_range, snippet}`                                                      |
| tech_stack               | text[]             | for reusable-question matching                                                       |
| reusable                 | boolean            | true if generic enough to serve other candidates                                     |
| created_at               | timestamptz        |                                                                                      |

## `interview_sessions`

| column            | type                        | notes                                         |
| ----------------- | --------------------------- | --------------------------------------------- |
| id                | uuid, pk                    |                                               |
| user_id           | uuid, fk → users           |                                               |
| github_profile_id | uuid, fk → github_profiles |                                               |
| interview_type    | text                        | `text` \| `voice`                         |
| mode              | text                        | `timed`\|`untimed`\|`by_difficulty`     |
| camera_enabled    | boolean                     |                                               |
| status            | text                        | `in_progress`\|`completed`\|`abandoned` |
| started_at        | timestamptz                 |                                               |
| completed_at      | timestamptz, nullable       |                                               |

## `session_questions`

| column              | type                           | notes                                    |
| ------------------- | ------------------------------ | ---------------------------------------- |
| id                  | uuid, pk                       |                                          |
| session_id          | uuid, fk → interview_sessions |                                          |
| question_id         | uuid, fk → questions_bank     |                                          |
| order_index         | int                            |                                          |
| candidate_answer    | text                           |                                          |
| ai_feedback         | text                           |                                          |
| score               | numeric                        | 0–10                                    |
| follow_up_questions | jsonb, nullable                | AI-generated follow-ups + answers if any |
| answered_at         | timestamptz, nullable          |                                          |

## `candidate_reports`

| column                  | type                                   | notes                  |
| ----------------------- | -------------------------------------- | ---------------------- |
| id                      | uuid, pk                               |                        |
| session_id              | uuid, fk → interview_sessions, unique | one report per session |
| overall_score           | numeric                                |                        |
| strengths               | jsonb                                  | list of strings        |
| weaknesses              | jsonb                                  | list of strings        |
| improvement_suggestions | jsonb                                  | list of strings        |
| generated_at            | timestamptz                            |                        |

## `interview_usage` (free tier monthly counter — single source of truth)

| column          | type              | notes              |
| --------------- | ----------------- | ------------------ |
| id              | uuid, pk          |                    |
| user_id         | uuid, fk → users |                    |
| month_year      | text              | `YYYY-MM` format |
| interview_count | integer           | default 0          |
| created_at      | timestamptz       |                    |
| updated_at      | timestamptz       |                    |

Unique on `(user_id, month_year)`. Resets automatically each month by
design (new row per month) — no cron job needed. **Increment via a single
atomic Postgres query from Go** (`UPDATE ... SET count = count + 1 ...`
or `INSERT ... ON CONFLICT DO UPDATE`) to avoid race conditions when
concurrent requests come in — see doc 08.

## `analyze_usage` (daily repo-analysis counter, abuse prevention)

| column  | type              | notes     |
| ------- | ----------------- | --------- |
| id      | uuid, pk          |           |
| user_id | uuid, fk → users |           |
| date    | date              |           |
| count   | integer           | default 0 |

Unique on `(user_id, date)`. Free tier: 10/day. Paid tier: 50/day.

## `interview_recordings` (see doc 06)

| column               | type                           | notes                                 |
| -------------------- | ------------------------------ | ------------------------------------- |
| id                   | uuid, pk                       |                                       |
| session_id           | uuid, fk → interview_sessions |                                       |
| video_url            | text                           | S3/Supabase Storage URL               |
| duration_seconds     | integer                        |                                       |
| file_size_mb         | numeric                        |                                       |
| storage_status       | text                           | `processing`\|`ready`\|`failed` |
| consent_given        | boolean                        | not null                              |
| retention_expires_at | timestamptz                    | auto-delete date                      |
| created_at           | timestamptz                    |                                       |

## `proctoring_events` (see doc 06)

| column               | type                           | notes                                                                            |
| -------------------- | ------------------------------ | -------------------------------------------------------------------------------- |
| id                   | uuid, pk                       |                                                                                  |
| session_id           | uuid, fk → interview_sessions |                                                                                  |
| event_type           | text                           | `tab_switch`\|`no_face`\|`multiple_faces`\|`window_blur`\|`copy_paste` |
| timestamp_in_session | integer                        | seconds into interview                                                           |
| severity             | text                           | `low`\|`medium`\|`high`                                                    |
| created_at           | timestamptz                    |                                                                                  |

## Required Indexes

```sql
CREATE INDEX idx_github_profiles_user_repo ON github_profiles(user_id, repo_full_name);
CREATE INDEX idx_sessions_user ON interview_sessions(user_id);
CREATE INDEX idx_usage_user_month ON interview_usage(user_id, month_year);
CREATE INDEX idx_analyze_usage_user_date ON analyze_usage(user_id, date);
CREATE INDEX idx_questions_reusable ON questions_bank(reusable) WHERE reusable = true;
CREATE INDEX idx_pinned_repos_user ON pinned_repos(user_id);
CREATE INDEX idx_proctoring_events_session ON proctoring_events(session_id);
```

## RLS Policy Pattern (apply to every user-owned table)

```sql
ALTER TABLE interview_sessions ENABLE ROW LEVEL SECURITY;

CREATE POLICY "Users can view own sessions"
  ON interview_sessions FOR SELECT
  USING (auth.uid() = user_id);

CREATE POLICY "Users can insert own sessions"
  ON interview_sessions FOR INSERT
  WITH CHECK (auth.uid() = user_id);
```

Repeat this pattern for: `github_profiles`, `pinned_repos`,
`interview_sessions`, `session_questions`, `candidate_reports`,
`interview_usage`, `analyze_usage`, `interview_recordings`,
`proctoring_events`.

`questions_bank` is partially shared (reusable questions) — RLS should allow
read access to `reusable = true` rows for all authenticated users, but write
access only via the Go service role.

## Go Model Example (GORM, matching StayChat pattern)

```go
type User struct {
    ID              string    `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
    GithubID        *int64    `gorm:"unique"`
    GithubUsername  *string
    GoogleID        *string   `gorm:"unique"`
    Email           *string
    AvatarURL       string
    PlanType        string    `gorm:"default:free"`
    Role            string    `gorm:"default:candidate"`
    GithubConnected bool      `gorm:"default:false"`
    CreatedAt       time.Time
    LastLoginAt     time.Time
}
```
