# Voice Pipeline Improvements: Engineering Plan (Level 1 + Level 2)

## 1. Purpose

This plan defines an implementation-ready roadmap for improving the current server-side voice pipeline in two phases:

1. Level 1: Reliability hardening and deterministic behavior in the existing architecture.
2. Level 2: Pipecat-style turn-management and observability patterns, still inside the current TeaNode Go architecture.

This document is written so another agent can implement it directly with minimal additional design work.

## 2. Goals

1. Eliminate nondeterministic behavior in STT/TTS provider selection.
2. Make negotiated voice features (`server_vad`, `server_turn`, `server_denoise`) actually control runtime behavior.
3. Make `voice.input.commit` functionally meaningful for push-to-talk and explicit client turn commit flows.
4. Improve interruption reliability and make interruption behavior configurable and testable.
5. Add first-class turn/latency observability for debugging and release gating.

## 3. Non-Goals

1. Full migration to Pipecat runtime.
2. Transport migration from WebSocket binary framing to WebRTC.
3. Replacing TeaNode conversation/tool orchestration.
4. Production infra changes unrelated to voice pipeline runtime behavior.

## 4. Current Baseline (for implementers)

Reference files:

- Voice session start/end RPC: `internal/api/v1api/rpc_voice.go`
- Voice frame schema: `internal/voice/binary.go`
- Voice session state: `internal/voice/session.go`
- Voice pipeline loops: `internal/voice/pipeline.go`
- Gateway voice adapters: `internal/gw/gateway.go`
- Provider registry lookup: `internal/providers/registry.go`
- Frontend voice hook: `web/src/hooks/useVoiceSession.ts`
- Call UX hook: `web/src/hooks/useVoiceCall.ts`

Known issues to address:

1. `server_vad`, `server_turn`, `server_denoise` are accepted but largely not enforced in runtime branching.
2. `voice.input.commit` currently emits an event but does not force pipeline turn finalization.
3. STT/TTS provider selection uses first-match map iteration, which can be nondeterministic.
4. Interruption trigger semantics are single-path and not strategy-driven.
5. Limited structured latency/turn metrics for e2e gates.

## 5. Delivery Strategy

Implement in two milestones with strict acceptance criteria. Level 1 must land before Level 2.

- Milestone A: Level 1 hardening (correctness first)
- Milestone B: Level 2 turn strategy + observability

## 6. Milestone A (Level 1): Reliability Hardening

### A1. Deterministic provider routing

Objective: remove nondeterministic STT/TTS provider choice.

Changes:

1. Add voice provider selection config fields:
- `voice.transcriber_provider` (optional string)
- `voice.synth_provider` (optional string)

2. Extend provider registry APIs to resolve by name with capability check:
- `FindTranscriberByName(name string)`
- `FindSynthesizerByName(name string)`

3. Update gateway voice provider adapter to use configured provider when set; fallback to existing discovery if unset.

4. Emit startup/runtime warnings if configured provider is missing or lacks required capability.

Files:

- `internal/configs/config.go`
- `internal/configs/schema.json`
- `internal/providers/registry.go`
- `internal/gw/gateway.go`
- `internal/voice/gateway.go` (if interface changes required)

Acceptance criteria:

1. Same configured provider is selected deterministically across runs.
2. Missing provider configuration yields clear warning and safe fallback.
3. Existing behavior remains unchanged when new config is omitted.

### A2. Feature-flag enforcement (`server_vad`, `server_turn`, `server_denoise`)

Objective: runtime behavior must reflect negotiated features.

Changes:

1. `ServerVAD=false` path:
- Do not run automatic VAD segmentation in `audioInputLoop`.
- Buffer inbound audio for explicit commit mode.

2. `ServerTurn=false` path:
- Do not auto-commit turns on VAD end.
- Require explicit commit trigger to finalize buffered speech.

3. `ServerDenoise` handling:
- Introduce a no-op denoise interface boundary now (preparation for future implementation).
- Ensure flag is visible in logs/events for traceability.

Files:

- `internal/voice/session.go`
- `internal/voice/pipeline.go`
- `internal/api/v1api/rpc_voice.go`
- `internal/api/v1api/frames.go`

Acceptance criteria:

1. Behavior differs observably between flag on/off modes.
2. Backward compatibility preserved with current default path (`true/true/true`).
3. No deadlocks in either auto-turn or explicit-turn modes.

### A3. Make `voice.input.commit` functional

Objective: explicit commit actually triggers turn transcription+commit.

Changes:

1. Add session-level explicit input buffer for commit mode.
2. On `voice.input.commit`:
- freeze current buffered audio as one turn,
- generate `turn_id`,
- run transcription pipeline,
- emit normal lifecycle events (`speech_ended` equivalent or explicit `input_committed`).

3. Add reason propagation from RPC `voice.input.commit.reason` into `turn.event` payload for diagnostics.

Files:

- `internal/voice/session.go`
- `internal/voice/pipeline.go`
- `internal/api/v1api/rpc_voice.go`
- `internal/api/v1api/frames.go`

Acceptance criteria:

1. In explicit-turn mode, speech is committed only after commit signal.
2. Commit creates user turn end-to-end (transcript -> run -> response).
3. Commit without buffered audio is handled gracefully with a clear event/reason.

### A4. Level 1 observability and metrics primitives

Objective: add durable metrics that support regression detection.

Changes:

1. Introduce canonical turn timeline event struct in voice package:
- `session_id`, `turn_id`, `stage`, `ts_ms`, `reason`, `queue_depth`, `run_id`, `response_id`.

2. Capture and emit latency checkpoints:
- speech_end -> transcript_final
- transcript_final -> response_started
- barge_in_triggered -> flush_sent

3. Add counters for:
- drop reasons
- queue overflows
- barge-in count
- commit mode usage

Files:

- `internal/voice/pipeline.go`
- `internal/voice/session.go`
- `internal/api/v1api/frames.go` (if payload schema extensions needed)
- `test/voicee2e/internal/report/report.go` (if metric ingestion update is needed)

Acceptance criteria:

1. All new metrics appear in logs/events during e2e runs.
2. Metrics cover interruption and queue paths.
3. Existing clients remain compatible with added optional fields.

## 7. Milestone B (Level 2): Turn Strategy + Observer Pattern

### B1. Turn strategy abstraction (Pipecat-style concept)

Objective: replace hardcoded interruption/turn policies with pluggable strategy.

Changes:

1. Add `TurnStrategy` interface:
- methods for speech start/end decisions,
- barge-in trigger decision,
- queue/commit prioritization.

2. Implement default strategy equivalent to current behavior (safe migration).

3. Implement improved strategy variant:
- debounce interruption triggers,
- minimum speech duration or confidence gates,
- optional backchannel filter (e.g., ignore tiny acknowledgments during TTS).

4. Select strategy via config:
- `voice.turn_strategy` (`legacy`, `balanced`, future values).

Files:

- `internal/voice/` (new files, e.g., `turn_strategy.go`, `turn_strategy_balanced.go`)
- `internal/voice/pipeline.go`
- `internal/configs/config.go`
- `internal/configs/schema.json`

Acceptance criteria:

1. Strategy can be switched without code changes.
2. Legacy strategy reproduces baseline behavior.
3. Balanced strategy measurably reduces false barge-ins without increasing missed interruptions.

### B2. Observer layer for turn/latency/idle

Objective: decouple telemetry from core loops and align with Pipecat-style observers.

Changes:

1. Introduce observer interfaces:
- `TurnObserver`
- `LatencyObserver`
- `IdleObserver`

2. Wire observer hooks into session lifecycle.

3. Implement default in-process observers:
- turn tracking observer,
- user-bot latency observer,
- idle detector (time since last user speech and last assistant response).

4. Emit structured events consumable by e2e harness and UI diagnostics.

Files:

- `internal/voice/` (new observer files)
- `internal/voice/session.go`
- `internal/voice/pipeline.go`
- optional UI surfacing: `web/src/hooks/useVoiceCall.ts`

Acceptance criteria:

1. Core pipeline remains functionally unchanged when observers are disabled.
2. Observer outputs match event timelines for real scenarios (S1-S6).
3. Idle detection supports configurable thresholds.

### B3. Interruption model refinement

Objective: make interruption behavior deterministic and tunable.

Changes:

1. Replace direct threshold check in `audioInputLoop` with strategy decision.
2. Add explicit interruption lifecycle states:
- `barge_in_candidate`
- `barge_in_triggered`
- `barge_in_suppressed` (with reason)

3. Ensure flush frame and run abort ordering are always preserved.
4. Add guard for stale late-arriving deltas from aborted runs.

Files:

- `internal/voice/pipeline.go`
- `internal/voice/session.go`
- `internal/voice/pipeline_test.go`

Acceptance criteria:

1. Interruption stop latency stays within target bounds.
2. No stale audio continues after confirmed barge-in.
3. Suppressed interruptions are explainable via reason codes.

## 8. Implementation Order (Task Queue)

Execute tasks in this order:

1. A1 deterministic provider routing.
2. A2 feature-flag runtime branching.
3. A3 functional `voice.input.commit`.
4. A4 Level 1 metrics/timeline.
5. B1 turn strategy abstraction + legacy strategy.
6. B2 observers (turn, latency, idle).
7. B3 balanced strategy + interruption refinements.
8. Documentation and runbook update.

## 9. Test Plan

### Unit tests

1. Provider selection:
- deterministic selection by name,
- fallback behavior,
- missing provider warnings.

2. Feature flags:
- VAD on/off behavior,
- server_turn on/off commit behavior,
- explicit commit with and without buffered audio.

3. Strategy:
- legacy path parity,
- balanced interruption gating,
- suppressed candidate reason correctness.

4. Observers:
- event emission completeness,
- latency computations,
- idle threshold transitions.

Target files:

- `internal/providers/registry_test.go`
- `internal/voice/session_test.go`
- `internal/voice/pipeline_test.go`
- new observer/strategy test files under `internal/voice/`

### Integration tests

1. `internal/api/v1api/rpc_voice_test.go`:
- commit flow in explicit-turn mode,
- payload compatibility for new optional fields.

2. Voice e2e scenarios (existing harness):
- S1-S6 pass,
- interruption metrics present,
- queue overflow/drop reasons captured.

### Commands

1. `go build ./...`
2. `go vet ./...`
3. `go test -race ./internal/voice/...`
4. `go test ./internal/providers/...`
5. `go test ./internal/api/v1api/...`
6. `make test-voice-e2e-smoke`

## 10. Rollout Plan

### Phase 1: Dark launch toggles (Level 1)

1. Ship config fields disabled/default-safe.
2. Enable deterministic provider selection in staging.
3. Enable explicit commit mode tests in staging only.
4. Validate metrics and compare to baseline.

### Phase 2: Level 1 GA

1. Enable Level 1 defaults for all voice sessions.
2. Monitor regression dashboard for:
- dropped turn rate,
- interruption stop latency,
- response start latency.

### Phase 3: Level 2 canary

1. Enable observer layer and balanced strategy for canary sessions.
2. Compare against legacy strategy control cohort.
3. Roll forward only if interruption reliability and latency do not regress.

## 11. Risks and Mitigations

1. Risk: Behavior regressions from new branching logic.
- Mitigation: preserve legacy default strategy and add mode-specific tests.

2. Risk: Increased complexity in session state.
- Mitigation: isolate strategy/observer modules; keep core loops small.

3. Risk: Client compatibility issues with event payload changes.
- Mitigation: additive payload fields only; avoid breaking envelope schema.

4. Risk: Flaky interruption tests due to timing jitter.
- Mitigation: use tolerance windows and timeline checkpoints instead of exact timestamps.

## 12. Definition of Done

Level 1 is done when:

1. Provider routing is deterministic/configurable.
2. Feature flags alter runtime behavior as designed.
3. `voice.input.commit` completes an actual turn in explicit-turn mode.
4. Level 1 metrics are emitted and validated in smoke e2e runs.

Level 2 is done when:

1. Strategy abstraction is in place with at least `legacy` and `balanced` modes.
2. Observer outputs (turn/latency/idle) are integrated and test-covered.
3. Balanced interruption strategy improves false interruption rate without hurting missed interruption rate.
4. S1-S6 voice e2e scenarios pass under default strategy.

## 13. Handoff Notes for Implementing Agent

1. Do not change transport protocol framing unless explicitly required.
2. Keep all new JSON fields optional and backward compatible.
3. Avoid touching unrelated gateway event broadcasting behavior.
4. Prefer incremental PRs aligned with Section 8 task queue.
5. For each PR, include:
- changed behavior summary,
- before/after metrics from smoke suite,
- explicit rollback switch (config or strategy fallback).
