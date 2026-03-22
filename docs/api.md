# HTTP API v1 Overview

TeaNode exposes an HTTP API under `/api`. This document gives a high-level overview of the current surface and how it maps onto TeaNode internals.

## HTTP Endpoints

### Authentication (public, no auth required)

- `GET/POST /api/health` — Lightweight health/liveness check.
- `GET/POST /api/auth/status` — Current authentication status.
- `POST /api/auth/setup` — Initial setup (first-run password creation).
- `POST /api/auth/login` — User login (returns session cookie).
- `POST /api/auth/logout` — User logout.

### WebSocket

- `GET /api/websocket` — WebSocket upgrade endpoint for JSON-RPC over WS. Most application state flows through this channel (see WebSocket RPC Methods below).

### Media

- `POST /api/media/upload` — Upload binary media (50 MB max).
- `GET /api/media/{id}` — Retrieve media by ID. Public (no auth) so LLM providers can fetch images.

### Audio

- `POST /api/audio/transcribe` — Transcribe audio to text (25 MB max). Requires a provider implementing `TranscribeProvider`.
- `POST /api/audio/synthesize` — Request a TTS token (single-use, valid 60s).
- `GET /api/audio/stream?token=...` — Stream synthesized audio using the token from `/synthesize`.

### OpenAI-Compatible

- `POST /api/chat/completions` — OpenAI-compatible Chat Completions endpoint. Accepts a subset of the OpenAI schema, routes to a configured TeaNode agent and provider, and supports streaming via the `stream` flag.

### Integration Relays (conditional)

- `GET /api/browser` — Browser relay WebSocket (when browser integration is configured).
- `GET /api/terminal` — Terminal relay WebSocket (when terminal integration is configured).

## WebSocket RPC Methods

The WebSocket endpoint dispatches JSON-RPC calls. The current method set (69 methods):

### Core

| Method | Description |
|--------|-------------|
| `connect` | Handshake; returns capabilities (e.g. `"audio"` when voice providers are available) |
| `health` | Health check |

### Conversations

| Method | Description |
|--------|-------------|
| `conversations.send` | Send a message |
| `conversations.history` | Fetch message history |
| `conversations.abort` | Cancel an in-progress run |
| `conversations.list` | List conversations for an agent |
| `conversations.delete` | Delete a conversation |
| `conversations.setDefault` | Set default conversation |
| `conversations.todos.list` | List TODOs in a conversation |
| `conversations.todos.batch` | Batch update conversation TODOs |

### Agents

| Method | Description |
|--------|-------------|
| `agents.list` | List available agents |
| `agents.setDefault` | Set default agent |
| `agents.config.schema` | Get agent config JSON schema |
| `agents.config.list` | List agent configs |
| `agents.config.save` | Save agent config |
| `agents.config.delete` | Delete agent config |
| `agents.avatar.set` | Set agent avatar |
| `agents.avatar.remove` | Remove agent avatar |

### Models

| Method | Description |
|--------|-------------|
| `models.list` | List available models |

### Jobs

| Method | Description |
|--------|-------------|
| `jobs.list` | List scheduled jobs |
| `jobs.create` | Create a job |
| `jobs.update` | Update a job |
| `jobs.delete` | Delete a job |
| `jobs.trigger` | Trigger immediate execution |

### Configuration

| Method | Description |
|--------|-------------|
| `config.schema` | Get global config JSON schema |
| `config.get` | Get global config |
| `config.update` | Update global config |

### Sessions & Tokens

| Method | Description |
|--------|-------------|
| `sessions.list` | List active sessions |
| `sessions.revoke` | Revoke a session |
| `auth.tokens.list` | List API tokens |
| `auth.tokens.create` | Create an API token |
| `auth.tokens.delete` | Delete an API token |
| `auth.changePassword` | Change password |

### Users (admin)

| Method | Description |
|--------|-------------|
| `users.list` | List users |
| `users.create` | Create a user |
| `users.delete` | Delete a user |
| `users.update` | Update a user |
| `users.changePassword` | Change user password |
| `users.setRole` | Set user role |

### Profile

| Method | Description |
|--------|-------------|
| `profile.get` | Get current user profile |
| `profile.update` | Update profile (name, avatar) |
| `profile.avatar.remove` | Remove profile avatar |

### Skills

| Method | Description |
|--------|-------------|
| `skills.local.list` | List local skills |
| `skills.library.search` | Search skill library/registry |
| `skills.install` | Install a skill |
| `skills.installed.list` | List installed skills |
| `skills.uninstall` | Uninstall a skill |
| `skills.update` | Update a skill |
| `skills.setEnabled` | Enable/disable a skill for an agent |

### Secrets

| Method | Description |
|--------|-------------|
| `secrets.list` | List secret names |
| `secrets.set` | Set a secret value |

### Voice

| Method | Description |
|--------|-------------|
| `voice.providers` | List voice providers |
| `voice.start` | Start a voice input session |
| `voice.end` | End a voice input session |
| `voice.input.commit` | Commit voice input buffer |
| `voice.response.cancel` | Cancel voice response playback |

Binary WebSocket frames are used for streaming audio input during voice sessions.

### Projects

| Method | Description |
|--------|-------------|
| `projects.list` | List projects |
| `projects.create` | Create a project |
| `projects.rename` | Rename a project |
| `projects.delete` | Delete a project |

### Questions

| Method | Description |
|--------|-------------|
| `questions.list` | List open questions (from `ask_user_question` tool) |
| `questions.answer` | Answer a pending question |

### Tab

| Method | Description |
|--------|-------------|
| `tab.attach` | Attach a browser tab for context injection |
| `tab.detach` | Detach a browser tab |
| `tab.commandResult` | Send command result from an attached tab |

### Usage

| Method | Description |
|--------|-------------|
| `usages.list` | List usage statistics |

### Memory

| Method | Description |
|--------|-------------|
| `memory.list` | List memory entries |
| `memory.search` | Search memory (semantic when embeddings are enabled) |
| `memory.delete` | Delete a memory entry |

## Relationship to Internals

- The v1 API is a thin HTTP/WS layer over:
  - `internal/coordinators` for conversation orchestration and active run management.
  - `internal/runners` for LLM turn execution and tool calls.
  - `internal/providers` for model-specific HTTP calls.
  - `internal/store` for persistent conversation, entity, and memory storage.
  - `internal/voice` for voice session management.
  - `internal/integrations` for browser, terminal, tab, and question brokering.
- Authentication middleware in `internal/web` handles session cookies, bearer tokens, and path-based public access rules.

This document is intentionally high-level and meant as a starting point for navigating the v1 API implementation rather than a full reference.
