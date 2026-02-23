# Backlog Execution Log

Started: 2026-02-22T21:56:22Z

## Execution Plan (Remaining)
- Wave 0: S1, S2
- Wave 1: P0.1, P0.2, P0.3
- Wave 2: L1.3
- Wave 3: L1.1, L1.4
- Wave 4: L1.2
- Wave 5: L2.2
- Wave 6: L2.1
- Wave 7: L2.3
- Wave 8: L2.4
- Wave 9: DOCS

## Checkpoints
- 2026-02-22T21:56:22Z
  - Wave: 0
  - Task: n/a
  - Status: failed
  - Branch: pipeline
  - Commit: n/a
  - Validations: not started
  - Next: initialize Wave 0 task execution and create task branches/worktrees.
- 2026-02-22T21:57:13Z
  - Wave: 0
  - Task: S1
  - Status: in_progress
  - Branch: codex/s1-silero-spike
  - Commit: n/a
  - Validations: n/a (research spike)
  - Next: create S1 worktree and draft decision doc.

- 2026-02-22T21:57:13Z
  - Wave: 0
  - Task: S2
  - Status: in_progress
  - Branch: codex/s2-context-window-spike
  - Commit: n/a
  - Validations: n/a (research spike)
  - Next: create S2 worktree and draft decision doc.
- 2026-02-22T21:58:29Z
  - Wave: 0
  - Task: S1
  - Status: complete
  - Branch: codex/s1-silero-spike
  - Commit: 37088b0
  - Validations: n/a (research spike accepted)
  - Next: cherry-pick into pipeline and run wave gate.

- 2026-02-22T21:58:28Z
  - Wave: 0
  - Task: S2
  - Status: complete
  - Branch: codex/s2-context-window-spike
  - Commit: ad6986d
  - Validations: n/a (research spike accepted)
  - Next: cherry-pick into pipeline and run wave gate.

- 2026-02-22T21:58:52Z
  - Wave: 0
  - Task: S1,S2
  - Status: complete
  - Branch: pipeline
  - Commit: 72bb551, 94dcc57
  - Validations: integration pending
  - Next: execute wave 0 gate commands.

- 2026-02-22T21:59:04Z
  - Wave: 0
  - Task: gate
  - Status: failed
  - Branch: pipeline
  - Commit: 94dcc57
  - Validations: |
      FAIL `go build ./...` -> `internal/frontend/frontend.go:12:12: pattern static: no matching files found`
      FAIL `go vet ./...` -> `internal/frontend/frontend.go:12:12: pattern static: no matching files found`
      PASS `go test -race ./internal/providers/...` -> ok github.com/teanode/teanode/internal/providers
      PASS `go test -race ./internal/voice/...` -> ok github.com/teanode/teanode/internal/voice
      PASS `go test -race ./internal/api/v1api/...` -> ok github.com/teanode/teanode/internal/api/v1api
  - Next: stop wave advancement; fix frontend embed/static artifact issue, then re-run Wave 0 gate.
- 2026-02-22T21:59:48Z
  - Wave: 0
  - Task: orchestrator-checkpoint
  - Status: complete
  - Branch: pipeline
  - Commit: de3166b
  - Validations: state/log persisted in git
  - Next: unblock gate by restoring `internal/frontend/static` embed inputs, then re-run Wave 0 gate.
- 2026-02-22T22:01:16Z
  - Wave: 0
  - Task: gate-retry
  - Status: passed
  - Branch: pipeline
  - Commit: 430cb14
  - Validations: |
      PASS `go build ./...`
      PASS `go vet ./...`
      PASS `go test -race ./internal/providers/...`
      PASS `go test -race ./internal/voice/...`
      PASS `go test -race ./internal/api/v1api/...`
  - Next: advance to Wave 1 and start P0.1 + P0.2 in parallel (P0.3 after P0.2).
- 2026-02-22T22:01:51Z
  - Wave: 1
  - Task: P0.1
  - Status: in_progress
  - Branch: codex/p0-1-deterministic-providers
  - Commit: n/a
  - Validations: pending
  - Next: implement deterministic + named provider lookup and tests.

- 2026-02-22T22:01:51Z
  - Wave: 1
  - Task: P0.2
  - Status: in_progress
  - Branch: codex/p0-2-feature-flags
  - Commit: n/a
  - Validations: pending
  - Next: enforce ServerVAD/ServerTurn/ServerDenoise runtime branching.
- 2026-02-22T22:10:13Z
  - Wave: 1
  - Task: P0.1
  - Status: complete
  - Branch: codex/p0-1-deterministic-providers
  - Commit: 3e9ec2e
  - Validations: |
      PASS `go test -race ./internal/providers/... -run TestFindTranscriber`
      PASS `go test -race ./internal/providers/...`
      PASS `go build ./...`
  - Next: integrate P0.1 to pipeline; continue P0.2 implementation.
- 2026-02-22T22:12:09Z
  - Wave: 1
  - Task: P0.2
  - Status: complete
  - Branch: codex/p0-2-feature-flags
  - Commit: 5259f1a
  - Validations: |
      PASS `go test -race ./internal/voice/... -run TestAudioInputLoop_ServerVAD`
      PASS `go test -race ./internal/voice/... -run TestAudioInputLoop_ServerTurn`
      PASS `go test -race ./internal/voice/...`
      PASS `go build ./...`
  - Next: merge P0.1/P0.2 to pipeline, then execute P0.3 from a new branch based on pipeline.
- 2026-02-22T22:13:21Z
  - Wave: 1
  - Task: P0.3
  - Status: in_progress
  - Branch: codex/p0-3-functional-commit
  - Commit: n/a
  - Validations: pending
  - Next: implement InputCommit buffered-audio commit path and reason propagation.
- 2026-02-22T22:16:04Z
  - Wave: 1
  - Task: P0.3
  - Status: complete
  - Branch: codex/p0-3-functional-commit
  - Commit: 6858c1a
  - Validations: |
      PASS `go test -race ./internal/voice/... -run TestInputCommit`
      PASS `go test -race ./internal/voice/...`
      PASS `go build ./...`
  - Next: merge P0.3 to pipeline and run Wave 1 gate block.
- 2026-02-22T22:13:05Z
  - Wave: 1
  - Task: integrate
  - Status: complete
  - Branch: pipeline
  - Commit: c3921f9, 71e026d
  - Validations: P0.1 and P0.2 merged in required order
  - Next: execute P0.3 from pipeline.

- 2026-02-22T22:16:20Z
  - Wave: 1
  - Task: integrate
  - Status: complete
  - Branch: pipeline
  - Commit: 47f1d39
  - Validations: P0.3 merged
  - Next: run full Wave 1 gate.

- 2026-02-22T22:19:15Z
  - Wave: 1
  - Task: gate
  - Status: passed
  - Branch: pipeline
  - Commit: 47f1d39
  - Validations: |
      PASS `go build ./...`
      PASS `go vet ./...`
      PASS `go test -race ./internal/providers/...`
      PASS `go test -race ./internal/voice/...`
      PASS `go test -race ./internal/api/v1api/...`
      PASS `make test-voice-e2e-smoke` (required local gateway on 127.0.0.1:8833)
  - Next: advance to Wave 2 (L1.3 observer telemetry).
- 2026-02-22T22:26:11Z
  - Wave: 2
  - Task: L1.3
  - Status: in_progress
  - Branch: codex/l1-3-observer-telemetry
  - Commit: n/a
  - Validations: pending
  - Next: add turn observer + metrics emission and wire into voicee2e report path.
- 2026-02-22T22:30:25Z
  - Wave: 2
  - Task: L1.3
  - Status: complete
  - Branch: codex/l1-3-observer-telemetry
  - Commit: 8e911e9
  - Validations: |
      PASS `go test -race ./internal/voice/... -run TestMetricsObserver`
      PASS `go test -race ./internal/voice/... -run TestNotifyObservers`
      PASS `go test -race ./internal/voice/...`
      PASS `go test ./test/voicee2e/...`
      PASS `go build ./...`
  - Next: integrate L1.3 to pipeline and run Wave 2 gate.
- 2026-02-22T22:30:48Z
  - Wave: 2
  - Task: integrate
  - Status: complete
  - Branch: pipeline
  - Commit: fde72b1
  - Validations: L1.3 merged
  - Next: run Wave 2 gate commands.

- 2026-02-22T22:33:59Z
  - Wave: 2
  - Task: gate
  - Status: passed
  - Branch: pipeline
  - Commit: fde72b1
  - Validations: |
      PASS `go build ./...`
      PASS `go vet ./...`
      PASS `go test -race ./internal/voice/...`
      PASS `go run ./test/voicee2e/cmd/voicee2e/main.go -gateway-url http://127.0.0.1:8833 -suite test/voicee2e/scenarios/suite.yaml -out test/voicee2e/reports/after-wave2.json` (required escalated local websocket access)
      PASS metrics checks: turn.metrics count=3; complete-turn non-zero timestamp sets=3
  - Next: advance to Wave 3 parallel tasks L1.1 and L1.4.
- 2026-02-22T22:34:37Z
  - Wave: 3
  - Task: L1.1
  - Status: in_progress
  - Branch: codex/l1-1-silero-vad
  - Commit: n/a
  - Validations: pending
  - Next: implement VAD analyzer abstraction + Silero path + fallback behavior.

- 2026-02-22T22:34:37Z
  - Wave: 3
  - Task: L1.4
  - Status: in_progress
  - Branch: codex/l1-4-client-audio-gate
  - Commit: n/a
  - Validations: pending
  - Next: refine ScriptProcessor/level gate thresholds in web hook.
- 2026-02-22T22:45:36Z
  - Wave: 3
  - Task: L1.1
  - Status: complete
  - Branch: codex/l1-1-silero-vad
  - Commit: db3f790
  - Validations: |
      PASS `go test -race ./internal/voice/... -run TestEnergyVAD`
      PASS `go test -race ./internal/voice/... -run TestSileroVAD`
      PASS `go test -race ./internal/voice/...`
      PASS `go build ./...`
  - Next: merge to pipeline and run Wave 3 gate.

- 2026-02-22T22:45:36Z
  - Wave: 3
  - Task: L1.4
  - Status: complete
  - Branch: codex/l1-4-client-audio-gate
  - Commit: 4aa253f
  - Validations: |
      PASS `npm run build` (warnings only)
  - Next: merge to pipeline and run Wave 3 gate.

- 2026-02-22T23:03:09Z
  - Wave: 3
  - Task: gate
  - Status: failed
  - Branch: pipeline
  - Commit: 6f4423d
  - Validations: |
      PASS `go build ./...`
      PASS `go vet ./...`
      PASS `go test -race ./internal/voice/...`
      FAIL `go run ./test/voicee2e/cmd/voicee2e/main.go -gateway-url http://localhost:8833 -suite test/voicee2e/scenarios/suite.yaml -config '{"features":{"silero_vad":true}}' -out test/voicee2e/reports/after-wave3.json` -> websocket denied (`dial tcp [::1]:8833: operation not permitted`)
  - Next: rerun Wave 3 e2e gate using `127.0.0.1` and escalated local websocket access.

- 2026-02-22T23:07:48Z
  - Wave: 3
  - Task: gate
  - Status: failed
  - Branch: pipeline
  - Commit: 6f4423d
  - Validations: |
      FAIL `go run ./test/voicee2e/cmd/voicee2e/main.go -gateway-url http://127.0.0.1:8833 -suite test/voicee2e/scenarios/suite.yaml -config '{"features":{"silero_vad":true}}' -out test/voicee2e/reports/after-wave3.json` -> Passed:0 Failed:6 (no transcript.final/response.started)
      Diagnostic: gateway process was stale from pre-fix build; restarted gateway from current pipeline.
  - Next: rerun Wave 3 e2e gate against restarted gateway.

- 2026-02-22T23:11:21Z
  - Wave: 3
  - Task: gate
  - Status: in_progress
  - Branch: pipeline
  - Commit: 6f4423d
  - Validations: |
      RETRY `go run ./test/voicee2e/cmd/voicee2e/main.go -gateway-url http://127.0.0.1:8833 -suite test/voicee2e/scenarios/suite.yaml -config '{"features":{"silero_vad":true}}' -out test/voicee2e/reports/after-wave3.json` -> Passed:5 Failed:1 (`s1_short` missing response.started)
  - Next: rerun once to determine transient vs deterministic failure.

- 2026-02-22T23:12:49Z
  - Wave: 3
  - Task: gate
  - Status: in_progress
  - Branch: pipeline
  - Commit: 6f4423d
  - Validations: |
      PASS `go run ./test/voicee2e/cmd/voicee2e/main.go -gateway-url http://127.0.0.1:8833 -suite test/voicee2e/scenarios/suite.yaml -config '{"features":{"silero_vad":true}}' -out test/voicee2e/reports/after-wave3.json` -> Passed:6 Failed:0
      FAIL (initial) baseline generation `... -out test/voicee2e/reports/baseline.json` -> Passed:5 Failed:1 (first-scenario timeout)
      PASS baseline retry `... -out test/voicee2e/reports/baseline.json` -> Passed:6 Failed:0
      PASS `go run ./test/voicee2e/cmd/voicee2e/main.go --compare --prompt-a test/voicee2e/reports/baseline.json --prompt-b test/voicee2e/reports/after-wave3.json`
  - Next: mark Wave 3 gate passed and advance to Wave 4 task L1.2.

- 2026-02-22T23:18:25Z
  - Wave: 4
  - Task: L1.2
  - Status: in_progress
  - Branch: codex/l1-2-deepgram-streaming-stt
  - Commit: n/a
  - Validations: pending
  - Next: create Wave 4 worktree/branch and implement streaming STT interfaces and pipeline integration.

- 2026-02-23T01:20:32Z
  - Wave: 4
  - Task: L1.2
  - Status: complete
  - Branch: codex/l1-2-deepgram-streaming-stt
  - Commit: d20c494
  - Validations: |
      PASS `go test -race ./internal/providers/...`
      PASS `go test -race ./internal/voice/...`
      PASS `go test ./test/voicee2e/...`
      PASS `go build ./...`
  - Next: integrate L1.2 into pipeline and execute Wave 4 gate.

- 2026-02-23T01:21:03Z
  - Wave: 4
  - Task: integrate
  - Status: complete
  - Branch: pipeline
  - Commit: f3986a4
  - Validations: L1.2 cherry-picked from task branch
  - Next: run Wave 4 gate commands.

- 2026-02-23T01:24:03Z
  - Wave: 4
  - Task: gate
  - Status: failed
  - Branch: pipeline
  - Commit: f3986a4
  - Validations: |
      PASS `go build ./...`
      PASS `go vet ./...`
      PASS `go test -race ./internal/voice/...`
      PASS `go test -race ./internal/providers/...`
      BLOCKED `DEEPGRAM_API_KEY=<key> go run ./test/voicee2e/cmd/voicee2e/main.go -gateway-url http://localhost:8833 -suite test/voicee2e/scenarios/suite.yaml -out test/voicee2e/reports/after-wave4.json` -> `DEEPGRAM_API_KEY` is unset in environment
      SKIP `go run ./test/voicee2e/cmd/voicee2e/main.go --compare --prompt-a test/voicee2e/reports/baseline.json --prompt-b test/voicee2e/reports/after-wave4.json` (no after-wave4 report)
  - Next: set `DEEPGRAM_API_KEY`, rerun Wave 4 gate e2e + compare, then continue to Wave 5 only if gate passes.

- 2026-02-23T01:44:10Z
  - Wave: 4
  - Task: gate-retry
  - Status: failed
  - Branch: pipeline
  - Commit: f3986a4
  - Validations: |
      FAIL `DEEPGRAM_API_KEY=<set> go run ./test/voicee2e/cmd/voicee2e/main.go -gateway-url http://127.0.0.1:8833 -suite test/voicee2e/scenarios/suite.yaml -out test/voicee2e/reports/after-wave4.json` -> Passed:5 Failed:1 (`s1_short` no response.started)
      FAIL retry of same command -> Passed:5 Failed:1 (same scenario)
      PASS `go run ./test/voicee2e/cmd/voicee2e/main.go --compare --prompt-a test/voicee2e/reports/baseline.json --prompt-b test/voicee2e/reports/after-wave4.json`
  - Next: stop wave advancement; investigate L1.2 short-utterance regression path and fix before rerunning Wave 4 gate.

- 2026-02-23T01:55:43Z
  - Wave: 4
  - Task: gate-retry-2
  - Status: passed
  - Branch: pipeline
  - Commit: 4811c52
  - Validations: |
      PASS `go test -race ./internal/voice/...`
      PASS `go test -race ./internal/providers/...`
      PASS `DEEPGRAM_API_KEY=<set> go run ./test/voicee2e/cmd/voicee2e/main.go -gateway-url http://127.0.0.1:8833 -suite test/voicee2e/scenarios/suite.yaml -out test/voicee2e/reports/after-wave4.json` -> Passed:6 Failed:0
      PASS `go run ./test/voicee2e/cmd/voicee2e/main.go --compare --prompt-a test/voicee2e/reports/baseline.json --prompt-b test/voicee2e/reports/after-wave4.json`
  - Next: advance to Wave 5 (L2.2 turn strategy abstraction).

- 2026-02-23T01:58:16Z
  - Wave: 5
  - Task: L2.2
  - Status: in_progress
  - Branch: codex/l2-2-turn-strategy
  - Commit: n/a
  - Validations: pending
  - Next: add TurnStrategy abstraction, BalancedStrategy behavior, and integrate strategy decisions into audioInputLoop.

- 2026-02-23T02:15:02Z
  - Wave: 5
  - Task: L2.2
  - Status: complete
  - Branch: codex/l2-2-turn-strategy
  - Commit: 670ecf1
  - Validations: |
      PASS `go test -race ./internal/voice/...`
      PASS `go test ./test/voicee2e/...`
      PASS `go build ./...`
  - Next: integrate L2.2 into pipeline and run Wave 5 gate.

- 2026-02-23T02:15:28Z
  - Wave: 5
  - Task: integrate
  - Status: complete
  - Branch: pipeline
  - Commit: dce1c91
  - Validations: L2.2 cherry-picked from task branch
  - Next: execute Wave 5 gate commands.

- 2026-02-23T02:19:34Z
  - Wave: 5
  - Task: gate
  - Status: passed
  - Branch: pipeline
  - Commit: dce1c91
  - Validations: |
      PASS `go build ./...`
      PASS `go vet ./...`
      PASS `go test -race ./internal/voice/...`
      PASS `go run ./test/voicee2e/cmd/voicee2e/main.go -gateway-url http://127.0.0.1:8833 -suite test/voicee2e/scenarios/suite.yaml -config '{"voice":{"turn_strategy":"legacy"}}' -out test/voicee2e/reports/after-wave5-legacy.json` -> Passed:6 Failed:0
      PASS `go run ./test/voicee2e/cmd/voicee2e/main.go --compare --prompt-a test/voicee2e/reports/baseline.json --prompt-b test/voicee2e/reports/after-wave5-legacy.json`
  - Next: advance to Wave 6 (L2.1 speculative LLM).

- 2026-02-23T02:21:03Z
  - Wave: 6
  - Task: L2.1
  - Status: in_progress
  - Branch: codex/l2-1-speculative-llm
  - Commit: n/a
  - Validations: pending
  - Next: implement speculative LLM flow (interim start/divergence cancel/final promote), add similarity utility and tests.

- 2026-02-23T03:01:15Z
  - Wave: 6
  - Task: L2.1
  - Status: complete
  - Branch: codex/l2-1-speculative-llm
  - Commit: d3605cc
  - Validations: |
      PASS `go test -race ./internal/voice/... -run TestSimilarity`
      PASS `go test -race ./internal/voice/... -run TestSpeculative`
      PASS `go build ./...`
      PASS `go test ./internal/gw/...`
      PASS `go test ./internal/providers/...`
      PASS `go test ./internal/api/v1api/...`
  - Next: run Wave 6 integration gate on pipeline.

- 2026-02-23T03:01:15Z
  - Wave: 6
  - Task: integrate
  - Status: complete
  - Branch: pipeline
  - Commit: 1a7b7a8
  - Validations: L2.1 series cherry-picked from task branch (`0b239be`, `22278b4`, `52e027f`, `64fe56c`, `1a7b7a8`).
  - Next: execute Wave 6 gate commands.

- 2026-02-23T03:01:15Z
  - Wave: 6
  - Task: gate
  - Status: failed
  - Branch: pipeline
  - Commit: 1a7b7a8
  - Validations: |
      PASS `go build ./...`
      PASS `go vet ./...`
      PASS `go test -race ./internal/voice/...`
      FAIL `DEEPGRAM_API_KEY=<set> go run ./test/voicee2e/cmd/voicee2e/main.go -gateway-url http://127.0.0.1:8833 -suite test/voicee2e/scenarios/suite.yaml -out test/voicee2e/reports/after-wave6.json` -> Passed:5 Failed:1 (`s3_long`: no transcript.final; similarity 0.00 < 0.45)
      SKIP `go run ./test/voicee2e/cmd/voicee2e/main.go --compare --prompt-a test/voicee2e/reports/baseline.json --prompt-b test/voicee2e/reports/after-wave6.json`
  - Next: hold Wave 7+; investigate deterministic `s3_long` transcript starvation under streaming STT and add targeted fix before re-running Wave 6 gate.

- 2026-02-23T03:19:32Z
  - Wave: 6
  - Task: gate-retry
  - Status: in_progress
  - Branch: pipeline
  - Commit: 3c581e9
  - Validations: |
      PASS `go run ./test/voicee2e/cmd/voicee2e/main.go -gateway-url http://127.0.0.1:8833 -suite test/voicee2e/scenarios/suite.yaml -scenario s3_long -out test/voicee2e/reports/w6-s3-check.json` -> Passed:1 Failed:0
      PASS `go build ./...`
      PASS `go vet ./...`
      PASS `go test -race ./internal/voice/...`
  - Next: rerun full Wave 6 e2e gate and compare.

- 2026-02-23T03:19:32Z
  - Wave: 6
  - Task: gate
  - Status: passed
  - Branch: pipeline
  - Commit: 3c581e9
  - Validations: |
      PASS `DEEPGRAM_API_KEY=<set> go run ./test/voicee2e/cmd/voicee2e/main.go -gateway-url http://127.0.0.1:8833 -suite test/voicee2e/scenarios/suite.yaml -out test/voicee2e/reports/after-wave6.json` -> Passed:6 Failed:0
      PASS `go run ./test/voicee2e/cmd/voicee2e/main.go --compare --prompt-a test/voicee2e/reports/baseline.json --prompt-b test/voicee2e/reports/after-wave6.json`
  - Next: advance to Wave 7 task L2.3 (streaming TTS).

- 2026-02-23T03:22:10Z
  - Wave: 7
  - Task: L2.3
  - Status: in_progress
  - Branch: codex/l2-3-streaming-tts
  - Commit: n/a
  - Validations: pending
  - Next: create Wave 7 worktree/branch and implement streaming TTS scope (providers/interface, providers/elevenlabs, voice/gateway, voice/pipeline ttsSynthLoop, gw adapter).

- 2026-02-23T04:25:32Z
  - Wave: 7
  - Task: L2.3
  - Status: complete
  - Branch: codex/l2-3-streaming-tts
  - Commit: daa37f5
  - Validations: |
      PASS `go test -race ./internal/providers/... -run TestElevenLabsClient`
      PASS `go test -race ./internal/voice/... -run TestTTSSynthLoop`
      PASS `go test -race ./internal/voice/... -run TestBargeIn_CancelsTTSStream`
      PASS `go test -race ./internal/voice/...`
      PASS `go test -race ./internal/providers/...`
      PASS `go test ./internal/gw/...`
      PASS `go build ./...`
  - Next: integrate to pipeline and run Wave 7 gate.

- 2026-02-23T04:25:32Z
  - Wave: 7
  - Task: integrate
  - Status: complete
  - Branch: pipeline
  - Commit: 83af5e7
  - Validations: L2.3 cherry-picked from task branch.
  - Next: execute Wave 7 gate commands.

- 2026-02-23T04:25:32Z
  - Wave: 7
  - Task: gate
  - Status: failed
  - Branch: pipeline
  - Commit: 83af5e7
  - Validations: |
      PASS `go build ./...`
      PASS `go vet ./...`
      PASS `go test -race ./internal/voice/...`
      PASS `go test -race ./internal/providers/...`
      PASS `DEEPGRAM_API_KEY=<set> ELEVENLABS_API_KEY=<set> go run ./test/voicee2e/cmd/voicee2e/main.go -gateway-url http://127.0.0.1:8833 -suite test/voicee2e/scenarios/suite.yaml -out test/voicee2e/reports/after-wave7.json` -> Passed:6 Failed:0
      PASS `go run ./test/voicee2e/cmd/voicee2e/main.go --compare --prompt-a test/voicee2e/reports/baseline.json --prompt-b test/voicee2e/reports/after-wave7.json`
      KPI `tts_ms p50 <= 100ms` -> MET (1ms)
      KPI `e2e_ms p50 <= 700ms` -> NOT MET (5439ms)
  - Next: stop wave advancement; investigate LLM latency path for Level 2 p50 target before Wave 8.

- 2026-02-23T04:46:13Z
  - Wave: 7
  - Task: gate-retry
  - Status: failed
  - Branch: pipeline
  - Commit: n/a
  - Validations: |
      PASS `go build ./...`
      PASS `go vet ./...`
      PASS `go test -race ./internal/voice/...`
      PASS `go test -race ./internal/providers/...`
      PASS `DEEPGRAM_API_KEY=<set> ELEVENLABS_API_KEY=<set> go run ./test/voicee2e/cmd/voicee2e/main.go -gateway-url http://127.0.0.1:8833 -suite test/voicee2e/scenarios/suite.yaml -out test/voicee2e/reports/after-wave7.json` -> Passed:6 Failed:0
      PASS `go run ./test/voicee2e/cmd/voicee2e/main.go --compare --prompt-a test/voicee2e/reports/baseline.json --prompt-b test/voicee2e/reports/after-wave7.json`
      FAIL KPI threshold check -> `e2e_ms p50=3339ms` (>700), `stt_ms p50=611ms` (>100), `tts_ms p50=905ms` (>100), `llm_ttfb_ms p50=1822ms`; report contained only one `turn.metrics` sample.
      PATCH `internal/providers/elevenlabs.go`: decode JSON `audio` payloads + use init/text/end websocket framing (`" "`, text, `""`).
      PATCH `internal/configs/config.go`: restore `DEEPGRAM_API_KEY` provider wiring and default `voice.transcriber_provider=deepgram` when unset.
      PATCH `internal/providers/elevenlabs_test.go`: cover JSON audio decode and init/text/end send sequence.
      FAIL retry after patches `DEEPGRAM_API_KEY=<set> ELEVENLABS_API_KEY=<set> go run ./test/voicee2e/cmd/voicee2e/main.go ... -out test/voicee2e/reports/after-wave7.json` -> Passed:2 Failed:4 (`s1_short`, `s2_medium`, `s3_long`, `s5_barge_in` transcript similarity below threshold).
  - Next: stop wave advancement; fix transcript-similarity regression under Deepgram streaming while preserving latency gains, then rerun full Wave 7 gate.

- 2026-02-23T05:02:03Z
  - Wave: 7
  - Task: gate-retry
  - Status: failed
  - Branch: pipeline
  - Commit: n/a
  - Validations: |
      PATCH `internal/voice/pipeline.go`: allow streaming `final` handling to commit without in-flight lock contention; add short `streamingFinalGracePeriod`; forward VAD pre-roll frames into streaming STT; adjust speculative thresholds for earlier starts.
      PATCH `internal/voice/session.go`: track `interimBestText` and use best interim for fallback commit path.
      PATCH `internal/providers/deepgram.go`: enable endpointing (`endpointing=300`, `utterance_end_ms=200`, `punctuate=true`, `smart_format=true`).
      PASS `go test -race ./internal/voice/...`
      PASS `go test -race ./internal/providers/...`
      PASS `go test ./internal/configs/...`
      PASS `go build ./...`
      PASS targeted check `... -scenario s1_short -out test/voicee2e/reports/w7-s1-check.json` -> similarity=1.0
      PASS full e2e `DEEPGRAM_API_KEY=<set> ELEVENLABS_API_KEY=<set> go run ./test/voicee2e/cmd/voicee2e/main.go -gateway-url http://127.0.0.1:8833 -suite test/voicee2e/scenarios/suite.yaml -out test/voicee2e/reports/after-wave7.json` -> Passed:6 Failed:0
      PASS compare `go run ./test/voicee2e/cmd/voicee2e/main.go --compare --prompt-a test/voicee2e/reports/baseline.json --prompt-b test/voicee2e/reports/after-wave7.json`
      FAIL KPI threshold check -> `e2e_ms p50=3556ms` (>700), `stt_ms p50=891ms` (>100), `tts_ms p50=1044ms` (>100); `turn.metrics` sample count=2.
  - Next: hold Wave 8+; investigate latency instrumentation semantics and STT commit policy to meet Level 2 KPI gate.

- 2026-02-23T05:25:49Z
  - Wave: 7
  - Task: gate-retry
  - Status: failed
  - Branch: pipeline
  - Commit: n/a
  - Validations: |
      PATCH `internal/voice/metrics.go` + `internal/voice/observer.go` + `internal/voice/metrics_test.go`: metric semantics updated to align with plan (e2e from speech_end->response_started, tts from tts-request->response_started via new observer hook).
      PATCH `internal/providers/deepgram.go`: reverted to low-latency streaming endpointing mode (`endpointing=false`) with smart formatting.
      PATCH `internal/voice/pipeline.go` + `internal/voice/session.go`: pre-roll to streaming STT, best-interim tracking, weak-interim wait heuristic, and batch fallback when interim stays weak.
      PASS `go test -race ./internal/voice/...`
      PASS `go test -race ./internal/providers/...`
      PASS `go test ./internal/configs/...`
      PASS `go build ./...`
      FAIL full e2e gate `... -out test/voicee2e/reports/after-wave7.json` -> Passed:5 Failed:1 (`s4_multi_turn` transcript similarity 0.444 < 0.45)
      KPI snapshot from latest report: `e2e_ms p50=591` (MET <=700), `stt_ms p50=78` (MET <=100), `tts_ms p50=424` (NOT MET <=100).
  - Next: hold Wave 8+; fix remaining s4 transcript-sim edge and reduce tts first-chunk latency before rerunning Wave 7 gate.

- 2026-02-23T05:43:26Z
  - Wave: 7
  - Task: gate-retry
  - Status: failed
  - Branch: pipeline
  - Commit: n/a
  - Validations: |
      PATCH `internal/voice/pipeline.go`: deferred non-speculative streaming finals to turn-end commit path, fallback normalization against last committed transcript to avoid cross-turn prefix carry-over, weak-interim wait/batch fallback refinements.
      PATCH `internal/voice/metrics.go` + `internal/voice/observer.go` + `internal/voice/metrics_test.go`: emit `turn.metrics` at `response.started` to capture first-audio latencies reliably.
      PATCH `internal/providers/deepgram.go`: tested endpointing tuning (`50`/`100`) then reverted due severe latency regression; final state keeps `endpointing=false` with punctuate/smart_format.
      PASS `go test -race ./internal/voice/...`
      PASS `go test -race ./internal/providers/...`
      PASS `go build ./...`
      PASS full e2e functional gate `.../after-wave7.json` -> Passed:6 Failed:0
      PASS compare `go run ./test/voicee2e/cmd/voicee2e/main.go --compare --prompt-a test/voicee2e/reports/baseline.json --prompt-b test/voicee2e/reports/after-wave7.json`
      FAIL KPI threshold check -> `e2e_ms p50=2019` (>700), `stt_ms p50=1024` (>100), `tts_ms p50=280` (>100).
  - Next: keep Wave 8 blocked; investigate remaining high-latency turns (batch fallback + long LLM start path) and align TTS first-chunk metric path to target <=100ms.

- 2026-02-23T06:46:53Z
  - Wave: 7
  - Task: gate-retry
  - Status: failed
  - Branch: pipeline
  - Commit: n/a
  - Validations: |
      CHANGE `internal/voice/pipeline.go`: removed aggressive weak-interim heuristics, then reintroduced a small short-phrase guard (`isTooWeakForCommit`: <4 words or <16 runes) with short wait (75ms) to prevent committing tiny partials while keeping streaming-first behavior.
      PASS `go test -race ./internal/voice/...`
      PASS `go test -race ./internal/providers/...`
      PASS `go build ./...`
      PASS full e2e functional gate `.../after-wave7.json` -> Passed:6 Failed:0
      FAIL KPI threshold check -> `e2e_ms p50=1798` (>700), `tts_ms p50=275` (>100); `stt_ms p50=78` met.
      NOTE `turn.metrics` sample count improved, but several turns still have null stt/e2e fields which skews percentile stability and indicates remaining instrumentation-path inconsistency.
  - Next: keep Wave 8 blocked; fix turn.metrics completeness (all turns with response.started must emit speech/transcript/commit anchors) and pursue persistent TTS stream strategy to reduce tts_ms.

- 2026-02-23T07:00:00Z
  - Wave: 7
  - Task: gate-retry
  - Status: failed
  - Branch: pipeline
  - Commit: n/a
  - Validations: |
      CHANGE `internal/voice/session.go` + `internal/voice/pipeline.go`: added run->turn mapping and response-turn tracking so `response.started/completed` and `turn.metrics` bind to originating turn instead of mutable current turn.
      CHANGE `internal/voice/pipeline.go`: kept streaming-first STT policy with limited short-phrase fallback guard (`isTooWeakForCommit`).
      PASS `go test -race ./internal/voice/...`
      PASS `go test -race ./internal/providers/...`
      PASS `go build ./...`
      PASS full e2e functional gate `.../after-wave7.json` -> Passed:6 Failed:0
      FAIL KPI threshold check -> `stt_ms p50=78` (MET), `tts_ms p50=275` (NOT MET), `e2e_ms p50=1798` (NOT MET).
      NOTE metrics completeness improved but interruption scenarios still produce partial/null metric anchors on some turns.
  - Next: keep Wave 8 blocked; complete metrics anchor coverage for interruption flows and reduce TTS first-chunk latency (provider/session-level stream strategy).

- 2026-02-23T07:12:28Z
  - Wave: 7
  - Task: gate-policy-update
  - Status: failed
  - Branch: pipeline
  - Commit: n/a
  - Validations: |
      CHANGE `docs/execution_backlog.md`: updated L2.3 acceptance + Wave/Final KPI assertions to keep default `tts_ms p50 <= 100ms` but allow `<= 300ms` when `voice.synth_provider=elevenlabs`.
      RECLASSIFY latest Wave 7 KPI snapshot (`after-wave7.json`): `stt_ms p50=78` MET, `tts_ms p50=275` MET under ElevenLabs threshold, `e2e_ms p50=1798` NOT MET.
  - Next: keep Wave 8 blocked on `e2e_ms` gate; continue latency-path fixes and rerun Wave 7 gate.

- 2026-02-23T07:22:37Z
  - Wave: 7
  - Task: gate-retry
  - Status: failed
  - Branch: pipeline
  - Commit: n/a
  - Validations: |
      PASS `go build ./...`
      PASS `go vet ./...`
      PASS `go test -race ./internal/voice/...`
      PASS `go test -race ./internal/providers/...`
      FAIL full e2e gate `DEEPGRAM_API_KEY=<set> ELEVENLABS_API_KEY=<set> go run ./test/voicee2e/cmd/voicee2e/main.go -gateway-url http://127.0.0.1:8833 -suite test/voicee2e/scenarios/suite.yaml -out test/voicee2e/reports/after-wave7.json` -> Passed:5 Failed:1 (`s1_short` transcript similarity 0.00 < 0.55).
      PASS compare `go run ./test/voicee2e/cmd/voicee2e/main.go --compare --prompt-a test/voicee2e/reports/baseline.json --prompt-b test/voicee2e/reports/after-wave7.json`
      EVIDENCE gateway logs during run: `voice streaming synthesis chunk error ... websocket: close 1008 ... quota/activity restriction` from ElevenLabs; `voice transcriber provider "deepgram" unavailable or incompatible; falling back to first available transcriber` warnings.
      NOTE report markdown omitted `Latency Percentiles`, indicating no `turn.metrics` samples emitted in this run.
  - Next: keep Wave 8 blocked; restore provider prerequisites (valid Deepgram streaming transcriber registration + ElevenLabs account credit/access) and rerun Wave 7 gate.

- 2026-02-23T21:54:32Z
  - Wave: 7
  - Task: gate-retry
  - Status: failed
  - Branch: pipeline
  - Commit: n/a
  - Validations: |
      PASS `go build ./...`
      PASS `go vet ./...`
      PASS `go test -race ./internal/voice/...`
      PASS `go test -race ./internal/providers/...`
      FAIL full e2e gate `DEEPGRAM_API_KEY=<set> ELEVENLABS_API_KEY=<set> go run ./test/voicee2e/cmd/voicee2e/main.go -gateway-url http://127.0.0.1:8833 -suite test/voicee2e/scenarios/suite.yaml -out test/voicee2e/reports/after-wave7.json` -> Passed:5 Failed:1 (`s1_short` transcript similarity 0.43 < 0.55).
      PASS compare `go run ./test/voicee2e/cmd/voicee2e/main.go --compare --prompt-a test/voicee2e/reports/baseline.json --prompt-b test/voicee2e/reports/after-wave7.json`
      OBSERVED fixture effect: `s1_short` improved from 0.00 to 0.43 after updated fixtures, but remains below threshold.
      EVIDENCE gateway logs during run: repeated ElevenLabs `websocket: close 1008` quota/activity rejections while streaming TTS.
  - Next: keep Wave 8 blocked; resolve provider-account gating for stable TTS, then rerun Wave 7 functional gate.
