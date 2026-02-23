# Conversation Debugger Engineering Plan

## 1. Purpose

Define an implementation-ready plan for a conversation-level debugger that lets engineers inspect, for each turn:

1. Audio captured and synthesized.
2. Turn-boundary detection and commit reasons.
3. Transcript evolution (partial -> final).
4. Response generation lifecycle.
5. TTS chunking/latency behavior.
6. Warnings/fallback/error context needed to fix regressions.

The primary UX is an in-product debug mode in the main conversation panel, with deeper detail in a side drawer and export bundle.

## 2. Goals

1. Make root-cause analysis possible without grepping raw logs.
2. Preserve turn-level causality with stable IDs and ordered timestamps.
3. Enable one-click export of a complete debug bundle for offline analysis.
4. Keep default user UX unchanged when debug mode is off.
5. Avoid impacting live voice-path latency under normal operation.

## 3. Non-goals

1. Full observability platform replacement.
2. Rebuilding existing voice runtime architecture.
3. Always-on continuous audio recording for all sessions.
4. Production analytics dashboarding beyond debug workflows.

## 4. Current Assets to Reuse

The system already has key hooks and metrics needed for this effort:

1. Voice lifecycle and pipeline event points:
   - `internal/voice/pipeline.go`
   - `internal/voice/session.go`
2. Observer pattern:
   - `internal/voice/observer.go`
3. Turn metric calculation/emission:
   - `internal/voice/metrics.go`
   - `internal/voice/metrics_test.go`
4. Existing test/e2e infrastructure:
   - `test/voicee2e/`
   - `docs/test/VOICE_PIPELINE_TEST_PLAN_V2.md`

## 5. Product Requirements

### 5.1 Main Conversation Panel (Debug Toggle ON)

Show inline per-turn debug summary:

1. `turn_id`, `run_id` (if available).
2. Stage latencies: `stt_ms`, `llm_ttfb_ms`, `tts_ms`, `e2e_ms`.
3. Mini timeline markers:
   - `speech_end`
   - `transcript.final`
   - `response.started`
   - `tts.first_chunk`
4. Quick actions:
   - Play input clip
   - Play output clip
   - Open trace drawer

### 5.2 Debug Drawer (Deep Inspect)

1. Full ordered event log for selected turn.
2. Transcript evolution list (partial/final events with timestamps).
3. Response details (start/completion/cancel reason).
4. Provider diagnostics (fallbacks, ws close codes, retries, quota).
5. Raw payload links (`json`, `wav` artifacts).

### 5.3 Export

One-click per-session debug bundle zip containing:

1. `session.json`
2. `turns.ndjson`
3. `metrics.json`
4. `artifacts/turn-xxxx/{input.wav,tts.wav,transcript.json}`
5. `warnings.log`
6. `README.md` template for expected vs observed behavior annotations

## 6. Technical Design

### 6.1 Canonical Trace Event

Introduce a normalized trace event schema as append-only source of truth:

```json
{
  "ts_unix_ms": 0,
  "session_id": "s_...",
  "conversation_id": "c_...",
  "turn_id": "t_...",
  "run_id": "run_...",
  "response_id": "resp_...",
  "event": "transcript.final",
  "stage": "stt",
  "payload": {},
  "severity": "info"
}
```

Requirements:

1. Every event has monotonic session timestamp.
2. Event must include stable IDs where known.
3. Missing IDs are explicit `null`, not omitted.
4. Event ordering is by write sequence; ties resolved by sequence number.

### 6.2 Turn Artifact Layout

Store artifacts under a session-scoped root:

```text
debug_traces/<session_id>/
  session.json
  turns.ndjson
  metrics.json
  warnings.log
  artifacts/
    turn-0001/
      input.wav
      tts.wav
      transcript.json
    turn-0002/
      ...
```

`session_manifest.json` maps turn IDs to artifact paths and summarizes anchor completeness.

### 6.3 Capture Modes

1. `normal` (default):
   - turn-scoped audio only (input/tts clips)
   - low overhead
2. `full_capture` (opt-in):
   - continuous input/output timeline recording
   - higher storage/privacy cost

### 6.4 Backend Components

Add a debug trace package (suggested path `internal/voice/debugtrace/`):

1. `collector.go`:
   - receives normalized events from observer/pipeline hooks
   - non-blocking enqueue with bounded ring buffer
2. `writer.go`:
   - async file writer for ndjson/manifest/artifacts
3. `store.go`:
   - query by session, turn, event type
4. `export.go`:
   - zip bundle generation

Do not block real-time audio loops on trace I/O.

### 6.5 API Endpoints

Add debug endpoints (protected; non-public by default):

1. `GET /api/debug/voice/sessions`
2. `GET /api/debug/voice/sessions/{sessionId}`
3. `GET /api/debug/voice/sessions/{sessionId}/turns`
4. `GET /api/debug/voice/sessions/{sessionId}/turns/{turnId}`
5. `GET /api/debug/voice/sessions/{sessionId}/artifacts/{path}`
6. `POST /api/debug/voice/sessions/{sessionId}/export`

### 6.6 UI Integration

Files likely touched:

1. `web/src/routes/...` (conversation page route)
2. `web/src/components/...` (turn card / drawer / badges)
3. `web/src/hooks/...` (debug fetch hooks)

Behavior:

1. Debug OFF: zero visual changes.
2. Debug ON: show inline debug strip per turn + drawer controls.
3. Lazy-load heavy artifacts only when drawer/players opened.

## 7. Delivery Plan

### Phase 1: Trace Backbone + Minimal UI

Scope:

1. Canonical event schema and collector/writer.
2. `turns.ndjson`, `session.json`, `metrics.json`.
3. Basic debug toggle + per-turn metric badges + mini timeline.

Acceptance:

1. For each completed turn, required anchors present or explicitly marked missing.
2. Debug OFF path unchanged.
3. No measurable p50 latency regression in core voice e2e.

### Phase 2: Artifact Capture + Drawer

Scope:

1. Turn-scoped artifact writing (`input.wav`, `tts.wav`, `transcript.json`).
2. Debug drawer with event log, transcript evolution, diagnostics.
3. Quick-play audio from main panel.

Acceptance:

1. Multi-turn sessions map artifacts to correct turns.
2. Interruption turns include cancel reason and partial/final trace.
3. Drawer can render a failing turn without reading raw logs.

### Phase 3: Export + Operational Hardening

Scope:

1. Zip export endpoint.
2. Retention policy and disk budget controls.
3. Redaction options for transcript/payload/audio.
4. Optional `full_capture` mode.

Acceptance:

1. Export bundle is self-contained and analyzable offline.
2. Retention cleanup works and does not race active writes.
3. Security controls gate access to debug data.

## 8. Detailed Requirements for Multi-turn Causality

Must-have invariants:

1. One selected `turn_id` per `response.started`.
2. `response.completed` or `response.canceled` references originating turn.
3. Barge-in/cancel events reference interrupted response turn and interrupting turn.
4. Transcript events include both provider stream sequence and wall clock.
5. Manifest must contain ordered turn list with parent/interrupt linkage.

## 9. Performance and Safety Constraints

1. Trace ingestion must be non-blocking in hot loops.
2. Backpressure strategy:
   - drop debug events before blocking voice runtime
   - increment `debug_trace_drop_count`
3. File writes batched and buffered.
4. Artifact capture configurable by env/config:
   - `voice.debug.enabled`
   - `voice.debug.mode` (`off|normal|full_capture`)
   - `voice.debug.retention_hours`
   - `voice.debug.max_disk_mb`
5. Default production posture: disabled unless explicitly enabled.

## 10. Security and Privacy

1. Debug endpoints require privileged auth.
2. Provide redaction toggles:
   - transcript redaction
   - prompt/payload redaction
   - audio artifact suppression
3. Include warning banner in UI when debug capture is active.
4. Ensure export bundles are excluded from standard public logs/artifact uploads unless explicit.

## 11. Testing Plan

### 11.1 Unit

1. Event normalization and ID propagation.
2. Manifest turn mapping correctness.
3. Retention pruning and size limits.
4. Redaction behavior.

### 11.2 Integration

1. Multi-turn with interruption: turn linkage correctness.
2. Provider fallback warnings appear in trace.
3. Export zip contains all expected files and valid JSON.

### 11.3 E2E

1. Debug OFF: baseline UI and voice behavior unchanged.
2. Debug ON (`normal`): per-turn badges/timeline/drawer visible.
3. Failing scenario reproduction: trace shows enough detail to classify root cause (`stt`, `llm`, `tts`, `turning`, `provider`).

### 11.4 Gates

1. `go test -race ./internal/voice/...`
2. `go test -race ./internal/providers/...`
3. UI tests for debug toggle and drawer render paths.
4. No regression to core functional voicee2e scenarios.

## 12. Implementation Backlog

1. Create `internal/voice/debugtrace` package.
2. Add observer-to-trace bridge in `internal/voice/observer.go`.
3. Add pipeline/session hook emissions where IDs are currently missing.
4. Persist artifacts per turn in `internal/voice/pipeline.go`/`session.go`.
5. Add debug API handlers in gateway/api layer.
6. Add frontend debug toggle, inline trace strip, and drawer.
7. Add export endpoint + zip creator.
8. Add docs for operators and privacy.

## 13. Risks and Mitigations

1. Risk: Debug writes impact runtime latency.
   - Mitigation: async buffered writer, drop-on-pressure.
2. Risk: Incomplete turn linkage in interruption flows.
   - Mitigation: explicit invariants + integration tests.
3. Risk: Sensitive data leakage.
   - Mitigation: auth gates, redaction, disabled-by-default.
4. Risk: Disk growth from artifacts.
   - Mitigation: retention limits + max disk budget + mode controls.

## 14. Definition of Done

1. Engineer can debug a failing multi-turn session from UI and export bundle without reading raw server logs.
2. For each turn, system surfaces:
   - audio in/out artifacts
   - transcript evolution
   - response lifecycle
   - turn boundary events/reasons
   - stage latency metrics
3. Debug OFF remains clean and unchanged for normal users.
4. Performance, race, and functional gates remain green.
