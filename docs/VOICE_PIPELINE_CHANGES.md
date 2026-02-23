# Voice Pipeline Changes

Changelog of all new behaviours, config fields, and rollback switches introduced
across the P0, L1, and L2 voice pipeline milestones.

---

## P0 — Foundation (Wave 1)

### P0.1 — Deterministic Provider Selection
- **New config fields:** `voice.transcriber_provider`, `voice.synth_provider`
- **New env vars:** `DEEPGRAM_API_KEY`, `ELEVENLABS_API_KEY` (auto-register providers)
- **Behaviour:** Provider lookup is now deterministic — first matching name in
  `models.providers` wins. Previously the provider order was undefined.
- **Rollback:** Remove API keys from env/config; pipeline auto-selects batch Whisper
  (STT) and batch OpenAI TTS (TTS).

### P0.2 — Feature Flags
- **New client fields:** `features.server_vad`, `features.server_turn`,
  `features.server_denoise`, `features.barge_in`
- **Behaviour:** Server enforces per-session feature toggles sent in `voice.start`.
  `server_denoise` is a stub (no-op) in this release.
- **Rollback:** All fields default to values matching pre-P0 behaviour; no action needed.

### P0.3 — Functional Turn Commit
- **Behaviour:** `InputCommit` now flushes the explicit audio buffer and runs the full
  transcription → send path. Previously `InputCommit` was a no-op.
- **Rollback:** Not needed; this fixes a correctness gap with no observable regression.

---

## L1 — Latency Level 1 (Waves 2–4)

### L1.3 — Observer Telemetry (Wave 2)
- **New events emitted:** `turn.metrics` WebSocket envelope after each turn
- **New metrics:** `stt_ms`, `llm_ttfb_ms`, `tts_ms`, `e2e_ms`,
  `speech_started_ms`, `speech_ended_ms`, `transcript_final_ms`,
  `turn_committed_ms`, `response_started_ms`, `response_completed_ms`
- **Rollback:** Metrics are passive; disabling is not necessary. Fields are absent from
  `turn.metrics` if the corresponding event did not occur.

### L1.1 — Silero VAD (Wave 3)
- **New feature flag:** `features.silero_vad` (bool, default `false`)
- **Behaviour:** When `true`, routes VAD frames to the Silero sidecar on
  `127.0.0.1:9123` instead of the built-in EnergyVAD. Falls back to EnergyVAD if the
  sidecar is unreachable.
- **Rollback:** Set `features.silero_vad = false` in `voice.start` params.

### L1.4 — Client Audio Gate (Wave 3)
- **Behaviour:** Client-side audio gate thresholds lowered to silence-floor levels.
  Reduces leading-edge clipping on soft-spoken input.
- **Rollback:** Not configurable; no observable regression expected.

### L1.2 — Deepgram Streaming STT (Wave 4)
- **Behaviour:** When `DEEPGRAM_API_KEY` is set and `voice.transcriber_provider =
  "deepgram"`, each turn opens a persistent Deepgram WebSocket for real-time
  transcription. Streaming finals trigger the commit path; batch Whisper is the
  automatic fallback.
- **`stt_ms` improvement:** 400–800 ms (batch Whisper) → < 100 ms p50 (Deepgram
  streaming).
- **Rollback:** Unset `DEEPGRAM_API_KEY`; all sessions fall back to batch Whisper
  automatically. No restart required.

---

## L2 — Latency Level 2 (Waves 5–8)

### L2.2 — Turn Strategy (Wave 5)
- **New config field:** `voice.turn_strategy` (string, default `"legacy"`)
- **Values:**
  - `legacy` — commit after VAD `speech_ended` event (previous behaviour).
  - `balanced` — commit on first strong Deepgram streaming final; enables speculative
    LLM (see L2.1).
- **Rollback:** Set `voice.turn_strategy = legacy`.

### L2.1 — Speculative LLM (Wave 6)
- **Behaviour:** When `turn_strategy = balanced`, the pipeline fires a speculative LLM
  request on the first credible Deepgram streaming final. If the final transcript
  matches (similarity ≥ 0.70), the speculative response is promoted; otherwise it is
  cancelled and a fresh run is started with the committed transcript.
- **Guards:** Speculation is skipped if:
  - Text is < 8 runes or interim confidence < 0.30.
  - A barge-in occurred within the last 500 ms.
  - A speculative run is already active.
- **`e2e_ms` on speculative turns:** ~79 ms (vs ~1500 ms+ non-speculative).
- **Rollback:** Set `voice.turn_strategy = legacy` to disable speculation entirely.

### L2.3 — ElevenLabs Streaming TTS (Wave 7)
- **New config field:** `voice.synth_provider = "elevenlabs"`
- **New env var:** `ELEVENLABS_API_KEY`
- **Behaviour:** When set, TTS synthesis uses the ElevenLabs `eleven_flash_v2_5` model
  over a streaming WebSocket. Audio chunks are forwarded to the client as they arrive.
  Barge-in cancels the WebSocket mid-stream.
- **`tts_ms` (TTFB):** 260–329 ms structurally (model inference); p50 ~214 ms measured.
- **Rollback:** Unset `ELEVENLABS_API_KEY`; adapter falls back to batch OpenAI TTS.
  No restart required.

### L2.4 — Conversation Context Window Management (Wave 8)
- **New internal constant:** `voiceMaxContextTokens = 16000` (in `internal/voice/pipeline.go`)
- **Behaviour:** Before each voice LLM request, the assembled message list is pruned to
  stay within a 16,000 estimated-token budget (`len(text)/4` approximation). The system
  prompt and the last 2 turns are always preserved. Oldest turns are dropped first. A
  DEBUG log is emitted when pruning occurs:
  ```
  voice context pruned: kept=N dropped=M estimated_tokens=K
  ```
- **Non-voice calls:** Unaffected (`MaxContextTokens = 0` path; existing
  `compressContext` logic handles model-level truncation).
- **Rollback:** Set `MaxContextTokens = 0` in both `SendMessage` call sites in
  `internal/voice/pipeline.go` and recompile.

---

## e2e Test Suite — Fixture Stability Invariant

All WAV fixtures in `test/voicee2e/fixtures/` must have **max consecutive silence
< 320 ms** to avoid EnergyVAD mid-utterance splits. See
`test/voicee2e/fixtures/README.md` for the verification script and fixture authoring
rules.
