# TODO

## Priority: Next Steps

These are the highest-impact items to tackle next, roughly in order.

### 1. Agent Tool Discovery & Self-Documentation
- [ ] Tool search tool (let agents discover available tools by name/description search)
- [ ] TeaNode documentation tool (let agents explore TeaNode's own docs, config schema, API surface)

### 2. Robustness & Reliability
- [ ] Add context timeouts to non-shell tool execution (shell tool already has timeouts)

### 3. Security Hardening
- [ ] Add rate limiting to general API endpoints (auth endpoints already have per-IP rate limiting)

### 4. Test Coverage Expansion
- [ ] `internal/api/v1api` handler tests (HTTP handlers, SSE streaming, auth middleware, media endpoints)
- [ ] WebSocket RPC handler tests (`conversations.send`, `conversations.history`, etc.)
- [ ] `internal/web` tests (embedded SPA serving, static assets)
- [ ] CLI command tests in `cmd/` (node, terminal flag parsing and wiring)
- [ ] Audio transcription/synthesis tests (OpenAI Transcribe, Synthesize methods, REST endpoints)
- [ ] Frontend component and route tests (`useBackend.test.ts` exists; no component/integration tests yet)
- [ ] Concurrent access / stress tests for parallel requests
- [ ] Edge-case tests (malformed JSON, truncated SSE streams, oversized payloads)

### 5. Provider Ecosystem
- [ ] Google Gemini provider
- [ ] Provider failover (multiple API keys / auth profiles with fallback)
- [ ] OAuth-based provider auth (Anthropic Pro/Max, OpenAI)
- [ ] Per-model tool gating (enable/disable tools based on model capabilities)

### 6. Voice & Audio (next phase)
- [ ] Streaming TTS (chunked audio playback for long responses)
- [ ] TTS as an agent tool (let the agent proactively speak / send audio clips)
- [ ] Voice activity detection (auto-start/stop recording without button)
- [ ] Realtime voice conversation mode (bidirectional audio streaming)

### 7. New Messaging Channels
- [ ] Slack channel integration
- [ ] WhatsApp channel integration

---

## Completed

### Testing
Core infrastructure packages have high test coverage:
- `internal/providers` ~92% (Anthropic, OpenAI, registry, NewProvider factory)
- `internal/jobs` ~89% (store, types/frontmatter, scheduler with cron dedup, one-shot delays, tools API)
- `internal/sessions` ~95% (store: create, get, touch, delete, list)
- `internal/util/security` ~92% (GenerateRandom, GenerateRandomString, HashPassword, VerifyPassword, NewULID)
- `internal/media` — sharded storage, metadata sidecars, legacy compat, orphan cleanup (27 tests)
- `internal/configs` — config loading, file parsing, defaults
- `internal/skills` — registration, tool spec building, loader
- `internal/agents` — tool registration, runner, context handling
- `internal/tools/{github,gitlab,google,claudecode,homeassistant,unifiprotect}` — registration and spec tests
- `web/src/hooks/useBackend.test.ts` — WebSocket RPC client hook

### Robustness
- [x] Interrupted tool-call recovery (synthetic tool results for unanswered calls in `runner.go`)
- [x] Sync LLM call timeouts (90s non-streaming, 20s model list in `providers/timeouts.go`)
- [x] Mic button visible during agent run (voice input while running, queued like typed messages)

### Error Handling
- [x] Structured JSON error responses across all HTTP handlers (`internal/web/errors.go`)
- [x] Handle marshal errors in `internal/provider/openai.go`
- [x] Surface errors on malformed JSON during streaming parse

### Logging & Observability
- [x] Replace `log.Println` with structured logging (go-logging with levels)
- [x] Add debug logging for tool execution and LLM requests

### Security
- [x] Forwarder key middleware for secure reverse proxy deployments (X-Forwarded-For trust)
- [x] Per-IP rate limiting on auth endpoints (token bucket in `auth.go`)
- [x] CORS origin restriction (origin validation via `isWebSocketOriginAllowed()` in `websocket.go`)
- [x] Cookie-based WebSocket auth (HttpOnly session cookies, no query param token)
- [x] Tool policies (allowlist/denylist per agent or group)

### Features
- [x] Support multiple LLM providers (provider registry with name-qualified models)
- [x] Implement graceful shutdown (signal.NotifyContext with SIGTERM)
- [x] Add configuration hot-reload without restart (file watcher on config, agents, skills, crons)
- [x] Model list caching with 24-hour TTL and disk persistence (auto-invalidated on config reload)
- [x] Media store for image storage and serving (base64 extraction from tool results, `/api/v1/media`)
- [x] Version info injection via ldflags (Server header, build metadata)
- [x] Multimodal / vision support (image attachments in messages, provider content parts, media upload endpoint)

### Audio / Voice
- [x] Provider capability interfaces (`AudioTranscriber`, `AudioSynthesizer` in `providers/interface.go`)
- [x] OpenAI implementation (Whisper STT, TTS-1 synthesis in `openai.go`)
- [x] Registry capability lookup (`FindTranscriber`, `FindSynthesizer`)
- [x] REST endpoints (`POST /api/v1/audio/transcribe`, `POST /api/v1/audio/synthesize`)
- [x] Capability gating (`audio` in connect response capabilities array)
- [x] Frontend recording hook (`useAudioRecorder.ts` — MediaRecorder, iOS mp4 fallback)
- [x] Frontend TTS hook (`useTTS.ts` — reused HTMLAudioElement for iOS Safari)
- [x] Chat UI integration (mic button, recording indicator, auto-read after voice send)
- [x] Voice preferences (auto-send toggle, TTS voice selector in PreferencesArea)
- [x] Discord & Telegram channel attachment support (voice, photo, document)

### Agent Tools
- [x] Web search tool (Brave Search API)
- [x] Web fetch / URL reading tool (HTML → markdown extraction)
- [x] Shell command execution tool (sh -c with timeout, output truncation, exit code reporting)
- [x] Memory read/write/edit/search tools (workspace-scoped filesystem)
- [x] General filesystem tools (read, write, list, info, mkdir, delete, move)
- [x] Cron / scheduled tasks tool (create, list, update, delete, trigger)
- [x] One-shot reminders via cron (delay parameter, session-bound, auto-delete)
- [x] Browser tools (navigate, screenshot, snapshot, click, type, evaluate, tab management)
- [x] Headless browser support (direct CDP connection to chromedp/headless-shell)
- [x] Terminal tools (screenshot, type, press key, connection list)
- [x] Conversation tools (list, compact)
- [x] GitHub tools (repos, issues, pulls, actions, releases, search)
- [x] GitLab tools (projects, issues, merge requests, pipelines, releases)
- [x] Google tools (calendar, contacts, drive, gmail, tasks)
- [x] Claude Code tool
- [x] Home Assistant tools (entity control, state queries, access control, read-only mode)
- [x] UniFi Protect tools (camera snapshots, events, device info, dual auth)

### Multi-Agent & Routing
- [x] Multi-agent support (multiple agent configs with separate workspaces)
- [x] Agent routing (route requests to different agents based on channel/context)
- [x] Inter-agent messaging (agent_list, agent_message tools with permission control)
- [x] Subagent spawning (subagent_spawn tool in `interagent.go`)

### Messaging Channels
- [x] Telegram channel integration (per-chat sessions, model overrides, slash commands)
- [x] Discord channel integration (per-channel sessions, model overrides, slash commands)
- [x] Channel-level routing to specific agents

### Conversation Management
- [x] Conversation state patch (per-channel model overrides in Discord/Telegram)
- [x] Conversation pruning / context compaction (summarize old messages)
- [x] JSONL-based persistent conversation storage with titles
- [x] Background conversation summarizer (auto-generate titles and summaries)
- [x] Configurable summarizer settings (timing, thresholds, char limits via schema)

### Automation
- [x] Cron job scheduler with 5-field expression support (5s tick with deduplication to prevent double-fires)
- [x] Persistent cron storage with hot-reload
- [x] Per-job model overrides and manual triggering
- [x] One-shot reminder support (delay-based timers, conversation-bound, auto-cleanup)

### System Prompt & Context
- [x] Modular system prompt builder (template-based composable sections)
- [x] Runtime info injection (date, time, timezone)
- [x] Memory/workspace context loading (AGENT.md, MEMORY.md, SKILLS.md)
- [x] Skill prompt injection into system prompt
- [x] Context compaction / summarization for long conversations
- [x] Schema-driven config defaults (single source of truth in JSON schemas)

### CLI
- [x] Node command (`teanode node` with port flag)
- [x] Terminal command (`teanode terminal` with PTY relay and machine info)
- [x] Global flags (`--dir`, `--log-level` with env var support)

### Frontend
- [x] React/TypeScript SPA with WebSocket RPC client
- [x] Conversation UI with streaming responses
- [x] Job scheduling interface
- [x] Agent editor
- [x] Settings and preferences
- [x] Voice input and TTS playback
- [x] Mobile-responsive layout (breakpoints, edge swipe sidebar, responsive drawer)

### Plugin / Extension System
- [x] Skills system (JSON-defined tools with shell and HTTP execution)
- [x] Hot-reloadable skill loading from `~/.teanode/skills/`
- [x] Chrome extension for browser relay (Manifest v3)

---

## Backlog

Lower priority or longer-term items.

### Features
- [ ] Deepen `/health` endpoint (check workspace availability, provider reachability)
- [ ] Optimize `workspace_list` tool (caching or streaming instead of full tree walk)
- [ ] Queue modes for concurrent requests (serial, parallel, drop)
- [ ] Expand runtime info in system prompt (OS, architecture, shell type, hostname — username/timezone/homedir already injected)

### Security & Sandboxing
- [ ] Tool approval workflows (user confirms before sensitive tool execution)
- [ ] Docker-based sandbox for tool execution (per-conversation containers)

### Automation
- [ ] Webhook endpoints for external event triggers
- [ ] Background job lifecycle management

### CLI
- [ ] Interactive onboarding wizard (`teanode onboard`)
- [ ] Configuration management commands (`teanode config`)
- [ ] Health check / diagnostics command (`teanode doctor`)

### Frontend
- [ ] Keyboard shortcuts

### Plugin / Extension System
- [ ] Plugin SDK for registering custom tools, providers, and hooks

### Documentation
- [ ] Document OpenAI-compatible API surface and any deviations
- [ ] Document WebSocket RPC frame format and available methods
- [ ] Add deployment / production setup guide
- [ ] Add memory tool usage examples for agent authors
