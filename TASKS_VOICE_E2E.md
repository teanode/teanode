# Voice E2E Automation Tasks

Derived from `/Users/hao/Projects/teanode-sync-voice-text/docs/ENGINEERING_PLAN_VOICE_E2E_AUTOMATION.md`.

## Execution Rules

- Branch naming: `codex/voice-<TASK_ID>-<short-name>`
- Do not start blocked tasks before dependencies are complete.
- After each task: run acceptance checks, commit, and report:
  - changed files
  - test results
  - blockers
- Prefer deterministic checks and machine-readable outputs.

## Dependency Graph (High Level)

- Foundation: `E1`
- Protocol + fixtures: `E2`, `E3` (depend on `E1`)
- Scenario runner + assertions: `E4`, `E5` (depend on `E2`,`E3`)
- Interruption checks: `E6` (depends on `E4`,`E5`)
- Prompt experiment loop: `E7` (depends on `E5`)
- CI + docs + signoff: `E8`, `E9` (depend on `E6`,`E7`)

## Lane A: Harness Foundation

- [x] `E1` Create voice E2E harness skeleton
  - Depends on: none
  - Scope:
    - Add `test/voicee2e/` structure:
      - `cmd/voicee2e/main.go`
      - `internal/runner/`
      - `internal/protocol/`
      - `internal/report/`
    - Implement config loading and CLI flags:
      - `--scenario`, `--suite`, `--gateway-url`, `--out`
    - Add baseline report JSON schema.
  - Acceptance checks:
    - `go build ./test/voicee2e/cmd/voicee2e`
    - `go test ./test/voicee2e/...`

- [x] `E2` Implement websocket voice protocol driver
  - Depends on: `E1`
  - Scope:
    - Implement `voice.start`, `voice.end`, binary frame send/receive.
    - Reuse existing binary wire format from `internal/voice/binary.go`.
    - Capture timeline markers:
      - speech_started, speech_ended, transcript.final
      - turn_queued, turn_committed, turn_dropped
      - response.started, response.completed, barge_in_triggered
  - Acceptance checks:
    - `go test ./test/voicee2e/... -run TestProtocol`
    - Local dry-run against gateway without panic/hang.

## Lane B: Fixtures + Scenario Definitions

- [x] `E3` Add synthetic audio fixtures and scenario specs
  - Depends on: `E1`
  - Scope:
    - Add fixture index and metadata under `test/voicee2e/fixtures/`.
    - Add scenario files under `test/voicee2e/scenarios/` for:
      - S1 short
      - S2 medium
      - S3 long
      - S4 multi-turn
      - S5 barge-in
      - S6 rapid interruption
    - Define expected transcript targets and timing bounds per scenario.
  - Acceptance checks:
    - `go test ./test/voicee2e/... -run TestScenarioLoad`
    - Fixture/schema validation passes.

## Lane C: Assertions, Metrics, and Reports

- [x] `E4` Implement lifecycle assertions and latency metrics
  - Depends on: `E2`,`E3`
  - Scope:
    - Assert required lifecycle ordering and completeness.
    - Compute:
      - speech_end -> transcript.final
      - turn_committed -> response.started
      - response.started -> response.completed
    - Emit failure reason codes.
  - Acceptance checks:
    - `go test ./test/voicee2e/... -run TestLifecycleAssertions`
    - `go test ./test/voicee2e/... -run TestLatencyMetrics`

- [x] `E5` Add transcription, relevance, and pacing scoring
  - Depends on: `E2`,`E3`
  - Scope:
    - Transcription similarity scoring against expected text.
    - Pacing checks:
      - sentence count limits
      - duration bounds
    - Relevance scoring framework (rule-based first; pluggable judge later).
  - Acceptance checks:
    - `go test ./test/voicee2e/... -run TestScoring`
    - Scenario report contains per-metric scores.

- [x] `E6` Implement interruption correctness checks
  - Depends on: `E4`,`E5`
  - Scope:
    - Detect barge-in trigger during active response.
    - Verify stop timing budget (`barge_in -> response audio stop`).
    - Verify interruption turn is committed and answered next.
  - Acceptance checks:
    - `go test ./test/voicee2e/... -run TestInterruption`
    - S5/S6 pass on local gateway with stable thresholds.

## Lane D: Prompt Iteration and Automation Integration

- [x] `E7` Add prompt variant A/B evaluation support
  - Depends on: `E5`
  - Scope:
    - Add `test/voicee2e/prompts/` and variant loading.
    - Run matrix with Prompt A vs Prompt B.
    - Produce delta report (relevance, pacing, latency, interruption regressions).
  - Acceptance checks:
    - `go test ./test/voicee2e/... -run TestPromptCompare`
    - CLI supports compare mode with clear summary output.

- [x] `E8` Add make targets and CI workflow entrypoints
  - Depends on: `E6`,`E7`
  - Scope:
    - `make voice-e2e-smoke`
    - `make voice-e2e`
    - `make voice-e2e-compare PROMPT_A=... PROMPT_B=...`
    - Add CI config for smoke on PR and full on scheduled/nightly.
  - Acceptance checks:
    - `make voice-e2e-smoke` runs end-to-end.
    - CI workflow validates locally (linted YAML / dry-run where possible).

- [x] `E9` Add runbook and final signoff checklist
  - Depends on: `E6`,`E7`
  - Scope:
    - Add `/docs/VOICE_E2E_RUNBOOK.md`:
      - setup
      - run commands
      - how to read failures
      - tuning workflow
    - Add signoff checklist mapped to S1-S6.
  - Acceptance checks:
    - Runbook commands are copy-paste valid.
    - One baseline report attached in `test/voicee2e/reports/`.

## Parallel Execution Suggestion

- Subagent 1: `E1` -> `E2`
- Subagent 2: `E3` (can run after `E1`)
- Subagent 3: `E4` + `E5` (after `E2`,`E3`)
- Subagent 4: `E6` + `E7` (after dependencies)
- Subagent 5: `E8` + `E9` (after `E6`,`E7`)

## Merge Order

1. `E1`
2. `E2`, `E3` (parallel)
3. `E4`, `E5` (parallel)
4. `E6`
5. `E7`
6. `E8`, `E9`

## Global Verification Suite (Before Closing E9)

- `go build ./...`
- `go test ./...`
- `go test -race ./internal/voice/...`
- `go test ./test/voicee2e/...`
- `cd web && ./node_modules/.bin/tsc --noEmit`
