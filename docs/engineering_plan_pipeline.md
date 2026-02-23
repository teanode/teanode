# Voice Pipeline Engineering Improvement Plan

**Status:** Proposed
**Module:** `github.com/teanode/teanode`
**Primary package:** `internal/voice/`
**Supporting packages:** `internal/providers/`, `internal/gw/`

---

## Context: Current Architecture Summary

> **Note on companion plan:** A second plan (`ENGINEERING_PLAN_VOICE_PIPELINE_LEVEL1_LEVEL2.md`) covers correctness hardening and architectural abstractions (deterministic provider routing, feature-flag enforcement, TurnStrategy, observer pattern). This document incorporates those correctness fixes as a mandatory **Phase 0** before latency improvements, and adopts the TurnStrategy and observer-pattern framing for Levels 1–2. The primary distinction: that plan fixes correctness but makes no measurable latency improvement; this plan does both.

---

The pipeline is a pure Go server with four concurrent goroutine loops connected by buffered channels:

```
audioInCh (cap 64)  →  audioInputLoop     → spawns transcribeAndSend()
                                                  ↓  (Whisper batch HTTP)
ttsInCh   (cap 32)  ←  llmEventForwarder  ← gateway SSE subscription
                    →  ttsSynthLoop        → (OpenAI TTS HTTP per sentence)
audioOutCh (cap 128) ← ttsSynthLoop        → audioOutputLoop → WebSocket
```

**Current latency chain (happy path, rough estimates):**
- T0 VAD detects end of speech (320 ms trailing silence timer)
- T1 Whisper batch transcription: +400–800 ms
- T2 LLM TTFB (first token): +300–600 ms
- T3 First sentence TTS synthesis: +300–500 ms
- T4 First audio frame on wire: +50–100 ms
- **Total typical: 1.3–2.3 s from end of speech to first bot audio**

**Core limitations to fix:**
1. Energy-based RMS VAD — fragile with background noise, keyboard, echoed audio
2. Batch STT — Whisper called only after full silence; adds 400–800 ms on the critical path every turn
3. No latency telemetry — no data to measure improvements or detect regressions
4. Sentence extraction is regex-only — can produce unnatural TTS boundaries
5. TTS is request-per-sentence — HTTP round trip per sentence adds latency

---

---

## Phase 0 — Correctness Prerequisites

**Must ship before any Level 1 latency work.** These are existing bugs, not new features. None of them requires touching the VAD, STT, LLM, or TTS path.

---

### P0.1 — Deterministic Provider Selection

**File:** `internal/providers/registry.go`

**Bug:** `FindTranscriber()` and `FindSynthesizer()` iterate `r.clients` which is a `map[string]Provider`. Go map iteration is intentionally randomized. With more than one provider registered that implements `AudioTranscriber`, a different provider can be selected on each call. This will become acute once Deepgram (L1.2) and ElevenLabs (L2.3) are registered alongside OpenAI.

**Fix:** Add named lookup and an ordered fallback list.

```go
// In Registry struct, add:
transcriberOrder  []string  // ordered provider names for STT fallback
synthesizerOrder  []string  // ordered provider names for TTS fallback

// New methods:
func (r *Registry) FindTranscriberByName(name string) (AudioTranscriber, bool)
func (r *Registry) FindSynthesizerByName(name string) (AudioSynthesizer, bool)
```

`FindTranscriber()` is updated to iterate `transcriberOrder` (a `[]string` set at registration time) rather than the map directly. `FindTranscriberByName()` looks up by explicit name with a capability check. Both existing call sites in `pipeline.go` continue to work unchanged — they call `FindTranscriber()` which now has deterministic order.

**Config addition** (`internal/configs/config.go` / schema):
```
voice.transcriber_provider  string  // optional; name of preferred STT provider
voice.synth_provider        string  // optional; name of preferred TTS provider
```

When set, the gateway voice adapter calls `FindTranscriberByName()` first; falls back to ordered `FindTranscriber()` if the named provider is absent or lacks the capability. Emit a `WARNING` log on fallback: `"configured transcriber %q not found, using %q"`.

**Acceptance criteria:**
- Same provider is selected across 100 consecutive calls when multiple providers are registered.
- Missing `voice.transcriber_provider` config yields no warning (uses ordered default).
- Named provider that lacks capability falls back gracefully without panic.

---

### P0.2 — Feature Flag Runtime Enforcement

**File:** `internal/voice/pipeline.go`, `internal/voice/session.go`

**Bug:** `Session.Features.ServerVAD`, `ServerTurn`, and `ServerDenoise` are accepted in `voice.start` RPC and stored in the session struct, but `audioInputLoop` in `pipeline.go` never branches on them. The flags are decorative — all sessions run server VAD and auto-turn regardless of what the client negotiated.

**Fixes:**

`ServerVAD=false` path in `audioInputLoop`:
- Do not call `vad.ProcessFrame()`. Audio accumulates into a raw buffer.
- Speech start/end is driven only by explicit `voice.input.commit` (see P0.3) or client-signaled events.
- Barge-in is still possible via client-initiated `voice.response.cancel`.

`ServerTurn=false` path:
- VAD still runs (it detects energy levels), but do not auto-commit turns on `ended=true`.
- Instead, set a `speechReady` flag. Turn commits only on explicit `InputCommit()` call.

`ServerDenoise`:
- Introduce a no-op `AudioDenoiser` interface now. No implementation yet.
- Log at session start: `"server_denoise requested but not implemented; ignoring"` when `ServerDenoise=true`.
- This creates the interface boundary for a future implementation without silent incorrect behavior.

```go
// New interface in internal/voice/gateway.go or new file:
type AudioDenoiser interface {
    Denoise(pcm []byte) []byte
}
```

**Acceptance criteria:**
- `ServerVAD=false` session does not emit `speech_started` or `speech_ended` events automatically.
- `ServerTurn=false` session does not auto-commit after VAD silence; only explicit commit triggers turn.
- `ServerDenoise=true` session logs the unimplemented warning exactly once at start.
- All existing tests pass with default feature flags (`true/true/false`).

---

### P0.3 — Make `voice.input.commit` Functional

**File:** `internal/voice/session.go` (lines 182–188), `internal/voice/pipeline.go`

**Bug:** `InputCommit()` in `session.go` emits a `turn_committed` event but does nothing else. It does not transcribe buffered audio, does not generate a turn ID, and does not call `SendMessage`. Push-to-talk mode is entirely broken.

**Fix:** Wire `InputCommit()` to the actual transcription pipeline.

```go
// Session gains an explicit input buffer used in ServerVAD=false or ServerTurn=false modes:
explicitAudioBuf  []byte  // guarded by stateMu; accumulates audio when auto-commit is off
```

In `audioInputLoop`, when `ServerVAD=false` or `ServerTurn=false`: append each frame to `explicitAudioBuf` instead of (or in addition to) `speechBuf`.

`InputCommit()` updated:
```go
func (self *Session) InputCommit(reason string) {
    self.stateMu.Lock()
    captured := append([]byte(nil), self.explicitAudioBuf...)
    self.explicitAudioBuf = self.explicitAudioBuf[:0]
    self.stateMu.Unlock()

    if len(captured) < minCommittedTurnBytes {
        self.sendVoiceEvent("turn.event", turnEventPayload{
            Event:  "turn_dropped",
            Reason: "dropped_too_short_audio",
        })
        return
    }
    turnId := self.newTurnId()
    self.startNewTurn(turnId)
    self.sendVoiceEvent("turn.event", turnEventPayload{
        TurnID: turnId,
        Event:  "input_committed",
        Reason: reason,
    })
    if !self.TryStartTurnTranscription(turnId) {
        return
    }
    go func() {
        defer self.FinishTurnTranscription(turnId)
        self.transcribeAndSend(turnId, captured)
    }()
}
```

The RPC handler `handleVoiceInputCommit` passes the `reason` field from the request payload.

**Acceptance criteria:**
- In `ServerVAD=false` mode, `voice.input.commit` creates a complete turn: transcription → LLM run → TTS response.
- Commit with < 200ms of buffered audio emits `turn_dropped` with `dropped_too_short_audio`.
- Commit with zero buffered audio emits `turn_dropped` with `dropped_empty_audio` (new reason code).
- Multiple rapid commits are serialized correctly (no race on `explicitAudioBuf`).

---

## Level 1 — Quick Wins

These fit within the current architecture, require no new goroutines beyond what Phase 0 adds, and can each be shipped and validated independently.

---

### L1.1 — ML-Based VAD with Silero ONNX

**File to modify:** `internal/voice/vad.go`
**Files to add:** `internal/voice/vad_silero.go`
**New dependency:** `github.com/yalue/onnxruntime_go` (ONNX Runtime Go bindings) + Silero VAD v5 ONNX model file

#### Why

The current `VADState.ProcessFrame()` uses a fixed RMS threshold (0.04 positive, 0.02 negative). This fails when:
- User has background noise (music, AC, traffic)
- Microphone gain differs between devices
- Echoed bot audio leaks back into mic — false barge-ins
- User speaks quietly — missed speech start

Silero VAD is a 2 MB ONNX model that produces a speech probability [0.0–1.0] per 32 ms chunk. It is the de facto standard for server-side VAD and was chosen specifically for this use case by Pipecat, LiveKit, and Daily.

#### Interface change

Extract the current implicit `VADState` contract into a formal interface. This makes the swap transparent to `audioInputLoop`.

Add to `internal/voice/vad.go`:

```go
// VADAnalyzer is the interface for voice activity detection.
// ProcessFrame receives one 20ms s16le PCM frame and returns:
//   - started: true on the frame where speech onset is confirmed
//   - ended: true on the frame where trailing silence confirms speech end
//   - score: instantaneous confidence [0.0, 1.0]
type VADAnalyzer interface {
    ProcessFrame(pcm []byte) (started bool, ended bool, score float64)
    Reset()
}
```

Rename current `VADState` to `EnergyVAD` so it continues to implement `VADAnalyzer`. No behaviour change — same thresholds, same constants.

#### New file: `internal/voice/vad_silero.go`

```go
package voice

// SileroVAD wraps the Silero VAD v5 ONNX model via onnxruntime_go.
// The model expects 512-sample windows at 16 kHz (32 ms).
// Internally it re-chunks 20 ms frames from audioInCh into 32 ms windows.
type SileroVAD struct {
    // ONNX session handle (onnxruntime_go.Session)
    // Hidden/opaque state vector (64-float h, c for LSTM)
    // Ring buffer for re-chunking 20 ms → 32 ms
    // Adaptive thresholds
    positiveThreshold float64 // default 0.50
    negativeThreshold float64 // default 0.35
    minSpeechFrames   int     // default 6 (×32ms = 192ms)
    redemptionFrames  int     // default 15 (×32ms = 480ms)
}

// NewSileroVAD loads the ONNX model from modelPath and returns a ready VADAnalyzer.
// modelPath should be an embedded or filesystem path to silero_vad_v5.onnx.
func NewSileroVAD(modelPath string) (*SileroVAD, error)
```

Key implementation notes for the implementing agent:
- Silero v5 ONNX input: `[1, 512]` float32 tensor named `"input"`, plus `h` and `c` state tensors
- Audio input is s16le; convert to float32 in [-1.0, 1.0] before inference
- The LSTM state (`h`, `c`) must persist across frames — do NOT reset between frames within one session
- Reset the LSTM state on session start and after confirmed speech end
- Re-chunk: maintain a 32-sample carry buffer; when you have 512 samples (32ms) run inference; emit partial probability for intermediate 20ms frames by linear interpolation
- Embed the model file using Go 1.16 `//go:embed silero_vad_v5.onnx` in a small `internal/voice/assets.go` file; download the model during CI from the official Silero releases and cache it
- The model file is approximately 2 MB; commit it to the repo under `internal/voice/` or `assets/models/`

#### Pipeline integration

In `pipeline.go`, `audioInputLoop()` currently constructs `vad := &VADState{}`. Change to:

```go
var vad VADAnalyzer
if sileroModelAvailable() {
    var err error
    vad, err = NewSileroVAD(sileroModelPath)
    if err != nil {
        pipelineLog.Warningf("silero vad unavailable, falling back to energy vad: %v", err)
        vad = &EnergyVAD{}
    }
} else {
    vad = &EnergyVAD{}
}
```

No other changes to `audioInputLoop` — the interface is identical.

#### Config

Add to `Features` struct in `session.go`:

```go
type Features struct {
    ServerVAD     bool   `json:"server_vad"`
    ServerTurn    bool   `json:"server_turn"`
    ServerDenoise bool   `json:"server_denoise"`
    BargeIn       bool   `json:"barge_in"`
    SileroVAD     bool   `json:"silero_vad"`    // NEW: prefer Silero over energy VAD
}
```

#### Validation

Run the e2e test suite twice — once with energy VAD (default), once with Silero — and compare:
- False speech starts per session (observed from `speech_started` events with no subsequent `turn_committed`)
- Spurious barge-ins per session
- VAD-to-speech-end latency (should be similar or lower)

---

### L1.2 — Streaming STT via Deepgram

**Files to modify:** `internal/providers/interface.go`, `internal/voice/gateway.go`, `internal/voice/pipeline.go`
**Files to add:** `internal/providers/deepgram.go`
**New dependency:** none (use standard `net/http` WebSocket via `gorilla/websocket` already in go.mod)

#### Why

The current `transcribeAndSend()` is called after the full speech buffer is collected (at VAD end). It then:
1. Wraps PCM in WAV
2. POSTs to Whisper `/audio/transcriptions`
3. Waits for HTTP response
4. Returns text

This adds 400–800 ms to every turn. With streaming STT (Deepgram Nova-2), audio is piped in real-time and a final transcript arrives within ~100 ms of the last spoken word — because the model is processing audio as the user speaks.

#### New interface additions to `internal/providers/interface.go`

```go
// StreamingTranscriber is an optional capability for real-time speech-to-text.
// A provider may implement both AudioTranscriber and StreamingTranscriber.
type StreamingTranscriber interface {
    OpenTranscribeStream(ctx context.Context, req StreamTranscribeRequest) (TranscribeStream, error)
}

// StreamTranscribeRequest contains parameters for opening a streaming STT session.
type StreamTranscribeRequest struct {
    SampleRateHz int
    Channels     int
    Language     string // optional BCP-47 hint
    Model        string // e.g. "nova-2"
}

// TranscribeStream is an active streaming STT session.
// The caller sends audio via SendAudio and receives events from Events().
// The stream must be closed by calling Close() when done.
type TranscribeStream interface {
    SendAudio(pcm []byte) error
    Events() <-chan TranscribeStreamEvent
    Close() error
}

// TranscribeStreamEvent is one STT result from a streaming session.
type TranscribeStreamEvent struct {
    Type       string  // "interim" | "final" | "error"
    Text       string
    IsFinal    bool
    Confidence float64
    Err        error   // set when Type == "error"
}
```

#### New registry method in `internal/providers/registry.go`

```go
// FindStreamingTranscriber returns the first provider implementing StreamingTranscriber.
func (r *Registry) FindStreamingTranscriber() (StreamingTranscriber, string, bool) {
    for name, client := range r.clients {
        if st, ok := client.(StreamingTranscriber); ok {
            return st, name, true
        }
    }
    return nil, "", false
}
```

#### New voice interface in `internal/voice/gateway.go`

```go
// VoiceStreamingTranscriber wraps a streaming STT session for voice use.
type VoiceStreamingTranscriber interface {
    OpenStream(ctx context.Context, format AudioFormat) (VoiceTranscribeStream, error)
}

type VoiceTranscribeStream interface {
    SendAudio(pcm []byte) error
    Events() <-chan VoiceTranscribeEvent
    Close() error
}

type VoiceTranscribeEvent struct {
    Type    string // "interim" | "final" | "error"
    Text    string
    IsFinal bool
    Err     error
}
```

Add to `VoiceProviderRegistry`:

```go
FindStreamingTranscriber() (VoiceStreamingTranscriber, string, bool)
```

#### Pipeline integration — `internal/voice/pipeline.go`

When a `VoiceStreamingTranscriber` is available, replace batch transcription with streaming. The `audioInputLoop` is restructured as follows:

```
Session start:
  → If streaming transcriber available:
      → Open TranscribeStream (one persistent WebSocket connection per session)
      → Spawn goroutine: streamingTranscribeLoop()

audioInputLoop changes:
  → Each frame is sent to VAD as before
  → If streaming mode AND vad.IsSpeaking: also forward frame via stream.SendAudio()
  → When VAD fires ended: no longer spawn transcribeAndSend goroutine
    (the streaming transcriber will emit a final event)

New goroutine: streamingTranscribeLoop()
  → Reads from stream.Events()
  → On Type=="final": calls transcribeAndSend(turnId, text) with pre-validated text
    (no WAV wrapping, no HTTP call — text already available)
  → On Type=="interim": records latest interim text in session state
    (used by L2.1 speculative LLM below)
  → On Type=="error": log warning, fall back to Whisper for this turn
```

Add to `Session` struct:

```go
streamingSTTStream  VoiceTranscribeStream   // nil if not using streaming STT
interimText         string                  // latest interim transcript (guarded by stateMu)
```

#### New file: `internal/providers/deepgram.go`

Implement `DeepgramClient` that implements `Provider` (stub/no-op for LLM methods) and `StreamingTranscriber`.

Deepgram streaming protocol:
- Connect to `wss://api.deepgram.com/v1/listen?model=nova-2&encoding=linear16&sample_rate=16000&channels=1&interim_results=true&endpointing=false`
- Auth: `Authorization: Token <api_key>` header on WebSocket upgrade
- Send: raw s16le PCM bytes (no framing)
- Receive: JSON messages with structure:
  ```json
  {
    "type": "Results",
    "channel": { "alternatives": [{ "transcript": "...", "confidence": 0.99 }] },
    "is_final": true,
    "speech_final": false
  }
  ```
- On `is_final == true && speech_final == true`: use as the committed transcript
- On `is_final == true && speech_final == false`: use as interim
- Keep-alive: send JSON `{"type":"KeepAlive"}` every 8 seconds
- Close: send JSON `{"type":"CloseStream"}` before closing WebSocket

Key implementation detail: One WebSocket connection per voice session (not per utterance). The connection persists for the entire call. Deepgram's `endpointing=false` disables their internal endpoint detection — we rely on our own VAD for turn boundaries.

#### Fallback strategy

If `StreamingTranscriber` is not registered or `OpenStream` fails, `audioInputLoop` falls back to the existing `transcribeAndSend` + Whisper path. This ensures backward compatibility.

---

### L1.3 — Latency Telemetry via Observer Pattern

**Files to modify:** `internal/voice/session.go`, `internal/voice/pipeline.go`
**Files to add:** `internal/voice/observer.go`, `internal/voice/metrics.go`

#### Why

Without measurement we cannot validate any improvement. The e2e harness records event timestamps, but per-component TTFB within a turn (VAD→STT, STT→LLM, LLM first token, TTS first byte) is not surfaced anywhere.

Rather than adding timestamp captures directly inside the pipeline goroutines (which couples telemetry to business logic), use an **observer pattern**: the pipeline notifies registered observers at lifecycle hooks; observers handle recording and emission. This keeps the core loops clean and allows observers to be disabled without touching pipeline code — a pattern the companion plan correctly identified.

#### New file: `internal/voice/observer.go`

```go
package voice

import "time"

// TurnObserver is notified at each stage of a voice turn's lifecycle.
// Implementations must be goroutine-safe; methods may be called from
// different goroutines (audioInputLoop, ttsSynthLoop, llmEventForwarder).
type TurnObserver interface {
    OnSpeechStarted(turnId string, t time.Time, vadScore float64)
    OnSpeechEnded(turnId string, t time.Time)
    OnTranscribeStart(turnId string, t time.Time)
    OnTranscribeEnd(turnId string, t time.Time, text string)
    OnLLMStart(turnId string, t time.Time, runId string)
    OnLLMFirstToken(turnId string, t time.Time)
    OnTTSStart(turnId string, t time.Time)
    OnFirstAudioSent(turnId string, t time.Time)
    OnResponseComplete(turnId string, t time.Time)
    OnTurnDropped(turnId string, reason string)
    OnBargeIn(turnId string, t time.Time)
}

// LatencyObserver receives the computed latency summary at the end of each turn.
type LatencyObserver interface {
    OnTurnMetrics(m TurnMetrics)
}

// IdleObserver is notified when the session has been idle beyond a threshold.
type IdleObserver interface {
    OnIdle(sessionId string, idleSince time.Time)
}
```

#### New file: `internal/voice/metrics.go`

```go
package voice

import "time"

// TurnMetrics records TTFB timestamps for one complete voice turn.
// Zero values mean the stage was not reached in this turn.
type TurnMetrics struct {
    SessionID string
    TurnID    string

    SpeechStartedAt  time.Time
    SpeechEndedAt    time.Time
    TranscribeStart  time.Time
    TranscribeEnd    time.Time
    LLMStart         time.Time
    LLMFirstToken    time.Time
    TTSStart         time.Time
    FirstAudioSent   time.Time
    ResponseComplete time.Time

    // Derived (computed at OnResponseComplete)
    VoiceActivityDetectionMs int64 // SpeechEndedAt - SpeechStartedAt
    STTLatencyMs             int64 // TranscribeEnd - TranscribeStart
    LLMTTFBMs                int64 // LLMFirstToken - LLMStart
    TTSLatencyMs             int64 // FirstAudioSent - TTSStart
    EndToEndMs               int64 // FirstAudioSent - SpeechEndedAt
    TotalResponseMs          int64 // ResponseComplete - SpeechEndedAt
}

// MetricsObserver is the built-in TurnObserver + LatencyObserver that
// collects timestamps and emits turn.metrics events via the session's sendVoiceEvent.
type MetricsObserver struct {
    sessionId   string
    emitFn      func(eventType string, payload interface{})
    mu          sync.Mutex
    current     TurnMetrics
}
```

`MetricsObserver.OnResponseComplete()` computes derived durations and calls `emitFn("turn.metrics", ...)` with the full struct serialized to a flat map. Also logs at INFO level.

#### Session integration

Add to `Session` struct:
```go
observers []TurnObserver  // populated at NewSession; never mutated after Start()
```

Add helper to `session.go`:
```go
func (self *Session) notifyObservers(fn func(TurnObserver)) {
    for _, obs := range self.observers {
        fn(obs)
    }
}
```

In `pipeline.go`, replace direct timestamp captures with observer calls. Example:
```go
// In audioInputLoop, after if started {
self.notifyObservers(func(o TurnObserver) {
    o.OnSpeechStarted(turnId, time.Now(), score)
})
```

The table below maps each hook to its call site (goroutine-local, no locks needed for the call itself):

| Observer method | Goroutine | Trigger |
|----------------|-----------|---------|
| `OnSpeechStarted` | `audioInputLoop` | after `if started {` |
| `OnSpeechEnded` | `audioInputLoop` | after `if ended {` |
| `OnTranscribeStart` | `transcribeAndSend` goroutine | top of function |
| `OnTranscribeEnd` | `transcribeAndSend` goroutine | after text validated |
| `OnLLMStart` | `commitVoiceTurn` (any) | before `deps.SendMessage()` |
| `OnLLMFirstToken` | `llmEventForwarder` | first `delta` with text |
| `OnTTSStart` | `ttsSynthLoop` | before first `SynthesizePCM` |
| `OnFirstAudioSent` | `ttsSynthLoop` | after first `enqueueAudioOut` |
| `OnResponseComplete` | `ttsSynthLoop` | on `""` sentinel |
| `OnTurnDropped` | multiple | on any drop path |
| `OnBargeIn` | `triggerBargeIn` | inside `bargeInOnce.Do` |

`MetricsObserver` is the default observer wired in `NewSession`. When observers are `nil` or empty, `notifyObservers` is a no-op — core loop behavior is unchanged.

#### E2E test harness update

Add handler for `turn.metrics` event in `test/voicee2e/internal/protocol/client.go`. Record into `ScenarioResult` as `[]TurnMetrics`. The JSON/Markdown report should emit p50/p90/p99 for `e2e_ms`, `stt_ms`, `llm_ttfb_ms`, and `tts_ms` across all turns in a run, enabling comparison between baseline and candidate runs.

---

### L1.4 — Client-Side Audio Gate Refinement

**Files to modify:** `web/src/hooks/useVoiceSession.ts` (or `useVoiceCall.ts` — wherever the ScriptProcessor callback lives)

#### Why

The client currently gates on RMS > 0.03 OR maxAbs > 0.12 with a 350 ms hangover. This is intended to save bandwidth but in practice the thresholds are too permissive — the client still sends near-silence frames that the server VAD then has to ignore, burning CPU and channel capacity.

More importantly, the client gate adds latency at speech onset: if a user starts speaking softly, the first frame might be below 0.12 max but still meaningful to Silero VAD. The gate can clip word beginnings.

#### Changes

1. **Remove the client-side gate entirely** when server VAD is enabled (negotiated via `features.server_vad`). The server VAD — especially Silero — is more accurate than a simple amplitude threshold. The bandwidth cost of 320 bytes every 20 ms (16 KB/s) is negligible.

2. **If keeping a gate**, reduce its role to a pure bandwidth guard at a very low threshold (silence floor only):
   - Lower RMS threshold: `0.005` (captures all audible speech including whispers)
   - Remove the `maxAbs` check entirely
   - Reduce hangover to `150 ms`

3. Add a comment in the TypeScript explaining the gate's purpose is bandwidth protection only, not VAD.

---

## Level 2 — Medium Effort

These require new state, new interfaces, or new goroutines. Each L2 item depends on or works best in combination with the L1 items above.

---

### L2.1 — Speculative LLM Calls on Interim Transcripts

**Prerequisite:** L1.2 (Streaming STT) must be implemented and providing `interim` events.
**Files to modify:** `internal/voice/session.go`, `internal/voice/pipeline.go`, `internal/voice/gateway.go`

#### Why

Current critical path after end of speech:
```
VAD end → STT final → LLM start → LLM first token
```

With streaming STT delivering interim transcripts, the user's intent is often clear 200–400 ms before they finish speaking. We can start the LLM immediately on a stable interim transcript and discard/replace it only if the final transcript diverges significantly.

Pipecat reports a consistent ~300 ms latency reduction from this technique.

#### New session state

Add to `Session` struct:

```go
speculativeRunId    string      // run ID of the current speculative LLM call
speculativeText     string      // text used for speculative call
speculativeMu       sync.Mutex  // separate lock to avoid contention with stateMu
```

Add to `VoiceProviderRegistry` / `GatewayDeps`:

```go
// CancelRun cancels a run by ID without marking it as a user-triggered barge-in.
// Used for speculative run cleanup on transcript mismatch.
CancelRun(runId string)
```

#### Algorithm in `audioInputLoop` / new goroutine

When the streaming STT emits an `interim` event:
1. Record `session.interimText = event.Text`
2. If `len(interimText) >= speculativeMinRunes` (suggested: 20 runes) AND confidence > 0.80:
   - If no speculative run is active AND no real run is active:
     - Call `deps.SendMessage()` with `interimText` and a flag `IsSpeculative: true`
     - Store `speculativeRunId` and `speculativeText`
   - If a speculative run is active and the new interim differs significantly:
     - Cancel previous speculative run via `CancelRun(speculativeRunId)`
     - Start a new speculative run with updated text

When the streaming STT emits a `final` event:
1. Compute similarity between `finalText` and `speculativeText`
   - Use normalized edit distance: `editDistance(a, b) / max(len(a), len(b))`
   - If similarity ≥ 0.80 (texts are close enough):
     - **Promote** the speculative run: it becomes the real run
     - `SetCurrentRunId(speculativeRunId)`, clear `speculativeRunId`
     - The LLM response that's already streaming is the correct response
     - Emit `transcript.final` event as usual
   - If similarity < 0.80 (texts diverged):
     - Cancel the speculative run via `CancelRun(speculativeRunId)`
     - Start a fresh LLM run with the final text
     - Log: `"speculative run cancelled (diverged): similarity=%.2f"`

On barge-in (in `triggerBargeIn()`):
- Also cancel the speculative run if one is active

#### Similarity function

Add to `internal/voice/` a small `similarity.go` file implementing Wagner-Fischer edit distance on rune slices. This is O(n×m) but utterances are short enough that it's negligible. Alternatively, use a simple token-level Jaccard similarity for speed.

#### Gateway change — speculative flag

Add `IsSpeculative bool` to `VoiceSendMessageParams`. The gateway should handle speculative runs identically to real runs internally, but may log them differently. The cancellation via `CancelRun()` (not `AbortRun()`) should suppress the `barge_in_triggered` event since the user did not actually interrupt — the transcript simply refined.

#### Guardrails

- Do not start a speculative run if barge-in was recently triggered (within 500 ms)
- Do not start a speculative run if `HasTranscriptionInFlight()` is true (transcript not yet confirmed)
- Cap speculative run promotion window at 3 seconds: if the speculative run is > 3 seconds old when the final transcript arrives, cancel it and start fresh (model may have already hallucinated an irrelevant response)

---

### L2.2 — TurnStrategy Abstraction + Adaptive End-of-Turn Detection

**Prerequisite:** L1.2 (Streaming STT) for interim transcripts; L1.1 (Silero VAD) for reliable VAD scores.
**Files to modify:** `internal/voice/pipeline.go`, `internal/voice/session.go`
**Files to add:** `internal/voice/turn_strategy.go`, `internal/voice/turn_strategy_balanced.go`

#### Why

Two related problems share a root cause — both are hardcoded thresholds in `audioInputLoop` with no way to tune or swap:

1. **Premature end-of-turn commits**: The 320 ms silence timer fires mid-sentence when the user pauses ("I want to... book a flight").
2. **False barge-ins**: The `score >= 0.06` check in `audioInputLoop:57` fires on keyboard sounds and echo during playback.

The companion plan correctly identified that the fix is a **`TurnStrategy` interface** that makes both decisions pluggable. This is a stronger abstraction than a standalone `EndOfTurnDecider` because it covers the barge-in decision too, and both decisions share context (VAD score, interim text, response state).

#### New file: `internal/voice/turn_strategy.go`

```go
package voice

// TurnDecision is the output of a TurnStrategy barge-in evaluation.
type TurnDecision int

const (
    TurnDecisionIgnore    TurnDecision = iota // do nothing
    TurnDecisionCandidate                      // noteworthy but not yet triggered
    TurnDecisionTrigger                        // trigger barge-in now
)

// TurnContext is the read-only session state passed to strategy decisions.
type TurnContext struct {
    VADScore          float64
    SilenceDurationMs int
    InterimText       string
    RunActive         bool    // LLM run is in progress
    ResponseActive    bool    // TTS response is playing
    SpeechDurationMs  int     // how long current speech burst has been
}

// TurnStrategy encapsulates all policy decisions about speech turns and interruptions.
// Implementations must be goroutine-safe.
type TurnStrategy interface {
    // ShouldCommitTurn decides whether the current VAD-proposed end-of-speech
    // should be committed as a complete turn. ctx.SilenceDurationMs is elapsed
    // silence since VAD fired ended. ctx.InterimText is the latest streaming
    // STT interim (empty string if streaming STT is not active).
    ShouldCommitTurn(ctx TurnContext) bool

    // EvaluateBargeIn is called on each speech-active frame when a response
    // is in progress. Returns TurnDecisionTrigger to interrupt, Candidate to
    // record the potential interruption without acting, Ignore to do nothing.
    EvaluateBargeIn(ctx TurnContext) TurnDecision

    // Name returns the strategy name for logging and config.
    Name() string
}
```

#### `LegacyStrategy` — safe migration path

Reproduces current hardcoded behaviour exactly. This is the default until `balanced` is validated.

```go
// LegacyStrategy reproduces the original pipeline.go thresholds verbatim.
// ShouldCommitTurn: always true when VAD fires ended (relies on vadRedemptionFrames
//   inside EnergyVAD — no additional check).
// EvaluateBargeIn: TurnDecisionTrigger when score >= 0.06 and run or response active.
type LegacyStrategy struct{}
```

#### `BalancedStrategy` — the actual improvement

```go
type BalancedStrategy struct {
    MinSilenceMs     int // 150 default: never commit before this
    MaxSilenceMs     int // 700 default: always commit after this (safety net)
    BargeInMinScore  float64 // 0.12 default: higher than legacy 0.06
    BargeInDebounceMs int    // 120 default: ignore score spikes < this duration
}

func (b *BalancedStrategy) ShouldCommitTurn(ctx TurnContext) bool {
    if ctx.SilenceDurationMs < b.MinSilenceMs {
        return false
    }
    if ctx.SilenceDurationMs >= b.MaxSilenceMs {
        return true // unconditional safety net
    }
    text := strings.TrimSpace(ctx.InterimText)
    if endsWithSentenceTerminator(text) {
        return ctx.SilenceDurationMs >= 150
    }
    if endsWithDanglingConjunction(text) {
        return ctx.SilenceDurationMs >= 600
    }
    return ctx.SilenceDurationMs >= 350
}

func (b *BalancedStrategy) EvaluateBargeIn(ctx TurnContext) TurnDecision {
    if ctx.VADScore < b.BargeInMinScore {
        return TurnDecisionIgnore
    }
    if ctx.SpeechDurationMs < b.BargeInDebounceMs {
        return TurnDecisionCandidate // score is high but too brief — watch, don't act
    }
    return TurnDecisionTrigger
}
```

Dangling conjunction list (add to `turn_strategy_balanced.go`):
```go
var danglingConjunctions = []string{
    " and", " but", " or", " so", " because", " although", " since",
    " when", " while", " after", " before", " if", " unless", " that",
}
```

#### Barge-in candidate event

`EvaluateBargeIn` returning `TurnDecisionCandidate` emits a new event (without triggering barge-in):
```go
self.sendVoiceEvent("turn.event", turnEventPayload{
    TurnID: turnId,
    Event:  "barge_in_candidate",
    VADScore: score,
})
```

If the next frame escalates to `TurnDecisionTrigger`, barge-in fires as usual. If VAD drops back below threshold, a `barge_in_suppressed` event is emitted with `Reason: "score_dropped"`. This three-state model (candidate → triggered/suppressed) makes interruption behavior visible and debuggable.

#### Pipeline integration

In `audioInputLoop`, replace the hardcoded threshold check:
```go
// BEFORE (pipeline.go:57-61):
if self.Features.BargeIn && score >= bargeInTriggerMinScore && (...) {
    self.triggerBargeIn()
}

// AFTER:
if self.Features.BargeIn && (self.GetCurrentRunId() != "" || self.GetCurrentResponseId() != "") {
    ctx := TurnContext{VADScore: score, SpeechDurationMs: ..., RunActive: ..., ResponseActive: ...}
    switch self.strategy.EvaluateBargeIn(ctx) {
    case TurnDecisionTrigger:
        self.triggerBargeIn()
    case TurnDecisionCandidate:
        self.sendVoiceEvent("turn.event", turnEventPayload{Event: "barge_in_candidate", VADScore: score})
    }
}
```

Replace the `if ended {` auto-commit with:
```go
if ended {
    ctx := TurnContext{SilenceDurationMs: silenceDurationMs, InterimText: self.GetInterimText(), ...}
    if !self.strategy.ShouldCommitTurn(ctx) {
        continue // wait for more silence or text signal
    }
    // ... existing commit logic ...
}
```

#### Config

```
voice.turn_strategy   string  // "legacy" (default) | "balanced"
```

Selected at session creation via `NewSession`. Switch without code changes.

#### Stage 3 (future): lightweight LLM end-of-turn classifier

The `TurnStrategy` interface is the hook point. A future `LLMTurnStrategy` implementation can call `gpt-4o-mini` with a single-token yes/no prompt for accurate end-of-turn detection. This swaps in without touching `audioInputLoop`.

---

### L2.3 — Streaming TTS with First-Chunk Playback

**Files to modify:** `internal/providers/interface.go`, `internal/voice/gateway.go`, `internal/voice/pipeline.go`
**Files to add:** `internal/providers/elevenlabs.go` (or `cartesia.go`)

#### Why

Current TTS is request-per-sentence: send text → wait for full audio → send to client. The wait for OpenAI TTS is 200–400 ms per sentence. With streaming TTS, audio bytes start flowing within 50–100 ms of submitting text and we can play the first chunk while the model is still generating the rest.

#### New interface in `internal/providers/interface.go`

```go
// StreamingAudioSynthesizer supports incremental TTS where audio is returned
// as a stream of PCM chunks rather than a single response.
type StreamingAudioSynthesizer interface {
    SynthesizeStream(ctx context.Context, req SynthesizeStreamRequest) (<-chan SynthesizeChunk, error)
}

type SynthesizeStreamRequest struct {
    Text       string
    Voice      string
    SampleRate int
    // LatencyMode: "lowest" | "balanced" | "highest_quality"
    // Provider-specific; controls buffering vs latency trade-off.
    LatencyMode string
}

type SynthesizeChunk struct {
    PCM []byte // s16le PCM at requested SampleRate
    Err error  // non-nil on error; channel closes after error
}
```

Add to `VoiceSynthesizer` interface in `internal/voice/gateway.go`:

```go
// SynthesizePCMStream returns a channel of PCM chunks for streaming playback.
// If the underlying synthesizer does not support streaming, it may return a
// single chunk after full synthesis completes (graceful degradation).
SynthesizePCMStream(ctx context.Context, text, voice string, sampleRateHz int) (<-chan []byte, error)
```

#### New file: `internal/providers/elevenlabs.go`

ElevenLabs WebSocket streaming protocol:
- URL: `wss://api.elevenlabs.io/v1/text-to-speech/{voice_id}/stream-input?model_id=eleven_flash_v2_5&output_format=pcm_24000`
- Auth: `xi-api-key` header
- Protocol: send JSON text chunks → receive binary PCM audio chunks
- Request: `{"text": "Hello", "try_trigger_generation": true}` to start; `{"text": ""}` to flush; `{"text": " "}` end-of-stream signal
- Response: binary WebSocket frames containing raw PCM at 24 kHz

```go
type ElevenLabsClient struct {
    apiKey  string
    voiceID string // configurable, e.g. "21m00Tcm4TlvDq8ikWAM" (Rachel)
}

// SynthesizeStream implements StreamingAudioSynthesizer.
func (e *ElevenLabsClient) SynthesizeStream(ctx context.Context, req SynthesizeStreamRequest) (<-chan SynthesizeChunk, error) {
    // Open WebSocket
    // Goroutine 1: send text in chunks (whole sentence at once for voice calls)
    // Goroutine 2: read binary frames, convert to SynthesizeChunk, send to channel
    // On ctx.Done() or error: close WebSocket, drain and close channel
}
```

**Alternative — Cartesia:** Cartesia's API (`api.cartesia.ai/tts/websocket`) uses a similar WebSocket streaming model with slightly lower latency. The interface above works for both providers.

#### Pipeline integration — `ttsSynthLoop`

Change `ttsSynthLoop` to use `SynthesizePCMStream`:

```go
chunks, err := synth.SynthesizePCMStream(ttsCtx, sentence, "alloy", self.AudioOut.SampleRateHz)
if err != nil {
    // fall back or log
    continue
}
for chunk := range chunks {
    if chunk.Err != nil {
        break
    }
    if len(chunk.PCM) == 0 {
        continue
    }
    // Record FirstAudioSent on first non-empty chunk (for L1.3 metrics)
    payload := EncodeBinaryAudioFrame(BinaryAudioFrame{
        FrameType:   FrameTypeAudioOut,
        Seq:         self.NextOutSeq(),
        CaptureTSMs: time.Now().UnixMilli(),
        Data:        chunk.PCM,
    })
    self.enqueueAudioOut(payload)
}
```

The client already handles incremental audio frames via its `outputQueueRef` playback queue — no client changes needed.

#### Graceful degradation wrapper

The adapter in `internal/gw/gateway.go` (`voiceSynthesizerAdapter`) should check for `StreamingAudioSynthesizer` first; if not available, wrap the batch `SynthesizePCM` in a single-chunk channel. This means the `ttsSynthLoop` can always use the streaming API without caring about provider capabilities.

---

### L2.4 — Conversation Context Window Management

**Files to modify:** Primarily in `internal/gw/gateway.go` (runner/agent layer); minor changes to `internal/voice/pipeline.go`

#### Why

Long voice sessions accumulate conversation history without bound. When history grows near the model's context window, the LLM starts truncating, which causes amnesia mid-call, and provider latency increases as the prompt grows. Voice calls need a tighter context budget than text chat because responses must stay brief and latency-sensitive.

#### Approach

The implementing agent should identify where conversation history is built into the prompt before each LLM call. This is in the runner/agent code in `internal/gw/gateway.go` or adjacent agent files. The exact location depends on how `SendMessage` assembles the request.

Implement the following strategy at the point where messages are assembled:

**Token budget:** Set a voice-specific context limit. For GPT-4o: 16,000 tokens max context (leaving 4,000 for the response). For Claude 3.5 Sonnet: 20,000 tokens. Estimate tokens as `len(text) / 4` (rough but fast, no tokenizer needed).

**Pruning algorithm:**
1. Always include: system prompt (including voice suffix), last 2 turns (current + previous)
2. Fill remaining budget from most-recent turns backward
3. When dropping old turns, drop user+assistant pairs together
4. Emit a `DEBUG` log when messages are pruned: `"voice context pruned: kept=%d dropped=%d estimated_tokens=%d"`

**Voice-specific budget in `VoiceSendMessageParams`:**

```go
type VoiceSendMessageParams struct {
    AgentID            string
    ConversationID     string
    Message            string
    Model              string
    SystemPromptSuffix string
    MaxContextTokens   int // NEW: 0 = use default; voice sets e.g. 16000
}
```

Add a constant in `pipeline.go`:

```go
const voiceMaxContextTokens = 16000
```

Set it in `commitVoiceTurn()` when calling `deps.SendMessage()`.

---

## Success Metrics

All metrics are measured using the e2e test harness with `turn.metrics` events from L1.3, plus manual testing against `test/voicee2e/scenarios/`.

### Primary KPIs

| Metric | Baseline | After P0 | After L1 | After L2 | How Measured |
|--------|----------|----------|----------|----------|--------------|
| E2E latency (speech end → first bot audio) p50 | ~1,500 ms | same | ≤ 1,000 ms | ≤ 700 ms | `turn.metrics.e2e_ms` |
| STT latency p50 | 400–800 ms | same | ≤ 150 ms | ≤ 100 ms | `turn.metrics.stt_ms` |
| LLM TTFB p50 | 300–600 ms | same | same | same | `turn.metrics.llm_ttfb_ms` |
| TTS first chunk latency p50 | 200–400 ms | same | same | ≤ 100 ms | `turn.metrics.tts_ms` |
| False speech starts per session | unmeasured | same | ≤ 1 | ≤ 0.5 | `speech_started` with no `turn_committed` |
| Spurious barge-ins per session | unmeasured | same | ≤ 0.5 | ≤ 0.2 | `barge_in_triggered` with no user intent |
| Premature turn commits (mid-sentence) | unmeasured | same | same | ≤ 0.3/session | Manual + EOT tests |
| Transcript accuracy (WER) | unmeasured | same | equal or better | equal or better | Deepgram vs Whisper on fixtures |

### P0 Correctness Counters (new, emitted via observer)

| Counter | Expected after P0 |
|---------|-------------------|
| Provider selection non-determinism events | 0 |
| `voice.input.commit` with zero-byte buffer events | > 0 (functional, emits `dropped_empty_audio`) |
| Sessions where feature flags diverge from observed behavior | 0 |
| `barge_in_candidate` events per session | visible in logs; ≥ 1 in noisy environments |
| `barge_in_suppressed` events per session | visible in logs |

### Secondary KPIs

| Metric | Target | How Measured |
|--------|--------|--------------|
| Session error rate | ≤ 0.5% | Streaming STT reconnect + TTS stream errors in logs |
| Memory growth over 10-min session | ≤ 50 MB | `go tool pprof` heap |
| Goroutine count per session delta | ≤ 8 (up from 4) | `runtime.NumGoroutine()` |
| STT fallback activation rate | < 5% | Log count of "falling back to batch STT" |
| Turn drop rate (all reasons) | < 10% of detected speech | Observer drop counter |
| Queue overflow rate | < 1% of turns | `dropped_queue_overflow` event count |

### Regression Guard

The e2e suite (`test/voicee2e/`) must pass `MinTranscriptSimilarity` and `MaxBargeStopMS` thresholds before and after every milestone. Use `--compare` to compare baseline vs. candidate JSON reports. The `RequireBargeIn` scenario must not regress in latency or reliability at any phase.

---

## Rollout Plan

### Phase 1 — Dark launch (P0 changes)

Ship P0.1–P0.3 with new config fields defaulting to their current behavior:
- `voice.transcriber_provider` unset → existing first-match fallback (unchanged behavior).
- `server_vad/server_turn` defaults remain `true` → existing auto-VAD path unchanged.
- `voice.turn_strategy` defaults to `"legacy"` → identical current behavior.

Enable in staging only. Validate that all existing e2e scenarios pass. No production behavior changes yet.

### Phase 2 — Level 1 GA

Enable streaming STT (Deepgram) in staging for all voice sessions. Collect `turn.metrics` data. Compare p50 `e2e_ms` and `stt_ms` against baseline report. Roll to production only when:
- p50 `e2e_ms` ≤ 1,000 ms confirmed in staging under real traffic.
- STT fallback rate < 5% (Deepgram WebSocket stable).
- No new `FAILED` e2e scenarios.

### Phase 3 — Level 2 canary

Enable `voice.turn_strategy = "balanced"` for a canary subset (suggested 10% of sessions). Compare against `"legacy"` control cohort using the `--compare` report tool:
- `barge_in_triggered` rate must decrease or hold.
- Premature commit rate must decrease.
- p50 `e2e_ms` must not increase.

Roll forward to 100% only after canary holds for 48 hours without regression.

### PR discipline

Each PR must include:
1. Changed behavior summary (one paragraph).
2. Before/after metrics from a smoke e2e run (`--compare` output).
3. An explicit rollback switch: config flag, strategy fallback, or feature flag default that reverts to previous behavior without a code deployment.

---

## Validation Plan

### Step 1 — Baseline measurement (before any changes)

```bash
go run ./test/voicee2e/cmd/voicee2e/main.go \
  -gateway-url http://localhost:8833 \
  -suite test/voicee2e/scenarios/suite.yaml \
  -out test/voicee2e/reports/baseline.json
```

After adding L1.3 (latency telemetry), re-run to capture `turn.metrics` in the baseline report. Store this as the reference.

### Step 2 — Unit tests per change

**Standard commands for all changes:**
```bash
go build ./...
go vet ./...
go test -race ./internal/voice/...
go test -race ./internal/providers/...
go test -race ./internal/api/v1api/...
```

The `-race` flag is mandatory for this codebase. `audioInputLoop`, `llmEventForwarder`, `ttsSynthLoop`, and `audioOutputLoop` share state through `stateMu` and atomic fields. Race conditions in this code cause audio corruption and deadlocks that are silent in normal test runs.

**P0.1 Deterministic provider routing:**
- Add `TestFindTranscriber_Deterministic`: register 3 providers, 2 of which implement `AudioTranscriber`; call `FindTranscriber()` 100 times and assert the same provider name is returned each time.
- Add `TestFindTranscriberByName_NotFound`: assert fallback behavior when named provider is absent.
- Add `TestFindTranscriberByName_WrongCapability`: assert fallback when named provider doesn't implement `AudioTranscriber`.

**P0.2 Feature flag enforcement:**
- Add `TestAudioInputLoop_ServerVADFalse`: assert no `speech_started` events emitted automatically.
- Add `TestAudioInputLoop_ServerTurnFalse`: assert turn is not committed on VAD silence; only on `InputCommit()`.

**P0.3 Functional input commit:**
- Add `TestInputCommit_TriggersTranscription`: assert full turn pipeline runs on commit.
- Add `TestInputCommit_EmptyBuffer`: assert `dropped_empty_audio` event.
- Add `TestInputCommit_TooShort`: assert `dropped_too_short_audio` event.

**L1.1 Silero VAD:**
- Add `TestSileroVAD_SpeechStart` and `TestSileroVAD_SpeechEnd` to `vad_test.go`
- Use the existing fixture WAV files from `test/voicee2e/fixtures/` decoded to raw PCM
- Assert that Silero fires `started` within 2 frames of actual speech onset in fixtures
- Assert that Silero fires `ended` within 600 ms of speech completion in fixtures
- Assert `EnergyVAD` still passes all existing `vad_test.go` tests (no regression)

**L1.2 Streaming STT (Deepgram):**
- Add `TestDeepgramClient_StreamTranscribe` with a mock WebSocket server
- Simulate the Deepgram protocol: accept connection, receive PCM, send mock `Results` JSON
- Assert `TranscribeStreamEvent{Type: "final", Text: "hello world", IsFinal: true}` is emitted
- Test error handling: close WebSocket mid-stream, assert `TranscribeStreamEvent{Err: ...}` emitted

**L1.3 Metrics:**
- Add `TestTurnMetrics_AllFieldsSet` using an in-process mock session
- Assert all timestamp fields are non-zero after a complete simulated turn
- Assert derived durations are computed correctly

**L2.1 Speculative LLM:**
- Add `TestSimilarity_EditDistance` for the transcript similarity function
- Add `TestSpeculative_PromotedOnMatch` and `TestSpeculative_CancelledOnDivergence` using mock `GatewayDeps`

**L2.2 TurnStrategy:**
- Add `TestLegacyStrategy_BargeIn`: assert TriggerDecision at score ≥ 0.06 (exact legacy threshold)
- Add `TestBalancedStrategy_BargeIn_Debounce`: assert CandidateDecision on first high-score frame, TriggerDecision only after debounce duration
- Add `TestBalancedStrategy_BargeIn_ScoreDrop`: assert IgnoreDecision after score drops — no barge-in triggered
- Add `TestBalancedStrategy_ShouldCommit_DanglingConjunction`: assert false at 300 ms, true at 700 ms
- Add `TestBalancedStrategy_ShouldCommit_SentenceTerminator`: assert true at 150 ms
- Add `TestBalancedStrategy_ShouldCommit_MaxSilence`: assert true at 700 ms regardless of text
- Add `TestBargeInCandidate_EventEmitted`: assert `barge_in_candidate` event in pipeline with mock strategy returning Candidate
- Add `TestBargeInSuppressed_EventEmitted`: assert `barge_in_suppressed` event when score drops after candidate

### Step 3 — Integration test per change

Each change should be validated with a fresh e2e run against a running server:

```bash
# After each improvement, run full suite and compare to baseline
go run ./test/voicee2e/cmd/voicee2e/main.go \
  -gateway-url http://localhost:8833 \
  -suite test/voicee2e/scenarios/suite.yaml \
  -out test/voicee2e/reports/after-l1-1.json

go run ./test/voicee2e/cmd/voicee2e/main.go \
  --compare \
  --prompt-a test/voicee2e/reports/baseline.json \
  --prompt-b test/voicee2e/reports/after-l1-1.json
```

**Acceptance criteria per e2e run:**
- All scenario `MinTranscriptSimilarity` assertions pass
- `RequireBargeIn` scenarios complete within `MaxBargeStopMS`
- `MaxResponseLatencyMS` passes (must not regress; should improve with L1)
- No new `FAILED` scenarios introduced

### Step 4 — Load / extended session test

After all L1 changes: run a single voice session for 10 minutes using looped fixtures (the e2e harness can loop a scenario). Monitor:
- No memory growth > 50 MB (check with `go tool pprof`)
- No goroutine leaks (count goroutines at start and end)
- Streaming STT WebSocket stays connected (check reconnect log count = 0)
- No `audioIn queue full` or `audioOut queue full` warnings in logs

---

## Implementation Notes for the Implementing Agent

These are judgement calls and context the code does not make obvious.

### Ordering matters — see "Implementation Ordering" section above

The full sequence is: P0.1 → P0.2 → P0.3 → L1.3 → L1.1 → L1.4 → L1.2 → L2.2 → L2.1 → L2.3 → L2.4.

P0 items are correctness fixes, not performance improvements. Do not skip them to get to the latency work faster — P0.1 (nondeterministic provider selection) will cause intermittent failures when you register Deepgram alongside OpenAI in L1.2.

### Critical invariants to preserve

1. **`bargeInOnce.Do()`** — Never call `triggerBargeIn()` outside this guard. Multiple goroutines can race to trigger barge-in (VAD goroutine + transcription goroutine) and the `sync.Once` is the only thing preventing double-cancellation. When adding speculative run cancellation (L2.1), use a separate `speculativeCancelOnce` or just check `speculativeRunId != ""` under the speculative mutex.

2. **Channel non-blocking** — `enqueueAudioOut()` and `enqueueAudioIn()` are intentionally non-blocking (they return false on full). Do not change them to blocking sends. A blocked send in the audio path will cause the WebSocket handler goroutine to hang, breaking the entire connection. Use select with `default:`.

3. **`transcribeInFlight` map** — The `TryStartTurnTranscription()` / `FinishTurnTranscription()` pair prevents duplicate transcriptions. When switching to streaming STT (L1.2), you no longer spawn a goroutine per utterance, but you still need to call `TryStartTurnTranscription(turnId)` when the streaming transcriber receives a final event and `FinishTurnTranscription(turnId)` afterward. The `ttsSynthLoop` checks `HasTranscriptionInFlight()` before starting a response — this check must remain valid.

4. **`conversationEventSubscriber` drop policy** — In `pipeline.go` lines 500–523: non-critical delta events are dropped (`default:`) while terminal events (final/error/aborted) make room by evicting a delta. Do not change this. If you make the event buffer larger, you risk stalling the llmEventForwarder on barge-in because the channel fills with stale deltas.

5. **`TurnID` vs `ResponseID`** — These are different concepts. A `TurnID` is created on VAD speech start and identifies the user's utterance. A `ResponseID` is created when the first TTS sentence starts and identifies the bot's reply. They are not correlated 1:1 (a turn can be dropped, a response can be interrupted). Do not conflate them.

6. **Context cancellation on barge-in** — TTS cancellation in `triggerBargeIn()` works via `SwapTTSCancel(nil)` returning the previous cancel function. For streaming TTS (L2.3), the context passed to `SynthesizePCMStream` is this same cancellable context — make sure the streaming provider's goroutine exits when the context is cancelled. Test this explicitly: trigger a barge-in while a streaming TTS chunk is mid-flight and assert no goroutine leak.

7. **Provider registry is not thread-safe for writes** — `FindTranscriber()` iterates the map without a lock (safe because the registry is built once at startup and never mutated after). If you add dynamic provider registration, add a `sync.RWMutex` to `Registry`.

### On adding the Silero ONNX dependency

The current `go.mod` has no ML or CGO dependencies. Adding `github.com/yalue/onnxruntime_go` brings in CGO and requires the ONNX Runtime shared library (`libonnxruntime.so` / `.dylib` / `.dll`). This has implications:

- **Docker build**: The `Dockerfile` must install `libonnxruntime` (or copy the `.so` from a build stage). Check the official ONNX Runtime releases for a static linking option.
- **CI**: GitHub Actions or whatever CI system is used needs the runtime library. Add it to the CI setup step.
- **Cross-compilation**: CGO breaks `GOOS=linux GOARCH=amd64` cross-compilation on macOS unless you use Docker or a cross-compile toolchain. Evaluate whether this is acceptable before committing to this dependency.

**Alternative if CGO is unacceptable:** Run Silero VAD as a tiny Python sidecar (Flask or FastAPI) on the same host, and call it via HTTP. One endpoint: `POST /vad { "pcm_base64": "...", "sample_rate": 16000 }` → `{ "probability": 0.92 }`. This adds ~1 ms latency per frame but keeps the Go binary CGO-free. The `VADAnalyzer` interface makes this swap transparent.

### On Deepgram streaming cost model

Deepgram charges per minute of audio streamed, not per transcription. A persistent WebSocket connection for a 10-minute call costs the same whether the user spoke 2 minutes or 8 minutes. Budget accordingly. You may want to close and reopen the stream after a configurable idle period (e.g. 30 seconds of silence) to avoid paying for idle time. The `audioInputLoop` can detect sustained VAD-silence and close/reopen the stream.

### On ElevenLabs vs Cartesia for streaming TTS

Both providers support WebSocket streaming. The differences relevant to this codebase:

| | ElevenLabs | Cartesia |
|---|---|---|
| Latency | ~100 ms TTFB | ~50 ms TTFB |
| Quality | Very high | High |
| Voice cloning | Yes | Yes |
| Go SDK | None (use raw WS) | None (use raw WS) |
| Output format | PCM/MP3/WAV | PCM |
| Min per-session cost | None | None |
| Rate limit | Per plan | Per plan |

Recommendation: Implement ElevenLabs first (more documentation, larger community). The `StreamingAudioSynthesizer` interface makes swapping to Cartesia later a single-file change.

### On test fixture coverage gaps

The current fixture set in `test/voicee2e/fixtures/` focuses on clear speech in quiet environments. Before claiming L1.1 (Silero VAD) is an improvement, add fixtures for:
- Background noise (recorded with AC hum, keyboard, traffic)
- Echo scenario (simulated playback leakage)
- Quiet/whispered speech
- Non-native accented English

These can be synthetic or recorded. The e2e harness already supports arbitrary WAV files — just add them to the fixtures directory and reference in scenario YAML.

### On the e2e harness `turn.metrics` extension

When extending the harness to capture `turn.metrics`, store the metrics in the timeline alongside other events. The `ScenarioResult` struct (in `test/voicee2e/internal/model/model.go`) should gain a `TurnMetrics []TurnMetricsSnapshot` field. The comparison report should include latency percentiles (p50, p90, p99) for each metric, not just raw values.

---

## Implementation Ordering

Execute strictly in this sequence. Each item is a shippable PR:

```
P0.1  Deterministic provider routing          ← correctness bug; zero risk
P0.2  Feature flag enforcement                ← correctness bug; unblocks P0.3
P0.3  Functional voice.input.commit           ← correctness bug; standalone
L1.3  Latency telemetry (observer pattern)    ← measurement first; all later
                                                items depend on having data
L1.1  Silero VAD                              ← high impact, self-contained
L1.4  Client audio gate                       ← 1-line change; ship with L1.1
L1.2  Deepgram streaming STT                  ← biggest latency win
L2.2  TurnStrategy + BalancedStrategy         ← do before speculative LLM so
                                                interim transcripts are stable
L2.1  Speculative LLM on interim transcripts  ← requires L1.2 + L2.2 stable
L2.3  Streaming TTS                           ← independent; can parallelize
L2.4  Context window management               ← last; touches gateway layer
```

---

## Definition of Done

### Phase 0 is done when:

1. `FindTranscriber()` returns the same provider across 100 calls with multiple providers registered (verified by unit test).
2. `ServerVAD=false` session emits no automatic `speech_started` events (verified by unit test + e2e with explicit-turn scenario).
3. `voice.input.commit` with 500 ms of buffered audio produces a full turn: `transcript.final` → `response.started` → `response.completed` (verified by e2e).
4. `go test -race ./internal/voice/...` passes with zero race reports.

### Level 1 is done when:

1. p50 `e2e_ms` ≤ 1,000 ms measured in e2e report (baseline was ~1,500 ms).
2. p50 `stt_ms` ≤ 150 ms with Deepgram streaming enabled.
3. False speech start rate ≤ 1 per session with Silero VAD on standard fixture set.
4. All existing e2e scenarios pass `MinTranscriptSimilarity` and `MaxBargeStopMS`.
5. `turn.metrics` events appear in every e2e run and are captured in the report.
6. Streaming STT fallback rate < 5% in a 1-hour soak run.

### Level 2 is done when:

1. p50 `e2e_ms` ≤ 700 ms with speculative LLM + streaming TTS active.
2. `BalancedStrategy` measurably reduces `barge_in_triggered` rate vs `LegacyStrategy` in canary without increasing missed interruptions.
3. Premature commit rate ≤ 0.3 per session (measured via manual review of `turn_committed` events that lack a complete sentence in `transcript.final`).
4. Strategy switch (`voice.turn_strategy = legacy`) reproduces exact baseline behavior (verified by comparing e2e `--compare` output with original baseline report).
5. `go test -race ./internal/voice/...` passes with zero race reports.

---

## Out of Scope for This Plan

The following were evaluated but excluded from L1/L2:

- **WebRTC transport**: Requires a STUN/TURN server, ICE negotiation, and client SDK changes. High complexity, medium latency benefit. Revisit at L3.
- **OpenAI Realtime API**: Eliminates STT+LLM+TTS pipeline entirely but removes provider choice and adds significant cost and lock-in. Revisit as a parallel track if vendor commitment is acceptable.
- **Frame-based pipeline architecture (Pipecat-style)**: Major architectural refactor estimated at 4–6 weeks. High extensibility value long-term. Revisit at L3 if L1/L2 validate the pipeline direction.
- **Parallel TTS branches**: Premature optimisation. Address after streaming TTS (L2.3) proves its value.
