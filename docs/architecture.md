# TeaNode Architecture

TeaNode is a Go-based LLM gateway that orchestrates conversations, tools, skills, and background jobs. It runs one or more “agents” (LLM-backed assistants), exposes HTTP/WebSocket APIs, and integrates with external systems (GitLab, GitHub, Google, shell, filesystem, etc.).

This document summarizes the high-level architecture and how the main pieces fit together.

---

## Top-Level Layout

Repository root (key items only):

- `main.go` – entrypoint; bootstraps configuration, HTTP server, and the main agent.
- `cmd/` – additional command-line tools or entrypoints (if any).
- `internal/` – core implementation packages:
  - `agents/` – LLM agents and conversation orchestration.
  - `api/` – HTTP/WS API handlers.
  - `channels/` – transport channels (e.g. web UI, CLI, other frontends).
  - `configs/` – configuration loading and validation.
  - `conversations/` – conversation storage and summaries.
  - `frontend/` – web UI/backend glue.
  - `gw/` – gateway process orchestration.
  - `integrations/` – external service integrations (if needed beyond tools).
  - `jobs/` – background jobs and reminders.
  - `media/` – media storage (images/audio) abstraction.
  - `provider/` – model provider abstraction (OpenAI, Anthropic, etc.).
  - `skills/` – markdown frontmatter-defined tool/skill bundles.
  - `tools/` – concrete tool implementations (GitLab, GitHub, shell, etc.).
  - `util/` – shared helpers.
  - `version/` – build version information.
  - `watcher/` – file watching / hot reload utilities.
  - `web/` – static web assets server.
- `web/` – React/JS frontend source (built into static assets).
- `docs/` – documentation (this file).
- `skills.examples/` – example markdown skill definitions.
- `config.example.json` – example TeaNode configuration.
- `Dockerfile`, `docker-compose.yml` – container / deployment setup.
- `Makefile` – build targets (binary + web assets).

---

## Core Concepts

### Agents

An **agent** is an LLM-backed conversational entity that can:

- Read and maintain conversation context.
- Call tools (GitLab, filesystem, shell, etc.).
- Use _skills_ (higher-level workflows composed of tools).
- Interact with other agents.

Key code: `internal/agents/`

Important pieces:

- `registry.go`  
  Registers available agents by ID and provides access to them at runtime. (Extended with metadata in recent versions for better multi-agent flows.)

- `runner.go` / `runctx.go`  
  Orchestrate the main loop of a conversation turn:
  1. Feed the current conversation context and system prompt to the LLM.
  2. Interpret tool calls from the model.
  3. Execute tools and feed results back.
  4. Produce a final user-facing response.

- `context.go`  
  Defines conversation context structures (messages, tool calls, state) used by the runner.

- `systemprompt.go` / `systemprompt.txt`  
  Compose the agent’s system prompt, including:
  - Core behavior instructions.
  - Workspace-driven configuration (AGENT.md, MEMORY.md, SKILLS.md).

- `conversation_tools.go` / `tools.go`  
  Bridge between the agent and the underlying tools:
  - Expose tools to the LLM as function-calling endpoints.
  - Route tool calls to the corresponding `internal/tools/*` implementation.

- `compact.go` / `summarizer.go`  
  Implement conversation summarization and compaction:
  - Periodically summarize older parts of a conversation.
  - Keep token usage manageable while preserving context.

- `interagent.go`  
  Implements **agent-to-agent messaging**, backing the `agent_message` tool so one agent can delegate tasks to another.

### Tools

A **tool** is a concrete capability the agent can invoke: run a shell command, call an HTTP API, manipulate the filesystem, query GitLab, etc.

Key code: `internal/tools/`

Tools are grouped by domain, examples:

- `shell/` – execute shell commands in a controlled environment.
- `filesystem/` – read/write local files with path and safety checks.
- `workspace/` – persistent workspace storage (AGENT.md, MEMORY.md, logs, etc.).
- `gitlab/`, `github/` – GitLab/GitHub issues and MRs.
- `google/` – Gmail, Calendar, Drive.
- `search/` – web search via Brave or similar.
- `fetch/` – generic HTTP GET.

Each tool:

- Defines a JSON-based request/response schema.
- Has a Go handler that:
  - Validates parameters.
  - Performs the requested operation.
  - Returns a structured result (or a structured error).

Tools are wired into the agent layer via `internal/agents/tools.go` so the LLM sees them as “function tools.”

### Skills

A **skill** is a markdown frontmatter-defined bundle of one or more tools plus an optional prompt.

Key code: `internal/skills/`, `skills.examples/`

Skill definition (`SkillDefinition`):

- `Name` – unique identifier.
- `Description` – what the skill does.
- `Prompt` – additional instruction text when this skill is active.
- `Tools` – a list of `ToolDefinition` entries.

Each `ToolDefinition` describes:

- `Name`, `Description`.
- `Type` – `"shell"` or `"http"`.
- `Parameters` – JSON schema for the LLM’s arguments.
- For shell:
  - `Command` – command + args.
  - `WorkingDirectory`.
- For HTTP:
  - `Method`, `URL`, `Headers`, `Body`.
- `Timeout`.

`internal/skills/loader.go`:

- Loads all `*.md` files from a skills directory.
- Validates them (e.g., `shell` tools must have a command; `http` tools must have a URL).
- Logs warnings and skips malformed entries instead of failing the whole process.

Skills give a declarative way to add higher-level capabilities without changing Go code.

### Conversations

TeaNode maintains **conversations** (sessions) with:

- A history of messages (user, assistant, tool calls).
- Optional summaries and metadata.

Key code: `internal/conversations/`

Responsibilities:

- Store and retrieve conversation histories.
- Maintain summaries for long-running chats (used by the agents’ compactor/summarizer).
- Provide APIs to list, resume, or compact conversations.

The API layer (see below) exposes these via HTTP/WebSocket so frontends and external consumers can interact with conversational state.

### Jobs

**Jobs** are scheduled tasks or one-shot reminders.

Key code: `internal/jobs/`

Main components:

- `types.go` – job definitions (ID, schedule, payload, etc.).
- `jobs.go` – public API for creating, listing, updating, deleting jobs.
- `scheduler.go` – cron-like scheduler:
  - Parses cron expressions or one-shot delays.
  - Triggers jobs at the appropriate times.
- `store.go` – persistence for jobs (in-memory or on-disk depending on config).
- `tools.go` – exposes jobs functionality as tools for the LLM:
  - `jobs_list`, `jobs_create`, `jobs_update`, `jobs_delete`, `jobs_trigger`.

Typical uses:

- User reminders (“remind me in 30 minutes”).
- Recurring maintenance (e.g. daily self-review agent tasks).
- Automated workflows (e.g. morning GitLab summary).

### Provider

The **provider** layer abstracts actual LLM backends.

Key code: `internal/provider/`

Responsibilities:

- Map a logical model name (e.g. `"gpt-5.1"`, `"claude-opus"`) to an underlying API call.
- Handle auth, rate limits, retries, and streaming as needed.
- Present a simple interface to the agent layer:
  - “Given this prompt and tools, run a completion and return a structured result.”

This makes TeaNode portable across providers and configurable via `config.json`.

### Gateway and API

The **gateway** coordinates all components and exposes APIs.

Key code:

- `internal/gw/` – overall process orchestration (startup, shutdown, hot reload, etc.).
- `internal/api/` – HTTP/WS endpoints:
  - Manage conversations, send/receive messages.
  - Expose tools (e.g. browser, terminal) when available.
  - Health checks, configuration inspection.

On startup (`main.go`):

1. Load configuration from JSON and environment (`internal/configs`).
2. Initialize logging and version info (`internal/version`).
3. Initialize tools, skills, jobs, and agents.
4. Start the HTTP server and any other channels (e.g. WebSocket, CLI).

Frontends (CLI, web UI, etc.) connect to the API to:

- Start or continue conversations.
- Send user messages.
- Receive agent responses and tool/tool output.

### Frontend and Web Assets

The **web UI** is a React-based client living under `web/`.

- `web/` (JS/React source) is built with webpack via `npm run build`.
- The built assets (HTML, JS, CSS) are served by `internal/web` from the compiled bundle directory.
- `Makefile` ties this together:
  - `cd web && npm run build` to produce assets.
  - `go build` with version flags to produce the `teanode` binary.

This gives a browser-based chat UI for interacting with agents, tools, and conversations.

---

## Data & Configuration

### Configuration

- `config.example.json` shows typical config structure:
  - Server ports, model providers, tool options, skills directory, etc.
- Config is parsed in `internal/configs` and passed down to other subsystems on startup.

### Workspace & Memory

While not hardcoded into Go types, TeaNode often uses a **workspace** directory (backed by the `workspace` tool) for:

- `AGENT.md` – agent behavior and operating instructions.
- `MEMORY.md` – long-term curated memory (preferences, workflows).
- `SKILLS.md` – human-readable summary of learned skills/workflows.
- `memory/YYYY-MM-DD.md` – daily logs of activity and notes.

The main agent reads and updates these files to persist behavior and memory across sessions.

---

## Typical Request Flow

1. **Client sends a message** via HTTP/WS API:
   - Includes conversation ID (existing) or creates a new one.

2. **API layer** forwards to the gateway/agents:
   - Looks up or creates a conversation record.
   - Invokes the appropriate agent (usually the “main” assistant).

3. **Agent runner**:
   - Builds the system prompt (systemprompt + AGENT.md + other config).
   - Assembles recent conversation history and any summaries.
   - Calls the provider’s LLM API with tools enabled.

4. **LLM calls tools** (zero or more times):
   - The agent layer translates tool calls into `internal/tools` invocations.
   - Tools talk to external systems (GitLab, Google, filesystem, etc.).
   - Results are fed back into the LLM as new messages.

5. **Agent produces final reply**:
   - The runner collects the assistant’s final message.
   - Conversation is updated (messages stored, summaries updated if needed).
   - Response is returned to the client.

6. **Jobs and multi-agent flows** (optional):
   - Agents may create/update jobs via the `jobs` tool.
   - Agents may delegate tasks to other agents via `agent_message`.
   - Background scheduler triggers jobs, which in turn create new messages to agents.

---

## Extensibility

To extend TeaNode, you can:

- **Add a new tool**:
  - Create a package under `internal/tools/yourtool`.
  - Define request/response types and a handler.
  - Register it in the tools registry so the agent can see it.

- **Add a new skill**:
  - Write a `.md` file in the configured skills directory (see `skills.examples`).
  - Define one or more shell/HTTP tools and their parameter schemas.
  - Restart TeaNode or trigger skills reload.

- **Add a new agent**:
  - Implement an agent factory in `internal/agents`.
  - Register it in the agent registry with an ID and metadata.
  - Optionally expose it via a dedicated API or let other agents call it via `agent_message`.

- **Customize behavior**:
  - Edit `AGENT.md` to change global behavior patterns.
  - Use `MEMORY.md`, `SKILLS.md`, and daily memory files to encode stable preferences and workflows.
  - Schedule jobs for periodic tasks.

---

This document is intentionally high-level. For details, see the GoDoc comments in the respective `internal/*` packages and the example skills in `skills.examples/`.
