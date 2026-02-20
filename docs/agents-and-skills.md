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

Skills are declarative, YAML-defined bundles of tools and prompts that extend what an agent can do without changing Go code.

High-level flow:

1. TeaNode loads all skill definitions (YAML files) from the configured skills directory (for the default gateway this is typically `~/.teanode/skills/`, but it can be overridden via the `--dir` flag or config).
2. Each skill defines a set of tools (shell or HTTP) and an optional prompt.
3. When an agent is created, it can be configured to enable one or more skills.
4. The agent surfaces the skill tools to the LLM as regular function tools, and includes the skill prompt in its system prompt when active.

Important files:

- `types.go` – core structs for skills and tools (`SkillDefinition`, `ToolDefinition`).
- `loader.go` – loads and validates YAML skill files:
  - Reads `*.yaml` from the skills directory.
  - Validates required fields (e.g. shell `command`, HTTP `url`).
  - Logs and skips malformed entries instead of failing the whole process.
- `register.go` – registers loaded skills and their tools in the global tool registry.
- `tool.go` – runtime execution for skill tools:
  - For **shell** tools, runs `sh -c` with a working directory and timeout.
  - For **HTTP** tools, performs the HTTP request (method, URL, headers, body) with a timeout.
- `skills.go` – convenience helpers for working with registered skills.

### Skill definition basics

A minimal example of a skill (conceptual, see `skills.examples/` for real files):

```yaml
name: git-helper
description: High-level Git helper commands.
prompt: |
  You have tools for inspecting and modifying a local Git repository.
  Prefer these tools over running arbitrary shell commands.
tools:
  - name: git_status
    description: Show current Git status.
    type: shell
    command: git status --short --branch
    timeout: 5s
  - name: git_remote_info
    description: Show configured remotes.
    type: shell
    command: git remote -v
    timeout: 5s
```

This keeps responsibilities clean:

- `internal/tools` implements low-level primitives (shell, HTTP, filesystem, etc.).
- `internal/skills` composes those primitives into higher-level, domain-specific capabilities.
- `internal/agents` exposes both primitives and skills to the LLM via the tool-calling interface.
