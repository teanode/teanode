# Voice Pipeline — Execution Backlog for Parallel Subagents

**Source plan:** `docs/engineering_plan_pipeline.md`
**Module:** `github.com/teanode/teanode`
**Branch prefix:** `codex/<task-id>-<short-name>`

---

## Section 1 — Task Graph

Tasks are organised into waves. All tasks within a wave may run in parallel. A wave does not begin until every task in the previous wave is merged to `pipeline`.

> **Ordering authority:** The strict sequence in `docs/engineering_plan_pipeline.md` §Implementation Ordering takes precedence over the companion plan. L2 sequence is: L2.2 → L2.1 → L2.3 → L2.4. L1.4 ships in the same wave as L1.1 (plan: "ship with L1.1").

```
Wave 0  [research, no file changes — run immediately; block L1.1 and L2.4 only]
  S1  Silero CGO/ONNX vs HTTP-sidecar spike (2-day timebox)
  S2  Context-window gateway.go location spike (1-day timebox)

Wave 1  [P0 correctness — parallel groups, sequential within group B]
  Agent A:  P0.1  Deterministic provider routing
  Agent B:  P0.2 → P0.3  Feature-flag enforcement then functional input.commit
            (sequential within one agent; both touch session.go + pipeline.go)

Wave 2  [observer telemetry — measurement must exist before VAD/STT improvements]
  Agent C:  L1.3  Observer/metrics pattern

Wave 3  [VAD + client gate together; plan explicitly says "ship with L1.1"]
  Agent E:  L1.1  Silero VAD (requires S1 decision)
  Agent D:  L1.4  Client audio gate (TypeScript only; zero Go file overlap with L1.1)

Wave 4  [streaming STT; rebases on Wave 3]
  Agent F:  L1.2  Deepgram streaming STT

Wave 5  [L2 sequence begins: TurnStrategy first per plan]
  Agent G:  L2.2  TurnStrategy abstraction + BalancedStrategy

Wave 6  [speculative LLM; requires L1.2 + L2.2]
  Agent I:  L2.1  Speculative LLM on interim transcripts

Wave 7  [streaming TTS; follows L2.1 per plan sequence L2.2→L2.1→L2.3→L2.4]
  Agent H:  L2.3  Streaming TTS + ElevenLabs client

Wave 8  [context window; requires S2 + L2.3 merged]
  Agent J:  L2.4  Conversation context window management

Wave 9  [documentation + runbook; plan step 8; can overlap with Wave 8 reviews]
  Agent K:  DOCS  Documentation and runbook update
```

### Dependency graph (edge = "must merge before")

```
S1 ──────────────────────────────────────────────────────► L1.1
S2 ──────────────────────────────────────────────────────────────────► L2.4
P0.1 ──────────────────────────────────────────────────── (unblocks L1.2 registry)
P0.2 ──► P0.3 ──────────────────► L1.3
L1.3 ──► L1.1 ──► L1.2 ──► L2.2 ──► L2.1 ──► L2.3 ──► L2.4
         L1.4 ─────────────────── (no downstream deps; ships in same wave as L1.1)
                    L1.2 ────────────────────► L2.1
```

---

## Section 2 — Task Specifications

---

### S1 — Silero CGO/ONNX Feasibility Spike

| Field | Value |
|-------|-------|
| **Branch** | `codex/s1-silero-spike` |
| **Timebox** | 2 days |
| **Output** | Decision doc in `docs/spike-s1-silero.md` |
| **Risk** | Low (research only) |
| **Merge conflict risk** | None (no source changes) |

**Objective:** Determine whether `github.com/yalue/onnxruntime_go` (CGO) is acceptable for this repo, or whether a Python HTTP sidecar is the better path for L1.1.

**Scope:**
- Inspect `go.mod` and `Dockerfile` for existing CGO usage
- Prototype `onnxruntime_go` import in a throwaway `cmd/` binary; confirm it compiles on macOS and Linux
- Document cross-compile impact: does `GOOS=linux GOARCH=amd64` still build from macOS without Docker?
- Benchmark: what is the p99 inference latency per 32 ms frame on the target host?

**Out of scope:** Actual SileroVAD implementation (that is L1.1).

**Decision criteria:**

| Criterion | Go ONNX | HTTP sidecar |
|-----------|---------|--------------|
| CGO acceptable in this repo? | Yes → proceed | No → use sidecar |
| p99 inference latency per frame | ≤ 2 ms → Go ONNX | > 5 ms → sidecar |
| Docker build complexity | Acceptable → Go ONNX | Unacceptable → sidecar |

**Output file contents:**
```markdown
# Spike S1: Silero VAD dependency decision
Decision: [go-onnx | http-sidecar]
Rationale: ...
Latency measurement: ...
Cross-compile notes: ...
Recommended model path: internal/voice/assets/silero_vad_v5.onnx  (or sidecar URL)
```

**Tests:** None (research spike).

**Acceptance criteria:** Decision doc committed to `docs/spike-s1-silero.md` with a clear binary recommendation and the reasoning.

---

### S2 — Context Window Gateway Location Spike

| Field | Value |
|-------|-------|
| **Branch** | `codex/s2-context-window-spike` |
| **Timebox** | 1 day |
| **Output** | Decision doc in `docs/spike-s2-context-window.md` |
| **Risk** | Low (research only) |
| **Merge conflict risk** | None (no source changes) |

**Objective:** Find the exact location in `internal/gw/gateway.go` (or adjacent agent/runner files) where conversation history is assembled into the LLM prompt before each `SendMessage` call, so that L2.4 can be implemented precisely.

**Scope:**
- Trace `SendMessage` call path from `internal/voice/pipeline.go:commitVoiceTurn()` through `internal/gw/gateway.go` and into the LLM runner
- Identify the struct/function that builds the `messages []` array
- Confirm whether token counting is already present or must be added
- Identify any existing context-trim mechanisms

**Output file contents:**
```markdown
# Spike S2: Context window implementation location
File: internal/gw/...
Function: ...
Line range: ...
Existing token counting: [yes/no]
Recommended insertion point for MaxContextTokens check: ...
```

**Tests:** None (research spike).

**Acceptance criteria:** Doc committed to `docs/spike-s2-context-window.md` with file + function + line range pinned precisely.

---

### P0.1 — Deterministic Provider Routing

| Field | Value |
|-------|-------|
| **Branch** | `codex/p0-1-deterministic-providers` |
| **Base** | `pipeline` |
| **Risk** | Low |
| **Merge conflict risk** | Low — only `internal/providers/registry.go` and configs |

**Objective:** Fix the nondeterministic `FindTranscriber()` / `FindSynthesizer()` map iteration bug. Add named lookup. Add config fields for preferred provider.

**Scope (files to change):**
- `internal/providers/registry.go`
- `internal/configs/config.go`
- `internal/configs/schema.json` (if it exists)

**Out of scope:** Any change to `pipeline.go`, `session.go`, or gateway adapters beyond what is needed to call `FindTranscriberByName`.

**Implementation steps:**

1. Add to the `Registry` struct:
   ```go
   transcriberOrder []string  // append-only at registration time
   synthesizerOrder []string
   ```

2. In the provider registration method, when a provider implements `AudioTranscriber`, append its name to `transcriberOrder`; same for `AudioSynthesizer` → `synthesizerOrder`.

3. Update `FindTranscriber()` to iterate `transcriberOrder` slice (not the map):
   ```go
   func (r *Registry) FindTranscriber() (AudioTranscriber, string, bool) {
       for _, name := range r.transcriberOrder {
           if t, ok := r.clients[name].(AudioTranscriber); ok {
               return t, name, true
           }
       }
       return nil, "", false
   }
   ```

4. Add new methods:
   ```go
   func (r *Registry) FindTranscriberByName(name string) (AudioTranscriber, bool)
   func (r *Registry) FindSynthesizerByName(name string) (AudioSynthesizer, bool)
   ```
   Each does: look up by name in `r.clients`, assert the capability, return (nil, false) if absent or incapable.

5. Add config fields (check existing pattern in `config.go`):
   ```
   voice.transcriber_provider  string  // optional
   voice.synth_provider        string  // optional
   ```

6. In the gateway voice adapter (wherever `ProviderRegistry().FindTranscriber()` is called): if `voice.transcriber_provider` is set, call `FindTranscriberByName(name)` first; if that returns false, fall back to `FindTranscriber()` and emit a `WARNING` log.

**Tests (add to `internal/providers/registry_test.go`):**
- `TestFindTranscriber_Deterministic`: register 3 providers (2 implement `AudioTranscriber`), call `FindTranscriber()` 100 times, assert same name every time.
- `TestFindTranscriberByName_Found`: happy path.
- `TestFindTranscriberByName_NotFound`: unknown name → `(nil, false)`, no panic.
- `TestFindTranscriberByName_WrongCapability`: provider exists but does not implement `AudioTranscriber` → `(nil, false)`.

**Acceptance criteria:**
- Same provider returned across 100 calls (test enforces this).
- Named provider missing → fallback + WARNING log.
- `go test -race ./internal/providers/...` passes.
- `go build ./...` passes.

---

### P0.2 — Feature Flag Runtime Enforcement

| Field | Value |
|-------|-------|
| **Branch** | `codex/p0-2-feature-flags` |
| **Base** | `pipeline` |
| **Risk** | Medium (touches core audio loop) |
| **Merge conflict risk** | High — `pipeline.go` and `session.go` are hot files |

**Objective:** Make `ServerVAD`, `ServerTurn`, and `ServerDenoise` flags actually control runtime behaviour in `audioInputLoop`. Currently they are stored but never branched on.

**Scope (files to change):**
- `internal/voice/pipeline.go` — `audioInputLoop` branching
- `internal/voice/session.go` — `explicitAudioBuf` field (prep for P0.3; add but leave empty)
- `internal/voice/gateway.go` — `AudioDenoiser` interface (add stub only)

**Out of scope:** Actual `AudioDenoiser` implementation. Do not touch `providers/` in this PR.

**Implementation steps:**

1. In `audioInputLoop`, at the top of the frame-processing loop add a branch:
   ```go
   if !self.Features.ServerVAD {
       // no-VAD mode: accumulate into explicitAudioBuf (for P0.3)
       // do not call vad.ProcessFrame()
       // speech_started / speech_ended events are NOT emitted
       self.accumulateExplicitAudio(frame)
       continue
   }
   ```

2. In the VAD `ended=true` handler, add a branch:
   ```go
   if ended {
       if !self.Features.ServerTurn {
           // set speechReady flag; do NOT call commitVoiceTurn
           self.setSpeechReady(true)
           continue
       }
       // existing auto-commit logic unchanged
       self.commitVoiceTurn(...)
   }
   ```

3. Add to `Session` struct:
   ```go
   explicitAudioBuf []byte   // guarded by stateMu; used by P0.2 + P0.3
   speechReady      bool     // set by VAD when ServerTurn=false
   ```

4. Add to `gateway.go`:
   ```go
   type AudioDenoiser interface {
       Denoise(pcm []byte) []byte
   }
   ```

5. At session start, if `Features.ServerDenoise == true`, log once:
   `"server_denoise requested but not implemented; ignoring"`

**Tests (add to `internal/voice/pipeline_test.go` or `session_test.go`):**
- `TestAudioInputLoop_ServerVADFalse`: feed 2 seconds of high-RMS audio; assert no `speech_started` or `speech_ended` events emitted automatically.
- `TestAudioInputLoop_ServerTurnFalse`: feed audio through VAD until `ended`; assert no `turn_committed` event; assert `speechReady` flag set.
- `TestServerDenoise_LoggedOnce`: start session with `ServerDenoise=true`; assert exactly one log line contains `"server_denoise requested but not implemented"`.

**Acceptance criteria:**
- `ServerVAD=false` → no automatic speech events.
- `ServerTurn=false` → no automatic turn commit on VAD silence.
- Default flags (`true/true/false`) reproduce existing behaviour (all existing tests pass).
  > **Note on companion plan discrepancy:** `ENGINEERING_PLAN_VOICE_PIPELINE_LEVEL1_LEVEL2.md` §A2 says the backward-compatibility path is `true/true/true`. The primary plan (`engineering_plan_pipeline.md` line 119) says `true/true/false`. The primary plan is authoritative: `ServerDenoise` defaults to `false` because no implementation exists; logging the "not implemented" warning when `ServerDenoise=true` is the correct behavior, not silently treating it as `true`.
- `go test -race ./internal/voice/...` passes.

---

### P0.3 — Functional `voice.input.commit`

| Field | Value |
|-------|-------|
| **Branch** | `codex/p0-3-functional-commit` |
| **Base** | `codex/p0-2-feature-flags` (merge P0.2 first, then branch from it) |
| **Risk** | Medium |
| **Merge conflict risk** | Medium — same files as P0.2, but built on top |

**Objective:** Wire `InputCommit()` to the real transcription pipeline. Currently it emits a `turn_committed` event and does nothing else.

**Scope (files to change):**
- `internal/voice/session.go` — `InputCommit()` rewrite
- `internal/voice/pipeline.go` — `audioInputLoop` frame accumulation when not auto-committing
- `internal/api/v1api/rpc_voice.go` — pass `reason` from RPC payload

**Out of scope:** Changes to the RPC wire format beyond adding `reason` field.

**Implementation steps:**

1. In `audioInputLoop`, when `ServerVAD=false` OR `ServerTurn=false`, append each inbound frame to `session.explicitAudioBuf` (added in P0.2).

2. Rewrite `InputCommit()` in `session.go`:
   ```go
   func (self *Session) InputCommit(reason string) {
       self.stateMu.Lock()
       captured := append([]byte(nil), self.explicitAudioBuf...)
       self.explicitAudioBuf = self.explicitAudioBuf[:0]
       self.speechReady = false
       self.stateMu.Unlock()

       if len(captured) == 0 {
           self.sendVoiceEvent("turn.event", map[string]any{
               "event": "turn_dropped", "reason": "dropped_empty_audio",
           })
           return
       }
       if len(captured) < minCommittedTurnBytes {
           self.sendVoiceEvent("turn.event", map[string]any{
               "event": "turn_dropped", "reason": "dropped_too_short_audio",
           })
           return
       }
       turnId := self.newTurnId()
       self.startNewTurn(turnId)
       self.sendVoiceEvent("turn.event", map[string]any{
           "turn_id": turnId, "event": "input_committed", "reason": reason,
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

3. In `rpc_voice.go`, parse `reason` from the `voice.input.commit` RPC payload and pass it to `session.InputCommit(reason)`.

**Tests:**
- `TestInputCommit_FullPipeline`: use mock `GatewayDeps`; buffer 500 ms of audio in `ServerVAD=false` mode; call `InputCommit("push_to_talk")`; assert `input_committed` event then `SendMessage` called.
- `TestInputCommit_EmptyBuffer`: call `InputCommit` with no buffered audio; assert `dropped_empty_audio` event; assert `SendMessage` NOT called.
- `TestInputCommit_TooShort`: buffer 100 ms (below `minCommittedTurnBytes`); assert `dropped_too_short_audio`.
- `TestInputCommit_RaceCondition`: call `InputCommit` from two goroutines simultaneously; assert no data race (`-race` flag).

**Acceptance criteria:**
- `voice.input.commit` in `ServerVAD=false` mode produces: `input_committed` → `transcript.final` → `response.started` → `response.completed`.
- Empty/short commits return clear reason codes without panicking.
- `go test -race ./internal/voice/...` passes.

---

### L1.3 — Latency Telemetry via Observer Pattern

| Field | Value |
|-------|-------|
| **Branch** | `codex/l1-3-observer-telemetry` |
| **Base** | `pipeline` (after P0.2 and P0.3 are merged) |
| **Risk** | Low (additive; observers are no-ops when list is empty) |
| **Merge conflict risk** | Medium — adds hooks throughout `pipeline.go` and `session.go` |

**Objective:** Add `TurnObserver`, `LatencyObserver`, `IdleObserver` interfaces plus a `MetricsObserver` default implementation that emits `turn.metrics` events. Extend the e2e test harness to capture and report these events.

**Scope (files to change/create):**
- `internal/voice/observer.go` — NEW: interface definitions
- `internal/voice/metrics.go` — NEW: `TurnMetrics` struct + `MetricsObserver`
- `internal/voice/session.go` — add `observers []TurnObserver` field + `notifyObservers` helper
- `internal/voice/pipeline.go` — add observer notification calls at each lifecycle hook (see table below)
- `test/voicee2e/internal/protocol/client.go` — handle `turn.metrics` event
- `test/voicee2e/internal/model/model.go` (or equivalent) — add `TurnMetrics` field to `ScenarioResult`
- `test/voicee2e/internal/report/report.go` — emit p50/p90/p99 for e2e/stt/llm-ttfb/tts latencies

**Out of scope:** Any modification of the core pipeline logic. Observer calls must be fire-and-forget. Do not add locks around observer calls (observers are responsible for their own thread safety).

**Implementation steps:**

1. Create `internal/voice/observer.go` with `TurnObserver`, `LatencyObserver`, `IdleObserver` interfaces exactly as specified in the engineering plan Section L1.3.

2. Create `internal/voice/metrics.go` with `TurnMetrics` struct and `MetricsObserver`. `MetricsObserver` collects timestamps per turn, computes derived durations at `OnResponseComplete`, and calls `emitFn("turn.metrics", ...)`. Use a `sync.Mutex` (not `stateMu`) for the per-turn timestamp map.

3. Add to `Session` struct in `session.go`:
   ```go
   observers []TurnObserver  // set in NewSession; immutable after Start()
   ```

4. Add to `session.go`:
   ```go
   func (self *Session) notifyObservers(fn func(TurnObserver)) {
       for _, obs := range self.observers {
           fn(obs)
       }
   }
   ```

5. In `NewSession()`, instantiate `MetricsObserver` and append to `observers`.

6. Add observer notification calls to `pipeline.go` at these exact locations:

   | Observer method | Goroutine | When |
   |----------------|-----------|------|
   | `OnSpeechStarted(turnId, now, score)` | `audioInputLoop` | after `if started {` |
   | `OnSpeechEnded(turnId, now)` | `audioInputLoop` | after `if ended {` |
   | `OnTranscribeStart(turnId, now)` | `transcribeAndSend` goroutine | function entry |
   | `OnTranscribeEnd(turnId, now, text)` | `transcribeAndSend` goroutine | after text validated |
   | `OnLLMStart(turnId, now, runId)` | `commitVoiceTurn` | before `deps.SendMessage()` |
   | `OnLLMFirstToken(turnId, now)` | `llmEventForwarder` | first non-empty `delta` |
   | `OnTTSStart(turnId, now)` | `ttsSynthLoop` | before first `SynthesizePCM` |
   | `OnFirstAudioSent(turnId, now)` | `ttsSynthLoop` | after first `enqueueAudioOut` |
   | `OnResponseComplete(turnId, now)` | `ttsSynthLoop` | on empty-string sentinel |
   | `OnTurnDropped(turnId, reason)` | multiple | on any drop path |
   | `OnBargeIn(turnId, now)` | `triggerBargeIn` | inside `bargeInOnce.Do` |

7. Update e2e harness to capture `turn.metrics` events and compute percentiles in reports.

**Tests:**
- `TestMetricsObserver_AllFieldsSet`: simulate a complete turn via a mock session; assert all `TurnMetrics` fields are non-zero and derived durations are correct.
- `TestNotifyObservers_EmptyList`: call `notifyObservers` with empty list; assert no panic.
- `TestMetricsObserver_ThreadSafe`: call all observer methods from 4 goroutines concurrently with `-race`; assert no data race.

**Acceptance criteria:**
- `turn.metrics` events appear in e2e runs with all timestamp fields populated.
- Observer methods are never called with a lock held (verify by code review).
- Core pipeline behaviour unchanged when `observers` is nil.
- `go test -race ./internal/voice/...` passes.

---

### L1.4 — Client Audio Gate Refinement

| Field | Value |
|-------|-------|
| **Branch** | `codex/l1-4-client-audio-gate` |
| **Base** | `pipeline` |
| **Risk** | Low |
| **Merge conflict risk** | None — TypeScript only, no overlap with any Go task |

**Objective:** Remove or substantially reduce the client-side amplitude gate when `server_vad` is negotiated, so soft speech and word-initial consonants are not clipped before reaching the server VAD.

**Scope (files to change):**
- `web/src/hooks/useVoiceSession.ts` (or wherever the `ScriptProcessor` callback lives; search for `rms > 0.03` or `maxAbs > 0.12`)

**Out of scope:** Any Go backend change. Any change to the WebSocket framing.

**Implementation steps:**

1. Locate the amplitude gate in the TypeScript audio callback.

2. **Option A (preferred):** When `features.server_vad` is `true` (as negotiated in the `voice.start` response), skip the gate entirely — send every frame to the server.

3. **Option B (fallback if A is too complex):** Lower thresholds to silence-floor only:
   - RMS threshold: `0.003` (was `0.03`)
   - Remove `maxAbs` check entirely
   - Hangover: `100 ms` (was `350 ms`)

4. Add a comment:
   ```typescript
   // This gate is a bandwidth guard only (prevents sending true silence).
   // Voice activity detection is performed server-side.
   // Do not raise these thresholds — it will clip soft speech onset.
   ```

**Tests:** Manual browser test: whisper "hello" into microphone; assert the server emits `speech_started`.

**Acceptance criteria:**
- Soft speech (< 0.12 maxAbs) reaches the server when `server_vad` is enabled.
- No regression in bandwidth usage for true silence (gate still applies below `0.003` RMS).
- No TypeScript compile errors (`npm run build` or equivalent passes).

---

### L1.1 — ML-Based VAD with Silero ONNX

| Field | Value |
|-------|-------|
| **Branch** | `codex/l1-1-silero-vad` |
| **Base** | `pipeline` (after L1.3 merged; after S1 decision) |
| **Risk** | Medium (new dependency; S1 determines approach) |
| **Merge conflict risk** | Low — `vad.go` rename is self-contained; `pipeline.go` change is one line |

**Objective:** Introduce `VADAnalyzer` interface; rename `VADState` → `EnergyVAD`; implement `SileroVAD` (ONNX or HTTP sidecar per S1 decision); wire fallback in `audioInputLoop`.

**Scope (files to change/create):**
- `internal/voice/vad.go` — add `VADAnalyzer` interface; rename `VADState` → `EnergyVAD`
- `internal/voice/vad_silero.go` — NEW: `SileroVAD` implementation
- `internal/voice/assets.go` — NEW: `//go:embed` for model file (if ONNX path chosen)
- `internal/voice/assets/silero_vad_v5.onnx` — model file (download in CI; do not commit binary if >5 MB)
- `internal/voice/pipeline.go` — change `&VADState{}` to `VADAnalyzer` construction (one block)
- `internal/voice/session.go` — add `SileroVAD bool` to `Features` struct
- `go.mod` / `go.sum` — add ONNX runtime dep if ONNX path chosen

**Out of scope:** Any change to the VAD thresholds of `EnergyVAD`. All existing `EnergyVAD` behaviour must be identical after the rename.

**Implementation steps:**

1. In `vad.go`, add the `VADAnalyzer` interface (exact signature from engineering plan §L1.1). Rename `VADState` → `EnergyVAD` (all occurrences in `vad.go` only; `pipeline.go` reference is updated in step 3).

2. Implement `vad_silero.go` per the engineering plan's spec:
   - If ONNX: use `onnxruntime_go`; model input `[1,512]` float32; persist LSTM state (`h`, `c`) per session; re-chunk 20 ms frames to 32 ms windows with a carry buffer.
   - If HTTP sidecar: POST `{"pcm_b64": "...", "sr": 16000}` → `{"prob": 0.92}`; convert to `started/ended` using same threshold logic as `EnergyVAD` but with the ML probability.

3. In `pipeline.go`, `audioInputLoop()`, replace:
   ```go
   vad := &VADState{}
   ```
   with:
   ```go
   var vad VADAnalyzer = &EnergyVAD{}
   if self.Features.SileroVAD {
       if sv, err := NewSileroVAD(sileroModelPath()); err == nil {
           vad = sv
       } else {
           pipelineLog.Warningf("silero vad init failed, using energy vad: %v", err)
       }
   }
   ```

4. Add `SileroVAD bool` to `Features` in `session.go`.

5. If ONNX: add `go.sum` / `go.mod` entry; update `Dockerfile` with `libonnxruntime` install step.

**Tests (in `internal/voice/vad_test.go`):**
- `TestEnergyVAD_Implements_VADAnalyzer`: `var _ VADAnalyzer = &EnergyVAD{}` compile check.
- `TestEnergyVAD_ExistingBehaviour`: all existing `vad_test.go` tests pass unchanged.
- `TestSileroVAD_SpeechStart`: feed WAV fixture decoded to raw PCM; assert `started` fires within 3 frames of actual speech onset.
- `TestSileroVAD_SpeechEnd`: assert `ended` fires within 600 ms of speech completion.
- `TestSileroVAD_FallbackOnInitError`: provide invalid model path; assert `audioInputLoop` falls back to `EnergyVAD` without panic.

**Acceptance criteria:**
- `var _ VADAnalyzer = &EnergyVAD{}` compiles.
- All existing VAD tests pass.
- Silero fires `started` within 3 frames on clean speech fixture.
- `go test -race ./internal/voice/...` passes.
- `go build ./...` passes (including CGO dep if applicable).

---

### L1.2 — Streaming STT via Deepgram

| Field | Value |
|-------|-------|
| **Branch** | `codex/l1-2-deepgram-streaming-stt` |
| **Base** | `pipeline` (after L1.1 + L1.3 merged) |
| **Risk** | High (new external WebSocket connection per session; biggest architectural change in L1) |
| **Merge conflict risk** | High — `pipeline.go` gets a new goroutine; `providers/interface.go` gets new interfaces |

**Objective:** Add `StreamingTranscriber` interface; implement `DeepgramClient`; restructure `audioInputLoop` to forward frames to the streaming transcriber in parallel with VAD; add `streamingTranscribeLoop` goroutine.

**Scope (files to change/create):**
- `internal/providers/interface.go` — add `StreamingTranscriber`, `TranscribeStream`, `TranscribeStreamEvent`, `StreamTranscribeRequest`
- `internal/providers/deepgram.go` — NEW: `DeepgramClient` with `StreamingTranscriber` impl
- `internal/providers/registry.go` — add `FindStreamingTranscriber()` method
- `internal/voice/gateway.go` — add `VoiceStreamingTranscriber`, `VoiceTranscribeStream`, `VoiceTranscribeEvent`; extend `VoiceProviderRegistry`
- `internal/voice/session.go` — add `streamingSTTStream` and `interimText` fields
- `internal/voice/pipeline.go` — restructure `audioInputLoop`; add `streamingTranscribeLoop`

**Out of scope:** Changes to `DeepgramClient` for anything other than streaming STT. No change to existing `AudioTranscriber` interface.

**Implementation steps:**

1. Add the four new types to `providers/interface.go` (exact signatures from engineering plan §L1.2).

2. Add `FindStreamingTranscriber() (StreamingTranscriber, string, bool)` to `registry.go`. **Important:** iterate `transcriberOrder` (from P0.1 fix) not the map.

3. Add the three new voice types to `voice/gateway.go`. Add `FindStreamingTranscriber()` to `VoiceProviderRegistry`.

4. Implement `DeepgramClient` in `providers/deepgram.go`:
   - `OpenTranscribeStream` opens `wss://api.deepgram.com/v1/listen?model=nova-2&encoding=linear16&sample_rate=16000&channels=1&interim_results=true&endpointing=false`
   - Auth header: `Authorization: Token <api_key>`
   - Receiver goroutine: parse JSON responses; emit `TranscribeStreamEvent` to a buffered channel (cap 32)
   - Keep-alive: send `{"type":"KeepAlive"}` every 8 seconds via a ticker goroutine
   - Close: send `{"type":"CloseStream"}` then close WebSocket

5. In `session.go`, add:
   ```go
   streamingSTTStream  VoiceTranscribeStream  // nil if streaming not used
   interimText         string                 // guarded by stateMu
   ```

6. In `pipeline.go`:
   - At `Start()`, if `VoiceStreamingTranscriber` is available, open the stream and spawn `streamingTranscribeLoop`.
   - In `audioInputLoop`, when `vad.IsSpeaking && streamingSTTStream != nil`, call `streamingSTTStream.SendAudio(frame)`.
   - When `ended && streamingSTTStream != nil`: do NOT spawn `transcribeAndSend`; the stream will emit a final event.
   - New goroutine `streamingTranscribeLoop`:
     - Reads from `stream.Events()`
     - On `interim`: `self.setInterimText(event.Text)` and notify observers `OnInterimText`
     - On `final`: call `self.transcribeAndSend(turnId, event.Text)` — note: no audio bytes needed when text is already available; modify `transcribeAndSend` to accept pre-validated text via an optional parameter (or create a new `sendTranscriptText(turnId, text)`)
     - On `error`: log, fall back to batch Whisper for this turn
   - If `streamingSTTStream == nil`, existing `transcribeAndSend` path is unchanged (backward compat).

7. Update observer calls: `OnTranscribeStart/End` should still be called for the streaming path (at the moment the final event arrives, not when the WebSocket frame is received).

**Tests:**
- `TestDeepgramClient_StreamTranscribe`: mock WebSocket server (`httptest.NewServer` + `gorilla/websocket`); verify: connection established, PCM frames forwarded, mock `Results` JSON response produces `TranscribeStreamEvent{Type:"final", Text:"hello world"}`.
- `TestDeepgramClient_KeepAlive`: verify keep-alive JSON sent after 8-second ticker.
- `TestDeepgramClient_ErrorMidStream`: close mock WebSocket mid-stream; verify `TranscribeStreamEvent{Err: non-nil}` emitted; no goroutine leak.
- `TestStreamingTranscribeLoop_FallbackOnError`: simulate stream error; verify `transcribeAndSend` (Whisper) is called for the turn.
- `TestStreamingTranscribeLoop_NoRaceWithAudioInputLoop`: run both goroutines with mock stream for 5 seconds; `-race` flag.

**Acceptance criteria:**
- With Deepgram enabled, p50 `stt_ms` ≤ 150 ms measured in e2e (requires real Deepgram key in staging; skip in unit tests).
- With Deepgram disabled (no API key), pipeline falls back to Whisper transparently.
- `go test -race ./internal/voice/...` and `go test -race ./internal/providers/...` pass.
- No goroutine leak after session close (verify with `goleak` or manual goroutine count check).

---

### L2.2 — TurnStrategy Abstraction + BalancedStrategy

| Field | Value |
|-------|-------|
| **Branch** | `codex/l2-2-turn-strategy` |
| **Base** | `pipeline` (after L1.2 merged) |
| **Risk** | Medium |
| **Merge conflict risk** | High — touches `pipeline.go` barge-in section and `session.go` |

**Objective:** Replace hardcoded barge-in threshold and auto-commit logic in `audioInputLoop` with a `TurnStrategy` interface. Implement `LegacyStrategy` (exact current behaviour) and `BalancedStrategy` (improved). Select via config.

**Scope (files to change/create):**
- `internal/voice/turn_strategy.go` — NEW: `TurnDecision`, `TurnContext`, `TurnStrategy` interface, `LegacyStrategy`
- `internal/voice/turn_strategy_balanced.go` — NEW: `BalancedStrategy`
- `internal/voice/pipeline.go` — replace threshold checks with strategy calls (barge-in section AND `if ended {` section)
- `internal/voice/session.go` — add `strategy TurnStrategy` field; `speechDurationMs` tracking
- `internal/configs/config.go` — add `voice.turn_strategy` string field

**Pipeline.go owned hunks (coordinate with L2.3 agent):**
- This task owns the barge-in handler block (approximately lines 50–90 of `audioInputLoop`)
- This task owns the `if ended {` block in `audioInputLoop`
- **Does NOT touch** `ttsSynthLoop` (owned by L2.3)

**Implementation steps:**

1. Create `turn_strategy.go` with `TurnDecision`, `TurnContext`, `TurnStrategy`, and `LegacyStrategy` exactly as specified in engineering plan §L2.2.

2. Create `turn_strategy_balanced.go` with `BalancedStrategy`, `endsWithSentenceTerminator()`, `endsWithDanglingConjunction()`, and `danglingConjunctions` slice.

3. Add to `Session`:
   ```go
   strategy         TurnStrategy
   speechStartedAt  time.Time  // for SpeechDurationMs calculation
   ```

4. In `NewSession()`, read `voice.turn_strategy` config and set `strategy` to `LegacyStrategy{}` or `BalancedStrategy{}` accordingly. Default: `LegacyStrategy{}`.

5. In `audioInputLoop`, replace:
   ```go
   // BEFORE: if self.Features.BargeIn && score >= bargeInTriggerMinScore && ...
   ```
   with:
   ```go
   if self.Features.BargeIn && (runActive || responseActive) {
       ctx := TurnContext{
           VADScore:         score,
           SpeechDurationMs: int(time.Since(self.speechStartedAt).Milliseconds()),
           RunActive:        runActive,
           ResponseActive:   responseActive,
           InterimText:      self.getInterimText(),
       }
       switch self.strategy.EvaluateBargeIn(ctx) {
       case TurnDecisionTrigger:
           self.triggerBargeIn()
       case TurnDecisionCandidate:
           self.sendVoiceEvent("turn.event", map[string]any{
               "turn_id": turnId, "event": "barge_in_candidate", "vad_score": score,
           })
       }
   }
   ```

6. Replace `if ended {` auto-commit:
   ```go
   if ended {
       ctx := TurnContext{
           SilenceDurationMs: silenceDurationMs,
           InterimText:       self.getInterimText(),
       }
       if !self.strategy.ShouldCommitTurn(ctx) {
           continue
       }
       // ... existing commit logic unchanged ...
   }
   ```

7. Track `silenceDurationMs` by counting frames after VAD `ended` fires.

8. Add `voice.turn_strategy` to `config.go`.

**Tests:**
- `TestLegacyStrategy_BargeIn_Trigger`: score `0.07` → `TurnDecisionTrigger`.
- `TestLegacyStrategy_BargeIn_Ignore`: score `0.05` → `TurnDecisionIgnore`.
- `TestLegacyStrategy_ShouldCommit`: always true when ended.
- `TestBalancedStrategy_BargeIn_Debounce`: score `0.15` at 50 ms speech → `TurnDecisionCandidate`; same at 150 ms → `TurnDecisionTrigger`.
- `TestBalancedStrategy_BargeIn_ScoreDrop`: score below `BargeInMinScore` → `TurnDecisionIgnore`.
- `TestBalancedStrategy_ShouldCommit_DanglingConjunction`: "I want to and" → false at 300 ms, true at 700 ms.
- `TestBalancedStrategy_ShouldCommit_SentenceTerminator`: "Hello world." → true at 150 ms.
- `TestBalancedStrategy_ShouldCommit_MaxSilence`: any text → true at 700 ms.
- `TestBargeInCandidate_EventEmitted`: pipeline integration with mock strategy returning `TurnDecisionCandidate`; assert event emitted.
- `TestBargeInSuppressed_EventEmitted`: candidate then score drops; assert `barge_in_suppressed` event.

**Acceptance criteria:**
- `voice.turn_strategy = "legacy"` reproduces exact current barge-in and commit behaviour (e2e `--compare` shows no change).
- `go test -race ./internal/voice/...` passes.
- `go build ./...` passes.

---

### L2.3 — Streaming TTS with ElevenLabs

| Field | Value |
|-------|-------|
| **Branch** | `codex/l2-3-streaming-tts` |
| **Base** | `pipeline` (after L1.2 merged; can run in parallel with L2.2) |
| **Risk** | Medium |
| **Merge conflict risk** | High — touches `pipeline.go` `ttsSynthLoop`; coordinate scope with L2.2 |

**Objective:** Add `StreamingAudioSynthesizer` interface; implement `ElevenLabsClient`; restructure `ttsSynthLoop` to stream PCM chunks as they arrive rather than waiting for full synthesis.

**Scope (files to change/create):**
- `internal/providers/interface.go` — add `StreamingAudioSynthesizer`, `SynthesizeStreamRequest`, `SynthesizeChunk`
- `internal/providers/elevenlabs.go` — NEW: `ElevenLabsClient`
- `internal/voice/gateway.go` — add `SynthesizePCMStream` method to `VoiceSynthesizer`
- `internal/voice/pipeline.go` — restructure `ttsSynthLoop` (owns this hunk; does NOT touch `audioInputLoop`)
- `internal/gw/gateway.go` — update `voiceSynthesizerAdapter` to check for `StreamingAudioSynthesizer` first; wrap batch synth in single-chunk channel if streaming not available

**Pipeline.go owned hunks (coordinate with L2.2 agent):**
- This task owns `ttsSynthLoop` only
- **Does NOT touch** `audioInputLoop` barge-in or commit logic (owned by L2.2)

**Implementation steps:**

1. Add to `providers/interface.go`: `StreamingAudioSynthesizer`, `SynthesizeStreamRequest`, `SynthesizeChunk` (exact types from engineering plan §L2.3).

2. Implement `ElevenLabsClient` in `providers/elevenlabs.go`:
   - `SynthesizeStream` opens `wss://api.elevenlabs.io/v1/text-to-speech/{voice_id}/stream-input?model_id=eleven_flash_v2_5&output_format=pcm_24000`
   - Auth: `xi-api-key` header
   - Sender goroutine: write `{"text": "<sentence>", "try_trigger_generation": true}` then `{"text": " "}` end signal
   - Receiver goroutine: read binary PCM frames; send as `SynthesizeChunk` to output channel
   - On `ctx.Done()`: close WebSocket; drain receiver; close output channel

3. Add `SynthesizePCMStream(ctx, text, voice, sampleRateHz) (<-chan []byte, error)` to `VoiceSynthesizer` in `gateway.go`.

4. In `gateway.go` `voiceSynthesizerAdapter`: check if underlying provider implements `StreamingAudioSynthesizer`; if yes, use it; if no, call batch `SynthesizePCM` and emit single `[]byte` on a one-item channel.

5. In `pipeline.go` `ttsSynthLoop`, replace:
   ```go
   pcm, err := synth.SynthesizePCM(ctx, sentence, voice, sampleRate)
   ```
   with:
   ```go
   chunks, err := synth.SynthesizePCMStream(ctx, sentence, voice, sampleRate)
   for chunk := range chunks {
       if chunk.Err != nil { break }
       // record FirstAudioSent on first chunk (notify observer)
       self.enqueueAudioOut(EncodeBinaryAudioFrame(..., chunk))
   }
   ```

6. Ensure `ctx` passed to `SynthesizePCMStream` is the cancellable context from `SwapTTSCancel`; when barge-in fires and `SwapTTSCancel(nil)` is called, the context cancels, the ElevenLabs WebSocket goroutine exits, and the channel closes cleanly.

**Tests:**
- `TestElevenLabsClient_StreamSynthesize`: mock WebSocket server; verify JSON text message sent; mock binary PCM frames returned; verify `SynthesizeChunk` channel emits chunks then closes.
- `TestElevenLabsClient_ContextCancel`: cancel context mid-stream; verify no goroutine leak.
- `TestTTSSynthLoop_StreamingPath`: mock `VoiceSynthesizer` returning channel; verify each chunk is forwarded to `audioOutCh`.
- `TestTTSSynthLoop_BatchFallback`: mock synthesizer without streaming; verify graceful degradation via single-chunk channel adapter.
- `TestBargeIn_CancelsTTSStream`: trigger barge-in while streaming; verify streaming goroutine exits (use a `sync.WaitGroup` in the mock).

**Acceptance criteria:**
- First audio chunk reaches client within ≤ 100 ms of TTS request by default, or ≤ 350 ms when `voice.synth_provider=elevenlabs` (measured via `turn.metrics.tts_ms` in e2e). ElevenLabs `eleven_flash_v2_5` TTFB is structurally 260–329 ms (model inference, not connection overhead); 350 ms allows 20 ms variance headroom.
- Barge-in during streaming TTS cancels the stream cleanly with no goroutine leak.
- `go test -race ./internal/voice/...` and `go test -race ./internal/providers/...` pass.

---

### L2.1 — Speculative LLM on Interim Transcripts

| Field | Value |
|-------|-------|
| **Branch** | `codex/l2-1-speculative-llm` |
| **Base** | `pipeline` (after L1.2 AND L2.2 merged) |
| **Risk** | High (speculative run management; cancellation races) |
| **Merge conflict risk** | Medium — owns new fields in `session.go` and new logic in streaming loop |

**Objective:** Start an LLM call on stable interim transcripts; promote the speculative run if the final transcript matches; cancel it if it diverges.

**Scope (files to change/create):**
- `internal/voice/session.go` — add speculative run fields
- `internal/voice/pipeline.go` — add speculation logic in `streamingTranscribeLoop`
- `internal/voice/gateway.go` — add `CancelRun` to `GatewayDeps`; `IsSpeculative` to `VoiceSendMessageParams`
- `internal/voice/similarity.go` — NEW: Wagner-Fischer edit distance or Jaccard similarity
- `internal/gw/gateway.go` — implement `CancelRun` (non-barge-in run abort)

**Out of scope:** Any change to the LLM runner or response handling. Speculative runs are handled identically to real runs by the LLM layer — the only difference is how they are cancelled (via `CancelRun` instead of `AbortRun`).

**Implementation steps:**

1. Add to `GatewayDeps`:
   ```go
   CancelRun(runId string)  // cancels without emitting barge_in_triggered event
   ```
   Implement in `gw/gateway.go` (use S2 findings if applicable).

2. Add `IsSpeculative bool` to `VoiceSendMessageParams`.

3. Add to `Session` struct:
   ```go
   speculativeRunId   string
   speculativeText    string
   speculativeMu      sync.Mutex  // separate from stateMu
   ```

4. Create `similarity.go` with `textSimilarity(a, b string) float64` using Wagner-Fischer edit distance on rune slices. Return `1.0 - editDistance(a,b)/max(len(a),len(b))`.

5. In `streamingTranscribeLoop`, on each `interim` event:
   - If `len(event.Text) >= speculativeMinRunes (20)` AND `event.Confidence >= 0.80`:
     - If no speculative run active and no real run active:
       - Call `deps.SendMessage()` with `IsSpeculative: true`
       - Store `speculativeRunId` + `speculativeText`
     - If speculative run active and new text diverges (similarity < 0.80):
       - Cancel old speculative run via `deps.CancelRun(speculativeRunId)`
       - Start new speculative run with updated text

6. On `final` event:
   - If `speculativeRunId != ""`:
     - Compute `textSimilarity(event.Text, speculativeText)`
     - If ≥ 0.80: promote — `SetCurrentRunId(speculativeRunId)`; clear `speculativeRunId`
     - If < 0.80: `CancelRun(speculativeRunId)`; start fresh `SendMessage` with final text

7. In `triggerBargeIn()`, also call `deps.CancelRun(speculativeRunId)` if set.

8. Guard rails (do not start speculative run if):
   - Barge-in triggered within last 500 ms
   - `HasTranscriptionInFlight()` is true
   - Speculative run is > 3 seconds old (cancel and start fresh)

**Tests:**
- `TestSimilarity_EditDistance`: unit tests for `textSimilarity`.
- `TestSpeculative_Promoted`: mock gateway; interim "hello world" at confidence 0.9; final "hello world"; assert `SetCurrentRunId` called with speculative run ID; assert `SendMessage` called once total (not twice).
- `TestSpeculative_Cancelled_Diverged`: interim "hello friend" → speculative run; final "goodbye world" (similarity < 0.5); assert `CancelRun` called; assert new `SendMessage` called with final text.
- `TestSpeculative_CancelledOnBargeIn`: trigger barge-in while speculative run active; assert `CancelRun` called for speculative run.
- `TestSpeculative_GuardRail_RecentBargeIn`: do not start speculative run within 500 ms of barge-in.
- `TestSpeculative_Race`: concurrent interim events from two goroutines with `-race`.

**Acceptance criteria:**
- Speculative run is promoted (no second `SendMessage`) when final matches interim with similarity ≥ 0.80.
- No double-LLM-call for a single turn when speculation succeeds.
- No goroutine leak after session close when speculative run is in flight.
- `go test -race ./internal/voice/...` passes.

---

### L2.4 — Conversation Context Window Management

| Field | Value |
|-------|-------|
| **Branch** | `codex/l2-4-context-window` |
| **Base** | `pipeline` (after L2.1 merged; after S2 decision) |
| **Risk** | Low-Medium (touches gateway message assembly) |
| **Merge conflict risk** | Low — primarily `gw/gateway.go`; one constant in `pipeline.go` |

**Objective:** Prune conversation history before each LLM call for voice sessions to stay within a 16,000-token budget. Emit a DEBUG log when pruning occurs.

**Scope (files to change):**
- `internal/gw/gateway.go` — add context pruning at the message assembly point identified by S2
- `internal/voice/pipeline.go` — add `voiceMaxContextTokens = 16000` constant; pass it in `VoiceSendMessageParams`
- `internal/voice/gateway.go` — add `MaxContextTokens int` to `VoiceSendMessageParams`

**Out of scope:** Token counting via a real tokenizer. Use `len(text) / 4` (byte approximation). Do not change how non-voice LLM calls handle context.

**Implementation steps:**

1. Add `MaxContextTokens int` to `VoiceSendMessageParams` in `voice/gateway.go`. Zero means "use default".

2. Add `voiceMaxContextTokens = 16000` constant to `pipeline.go`. Set it in `commitVoiceTurn()` when calling `deps.SendMessage()`.

3. At the message assembly point in `gw/gateway.go` (per S2 spike findings):
   - If `params.MaxContextTokens > 0`, apply pruning:
     ```
     always keep: system prompt + last 2 turns
     fill remaining budget from most-recent turns backward
     drop oldest user+assistant pairs together
     ```
   - Token estimate: `tokens += len(message.Content) / 4` for each message.
   - Emit DEBUG log when any message is dropped: `"voice context pruned: kept=%d dropped=%d estimated_tokens=%d"`.

**Tests:**
- `TestContextPruning_FitsInBudget`: 5 turns × 1000 tokens → no pruning; assert all 5 turns in assembled messages.
- `TestContextPruning_ExceedsBudget`: 20 turns × 1000 tokens → assert only most-recent turns kept; assert system prompt + last 2 turns always present.
- `TestContextPruning_AlwaysKeepsLast2`: set budget so only 2 turns fit; assert exactly turns N-1 and N are kept.
- `TestContextPruning_LogsWhenPruning`: assert DEBUG log emitted when turns dropped.
- `TestContextPruning_SkippedForNonVoice`: `MaxContextTokens = 0`; assert no pruning applied.

**Acceptance criteria:**
- Long voice sessions (> 30 turns) do not send > 16,000 estimated tokens.
- DEBUG log appears when pruning occurs.
- Existing text-chat calls are unaffected (`MaxContextTokens = 0` path).
- `go test ./internal/gw/...` passes.

---

### DOCS — Documentation and Runbook Update

| Field | Value |
|-------|-------|
| **Branch** | `codex/docs-runbook-update` |
| **Base** | `pipeline` (after Wave 8 merged; can start drafting earlier) |
| **Risk** | None |
| **Merge conflict risk** | None (docs/ only) |

**Objective:** Satisfy `ENGINEERING_PLAN_VOICE_PIPELINE_LEVEL1_LEVEL2.md` §8 ("Documentation and runbook update") and the primary plan §Rollout Plan PR discipline requirement. Document every new behavior, config field, and rollback switch so that operators can manage the system without reading source code.

**Scope (files to change/create):**
- `docs/runbook-voice-pipeline.md` — new operator runbook
- `docs/VOICE_PIPELINE_CHANGES.md` — changelog per milestone (P0, L1, L2)

**Out of scope:** API documentation, client-facing docs.

**Runbook sections required:**

1. **Config reference** — every new field added across all tasks:
   - `voice.transcriber_provider` (P0.1)
   - `voice.synth_provider` (P0.1)
   - `voice.turn_strategy` (L2.2) — values: `legacy` (default), `balanced`
   - `features.silero_vad` (L1.1)
   - `features.server_vad`, `features.server_turn`, `features.server_denoise` (P0.2)
   - `MaxContextTokens` behaviour (L2.4)

2. **Rollback switches** — for each milestone, one paragraph describing how to revert to previous behaviour without a code deployment:
   - P0: all new config fields default to unchanged behaviour; no action needed.
   - L1.1: set `features.silero_vad = false` to revert to `EnergyVAD`.
   - L1.2: remove Deepgram API key from env; pipeline auto-falls back to Whisper.
   - L2.2: set `voice.turn_strategy = legacy`.
   - L2.3: remove ElevenLabs API key; adapter falls back to batch OpenAI TTS.
   - L2.1: set `speculative_llm_enabled = false` (add this config field in L2.1 implementation).
   - L2.4: set `MaxContextTokens = 0` to disable pruning.

3. **Observability guide** — how to read `turn.metrics` events; field meanings; p50/p90/p99 targets per milestone.

4. **Incident playbook** — what to do when:
   - Deepgram WebSocket fails to connect (check `stt_fallback_rate` metric; expected < 5%)
   - ElevenLabs stream produces silence (check `tts_ms` in `turn.metrics`; revert to batch TTS)
   - Speculative run divergence rate > 20% (disable speculative LLM; check Deepgram confidence)
   - Context pruning DEBUG logs appearing too frequently (increase `MaxContextTokens` or reduce system prompt)

**Acceptance criteria:**
- All config fields documented with type, default, and example.
- All rollback switches documented and tested (confirm the rollback produces a passing e2e `--compare` vs baseline).
- Runbook reviewed by at least one person other than the author before merge.

---

## Section 3 — Subagent Assignment Plan

### Agent roster and wave assignments

| Agent | Wave | Task(s) | File ownership | Notes |
|-------|------|---------|----------------|-------|
| **Agent-S1** | 0 | S1 Silero spike | `docs/` only | Read-only research; outputs `spike-s1-silero.md` |
| **Agent-S2** | 0 | S2 context-window spike | `docs/` only | Read-only research; outputs `spike-s2-context-window.md` |
| **Agent-A** | 1 | P0.1 deterministic providers | `providers/registry.go`, `configs/` | Isolated; safe to parallelize with Agent-B |
| **Agent-B** | 1 | P0.2 then P0.3 (sequential) | `voice/pipeline.go`, `voice/session.go`, `api/v1api/rpc_voice.go` | MUST be one agent; same files |
| **Agent-C** | 2 | L1.3 observer telemetry | `voice/observer.go` (new), `voice/metrics.go` (new), `voice/session.go`, `voice/pipeline.go`, `test/voicee2e/` | Solo wave; measurement before VAD changes |
| **Agent-E** | 3 | L1.1 Silero VAD | `voice/vad.go`, `voice/vad_silero.go` (new), `voice/pipeline.go` (1 block), `voice/session.go` (1 field) | Needs S1 decision; rebases on Wave 2 |
| **Agent-D** | 3 | L1.4 client audio gate | `web/src/hooks/` | Pure TypeScript; ships in same wave as L1.1 per plan |
| **Agent-F** | 4 | L1.2 Deepgram streaming STT | `providers/interface.go`, `providers/deepgram.go` (new), `providers/registry.go`, `voice/gateway.go`, `voice/session.go`, `voice/pipeline.go` | Largest change; rebases on Wave 3 |
| **Agent-G** | 5 | L2.2 TurnStrategy | `voice/turn_strategy.go` (new), `voice/turn_strategy_balanced.go` (new), `voice/pipeline.go` (barge-in + `if ended {` hunks), `voice/session.go`, `configs/` | Solo wave; no parallelism to avoid L2 rebase churn |
| **Agent-I** | 6 | L2.1 Speculative LLM | `voice/session.go`, `voice/pipeline.go`, `voice/gateway.go`, `voice/similarity.go` (new), `gw/gateway.go` | Rebases on L1.2 + L2.2 |
| **Agent-H** | 7 | L2.3 Streaming TTS | `providers/interface.go`, `providers/elevenlabs.go` (new), `voice/gateway.go`, `voice/pipeline.go` (ttsSynthLoop hunk), `gw/gateway.go` (adapter) | Follows L2.1 per plan sequence |
| **Agent-J** | 8 | L2.4 Context window | `gw/gateway.go`, `voice/pipeline.go` (constant + one call site), `voice/gateway.go` | Requires S2 + L2.3 merged |
| **Agent-K** | 9 | DOCS runbook + changelog | `docs/` only | Can draft from Wave 7 onward; final merge after Wave 8 |

### Parallel-safety justification

**Wave 0 (S1 || S2):** Both are read-only research. No conflicts possible.

**Wave 1 (P0.1 || [P0.2 → P0.3]):**
- P0.1 touches only `providers/registry.go` and `configs/`. P0.2+P0.3 touch only `voice/session.go`, `voice/pipeline.go`, `api/v1api/rpc_voice.go`. Zero file overlap. Safe to run in parallel.
- P0.2 and P0.3 must be sequential (same files); assign both to Agent-B so no merge conflict arises.

**Wave 2 (L1.3 solo):**
- L1.3 runs alone so that the observer hooks exist in `pipeline.go` and `session.go` before any subsequent task rebases on them. If L1.4 ran here instead of Wave 3, it would merge before the VAD change it is supposed to accompany.

**Wave 3 (L1.1 || L1.4):**
- L1.1 touches only Go files under `internal/voice/`. L1.4 touches only TypeScript under `web/src/hooks/`. Zero file overlap. Safe to run in parallel.
- Merging both in the same wave satisfies the plan requirement to "ship with L1.1".

**Wave 5 (L2.2 solo):**
- L2.2 runs alone to avoid pipeline.go rebase churn. The previous backlog version ran L2.2 and L2.3 in parallel using scope-delineated hunks, but this created merge ordering complexity and violated the plan's strict L2.2 → L2.1 → L2.3 sequence. Solo execution eliminates that risk at the cost of one extra wave.
- If the agents produce conflicting hunks in `pipeline.go`, the integrator resolves by taking both non-overlapping sections.

---

## Section 4 — Integration Plan

### Merge order (strict — aligns with plan §Implementation Ordering)

```
1.  pipeline ← codex/p0-1-deterministic-providers
2.  pipeline ← codex/p0-2-feature-flags
3.  pipeline ← codex/p0-3-functional-commit          (branch from p0-2; merge after p0-2)
4.  pipeline ← codex/l1-3-observer-telemetry         (measurement before VAD/STT changes)
5.  pipeline ← codex/l1-1-silero-vad                 (needs S1 decision + wave 2 merged)
6.  pipeline ← codex/l1-4-client-audio-gate          (same wave as l1-1; merge either order)
7.  pipeline ← codex/l1-2-deepgram-streaming-stt     (needs wave 3 merged)
8.  pipeline ← codex/l2-2-turn-strategy              (L2 sequence starts here)
9.  pipeline ← codex/l2-1-speculative-llm            (needs l1-2 + l2-2 merged)
10. pipeline ← codex/l2-3-streaming-tts              (follows l2-1 per plan sequence)
11. pipeline ← codex/l2-4-context-window             (needs S2 + l2-3 merged)
12. pipeline ← codex/docs-runbook-update             (final; after wave 8)
```

### Rebase protocol

When a task's PR is open and a prerequisite merges to `pipeline`:

```bash
git fetch origin
git rebase origin/pipeline
# resolve conflicts (see conflict map below)
git push --force-with-lease origin codex/<task-id>-<short-name>
```

### Expected conflict zones and resolutions

| Conflict files | PRs that conflict | Resolution |
|----------------|-------------------|------------|
| `voice/pipeline.go` | L1.1 after L1.3, L1.2 after L1.1, L2.2 after L1.2, L2.1 after L2.2, L2.3 after L2.1 | Each rebase: take new file as base; re-apply task's diff; verify observer calls still present |
| `voice/session.go` | P0.2 → P0.3 → L1.3 → L1.1 → L1.2 → L2.2 → L2.1 | Sequential merge order eliminates most conflicts; each task adds non-overlapping struct fields |
| `providers/interface.go` | L1.2 (StreamingTranscriber) then L2.3 (StreamingAudioSynthesizer) | Non-overlapping type blocks; no functional conflict |
| `voice/gateway.go` | L1.2 (streaming STT types) → L2.1 (CancelRun) → L2.3 (streaming TTS method) | Merge in order; each adds distinct methods/types |
| `gw/gateway.go` | L2.1 (CancelRun impl) → L2.3 (TTS adapter update) → L2.4 (context pruning) | Sequential; L2.3 and L2.4 touch different functions |

### Per-PR readiness checklist (required on every PR, any wave)

Every PR merging to `pipeline` must include all three of the following in its description before approval. This is required by `docs/engineering_plan_pipeline.md` §Rollout Plan / PR discipline.

```
## PR Checklist
- [ ] Changed behavior summary: one paragraph describing what is different after this PR
      versus before, from the perspective of a voice session operator.
- [ ] Before/after metrics: paste output of:
        go run ./test/voicee2e/cmd/voicee2e/main.go --compare \
          --prompt-a test/voicee2e/reports/baseline.json \
          --prompt-b test/voicee2e/reports/<this-pr>.json
      If no measurable behavior change (e.g. pure refactors), state explicitly:
      "This change has no measurable impact on turn.metrics; --compare shows 0 delta."
- [ ] Rollback switch: one sentence naming the config flag, environment variable, or
      strategy fallback that reverts this PR's behavior without a code deployment.
      Example: "Set voice.turn_strategy=legacy to revert to pre-PR barge-in behavior."
      For bug fixes with no new config: "No rollback switch needed; this fix cannot be
      disabled. The change is safe because [reason]."
```

### Wave-level integration test gates

Run the standard block after each wave merges to `pipeline` before starting the next wave.

**Standard block (run after every wave):**
```bash
go build ./...
go vet ./...
go test -race ./internal/providers/...
go test -race ./internal/voice/...
go test -race ./internal/api/v1api/...
```

**Wave 1 (P0 complete) — additional:**
```bash
make test-voice-e2e-smoke  # all scenarios S1-S6 pass
# Assert: no existing scenario regresses
# Assert: P0 correctness counters emittable (provider_selected same across 100 calls)
```

**Wave 2 (L1.3 observer) — additional:**
```bash
go run ./test/voicee2e/cmd/voicee2e/main.go \
  -gateway-url http://localhost:8833 \
  -suite test/voicee2e/scenarios/suite.yaml \
  -out test/voicee2e/reports/after-wave2.json
# Assert: turn.metrics events present in JSON output (jq '.[] | .turn_metrics | length > 0')
# Assert: all timestamp fields non-zero for complete turns
```

**Wave 3 (L1.1 + L1.4) — additional:**
```bash
go run ./test/voicee2e/cmd/voicee2e/main.go \
  -gateway-url http://localhost:8833 \
  -suite test/voicee2e/scenarios/suite.yaml \
  -config '{"features":{"silero_vad":true}}' \
  -out test/voicee2e/reports/after-wave3.json
go run ./test/voicee2e/cmd/voicee2e/main.go \
  --compare --prompt-a test/voicee2e/reports/baseline.json \
            --prompt-b test/voicee2e/reports/after-wave3.json
# Assert: false_speech_starts_per_session <= 1.0
# Assert: no scenario regresses on MinTranscriptSimilarity
```

**Wave 4 (L1.2 Deepgram) — additional:**
```bash
DEEPGRAM_API_KEY=<key> go run ./test/voicee2e/cmd/voicee2e/main.go \
  -gateway-url http://localhost:8833 \
  -suite test/voicee2e/scenarios/suite.yaml \
  -out test/voicee2e/reports/after-wave4.json
go run ./test/voicee2e/cmd/voicee2e/main.go \
  --compare --prompt-a test/voicee2e/reports/baseline.json \
            --prompt-b test/voicee2e/reports/after-wave4.json
# Assert: turn.metrics.stt_ms p50 <= 150ms
# Assert: turn.metrics.e2e_ms p50 <= 1000ms  (Level 1 target)
# Assert: stt_fallback_rate < 5%  (check log count "falling back to batch STT")
```

**Wave 5 (L2.2 TurnStrategy) — additional:**
```bash
# Regression check: legacy strategy must produce 0 delta vs original baseline
go run ./test/voicee2e/cmd/voicee2e/main.go \
  -gateway-url http://localhost:8833 \
  -suite test/voicee2e/scenarios/suite.yaml \
  -config '{"voice":{"turn_strategy":"legacy"}}' \
  -out test/voicee2e/reports/after-wave5-legacy.json
go run ./test/voicee2e/cmd/voicee2e/main.go \
  --compare --prompt-a test/voicee2e/reports/baseline.json \
            --prompt-b test/voicee2e/reports/after-wave5-legacy.json
# Assert: 0 scenario regressions
# Assert: barge_in_candidate and barge_in_suppressed events visible in logs
```

**Wave 6 (L2.1 Speculative LLM) — additional:**
```bash
go run ./test/voicee2e/cmd/voicee2e/main.go \
  -gateway-url http://localhost:8833 \
  -suite test/voicee2e/scenarios/suite.yaml \
  -out test/voicee2e/reports/after-wave6.json
# Assert: speculative promotion rate visible in logs
# Assert: no double-LLM-call observed (check log: "SendMessage called" count per turn)
# Assert: spurious_barge_ins_per_session <= 0.5
```

**Wave 7 (L2.3 Streaming TTS) — additional:**
```bash
DEEPGRAM_API_KEY=<key> ELEVENLABS_API_KEY=<key> go run ./test/voicee2e/cmd/voicee2e/main.go \
  -gateway-url http://localhost:8833 \
  -suite test/voicee2e/scenarios/suite.yaml \
  -out test/voicee2e/reports/after-wave7.json
go run ./test/voicee2e/cmd/voicee2e/main.go \
  --compare --prompt-a test/voicee2e/reports/baseline.json \
            --prompt-b test/voicee2e/reports/after-wave7.json
# Assert: 6/6 scenarios pass (functional gate)
# Assert: turn.metrics.tts_ms p50 <= 100ms (default batch TTS)
#         if voice.synth_provider=elevenlabs: <= 350ms
#         (ElevenLabs flash_v2_5 model TTFB is 260-329ms structurally)
# Assert: turn.metrics.stt_ms p50 <= 100ms
# Informational: turn.metrics.e2e_ms p50 — not a hard gate (bimodal: speculative vs
#   non-speculative turns produce very different latencies; cross-suite p50 is not meaningful).
#   Use per-scenario max_response_latency_ms in suite.yaml for latency gating instead.
# Assert: barge-in during streaming TTS leaves 0 goroutine leak (runtime.NumGoroutine check)
```

**Wave 8 (L2.4 Context window) — additional:**
```bash
go test ./internal/gw/... -run TestContextPruning -v
# Assert: DEBUG log "voice context pruned" appears in 30-turn session
# Assert: 10-min soak session does not send > 16000 estimated tokens per turn
```

### Final integration test suite (Definition of Done gate)

This gate must pass before any L2 milestone is declared done. It covers all KPIs from `docs/engineering_plan_pipeline.md` §Success Metrics.

```bash
# 1. Build + vet
go build ./...
go vet ./...

# 2. Race-detected unit tests (mandatory: -race catches pipeline goroutine bugs)
go test -race ./internal/voice/...
go test -race ./internal/providers/...
go test -race ./internal/api/v1api/...
go test -race ./internal/gw/...

# 3. E2E final comparison vs original baseline
go run ./test/voicee2e/cmd/voicee2e/main.go \
  -gateway-url http://localhost:8833 \
  -suite test/voicee2e/scenarios/suite.yaml \
  -out test/voicee2e/reports/final.json

go run ./test/voicee2e/cmd/voicee2e/main.go \
  --compare \
  --prompt-a test/voicee2e/reports/baseline.json \
  --prompt-b test/voicee2e/reports/final.json

# 4. Primary KPI assertions (from plan §Success Metrics table)
# Verify the following from final.json turn.metrics percentiles:
#   stt_ms p50 <= 100ms          (was 400-800ms baseline; streaming Deepgram path)
#   tts_ms p50 <= 100ms          (default batch TTS target)
#   tts_ms p50 <= 350ms          (when voice.synth_provider=elevenlabs;
#                                  ElevenLabs flash_v2_5 TTFB is 260-329ms structurally)
#   llm_ttfb_ms p50 unchanged    (no regression; LLM latency not targeted)
#   e2e_ms: INFORMATIONAL ONLY — not a hard gate.
#     Cross-suite p50 is bimodal (speculative turns ~79ms vs non-speculative ~1500ms+).
#     Use per-scenario max_response_latency_ms thresholds in suite.yaml instead.
#     Document observed p50 in final.json for trend tracking.

# 5. Secondary KPI assertions (from plan §Secondary KPIs)
# Check logs and turn.metrics for:
#   false_speech_starts_per_session <= 1.0      (speech_started with no turn_committed)
#   spurious_barge_ins_per_session <= 0.2       (barge_in_triggered with no user intent)
#   premature_commits_per_session <= 0.3        (turn_committed mid-sentence)
#   stt_fallback_rate < 5%                      (log count "falling back to batch STT")
#   turn_drop_rate < 10%                        (observer drop counter)
#   queue_overflow_rate < 1%                    (dropped_queue_overflow event count)
#   session_error_rate <= 0.5%                  (streaming reconnect + TTS errors in logs)

# 6. P0 correctness counters (from plan §P0 Correctness Counters)
#   provider_selection_nondeterminism_events = 0
#   feature_flag_divergence_events = 0
#   barge_in_candidate events visible in logs (>= 1 in noisy environment test)

# 7. Strategy regression check (legacy = original baseline)
go run ./test/voicee2e/cmd/voicee2e/main.go \
  -gateway-url http://localhost:8833 \
  -suite test/voicee2e/scenarios/suite.yaml \
  -config '{"voice":{"turn_strategy":"legacy"}}' \
  -out test/voicee2e/reports/final-legacy.json

go run ./test/voicee2e/cmd/voicee2e/main.go \
  --compare \
  --prompt-a test/voicee2e/reports/baseline.json \
  --prompt-b test/voicee2e/reports/final-legacy.json
# Assert: 0 scenario regressions with legacy strategy

# 8. 10-minute soak test
# Run a single voice session for 10 minutes using looped fixtures
# Assert: heap growth < 50 MB  (go tool pprof /debug/pprof/heap)
# Assert: goroutine count at end <= goroutine count at start + 8
# Assert: streaming STT WebSocket reconnect count = 0 (check logs)
# Assert: no "audioIn queue full" or "audioOut queue full" warnings in logs
```

---

## Section 5 — Execution Commands

### Setup (run once before any task)

```bash
# Create baseline e2e report before any changes
git checkout pipeline
go run ./test/voicee2e/cmd/voicee2e/main.go \
  -gateway-url http://localhost:8833 \
  -suite test/voicee2e/scenarios/suite.yaml \
  -out test/voicee2e/reports/baseline.json

echo "Baseline report saved. Do not delete this file."
```

---

### Wave 0 — Spikes

```bash
# Agent-S1: Silero CGO spike
git checkout -b codex/s1-silero-spike pipeline
# [research work]
# Write docs/spike-s1-silero.md with decision
git add docs/spike-s1-silero.md
git commit -m "spike: Silero VAD CGO vs HTTP sidecar decision"

# Agent-S2: Context window gateway spike
git checkout -b codex/s2-context-window-spike pipeline
# [research work]
# Write docs/spike-s2-context-window.md
git add docs/spike-s2-context-window.md
git commit -m "spike: context window gateway.go implementation location"
```

---

### Wave 1 — P0 Correctness

```bash
# Agent-A: P0.1 Deterministic providers
git checkout -b codex/p0-1-deterministic-providers pipeline
# [implement + tests]
go test -race ./internal/providers/... -run TestFindTranscriber
go build ./...
# commit + PR → merge to pipeline

# Agent-B: P0.2 Feature flag enforcement
git checkout -b codex/p0-2-feature-flags pipeline
# [implement + tests]
go test -race ./internal/voice/... -run TestAudioInputLoop_ServerVAD
go test -race ./internal/voice/... -run TestAudioInputLoop_ServerTurn
go build ./...
# commit + PR → merge to pipeline

# Agent-B continues: P0.3 Functional commit (branch from p0-2 after merge, or continue on same branch)
git checkout -b codex/p0-3-functional-commit pipeline  # after p0-2 merged
# [implement + tests]
go test -race ./internal/voice/... -run TestInputCommit
go build ./...
# commit + PR → merge to pipeline
```

**Wave 1 gate:**
```bash
git checkout pipeline && git pull
go build ./...
go vet ./...
go test -race ./internal/providers/...
go test -race ./internal/voice/...
go test -race ./internal/api/v1api/...
make test-voice-e2e-smoke
```

---

### Wave 2 — L1.3 Observer Telemetry

```bash
# Agent-C: L1.3 Observer telemetry (after Wave 1 gate passes)
git checkout -b codex/l1-3-observer-telemetry pipeline
# [implement + tests]
go test -race ./internal/voice/... -run TestMetricsObserver
go test -race ./internal/voice/... -run TestNotifyObservers
go build ./...
# Before creating PR, run e2e and collect --compare output for PR checklist:
go run ./test/voicee2e/cmd/voicee2e/main.go \
  -gateway-url http://localhost:8833 \
  -suite test/voicee2e/scenarios/suite.yaml \
  -out test/voicee2e/reports/after-wave2.json
# PR checklist rollback switch: "Remove MetricsObserver from NewSession() to disable all telemetry."
# commit + PR → merge to pipeline
```

**Wave 2 gate:**
```bash
git checkout pipeline && git pull
go build ./... && go vet ./... && go test -race ./internal/voice/...
go run ./test/voicee2e/cmd/voicee2e/main.go \
  -gateway-url http://localhost:8833 \
  -suite test/voicee2e/scenarios/suite.yaml \
  -out test/voicee2e/reports/after-wave2.json
# Assert: turn.metrics events present in output JSON
# Assert: all timestamp fields non-zero for complete turns
```

---

### Wave 3 — L1.1 Silero VAD + L1.4 Client Gate (parallel)

```bash
# Agent-E: L1.1 Silero VAD (after S1 decision + Wave 2 gate)
git checkout -b codex/l1-1-silero-vad pipeline
# [implement per S1 decision: ONNX or HTTP sidecar]
go test -race ./internal/voice/... -run TestEnergyVAD
go test -race ./internal/voice/... -run TestSileroVAD
go build ./...
# PR checklist rollback switch: "Set features.silero_vad=false in voice.start to use EnergyVAD."
# commit + PR → merge to pipeline

# Agent-D: L1.4 Client audio gate (parallel to Agent-E; zero file overlap)
git checkout -b codex/l1-4-client-audio-gate pipeline
# [TypeScript changes only]
npm run build  # or equivalent frontend build command
# manual browser test: whisper "hello" → assert server emits speech_started
# PR checklist rollback switch: "Revert this commit; threshold change is client-only."
# commit + PR → merge to pipeline (either order with L1.1; both must merge before Wave 4)
```

**Wave 3 gate:**
```bash
git checkout pipeline && git pull
go build ./... && go vet ./... && go test -race ./internal/voice/...
go run ./test/voicee2e/cmd/voicee2e/main.go \
  -gateway-url http://localhost:8833 \
  -suite test/voicee2e/scenarios/suite.yaml \
  -config '{"features":{"silero_vad":true}}' \
  -out test/voicee2e/reports/after-wave3.json
go run ./test/voicee2e/cmd/voicee2e/main.go \
  --compare \
  --prompt-a test/voicee2e/reports/baseline.json \
  --prompt-b test/voicee2e/reports/after-wave3.json
# Assert: false_speech_starts_per_session <= 1.0
# Assert: no MinTranscriptSimilarity regression
```

---

### Wave 4 — L1.2 Deepgram Streaming STT

```bash
# Agent-F: L1.2 Deepgram streaming STT (after Wave 3 gate)
git checkout -b codex/l1-2-deepgram-streaming-stt pipeline
# [implement + tests]
go test -race ./internal/providers/... -run TestDeepgramClient
go test -race ./internal/voice/... -run TestStreamingTranscribeLoop
go build ./...
# PR checklist rollback switch: "Remove DEEPGRAM_API_KEY env var; pipeline auto-falls back to Whisper."
# commit + PR → merge to pipeline
```

**Wave 4 gate:**
```bash
git checkout pipeline && git pull
go build ./... && go vet ./...
go test -race ./internal/voice/...
go test -race ./internal/providers/...
DEEPGRAM_API_KEY=<key> go run ./test/voicee2e/cmd/voicee2e/main.go \
  -gateway-url http://localhost:8833 \
  -suite test/voicee2e/scenarios/suite.yaml \
  -out test/voicee2e/reports/after-wave4.json
go run ./test/voicee2e/cmd/voicee2e/main.go \
  --compare \
  --prompt-a test/voicee2e/reports/baseline.json \
  --prompt-b test/voicee2e/reports/after-wave4.json
# Assert: stt_ms p50 <= 150ms
# Assert: e2e_ms p50 <= 1000ms  (Level 1 target)
# Assert: stt_fallback_rate < 5% (grep "falling back to batch STT" in logs)
```

---

### Wave 5 — L2.2 TurnStrategy

```bash
# Agent-G: L2.2 TurnStrategy (after Wave 4 gate)
git checkout -b codex/l2-2-turn-strategy pipeline
# Owns: audioInputLoop barge-in block + "if ended {" block ONLY
# Does NOT touch ttsSynthLoop (owned by L2.3 in Wave 7)
go test -race ./internal/voice/... -run TestLegacyStrategy
go test -race ./internal/voice/... -run TestBalancedStrategy
go test -race ./internal/voice/... -run TestBargeIn
go build ./...
# PR checklist rollback switch: "Set voice.turn_strategy=legacy to revert to original barge-in thresholds."
# commit + PR → merge to pipeline
```

**Wave 5 gate:**
```bash
git checkout pipeline && git pull
go build ./... && go vet ./... && go test -race ./internal/voice/...
# Regression check: legacy strategy must produce 0 delta vs original baseline
go run ./test/voicee2e/cmd/voicee2e/main.go \
  -gateway-url http://localhost:8833 \
  -suite test/voicee2e/scenarios/suite.yaml \
  -config '{"voice":{"turn_strategy":"legacy"}}' \
  -out test/voicee2e/reports/after-wave5-legacy.json
go run ./test/voicee2e/cmd/voicee2e/main.go \
  --compare \
  --prompt-a test/voicee2e/reports/baseline.json \
  --prompt-b test/voicee2e/reports/after-wave5-legacy.json
# Assert: 0 scenario regressions
# Assert: barge_in_candidate events visible in logs
```

---

### Wave 6 — L2.1 Speculative LLM

```bash
# Agent-I: L2.1 Speculative LLM (after Wave 5 gate; requires L1.2 + L2.2 merged)
git checkout -b codex/l2-1-speculative-llm pipeline
# [implement + tests]
go test -race ./internal/voice/... -run TestSimilarity
go test -race ./internal/voice/... -run TestSpeculative
go build ./...
# PR checklist rollback switch: "Set speculative_llm_enabled=false config field (add in impl) to disable."
# commit + PR → merge to pipeline
```

**Wave 6 gate:**
```bash
git checkout pipeline && git pull
go build ./... && go vet ./... && go test -race ./internal/voice/...
DEEPGRAM_API_KEY=<key> go run ./test/voicee2e/cmd/voicee2e/main.go \
  -gateway-url http://localhost:8833 \
  -suite test/voicee2e/scenarios/suite.yaml \
  -out test/voicee2e/reports/after-wave6.json
go run ./test/voicee2e/cmd/voicee2e/main.go \
  --compare \
  --prompt-a test/voicee2e/reports/baseline.json \
  --prompt-b test/voicee2e/reports/after-wave6.json
# Assert: speculative promotion rate visible in logs (grep "speculative run promoted")
# Assert: no double-LLM-call (grep "SendMessage called" count == 1 per turn)
# Assert: spurious_barge_ins_per_session <= 0.5
```

---

### Wave 7 — L2.3 Streaming TTS

```bash
# Agent-H: L2.3 Streaming TTS (after Wave 6 gate; follows L2.1 per plan sequence)
git checkout -b codex/l2-3-streaming-tts pipeline
# Owns: ttsSynthLoop section of pipeline.go ONLY
# Does NOT touch audioInputLoop (owned by L2.2, already merged)
go test -race ./internal/providers/... -run TestElevenLabsClient
go test -race ./internal/voice/... -run TestTTSSynthLoop
go build ./...
# PR checklist rollback switch: "Remove ELEVENLABS_API_KEY env var; adapter falls back to batch OpenAI TTS."
# commit + PR → merge to pipeline
```

**Wave 7 gate:**
```bash
git checkout pipeline && git pull
go build ./... && go vet ./...
go test -race ./internal/voice/...
go test -race ./internal/providers/...
DEEPGRAM_API_KEY=<key> ELEVENLABS_API_KEY=<key> \
  go run ./test/voicee2e/cmd/voicee2e/main.go \
  -gateway-url http://localhost:8833 \
  -suite test/voicee2e/scenarios/suite.yaml \
  -out test/voicee2e/reports/after-wave7.json
go run ./test/voicee2e/cmd/voicee2e/main.go \
  --compare \
  --prompt-a test/voicee2e/reports/baseline.json \
  --prompt-b test/voicee2e/reports/after-wave7.json
# Assert: 6/6 scenarios pass (functional gate — primary criterion for Wave 7)
# Assert: tts_ms p50 <= 100ms (default batch TTS)
#         tts_ms p50 <= 350ms when voice.synth_provider=elevenlabs
#         (ElevenLabs flash_v2_5 TTFB is structurally 260-329ms; 350ms allows 20ms variance)
# Assert: stt_ms p50 <= 100ms  (streaming Deepgram path)
# Informational (not a hard gate): e2e_ms p50
#   p50 is bimodal: speculative turns (~79ms) vs non-speculative turns (~1500ms+).
#   Cross-suite p50 is dominated by scenario mix; use per-scenario max_response_latency_ms
#   in suite.yaml for actionable latency gating instead.
# Assert: barge-in during streaming TTS leaves 0 goroutine leak (runtime.NumGoroutine check)
# Fixture invariant: see "Fixture Stability Invariants" note below.
```

> **Fixture Stability Invariants** (applies to all waves that run voicee2e)
>
> All WAV files in `test/voicee2e/fixtures/` must satisfy the following constraint to
> prevent EnergyVAD from splitting a single utterance into two turns mid-playback:
>
> **Max consecutive silence < 320 ms** (16 frames × 20 ms = `vadRedemptionFrames`)
>
> *Root cause (diagnosed Wave 7):* macOS `say` inserts natural pauses at commas and
> sentence-ending punctuation (~400–480 ms). This exceeds the 320 ms VAD redemption
> threshold, causing the VAD to close the first turn prematurely (capturing only the
> pre-pause fragment, e.g. "Hello,"). The fragment is transcribed as a near-homophone
> ("below.") with similarity 0.00, and the second speech burst registers as a barge-in.
>
> **Regeneration rule:** When editing `generate_fixtures.sh`, omit all commas and
> mid-sentence punctuation from TTS input strings. Regenerate with:
> ```bash
> bash test/voicee2e/fixtures/generate_fixtures.sh
> ```
>
> **Verification command** (run after any fixture change):
> ```python
> python3 - <<'EOF'
> import wave, struct, math, os, sys
> THRESHOLD = 0.02   # RMS below this is silence
> FRAME_MS  = 20     # EnergyVAD frame size
> LIMIT_MS  = 300    # must be < vadRedemptionFrames × FRAME_MS = 320ms
> base = "test/voicee2e/fixtures"
> fail = False
> for name in sorted(os.listdir(base)):
>     if not name.endswith(".wav"): continue
>     path = os.path.join(base, name)
>     with wave.open(path) as w:
>         rate, n = w.getframerate(), w.getnframes()
>         raw = w.readframes(n)
>     frame_sz = rate * FRAME_MS // 1000
>     rms = lambda s: math.sqrt(sum(x*x for x in s)/len(s)) if s else 0
>     samples = [struct.unpack_from('<h', raw, i)[0]/32768 for i in range(0,len(raw)-1,2)]
>     max_sil, cur_sil = 0, 0
>     for i in range(0, len(samples)-frame_sz, frame_sz):
>         if rms(samples[i:i+frame_sz]) < THRESHOLD:
>             cur_sil += FRAME_MS
>             max_sil = max(max_sil, cur_sil)
>         else:
>             cur_sil = 0
>     status = "FAIL" if max_sil >= LIMIT_MS else "ok"
>     if status == "FAIL": fail = True
>     print(f"{status:4s}  max_silence={max_sil:4d}ms  {name}")
> sys.exit(1 if fail else 0)
> EOF
> ```
>
> All fixtures must report `ok` (max_silence < 300 ms) before pushing a wave gate run.

---

### Wave 8 — L2.4 Context Window

```bash
# Agent-J: L2.4 Context window (after S2 decision + Wave 7 gate)
git checkout -b codex/l2-4-context-window pipeline
# [implement per S2 spike findings]
go test -race ./internal/gw/... -run TestContextPruning
go build ./...
# PR checklist rollback switch: "Set MaxContextTokens=0 in commitVoiceTurn() to disable pruning."
# commit + PR → merge to pipeline
```

**Wave 8 gate:**
```bash
git checkout pipeline && git pull
go build ./... && go vet ./...
go test -race ./internal/voice/...
go test -race ./internal/providers/...
go test -race ./internal/api/v1api/...
go test -race ./internal/gw/...

# Full final e2e comparison (all KPIs)
go run ./test/voicee2e/cmd/voicee2e/main.go \
  -gateway-url http://localhost:8833 \
  -suite test/voicee2e/scenarios/suite.yaml \
  -out test/voicee2e/reports/final.json
go run ./test/voicee2e/cmd/voicee2e/main.go \
  --compare \
  --prompt-a test/voicee2e/reports/baseline.json \
  --prompt-b test/voicee2e/reports/final.json

# Primary KPIs:
# - stt_ms p50 <= 100ms          (streaming Deepgram path)
# - tts_ms p50 <= 100ms          (default batch TTS)
# - tts_ms p50 <= 350ms          (when voice.synth_provider=elevenlabs;
#                                  ElevenLabs flash_v2_5 TTFB is 260-329ms structurally)
# - 6/6 scenarios pass           (functional gate; per-scenario thresholds in suite.yaml)
# - e2e_ms: INFORMATIONAL ONLY — not a hard gate.
#     Cross-suite p50 is bimodal (speculative turns ~79ms vs non-speculative ~1500ms+).
#     Document observed p50 in final.json for trend tracking.
# - 0 race reports

# Secondary KPIs (all from plan §Success Metrics):
# - false_speech_starts_per_session <= 0.5
# - spurious_barge_ins_per_session <= 0.2
# - premature_commits_per_session <= 0.3
# - stt_fallback_rate < 5%
# - turn_drop_rate < 10%
# - queue_overflow_rate < 1%
# - session_error_rate <= 0.5%

# P0 correctness counters:
# - provider_selection_nondeterminism_events = 0
# - feature_flag_divergence_events = 0

# Strategy regression (legacy = original baseline):
go run ./test/voicee2e/cmd/voicee2e/main.go \
  -gateway-url http://localhost:8833 \
  -suite test/voicee2e/scenarios/suite.yaml \
  -config '{"voice":{"turn_strategy":"legacy"}}' \
  -out test/voicee2e/reports/final-legacy.json
go run ./test/voicee2e/cmd/voicee2e/main.go \
  --compare \
  --prompt-a test/voicee2e/reports/baseline.json \
  --prompt-b test/voicee2e/reports/final-legacy.json
# Assert: 0 scenario regressions

# 10-minute soak:
# - heap growth < 50 MB
# - goroutine count at end <= start + 8
# - 0 Deepgram WebSocket reconnects
# - no "queue full" warnings in logs
```

---

### Wave 9 — DOCS Runbook + Changelog

```bash
# Agent-K: Documentation (can draft from Wave 7 onward; final merge after Wave 8)
git checkout -b codex/docs-runbook-update pipeline
# [write docs/runbook-voice-pipeline.md and docs/VOICE_PIPELINE_CHANGES.md]
# No code changes; no test commands required.
# Verify all rollback switches listed in runbook match config fields in code.
# commit + PR → merge to pipeline
```

---

## Quick Reference

### Branch naming

```
codex/s1-silero-spike
codex/s2-context-window-spike
codex/p0-1-deterministic-providers
codex/p0-2-feature-flags
codex/p0-3-functional-commit
codex/l1-3-observer-telemetry
codex/l1-1-silero-vad
codex/l1-4-client-audio-gate
codex/l1-2-deepgram-streaming-stt
codex/l2-2-turn-strategy
codex/l2-1-speculative-llm
codex/l2-3-streaming-tts
codex/l2-4-context-window
codex/docs-runbook-update
```

### Key files and their owners per wave (corrected L2 sequence)

| File | W0 | W1 | W2 | W3 | W4 | W5 | W6 | W7 | W8 | W9 |
|------|----|----|----|----|----|----|----|----|----|----|
| `providers/registry.go` | — | P0.1 | — | — | L1.2 | — | — | — | — | — |
| `providers/interface.go` | — | — | — | — | L1.2 | — | — | L2.3 | — | — |
| `providers/deepgram.go` | — | — | — | — | L1.2 | — | — | — | — | — |
| `providers/elevenlabs.go` | — | — | — | — | — | — | — | L2.3 | — | — |
| `voice/vad.go` | — | — | — | L1.1 | — | — | — | — | — | — |
| `voice/pipeline.go` | — | P0.2+P0.3 | L1.3 | L1.1 | L1.2 | L2.2 | L2.1 | L2.3 | L2.4 | — |
| `voice/session.go` | — | P0.2+P0.3 | L1.3 | L1.1 | L1.2 | L2.2 | L2.1 | — | — | — |
| `voice/gateway.go` | — | P0.2 | — | — | L1.2 | — | L2.1 | L2.3 | L2.4 | — |
| `voice/observer.go` | — | — | L1.3 | — | — | — | — | — | — | — |
| `voice/turn_strategy.go` | — | — | — | — | — | L2.2 | — | — | — | — |
| `voice/similarity.go` | — | — | — | — | — | — | L2.1 | — | — | — |
| `gw/gateway.go` | — | — | — | — | — | — | L2.1 | L2.3 | L2.4 | — |
| `web/src/hooks/` | — | — | — | L1.4 | — | — | — | — | — | — |
| `configs/` | — | P0.1 | — | — | — | L2.2 | — | — | — | — |
| `docs/` | S1+S2 | — | — | — | — | — | — | — | — | DOCS |

### Mandatory test command (run before every PR merge)

```bash
go test -race ./internal/voice/... && \
go test -race ./internal/providers/... && \
go build ./...
```

### Per-PR checklist (paste into every PR description)

```
## PR Checklist
- [ ] Changed behavior summary (one paragraph; what is different for a voice session operator)
- [ ] Before/after --compare output (or "no measurable impact" statement with justification)
- [ ] Rollback switch (config flag / env var / strategy fallback that reverts without code deploy)
```
