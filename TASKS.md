# Server-Side Voice Pipeline Tasks

This task plan is derived from `/Users/hao/Projects/teanode-sync-voice-text/docs/ENGINEERING_PLAN.md` and organized for parallel execution with subagents.

## Execution Rules

- Use task IDs in branch/PR names: `codex/voice-<TASK_ID>-<short-name>`.
- Keep changes scoped to listed files per task.
- Do not start blocked tasks until dependencies are marked done.
- Every completed task must include tests from its acceptance criteria.

## Open Decision Gate (Must Resolve First)

- [x] `GATE-1` Resolve Phase 1 `audio_out` codec behavior (`pcm_s16le` true PCM vs allow `mp3` passthrough).
  - Why: plan contains a contradiction that affects backend adapter + frontend playback path.
  - Decision output: one written choice plus file-level impact list.
  - Blocks: `B5`, `F3`, `F4`, `T4`, `T5`.

## Parallel Lanes

## Lane A: Core Voice Package (`internal/voice`)

- [x] `A1` Create package skeleton and types (`gateway.go`, `session.go`, `pipeline.go`, `vad.go`, `binary.go`, `audio.go`, `sentences.go`).
  - Depends on: none
  - Files:
    - `/Users/hao/Projects/teanode-sync-voice-text/internal/voice/gateway.go`
    - `/Users/hao/Projects/teanode-sync-voice-text/internal/voice/session.go`
    - `/Users/hao/Projects/teanode-sync-voice-text/internal/voice/pipeline.go`
    - `/Users/hao/Projects/teanode-sync-voice-text/internal/voice/vad.go`
    - `/Users/hao/Projects/teanode-sync-voice-text/internal/voice/binary.go`
    - `/Users/hao/Projects/teanode-sync-voice-text/internal/voice/audio.go`
    - `/Users/hao/Projects/teanode-sync-voice-text/internal/voice/sentences.go`
  - Acceptance:
    - All files compile with placeholder-safe logic.
    - `Session` concurrency primitives present (`sync.Once`, `sync.RWMutex`, `atomic.Uint64`).

- [x] `A2` Implement binary codec (`uint48` seq, 18-byte header, parse/encode validation).
  - Depends on: `A1`
  - Acceptance:
    - Round-trip encode/decode passes deterministic fixtures.
    - Invalid magic/frame/header length rejected.

- [x] `A3` Implement VAD engine and frame-state transitions.
  - Depends on: `A1`
  - Acceptance:
    - Threshold/hysteresis behavior matches plan (`5` start, `12` redemption).
    - RMS computed from s16le normalized samples.

- [x] `A4` Implement sentence extraction + abbreviation exceptions + flush remainder.
  - Depends on: `A1`
  - Acceptance:
    - Incremental extraction API works with `alreadyEnqueued`.
    - Abbreviation map implemented as specified.

- [x] `A5` Implement PCM-to-WAV wrapper.
  - Depends on: `A1`
  - Acceptance:
    - 44-byte WAV header correctness validated by unit tests.

- [x] `A6` Implement session lifecycle and 4-goroutine pipeline with non-blocking output enqueue.
  - Depends on: `A1`, `A2`, `A3`, `A4`, `A5`
  - Acceptance:
    - `Start()` launches loops; `Close()` idempotent.
    - `audioInputLoop` never blocks on downstream channels.
    - Barge-in path cancels run/TTS and emits flush.

## Lane B: Gateway + API Wiring (Go backend)

- [x] `B1` Extend voice/control frame types in `/internal/api/v1api/frames.go`.
  - Depends on: none
  - Acceptance:
    - Adds `voiceEnvelope` + payload structs without changing existing frame structs.
    - Adds `validateVoiceAudioFormats` enforcing Phase 1 constraints.

- [x] `B2` Add voice RPC handlers in `/internal/api/v1api/rpc_voice.go`.
  - Depends on: `B1`, `A1`
  - Acceptance:
    - Supports `voice.start`, `voice.end`, `voice.response.cancel`, `voice.input.commit`.
    - Returns expected error codes (`400`, `404`, `409`, `500`) per plan.

- [x] `B3` Update websocket connection handling for binary frames + single active session pointer.
  - Depends on: `B1`, `B2`, `A2`, `A6`
  - Files:
    - `/Users/hao/Projects/teanode-sync-voice-text/internal/api/v1api/websocket.go`
  - Acceptance:
    - Handles `websocket.BinaryMessage`.
    - Cleans up active session on disconnect.
    - Dispatches new voice methods in request switch.

- [x] `B4` Extend gateway interface + implement voice adapters/bridges.
  - Depends on: `A1`
  - Files:
    - `/Users/hao/Projects/teanode-sync-voice-text/internal/gw/gw.go`
    - `/Users/hao/Projects/teanode-sync-voice-text/internal/gw/gateway.go`
  - Acceptance:
    - `StartVoiceSession` implemented and returns `*voice.Session`.
    - Subscriber bridge map supports subscribe/unsubscribe symmetry.

- [x] `B5` Implement synthesizer adapter output semantics per `GATE-1`.
  - Depends on: `B4`, `GATE-1`
  - Acceptance:
    - Codec behavior matches resolved decision.
    - TODO comments/documentation updated where necessary.

## Lane C: Frontend Voice Transport + Hook Migration

- [x] `F1` Add binary send/receive API to `/web/src/rpc.ts`.
  - Depends on: none
  - Acceptance:
    - Module-level `sendBinary` and `onBinaryMessage` introduced.
    - `ArrayBuffer`/`Blob` handling occurs before JSON parsing.

- [x] `F2` Re-export binary API from `/web/src/hooks/useWebSocket.ts`.
  - Depends on: `F1`
  - Acceptance:
    - Hook return shape includes `{ sendRpc, sendBinary, onBinaryMessage }`.

- [x] `F3` Implement `/web/src/hooks/useVoiceSession.ts` (new).
  - Depends on: `F1`, `F2`, `GATE-1`
  - Acceptance:
    - Encodes input frames to spec (magic/type/seq/ts/duration/payload).
    - Handles flush (`0x03`) by stopping current playback and queue.
    - Reads `result.session_id` from `voice.start` response.

- [x] `F4` Migrate `/web/src/hooks/useVoiceCall.ts` to thin adapter over `useVoiceSession`.
  - Depends on: `F3`
  - Acceptance:
    - Removes VAD, sentence splitting, streaming TTS coupling.
    - Preserves existing exported interface fields; adds `sendBinary` + `onBinaryMessage` options.
    - Keeps UX behavior (chimes, mute, call duration, wake lock, AudioContext user-gesture safety).

- [x] `F5` Mark `/web/src/hooks/useStreamingTTS.ts` deprecated (JSDoc only).
  - Depends on: none
  - Acceptance:
    - Only top-level deprecation comment added; no logic changes.

- [x] `F6` Update conversation route integration to pass new voice hook dependencies.
  - Depends on: `F2`, `F4`
  - Files:
    - `/Users/hao/Projects/teanode-sync-voice-text/web/src/routes/conversations/$agentId/route.tsx`
  - Acceptance:
    - `useVoiceCall` receives `sendBinary` and `onBinaryMessage`.

## Lane D: Tests + Verification + Integration

- [x] `T1` Add `internal/voice` tests (`binary_test.go`, `vad_test.go`, `session_test.go`).
  - Depends on: `A2`, `A3`, `A6`
  - Acceptance:
    - Includes race-focused coverage for concurrent state and barge-in non-blocking guarantees.

- [x] `T2` Add `rpc_voice` tests in `/internal/api/v1api/rpc_voice_test.go`.
  - Depends on: `B2`, `B3`
  - Acceptance:
    - Covers second start `409`, invalid codec `400`, end-without-session `404`.

- [x] `T3` TypeScript compile/integration checks.
  - Depends on: `F4`, `F6`
  - Acceptance:
    - `npx tsc --noEmit` passes from `/web`.

- [x] `T4` Protocol grep checks from plan.
  - Depends on: `F3`, `F4`, `GATE-1`
  - Acceptance:
    - `decodeAudioData` presence/absence matches resolved `GATE-1`.
    - `useStreamingTTS` removed from `useVoiceCall.ts`.
    - `session_id` usage verified in `useVoiceSession.ts`.

- [x] `T5` Full verification command suite.
  - Depends on: `T1`, `T2`, `T3`, `B5`
  - Commands:
    - `go build ./...`
    - `go vet ./...`
    - `go test -race ./internal/voice/...`
    - `go test ./internal/api/v1api/...`
    - `cd web && npx tsc --noEmit`

- [ ] `T6` Manual end-to-end validation checklist execution.
  - Depends on: `T5`
  - Acceptance:
    - Confirms session lifecycle, VAD turn events, transcript, response lifecycle, barge-in flush, single-active-session `409`, envelope shape.
  - Current status:
    - Blocked on manual validation pending stable local behavior for long-form topic relevance and interruption UX.

## Suggested Subagent Assignment

- Subagent 1: Lane A (`A1`-`A6`)
- Subagent 2: Lane B (`B1`-`B5`)
- Subagent 3: Lane C (`F1`-`F6`)
- Subagent 4: Lane D (`T1`-`T6`) starts early with scaffolding, finishes after merges

## Merge Order

1. `A1`, `B1`, `F1` (foundation)
2. `A2`-`A5`, `B4`, `F2`, `F5` (independent mid-layer)
3. `GATE-1` decision
4. `A6`, `B2`, `B3`, `F3`, `F4`, `F6`
5. `B5`
6. `T1`-`T6`
