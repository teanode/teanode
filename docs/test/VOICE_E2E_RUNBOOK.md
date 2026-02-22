# Voice E2E Runbook

## Prerequisites

1. Gateway runs locally on `http://127.0.0.1:8833`.
2. Valid provider credentials are configured for transcription + LLM + TTS.
3. WAV fixtures exist in `test/voicee2e/fixtures/` (repo-relative path).

## Commands

### Smoke

```bash
make voice-e2e-smoke
```

### Full matrix

```bash
make voice-e2e
```

### Prompt compare

```bash
make voice-e2e-compare PROMPT_A=test/voicee2e/prompts/v1_baseline.txt PROMPT_B=test/voicee2e/prompts/v2_on_topic.txt
```

## Direct CLI examples

```bash
go run ./test/voicee2e/cmd/voicee2e --suite test/voicee2e/scenarios/suite.yaml --scenario s5_barge_in --out test/voicee2e/reports/manual-s5.json
```

```bash
go run ./test/voicee2e/cmd/voicee2e --suite test/voicee2e/scenarios/suite.yaml --prompt test/voicee2e/prompts/v2_on_topic.txt --out test/voicee2e/reports/v2.json
```

## What to inspect in reports

1. Lifecycle:
- `speech_started`
- `speech_ended`
- `transcript.final`
- `turn_committed`
- `response.started`
- `response.completed`
- `barge_in_triggered` (for interruption scenarios)

2. Metrics:
- `latency_speech_end_to_transcript_ms`
- `transcript_similarity`
- `tts_sentence_count`
- `barge_in_count`

3. Failures:
- transcript missing
- response missing
- barge-in expected but absent
- latency over threshold
- transcript similarity below threshold

## Signoff checklist (S1-S6)

Mark each scenario pass/fail with report path:

- [ ] S1 short utterance
- [ ] S2 medium utterance
- [ ] S3 long utterance
- [ ] S4 multi-turn
- [ ] S5 barge-in interruption
- [ ] S6 rapid interruptions

Release gate:

1. No scenario failures.
2. No deadlocks/hangs.
3. Interruption scenarios confirm stop + next-turn commit.
4. Prompt candidate does not regress interruption or latency metrics.
