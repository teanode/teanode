# TeaNode

Personal AI assistant node. Exposes an OpenAI-compatible API that proxies to configurable LLM providers, with persistent memory, a web UI, and a self-improving agent workspace.

## Features

- **Multi-provider LLM support** — OpenAI, Anthropic, Google Gemini, OpenRouter, with per-model routing
- **OpenAI-compatible API** — drop-in `/api/v1/chat/completions` endpoint
- **Embedded web UI** — React SPA with streaming conversations, voice input, and job management
- **Agent workspace** — editable markdown files (`AGENT.md`, `MEMORY.md`) shape agent behavior without code changes
- **30+ built-in tools** — shell, filesystem, browser automation, GitHub, GitLab, Google services, Home Assistant, and more
- **Skills system** — extend agents with markdown-defined tools (shell, HTTP, workflow) — no Go code required
- **Voice** — speech-to-text (Whisper, Deepgram) and text-to-speech (OpenAI TTS, ElevenLabs)
- **Background jobs** — cron scheduling and one-shot reminders
- **Multi-agent** — multiple agents with separate configs, inter-agent messaging, and subagent spawning
- **Messaging channels** — Telegram and Discord bot integrations with per-channel routing
- **Storage backends** — filesystem (YAML/JSONL) or PostgreSQL
- **Semantic memory** — structured memory items with embedding-based search
- **Browser & terminal relay** — headless Chrome (CDP) and PTY-based terminal control

## Prerequisites

- Go 1.25+
- Node.js 20+ (for frontend development)
- An LLM provider API key (OpenAI, Anthropic, etc.)

## Quick Start

```sh
export OPENAI_API_KEY=sk-...
go run . node
```

The node listens on `http://localhost:8833` by default and serves the web UI at the root path.

### Build

```sh
make all        # build frontend + backend
make teanode    # backend only
make web        # frontend only
```

Or without make:

```sh
cd web && npm install && npm run build && cd ..
go build -o teanode .
```

### Run

```sh
./teanode node                  # run in foreground
./teanode node --port 8080      # custom port
./teanode start                 # run in background (daemonize)
./teanode stop                  # stop the background process
./teanode restart               # restart the background process
./teanode status                # check if the node is running
```

Global flags:

```sh
./teanode --dir /path/to/data node    # custom data directory (default: ~/.teanode)
./teanode --log-level DEBUG node      # set log level (DEBUG, INFO, WARNING, ERROR, CRITICAL)
```

### Docker

```sh
docker-compose up --build
```

See `Dockerfile` and `docker-compose.yml` for the full setup, which includes a headless Chrome service for browser tools.

## Configuration

TeaNode reads configuration from `~/.teanode/config.yaml`, with environment variables taking precedence.

### Common Environment Variables

| Variable | Description |
| --- | --- |
| `OPENAI_API_KEY` | OpenAI API key |
| `ANTHROPIC_API_KEY` | Anthropic API key |
| `GEMINI_API_KEY` | Google Gemini API key |
| `OPENROUTER_API_KEY` | OpenRouter API key |
| `DEEPGRAM_API_KEY` | Deepgram API key (audio transcription) |
| `ELEVENLABS_API_KEY` | ElevenLabs API key (text-to-speech) |
| `TEANODE_NODE_PORT` | Listen port (default: 8833) |
| `TEANODE_NODE_BIND` | Bind address (`loopback`, `lan`, or specific IP) |
| `TEANODE_NODE_TOKEN` | Bearer token for API authentication |
| `TEANODE_STORE` | Storage backend (`filesystem` or `postgres`) |
| `DISCORD_BOT_TOKEN` | Discord bot token |
| `TELEGRAM_BOT_TOKEN` | Telegram bot token |

### Example Config

```yaml
node:
  port: 8833
  bind: loopback
models:
  default: gpt-5.1
  provider: openai
  baseUrl: https://api.openai.com/v1
```

### Workspace Layout

On first run, TeaNode creates its data directory under `~/.teanode/`:

- `agents/<agentId>/workspace/AGENT.md` — agent operating instructions
- `agents/<agentId>/workspace/MEMORY.md` — long-term curated memory
- `agents/<agentId>/workspace/SKILLS.md` — human-readable skill summary
- `users/<userId>/workspace/USER.md` — user-specific instructions
- `users/<userId>/workspace/MEMORY.md` — user-specific memory

Editing `AGENT.md` is the simplest way to customize agent behavior without touching Go code.

## API

TeaNode exposes:

- **OpenAI-compatible HTTP API** at `/api/v1/chat/completions`
- **WebSocket RPC** for real-time communication (conversations, agents, jobs, config, skills, memory)
- **REST endpoints** for media, audio transcription/synthesis, and health checks
- **Health check** at `/api/v1/health`

```sh
curl -X POST \
  -H "Authorization: Bearer $TEANODE_NODE_TOKEN" \
  -H "Content-Type: application/json" \
  http://localhost:8833/api/v1/chat/completions \
  -d '{
    "model": "gpt-5.1",
    "messages": [
      {"role": "user", "content": "Hello from TeaNode"}
    ]
  }'
```

See `docs/api-v1.md` for the full API reference.

## Skills

Skills let you extend agents with custom tools defined in markdown files — no Go code required.

1. Look at `skills.examples/` for samples (weather, dictionary, git, GitHub incident triage, etc.)
2. Create your own `.md` file in `~/.teanode/skills/`
3. Define `shell`, `http`, or `workflow` tools with an optional prompt
4. TeaNode hot-reloads skills automatically

See `docs/agents-and-skills.md` for the schema and lifecycle.

## Documentation

Detailed documentation is in the `docs/` directory:

- [Getting Started](docs/getting-started.md) — installation, build, first run, configuration
- [Architecture](docs/architecture.md) — runtime topology, package structure, startup flow
- [Agents and Skills](docs/agents-and-skills.md) — agent runtime, skills framework, system prompt composition
- [API Reference](docs/api-v1.md) — HTTP endpoints and WebSocket RPC methods
- [Conversations](docs/conversations.md) — conversation storage and message persistence
- [Jobs](docs/jobs.md) — cron scheduler and background automations
- [Auto ACME](docs/autoacme-alpn.md) — automatic TLS certificate management

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup, coding conventions, and contribution guidelines.
