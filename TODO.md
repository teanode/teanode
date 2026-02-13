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

- [ ] Replace `log.Println` with structured logging (levels, fields)
- [ ] Add debug logging for tool execution and LLM requests
- [ ] Add Prometheus metrics or similar observability endpoint

## Security

- [ ] Add rate limiting to API endpoints
- [ ] Restrict CORS origin (`CheckOrigin` currently allows all)
- [ ] Avoid passing auth token in WebSocket query params (log leakage risk)

## Features

- [ ] Support multiple LLM providers beyond OpenAI-compatible APIs
- [ ] Implement graceful shutdown with in-flight request draining
- [ ] Deepen `/health` endpoint (check workspace availability, provider reachability)
- [ ] Add configuration hot-reload without restart
- [ ] Optimize `memory_list` tool (caching or streaming instead of full tree walk)

## Agent Tools

- [ ] Web search tool (Brave, Perplexity, or similar)
- [ ] Web fetch / URL reading tool (HTML → markdown extraction)
- [ ] Bash / command execution tool with approval workflow
- [ ] File read/write/edit tools (general filesystem, not just memory workspace)
- [ ] Image understanding / vision tool (pass images to multimodal LLM)
- [ ] TTS (text-to-speech) tool
- [ ] Cron / scheduled tasks tool (reminders, recurring jobs)

## Provider Support

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

- [ ] Telegram channel integration
- [ ] Discord channel integration
- [ ] Slack channel integration
- [ ] WhatsApp channel integration
- [ ] Channel-level routing to specific agents

## Session Management

- [ ] Session state patch (per-session model override, thinking level, etc.)
- [x] Session pruning / context compaction (summarize old messages)
- [ ] Queue modes for concurrent requests (serial, parallel, drop)

## Security & Sandboxing

- [ ] Tool approval workflows (user confirms before sensitive tool execution)
- [ ] Docker-based sandbox for tool execution (per-session containers)
- [ ] Tool policies (allowlist/denylist per agent or group)

## Automation

- [ ] Cron job scheduler with expression support
- [ ] Webhook endpoints for external event triggers
- [ ] Background job lifecycle management

## System Prompt & Context

- [ ] Modular system prompt builder (composable sections)
- [ ] Runtime info injection (host, OS, shell, model, agent ID)
- [ ] Memory recall with citation support
- [x] Context compaction / summarization for long conversations

## CLI

- [ ] Interactive onboarding wizard (`teanode onboard`)
- [ ] CLI chat mode (`teanode chat` — talk to agent from terminal)
- [ ] Session management commands (`teanode sessions list/delete`)
- [ ] Configuration management commands (`teanode config`)
- [ ] Health check / diagnostics command (`teanode doctor`)

## Plugin / Extension System

- [ ] Plugin SDK for registering custom tools, providers, and hooks
- [ ] Skills system (installable agent capabilities with manifests)

## Documentation

- [ ] Document OpenAI-compatible API surface and any deviations
- [ ] Document WebSocket RPC frame format and available methods
- [ ] Add deployment / production setup guide
- [ ] Add memory tool usage examples for agent authors
