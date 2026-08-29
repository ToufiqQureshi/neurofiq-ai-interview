# Cost Controls, Caching & Tier Limits

## Pricing Model

```
FREE TIER (no ads — see rationale below):
- Pinned repos: max 2
- Repo analysis: 10/day
- Interviews: 5/month, TEXT-ONLY
- Basic AI feedback/report
- Camera: Tier A preview only (no recording)

PAID TIER (₹/$7-9/month):
- Pinned repos: max 10
- Repo analysis: 50/day
- Interviews: unlimited
- Voice interviews unlocked
- Detailed AI reports
- Camera recording + proctoring (Tier B) unlocked
```

**Why not ads on the free tier:** at realistic early traffic, ad revenue is
negligible ($1-5 CPM → a few dollars/month at best) and ads actively hurt
UX in a serious practice-interview context. Freemium + eventual B2B/
affiliate is a better fit than ads.

## Caching Layers (all owned/managed by Go, backed by Postgres)

| Data | Where | TTL |
|------|-------|-----|
| GitHub repo analysis | Postgres (`github_profiles`), via Go | 7 days |
| GitHub repo list | Go in-memory/Postgres cache + ETag | 1 hour |
| Generated reusable questions | Postgres (`questions_bank`), via Go | Permanent (reused) |
| Static frontend assets | Cloudflare CDN | 1 year (versioned) |
| Landing page | Cloudflare Pages edge | Static |

**Redis: skip for MVP.** Postgres-as-cache (per the schema in doc 02) is
sufficient at MVP scale, and keeps caching logic inside Go rather than
adding a third service to operate. Add Redis only when Postgres query
latency for cache lookups exceeds ~200ms or you cross ~100 concurrent
users.

```go
func GetRepoAnalysis(userID, repo string) (map[string]any, error) {
    cached, err := db.GetGithubProfile(userID, repo)
    if err == nil && cached.ExpiresAt.After(time.Now()) {
        return cached.AnalysisJSON, nil // cache hit
    }

    structure := extractDeterministicStructure(userID, repo) // Go-side
    analysis, err := callPythonWorker("/internal/analyze-repo", structure)
    if err != nil {
        return nil, err
    }

    db.UpsertGithubProfile(userID, repo, analysis, time.Now().Add(7*24*time.Hour))
    return analysis, nil
}
```

## AI Token-Saving Tactics

1. **Send structured snippets, never raw full files.** Go extracts function
   signatures, class definitions, key logic via targeted sampling before
   ever calling the Python worker — see doc 04 for the exact file-selection
   algorithm.
2. **Batch every generation.** One Go→Python call producing 5-10 structured
   JSON questions, never one call per question.
3. **Reuse generic questions.** Before calling Python for new questions, Go
   checks `questions_bank` for `reusable=true` rows matching the
   candidate's tech stack — if 5+ exist, skip the internal call (and
   therefore the Claude call) entirely.
4. **Cap `max_tokens` on every Claude call** inside the Python worker (500
   for text evaluation, 150 for voice turns) to force concise output.
5. **Use cheaper models for simple/deterministic-adjacent tasks** and
   reserve the stronger model for complex architecture analysis and
   nuanced evaluation — this choice lives in the Python worker's model
   selection logic.

```go
func GetOrGenerateQuestions(userID string, analysis map[string]any) ([]Question, error) {
    similar := db.FindReusableQuestions(analysis["languages"].([]string), 5)
    if len(similar) >= 5 {
        return similar, nil // no Python/Claude call needed
    }
    return callPythonWorker("/internal/generate-questions", analysis)
}
```

## Usage Limit Enforcement Pattern (Go, atomic, single source of truth)

```go
const FreeTierLimit = 5 // interviews/month

func CheckInterviewLimit(userID string) (UsageStatus, error) {
    user, _ := db.GetUser(userID)
    if user.PlanType == "paid" {
        return UsageStatus{Allowed: true, Remaining: -1, Plan: "paid"}, nil
    }

    monthYear := time.Now().Format("2006-01")
    // Atomic upsert-and-read in a single transaction to avoid race conditions
    var usage InterviewUsage
    db.Transaction(func(tx *gorm.DB) error {
        tx.Clauses(clause.OnConflict{
            Columns:   []clause.Column{{Name: "user_id"}, {Name: "month_year"}},
            DoNothing: true,
        }).Create(&InterviewUsage{UserID: userID, MonthYear: monthYear, Count: 0})
        return tx.Where("user_id = ? AND month_year = ?", userID, monthYear).First(&usage).Error
    })

    remaining := FreeTierLimit - usage.Count
    return UsageStatus{
        Allowed: remaining > 0, Remaining: max(0, remaining),
        Plan: "free", Used: usage.Count, Limit: FreeTierLimit,
    }, nil
}
```

Monthly reset is automatic by design — a new row per `(user_id, month_year)`
means no cron/scheduled reset job is needed. Historical usage data is
naturally preserved for analytics.

**Why this must be atomic and Go-only:** if both Go and Python
independently checked "is this user under their limit," two concurrent
requests could both read "4/5 used, allowed" before either increments the
counter — bypassing the cap. See doc 08 for the full race-condition
discussion.

## Cost Guarantee Summary (what every feature must satisfy)

| Feature | Cost driver | Hard cap mechanism | Enforced in |
|---------|-------------|----------------------|--------------|
| Repo analysis | Input tokens | 15k token hard budget regardless of repo size (doc 04) | Go |
| Text interview | Claude calls | Batched question gen, capped follow-ups | Go orchestrates, Python executes |
| Voice interview | STT/TTS + Claude | 90s/answer cap, 1 follow-up max, 150 output tokens/turn (doc 05) | Go (session limits) + Python (token limits) |
| Camera/proctoring | Storage + compute | Client-side MediaPipe (zero server AI cost), compressed video (doc 06) | Frontend + Go |

The unifying principle across all docs: **no feature's cost is allowed to
scale with user input size or session length without an explicit hard cap,
and every usage-limit check happens in exactly one place (Go).**
