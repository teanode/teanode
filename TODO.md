# TODO

## Testing

- [ ] Add gateway HTTP handler tests (`/v1/chat/completions`, `/health`, auth middleware)
- [ ] Add WebSocket RPC handler tests (`chat.send`, `chat.history`, `sessions.list`, etc.)
- [ ] Add config loading tests (file parsing, env variable overrides, defaults)
- [ ] Add concurrent access / stress tests for parallel requests
- [ ] Add edge-case tests (malformed JSON, truncated SSE streams, oversized payloads)

## Error Handling

- [ ] Improve HTTP error responses (structured JSON errors instead of plain strings)
- [ ] Add context timeouts to tool execution (currently unbounded)
- [ ] Add timeout for sync LLM calls (currently no explicit timeout)
- [ ] Handle ignored marshal errors in `internal/provider/openai.go`
- [ ] Avoid silent failures on malformed JSON during streaming parse

## Logging & Observability

- [x] Replace `log.Println` with structured logging (go-logging with levels)
- [ ] Add debug logging for tool execution and LLM requests
- [ ] Add Prometheus metrics or similar observability endpoint

## Security

- [ ] Add rate limiting to API endpoints
- [ ] Restrict CORS origin (`CheckOrigin` currently allows all)
- [ ] Avoid passing auth token in WebSocket query params (log leakage risk)

## Features

- [x] Support multiple LLM providers (provider registry with name-qualified models)
- [x] Implement graceful shutdown (signal.NotifyContext with SIGTERM)
- [ ] Deepen `/health` endpoint (check workspace availability, provider reachability)
- [x] Add configuration hot-reload without restart (file watcher on config, skills, crons)
- [ ] Optimize `memory_list` tool (caching or streaming instead of full tree walk)

## Agent Tools

- [x] Web search tool (Brave Search API)
- [ ] Web fetch / URL reading tool (HTML → markdown extraction)
- [ ] Bash / command execution tool with approval workflow
- [x] Memory read/write/edit/search tools (workspace-scoped filesystem)
- [ ] General filesystem tools (read/write outside workspace)
- [ ] Image understanding / vision tool (pass images to multimodal LLM)
- [ ] TTS (text-to-speech) tool
- [x] Cron / scheduled tasks tool (create, list, update, delete, trigger)
- [x] Browser tools (navigate, screenshot, snapshot, click, type, evaluate, tab management)
- [x] Headless browser support (direct CDP connection to chromedp/headless-shell)
- [x] Terminal tools (screenshot, type, press key, connection list)
- [x] Session tools (set title)

## Provider Support

- [x] Provider registry with multi-provider support and model qualification
- [ ] Anthropic Claude provider (native API, not just OpenAI-compatible)
- [ ] Google Gemini provider
- [ ] Provider failover (multiple API keys / auth profiles with fallback)
- [ ] OAuth-based provider auth (Anthropic Pro/Max, OpenAI)
- [ ] Per-model tool gating (enable/disable tools based on model capabilities)

## Multi-Agent & Routing

- [ ] Multi-agent support (multiple agent configs with separate workspaces)
- [ ] Agent routing (route requests to different agents based on channel/context)
- [ ] Subagent spawning (agent can spawn isolated sub-conversations)

## Messaging Channels

- [x] Telegram channel integration (per-chat sessions, model overrides, slash commands)
- [x] Discord channel integration (per-channel sessions, model overrides, slash commands)
- [ ] Slack channel integration
- [ ] WhatsApp channel integration
- [ ] Channel-level routing to specific agents

## Session Management

- [x] Session state patch (per-channel model overrides in Discord/Telegram)
- [x] Session pruning / context compaction (summarize old messages)
- [x] JSONL-based persistent session storage with titles
- [ ] Queue modes for concurrent requests (serial, parallel, drop)

## Security & Sandboxing

- [ ] Tool approval workflows (user confirms before sensitive tool execution)
- [ ] Docker-based sandbox for tool execution (per-session containers)
- [ ] Tool policies (allowlist/denylist per agent or group)

## Automation

- [x] Cron job scheduler with 5-field expression support
- [x] Persistent cron storage with hot-reload
- [x] Per-job model overrides and manual triggering
- [ ] Webhook endpoints for external event triggers
- [ ] Background job lifecycle management

## System Prompt & Context

- [x] Modular system prompt builder (template-based composable sections)
- [x] Runtime info injection (date, time, timezone)
- [x] Memory/workspace context loading (AGENTS.md, MEMORY.md, daily logs)
- [x] Skill prompt injection into system prompt
- [x] Context compaction / summarization for long conversations
- [ ] Runtime host/OS/shell info injection

## CLI

- [x] Gateway command (`teanode gateway` with port flag)
- [x] Terminal command (`teanode terminal`)
- [ ] Interactive onboarding wizard (`teanode onboard`)
- [ ] Session management commands (`teanode sessions list/delete`)
- [ ] Configuration management commands (`teanode config`)
- [ ] Health check / diagnostics command (`teanode doctor`)

## Plugin / Extension System

- [x] Skills system (JSON-defined tools with shell and HTTP execution)
- [x] Hot-reloadable skill loading from `~/.teanode/skills/`
- [ ] Plugin SDK for registering custom tools, providers, and hooks

## Documentation

- [ ] Document OpenAI-compatible API surface and any deviations
- [ ] Document WebSocket RPC frame format and available methods
- [ ] Add deployment / production setup guide
- [ ] Add memory tool usage examples for agent authors
