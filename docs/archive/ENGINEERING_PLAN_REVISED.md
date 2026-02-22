# Server-Side Voice Pipeline: Revised Engineering Plan

## 1. Purpose

This revised plan addresses behavioral regressions observed after migrating from the original client-side voice loop to the server-side voice pipeline.

Target user-visible behavior to restore:
1. User speaks and sees their utterance appear as a user message.
2. Assistant response text streams in the UI while audio is spoken.
3. If user interrupts, assistant audio stops immediately.
4. The interruption utterance is not lost; it is transcribed, committed, and answered next.

## 2. Confirmed Current Gaps

### G1. Turn loss before commit
Current pipeline drops turns due to strict thresholds and early filters:
- Audio too short (`minCommittedTurnBytes`)
- Transcript empty
- Transcript too short (`minCommittedTextRunes`)

Impact: User spoke, but no user message appears.

### G2. `run in flight` causes valid turns to be discarded
Current rule drops transcripts whenever a run is active.

Impact: Interruption speech can be ignored instead of queued/committed.

### G3. Interruption semantics weaker than original client behavior
Original behavior interrupted on speech start while assistant run was active.
Current server behavior only triggers barge-in when response playback state is active.

Impact: User starts speaking but agent may continue talking/responding from stale run.

### G4. Missing voice-mode instruction parity
Original client path always sent voice-specific prompt guidance with utterances.
Current server path sends plain transcribed text without voice-mode suffix.

Impact: Responses can be less conversational, longer, and topic-shift prone.

### G5. VAD calibration mismatch
Server VAD thresholds and frame counts diverge from previously tuned client behavior.

Impact: Over-segmentation into micro-turns and unstable turn boundaries.

### G6. Lifecycle visibility and determinism
Voice events are present, but there is no strict lifecycle contract for:
- what happens to late transcripts during active run,
- when turns are queued vs dropped,
- and how many pending turns are retained.

Impact: hard-to-predict interaction flow under rapid speech/interruption.

## 3. Revised Functional Requirements

### FR1. No silent loss of meaningful user speech
If speech ends and transcription yields non-empty text above minimal threshold, it must either:
- be committed immediately, or
- be queued for later commit,
- never silently discarded due to active run.

### FR2. Deterministic interruption model
On user speech start during assistant run/response:
1. Trigger barge-in immediately.
2. Abort active run.
3. Flush audio output.
4. Keep capturing interruption turn and process it next.

### FR3. Single-run + bounded turn queue model
Per voice session:
- Max one active run.
- FIFO queue for pending transcribed turns (bounded, e.g. 3).
- Overflow policy: drop oldest uncommitted pending turn with explicit log/event.

### FR4. Voice-mode prompt parity
Every committed voice turn must apply voice interaction prompt suffix equivalent to previous client behavior.

### FR5. Strong observability with low noise
INFO logs provide concise lifecycle trace:
- speech_started
- speech_ended
- transcript.final
- turn queued/committed/dropped
- run started/aborted/final
- response started/completed/flushed

Per-frame logs remain DEBUG-only and throttled.

## 4. Revised State Machine

Session-level states (conceptual):
- `IDLE` (no active run, no pending response)
- `LISTENING`
- `RUN_ACTIVE`
- `RESPONDING`

Turn-level states:
- `CAPTURING`
- `TRANSCRIBED`
- `QUEUED`
- `COMMITTED`
- `DROPPED` (with reason)

Rules:
1. `speech_started` creates new `turn_id` and enters `CAPTURING`.
2. `speech_ended` finalizes capture and schedules transcription.
3. If transcript qualifies and no run active: commit immediately.
4. If transcript qualifies and run active: enqueue turn (`QUEUED`) instead of ignore.
5. On run terminal (`final/error/aborted`): dequeue next pending turn and commit.
6. On barge-in trigger: abort active run, flush response, then process queued/next turn.

## 5. Architecture Changes

### 5.1 `internal/voice/session.go`
Add session-scoped pending turn queue and lifecycle metadata:
- `pendingTurns []PendingTurn`
- `maxPendingTurns int` (default 3)
- synchronized helpers:
  - `EnqueuePendingTurn(turnID, text)`
  - `DequeuePendingTurn()`
  - `HasPendingTurns()`
  - `DropOldestPendingTurn(reason)`

Define `PendingTurn`:
- `turnID string`
- `text string`
- `createdAt time.Time`

### 5.2 `internal/voice/pipeline.go`
Replace `run in flight => ignore` behavior:
- If run active, enqueue qualifying transcript instead of dropping.
- Emit `turn.event` payload for queued/dropped reasons.

Add commit drain on terminal run events:
- In `llmEventForwarder` when state is terminal:
  - clear current run
  - drain one pending turn and commit

Strengthen barge-in trigger condition:
- Trigger on speech start when either:
  - `currentRunID != ""`, or
  - `currentResponseID != ""`

Apply voice-mode prompt on commit:
- `SendMessage` with `SystemPromptSuffix` equivalent to legacy `VOICE_CALL_PROMPT`.

### 5.3 `internal/gw/gateway.go`
No API shape change required, but ensure aborted run emits terminal event reliably.
Maintain event delivery guarantees for terminal states.

### 5.4 `web/src/hooks/useVoiceSession.ts`
Keep transport role only; no client-side VAD/TTS orchestration return.
Optional enhancement:
- handle explicit `turn.event` queued/dropped payloads to show UX hints.

## 6. Protocol Additions (Voice Envelope Payload)

Extend `turn.event` payload with optional fields:
- `reason`:
  - `queued_run_active`
  - `dropped_queue_overflow`
  - `dropped_too_short_audio`
  - `dropped_too_short_text`
  - `dropped_empty_transcript`
- `queue_depth` (int)

This is backward-compatible with existing envelope/event handling.

## 7. VAD and Turning Calibration Plan

Introduce explicit config constants grouped in one section:
- start threshold
- end threshold
- min speech frames
- redemption frames
- min committed bytes
- min committed text runes

Add calibration matrix tests:
1. normal speech phrase
2. short acknowledgement ("yeah", "okay")
3. interruption during assistant speech
4. background noise burst

Goal: allow brief but meaningful interruptions without flooding micro-turns.

## 8. Logging and Diagnostics

INFO-level canonical flow line format:
- `voice flow: <session> <turn> <stage> key=value ...`

Required stages:
- `turn_start`
- `turn_end`
- `transcribe_ok`
- `turn_queued`
- `turn_commit`
- `turn_drop`
- `run_abort`
- `response_start`
- `response_flush`
- `response_done`

Keep per-frame audio and LLM delta logs at DEBUG.

## 9. Test Plan (Revised)

### Unit Tests

#### `internal/voice/session_test.go`
- queue enqueue/dequeue ordering
- queue overflow drop policy
- concurrent queue access (`-race`)

#### `internal/voice/pipeline_test.go` (new)
- active run + transcript => queued (not dropped)
- terminal run => queued turn committed next
- barge-in while run active aborts and flushes
- short audio/text drop emits correct reason

#### `internal/api/v1api/rpc_voice_test.go`
- unchanged existing tests
- optional: assert new `turn.event` payload fields are accepted structurally

### Integration Tests
- Simulated event stream with two turns where second starts during first response:
  - verify first aborted
  - verify second committed and answered

### Command Suite
1. `go build ./...`
2. `go vet ./...`
3. `go test -race ./internal/voice/...`
4. `go test ./internal/api/v1api/...`
5. `cd web && ./node_modules/.bin/tsc --noEmit`

## 10. Rollout Plan

### Phase R1: Correctness-first
- Implement queue + commit-after-terminal logic.
- Implement stronger barge-in trigger.
- Reintroduce voice-mode prompt suffix.

### Phase R2: Calibration
- Tune VAD and minimum thresholds against manual scripts.

### Phase R3: UX polish
- Optional UI hints for queued/dropped turns.
- Optional metrics counters for drop reasons.

## 11. Acceptance Criteria

A run is accepted only if all are true:
1. Interruption utterances are not lost under normal speech conditions.
2. No `transcript ignored (run in flight)` for qualifying speech; replaced by queued behavior.
3. Assistant audio always flushes within one turn-start after barge-in.
4. User and assistant text sequencing remains coherent in conversation timeline.
5. Observed relevance improves due to voice-mode prompt suffix restoration.
6. Existing non-voice websocket/RPC behavior unchanged.

## 12. Implementation Task Breakdown (Revised)

- `R1` Add pending-turn queue primitives in `session.go`.
- `R2` Replace run-in-flight drop with queue + event reasons in `pipeline.go`.
- `R3` Drain queued turn on run terminal events.
- `R4` Barge-in trigger on active run OR active response.
- `R5` Add voice-mode prompt suffix to voice commits.
- `R6` Add/adjust tests for queueing + interruption determinism.
- `R7` VAD + thresholds calibration pass.
- `R8` Manual end-to-end verification checklist run and signoff.

