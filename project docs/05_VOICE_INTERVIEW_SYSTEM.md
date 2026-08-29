# Voice Interview System — Human-Like Conversational AI

**Tier gating: Voice is a PAID-tier feature.** Text-based interview stays
free-tier (cheap, ~$0.08–0.15/interview). Voice costs 5-10x more
(~$0.28–0.35/interview even with caps) and is the premium differentiator.

This is the feature where the Go/Python split earns its complexity — Go's
goroutine model handles many concurrent WebSocket audio connections well;
Python stays focused purely on the Claude conversation logic per turn.

## Stack & Ownership

```
Go        → WebSocket connection handling (candidate ↔ server)
          → Streams audio to Deepgram (STT)
          → Streams audio to ElevenLabs (TTS)
          → Manages session state, duration caps, turn limits
          → Calls Python worker internally per turn for the AI decision

Python    → Receives {transcript, session_context} via
            /internal/voice-turn
          → Runs the Claude conversational logic
          → Returns {ai_response_text, is_followup, move_to_next}
          → Stateless — Go tells it everything it needs each call,
            Python remembers nothing between calls
```

## Flow

```
1. Go: AI speaks question (ElevenLabs TTS, streamed to candidate)
2. Candidate speaks answer (mic recording, waveform shown as visual feedback)
3. Go: streams audio to Deepgram, gets transcript
4. Go → Python: POST /internal/voice-turn {transcript, session_context}
5. Python → Claude: decides — answer sufficient → next question |
                              ambiguous → one follow-up |
                              off-topic → gentle redirect
6. Python → Go: returns ai_response_text
7. Go: streams ai_response_text to ElevenLabs, plays audio to candidate
   loop back to step 2 until interview complete
```

## What Makes It Actually Feel Human (not just "voice added")

### 1. Conversational system prompt (lives in Python worker)

```python
INTERVIEWER_SYSTEM_PROMPT = """
You are Alex, a friendly senior engineer conducting a technical interview.
You're warm but professional — like a real interviewer who wants the
candidate to succeed, not trip them up.

CONVERSATION STYLE:
- Use natural fillers occasionally ("I see", "that makes sense")
- Briefly acknowledge their answer before the next question
- If the answer is good: brief positive acknowledgment + next question
- If weak/vague: ask ONE clarifying follow-up, don't just fail them
- Keep YOUR responses SHORT (2-3 sentences max) — this is a conversation
- Never sound robotic or like reading from a script
"""
```

### 2. Voice selection

Pick a conversational ElevenLabs voice (not news-anchor robotic), use the
"Turbo" model for natural pacing/pauses. Consider Hinglish support as an
optional toggle later — not MVP.

### 3. Latency is the make-or-break factor

- Target: **under 2 seconds** response time to feel like a real
  conversation. 2-4s is acceptable. 5s+ breaks the illusion entirely.
- The Go↔Python internal call ADDS latency vs. a monolith — this must be
  budgeted for explicitly. Keep the internal call itself lean (Python
  worker should stream its Claude response back to Go rather than waiting
  for the full completion).
- Achieved via streaming at every layer:
  - Streaming STT (Deepgram natively supports this) — Go side
  - Streaming Claude response (Python starts returning text to Go before
    the full response is generated, via a streamed internal HTTP response
    or chunked transfer)
  - Streaming TTS (Go sends text to ElevenLabs sentence-by-sentence as it
    arrives from Python, not waiting for the full reply)

```go
// Go: WebSocket handler, calls Python worker, streams TTS back
func handleVoiceTurn(conn *websocket.Conn, transcript string, ctx SessionContext) {
    correlationID := uuid.New().String()

    respStream, err := callPythonWorkerStreaming(
        "/internal/voice-turn",
        map[string]any{"transcript": transcript, "session_context": ctx},
        correlationID,
    )
    if err != nil {
        // fallback handling — see doc 12
        return
    }

    var sentenceBuffer strings.Builder
    for chunk := range respStream {
        sentenceBuffer.WriteString(chunk)
        if endsWithSentenceBoundary(sentenceBuffer.String()) {
            audio := streamToElevenLabs(sentenceBuffer.String())
            conn.WriteMessage(websocket.BinaryMessage, audio)
            sentenceBuffer.Reset()
        }
    }
}
```

```python
# Python: /internal/voice-turn, streams Claude output back to Go
@app.post("/internal/voice-turn")
async def voice_turn(req: VoiceTurnRequest):
    async def generate():
        async with claude_client.messages.stream(
            model="claude-sonnet-4-6",
            max_tokens=150,  # keep short for voice
            system=INTERVIEWER_SYSTEM_PROMPT,
            messages=build_messages(req.transcript, req.session_context),
        ) as stream:
            async for text in stream.text_stream:
                yield text

    return StreamingResponse(generate(), media_type="text/plain")
```

### 4. Interruption handling

- **MVP: push-to-talk.** Candidate holds a button to speak. Simple, no
  Voice Activity Detection complexity needed, and keeps the Go WebSocket
  logic simple too.
- **Phase 2 (optional): VAD** — detect natural pauses vs "still thinking"
  silence, allow barge-in.

## UI (Voice Interview Screen)

```
┌────────────────────────────────────────┐
│     [Waveform animation - AI speaking]  │
│         "Tell me about..."              │
│      (subtitle/transcript — toggle)     │
│  ─────────────────────────────────────  │
│         🎤 [Hold to Speak]              │
│    Question 3/7        [Skip] [Mute]    │
└────────────────────────────────────────┘
```

## Cost-Safety Caps (enforced in Go, since Go owns the session/WebSocket)

```go
const (
    MaxQuestions             = 7
    MaxAnswerDurationSeconds = 90  // auto-nudge "let's wrap up"
    MaxFollowupsPerQuestion  = 1   // never more than one follow-up
)
```

```python
# Python side also caps its own output regardless, as defense in depth
CLAUDE_MAX_TOKENS_PER_TURN = 150
```

Go is the enforcement point for session-level limits (duration, question
count) since it owns the WebSocket lifecycle. Python enforces its own
per-call token cap as a second layer of defense — belt and suspenders.

## Cost Estimate (per voice interview, with caps enforced)

```
Deepgram (7 answers × ~60s avg):              ~$0.03
ElevenLabs (7 questions + 3 follow-ups):       ~$0.15
Claude (7 turns × ~150 output tokens + input): ~$0.10
─────────────────────────────────────────────────────
TOTAL:                                         ~$0.28-0.35
```

Note: this does NOT include the marginal infra cost of running two AWS App
Runner services instead of one (~$15-25/month extra base, see doc 09) —
that's a fixed cost, not a per-interview variable cost.

## Build Sequencing Note

Ship **text-based interview first** (single Go↔Python call per answer, no
WebSocket complexity) to validate the core value prop. Voice — with its
WebSocket relay, streaming requirements, and Go/Python coordination — is
meaningfully more complex to get right and should be Phase 1.5, once the
core loop is proven. See `11_MVP_ROADMAP.md`.
