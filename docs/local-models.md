# Running against a local model

Locally hosted models (Ollama, llama.cpp, vLLM, LM Studio) are usually served
with an 8k-32k context window. TeaNode's static prefix — the system prompt plus
every tool definition — used to take about 23,000 tokens before the first user
message, which does not fit. TeaNode now shrinks that prefix automatically.

## Setup

Point a provider at the local server and tell TeaNode the model's context
window:

```yaml
models:
  default: local:qwen3
  contextWindow: 32768
  providers:
    - name: local
      baseUrl: http://127.0.0.1:11434/v1
      apiKey: ""
```

`models.contextWindow` is what selects the behavior below, so set it to the
window the server is actually running with.

## The compact prompt profile

A run switches from the `full` prompt profile to `compact` when the static
prefix would take more than 25% of the context window. The prefix is paid on
every request and conversation compaction cannot reclaim it, so the remaining
75% is what the conversation actually has to work with.

This is a budget rather than a fixed context-window cutoff because the prefix
varies by an order of magnitude with how many tools are registered. A node
running a handful of tools stays on the full profile at 32k; a node with every
integration enabled goes compact well above it. Two examples:

| Context window | Registered tools | Prefix | Share | Profile |
| --- | --- | --- | --- | --- |
| 40,000 | 53 | ~22,200 | 55% | compact |
| 40,000 | ~10 | ~5,000 | 13% | full |
| 200,000 | 53 | ~22,200 | 11% | full |

Under the compact profile:

| | full | compact |
| --- | --- | --- |
| Tool definitions | all registered tools | core tools only, rest deferred |
| Tool `returns` schemas | sent | dropped |
| System prompt guidance | long form | short form |
| AGENT.md / USER.md / ONBOARDING.md / SKILLS.md | 8,000 characters each | 2,000 characters each |

On a node with 53 tools registered, that is about 22,200 tokens of prefix under
`full` and about 3,400 under `compact`. Check which profile a run picked with
debug logging — the compact path logs the measured prefix and the window it was
compared against.

Nothing is disabled by the compact profile. Every tool stays callable and every
prompt section still covers its feature — charts, artifacts, and suggested
replies included — just in fewer words.

### Deferred tools and `tool_search`

Under the compact profile only the core tools ship their full definitions. The
rest are listed in an "Additional Tools" section of the system prompt as one
line each:

```
- gitlab_issues: Interact with GitLab issues.
- google_calendar: Interact with Google Calendar.
```

To call one, the model first loads it with `tool_search`, by exact name or by
keyword. The loaded tool's full definition appears in the next request, and the
model calls it on the following turn. One `tool_search` call loads at most five
tools, so a broad query cannot undo the saving.

Deferral is skipped when the agent's tool allowlist has already cut the tool set
down to roughly the core set — at that point the catalog and the extra round
cost more than the definitions they replace.

### Choosing the core tools

The default core set is general-purpose tools with no external service behind
them, plus the user-scoped memory and workspace so recall questions do not need
a lookup round:

`ask_user_question`, `datetime`, `filesystem`, `shell`, `user_memory`,
`user_workspace`, `web_fetch`, `web_search`

Override it when a different set matches what the agent actually does:

```yaml
tools:
  coreTools:
    - shell
    - filesystem
    - user_memory
    - gitlab_merge_requests
```

Keep this list short. Every tool on it is paid for on every request.

## Other things that help

- **Trim `AGENT.md`.** It goes into every request. Under the compact profile it
  is cut to 2,000 characters, and a truncated instruction file is worse than a
  short one.
- **Narrow the agent's tool allowlist.** An agent's `tools:` list removes tools
  from the run entirely, so they cost nothing at all — better than deferring
  them when the agent will never need them.
- **Prefer one broad tool over several narrow ones** in skills. Each tool
  carries its own name, description, and parameter schema.

## Related

- [Getting Started](getting-started.md) — installation and configuration
- [Agents and Skills](agents-and-skills.md) — system prompt composition
