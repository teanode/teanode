# Voice Pipeline Operator Runbook

> **Scope:** This runbook covers the TeaNode real-time voice pipeline introduced
> across milestones P0, L1, and L2 of the voice pipeline project. It is intended
> for operators who manage a running TeaNode gateway — not for end-users or clients.

---

## 1. Config Reference

All fields live in `~/.teanode/config.yaml` (or the directory set by `TEANODE_DIR`).
Fields are read at startup and re-read on `SIGHUP` (hot-reload). API keys can also be
injected via environment variable; env vars take precedence over the config file.

### 1.1 Provider selection (P0.1)

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `voice.transcriber_provider` | string | `"deepgram"` | STT provider name. Must match a name in `models.providers`. Falls back to batch Whisper if unset or provider not found. |
| `voice.synth_provider` | string | `""` (batch OpenAI TTS) | TTS provider name. Set to `"elevenlabs"` for streaming TTS. Falls back to batch OpenAI TTS if unset. |

**Environment overrides:**
```
DEEPGRAM_API_KEY=<key>    # auto-registers a "deepgram" provider and sets transcriber_provider
ELEVENLABS_API_KEY=<key>  # auto-registers an "elevenlabs" provider
```

**Example config:**
```yaml
voice:
  transcriber_provider: deepgram
  synth_provider: elevenlabs
models:
  providers:
    - name: deepgram
      base_url: https://api.deepgram.com
      api_key: <key>
    - name: elevenlabs
      base_url: https://api.elevenlabs.io
      api_key: <key>
```

### 1.2 Feature flags (P0.2)

Sent per-session by the client in the `voice.start` WebSocket RPC. Server enforces:

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `features.server_vad` | bool | `true` | Enable server-side VAD. If false, client is responsible for framing turns (push-to-talk mode). |
| `features.server_turn` | bool | `true` | Enable server-side turn detection and commit logic. If false, client commits turns via `voice.input.commit`. |
| `features.barge_in` | bool | `true` | Enable barge-in interruption: new speech cancels active TTS response. Can be overridden gateway-wide via `voice.barge_in` in config. |

**EnergyVAD parameters** (compile-time constants, not configurable):
- `vadPositiveThreshold = 0.04` RMS — frame must exceed this to count as speech.
- `vadNegativeThreshold = 0.02` RMS — frame below this counts as silence.
- `vadMinSpeechFrames = 10` (200 ms) — minimum run of speech frames to trigger `speech_started`.
- `vadRedemptionFrames = 16` (320 ms) — consecutive silence frames required to trigger `speech_ended`.

> **Fixture authoring note:** WAV fixtures used in `test/voicee2e` must have max
> consecutive silence < 320 ms to avoid mid-utterance VAD splits. See
> `test/voicee2e/fixtures/README.md` for the verification script.

### 1.3 Gateway-wide session policy (L2.2)

These fields set gateway-wide defaults for per-session policy. The client may send its own value in `voice.start`; when a config value is set it overrides the client.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `voice.turn_strategy` | string | `"legacy"` | Turn commit strategy. Values: `legacy` (commit after VAD end-of-speech), `balanced` (commit on first strong Deepgram streaming final). |
| `voice.barge_in` | bool | _(client decides, default `true`)_ | When set, overrides the client's `features.barge_in` for all sessions on this gateway. Useful to enforce a no-interruption policy deployment-wide. |

### 1.4 Context window budget (L2.4)

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `MaxContextTokens` (internal) | int | `16000` | Estimated-token cap for voice LLM requests. Uses `len(text)/4` approximation. Oldest turns are pruned when the budget is exceeded, always keeping the system prompt and the last 2 turns. Set to `0` to disable. |

This is a compile-time constant (`voiceMaxContextTokens = 16000` in `internal/voice/pipeline.go`).
To change it without a deployment, set `MaxContextTokens = 0` in both `SendMessage` call sites
in `pipeline.go` and recompile — or use the rollback switch below.

---

## 2. Rollback Switches

### P0 — Deterministic providers, feature flags, functional commit
No runtime rollback needed. All new config fields default to the unchanged behaviour
(batch Whisper, batch OpenAI TTS, energy VAD). Simply removing the new API keys from the
config or env reverts to the pre-P0 provider selection.

### L1.2 — Deepgram streaming STT
Remove or unset `DEEPGRAM_API_KEY` and remove the `deepgram` entry from
`models.providers`. The pipeline auto-falls back to batch Whisper for all new sessions.
In-flight sessions using Deepgram complete normally; no restart required.

### L2.2 — Turn strategy
Set `voice.turn_strategy = legacy` in the `voice.start` params. Takes effect on the
next session; no restart required. Gateway-wide: update `config.yaml` and send `SIGHUP`.

### L2.3 — ElevenLabs streaming TTS
Remove or unset `ELEVENLABS_API_KEY` and remove the `elevenlabs` entry from
`models.providers`. The TTS adapter immediately falls back to batch OpenAI TTS for all
new synthesis requests. No restart required.

### L2.4 — Context window pruning
Set `MaxContextTokens = 0` in both `VoiceSendMessageParams` call sites in
`internal/voice/pipeline.go` and recompile. This disables the 16k token budget check;
the existing model-level `compressContext` path remains active.

---

## 3. Observability Guide

### 3.1 `turn.metrics` events

The gateway emits a `turn.metrics` WebSocket envelope after each turn completes.
Fields:

| Field | Type | Description |
|-------|------|-------------|
| `turn_id` | string | ULID of the turn |
| `response_id` | string | ULID of the LLM response |
| `speech_started_ms` | int64 | Epoch ms when VAD fired `speech_started` |
| `speech_ended_ms` | int64 | Epoch ms when VAD fired `speech_ended` |
| `transcript_final_ms` | int64 | Epoch ms when `transcript.final` was emitted |
| `turn_committed_ms` | int64 | Epoch ms when the turn was committed to the LLM |
| `response_started_ms` | int64 | Epoch ms when first LLM token arrived |
| `response_completed_ms` | int64 | Epoch ms when TTS flush frame was sent |
| `stt_ms` | int64 | `transcript_final_ms - speech_ended_ms` (STT latency) |
| `llm_ttfb_ms` | int64 | `response_started_ms - turn_committed_ms` (LLM first-token latency) |
| `tts_ms` | int64 | Time from first TTS input to first audio chunk sent |
| `e2e_ms` | int64 | `response_started_ms - speech_ended_ms` (end-to-end perceived latency) |

### 3.2 Latency targets (post-L2 milestones)

| Metric | Target | Notes |
|--------|--------|-------|
| `stt_ms` p50 | ≤ 100 ms | Deepgram streaming path; batch Whisper is 400–800 ms |
| `tts_ms` p50 | ≤ 100 ms | Batch OpenAI TTS |
| `tts_ms` p50 | ≤ 350 ms | ElevenLabs `eleven_flash_v2_5`; TTFB is structurally 260–329 ms |
| `llm_ttfb_ms` | no regression | LLM latency is not a target; track for regressions only |
| `e2e_ms` | informational | Use per-scenario `max_response_latency_ms` in `suite.yaml` instead of cross-suite p50. |

### 3.3 Reading `voicee2e` reports

The `test/voicee2e` tool produces a JSON report and a Markdown summary:

```bash
go run ./test/voicee2e/cmd/voicee2e/main.go \
  -gateway-url http://localhost:8833 \
  -suite test/voicee2e/scenarios/suite.yaml \
  -out test/voicee2e/reports/run.json
# Markdown summary is written to test/voicee2e/reports/run.json.md
```

Key report fields:
- `passed_count` / `failed_count` — functional gate (6/6 required).
- `results[].metrics.transcript_similarity` — word-recall score vs `expected_text` in `suite.yaml`.
- `results[].turn_metrics[].tts_ms` — per-turn TTS TTFB.
- `results[].failures[]` — list of assertion failures with threshold values.

---

## 4. Incident Playbook

### 4.1 Deepgram WebSocket fails to connect

**Symptom:** Gateway logs show `stt streaming open error` or `dial tcp ... connection refused`.
Sessions fall back to batch Whisper automatically; `stt_ms` spikes to 400–800 ms.

**Response:**
1. Check `DEEPGRAM_API_KEY` is set and valid: `curl -H "Authorization: Token $DEEPGRAM_API_KEY" https://api.deepgram.com/v1/projects`.
2. Check network connectivity to `api.deepgram.com:443`.
3. Monitor `stt_fallback_rate` in `turn.metrics` logs; target < 5%.
4. If the key is invalid, rotate it in `~/.teanode/config.yaml` and send `SIGHUP`.
5. If Deepgram is down, the fallback path is fully operational — no action required beyond monitoring.

### 4.2 ElevenLabs stream produces silence or websocket 1008

**Symptom:** Gateway logs show `elevenlabs websocket close 1008 (activity/quota limit)`. TTS audio is missing or truncated. `tts_ms` spikes.

**Response:**
1. Check ElevenLabs account quota at `https://elevenlabs.io/app/subscription`.
2. Verify `ELEVENLABS_API_KEY` is valid.
3. If quota is exhausted, revert to batch OpenAI TTS by unsetting `ELEVENLABS_API_KEY` (see L2.3 rollback). No restart required — takes effect on next session.
4. `tts_ms` will drop to batch TTS latency (~100 ms p50) after rollback.

### 4.3 Context pruning logs appearing too frequently

**Symptom:** Gateway DEBUG logs show `voice context pruned: kept=N dropped=M` very often, suggesting the 16k budget is being hit mid-conversation.

**Response:**
1. Check whether the system prompt is unusually large (e.g. large workspace context). Reduce workspace file inclusion via `models.default_limits.max_workspace_file_chars`.
2. Increase `voiceMaxContextTokens` in `internal/voice/pipeline.go` and recompile. Current value: 16000 estimated tokens.
3. As a temporary measure, disable pruning by setting `MaxContextTokens = 0` (see L2.4 rollback).
