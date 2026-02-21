# Agents and Skills

This document complements `docs/architecture.md` with a focused overview of how **agents** and **skills** work internally.

## Agents (`internal/agents`)

Agents are LLM-backed workers that orchestrate conversations, tools, and skills.

Key responsibilities:

- Maintain conversation context (messages, tool calls, summaries).
- Build the system prompt (including workspace files like AGENT.md, MEMORY.md, SKILLS.md).
- Invoke tools and skills based on the model's tool calls.
- Coordinate conversation compaction and summarization.
- Support inter-agent communication.

Important files:

- `agent.go` – basic agent interface and helpers.
- `registry.go` – global registry of agents, used by the gateway and tools like `agent_list` / `agent_message`.
- `runner.go` / `runctx.go` – main execution loop for a single turn:
  - Prepare the model request (messages, tools, system prompt).
  - Stream or collect the model response.
  - Detect and execute tool calls.
  - Append final assistant messages to the conversation.
- `context.go` – types for messages, tool calls, and conversation state used by the runner.
- `systemprompt.go` / `systemprompt.txt` – compose the system prompt from:
  - Core TeaNode instructions.
  - Workspace configuration (AGENT.md, MEMORY.md, SKILLS.md).
  - Runtime metadata (date/time, etc.).
- `conversation_tools.go` – tools for managing conversations (list, compact, etc.) exposed back to agents.
- `compact.go` / `summarizer.go` – summarization and pruning logic for long conversations.
- `interagent.go` – inter-agent messaging implementation backing `agent_message`.
- `tools.go` – wiring between agents and concrete tools in `internal/tools`.

In practice, most agent behavior is driven by configuration (agent JSON in the workspace) plus the system prompt template, with `internal/agents` providing the runtime machinery.

## Skills (`internal/skills`)

Skills are declarative, markdown frontmatter-defined bundles of tools and prompts that extend what an agent can do without changing Go code.

High-level flow:

1. TeaNode loads all skill definitions (markdown `.md` files) from the configured skills directory.
2. Local top-level skills are loaded first; installed registry skills are loaded from `.installed/<name>/<version>/skill.md`.
3. For duplicate names, local skills win over installed ones, and installed skills use the newest version.
4. Each skill defines one or more tools (`shell`, `http`, or `workflow`) and an optional prompt body.
5. Agents can allowlist specific skills; enabled skill tools are exposed as normal function tools to the model.
6. Skill prompts are appended to the system prompt for enabled skills.

Important files:

- `types.go` – core structs for skills and tools (`SkillDefinition`, `ToolDefinition`, `ActionDefinition`).
- `loader.go` – loads and validates markdown skill files:
  - Reads `*.md` from the skills directory.
  - Validates required fields and workflow constraints.
  - Logs and skips malformed entries instead of failing the whole process.
- `register.go` – registers loaded skills and their tools in the global tool registry.
- `tool.go` – runtime execution for skill tools.
- `skills.go` – convenience helpers for working with registered skills.
- `registry_tool.go` – `skills` multi-action management tool (`list_registry`, `search`, `install`, `list_installed`, `update`, `uninstall`).
- `registry_backend.go` – install/update/uninstall/search implementation for online registries.
- `local_list.go` – local skill listing for UI/API.

### Skill file format

Each skill file is markdown with YAML frontmatter:

```md
---
name: example
description: Example skill
runtimeMinVersion: 0.1.0
tools:
  - name: ping
    description: Simple shell tool
    type: shell
    command: ["echo", "pong"]
    parameters:
      type: object
      properties: {}
---

Optional prompt text for system prompt injection.
```

### Tool types

- `shell`
  - Runs command/args directly (`exec.CommandContext`).
  - Supports templating in each command element.
  - Supports timeout and working directory.
  - Validates required input parameters from JSON schema `required`.

- `http`
  - Executes HTTP request with templated method/url/body/headers.
  - Supports timeout and response truncation.
  - Non-2xx responses return errors with body snippets.
  - Validates required input parameters from JSON schema `required`.

- `workflow`
  - Runs sequential multi-step actions (`steps`), each step being `shell` or `http`.
  - Also supports control-flow steps: `forEach` and `switch`.
  - Supports first-class multi-action routing using `actions` + `actionField`.
  - Step outputs are addressable in later steps (`steps.<name>`, `last`).
  - Supports conditional execution (`if`), retries (`retries`, `retryDelayMs`), and error policy (`onError: fail|continue`).
  - Supports always-run cleanup via workflow-level `finally`.
  - Supports structured output shaping for JSON-producing steps (`result: json`, `extract`, `select`, `saveAs`).
  - Supports optional output contract checks via `outputSchema`.
  - Returns structured execution metadata (step status, attempts, duration, output/error).
  - Validates required input parameters from JSON schema `required`.

### Template expressions

Tool fields and workflow step fields support `{{...}}` expressions with dotted paths and filters:

- Path lookup:
  - `{{location}}`
  - `{{steps.fetch.id}}`
- Filters:
  - `json`
  - `urlencode`
  - `base64`
  - `default:<text>`
  - `join:<separator>`

Secret and environment lookups:

- `{{secret:NAME}}` (TeaNode config `secrets.NAME`, then environment fallback)
- `{{env:NAME}}` (environment only)

Examples:

- `{{query|urlencode}}`
- `{{tags|join:,}}`
- `{{steps.fetch|json}}`
- `{{missing|default:unknown}}`

### Workflow step fields

Common step fields:

- `name`
- `type` (`shell` or `http`)
- `if`
- `timeout`
- `retries`
- `retryDelayMs`
- `onError` (`fail` or `continue`)
- `result` (`text` or `json`)
- `extract` (path into parsed JSON output)
- `select` (map of output key to path in parsed JSON output)
- `saveAs` (custom output key under `steps`)
- `outputSchema` (optional shape/type validation for step output)
- `auth` (shared HTTP auth profile name)

Shell step fields:

- `command`
- `workdir`

HTTP step fields:

- `method`
- `url`
- `headers`
- `body`

`forEach` step fields:

- `forEach` (path to iterable array, e.g. `steps.devices`)
- `as` (loop variable name, default `item`)
- `steps` (nested steps run for each item)

`switch` step fields:

- `switch` (path/expression to match)
- `cases` (array of `{ match, steps }`)
- `default` (steps to run when no case matches)

Workflow tool routing fields:

- `actionField` (selector input field name, default `action`)
- `actions` (map of action name to step list)

Shared HTTP auth profiles (`httpAuth`) at skill level:

- `bearer`: token-based authorization
- `basic`: username/password
- `apiKey`: header/query key injection

TeaNode config support for secrets:

- Add to `config.yaml`:
  - `secrets: { MY_TOKEN: "...", OTHER_SECRET: "..." }`
- Skills can consume these via `{{secret:MY_TOKEN}}`.
- Environment variables are used as fallback for missing config secrets.

### Skill definition basics

A minimal example of a skill (conceptual, see `skills.examples/` for real files):

```md
---
name: git-helper
description: High-level Git helper commands.
tools:
  - name: git_status
    description: Show current Git status.
    type: shell
    command: ["git", "status", "--short", "--branch"]
    timeout: 5
  - name: git_remote_info
    description: Show configured remotes.
    type: shell
    command: ["git", "remote", "-v"]
    timeout: 5
---

You have tools for inspecting and modifying a local Git repository.
Prefer these tools over running arbitrary shell commands.
```

This keeps responsibilities clean:

- `internal/tools` implements low-level primitives (shell, HTTP, filesystem, etc.).
- `internal/skills` composes those primitives into higher-level, reusable skills that can be shared across agents and workspaces.
