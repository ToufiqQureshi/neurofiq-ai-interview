# Neurofiq Campaign — Part 2: Post-Launch (Day 31-46)

Launch ke baad ka content. Sab roadmap (`11_MVP_ROADMAP.md`) se liya hai — koi feature invent nahi kiya.

**Sab ⚠️ hain** — jab wo feature actually ship ho jaye, tabhi post karna.

Build-to-launch content → `CAMPAIGN_PART1_BUILD_TO_LAUNCH.md` (Day 1-30)

---

## Day 31 — Email notifications ⚠️

```
Day 31 of Neurofiq 📬

First post-launch feature, and it came straight from watching real user behaviour.

Analysis takes ~20 seconds. Interviews take 15-20 minutes. Evaluation takes another 20 seconds.

So people do the obvious human thing: they start an analysis, switch tabs, and forget. The report sits there, finished, unread.

The product worked perfectly. The user never saw the output. That's a total loss, and it's my fault, not theirs.

So: email when the report is ready.

**Three engineering decisions worth explaining.**

**1. The email send must never block the request.**

  go func() {
      defer recover()   // Day 16's lesson, again
      sendReportEmail(user, reportID)
  }()

If my email provider is slow or down, that's an email problem. It must not become a "your analysis failed" problem. The report is already safely in Postgres — the email is a notification about it, not part of producing it.

**2. Track sent state in the database.**

A boolean on the session row. Because retries, redeploys and at-least-once delivery all mean the send path can run twice, and nobody wants the same "your report is ready" email three times.

Idempotency isn't just for payments.

**3. One email. Not a sequence.**

I'm not building a drip campaign. One transactional email, triggered by a thing the user actually asked for, with a direct link to the thing they wanted.

The moment you send a second unrequested email you've become a marketing channel, and users start filtering you out — which means they'll also miss the one email that mattered.

The smallest features often have the best ROI. This is maybe 60 lines of code, and it recovers users who already did the hard part. 📬
```

---

## Day 32 — Retry weak areas ⚠️

```
Day 32 of Neurofiq 🔁

There's been a "Retry Weak Areas" button in my report UI since launch, greyed out with a "Coming Soon" tag. Today it's real.

The reasoning behind it:

You finish an interview, score 6/10, and read that you were weak on error handling and concurrency. You now know exactly what to work on.

And then the product asks you to... do the entire 20-minute interview again from scratch, mostly answering questions you already got right.

That's a terrible loop. The whole point of a targeted report is a targeted retry.

**How it works:**

The evaluation already stores a per-question score. So a retry is:

1. Take the questions you scored below 6 on
2. Generate NEW questions probing the same concepts, from the same code
3. Run a shorter session — just those areas

**The critical detail: new questions, not the same ones.**

If I replayed the identical questions, you'd just memorise my expected answer. That measures recall, not understanding — which is the exact failure mode of Leetcode grinding that this whole product exists to fix.

Same concept, same code, different angle. Now improving your score requires actually understanding the concept.

**A cost note that made this easy:**

The expensive part — repo download, extraction, the big analysis LLM call — is already cached in Postgres from the original session. A retry only needs question generation and evaluation.

So a retry costs a fraction of a first interview. That in turn makes it cheap enough to allow generously on the free tier, which is exactly where a learning loop needs to live.

Good caching from Day 2 turned an expensive feature into a cheap one, 30 days later. 🔁
```

---

## Day 33 — CV Optimizer ⚠️

```
Day 33 of Neurofiq 📄

New feature, and it comes from watching the same failure over and over.

Most engineering resumes list technologies:

"Built a full-stack web app using React, Node.js, MongoDB and Express."

That sentence appears on tens of thousands of resumes. It says nothing about whether you can engineer. It's a list of things you've been near.

Meanwhile the actual signal is sitting in the repo, invisible:

• You handled a race condition with an advisory lock
• You cached with ETags to survive an API rate limit
• You made a deliberate trade-off between latency and consistency, and you can explain why

Those are the things that make a hiring manager stop scrolling. Almost nobody writes them down — because when you're building, those decisions feel obvious. It's just what you had to do. You don't perceive them as achievements.

So the CV Optimizer reuses the analysis engine that already exists.

Neurofiq has already read your repo. It already extracted your architecture patterns, your notable decisions, and the specific code behind them — that's what powers the interview questions.

The CV Optimizer points that same data at a different output:

**Before:**
"Built a REST API using Go and PostgreSQL."

**After:**
"Built a Go REST API handling concurrent repo analysis via goroutines; eliminated a TOCTOU race in usage limits using Postgres advisory locks; cut third-party API calls to near-zero with ETag-based conditional requests."

Same project. Same person. Wildly different signal.

The design constraint I'm holding to: **it must never invent anything.**

Every bullet has to trace back to something actually found in the code. A resume tool that hallucinates achievements isn't a productivity feature — it's a way to get someone humiliated in an interview.

So it's grounded in extracted facts, never free-form generation. If it can't point to the code, it doesn't write the line.

Your repo is already the evidence. It's just written in a format recruiters don't read. 📄
```

---

## Day 34 — LinkedIn Optimizer ⚠️

```
Day 34 of Neurofiq 💼

The companion to yesterday's CV Optimizer, and it solves a subtly different problem.

A resume gets read after someone already decided to look at you. LinkedIn decides whether they ever find you at all.

Recruiters search by keyword. They type "Golang backend distributed systems" into a search box and work through the results. If your profile doesn't contain the words they search, you don't exist — no matter how good your code is.

And most engineer profiles look like this:

Headline: "Software Developer | Passionate about technology"
About: three sentences about being a "quick learner"
Skills: HTML, CSS, JavaScript

That profile is invisible for every search that would actually match this person's work.

So the LinkedIn Optimizer takes the same repo analysis and rewrites three things:

**1. The headline** — the highest-leverage 220 characters on the internet for a job seeker. It's what shows in search results, in comment threads, in every recruiter's list view.

"Software Developer | Passionate about technology"
→ "Backend Engineer · Go, Python, Postgres · Concurrent systems & API infrastructure"

**2. The About section** — rewritten around what you've actually built and the problems you've actually solved, in first person, without the LinkedIn-voice cringe.

**3. Skills** — extracted from your real code and ordered by how much you've genuinely used them. Not aspirational. Not a list of everything you've ever installed once.

The reason this works at all is that it's grounded in the same analysis pipeline as everything else in the product.

It's not asking an LLM to imagine a good profile for a backend engineer. It's reading YOUR repositories and surfacing what's already true — the languages you actually write, the systems you actually built, the patterns you actually used.

The uncomfortable truth underneath both of these features:

Most engineers aren't underqualified. They're under-described. The work is real; the record of it is missing.

Your code already made the case. This just writes it down. 💼
```

---

## Day 35 — The paid voice tier ⚠️

```
Day 35 of Neurofiq 🎧

On Day 18 I shipped voice interviews for $0 using the browser's Web Speech API. It works, it's free, and for most users it's genuinely fine.

But it has real limits, and users found all of them:

• Browser support is inconsistent outside Chrome
• Accuracy drops hard on strong accents and in noisy rooms
• The synthesised voice sounds robotic, which breaks the "real interview" illusion

So today: a paid voice tier, using Deepgram for speech-to-text and ElevenLabs for text-to-speech.

**Why this waited until Day 35 and not Day 18.**

This is the highest-complexity integration in the entire product. It needs:

• WebSocket connections held open for the whole session
• Bidirectional audio streaming
• Reconnection logic when a phone switches from wifi to 5G
• Per-minute cost tracking so a long session can't quietly cost more than the subscription

Building that before knowing whether anyone even wanted voice would have been the definition of premature complexity.

Now I have real usage data. Voice mode is used. People asked for better voice. That's when it earns the complexity.

**The architecture decision that makes it safe:**

Go owns the WebSocket and every vendor call. Python still never touches audio.

Python's contract remains exactly what it's been since Day 8: text in, structured JSON out. It has no idea whether that text came from a keyboard, the free browser API, or a paid transcription vendor.

That's the payoff for holding a boundary for 27 days. Adding an entirely new input modality changed **zero lines** in the AI worker.

**And the free tier keeps its voice mode.** Web Speech stays exactly as it is. Paid gets better accuracy and a better voice — it doesn't get the only working version.

Never take a feature away to sell it back. 🎧
```

---

## Day 36 — Interruptions and turn-taking ⚠️

```
Day 36 of Neurofiq 🗣️

Now that voice is properly good, a subtler problem shows up — and it's the thing that separates "voice feature" from "actual conversation."

Real interviews have interruptions.

You start answering, realise you misunderstood, and stop. The interviewer says "sorry, I meant the caching layer specifically." You talk over each other for half a second. It's messy and completely normal.

My voice mode was strict push-to-talk. AI speaks fully → you speak fully → repeat. Clean, deterministic, and nothing like a conversation.

So: Voice Activity Detection and interruption handling.

**What VAD actually has to solve:**

1. **Detect speech vs silence** — energy thresholds are the naive version and they fail badly in a noisy room. Fan, keyboard, traffic all read as "speech."

2. **Know when a turn ENDS** — this is the genuinely hard part. Pause 800ms mid-sentence while thinking? That's not the end of your turn. Pause 800ms at the end of a complete thought? That is.

Get it wrong in one direction and you interrupt someone who was thinking. Get it wrong the other way and there's an awkward two-second gap after every answer.

3. **Barge-in** — if the candidate starts talking while the AI is speaking, stop the AI immediately. Nothing feels more robotic than a bot that keeps talking over you.

**The trade-off I'm making explicit:**

Perfect turn-taking needs semantic understanding — knowing whether a sentence is grammatically complete. That's another model in the hot path, more latency, more cost.

I'm starting with tuned silence thresholds plus barge-in. Good enough to feel natural, cheap enough to run on every session.

Ship the 80% that's cheap. Only pay for the last 20% if users tell you they need it. 🗣️
```

---

## Day 37 — Hinglish voice support ⚠️

```
Day 37 of Neurofiq 🇮🇳

A feature request I got repeatedly, and one that most Western-built interview tools completely ignore:

**Indian engineers don't interview in pure English.**

A real technical discussion in an Indian office sounds like:

"Toh basically maine yahan Redis use kiya, kyunki in-memory store fast hai — but the failure mode is, agar Redis down ho jaye toh?"

That's not broken English. That's how the conversation actually happens, in real interviews, at real Indian companies, every single day.

Force someone to explain a complex system in strictly formal English and you're not testing their engineering. You're testing their English fluency under pressure — which is a completely different skill and, for most of these roles, an irrelevant one.

**What this actually takes:**

1. **Transcription that handles code-switching.** Most STT models are configured for a single language and mangle mid-sentence switches. Deepgram supports multilingual models — but it needs explicit configuration, not defaults.

2. **Evaluation that doesn't penalise it.** This is the part that would be easy to get wrong. My evaluator scores "communication clarity" — and an LLM's default bias is to treat non-standard English as unclear.

So the instruction has to be explicit: **code-switching is not a communication deficiency. Score the technical clarity of the explanation, not its grammatical formality.**

3. **A question agent that can mirror it**, so the interview doesn't feel like the AI is politely correcting you every turn.

**Why this matters beyond a feature:**

My users are largely Indian engineers. Building an interview tool that implicitly requires formal English is building a tool that misjudges the people it's meant to serve.

Localisation isn't translating your UI. It's making sure your evaluation doesn't quietly penalise how your users actually think and speak. 🇮🇳
```

---

## Day 38 — Camera Tier B: actual recording ⚠️

```
Day 38 of Neurofiq 📹

On Day 19 I shipped the camera that records nothing — pure local self-view, zero backend, zero cost, and it got most of the behavioural benefit of proctoring.

Today, the version that actually records. And this one changes the product's risk profile completely.

**Why record at all?**

Because "watch your own interview back" is genuinely valuable feedback. You discover you say "um" every six words. You see yourself avoid eye contact when you're unsure. Nobody can tell you that as usefully as watching yourself.

**Why this is a much bigger deal than Tier A:**

Tier A was frontend-only. This one touches everything:

• Storage costs that scale linearly with usage
• Upload reliability on flaky mobile connections
• A retention policy — you can't just keep video forever
• Explicit consent, before the camera ever starts
• A deletion path that actually deletes

**The architecture:**

Recording happens client-side with MediaRecorder. Chunks upload during the session, not in one giant blob at the end — because a 200MB upload that fails at 95% on a mobile connection is a completely wasted interview.

Go orchestrates: issues a pre-signed upload URL, tracks completion, writes the metadata row. The video never passes through my application server. Bytes go browser → object storage, directly.

**The rules I'm holding myself to:**

• **Opt-in, always.** Default is Tier A (no recording). You choose to record.
• **Auto-delete after 30 days.** Not "we may retain" — an actual scheduled deletion job.
• **Delete means delete.** One button, removes the object and the row, immediately.
• **Only you can see it.** No recruiter access, no sharing, not in this tier.

**The honest framing:**

Storing video of people's faces is a fundamentally different responsibility from storing a JSON report. Every "we might use this later" reason to keep it longer is a reason to keep a liability longer.

Ship the smallest version of a data-heavy feature you can defend. 📹
```

---

## Day 39 — Consent, retention and deletion ⚠️

```
Day 39 of Neurofiq 🗄️

Yesterday I started storing video. Today, the unglamorous work that has to come with it — and that most side projects skip entirely until something goes wrong.

**What I actually hold about a user:**

• GitHub identity — username, email, avatar
• Their code — extracted snippets, cached in Postgres
• Their interview answers — literally them explaining how they think
• Their scores and feedback
• Optionally, video of their face

Written out like that, this isn't "some app data." It's a fairly intimate professional profile of a person.

**So, four things.**

**1. Consent that's specific, not buried.**

Not one checkbox for everything at signup. A clear prompt before the camera starts, saying exactly what gets stored and for how long.

Consent buried in a ToS nobody read isn't consent. It's paperwork.

**2. Retention limits, enforced by a job.**

Every data type gets an expiry:
• Video → 30 days
• Cached repo analysis → 7 days (it's already got an expires_at column)
• Reports → kept, because they're the thing the user actually wants

And it's enforced by a scheduled deletion job — not by a sentence in a policy document. A policy nobody enforces is a lie you've written down.

**3. Delete that genuinely deletes.**

One button. Removes the DB rows, removes the objects from storage, revokes the GitHub token.

Not a soft-delete flag. Not "removed from your view." Gone.

**4. A privacy policy that describes reality.**

Written after building the system, so it describes what the code actually does — not what I hoped it would do.

**Why do this pre-revenue, when nobody's asking?**

Because retrofitting deletion into a system that assumed permanence is genuinely hard. Every table you added without thinking about it becomes an archaeology project.

Building it in from the start costs a day. Bolting it on later costs a rewrite — usually under time pressure from someone who is not asking nicely. 🗄️
```

---

## Day 40 — Client-side proctoring ⚠️

```
Day 40 of Neurofiq 👁️

Proctoring is where interview tools usually go badly wrong, so let me be precise about what I'm building and what I'm refusing to build.

**What I'm refusing:**

Streaming video frames to a server for continuous AI analysis. Every frame is a model call. Costs scale with session length × user count and can genuinely run away from you. It's also invasive in a way I'm not comfortable with for a practice tool.

**What I'm building instead: everything runs in the browser.**

MediaPipe for face detection, running client-side, on the candidate's device. Plus the Page Visibility API for tab switching.

The browser computes signals like:
• Is a face present in frame?
• Is more than one face present?
• Did the tab lose focus, and for how long?

And sends only the **events** to Go. Never the frames.

  { "event": "tab_blur", "duration_ms": 4200, "at": "..." }

That's a few hundred bytes instead of a video stream. Storage cost is negligible. Server compute is zero. And I never receive a single video frame I'd then be responsible for.

**How the signals are actually presented — this matters more than the detection:**

They're **signals, not verdicts.**

The report doesn't say "CHEATING DETECTED." It says "you switched tabs 4 times during this interview, for 30 seconds total."

That's honest, and in a self-practice tool it's genuinely useful — most people have no idea how often they context-switch under pressure.

**The lines I'm holding:**

• Never a cheating accusation from a heuristic. Face-not-detected has a hundred innocent explanations.
• Always visible to the candidate that it's on, and what it measures.
• Opt-in, and the interview works completely fine with it off.

Proctoring built as surveillance produces defensive users and bad data. Proctoring built as self-awareness produces something people actually want to look at. 👁️
```

---

## Day 41 — Progress over time ⚠️

```
Day 41 of Neurofiq 📈

Users have been asking a question the product couldn't answer:

"Am I actually getting better?"

Every session ends with a score. But scores in isolation are almost meaningless — you don't know if 7/10 is progress or regression, because you can't remember what you got three weeks ago.

So: an analytics view that treats your sessions as a series, not as isolated events.

**What it shows:**

• Score trend across all sessions
• **Per-pillar breakdown over time** — correctness, depth, clarity, trade-offs, tracked separately
• Which topics keep coming up as weak areas
• Which repos you've been interviewed on

**The per-pillar view is the whole feature.**

Because a flat overall score of 7 hides everything interesting. Split it and a real pattern appears:

Correctness: 8 → 8 → 9  (solid, always was)
Trade-offs:  4 → 4 → 5  (this is your actual gap)

That's genuinely actionable. "Get better at interviews" isn't. "You consistently don't discuss what your choices cost" is.

**The design decision I spent the most time on: honesty vs motivation.**

It's tempting to make every chart go up and to the right. Ignore bad sessions. Smooth the line. Show a streak.

But this product's entire value is honest signal. A dashboard that flatters you is the same failure as the sycophantic LLM I fixed on Day 14 — just with charts instead of prose.

So it shows the real line, dips included. If you scored worse, you scored worse, and the interesting question is why.

**The engineering side is deliberately boring:**

Every session already stores per-question scores as structured JSON. This is aggregation over data I've had since day one, because I chose a structured schema instead of dumping prose into a text column.

Structured data you don't need yet is the cheapest feature you'll ever build. 📈
```

---

## Day 42 — The leaderboard, and its risks ⚠️

```
Day 42 of Neurofiq 🏆

Adding a leaderboard. This one needed more design thought than code, because leaderboards are very easy to get badly wrong.

**The obvious version is the wrong version.**

Rank everyone by score, top to bottom, public.

Three problems:

1. **It's not comparable.** Someone interviewed on a to-do app and someone interviewed on a distributed job queue don't get scored on the same difficulty. A raw score ranking rewards picking an easy repo.

2. **It punishes honesty.** Do a genuine interview on your hardest project and score 6. Do a soft one on a simple project and score 9. The leaderboard tells you to sandbag. That directly attacks the product's core value.

3. **Nobody wants to be publicly ranked as bad at their job.** Most users would simply opt out, leaving a leaderboard of the top 5% — useless to everyone else and demoralising to look at.

**So what I'm actually building:**

• **Opt-in.** You choose to appear.
• **Ranked within complexity bands.** The analysis already classifies repos as Low / Moderate / High complexity. You're compared to people interviewed at similar difficulty.
• **Percentile, not raw score.** "Top 15% for High-complexity repos" beats "#847."
• **Weekly cohorts**, not all-time. All-time leaderboards ossify — the top is unreachable, so nobody new engages.

**Why bother at all?**

Because retention on a self-improvement tool is genuinely hard. People do one interview, feel good, and never come back. A cohort you're part of gives you a reason to return that isn't just discipline.

**The rule I'm holding:**

The leaderboard must never create an incentive to game the interview. The moment ranking rewards picking an easy repo, the leaderboard is actively damaging the product it's meant to support.

Every gamification feature is an incentive system. Design the incentive first, the UI second. 🏆
```

---

## Day 43 — In-browser code editor ⚠️

```
Day 43 of Neurofiq ⌨️

Until now, every Neurofiq question has been discussion-based. Explain your architecture, defend your trade-offs, talk through your reasoning.

That's the highest-signal part of a real interview, and I stand by leading with it.

But there's a category it can't touch: **can you actually write the fix?**

So today: an in-browser code editor, with questions generated from your own code.

**What makes this different from Leetcode:**

The question isn't a puzzle. It's your code.

"Here's the rate limiter you wrote. It's an in-memory map. Make it work correctly across multiple instances."

"Here's your error handler — it swallows the original error. Refactor it to preserve the cause without leaking internals to the client."

You're not implementing a red-black tree you'll never use. You're improving something you already wrote, which is exactly what the job is.

**The engineering constraints:**

**1. Monaco for the editor.** It's what VS Code uses. Familiar keybindings, syntax highlighting, no learning curve mid-interview.

**2. I am NOT executing code — and that's a deliberate choice.**

Running untrusted user code means sandboxing, container isolation, resource limits, timeout handling, and an entire class of security problems that has eaten teams alive.

Instead, the AI evaluates the code as a senior engineer reviewing a PR would: is the approach right, does it handle the edge cases, what would you flag in review?

For "can you reason about a fix," that's the right level. And it costs zero execution infrastructure.

**3. Evaluation is grounded in their original code**, so the AI can compare before and after — not just judge a snippet in isolation.

**The honest limitation:**

Without execution, I can't verify the code compiles or passes tests. That's a real gap.

But the alternative is building and securing a code execution platform, and that's not a feature — that's a second product. If demand proves it's needed, that's when it earns the complexity. ⌨️
```

---

## Day 44 — The B2B pivot: company dashboard ⚠️

```
Day 44 of Neurofiq 🏢

The most-requested thing I did not expect, and it didn't come from candidates.

It came from small engineering teams:

"Can we send this to our candidates?"

Because they have the exact problem I started with on Day 1 — but from the other side. They get 200 applicants, they can't interview 200 people, and a resume tells them almost nothing about whether someone can actually build.

So: a company dashboard.

**What it does:**

• Company creates a role
• Sends a link to candidates
• Candidate connects their GitHub, picks a repo, does the interview
• Company sees the reports — scored, with reasoning, with code references

**The design decisions that actually matter here:**

**1. The candidate still picks their own repo.**

I was tempted to let companies specify "interview them on THIS repo." I won't.

Being interviewed on your own best work is the entire premise. Take that away and it becomes another arbitrary test — and I'd have rebuilt the thing I set out to replace.

**2. Candidates keep their report.**

Even if they don't get the role. They did the work, they get the feedback. Non-negotiable.

This is what makes it a fair exchange instead of free labour for the employer.

**3. Consent is explicit.**

The candidate sees exactly which company receives their report, before they start.

**The architectural bill:**

This is the first feature that breaks my single-tenant assumption. Every table so far has a user_id. Now some rows belong to an organisation, with members and roles, and permissions to check on every read.

That's a real migration and a real permissions layer — not a bolt-on.

**The strategic question I'm sitting with:**

B2B is where the revenue is. B2C is where the product's soul is — helping engineers, not filtering them.

I want to build the version where those aren't in conflict. Candidates get real feedback whether they're hired or not, companies get real signal, and nobody's data gets used against them.

That balance is a product decision, not an engineering one. 🏢
```

---

## Day 45 — Proctoring for the B2B tier ⚠️

```
Day 45 of Neurofiq 🛡️

If companies use Neurofiq for real hiring decisions, the proctoring question changes completely.

On Day 40 I built client-side signals for self-practice — tab switches, face presence, framed as self-awareness. That framing works when you're the only one reading it.

The moment a hiring decision depends on it, everything about that framing has to be re-examined.

**What actually changes:**

**1. Signals must never become verdicts.**

Client-side proctoring produces heuristics, not facts. Face not detected? Bad lighting, a laptop camera at a weird angle, someone who leans out of frame when they think.

If I hand a company a "cheating score," some candidate somewhere loses a job to a lighting condition. That's not a bug I'm willing to ship.

So the dashboard shows raw observations with context, and explicitly labels them as signals requiring human judgment. Never a verdict, never a percentage.

**2. Candidates see exactly what the company sees.**

Complete symmetry. Nothing in the company's view that isn't in the candidate's.

Asymmetric surveillance is how these products become something I wouldn't want to have built. If I'm not comfortable showing the candidate a data point, I shouldn't be collecting it.

**3. Disclosure before, not buried in a ToS.**

Before the interview starts: here's what's monitored, here's who sees it, here's how long it's kept.

**4. It's optional for the company too.**

Plenty of teams don't want it. Making it default-on would signal that suspicion is the normal mode of hiring.

**Where I'm drawing the hard line:**

No eye-tracking. No emotion detection. No "confidence scoring" from facial expressions.

That research is shaky at best, and it's demonstrably biased against neurodivergent people and across ethnicities. Building it would mean shipping discrimination with a technical veneer.

Some features are technically possible and still shouldn't exist. Knowing which is which is part of the job. 🛡️
```

---

## Day 46 — Scaling the Python worker independently ⚠️

```
Day 46 of Neurofiq ⚖️

Day 3 of this build, I claimed the Go/Python split would let me scale AI processing independently of request volume.

Today I'm finding out whether that was true or whether it was architecture astronomy.

**What the metrics show:**

Go's CPU sits low. It's doing I/O — database queries, GitHub calls, forwarding payloads. Goroutines handle it comfortably.

Python is the bottleneck. Not because Python is slow, but because each request holds a connection open for 15-25 seconds waiting on an LLM. Concurrency there is bounded by how long the model takes, not by how fast the code is.

Classic asymmetric scaling: request volume and AI volume are genuinely different curves.

**The change:**

Scale the Python service on its own, on its own metric.

Not CPU — CPU is misleading here, because a process waiting on an HTTP response looks idle. Scale on **in-flight requests per instance**, which is what actually correlates with saturation for this workload.

**The part that made this a config change instead of a project:**

The worker is stateless. Since Day 8. No DB credentials, no session affinity, no local files, no coordination between instances.

So "scale it" genuinely means: increase the instance count. That's it. No migration, no leader election, no distributed cache to introduce.

**What I'd have been facing if I'd cut that corner:**

If Python had grown a DB connection at any point in the last 45 days — and there were several convenient moments where it would have been easier — today would involve connection pool limits, N instances competing for the same rows, and a race condition hunt with real users on the platform.

**The honest reflection:**

The boundary on Day 8 felt like over-engineering at the time. It was one service, on one machine, with three users.

45 days later it turned a scaling project into a config change.

Architecture decisions don't pay off when you make them. They pay off — or bill you — much later, when you've forgotten you made them. ⚖️
```

---

## Build status — Part 2

Sab roadmap (`11_MVP_ROADMAP.md`) se hain, kuch invent nahi kiya.

| Day | Feature | Roadmap item |
|---|---|---|
| 31 | Email notifications | Phase 1.5 #19 |
| 32 | Retry weak areas | Phase 1.5 #18 |
| 33 | CV Optimizer | Sidebar "Soon" |
| 34 | LinkedIn Optimizer | Sidebar "Soon" |
| 35 | Paid voice tier (Deepgram + ElevenLabs) | Phase 1.5 #17 |
| 36 | VAD / interruption handling | Phase 2 #28 |
| 37 | Hinglish voice support | Phase 2 #29 |
| 38 | Camera Tier B (recording + storage) | Phase 1.5 #20 |
| 39 | Consent + retention + deletion | Phase 1.5 #21 |
| 40 | Client-side proctoring (MediaPipe) | Phase 2 #22 |
| 41 | Candidate analytics / trends | Phase 2 #25 |
| 42 | Leaderboard | Phase 2 #26 |
| 43 | In-browser code editor | Phase 2 #27 |
| 44 | Company dashboard (B2B) | Phase 2 #24 |
| 45 | Proctoring dashboard (B2B) | Phase 2 #23 |
| 46 | Scale Python worker independently | Phase 2 #30 |

### Posting notes

- **Sab ⚠️ hain** — feature ship hone ke baad hi post karna. Build-in-public me credibility hi sab kuch hai.
- Order roadmap ke hisaab se hai, par **jo pehle banao wahi pehle post karo** — sequence flexible hai.
- Sabse zyada engagement potential: **Day 37** (Hinglish — Indian dev audience ke liye personal hai), **Day 42** (leaderboard incentive design), **Day 45** (jo features banane se maine mana kiya), **Day 46** (Day 8 ka payoff — narrative closure).
- **Day 44 aur 45 opinionated hain** — B2B/surveillance pe stance leta hai. Ye engagement laata hai, par ye stance sach me maano tabhi post karna.
