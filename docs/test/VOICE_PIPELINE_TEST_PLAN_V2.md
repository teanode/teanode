# Voice Pipeline Test Plan V2

## 1. Purpose

This plan defines how to validate the voice pipeline for correctness, latency, and reliability in a way that catches future regressions, not just current known failures.

The current gates already run many tests, but they still allow cases where:

1. S1-S6 scenarios pass while KPI targets fail.
2. KPI calculations are based on too few `turn.metrics` samples.
3. Some assertions depend on manual log inspection.

This V2 plan makes gates machine-enforceable and statistically meaningful.

## 2. Scope and Non-goals

Scope:

1. Unit, integration, race, end-to-end, and soak validation for voice pipeline behavior.
2. Automated KPI and invariant checks from generated reports and logs.
3. Extended scenarios for interruption, low-volume speech, fallback, and resilience.

Non-goals:

1. Redesigning the voice architecture.
2. Provider-specific performance tuning beyond test observability needs.
3. Replacing existing S1-S6 scenarios.

## 3. Quality Objectives

The suite must enforce three outcome classes.

1. Correctness:
1. User speech is captured and committed correctly.
2. Responses start and complete correctly.
3. Interruptions work and are explainable through events/reason codes.

2. Performance:
1. End-to-end and stage latencies stay within target thresholds.
2. Percentile gates only run when sample sizes are sufficient.

3. Reliability:
1. No race conditions in critical packages.
2. No queue overflow, reconnect storms, deadlocks, or leak patterns.

## 4. Traceability Matrix (Risk -> Tests)

| Risk | User/Operator Impact | Required Test Coverage |
|---|---|---|
| Provider selection nondeterminism | Inconsistent STT/TTS behavior across sessions | P0 routing unit tests + correctness counters gate |
| Feature flags diverge from runtime behavior | Client and server behavior mismatch | Feature-flag unit tests + explicit mode integration tests |
| STT final starvation | Missing transcripts, failed turns | Streaming STT integration + long-turn scenario + fallback-rate checks |
| Premature/missed end-of-turn | Cut-off or delayed responses | Strategy tests + pause/resume scenarios + premature commit KPI |
| False barge-ins | Response interruptions without user intent | Barge-in candidate/suppressed/triggered assertions + spurious barge-in KPI |
| Streaming TTS regressions | Slow first audio or broken interruption | TTS stream unit/integration + barge-in-during-TTS scenarios |
| Concurrency leaks/deadlocks | Session instability over time | `-race` suite + soak checks (goroutines, reconnects, queue pressure) |
| Metric blind spots | Invalid gate decisions | Report schema checks + minimum sample threshold + repeated-run median |

## 5. Success Metrics and Gate Thresholds

The gate runner must compute all metrics from machine-readable outputs (`*.json` reports + structured logs).

### 5.1 Functional

1. Scenario pass rate: 100% for core suite (S1-S6).
2. Extended suite pass rate: >= 95% (with explicit allowlist for known quarantined flake tests).
3. Required lifecycle events present and ordered:
1. `speech_started`
2. `speech_ended`
3. `transcript.final`
4. `turn_committed`
5. `response.started`
6. `response.completed`

### 5.2 Performance

Primary KPIs (median of 3 repeated suite runs):

1. `e2e_ms p50 <= 700`
2. `stt_ms p50 <= 100`
3. `tts_ms p50 <= 100`
4. `llm_ttfb_ms p50` must not regress by > 20% vs baseline median

Sample sufficiency:

1. Minimum `turn.metrics` samples before percentile gate: `>= 30` across repeated runs.
2. If sample count is below threshold, gate fails as `insufficient_data`.

### 5.3 Reliability and Safety

1. `go test -race` must pass for:
1. `./internal/voice/...`
2. `./internal/providers/...`
3. `./internal/api/v1api/...`
4. `./internal/gw/...`

2. Secondary KPIs:
1. `false_speech_starts_per_session <= 0.5`
2. `spurious_barge_ins_per_session <= 0.2`
3. `premature_commits_per_session <= 0.3`
4. `stt_fallback_rate < 5%`
5. `turn_drop_rate < 10%`
6. `queue_overflow_rate < 1%`
7. `session_error_rate <= 0.5%`

3. P0 counters:
1. `provider_selection_nondeterminism_events == 0`
2. `feature_flag_divergence_events == 0`

4. Soak targets:
1. 10-minute soak: goroutine growth <= start + 8, no queue-full warnings.
2. 1-hour soak: STT reconnect rate <= 0.5%, fallback rate < 5%, heap growth < 50MB steady-state window.

## 6. Test Layers and Required Cases

### 6.1 Unit and Contract Tests

Required additions/coverage verification:

1. Provider routing contracts:
1. Deterministic default selection under multiple providers.
2. Named provider found/not found/wrong capability.

2. Feature-flag behavior:
1. `ServerVAD=false` suppresses auto speech boundary events.
2. `ServerTurn=false` suppresses auto commit.
3. Explicit commit empty/too-short/valid reason codes.

3. Turn strategy:
1. Legacy parity behavior.
2. Balanced strategy candidate/trigger/suppressed transitions.
3. Reason-code correctness for suppressed barge-ins.

4. Streaming providers:
1. Deepgram endpointing/finalization behavior.
2. ElevenLabs stream framing and decode handling.
3. Adapter fallback behavior when streaming not available.

### 6.2 Integration Tests

1. RPC compatibility:
1. `voice.start` payload compatibility for optional fields.
2. `voice.input.commit` explicit-turn end-to-end path.

2. Pipeline-provider integration:
1. Streaming STT normal and fallback path.
2. Streaming TTS normal and cancellation path.
3. Provider misconfiguration fallback with warning.

### 6.3 E2E Suites

Core suite:

1. Existing S1-S6 from `test/voicee2e/scenarios/suite.yaml`.

Extended suite (`extended_suite.yaml`, new):

1. `s7_low_volume_whisper` - ensure no clipping and acceptable transcript similarity.
2. `s8_pause_resume_sentence` - no premature commit on brief pause.
3. `s9_noise_burst_no_commit` - avoid false speech start/commit under short noise burst.
4. `s10_streaming_tts_interrupt_resume` - barge-in during streamed output and next-turn commit.
5. `s11_provider_fallback_recovery` - induced streaming STT failure; batch fallback path succeeds.
6. `s12_rapid_explicit_commits` - serialized commits with no buffer races.

### 6.4 Soak and Perturbation

1. 10-minute standard soak using looped mixed fixtures.
2. 1-hour nightly soak with periodic interruption scenarios.
3. Fault-injection mode (if available) or controlled proxy-based degradation:
1. Introduce transient WebSocket interruption.
2. Introduce delayed provider response windows.
3. Validate recovery and error-rate thresholds.

## 7. Gate Design (Machine Enforced)

## 7.1 Gate Stages

1. Stage A: fast pre-merge gate (PR blocking)
1. Unit tests for changed packages.
2. Race tests for `voice/providers` at minimum.
3. Core S1-S6 single run.

2. Stage B: full pre-release gate (branch blocking)
1. Full race suites (`voice/providers/api/gw`).
2. Core suite x3 repeated runs.
3. Extended suite x2 runs.
4. KPI aggregation and sample sufficiency checks.
5. Strategy regression check in legacy mode.

3. Stage C: nightly resilience gate
1. 1-hour soak.
2. Reconnect/fallback/queue-pressure checks.
3. Drift alerts vs 7-day baseline.

## 7.2 Gate Inputs and Outputs

Inputs:

1. Voice e2e JSON reports.
2. Structured logs (or normalized log exports).
3. Optional pprof snapshots for soak jobs.

Outputs:

1. `gate_result.json` with pass/fail and failure reasons.
2. `gate_summary.md` with KPI table, sample counts, scenario failures, and regression deltas.
3. Exit code non-zero on any hard failure.

## 8. Regression Strategy (Future-proofing)

1. Invariant assertions over implementation details:
1. Event ordering and presence.
2. One active response per turn boundary unless explicitly canceled.
3. Exactly one turn finalization path per committed turn.

2. Metamorphic tests:
1. Same fixture under different provider selection should preserve transcript intent threshold.
2. Legacy strategy run should remain non-regressive vs baseline compare.

3. Repeated-run statistics:
1. Use median of repeated runs, not single run.
2. Fail on high variance to flag instability (`p50` drift > configured band).

4. Baseline management:
1. Keep immutable baseline snapshots with timestamp and commit hash.
2. Compare against rolling baseline and stable baseline.

## 9. Implementation Work Packages for Agent

### WP1: Gate runner and report validator

Deliverables:

1. `test/voicee2e/cmd/voicee2e` support for repeated runs and aggregate output (if missing).
2. New validator package for:
1. report schema checks
2. event-order invariants
3. KPI threshold checks
4. sample sufficiency checks

Acceptance:

1. Fails deterministically with actionable reasons (`insufficient_data`, `kpi_fail`, `event_order_fail`, etc.).

### WP2: Extended scenarios

Deliverables:

1. `test/voicee2e/scenarios/extended_suite.yaml` with S7-S12.
2. Any required fixture metadata additions.

Acceptance:

1. Scenarios are deterministic and runnable locally.
2. Each scenario maps to a documented risk from Section 4.

### WP3: Reliability/soak automation

Deliverables:

1. Soak runner command wrapper.
2. Log parsers for reconnect/fallback/queue events.
3. Optional pprof snapshot helper.

Acceptance:

1. Produces machine-readable soak summary and threshold result.

### WP4: CI integration

Deliverables:

1. PR job for Stage A.
2. Branch/release job for Stage B.
3. Nightly job for Stage C.

Acceptance:

1. CI artifacts include `gate_result.json` and `gate_summary.md`.

### WP5: Runbook and docs updates

Deliverables:

1. Update `/docs/test/VOICE_E2E_RUNBOOK.md` with V2 gate commands.
2. Add troubleshooting for common blockers (`localhost` vs `127.0.0.1`, missing API keys, stale gateway binary).

Acceptance:

1. Runbook can be executed end-to-end by a new engineer without tribal knowledge.

## 10. Suggested Commands (Reference)

Core checks:

```bash
go build ./...
go vet ./...
go test -race ./internal/voice/...
go test -race ./internal/providers/...
go test -race ./internal/api/v1api/...
go test -race ./internal/gw/...
```

Core suite (single run):

```bash
go run ./test/voicee2e/cmd/voicee2e/main.go \
  -gateway-url http://127.0.0.1:8833 \
  -suite test/voicee2e/scenarios/suite.yaml \
  -out test/voicee2e/reports/core.json
```

Legacy regression check:

```bash
go run ./test/voicee2e/cmd/voicee2e/main.go \
  -gateway-url http://127.0.0.1:8833 \
  -suite test/voicee2e/scenarios/suite.yaml \
  -config '{"voice":{"turn_strategy":"legacy"}}' \
  -out test/voicee2e/reports/core-legacy.json
```

## 11. Exit Criteria

The V2 test system is considered complete when:

1. Gates are fully machine-evaluated (no manual grep required for pass/fail).
2. KPI decisions include sample sufficiency enforcement.
3. Extended and soak suites are integrated and producing stable outputs.
4. A known-bad case (current Wave 7 state with KPI failure) is correctly rejected with explicit reasons.
5. A known-good baseline is accepted reproducibly across repeated runs.

## 12. Ownership and Review Checklist

Before merge of this test framework:

1. QA/owner confirms risk coverage mapping is complete.
2. Voice pipeline owner confirms KPI thresholds are still product-relevant.
3. CI owner confirms runtime/cost profile is acceptable for Stage A/B/C schedule.
4. Documentation reviewer confirms runbook aligns with actual commands and artifacts.
