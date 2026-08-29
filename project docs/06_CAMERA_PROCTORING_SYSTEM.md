# Camera & Proctoring System

Two distinct tiers of this feature — build in this order. Ownership: Go
handles all recording/storage/proctoring-event orchestration; Python is not
involved in this feature at all (no AI needed for Tier A or Tier B as
specified — face detection runs client-side, not through Claude).

## Tier A: Simple Camera Preview (cheap, ship early)

- Browser `getUserMedia` camera permission, self-view only (like a Zoom
  mirror), **no recording, no storage, no AI analysis**.
- Purpose: real-interview "practice feel," candidate can see themselves.
- Cost: $0. Complexity: low (~1-2 hours of dev work). Pure frontend, no
  backend involvement at all.

```typescript
const CameraPreview = () => {
  const videoRef = useRef<HTMLVideoElement>(null)
  const [cameraOn, setCameraOn] = useState(true)

  useEffect(() => {
    if (cameraOn) {
      navigator.mediaDevices.getUserMedia({ video: true })
        .then(stream => {
          if (videoRef.current) videoRef.current.srcObject = stream
        })
        .catch(err => console.error("Camera access denied", err))
    }
  }, [cameraOn])

  return (
    <div className="camera-preview">
      <video ref={videoRef} autoPlay muted playsInline />
      <button onClick={() => setCameraOn(!cameraOn)}>
        {cameraOn ? "Turn off camera" : "Turn on camera"}
      </button>
    </div>
  )
}
```

## Tier B: Full Recording + Proctoring System

Everything below is gated behind explicit consent and is a paid-tier /
Phase 1.5+ feature. Entirely handled by Go — no Python involvement.

### Recording Pipeline

```
Candidate Browser
├── Camera stream (video)
├── Mic stream (audio)
└── Screen (optional — tab-switch detection doesn't need screen capture,
    just Page Visibility API)
       │  MediaRecorder API, chunked (~10s chunks)
       ▼
Chunked upload (HTTP) → Go receives + reassembles
       ▼
Upload to S3 / Supabase Storage (encrypted at rest) — Go-orchestrated
       ▼
Go background goroutine: merge chunks (ffmpeg), trigger proctoring analysis
```

```go
func UploadVideoChunk(c *gin.Context) {
    sessionID := c.Param("session_id")
    chunkIndex := c.Query("chunk_index")

    file, _ := c.FormFile("chunk")
    localPath := fmt.Sprintf("/tmp/%s_chunk_%s.webm", sessionID, chunkIndex)
    c.SaveUploadedFile(file, localPath)

    go uploadToS3(localPath, fmt.Sprintf("videos/%s/chunk_%s.webm", sessionID, chunkIndex))

    c.JSON(200, gin.H{"status": "received", "chunk_index": chunkIndex})
}

func FinalizeVideo(c *gin.Context) {
    sessionID := c.Param("session_id")
    go mergeVideoChunks(sessionID)      // ffmpeg merge, goroutine
    go processProctoringEvents(sessionID)
    c.JSON(200, gin.H{"status": "processing"})
}
```

### Proctoring Checks

**Deterministic (cheap, no AI, no Python worker needed):**
- Tab switch detection (Page Visibility API, client-side, posts event to Go)
- Copy-paste detection (clipboard events, client-side)
- Window blur events (client-side)
- No-face-detected / multiple-faces-detected (client-side CV, below)

**AI-powered — deliberately NOT built:**
- Attention/engagement scoring via continuous frame analysis — too
  expensive and not required.
- Face-match-to-GitHub-avatar — **not reliable**, most GitHub avatars
  aren't real photos. Don't build this.

### Critical cost decision: run face detection client-side, not server-side

Use **MediaPipe** (Google, open-source, runs in the candidate's browser via
WASM) instead of sending video frames to Claude or AWS Rekognition
continuously. This is free and keeps both Go and Python's server load at
zero for this feature — only lightweight *event flags* (e.g.
`"no_face_detected at 2:34"`) get POSTed to Go, never raw frames.

```typescript
import { FaceDetector, FilesetResolver } from "@mediapipe/tasks-vision"

const detectFace = async (videoElement: HTMLVideoElement) => {
  const vision = await FilesetResolver.forVisionTasks(
    "https://cdn.jsdelivr.net/npm/@mediapipe/tasks-vision/wasm"
  )
  const faceDetector = await FaceDetector.createFromOptions(vision, {
    baseOptions: { modelAssetPath: "face_detector.task" },
    runningMode: "VIDEO"
  })

  const detections = faceDetector.detectForVideo(videoElement, Date.now())
  if (detections.detections.length === 0) {
    reportEvent("no_face_detected")  // POST /interviews/{id}/proctoring-event → Go
  } else if (detections.detections.length > 1) {
    reportEvent("multiple_faces_detected")
  }
}
```

### Cost Estimate (30-min interview, full recording + proctoring)

```
Storage (compressed 480p, ~100-150MB): S3 storage ~$0.003/interview
S3 bandwidth (upload):                  ~$0.01/interview
FFmpeg merge/compress (Go's App Runner instance): negligible
MediaPipe (client-side):                $0
Proctoring event logs (Go → Postgres):  negligible DB cost
──────────────────────────────────────────────────────────
TOTAL EXTRA COST PER INTERVIEW:         ~$0.02-0.03
```

This stays cheap ONLY if continuous server-side frame-by-frame AI analysis
is avoided (would be 10-20x more expensive and would also pull the Python
worker into a feature it doesn't need to touch).

### Compliance — Non-negotiable, Build Before Launch

Video of a candidate's face is biometric/sensitive personal data in most
jurisdictions (GDPR in EU, India's DPDP Act 2023).

Required before this feature goes live:
1. **Explicit consent screen**, unchecked by default:
   > "I consent to video recording for interview practice purposes. Video
   > will be stored for 30 days and can be deleted anytime."
2. **Data retention policy** — auto-delete after N days via a Go scheduled
   job, tracked in `interview_recordings.retention_expires_at`.
3. **Candidate data control** — "Delete my recordings" button (GDPR right
   to erasure), download-own-video option (data portability).
4. **Terms of Service + Privacy Policy** updated to specifically describe
   video usage, storage duration, and deletion process.
5. **No third-party sharing** — video never leaves your infra to an
   external analysis service without explicit separate consent.

### Realistic Build Sequence

```
Week 1-2: Tier A — simple camera preview (self-view only, pure frontend)
Week 3-4: Recording + storage pipeline (Go: chunked upload, S3, playback)
Week 5:   Consent + compliance layer (retention policy, delete, ToS/Privacy)
Week 6-7: Client-side proctoring (MediaPipe face/tab-switch events → Go)
Week 8:   Proctoring dashboard (optional — only if pursuing B2B/recruiter angle)
```

### Sequencing Recommendation

Ship Tier A with the MVP (cheap, fast, improves perceived quality). Defer
Tier B (recording/proctoring) until the core interview loop (repo analysis
→ questions → feedback) is validated with real users.
