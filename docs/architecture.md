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
                                     |  cmd/node command   |
                                     +---------------------+
                                                |
                                                v
+-----------------------------------------------------------------------------------+
|                               HTTP Server (gorilla/mux)                           |
|    Middleware: forwarder -> logging -> server-name -> compression -> auth          |
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
                         |    |  | store (unified)   |            | store (media)         |
                         |    |  | config, users,    |            | uploads + generated   |
                         |    |  | agents, projects  |            | media files           |
                         |    |  +-------------------+            +-----------------------+
                         |    |
                         |    +----------------------------+
                         |                                 |
                         v                                 v
            +-----------------------------+     +-----------------------------+
            | runners.Registry            |     | jobs.Scheduler              |
            | Runner per configured agent |     | users/*/jobs/*.yaml         |
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
      | memory/workspace/filesystem/shell/search/fetch/datetime      |
      | github/gitlab/google/claudecode/codex/browser/tab/terminal   |
      | homeassistant/unifiprotect/agent/conversation/project/jobs   |
      +--------------------------+------------------------------------+
                                 |
                                 v
                      +-----------------------------------+
                      | providers.Registry                |
                      | openai / anthropic / deepgram /   |
                      | elevenlabs / ...                   |
                      +-----------------------------------+
```

## Process and Package Structure

- `main.go`
- Initializes CLI and global logging.
- Registers commands: `node`, `restart`, `terminal`, `tools`.

- `cmd/node.go`
- Bootstraps the full backend runtime.
- Opens store, loads configuration, builds provider/tool registries, creates coordinator and API components.
- Starts HTTP(S) server with optional TLS/AutoACME and graceful shutdown loop.

- `internal/coordinators`
- Central domain orchestrator used by API, bots, and voice sessions.
- Owns active run tracking, broadcasts, lifecycle actions, defaults (agent/conversation), model cache.

- `internal/api/v1api`
- Versioned API mounted at `/api/v1`.
- HTTP routes (health, auth, media, audio, OpenAI-compatible `/chat/completions`).
- WebSocket RPC endpoint (`/api/v1/websocket`) for conversations, jobs, config, skills, users, voice, projects, memory, todos, usage, questions, tabs.

- `internal/runners`
- `Runner` handles a full LLM turn, including streaming, tool loops, and conversation persistence.
- `Registry` manages multiple runners and per-user default agent/conversation state.
- Includes context compaction logic and system prompt overlays (short-term memory, TODOs, tabs, voice mode).

- `internal/summarizers`
- Background summarizer for conversation titles and descriptions.

- `internal/store`
- Unified store abstraction with backend implementations:
  - `fsstore` — file-based store (YAML + JSONL + msgpack) for conversations, agents, users, sessions, media, config, jobs, skills, projects, workspace files, memory items, todos, tokens, usage.
  - `dbstore` — database-backed store (PostgreSQL via GORM) implementing the same interface.
- Persists conversation message streams as JSONL files.

- `internal/models`
- Shared data types: Agent, Conversation, ConversationMessage, User, Session, Job, Skill, Media, Token, Project, WorkspaceFile, Configuration, MemoryItem, Todo, Usage.

- `internal/jobs`
- YAML job store (`users/<userId>/jobs/<jobId>.yaml`).
- Scheduler executes recurring/one-shot jobs by sending messages through the coordinator.

- `internal/skills`
- Loads local and installed skills from `skills/*.md` and `skills/.installed/**/skill.md`.
- Registers `shell`, `http`, and `workflow` tools into each agent's tool registry.

- `internal/providers`
- Provider abstraction and registry with qualified models (`provider:model`).
- Capability interfaces: `ChatProvider`, `TranscribeProvider`, `StreamingTranscribeProvider`, `SynthesizeProvider`, `StreamingSynthesizeProvider`, `EmbeddingProvider`.
- Used by runners, audio endpoints, voice sessions, and embedding operations.

- `internal/embeddings`
- Embedding client for semantic search in memory tools.
- Delegates to providers that implement `EmbeddingProvider`.

- `internal/frontend`
- Serves embedded web assets with SPA fallback and COOP/COEP headers.

- `internal/channels/discord`, `internal/channels/telegram`
- Optional bot channels that forward messages into the same coordinator run pipeline.

- `internal/voice`
- Voice session runtime used by WebSocket RPC (`voice.start`, binary audio frames, cancel/commit/end).

- `internal/integrations`
- `browsers` — Browser relay and headless browser management.
- `questions` — Broker for agent-to-user questions (ask_user_question tool).
- `tabs` — Browser tab attachment/detachment for page context injection.
- `terminals` — PTY-based terminal relay for interactive shell sessions.

- `internal/autoacme`
- Automatic TLS certificate management via ACME TLS-ALPN-01 challenge.

## Startup Flow (Current)

```text
CLI "teanode node"
  -> acquire PID lock
  -> open store (filesystem or postgres) + run migrations
  -> load configuration from store
  -> build providers registry
  -> setup browser relay/headless + terminal relay
  -> create PubSub, session tracker, summarizer, job scheduler
  -> ensure main agent exists (create "Tea" if none)
  -> create Coordinator + Lifecycle manager
  -> mount v1 API + frontend into mux server
  -> start Discord/Telegram bots (if configured)
  -> create HTTP server with middleware stack
  -> if TLS enabled: wrap listener with tls.NewListener + start AutoACME manager
  -> start HTTP(S) listener and serve
  -> watch for OS signals (graceful shutdown)
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
  config.yaml                           (global configuration, managed by store)
  node.pid                           (while node is running)
  users/
    <userId>/
      user.yaml
      workspace/
        USER.md
        ONBOARDING.md
        MEMORY.md
      conversations/
        <agentId>/
          <conversationId>.jsonl        (conversation messages, one JSON object per line)
          <conversationId>.todos/       (conversation-scoped TODOs)
      jobs/
        <jobId>.yaml
      sessions/
        <sessionId>.yaml
      tokens/
        <tokenId>.yaml
      memory.msgpack                    (user-scoped memory items)
  agents/
    <agentId>/
      agent.yaml
      workspace/
        AGENT.md
        MEMORY.md
        SKILLS.md
      memory.msgpack                    (agent-scoped memory items)
  projects/
    <projectId>/
      project.yaml
      workspace/
        PROJECT.md
        ...
      todos/
        <todoId>.yaml
      memory.msgpack                    (project-scoped memory items)
  media/
    <last2>/
      <mediaId>.<format>
  skills/
    *.md
    .installed/<skillName>/<version>/skill.md
    .installed/<skillName>/<version>/manifest.json
  .trash/
```

## Additional Packages

- `internal/lifecycle` — Process lifecycle management.
- `internal/onboarding` — First-run onboarding and workspace bootstrapping.
- `internal/prompts` — System prompt templates and composition (`systemprompt.txt`, `AGENT.md`, `MEMORY.md`, `ONBOARDING.md`, `PROJECT.md`, `SKILLS.md`, `USER.md`).
- `internal/pubsub` — In-process publish/subscribe for event broadcasting.
- `internal/schemas` — JSON schema definitions for configuration validation.
- `internal/version` — Build version metadata.
- `internal/web` — HTTP server, middleware (auth, compression, logging, server-name, forwarding), and error handling.
- `internal/util/` — Utility packages: `allowlist`, `atomicfile`, `bufferpool`, `cmdexec`, `cronexpr`, `datastruct`, `debugutil`, `deferutil`, `mimetypes`, `pending`, `ptrto`, `ratelimit`, `screenbuffer`, `security`, `sessiontracker`, `slashcommands`, `timeutil`, `trash`, `valueor`.

## Notes

- Config is YAML-based (`config.yaml`), managed through the unified store interface.
- The fsstore backend uses YAML for entity metadata, msgpack for memory items, and JSONL for conversation messages.
