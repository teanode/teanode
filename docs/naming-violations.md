# Naming Convention Violations

Full codebase audit against the naming conventions defined in `README.md`. Auto-generated files (`models_gen.go`, `models_generate.go`) and vendor code are excluded.

---

## 1. Acronyms Fully Capitalized in Lowercase-Starting Identifiers

The convention requires that when the first alphabetical character of an identifier is **lowercase**, acronyms use **only first-letter capitalization** (e.g. `sessionId`, `baseUrl`, `readSse`). These identifiers incorrectly use fully-capitalized acronyms.

### `internal/api/v1api/auth.go`

| Line | Current | Correct |
|------|---------|---------|
| 240 | `sessionID` (parameter) | `sessionId` |
| 251 | `getSessionByID` | `getSessionById` |
| 251 | `sessionID` (parameter) | `sessionId` |

### `internal/api/v1api/rpc.go`

| Line | Current | Correct |
|------|---------|---------|
| 42 | `avatarMediaID` | `avatarMediaId` |
| 98 | `avatarMediaID` | `avatarMediaId` |
| 139 | `agentAvatarMediaID` (method) | `agentAvatarMediaId` |
| 140 | `avatarMediaID` | `avatarMediaId` |
| 1182 | `tokenID` | `tokenId` |
| 1400 | `userID` | `userId` |
| 1874 | `projectID` | `projectId` |

### `internal/providers/anthropic.go`

| Line | Current | Correct |
|------|---------|---------|
| 98 | `anthropicSSEMessageStart` | `anthropicSseMessageStart` |
| 103 | `anthropicSSEContentBlockStart` | `anthropicSseContentBlockStart` |
| 109 | `anthropicSSEContentBlockDelta` | `anthropicSseContentBlockDelta` |
| 121 | `anthropicSSEMessageDelta` | `anthropicSseMessageDelta` |
| 297 | `systemJSON` | `systemJson` |
| 542 | `readSSE` (method) | `readSse` |

### `internal/providers/openai.go`

| Line | Current | Correct |
|------|---------|---------|
| 348 | `readSSE` (method) | `readSse` |

### `internal/providers/anthropic_test.go`

| Line | Current | Correct |
|------|---------|---------|
| 98 | `schemaJSON` | `schemaJson` |

### `internal/tools/claudecode/claudecode_test.go`

| Line | Current | Correct |
|------|---------|---------|
| 597 | `sessionsByID` | `sessionsById` |

### `internal/tools/codex/codex_test.go`

| Line | Current | Correct |
|------|---------|---------|
| 600 | `sessionsByID` | `sessionsById` |

### `internal/channels/discord/bot.go`

| Line | Current | Correct |
|------|---------|---------|
| 841 | `listAgentIDsFromStore` | `listAgentIdsFromStore` |
| 842 | `agentIDs` | `agentIds` |

### `internal/channels/telegram/bot.go`

| Line | Current | Correct |
|------|---------|---------|
| 939 | `chatID` | `chatId` |
| 980 | `listAgentIDsFromStore` | `listAgentIdsFromStore` |
| 981 | `agentIDs` | `agentIds` |
| 1001 | `chatID` | `chatId` |
| 1126 | `fileURL` | `fileUrl` |

### `internal/store/dbstore/dbmigrations/dbmigrations.go`

| Line | Current | Correct |
|------|---------|---------|
| 67 | `migrationIDs` | `migrationIds` |

### `internal/store/dbstore/database_migrate.go`

| Line | Current | Correct |
|------|---------|---------|
| 41 | `currentMigrationIDs` | `currentMigrationIds` |
| 46 | `unknownMigrationIDs` | `unknownMigrationIds` |

### `internal/store/fsstore/filesystem_records.go`

| Line | Current | Correct |
|------|---------|---------|
| 238 | `userIDs` | `userIds` |

### `internal/store/fsstore/filesystem_user.go`

| Line | Current | Correct |
|------|---------|---------|
| 55 | `userIDs` | `userIds` |
| 60 | `filteredUserIDs` | `filteredUserIds` |

### `internal/tools/homeassistant/homeassistant.go`

| Line | Current | Correct |
|------|---------|---------|
| 16 | `baseURL` (struct field) | `baseUrl` |

### `internal/tools/unifiprotect/unifiprotect.go`

| Line | Current | Correct |
|------|---------|---------|
| 16 | `baseURL` (struct field) | `baseUrl` |

### `internal/skills/tool.go`

| Line | Current | Correct |
|------|---------|---------|
| 161 | `DurationMs` (exported field) | `DurationMS` |

---

## 2. Abbreviations (Should Be Spelled Out)

The convention says to spell things out: "command" not "cmd", "response" not "resp", "configuration" not "config", etc.

### `config` → `configuration`

| File | Lines | Identifiers |
|------|-------|-------------|
| `internal/coordinators/coordinator.go` | 28, 65, 72 | struct field `config`, parameter `config` |
| `internal/tools/claudecode/claudecode.go` | 66, 79, 465 | `configFromContext`, `config` variable, `configTimeout` |
| `internal/tools/codex/codex.go` | 66, 78 | `configFromContext`, `config` variable |
| `internal/tools/configs/configs.go` | 23, 25 | type `configTool`, func `newConfigTool` |
| `internal/tools/workspace/workspace.go` | 99, 109, 112 | type `workspaceToolConfig`, field `config`, param `config` |
| `internal/tools/google/google.go` | 18, 25, 26 | `resolvedConfig`, `configFromContext`, `config` |
| `internal/tools/homeassistant/homeassistant.go` | 15, 26, 27, 38 | `resolvedConfig`, `configFromContext`, `config`, `haConfig` |
| `internal/tools/unifiprotect/unifiprotect.go` | 15, 28, 29, 40 | `resolvedConfig`, `configFromContext`, `config`, `upConfig` |
| `internal/store/fsstore/filesystem_records.go` | 129 | type `storeConfigRecord` |
| `internal/store/fsstore/filesystem_storage.go` | 13, 14, 24, 25 | `loadConfigRecord`, `configRecord`, `saveConfigRecord` |
| `internal/store/fsstore/filesystem_configuration.go` | 22, 33, 40, 161 | `configToModel`, `modelToConfig` |
| `internal/store/fsstore/filesystem_agent.go` | 41, 58, 112, 128 | `agentConfigToModel`, `modelToAgentConfig` |
| `internal/store/fsstore/filesystem_project.go` | 44, 114, 126 | `projectConfigToModel`, `modelToProjectConfig` |
| `internal/store/fsstore/filesystem_paths.go` | 13, 33, 61, 77 | `configFilename`, `userConfigFilename`, `agentConfigFilename`, `projectConfigFilename` |
| `internal/channels/telegram/bot.go` | 557 | `updateConfig` |
| `test/voicee2e/cmd/voicee2e/main.go` | 18, 63 | `cfg` variable |
| `test/voicee2e/internal/model/model.go` | 5 | `RunnerConfig` struct |
| `test/voicee2e/internal/runner/runner.go` | 26, 70 | `cfg` parameter |

### `info` → `information`

| File | Lines | Identifiers |
|------|-------|-------------|
| `internal/integrations/browsers/headlessbrowser/headlessbrowser.go` | 79, 114, 118, 487, 495 | `versionInfo`, `TargetInfos`, `info`, `targetInfo` |
| `internal/integrations/terminals/relay.go` | 35, 46, 214 | `MachineInfo`, `ConnectionInfo`, `info` |
| `internal/providers/anthropic.go` | 547, 642 | `blockInfo` type, `info` variable |
| `internal/providers/openai.go` | 209, 382 | `UsageInfo`, `ModelInfo` |
| `internal/tools/browser/browser.go` | 280, 644 | `info` variable, `keyInfo` function |
| `internal/tools/filesystem/filesystem.go` | 173, 257 | `fileInfo`, `info` |
| `internal/tools/claudecode/claudecode.go` | 50 | `sessionInfo` type |
| `internal/tools/codex/codex.go` | 50 | `sessionInfo` type |
| `internal/api/v1api/frames.go` | 63 | `voiceClientInfo` |
| `internal/store/fsstore/filesystem_workspace.go` | 151 | `fileInfos` |

### `params` → `parameters`

| File | Lines | Identifiers |
|------|-------|-------------|
| `internal/api/v1api/frames.go` | 68, 85, 89, 94 | `voiceStartParams`, `voiceEndParams`, `voiceResponseCancelParams`, `voiceInputCommitParams` |
| `internal/api/v1api/http.go` | 188 | `params` variable |
| `internal/coordinators/coordinator.go` | 49, 403, 408, 505 | `params` field and parameter |
| `internal/tools/browser/browser.go` | 283, 498 | `params` variable |
| `test/voicee2e/internal/protocol/client.go` | 25 | `Params` field |

### `args` → `arguments`

| File | Lines | Identifiers |
|------|-------|-------------|
| `internal/tools/claudecode/claudecode.go` | 36 | `args` in `commandRunner` type |
| `internal/tools/codex/codex.go` | 36, 66, 79, 435 | `args`, `extraArgs` |
| `internal/models/configurations.go` | 85 | `ExtraArgs` struct field |
| `internal/store/fsstore/filesystem_records.go` | 103 | `ExtraArgs` struct field |
| `internal/runners/runner.go` | 485 | `repairToolArgs` function |
| `internal/runners/runner_test.go` | 319, 370 | `toolArgs`, `args` |
| `test/voicee2e/internal/protocol/client.go` | 374 | `args` parameter |

### `cmd` → `command`

| File | Lines | Identifiers |
|------|-------|-------------|
| `internal/tools/google/tasks.go` | 85-92 | `cmdArgs` |
| `internal/tools/google/contacts.go` | 62-73 | `cmdArgs` |
| `internal/tools/google/drive.go` | 76-80 | `cmdArgs` |
| `internal/tools/google/calendar.go` | 114-122 | `cmdArgs` |
| `internal/tools/google/google_test.go` | many | `cmdArgs` |

### `msg` → `message`

| File | Lines | Identifiers |
|------|-------|-------------|
| `internal/tools/google/exec.go` | 53-57, 69, 81 | `errMsg`, `msg` |
| `internal/runners/compact_test.go` | 34 | `msg` |
| `test/voicee2e/internal/protocol/client.go` | 130 | `msgType` |

### `resp` → `response`

| File | Lines | Identifiers |
|------|-------|-------------|
| `internal/tools/homeassistant/homeassistant_test.go` | 19-21 | `serviceResp`, `historyResp`, `configResp` |

### `Len` → `Length`

| File | Lines | Identifiers |
|------|-------|-------------|
| `internal/channels/discord/bot.go` | 30 | `maxDiscordMessageLen` |
| `internal/channels/telegram/bot.go` | 31 | `maxTelegramMessageLen` |

### `Str` → `String`

| File | Lines | Identifiers |
|------|-------|-------------|
| `cmd/terminal.go` | 272 | `errStr` |
| `internal/channels/telegram/bot.go` | 590, 682, 804, 810 | `chatIdStr` |

### `Num` → `Number`

| File | Lines | Identifiers |
|------|-------|-------------|
| `internal/tools/claudecode/claudecode.go` | 513, 514 | `NumInputTokens`, `NumOutputTokens` |
| `internal/tools/codex/codex.go` | 506, 507 | `NumInputTokens`, `NumOutputTokens` |

### `Defs` → `Definitions`

| File | Lines | Identifiers |
|------|-------|-------------|
| `internal/runners/compact.go` | 108, 655 | `estimateToolDefsTokens`, `toolDefs` |

### `Addr` → `Address`

| File | Lines | Identifiers |
|------|-------|-------------|
| `internal/store/fsstore/filesystem_session.go` | 26 | `RemoteAddr` struct field |

### `Seq` → `Sequence`

| File | Lines | Identifiers |
|------|-------|-------------|
| `internal/api/v1api/frames.go` | 44 | `Seq` field |
| `internal/tools/terminal/terminal.go` | 177 | `seq` variable |
| `test/voicee2e/internal/protocol/client.go` | 73, 247 | `Seq` field, `frameSeq` |

### `buf` → `buffer`

| File | Lines | Identifiers |
|------|-------|-------------|
| `internal/util/screenbuffer/screenbuffer.go` | 52 | `csiBuf` |
| `internal/util/screenbuffer/screenbuffer_test.go` | many | `buf` variable |
| `internal/util/bufferpool/bufferpool_test.go` | many | `buf` variable |
| `internal/voice/binary.go` | 66 | `buf` variable |
| `internal/voice/vad_test.go` | 9 | `buf` variable |
| `internal/voice/pipeline_test.go` | 172 | `buf` variable |
| `test/voicee2e/internal/protocol/wav_test.go` | 57, 81 | `buf` variable |

### `mu` → `mutex`

| File | Lines | Identifiers |
|------|-------|-------------|
| `internal/voice/pipeline_test.go` | 21, 47 | `mu` field |

### `wg` → `waitGroup`

| File | Lines | Identifiers |
|------|-------|-------------|
| `internal/voice/session.go` | 62 | `wg` field |

### `ext` → `extension`

| File | Lines | Identifiers |
|------|-------|-------------|
| `internal/api/v1api/http.go` | 79, 150 | `ext` variable |
| `internal/providers/openai.go` | 451 | `ext` variable |

### `idx` → `index`

| File | Lines | Identifiers |
|------|-------|-------------|
| `internal/providers/registry.go` | 129 | `idx` variable |
| `internal/runners/compact_test.go` | 141 | `lastIdx` |

### `conn` → `connection`

| File | Lines | Identifiers |
|------|-------|-------------|
| `internal/integrations/browsers/relaybrowser/relaybrowser.go` | 53, 192 | `conn` variable |

### `rc` → `relayConnection`

| File | Lines | Identifiers |
|------|-------|-------------|
| `internal/integrations/browsers/relaybrowser/relaybrowser.go` | 64, 94, 107, 122, 134, 149, 161, 175, 186, 258, 299, 317, 335, 371 | `rc` variable |

### `tc` → `terminalConnection`

| File | Lines | Identifiers |
|------|-------|-------------|
| `internal/integrations/terminals/relay.go` | 116, 129, 217 | `tc` variable |

### `fd` → `fileDescriptor`

| File | Lines | Identifiers |
|------|-------|-------------|
| `internal/integrations/terminals/pty_linux.go` | 50, 56, 65, 84 | `fd` parameter |
| `internal/integrations/terminals/pty_darwin.go` | 69, 75, 84, 103 | `fd` parameter |
| `cmd/terminal.go` | 183 | `fd` variable |

### `orig` → `original`

| File | Lines | Identifiers |
|------|-------|-------------|
| `internal/integrations/terminals/pty_linux.go` | 66 | `orig` variable |
| `internal/integrations/terminals/pty_darwin.go` | 85 | `orig` variable |
| `internal/tools/claudecode/register_test.go` | 16, 38 | `origPath` |
| `internal/tools/codex/register_test.go` | 16, 38 | `origPath` |

### `dir` → `directory`

| File | Lines | Identifiers |
|------|-------|-------------|
| `internal/tools/claudecode/register_test.go` | 12 | `dir` variable |
| `internal/tools/codex/register_test.go` | 12 | `dir` variable |
| `test/voicee2e/internal/config/load_test.go` | 11 | `dir` variable |

### `att` → `attachment`

| File | Lines | Identifiers |
|------|-------|-------------|
| `internal/channels/discord/bot.go` | 877 | `att` variable |

### `sess` → `session`

| File | Lines | Identifiers |
|------|-------|-------------|
| `internal/api/v1api/websocket.go` | 151, 168 | `sess` variable |

### `dedup` → `deduplication`

| File | Lines | Identifiers |
|------|-------|-------------|
| `internal/api/v1api/websocket.go` | 190 | `dedupKey` |

### `sub` → `subrouter`

| File | Lines | Identifiers |
|------|-------|-------------|
| `internal/api/v1api/v1api.go` | 68 | `sub` variable |

### `sig` → `signal`

| File | Lines | Identifiers |
|------|-------|-------------|
| `cmd/gateway.go` | 437 | `sig` variable |

### `ch` → `channel`

| File | Lines | Identifiers |
|------|-------|-------------|
| `internal/util/pending/pending_test.go` | many | `ch` variable |
| `test/voicee2e/internal/protocol/client.go` | 108 | `ch` field |

### `ip`/`ips` → `ipAddress`/`ipAddresses`

| File | Lines | Identifiers |
|------|-------|-------------|
| `internal/web/middleware.go` | 118, 127 | `ip`, `ips` |

### Miscellaneous abbreviations

| File | Line | Current | Correct |
|------|------|---------|---------|
| `internal/api/v1api/frames.go` | 41 | `V` (field) | `Version` |
| `internal/api/v1api/frames.go` | 45 | `TSMS` | `TimestampMilliseconds` |
| `internal/api/v1api/frames.go` | 53 | `FrameMS` | `FrameMilliseconds` |
| `internal/api/v1api/frames.go` | 101 | `AudioSeqRef` | `AudioSequenceReference` |
| `internal/api/v1api/frames.go` | 124 | `RetryAfterMS` | `RetryAfterMilliseconds` |
| `internal/api/v1api/openai.go` | 201, 209 | `errData` | `errorData` |
| `internal/voice/pipeline.go` | 39 | `cp` | `frameCopy` |
| `internal/voice/pipeline.go` | 99 | `tid` | `turnId` |
| `internal/voice/pipeline.go` | 114 | `sub` | `subscriber` |
| `internal/voice/pipeline.go` | 202 | `rid` | `responseId` |
| `internal/runners/test_main_test.go` | 32 | `removeErr` | `removeError` |
| `internal/runners/compact_test.go` | 25 | `tt` | `testCase` |
| `test/voicee2e/cmd/voicee2e/main.go` | 52 | `mdPath` | `markdownPath` |
| `test/voicee2e/cmd/voicee2e/main.go` | 68 | `cand` | `candidate` |
| `test/voicee2e/cmd/voicee2e/main.go` | 80 | `comp` | `comparison` |
| `test/voicee2e/cmd/voicee2e/main.go` | 88 | `out` | `output` |
| `test/voicee2e/internal/model/model.go` | 9 | `OutPath` | `OutputPath` |
| `test/voicee2e/internal/model/model.go` | 16, 22 | `SuiteSpec`, `ScenarioSpec` | `SuiteSpecification`, `ScenarioSpecification` |
| `test/voicee2e/internal/model/model.go` | 25 | `TimeoutSec` | `TimeoutSeconds` |
| `test/voicee2e/internal/model/model.go` | 106 | `CandPath` | `CandidatePath` |
| `test/voicee2e/internal/protocol/client.go` | 93 | `wsUrl` | `websocketUrl` |
| `test/voicee2e/internal/protocol/client.go` | 112, 114 | `waitersMu`, `timelineMu` | `waitersMutex`, `timelineMutex` |
| `test/voicee2e/internal/protocol/client.go` | 153, 178 | `typ` | `frameType` |
| `test/voicee2e/internal/protocol/client.go` | 169, 170 | `env` | `envelope` |
| `test/voicee2e/internal/protocol/wav.go` | 119 | `in`, `inRate`, `outRate` | `input`, `inputRate`, `outputRate` |
| `test/voicee2e/internal/protocol/wav.go` | 127 | `outLen` | `outputLength` |
| `test/voicee2e/internal/protocol/wav.go` | 131 | `out` | `output` |
| `test/voicee2e/internal/protocol/wav.go` | 133, 134, 135 | `src`, `idx`, `frac` | `source`, `sourceIndex`, `fraction` |
| `test/voicee2e/internal/runner/runner.go` | 84 | `sctx` | `scenarioContext` |
| `test/voicee2e/internal/assertions/assertions.go` | 82, 83 | `ew`, `aw` | `expectedWords`, `actualWords` |
| `test/voicee2e/internal/report/report_test.go` | 30 | `md` | `markdown` |

---

## 3. Single-Letter Variables

The convention says "Avoid single letter variables." Only `err` and `ctx` are explicitly exempted.

### Loop variables `i`, `j`, `n`

Widespread across the codebase. High-frequency files:

| File | Lines | Variable |
|------|-------|----------|
| `internal/util/screenbuffer/screenbuffer.go` | 62, 286, 597, 604 | `r`, `c`, `i`, `b`, `a`, `b` |
| `internal/voice/vad.go` | 72, 73 | `i`, `n` |
| `internal/voice/vad_test.go` | 10, 19, 30, 45, 48 | `i` |
| `internal/voice/sentences.go` | 52, 65, 73, 87, 102, 116, 120 | `i`, `j`, `s`, `r` |
| `internal/providers/openai.go` | 421 | `i`, `j` |
| `test/voicee2e/internal/protocol/wav.go` | 28, 97, 132, 140, 141, 142 | `i`, `v0`, `v1` |
| `test/voicee2e/internal/protocol/wav_test.go` | 14 | `i` |
| `test/voicee2e/internal/report/report.go` | 13, 28 | `v`, `b` |
| `test/voicee2e/internal/assertions/assertions.go` | 54, 69, 99 | `v`, `s` |
| `internal/util/pending/pending_test.go` | 12, 119, 163, 169, 170, 178 | `r`, `i`, `p` |
| `internal/util/bufferpool/bufferpool_test.go` | 57, 61 | `n`, `i` |

### Test parameter `t *testing.T`

Many test files use `t *testing.T` while the project convention in other test files uses `testing *testing.T`. Files with `t` instead of `testing`:

- `internal/tools/github/github_test.go` (partial)
- `internal/tools/github/register_test.go`
- `internal/tools/gitlab/gitlab_test.go` (all test functions)
- `internal/tools/gitlab/register_test.go`
- `internal/tools/google/google_test.go` (all test functions)
- `internal/tools/google/register_test.go`
- `internal/tools/projects/tools_test.go`
- `internal/tools/claudecode/register_test.go`
- `internal/tools/codex/register_test.go`
- `internal/tools/datetime/datetime_test.go`
- `internal/tools/workspace/workspace_test.go`

---

## 4. Wrong Receiver Name

The convention requires `self` as the receiver name. These use a different name:

| File | Lines | Current | Correct |
|------|-------|---------|---------|
| `internal/voice/vad.go` | 24, 31 | `v` | `self` |
| `test/voicee2e/internal/runner/runner.go` | 26, 40, 41, 70, 87 | `r` | `self` |

---

## Summary by Priority

### High Priority (exported identifiers, public API surface)

- `MachineInfo` / `ConnectionInfo` types in `internal/integrations/terminals/relay.go`
- `UsageInfo` / `ModelInfo` types in `internal/providers/openai.go`
- `ExtraArgs` field in `internal/models/configurations.go`
- `RemoteAddr` field in `internal/store/fsstore/filesystem_session.go`
- `DurationMs` field in `internal/skills/tool.go` (should be `DurationMS`)

### Medium Priority (internal identifiers, broad impact)

- `config` abbreviation: ~40+ occurrences across 15+ files
- `info` abbreviation: ~20+ occurrences across 10+ files
- Acronym violations in `anthropic.go` / `openai.go` (`readSSE`, `systemJSON`, etc.)
- Acronym violations in `rpc.go` (`avatarMediaID`, `tokenID`, etc.)
- Acronym violations in channels (`agentIDs`, `chatID`, `fileURL`, etc.)

### Lower Priority (test files, local variables)

- `t *testing.T` inconsistency across test files
- Single-letter loop variables (`i`, `j`, etc.)
- Short-lived abbreviations in test helpers
