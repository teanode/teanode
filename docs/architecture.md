# TeaNode Backend Architecture

This document describes the current Go backend architecture in this repository (`main.go`, `cmd/`, `internal/`).

## Runtime Topology (ASCII)

```text
+-----------------------------------------------------------------------------------+
|                                   teanode binary                                  |
|                                   (main.go / CLI)                                |
+-----------------------------------------------+-----------------------------------+
                                                |
                                                v
                                     +---------------------+
                                     | cmd/gateway command |
                                     +---------------------+
                                                |
                                                v
+-----------------------------------------------------------------------------------+
|                               HTTP Server (gorilla/mux)                           |
|      Middleware: auth -> compression -> server-name -> logging -> forwarder       |
+------------------------------+--------------------------------------+--------------+
                               |                                      |
                               v                                      v
                    +-----------------------+               +-----------------------+
                    | internal/api/v1api    |               | internal/frontend     |
                    | /api/v1/* (HTTP + WS) |               | Embedded SPA static   |
                    +-----------+-----------+               +-----------------------+
                                |
                                v
                    +----------------------------+
                    | internal/coordinators      |
                    | Coordinator (orchestrator) |
                    +----+----+----+----+--------+
                         |    |    |    |
                         |    |    |    +------------------------------------+
                         |    |    |                                         |
                         |    |    v                                         v
                         |    |  +-------------------+            +-----------------------+
                         |    |  | store (sessions,  |            | store (media)         |
                         |    |  | security, config) |            | uploads + generated   |
                         |    |  +-------------------+            | media files           |
                         |    |                                   +-----------------------+
                         |    |
                         |    +----------------------------+
                         |                                 |
                         v                                 v
            +-----------------------------+     +-----------------------------+
            | runners.Registry            |     | jobs.Scheduler              |
            | Runner per configured agent |     | users/*/jobs/*.md           |
            +--------------+--------------+     +---------------+-------------+
                           |                                    |
                           v                                    |
                  +-------------------+                         |
                  | runners.Runner    |<------------------------+
                  | per-conversation  |  jobs trigger SendMessage
                  | serialization     |
                  +---------+---------+
                            |
                            v
      +---------------------------------------------------------------+
      | ToolRegistry (built-ins + skill tools + inter-agent + jobs)  |
      | workspace/filesystem/shell/search/fetch/github/gitlab/google  |
      | codex/claudecode/homeassistant/unifiprotect/browser/terminal  |
      +--------------------------+------------------------------------+
                                 |
                                 v
                      +---------------------------+
                      | providers.Registry        |
                      | openai / anthropic / ...  |
                      +---------------------------+
```

## Process and Package Structure

- `main.go`
- Initializes CLI and global logging.
- Registers commands: `gateway`, `restart`, `terminal`.

- `cmd/gateway.go`
- Bootstraps the full backend runtime.
- Creates config/security/session/provider/tool/agent/scheduler/gateway/api/frontend components.
- Starts HTTP server and graceful shutdown loop.

- `internal/coordinators`
- Central domain orchestrator used by API, bots, and voice sessions.
- Owns active run tracking, broadcasts, lifecycle actions, defaults (agent/conversation), model cache.

- `internal/api/v1api`
- Versioned API mounted at `/api/v1`.
- HTTP routes (health, auth, media, audio, OpenAI-compatible `/chat/completions`).
- WebSocket RPC endpoint (`/api/v1/websocket`) for conversations, jobs, config, skills, users, voice, projects.

- `internal/runners`
- `Runner` handles a full LLM turn, including streaming, tool loops, and conversation persistence.
- `Registry` manages multiple runners and per-user default agent/conversation state.
- Includes context compaction logic.

- `internal/summarizers`
- Background summarizer for conversation titles and descriptions.

- `internal/store`
- Unified store abstraction with backend implementations:
  - `fsstore` — file-based store (YAML + JSONL) for conversations, agents, users, sessions, media, config, jobs, skills, projects, workspace files.
  - `dbstore` — database-backed store (PostgreSQL via GORM) implementing the same interface.
- Persists conversation headers and message streams as JSONL.

- `internal/models`
- Shared data types: Agent, Conversation, ConversationMessage, User, Session, Job, Skill, Media, Token, Project, WorkspaceFile, Configuration.

- `internal/jobs`
- Markdown + YAML-frontmatter job store (`users/<userId>/jobs/<jobId>.md`).
- Scheduler executes recurring/one-shot jobs by sending messages through gateway.

- `internal/skills`
- Loads local and installed skills from `skills/*.md` and `skills/.installed/**/skill.md`.
- Registers `shell`, `http`, and `workflow` tools into each agent's tool registry.

- `internal/providers`
- Provider abstraction and registry with qualified models (`provider:model`).
- Used by runners and audio endpoints (transcribe/synthesize when provider supports it).

- `internal/frontend`
- Serves embedded web assets with SPA fallback and COOP/COEP headers.

- `internal/channels/discord`, `internal/channels/telegram`
- Optional bot channels that forward messages into the same gateway run pipeline.

- `internal/voice`
- Voice session runtime used by WebSocket RPC (`voice.start`, binary audio frames, cancel/commit/end).

## Startup Flow (Current)

```text
CLI "teanode gateway"
  -> ensure directories
  -> load config.yaml + security.yaml
  -> create sessions store
  -> build providers registry
  -> setup browser relay/headless + terminal relay + media store
  -> create runners Registry and Runner per configured agent
  -> build tools per agent (built-ins + skills + inter-agent + jobs)
  -> create Scheduler + Summarizer
  -> create Coordinator
  -> mount v1 API + frontend into mux server
  -> start scheduler, summarizer
  -> start HTTP listener and serve
```

## Message Execution Flow

```text
HTTP/WS/Bot request
  -> coordinator.SendMessage(userId, agentId, conversationId, message, ...)
  -> resolve default agent/conversation when omitted
  -> track active run + broadcast "user_message"
  -> Runner.Run(...)
       -> append user message to conversation store
       -> build prompt/system context
       -> call provider stream
       -> emit deltas + tool calls/results
       -> append assistant result
  -> coordinator broadcasts "final" (or "error" / "aborted")
  -> summarizer notified
```

## Persistence Layout (Data Directory)

```text
~/.teanode/
  config.yaml
  security.yaml
  state.yaml
  models.yaml
  gateway.pid                           (while gateway is running)
  projects/
    <projectId>/
      project.yaml
      workspace/
        PROJECT.md
        ...
  media/
    <last2>/
      <mediaId>.<format>
      <mediaId>.meta.json
  sessions/
    <sessionId>.yaml
  users/
    <userId>/
      user.yaml
      conversations/<agentId>/<conversationId>.jsonl
      jobs/<jobId>.md
      workspace/
        USER.md
        ONBOARDING.md
        MEMORY.md
        memory/<YYYY-MM-DD>.md
  agents/
    <agentId>/
      config.yaml
      state.yaml
      workspace/
        AGENT.md
        MEMORY.md
        SKILLS.md
        memory/<YYYY-MM-DD>.md
  skills/
    *.md
    .installed/<skillName>/<version>/skill.md
    .installed/<skillName>/<version>/manifest.json
  .trash/
  .backup/
```

## Additional Packages

- `internal/lifecycle` — Process lifecycle management.
- `internal/onboarding` — First-run onboarding and workspace bootstrapping.
- `internal/prompts` — System prompt templates and composition.
- `internal/pubsub` — In-process publish/subscribe for event broadcasting.
- `internal/schemas` — JSON schema definitions for configuration validation.
- `internal/version` — Build version metadata.
- `internal/web` — HTTP server, middleware (auth, compression, logging, forwarding), and error handling.
- `internal/util/` — Utility packages: `atomicfile`, `bufferpool`, `cmdexec`, `cronexpr`, `datastruct`, `debugutil`, `deferutil`, `mimetypes`, `pending`, `ptrto`, `ratelimit`, `screenbuffer`, `security`, `sessiontracker`, `slashcommands`, `timeutil`, `trash`, `valueor`.

## Notes

- Config and state are YAML-based (`config.yaml`, `security.yaml`, `state.yaml`), not JSON.
