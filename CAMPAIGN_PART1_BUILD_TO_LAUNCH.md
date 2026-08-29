# Neurofiq Campaign — Part 1: Build to Launch (Day 1-30)

X Premium long-form posts. Har din ka **ek** post — copy-paste ready.

**✅ Ready to post:** Day 1-24, Day 30
**⚠️ Pehle banana hai:** Day 25-29

Post-launch content → `CAMPAIGN_PART2_POST_LAUNCH.md` (Day 31-46)

---

## Day 1 — The vision

```
Day 1 of building Neurofiq 🚀

Tech hiring has a measurement problem.

We say we want engineers who can design systems, reason about trade-offs, and debug production. Then we test them on whiteboard DSA puzzles they will never write again.

So the loop looks like this:

→ Company asks Leetcode Hard
→ Candidate grinds 6 months of patterns
→ Candidate gets hired
→ Candidate can't debug a race condition in prod

We're not measuring bad engineers. We're measuring the wrong thing.

Here's my bet:

The best interview signal already exists. It's sitting in the candidate's GitHub. Their architecture decisions, their trade-offs, the shortcuts they took at 2am and never cleaned up.

So I'm building Neurofiq — an AI interviewer that reads your actual repository, understands how it's built, and interviews you on YOUR code. Not trivia. Your code.

Over the next 30 days I'm building this completely in public.

You'll see:
• Why I split the backend into Go + Python
• How I beat GitHub's API rate limits for free
• A race condition that let users bypass billing entirely
• A voice interview feature that costs $0/month
• Every bug, including the embarrassing ones

Follow if you like backend engineering with the messy parts left in.

#buildinpublic #golang #ai
```

---

## Day 2 — Why context beats prompting

```
Day 2 of Neurofiq 🧠

Yesterday I said the AI interviews you on your own code. Today, the thing that actually makes that work.

Here's what happens if you just hand an LLM a job title and ask for interview questions:

"What is React?"
"Explain the difference between let and var."
"What are microservices?"

Useless. Anyone can Google that. It's the same interview for every candidate.

The problem isn't the model. It's that the model has no idea what you built.

So Neurofiq runs an analysis phase BEFORE the interview ever starts:

1. You log in with GitHub OAuth
2. You pick your most complex repo
3. Backend downloads it, scores every file, extracts the architecturally significant parts
4. The AI reads that context and generates questions from it

Now the same model asks:

"Why did you use a Singleton here when it breaks under concurrent access?"

Same LLM. Same temperature. Completely different interview.

One more design decision that matters more than it looks:

Analysis and interview are separate phases.

Analysis is the expensive part — repo download, extraction, a big LLM call. So it runs once and gets cached in Postgres. Every interview after that on the same repo is nearly free.

The lesson I keep relearning: when AI output feels generic, the fix is almost never a cleverer prompt. It's better context.
```

---

## Day 3 — The Go + Python split

```
Day 3 of Neurofiq 🏗️

Architecture reveal. The backend is two separate services, and this split is the single most important decision in the project.

⚙️ Service 1: Go (the orchestrator)

Owns everything stateful:
• Postgres
• GitHub OAuth + sessions
• Billing limits
• Rate limiting
• API routing
• Repo download + extraction

When 1,000 users hit it at once, goroutines absorb the I/O without a thread pool to tune or an event loop to protect.

🤖 Service 2: Python + FastAPI (the AI worker)

Owns exactly one thing: talking to the LLM.

Agno, DeepSeek, structured output schemas. The entire AI ecosystem lives in Python and fighting that is a waste of everyone's time.

But — and this is the rule I refuse to break — the Python worker has ZERO database access.

Go does all the stateful work, packages it into one payload, sends it over. Python computes and returns. That's the whole contract.

Why this split is worth the extra service:

Python is genuinely bad at high-concurrency I/O. Go has genuinely no AI ecosystem. Trying to force either one to do both means accepting a permanent weakness in your hottest path.

And the scaling story falls out for free:

AI becomes the bottleneck? Spin up 10 more Python containers behind a load balancer. They need no DB credentials, no persistent volumes, no coordination. Because they hold no state, they're interchangeable.

Boring architecture. Predictable failure modes. That's the point.
```

---

## Day 4 — Why Go

```
Day 4 of Neurofiq 🐹

"Why Go and not Node or Python for the orchestrator?"

Because of what this app actually does under load.

Analysing a repo means: download a ZIP over the network, decompress it in memory, walk thousands of files, score them, and pack the best ones into a payload.

That's I/O-heavy AND CPU-heavy, at the same time, for multiple users at once.

In Node, the CPU-heavy half blocks the event loop. One user analysing a 50,000-line repo means every other user feels the lag — including someone just trying to log in.

You can fix it with worker threads. But now you're managing a pool, serialising data across thread boundaries, and debugging a concurrency model bolted onto a runtime that was designed to avoid it.

In Go, it's this:

  go func() {
      analyze(repo)
  }()

The runtime multiplexes thousands of goroutines across every available core. No pool to size. No thread boundary to serialise across. Heavy analysis simply doesn't block a login request.

The unglamorous reasons matter too:

• Compiles to a single static binary. No node_modules black hole, no "works on my machine" dependency drift.
• Strictly typed. Refactors are safe.
• Standard library is genuinely good — HTTP, ZIP, crypto all built in.

Go is boring. It's opinionated in ways that constrain you productively. And at 3am on launch day, boring is the most valuable property a language can have.
```

---

## Day 5 — Refusing vendor lock-in

```
Day 5 of Neurofiq 🐘

I use Supabase. I also deliberately disabled Supabase Auth and Supabase Storage.

Let me explain why, because it's a decision I think a lot of people get wrong.

Backend-as-a-Service is genuinely great for MVPs. Auth in 10 minutes, storage in 5. The speed is real.

But there's a trap: the more of your app depends on proprietary SDKs, the less your app is portable.

If your auth flow, your storage layer and your row-level security all live inside one vendor's abstractions, then the day they raise prices 400% — or get acquired, or deprecate the SDK you built on — you don't have a migration. You have a rewrite.

So my rule: use managed services for the boring, standardised parts. Own the parts that define your app.

What that means in practice:

• Supabase = managed Postgres. That's it. I connect with a standard connection string, same as any Postgres anywhere.
• Auth = my own GitHub OAuth flow, written in Go, sessions in signed HttpOnly cookies.
• Storage = nothing. I stream repos through memory and never persist a file.

The test of whether this was worth it:

If I need to leave tomorrow, I dump Postgres, spin up RDS, change one env var. Zero code changes. Zero SDK migration. Zero auth rewrite.

Managed Postgres is a commodity — someone else running it is pure upside. Auth is your front door. Own your front door. 🛡️
```

---

## Day 6 — Beating GitHub's rate limits for free

```
Day 6 of Neurofiq 🚧

GitHub rate-limits API calls per token. If 1,000 users log in and we fetch their repo lists every time, we burn quota fast and eventually get throttled into uselessness.

The naive fix is a TTL cache — store the repo list for 5 minutes and hope. But then users who just created a repo don't see it, and you're guessing at a number that's wrong in both directions.

There's a much better answer, and it's a header most developers throw away.

Every GitHub API response includes an ETag — a hash of that exact response body.

So instead of discarding it, my Go backend stores the ETag in Postgres against that user's profile.

Next time that user loads their repos, the request goes out as:

  If-None-Match: "<saved_etag>"

If nothing has changed since last time, GitHub responds:

  304 Not Modified

Empty body. And here's the part that makes this worth doing:

**A 304 does not count against your rate limit.**

So the flow becomes:

• Nothing changed → 304 → zero quota, instant response, serve from our DB
• Something changed → 200 with fresh data → update the cache and the ETag

You get correctness AND speed AND quota savings, with no TTL guessing. The server tells you when its data changed instead of you predicting it.

Caching is usually framed as a performance optimisation. At scale against a rate-limited API, it's a survival mechanism. ⚡
```

---

## Day 7 — Never touching the disk

```
Day 7 of Neurofiq 💾

How do you analyse a 100MB repository without touching your hard drive?

The obvious approach:
download the ZIP → write to /tmp → unzip → read files → delete folder

It works on your laptop. It falls apart on a real server.

Here's what actually happens under load:

50 users analyse repos simultaneously. Each writes ~100MB, unzips to maybe 300MB, reads it, deletes it. Your disk queue length spikes, IOPS max out, and on cloud storage you hit the throttle ceiling and everything crawls.

Worse: if the process crashes mid-run, that temp folder never gets deleted. Do that a few hundred times and you're paged at 2am for a full disk.

So my Go backend never touches the disk at all:

  resp, _ := http.Get(zipballURL)
  data, _ := io.ReadAll(resp.Body)

  zr, _ := zip.NewReader(bytes.NewReader(data), int64(len(data)))
  // read entries straight out of RAM

Go's archive/zip reads directly from anything implementing io.ReaderAt — including a plain []byte. Extract the text, discard the binary, done.

What this buys me:

• No temp files → no cleanup cron, no orphaned folders after a crash
• No disk queue → no IOPS ceiling on concurrent analyses
• RAM is fast and, at these sizes, cheap

There's a real trade-off worth stating: this puts an upper bound on repo size, because a huge repo means a huge allocation. That's an acceptable constraint — I cap it and reject early with a clear error.

Constraints you choose deliberately are features. Constraints you discover in production are outages. 🚀
```

---

## Day 8 — The boundary I refuse to cross

```
Day 8 of Neurofiq 🐍

My Python AI worker has one rule that I will not break, no matter how convenient it gets:

It is never allowed to touch the database.

This sounds arbitrary. It's the most important line in the architecture.

Here's the flow as it stands:

Go receives the request →
  validates the session →
  checks the billing limit →
  downloads and extracts the repo →
  packages everything into one payload →
  sends it to Python →
Python calls the LLM and returns structured JSON.

Python holds no state. Ever.

Why this matters so much:

The moment Python needs to answer "is this user on the paid plan?", it needs a DB connection. Now two services read and write the same state, with no shared transaction between them.

Congratulations — you've just invented a distributed race condition, and you'll find it in production, six weeks from now, at the worst possible time.

Keeping Python stateless makes it a pure function:

  f(code) = analysis

Same input, same output, no side effects, no ordering requirements.

The payoff shows up when you scale.

AI load spikes? Spin up 10 identical Python containers behind a load balancer. No DB credentials to distribute. No connection pool exhaustion. No persistent volumes. No leader election. No migration coordination.

They're interchangeable *because* they're stateless.

Microservices don't fail because you drew the boundaries. They fail because you drew them and then let them blur when a feature was in a hurry. ⚖️
```

---

## Day 9 — The CSRF hole in my own OAuth flow

```
Day 9 of Neurofiq 🔒

Security audit day. I found a real vulnerability in code I wrote myself, and it's a good one to learn from.

OAuth flows use a `state` parameter for CSRF protection. You generate a random value, send it to the provider, and verify it matches when they redirect back.

Mine looked like this:

  state := "random_string_for_security"

And in the callback:

  if c.Query("state") != "random_string_for_security" {
      reject()
  }

Read that twice. It's comparing a constant against itself. It always passes.

That's not weak CSRF protection. It's the appearance of CSRF protection with none of the substance — which is worse, because it stops you looking.

The actual attack:

1. Attacker starts their own OAuth flow and captures a valid authorization code for that fixed state
2. Attacker tricks a logged-in victim into hitting the callback URL with it
3. Victim's session is now bound to the ATTACKER's GitHub identity

Textbook login-CSRF. The victim's future activity flows into an account the attacker controls.

The fix — generate it properly, per login, and bind it to the browser:

  b := make([]byte, 32)
  rand.Read(b)          // crypto/rand, never math/rand
  state := base64.URLEncoding.EncodeToString(b)
  session.Set("oauth_state", state)

Then on callback: compare against the session value, and delete it immediately so it can't be replayed.

The insight worth keeping:

The security property was never "the state is random." It's "the state is bound to THIS browser's session." Randomness is just how you make that binding unforgeable.

Hardcoding it removed the binding — the only thing that ever made it work.

Also hardened every cookie while I was in there: HttpOnly (blocks XSS theft), Secure (HTTPS only), SameSite=Lax.

Audit your own code like a stranger wrote it. 🕵️
```

---

## Day 10 — The extractor that decides what the AI reads

```
Day 10 of Neurofiq 🧹

You cannot feed an entire repository to an LLM. Even with a 128k context window.

Three reasons:

1. Cost — you pay per token, and most of a repo is noise
2. Latency — bigger context, slower response
3. Quality — this is the real one. Bury the important code in 100k tokens of boilerplate and the model's attention degrades. Classic needle-in-a-haystack.

So before any AI touches anything, a pure-Go extractor decides what's worth reading.

Step 1 — kill the noise:

node_modules, .git, vendor, dist, images, lock files, minified bundles. Dropped immediately. This alone removes 80-95% of most repos.

Step 2 — score what's left:

Files that define architecture score high:
  main.go, package.json, docker-compose.yml, schema files, entry points

Files that describe implementation detail score low:
  deeply nested test mocks, fixtures, generated code

Step 3 — pack to a hard budget:

Sort by score descending. Add files until you hit 60,000 characters. Stop.

The result: the AI only ever sees the most architecturally significant files in the repo.

Two properties I care about here:

**It's deterministic.** Same repo, same output, every time. No temperature, no variance, no "why did it pick different files this run?"

**It costs $0.** There's zero AI spend involved in deciding what the AI should read. Using an LLM to choose LLM input is a tempting loop that just doubles your bill.

Max signal, minimum noise, and the expensive model only gets pointed at things worth its attention. 🥩
```

---

## Day 11 — The race condition that gave away free compute

```
Day 11 of Neurofiq 🏎️

I set a hard limit: free users can analyse 3 repos. Each analysis costs me a real LLM call, so this is the thing standing between me and an unbounded bill.

Then I realised a user could trivially get 50.

The code looked completely reasonable:

  count := db.Count(profiles).Where(user_id)
  if count >= 3 { reject() }
  // ...do the analysis, save the row

Read it top to bottom and it's obviously correct. Read it as ten simultaneous requests and it's obviously broken.

This is TOCTOU — Time Of Check to Time Of Use.

Fire 10 requests in the same millisecond:

1. All 10 ask the DB: "has this user used fewer than 3?"
2. The DB honestly tells all 10: "yes, they're at 0"
3. All 10 pass the check
4. All 10 run the analysis

The check was never atomic with the use. There's a window between them, and concurrency lives in that window.

The fix — a Postgres advisory lock keyed to the user:

  tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", userID)

Now concurrent requests for the SAME user queue up and run one at a time. Different users are unaffected — the lock is per-user, so there's no global bottleneck. And `_xact_` means it auto-releases on commit or rollback, so a panic can't leave it stuck.

But locking alone still wasn't enough.

Because if request #1 only *checks* inside the lock and does the actual insert later (asynchronously), then request #2 acquires the lock and still sees a stale count.

So inside the same locked transaction I also insert a "pending" placeholder row. Check AND claim, atomically. The analysis fills that row in later.

Verification, because I don't trust reasoning alone on concurrency:

Fired 6 simultaneous requests for one user with a 3-repo limit.
Result: exactly 3 succeeded, 3 rejected. 🎯

Concurrency bugs never show up in manual testing. You click one button at a time. You have to deliberately go looking for them.
```

---

## Day 12 — Making an LLM reliable enough to build on

```
Day 12 of Neurofiq 🧠

Wrapping an LLM in an API call is a weekend project. Making one reliable enough that your backend can depend on it is the actual work.

Here's the failure mode that keeps you up at night:

Your prompt says "return JSON." The model returns JSON 98% of the time. The other 2% it wraps it in markdown fences, or adds a friendly sentence before it, or truncates mid-object.

So 2% of your users hit a 500. And you "fix" it by writing regex to strip markdown fences, and then another patch for trailing commas, and now you maintain a JSON parser made of hope.

Two decisions solved this.

**Model: DeepSeek**

GPT-4 class output on coding tasks at a fraction of the price. When you're bootstrapping and every analysis is a real API call, cost per token isn't a line item — it's an architectural constraint. Cheap enough to be generous with retries changes what you can build.

**Framework: Agno with a strict output schema**

I define the shape as a Pydantic model:

  class AnalysisResult(BaseModel):
      architecture_patterns: List[str]
      overall_complexity: str
      strengths: List[str]
      areas_for_probing: List[ProbingArea]

  agent = Agent(
      model=DeepSeek(id="deepseek-chat"),
      output_schema=AnalysisResult,
  )

Agno instructs the model toward that schema and validates the response against it. If validation fails, that's caught inside the framework — not by my Go backend at 2am.

What I get back is already a typed Python object. Not a string I have to be brave about.

The result downstream: my Go service does json.Unmarshal into a typed struct and it succeeds every time. No regex. No defensive string cleanup. No "unexpected end of JSON input" in the error logs.

When machines talk to machines, never negotiate the format at runtime. Enforce a schema at the boundary. 🎯
```

---

## Day 13 — The breakthrough (and it wasn't a prompt)

```
Day 13 of Neurofiq 📝

Genuine breakthrough today, and the interesting part is where the fix actually was.

My AI was generating questions like:

"Explain the architecture of your application."
"What were the main challenges in this project?"

Technically relevant. Completely forgettable. It reads like a form, not an interview.

My first instinct was to improve the prompt. I spent a while there. "Be more specific." "Ask deeper questions." "Reference the code." Marginal improvements at best.

The problem wasn't the instruction. It was that the question-generation step didn't HAVE anything specific to reference.

So I changed the data model instead.

The analysis phase now extracts, for each interesting decision:

  class ProbingArea(BaseModel):
      topic: str
      file_reference: str      # exact file it came from
      code_snippet: str        # the actual 3-5 lines

Those get cached in Postgres during analysis. Then question generation injects them directly, with an explicit instruction to build questions around that exact code.

Here's a real question it produced from a candidate's repo:

"In the OpenWA repo, you implement a multi-engine adapter pattern supporting both Baileys and whatsapp-web.js. How do you abstract the differences between these libraries to maintain a consistent internal API, and what breaking changes have you hit during version upgrades?"

That's not a generic question with a repo name pasted in. That's a question you can only ask if you read the code.

You cannot bluff that. If you built it, you'll have a great answer ready. If you copy-pasted it from a tutorial, the next 30 seconds are going to be very quiet.

The lesson I keep coming back to:

When AI output feels generic, prompt engineering is the expensive dead end. Give the model better structured data and the quality problem often just disappears. 🔥
```

---

## Day 14 — Killing the sycophant

```
Day 14 of Neurofiq 🎭

Default LLM behaviour is a serious problem for an interview product, and it took me a while to name it precisely.

LLMs are trained to be agreeable. Ask one to evaluate a weak answer and you get:

"Great start! You've touched on some important concepts here..."

In a chatbot, that's pleasant. In an interview product, it's a lie — and it makes the entire product worthless, because the whole value proposition is honest signal.

So I gave my agents explicit behavioural instructions.

For the **question** agent:

• Phrase every question the way a senior engineer would actually ask it out loud — never as a numbered spec item
• Reference the candidate's real code by name so it feels like a conversation about their work, not a quiz
• Sound curious and collegial: "I noticed you used X here — walk me through why" beats "Explain your use of X"
• Never mention that you're an AI

For the **evaluation** agent, a different set entirely:

• Write feedback the way a thoughtful hiring manager would say it face-to-face — direct and specific, encouraging even when the score is low
• Acknowledge what they got right BEFORE the gaps. Never open with criticism.
• Ban rubric-speak. "Lacks depth" is useless. Say concretely what was missing and what a stronger answer would have covered.

Now the cost optimisation nobody talks about:

I have four AI agents. I only gave persona instructions to the two that produce **candidate-facing** text.

My analysis agent and my company-discovery agent output internal JSON that no human ever reads. Spending prompt tokens teaching them to sound warm and human is pure waste — on every single call, forever.

Persona is a real feature. It's also a real line item. Spend it only where a human will actually see it.
```

---

## Day 15 — Scoring something as fuzzy as reasoning

```
Day 15 of Neurofiq 📊

How do you objectively score a spoken system-design answer?

Code is easy to grade — it compiles or it doesn't, tests pass or they don't. But "explain how you'd scale this" has no ground truth. Two great answers can be completely different.

Get this wrong and the score is noise, and a noisy score is worse than no score at all, because people act on it.

The evaluator agent takes three inputs:

1. The repo context (what they actually built)
2. What a strong answer to this question looks like
3. What the candidate actually said

And it scores 0-10 on four fixed pillars:

• Correctness — is it technically right?
• Depth of reasoning — do they know WHY, or just WHAT?
• Communication clarity — could a teammate follow this?
• Awareness of trade-offs — do they know what their choice costs?

The pillars being fixed is the whole trick. It's what makes scores comparable across different candidates, different repos, and different questions. A free-form "rate this answer" prompt gives you a number that means something different every time you call it.

But the field I'm proudest of isn't the score.

For every question, the evaluator also writes an "Ideal Answer Concept" — concretely what a strong answer would have covered. Not "you were wrong," but:

"A robust answer would explain using Redis INCR with EXPIRE for a fixed window, discuss the trade-off of added latency and a new dependency, and address the failure mode when Redis is down — fail-open vs fail-closed."

That single field changes what the product IS.

Without it, you get a verdict you can't act on. With it, you get a study plan generated from your own code and your own gaps.

Most interview feedback tells you that you failed. Very little tells you what to learn next. 📈
```

---

## Day 16 — Async Go, and a gotcha that can kill your whole server

```
Day 16 of Neurofiq ⏳

LLM calls are slow. A full repo analysis takes ~20 seconds end to end.

Browsers typically time out around 30s. Put Vercel or Cloudflare in front and the ceiling drops further. So "hold the HTTP connection open and hope" is not a strategy — it's a coin flip that gets worse under load.

So I decoupled it:

User clicks Analyze
  → Go fires a goroutine to do the work
  → API immediately returns 202 Accepted
  → React polls a /status endpoint every 3s
  → Status flips to "completed" when Postgres has the result

No Redis. No Celery. No RabbitMQ. No worker fleet.

A goroutine IS the job queue when your job is I/O-bound and idempotent. Don't add infrastructure you'd have to operate, monitor and pay for before you've proven you need it.

⚠️ Now the part that nearly cost me the entire server.

**Gin's Recovery() middleware does NOT protect goroutines you spawn inside a handler.**

Recovery wraps the request goroutine. The moment you `go func()`, you've left its scope. An unrecovered panic in there doesn't return a 500 — it takes down the whole process. Every connected user, every in-flight request, gone.

One malformed ZIP from one user could kill the server for everyone.

Every background goroutine needs its own:

  defer func() {
      if r := recover(); r != nil {
          log.Printf("PANIC in background job: %v", r)
          // release the reserved slot so the user can retry
      }
  }()

I also clean up the reserved quota slot in that recover, so a crashed analysis doesn't silently eat one of the user's 3 free repos.

The general lesson, and it applies to every language:

Framework safety nets stop at the request boundary. The moment you spawn something that outlives the request, you're outside the net and you own it entirely. 🪤
```

---

## Day 17 — The frontend stack, and why not Next.js

```
Day 17 of Neurofiq 🎨

React + Vite + Tailwind. No Next.js. That gets a reaction, so let me justify it.

Next.js is excellent at server-side rendering, SEO and fast first paint on public pages.

Neurofiq's app is an authenticated dashboard behind a login. There is no SEO to win — Google can't see any of it. There's no first-paint story to optimise for anonymous visitors, because there are none.

So SSR here is pure operational overhead: a Node server to run and pay for, hydration mismatches to debug, and a mental model of "does this run on the server or the client?" on every single component.

For an authenticated SPA, a static bundle on a CDN is simpler and genuinely faster after first load.

What each piece earns its place for:

**Vite** — HMR is effectively instant. That sounds like a small quality-of-life thing until you're 200 iterations deep into tuning a report layout. Dev speed compounds into more polish shipped.

**Tailwind v4** — CSS-first config now. Design tokens live in an @theme block, no config file:

  @theme {
    --color-paper: #fbfbfa;
    --color-surface: #ffffff;
    --color-ink: #0a0a0a;
    --color-line: #e8e8e6;
    --color-accent: #15803d;
  }

The decision that actually made the UI look good:

I defined the complete token set — neutrals, accent, status colours, type scale — BEFORE building a single component. Every card, badge, border and hover state pulls from it.

That's why the app looks like a coherent product instead of a collection of screens built on different days. And I never installed a component library. No MUI, no shadcn. Just tokens and Tailwind.

Consistency isn't a component library. It's a decision you make once, early, and then don't renegotiate. 💅
```

---

## Day 18 — The $0 voice interview

```
Day 18 of Neurofiq 🎙️

I wanted voice interviews. Speaking your answer is a completely different (and more realistic) experience than typing it.

The standard stack:

• Deepgram or AssemblyAI for speech-to-text
• ElevenLabs or PlayHT for text-to-speech
• WebSockets to stream audio both directions in real time

That's per-minute billing on two vendors, plus streaming infrastructure to build and babysit. For a pre-revenue product where any user could sit in a 30-minute interview, that's a variable cost I couldn't defend.

So I shipped it for $0, using an API that's been sitting in the browser for years.

**The Web Speech API.**

  // AI asks the question
  const utter = new SpeechSynthesisUtterance(question)
  speechSynthesis.speak(utter)

  // Candidate answers
  const rec = new webkitSpeechRecognition()
  rec.continuous = true
  rec.onresult = e => setAnswer(transcriptFrom(e))

Both run natively on the user's device.

What this means architecturally:

My backend never sees audio. It receives the final transcript as plain text — the exact same payload the typed mode already sends. One code path on the server, two input modes on the client.

The wins:

• Cost: $0. Not "cheap." Zero. It doesn't scale with usage at all.
• Latency: near-instant, because no audio ever crosses the network.
• Infrastructure: none. No WebSocket server, no connection state, no reconnection logic.

The honest trade-off, because there always is one:

Browser support is uneven. Chrome is excellent, others vary, and recognition quality is worse than a paid API on heavy accents or in noisy rooms.

So text mode is always available, and voice is explicitly the optional path. If a paid transcription tier ever makes sense, the boundary is already clean — the server only ever wanted text.

Before you pay a vendor, check what the platform already gives you for free. 🤑
```

---

## Day 19 — Perception is a feature

```
Day 19 of Neurofiq 👀

Two UX problems that turned out to be the same problem.

**Problem 1:** The interview didn't FEEL like an interview. It felt like a form with an AI attached. People weren't taking it seriously, which meant the answers — and therefore the signal — were weak.

**Problem 2:** After clicking Analyze, users stared at a spinner for 20 seconds. Some assumed it was broken and left.

The obvious solutions are both expensive and both wrong.

For #1: record the video, run gaze detection, do real proctoring. That's an S3 bill, a privacy and consent nightmare, and an ML pipeline to maintain — for a practice tool.

For #2: make it faster. But the 20 seconds is an LLM call. I can't make it meaningfully faster.

So I solved the perception instead of the mechanics.

**The camera that records nothing.**

  navigator.mediaDevices.getUserMedia({ video: true })

A Google-Meet-style split view showing the candidate their own local feed. No upload. No recording. No backend. No AI watching anyone.

But seeing yourself on screen makes you sit up, look at the camera, and speak in full sentences. That's the entire feature — and it delivers most of the behavioural value of real proctoring at zero cost and zero privacy risk.

**The loading screen that tells the truth.**

Instead of a spinner, cycle through the pipeline's actual stages:

"Cloning repository..."
"Scanning dependency tree..."
"Evaluating architectural patterns..."
"Identifying areas to probe..."

These aren't invented — they're genuinely what Go and Python are doing at that moment. Users wait happily when they can see progress, and the same 20 seconds now builds anticipation instead of doubt.

⚠️ A real bug the camera taught me:

getUserMedia returns a promise. If the user toggles the camera OFF while the permission prompt is still open, your cleanup runs with `stream` still null — a no-op. Then the promise resolves and attaches the stream anyway.

Camera light stays ON while your UI says "off."

That's not a UI bug. That's a trust bug, and a serious one. Always guard async effects with a cancelled flag:

  let cancelled = false
  // ...
  return () => { cancelled = true }

Perceived performance and perceived seriousness are real engineering problems. They just aren't always solved with more compute.
```

---

## Day 20 — Turning JSON into something a human wants to read

```
Day 20 of Neurofiq 📑

The AI produces genuinely great analysis. As JSON. Which is completely useless to the person it's about.

Today was building the report that turns it into something a candidate actually wants to open.

The structure:

• **One score at the top.** Large, unmissable. People want the headline first — hiding it under fold is just friction.

• **Per-question cards** with a colour-coded left border by score. Green / amber / red. You can scan the whole report in two seconds and know exactly where you struggled.

• **Strengths and gaps side by side.** Never stacked. Stacking implies sequence and makes the second one feel like an afterthought. Side by side reads as balance.

• **The exact code snippet** that the question came from, right there in the card. No context switching to GitHub to remember what you wrote.

• **The "Ideal Answer Concept"** under every single question.

That last one is the whole product, honestly.

Because a normal interview report is a verdict. "You scored 6/10." Now what? You don't know what a 9 would have looked like. You can't act on it.

This one says, concretely:

"A robust answer would explain using Redis with INCR and EXPIRE for a fixed window, discuss the trade-offs — scalability vs added latency and a new dependency — and address the failure mode when Redis goes down: fail-open vs fail-closed."

Now the candidate can look at their own code, read that, and think "ah — my error handling here is genuinely lazy."

That's the moment the product stops being a gatekeeper and starts being a teacher.

A small polish detail I fixed while building it: for skipped questions, the "Strengths" panel rendered as an empty heading with nothing under it. Looked broken. Now it's hidden entirely and the layout collapses to a single column.

Empty states are not a detail. They're where products look unfinished. 🚀
```

---

## Day 21 — The Job Map pivot

```
Day 21 of Neurofiq 🗺️

A pivot, driven by an uncomfortable question I asked myself:

Interview practice is useful. But nobody's actual goal is "get better at interviews." Their goal is to GET A JOB. Practice is a means to it.

So I'm building a Job Map — a directory of active startups, where they are, and who's hiring.

But I know myself well enough to know I'd never maintain a manually curated database. It'd be beautiful for two weeks and stale forever after.

So it had to be fully automatic from day one.

The pipeline:

A Go cron job runs every 6 hours (plus once on startup — otherwise a fresh deploy sits empty for 6 hours, which is a silly first impression).

It asks an Agno agent with web search to find real, currently-operating startups for a given city + sector. Structured output means it comes back as typed data, not prose I have to parse.

**The rotation trick I like most:**

24 seed queries — 6 Indian cities × 4 sectors. But how do you remember which one is next, across restarts and redeploys?

Don't. Derive it from the clock:

  interval := int64(6 * 3600)
  idx := int(time.Now().Unix() / interval % int64(len(seedQueries)))

The clock IS the cursor. Restart-proof, redeploy-proof, zero storage, zero migration. No cursor table to keep in sync.

**Dedupe on domain, never on name.**

"Rivigo" and "Rivigo (Mahindra Logistics)" are the same company. Names change, get acquired, get rebranded. `rivigo.com` is unambiguous.

  ON CONFLICT (domain) DO NOTHING

The database enforces it, so a bug in my code can't create duplicates.

**Free geocoding.**

OpenStreetMap's Nominatim for lat/lng, Leaflet for rendering. $0 in Google Maps API fees. Geocode once per new company, cache it forever.

Automated, zero marginal cost, and it gets more useful every 6 hours without me touching it. 📍
```

---

## Day 22 — Two bugs that only real data reveals

```
Day 22 of Neurofiq 📍

Both of today's bugs passed every test I ran in development. Both were completely invisible until real data hit the map.

**Bug 1: The map showed one pin.**

I geocode by city name. So all 15 companies in Delhi get the exact same lat/lng — down to the decimal.

15 markers rendered at the identical pixel. You see one. The other 14 are perfectly hidden underneath it.

The feature looked broken, and technically nothing was broken. Every marker rendered correctly. They were just stacked.

Fix: marker clustering. Overlapping pins collapse into a numbered badge — "9" over Delhi, "5" over Mumbai — and spiderfy apart when you click.

Also: auto-fit the viewport to the actual pin bounds instead of a hardcoded center. Before, the map opened centered on Bangalore at a zoom level where you had to manually zoom out to discover that Delhi existed at all.

**Bug 2: Company logos silently fell back to a generic globe.**

I don't host logos. I use Google's public favicon endpoint — free, no storage, no maintenance:

  google.com/s2/favicons?domain=stripe.com&sz=128

With an onError fallback to an initials tile for domains that have no icon.

The fallback never fired. Companies without a real favicon showed a grey globe instead of their initials.

Why? When Google has no icon for a domain, it returns a generic 16×16 globe placeholder — with a 404 status, but a completely valid PNG body.

The browser decodes it successfully. So `onError` never fires. Your error path is unreachable.

The fix — check what you actually got, not whether it failed:

  onLoad={e => {
    if (e.currentTarget.naturalWidth < 32) setFailed(true)
  }}

I asked for 128px. I got 16px. That's a placeholder, not a logo.

The lesson from both:

Three rows of seed data will never surface a stacking bug, and a happy-path status check will never surface a "successful failure."

Sometimes success IS the bug. 🕵️
```

---

## Day 23 — Real job listings, zero AI

```
Day 23 of Neurofiq 🕵️

Companies on a map is a directory. Companies with their actual open roles is a product.

So: how do you get live job listings for hundreds of companies?

The 2026 answer everyone reaches for: "point an LLM at their careers page and have it extract the jobs."

I think that's the wrong tool, for three concrete reasons:

1. **Cost** — one LLM call per company per refresh. Multiply by hundreds of companies every 6 hours.
2. **Speed** — seconds per company instead of milliseconds.
3. **Hallucination** — an LLM will confidently invent a job title that doesn't exist. In a jobs product, that's the one failure you cannot ship.

Then I looked at the problem properly and realised something obvious in hindsight:

**A careers page has to load its jobs from somewhere. And that somewhere must be public — because visitors aren't logged in.**

The clean, structured data already exists. It's sitting in a JSON endpoint. Nobody needs to "extract" anything.

So the Go service does this:

**Step 1** — fetch the careers page HTML and regex for an embedded ATS board:

  boards.greenhouse.io/<slug>
  jobs.lever.co/<slug>

Companies embed these themselves — that's how their own careers page renders. The link is right there in the markup.

**Step 2** — call that ATS's official public JSON API. No key, no auth, no scraping:

  boards-api.greenhouse.io/v1/boards/<slug>/jobs

Real result: Razorpay → 20 live roles with titles, locations and apply links.

**Step 3** — if the HTML scan finds nothing, guess the slug from the company name and verify against the API. Jobs come back? Confirmed.

Note the ordering there, because it matters:

Razorpay's Greenhouse slug is `razorpaysoftwareprivatelimited`, not `razorpay`. Guessing alone would have completely missed them. That's exactly why the HTML scan is the primary path and slug-guessing is only the fallback.

**Keeping it fresh:**

Sync is idempotent — upsert on (company_id, url), and delete any posting that's no longer in the response. Run it every 6 hours and closed roles disappear on their own.

Final numbers: $0 of AI spend, ~100ms per company, zero hallucinations, and the data is straight from the company's own system of record.

AI is for judgment calls. This was never a judgment call — it was a lookup dressed as one. 🏆
```

---

## Day 24 — Expanding job coverage ✅ *(shipped)*

```
Day 24 of Neurofiq 📈

Yesterday I shipped Greenhouse and Lever support. Today I measured how far that actually gets you — and then went hunting.

I tested 20 well-known Indian startups against the two platforms I supported.

Hit rate: 9 out of 20.

Which means over half the directory showed "no open roles" for companies that are obviously hiring. That's worse than showing nothing — it's actively misleading.

So I added four more, all free public JSON APIs, no key required:

**SmartRecruiters** — the biggest win. Swiggy: 75 roles. Freshworks: 100.
**Ashby** — common with newer, well-funded startups. Ramp: 138 roles.
**Workable** — strong in mid-size companies.
**Keka** — and this one has a story.

Keka's official developer API is partner-gated. You apply to their marketplace. Dead end.

So I opened a Keka-hosted careers page and watched the network tab:

  <company>.keka.com/careers/api/jobs/default/active

Public. No auth. Clean JSON array of open roles.

Which is yesterday's principle again: the careers page must fetch its jobs from a public endpoint, because visitors aren't logged in. The gated API is for HR admins WRITING data. The read path is necessarily open.

**Two bugs I found while testing — both of the "silently does nothing" kind:**

1. My Greenhouse regex missed regional boards. Groww uses job-boards.**eu**.greenhouse.io, and my pattern only matched the bare domain. One optional capture group fixed it.

2. Much worse: my periodic sync only queried companies where ats_type was already set.

So any company that failed detection once would never be re-checked. Which means adding four new providers would have had **zero effect on every company already in my database.** Only newly-discovered ones would benefit.

I'd have shipped a feature that appeared to work and quietly did nothing for existing data.

Fixed: the sync now re-detects companies with no ATS on every tick, because detection capability improves over time.

**The result on real data**, after one cycle:

Jupiter → 12 real roles via Keka
Turtlemint, Upstox → SmartRecruiters

The implementation cost of all four providers: 4 regex patterns, 4 fetch functions, 4 switch cases. The upsert logic, the dedupe, the delete-closed pass, the cron — completely untouched.

That's what a good abstraction earns you. The hard part was designing the pipeline once. Adding a source is now boring. 🔌
```

---

## Day 25 — Making unknown ATS platforms discover themselves ⚠️ *(build first)*

```
Day 25 of Neurofiq 🔍

I support six ATS platforms now. There are hundreds.

Every company on a niche or in-house HR tool falls through and shows zero jobs. And I can't write a regex for a platform I've never heard of — that's an infinite backlog by construction.

So instead of hardcoding more platforms, I'm making the system find them on its own.

The insight is the same one this whole feature rests on, taken one step further:

**A careers page must load its jobs from a public endpoint. So instead of guessing WHICH endpoint, just watch the page load and see.**

The approach:

1. Open the company's careers page in a headless browser
2. Record every network request the page makes
3. Filter for JSON responses
4. Score each one for "job-shaped-ness" — does it contain an array of objects with title-like and location-like fields?
5. The winner is the API. Save the URL against that company.

From then on, that company syncs like any other — no browser needed, just a plain HTTP call to the endpoint we learned.

Discover once, reuse forever.

**The honest trade-offs**, because this is the most expensive thing in the pipeline:

• A headless browser is heavy. Real RAM, real cold-start latency, real hosting cost.
• So it's strictly a fallback — it only runs when the fast regex path finds nothing.
• Response shapes vary wildly, so field mapping needs to be defensive and heuristic.
• It runs once per company, not per sync. That's what makes the cost acceptable.

The architecture ends up as three tiers, cheapest first:

1. **Regex the HTML** for a known ATS → milliseconds, free
2. **Guess the slug** and verify against a known API → milliseconds, free
3. **Headless discovery** for anything unknown → seconds, once per company

Always spend your expensive tool last, on the smallest possible set of inputs.

The nice property: platform #7, #8 and #50 need no new code from me. The system learns them. 🤖
```

---

## Day 26 — Migrating Go ↔ Python to gRPC ⚠️ *(build first)*

```
Day 26 of Neurofiq ⚡

Today I'm replacing the HTTP+JSON link between my Go orchestrator and my Python AI worker with gRPC.

Important framing: the HTTP version works fine. Nothing is on fire. This is an upgrade, not a rescue — and I think those are the more interesting engineering posts.

Three real problems with where it is today.

**Problem 1: Schema drift is silent.**

The same payload is described twice — once as a Go struct, once as a Pydantic model. Two files, two languages, maintained by hand.

Rename a field in one and forget the other, and nothing fails at compile time. It fails at runtime, in production, on a request that already cost me an LLM call.

**Problem 2: JSON overhead on large payloads.**

I ship up to 60KB of code snippets per analysis call. JSON means: serialise to text, send text, parse text back into structures. Protobuf is binary — smaller on the wire and meaningfully cheaper to encode and decode at that size.

**Problem 3: No streaming.**

Today the user waits for all 5 questions to finish generating before seeing any of them.

What gRPC gives me:

**One .proto as the single source of truth:**

  message AnalyzeRequest {
    string repo_full_name = 1;
    repeated CodeSnippet snippets = 2;
  }

That generates the Go structs AND the Python classes. Schema drift stops being a discipline problem and becomes structurally impossible.

**Server streaming**, which is the user-facing win:

Python emits each question as it's generated. Go forwards it immediately. The candidate sees question 1 while question 4 is still being written.

Same total latency. Completely different perceived speed.

**Plus, mostly for free:**
• HTTP/2 multiplexing — many concurrent calls over one connection, no repeated handshakes
• Deadlines are part of the contract, not manual context juggling on both sides
• mTLS replaces my shared `X-Internal-Secret` header with real transport-level identity

**When gRPC is the wrong call**, because it's not universal:

Public, browser-facing APIs. Browsers need grpc-web plus a proxy, tooling is thinner, and JSON is the native language of the web. The complexity isn't worth it there.

gRPC shines exactly where I'm using it: internal, service-to-service, typed, high-volume, and behind my own network boundary.

Right tool. Right boundary. That's the whole skill. 🔌
```

---

## Day 27 — Observability across two services ⚠️ *(build first)*

```
Day 27 of Neurofiq 🔭

Here's the tax nobody warns you about when you split into two services:

When something breaks, you no longer have one log stream. You have two, in different languages, with different formats, and no way to tell which lines belong to the same user request.

"Analysis failed for someone, sometime, maybe" is not debuggable.

So before launch, I'm making the split observable.

**1. Correlation IDs**

Every request that enters Go gets a UUID. That ID:

• Goes into every log line Go writes for that request
• Travels to Python in the request metadata
• Goes into every log line Python writes
• Comes back in the response header

Now one grep across both services reconstructs the entire journey of a single request. Without it, you're correlating by timestamp and vibes.

**2. Structured logs, not sentences**

  log.Printf("analysis failed for %s", repo)   ❌

  {"level":"error","event":"analysis_failed",
   "correlation_id":"...","repo":"...","duration_ms":18420}   ✅

Sentences are unqueryable. The moment you want "all failures for repos over 50MB in the last hour," prose logs are useless and structured logs are one filter.

**3. Health checks that actually check something**

  func health() {
      // ❌ return 200 OK
      // ✅ ping Postgres, ping the Python worker, THEN return
  }

A health check that always returns 200 is worse than none — your load balancer keeps routing traffic to a process that can't reach its database.

**4. Log the boundary crossings**

Every Go → Python call logs: what was sent (sizes, not contents), how long it took, and what came back. That single boundary is where the majority of weird failures live, and it's invisible unless you deliberately instrument it.

The rule I'm applying:

**If two services can't be debugged as one system, you didn't build a distributed system. You built two systems and a problem.**

This is not polish you add after launch. This is how you survive the first week of it. 🔭
```

---

## Day 28 — Deployment architecture ⚠️ *(build first)*

```
Day 28 of Neurofiq 🚢

Deployment day. Three components, three different hosting decisions, each chosen for a specific reason.

**Frontend → Cloudflare Pages**

It's a static Vite bundle. There's no server to run. Push to git, it builds, it's on a global CDN.

Cost: effectively $0. Ops burden: zero. This is the easiest decision in the entire stack, and it's only easy because I chose Vite over Next.js on Day 17. That decision is still paying rent.

**Go backend → AWS App Runner**

Container in, HTTPS endpoint out. Autoscaling, health checks and TLS handled for me.

I didn't want to run Kubernetes for two services. I also didn't want a Lambda, because my analysis goroutines outlive the request — serverless functions and background work are a genuinely bad marriage.

App Runner is the boring middle: containers without a control plane to babysit.

**Python worker → AWS App Runner, but PRIVATE**

This is the part that matters most.

The Python worker is **not exposed to the internet at all.** It sits on private networking, reachable only from the Go service.

Why this matters so much: that worker calls DeepSeek on my API key. If it were publicly reachable, my shared secret header would be the only thing between a stranger and an unlimited LLM bill.

Defence in depth:
1. Network — not routable from the internet
2. Auth — internal secret on every request, fails closed if unset
3. Cost — usage limits enforced in Go before the call is ever made

Any one of those failing shouldn't be catastrophic. That's the point of having three.

**Database → Supabase (managed Postgres)**

As covered on Day 5: managed Postgres, no BaaS SDKs, portable by design.

The through-line in all four choices:

**Pay someone else to run the boring, standardised parts. Keep full control of the parts where your product actually lives.**

CDNs, container runtimes and Postgres are commodities. My auth flow, my extraction pipeline and my AI worker's isolation are not. 🚢
```

---

## Day 29 — Billing, and why the hard part isn't checkout ⚠️ *(build first)*

```
Day 29 of Neurofiq 💳

Adding paid tiers today. Razorpay for payments — UPI-native, built for the Indian market, and the checkout integration is genuinely straightforward.

Which is why the interesting part of this post isn't the payment integration at all.

**The hard part of billing is enforcement, not collection.**

A paid tier is a promise about limits: this plan gets N analyses, that plan gets N×10. And every one of those analyses costs me a real LLM call.

So the enforcement code is the code standing between me and an unbounded bill. Getting the checkout right just means I get paid. Getting the limit right means I don't lose money on every power user.

And limit checks are exactly where concurrency bugs live:

  count := getUsage(userID)
  if count >= plan.Limit { reject() }
  // ...expensive work
  recordUsage(userID)

That's correct read top-to-bottom, and completely broken under ten simultaneous requests. All ten read the same stale count, all ten pass, all ten run.

Which is precisely the TOCTOU bug I hit on Day 11 with the free-tier repo limit.

The good news: I already fixed the pattern. Postgres advisory lock keyed to the user, check AND claim the slot inside one transaction:

  tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", userID)

Past me did present me a genuine favour.

That's the part of engineering that's hard to convey in a tutorial: solving one problem properly, at the right layer, means the next three features inherit the fix for free.

If I'd patched Day 11 with a sleep or a retry, I'd be re-solving it today with real money attached.

Other things I'm holding myself to here:

• **Webhooks are the source of truth**, not the browser redirect. Users close tabs. Payment providers retry webhooks.
• **Webhook handlers must be idempotent** — you WILL receive the same event twice.
• **Fail closed on ambiguity.** If I can't confirm a plan, treat it as free tier and let support fix it. Never the reverse.

Do the boring correctness work early. It compounds. 🔒
```

---

## Day 30 — Pre-launch audit & LAUNCH

```
Day 30 of Neurofiq 🚀 LAUNCH DAY

Before shipping, I ran a full security and correctness audit on the entire codebase. Found 10 real issues. Here are the ones worth learning from.

**🔴 My rate limiter was one GLOBAL bucket, not per-IP.**

5 requests/second — shared across every user on the platform. Three people polling a status endpoint could 429 everyone else.

I'd written it as abuse protection. It was actually a self-inflicted DoS waiting for my first traffic spike. Now it's one token bucket per client IP.

**🔴 My internal service secret had a fallback default in the source.**

If the env var ever failed to load, it silently fell back to a value anyone reading the repo could see — and that secret is the only thing protecting my AI worker from unlimited LLM calls on my card.

Now it fails CLOSED. No secret configured means every request is rejected. A service that won't start is infinitely better than one that starts insecure.

**🔴 A status endpoint reported every DB error as "still processing."**

So a genuine database failure was indistinguishable from work in progress. The frontend would poll forever, showing a spinner, for a job that was never coming back.

Always distinguish "not found yet" from "something is broken."

**🟡 A GitHub API call parsed the response without checking status.**

An expired token returns 401 with a JSON error body. That parsed "successfully" into an empty branch name, then failed three steps later with a completely useless message.

Check status before you parse. Every time.

**And my favourite — found only by actually running it:**

A placeholder row wrote `""` into a jsonb column. Postgres rejects empty string as invalid JSON.

Every single analysis request would have failed on launch day. One word fix: `"null"`.

I found it 6 hours before shipping, and only because I wrote a concurrency test that hit the real database instead of reasoning about the code. 😅

━━━━━━━━━━━━━━━━

**Neurofiq is LIVE.**

An AI interviewer that reads your actual GitHub repository, understands how you built it, and interviews you on your own architecture decisions.

Built with:
• Go — concurrent orchestrator
• Python + Agno + DeepSeek — stateless AI worker
• Postgres — no BaaS lock-in
• React + Vite + Tailwind

30 days. Every architecture decision, every bug, every embarrassing mistake, in public.

Thank you to everyone who followed along. If the deep-dives were useful, a repost genuinely helps more than you'd think. ❤️

Now go break it and tell me what you find 👇

[Your Link Here]
```

---

## Build status

| Day | Feature | Status |
|---|---|---|
| 1-23 | Core product + Job Map + ATS basics | ✅ Done |
| 24 | SmartRecruiters + Ashby + Workable + Keka | ✅ Done |
| 25 | Headless browser ATS auto-discovery | ⚠️ Build first |
| 26 | gRPC migration (Go ↔ Python) | ⚠️ Build first |
| 27 | Correlation IDs + structured logs + real health checks | ⚠️ Build first |
| 28 | Cloudflare Pages + App Runner ×2 (Python private) | ⚠️ Build first |
| 29 | Razorpay billing | ⚠️ Build first |
| 30 | Pre-launch audit | ✅ Done |

Post-launch content (Day 31-46) → `CAMPAIGN_PART2_POST_LAUNCH.md`
