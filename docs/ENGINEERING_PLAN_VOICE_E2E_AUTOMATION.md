# Voice Agent E2E Automation Plan

## 1. Objective

Build an automated end-to-end (E2E) test system that validates real voice-agent behavior and supports iterative improvement without manual testing.

Target outcomes:
1. Synthetic spoken input is sent through the same voice pipeline as real users.
2. Multiple utterance lengths and conversation styles are covered.
3. Response timing, pacing, and conversational quality are measured.
4. Interruption (barge-in) behavior is verified automatically.
5. Prompt variants can be evaluated and improved iteratively.

## 2. Scope

In scope:
- Gateway voice session path (`voice.start`, binary audio in/out, turn/response events, `voice.end`).
- Transcription quality checks from synthetic speech.
- Assistant response relevance and pacing checks.
- Barge-in stop behavior checks.
- Prompt tuning loop with measurable improvements.

Out of scope (phase 1):
- Human MOS evaluations in production traffic.
- Full browser automation of UI rendering (covered by protocol-level checks first).

## 3. Test Architecture

### 3.1 Core idea

Add a Go E2E harness that acts as a voice client:
1. Opens a websocket session against local gateway.
2. Sends `voice.start`.
3. Streams PCM frames generated from test audio fixtures.
4. Captures server events and outbound audio frames.
5. Produces a structured pass/fail report per scenario.

### 3.2 Components

1. `test/voicee2e/runner` (Go)
- Scenario loader (YAML/JSON).
- Session driver (rpc + binary framing).
- Timeline collector (events, timestamps, turn IDs, run IDs).
- Assertions and score output.

2. `test/voicee2e/fixtures`
- Canonical voice fixtures for:
  - short utterances
  - medium utterances
  - long utterances
  - multi-sentence speech
  - interruption speech clips

3. `test/voicee2e/scenarios`
- Declarative scenario specs with expected constraints.

4. `test/voicee2e/reports`
- JSON summary + markdown report with regressions and deltas.

## 4. Scenario Matrix

### S1. Single short utterance
- Input: one short sentence (5-10 words).
- Verify:
  - transcript emitted and non-empty
  - one committed turn
  - assistant starts and completes response
  - response latency under threshold

### S2. Single medium utterance
- Input: 1-2 full sentences.
- Verify:
  - transcript semantic match above threshold
  - assistant response on-topic
  - response pacing (target sentence count + duration bounds)

### S3. Long utterance
- Input: 3-5 sentences continuous speech.
- Verify:
  - does not split excessively into micro-turns
  - commits expected number of turns (configurable tolerance)
  - no dropped qualifying transcript

### S4. Multi-turn conversation
- Input: scripted 4-6 turn conversation with context carryover.
- Verify:
  - assistant references recent context correctly
  - no major off-topic drift
  - turn order integrity (user->assistant alternation)

### S5. Barge-in interruption
- Input:
  - user asks question
  - assistant starts speaking
  - second user utterance injected mid-response
- Verify:
  - barge-in event fired
  - previous response flush/stop within bound (e.g., <400ms from interruption)
  - interruption utterance committed
  - next assistant response answers interruption utterance

### S6. Rapid interruption stress
- Input: two quick interruptions in one session.
- Verify:
  - no deadlock/hang
  - no stale response continues talking after barge-in
  - queued/committed turn behavior remains deterministic

## 5. Metrics and Pass Criteria

### 5.1 Latency
- `speech_end -> transcript.final`
- `turn_committed -> response.started`
- `barge_in_triggered -> response audio stop`

Set initial thresholds and tighten over iterations.

### 5.2 Transcription quality
- Word error rate (WER) or normalized string similarity vs expected text.
- Minimum acceptable threshold per scenario type.

### 5.3 Conversational relevance
- For each scenario define expected intent tags.
- Score response relevance with LLM-as-judge (offline deterministic prompt + fixed model version) or rules.
- Must exceed minimum relevance score.

### 5.4 Pacing and style
- Max response sentence count (default <=3 unless requested).
- Response duration range by scenario class.
- Detect overlong monologues.

### 5.5 Robustness
- No qualifying transcript dropped silently.
- No run stuck in-flight at scenario end.
- Clean session close.

## 6. Prompt Iteration Workflow

### 6.1 Prompt versioning
- Store prompt variants under `test/voicee2e/prompts/`:
  - `v1_baseline.txt`
  - `v2_concise_on_topic.txt`
  - etc.
- Inject variant as `SystemPromptSuffix` for voice commits during tests.

### 6.2 Evaluation loop
1. Run full scenario matrix on baseline prompt.
2. Run same matrix on candidate prompt.
3. Compare:
  - relevance score
  - pacing score
  - interruption correctness
  - latency regressions
4. Promote candidate only if it improves target metrics without regressions beyond budget.

### 6.3 Guardrails for “stays on point”
- Prompt requirements:
  - answer only latest user intent unless user asks to recap
  - 1-3 sentences by default
  - no unrelated advice/disclaimers
  - ask a clarifying question if intent is ambiguous

## 7. Implementation Plan

## Phase A: Harness Foundation
1. Add websocket voice client helper in Go for start/send/receive/end.
2. Add binary frame sender from PCM fixtures.
3. Add timeline recorder and JSON report writer.

## Phase B: Scenario Coverage
1. Implement S1-S4 baseline scenarios.
2. Add assertions for transcript/commit/response lifecycle.
3. Add latency and pacing metrics.

## Phase C: Interruption Verification
1. Implement S5-S6 interruption scenarios.
2. Add explicit stop-audio detection and barge-in timing checks.
3. Add failure classification (`late_stop`, `stale_talk`, `dropped_interrupt`).

## Phase D: Prompt Optimization Loop
1. Add prompt variant runner (A/B).
2. Add comparative report with deltas and recommendation.
3. Add CI target to run nightly or on-demand.

## 8. CI/CD Integration

Add targets:
- `make voice-e2e` (local full matrix)
- `make voice-e2e-smoke` (fast subset)
- `make voice-e2e-compare PROMPT_A=... PROMPT_B=...`

CI gating suggestion:
- PR smoke gate: S1 + S5 mandatory pass.
- Nightly full matrix: all scenarios + prompt comparison trend.

## 9. Deliverables

1. E2E harness code under `test/voicee2e/`.
2. Scenario definitions and fixtures.
3. Metrics report output and markdown summary.
4. Prompt variant files + comparison script.
5. Runbook doc: how to run locally and interpret failures.

## 10. Acceptance Criteria

Plan is complete when:
1. A single command runs automated voice E2E scenarios end-to-end.
2. Reports clearly show transcript accuracy, response relevance, pacing, and interruption behavior.
3. At least one prompt iteration is measured and objectively selected.
4. Failures are actionable with scenario IDs, timestamps, and reason codes.

## 11. Risks and Mitigations

1. Non-determinism from model outputs
- Mitigation: focus gates on bounded, measurable behaviors; use tolerance windows.

2. Synthetic TTS-to-ASR mismatch
- Mitigation: maintain curated fixture set and per-scenario thresholds.

3. Runtime duration too long
- Mitigation: smoke subset for PRs, full run nightly.

4. Flaky interruption timing
- Mitigation: record precise timeline markers and allow narrow jitter tolerance.
