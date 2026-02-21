# Server-Side Voice Pipeline Revised Tasks

Derived from `/Users/hao/Projects/teanode-sync-voice-text/docs/ENGINEERING_PLAN_REVISED.md`.

## Execution Rules

- Branch naming: `codex/voice-<TASK_ID>-<short-name>`
- Do not start blocked tasks before dependencies are complete.
- After each task: run acceptance checks, commit, and report changed files/tests/blockers.
- Keep non-voice behavior unchanged.

## Dependency Graph (High Level)

- Foundation: `R1`
- Core behavior: `R2`, `R4`, `R5` (depends on `R1`)
- Lifecycle drain: `R3` (depends on `R2`)
- Validation: `R6` (depends on `R2`,`R3`,`R4`,`R5`)
- Tuning: `R7` (depends on `R6` baseline green)
- Manual signoff: `R8` (depends on `R6`,`R7`)

## Lane A: Session + Queue Primitives

- [x] `R1` Add pending-turn queue primitives to `internal/voice/session.go`
  - Depends on: none
  - Scope:
    - Add `PendingTurn` struct and queue fields.
    - Add synchronized helpers: enqueue/dequeue/has/drop-oldest.
    - Ensure no race regressions with existing session state.
  - Acceptance checks:
    - `go test -race ./internal/voice/...`
    - New/updated unit tests validate FIFO + overflow behavior.

## Lane B: Pipeline Behavior Corrections

- [x] `R2` Replace `run in flight => ignore` with queue behavior in `internal/voice/pipeline.go`
  - Depends on: `R1`
  - Scope:
    - Qualifying transcript while run active must enqueue, not drop.
    - Emit `turn.event` with reason + queue depth for queued/dropped outcomes.
    - Keep dedupe protections for duplicate transcribe commits.
  - Acceptance checks:
    - Unit tests for queued-on-active-run path.
    - Confirm no qualifying path logs `transcript ignored (run in flight)`.

- [x] `R3` Drain queued turn on terminal run events in `llmEventForwarder`
  - Depends on: `R2`
  - Scope:
    - On `final/error/aborted`: clear current run and commit next queued turn.
    - Preserve terminal event robustness under queue saturation.
  - Acceptance checks:
    - Tests proving queued turn commits after run terminal state.

- [x] `R4` Strengthen barge-in trigger semantics
  - Depends on: `R1`
  - Scope:
    - Trigger barge-in on speech start when `currentRunID != ""` OR `currentResponseID != ""`.
    - Keep non-blocking flush + abort behavior.
  - Acceptance checks:
    - Tests verifying run abort + flush on interruption during active run.

- [x] `R5` Restore voice-mode prompt suffix on server-side commits
  - Depends on: `R1`
  - Scope:
    - Apply voice-specific system prompt suffix when sending transcribed voice turns.
    - Keep prompt scoped to voice session path only.
  - Acceptance checks:
    - Tests or assertions that `SendMessage` receives non-empty `SystemPromptSuffix` for voice commits.

## Lane C: Testing + Validation

- [x] `R6` Add/expand tests for queueing and interruption determinism
  - Depends on: `R2`,`R3`,`R4`,`R5`
  - Scope:
    - `internal/voice/session_test.go`: queue ordering/overflow/concurrency.
    - New `internal/voice/pipeline_test.go`: queued-on-active-run, terminal-drain, barge-in flush, drop reasons.
    - Keep existing `rpc_voice` tests passing.
  - Acceptance checks:
    - `go test -race ./internal/voice/...`
    - `go test ./internal/api/v1api/...`

- [x] `R7` VAD + thresholds calibration pass
  - Depends on: `R6`
  - Scope:
    - Tune VAD and min-turn thresholds for fewer micro-turns while preserving interruption responsiveness.
    - Add calibration-oriented tests for short acknowledgements and noise bursts.
  - Acceptance checks:
    - `go test -race ./internal/voice/...`
    - Review logs for reduced empty/too-short drops during scripted manual sample runs.

- [ ] `R8` Manual end-to-end signoff checklist
  - Depends on: `R6`,`R7`
  - Scope:
    - Validate sequence:
      1. speech -> user text appears
      2. assistant text streams + audio plays
      3. interruption stops assistant promptly
      4. interruption utterance appears as next user message
      5. assistant responds to interruption utterance
    - Validate no regressions on session start/end and single-active-session conflict.
  - Acceptance checks:
    - Manual checklist recorded with pass/fail notes and logs.

## Parallel Execution Suggestion

- Subagent 1: `R1`
- Subagent 2: `R2` + `R3` (after `R1`)
- Subagent 3: `R4` + `R5` (after `R1`)
- Subagent 4: `R6` + `R7` + `R8` (after dependencies)

## Merge Order

1. `R1`
2. `R2`, `R4`, `R5` (parallel)
3. `R3`
4. `R6`
5. `R7`
6. `R8`

## Global Verification Suite (Run before closing R8)

- `go build ./...`
- `go vet ./...`
- `go test -race ./internal/voice/...`
- `go test ./internal/api/v1api/...`
- `cd web && ./node_modules/.bin/tsc --noEmit`
