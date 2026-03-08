# AgeMem-inspired scoped memory tools (TeaNode plan)

This document proposes an **AgeMem-inspired memory tool interface** for TeaNode, implemented in a way that is consistent with TeaNode’s existing **scoped workspace tools** (`agent_workspace`, `user_workspace`, `project_workspace`).

The goal is to empower the LLM to **organize learning over time** with:

- Explicit memory operations as tool calls (AgeMem Table 1)
- Stable, addressable long-term memory items (IDs, metadata)
- Scoping that prevents cross-contamination (agent vs user vs project)
- Retrieval that can evolve from keyword → hybrid/semantic
- Clear observability hooks for “reward-like” metrics (context efficiency, memory quality)

## Why three tools (agent/user/project) instead of one `memory` tool with `scope`

TeaNode already uses separate tools for scoped persistence:

- `agent_workspace`
- `user_workspace`
- `project_workspace` (requires `projectId`)

This separation reduces the model’s error surface: the tool name encodes the scope, so the model cannot accidentally store project-specific knowledge into user memory, etc.

**Recommendation:** implement a single underlying memory tool implementation, instantiated 3 times with different `resolveScope` logic (mirrors `internal/tools/workspace/workspace.go`).

## Proposed tools

### Tool names

- `agent_memory`
  - Scope: `agent` (implicit via runner AgentID)
- `user_memory`
  - Scope: `user` (implicit via authenticated user ID)
- `project_memory`
  - Scope: `project` (explicit `projectId` input)

### Action set (AgeMem-aligned)

AgeMem uses 6 memory actions:

- LTM: `ADD`, `UPDATE`, `DELETE`
- STM: `RETRIEVE`, `SUMMARY`, `FILTER`

TeaNode tool action names should be lower_snake / lower camel consistently with existing tools. Existing patterns use strings like `"read"`, `"write"`, etc.

**Proposed action enum:**

- `add` (LTM)
- `update` (LTM)
- `delete` (LTM)
- `retrieve` (STM→context injection pathway)
- `summary` (STM compression)
- `filter` (STM noise suppression)

Additionally, TeaNode will need *operational* actions for listing / inspection:

- `get` (by ID)
- `list` (by tag/time)
- `search` (keyword/hybrid/semantic; optional separate from `retrieve`)

Whether to split `search` vs `retrieve`:
- `retrieve` should be the “I want context to use now” action (ranked, returns snippets)
- `search` can be “exploratory recall” returning more metadata

You can implement only `retrieve` initially and add `search` later; schema below includes both for forward-compat.

## Data model (storage)

### Long-term memory item

A durable item stored in a dedicated table/collection (not workspace files):

Fields (suggested):

- `id` (ULID)
- `scope` (`agent|user|project`)
- `scopeId`
- `title` (optional)
- `content` (text/markdown)
- `tags` (string array)
- `source`
  - `type`: e.g. `conversation`, `tool_call`, `workspace_file`, `manual`
  - `ref`: e.g. conversation ID, message ID, tool call ID
- `createdAt`, `modifiedAt`
- `archivedAt` (optional)
- (future) `embedding` vector + embedding model name/version

### STM artifacts

`summary` and `filter` operate on conversation context, but can optionally produce LTM items.

For implementation simplicity:
- treat `summary` / `filter` as operations that produce text + optional `memoryItemId` if persisted.
- conversation-scoped “filtered view” can be implemented later by extending conversation state.

## Tool schemas (JSON)

The following is a concrete JSON-schema-style definition for tool inputs/outputs. It is designed to match TeaNode’s current style:

- Input fields: `action` plus per-action parameters
- Output: a JSON object encoded as a string (TeaNode tool convention)

### Shared input schema (all three tools)

```json
{
  "type": "object",
  "properties": {
    "action": {
      "type": "string",
      "enum": ["add", "update", "delete", "get", "list", "search", "retrieve", "summary", "filter"],
      "description": "The memory action to perform."
    },

    "id": { "type": "string", "description": "Memory item ID (get/update/delete)." },

    "title": { "type": "string", "description": "Optional title for a memory item." },
    "content": { "type": "string", "description": "Content to store/update." },
    "tags": {
      "type": "array",
      "items": { "type": "string" },
      "description": "Optional tags for organization and filtering."
    },

    "query": { "type": "string", "description": "Query string for search/retrieve." },
    "limit": { "type": "integer", "description": "Max number of results (default e.g. 10)." },

    "mode": {
      "type": "string",
      "enum": ["keyword", "hybrid", "semantic"],
      "description": "Retrieval mode. Initial implementation may support only keyword/hybrid."
    },

    "summaryTarget": {
      "type": "string",
      "enum": ["recent", "range"],
      "description": "What part of the conversation to summarize."
    },
    "range": {
      "type": "object",
      "properties": {
        "fromMessageIndex": { "type": "integer" },
        "toMessageIndex": { "type": "integer" }
      },
      "description": "Conversation message index range when summaryTarget=range."
    },

    "persist": {
      "type": "boolean",
      "description": "Whether to persist the summary/filter result into LTM in this scope."
    }
  },
  "required": ["action"]
}
```

### `project_memory` additional input requirement

`project_memory` requires:

```json
{
  "projectId": {
    "type": "string",
    "description": "Project ID for project memory operations."
  }
}
```

and adds `projectId` to `required`.

### Shared output schema

```json
{
  "type": "object",
  "properties": {
    "action": { "type": "string" },
    "success": { "type": "boolean" },

    "item": {
      "type": "object",
      "properties": {
        "id": { "type": "string" },
        "title": { "type": "string" },
        "content": { "type": "string" },
        "tags": { "type": "array", "items": { "type": "string" } },
        "createdAt": { "type": "string" },
        "modifiedAt": { "type": "string" }
      }
    },

    "items": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "id": { "type": "string" },
          "title": { "type": "string" },
          "snippet": { "type": "string", "description": "Short excerpt for retrieval results." },
          "tags": { "type": "array", "items": { "type": "string" } },
          "score": { "type": "number", "description": "Optional ranking score." }
        }
      }
    },

    "summary": { "type": "string", "description": "Summary/filter output." },
    "persistedId": { "type": "string", "description": "If persisted=true, ID of created/updated memory item." }
  }
}
```

## Semantics of actions

### `add`
- Creates a new LTM item.
- Input: `content` required; `title`, `tags` optional.
- Output: `item` with `id`.

### `update`
- Updates existing LTM item by `id`.
- Input: `id` required; at least one of `title/content/tags` required.
- Output: updated `item`.

### `delete`
- Deletes (or archives) item by `id`.
- Output: `{ success: true }`.

### `retrieve`
- Ranked retrieval intended to be inserted into the agent’s working context.
- Input: `query` required; `limit`, `mode` optional.
- Output: `items[]` each with `snippet` + `id`.

### `search`
- Similar to retrieve but can return more metadata and potentially more results.

### `summary` / `filter`
- Operate on conversation context.
- Input: `summaryTarget` + `range` (if needed), `persist`.
- Output: `summary` and optionally `persistedId`.

**Note:** implementing true STM filtering requires conversation-state extensions. Initial implementation can return a proposed filtered summary for the agent to use, while TeaNode continues to store raw messages.

## Implementation plan (concrete file map)

### 1) Storage
- Add store interface methods:
  - `CreateMemoryItem`
  - `GetMemoryItem`
  - `UpdateMemoryItem`
  - `DeleteMemoryItem`
  - `SearchMemoryItems`
- DB migration:
  - create `memory_items` table (and optionally `memory_item_tags` if normalized)

Suggested locations:
- `internal/store/interfaces.go` (extend Transaction)
- `internal/store/dbstore/dbmigrations/` (new migration)
- `internal/store/dbstore/database_memory.go` (new)
- `internal/models/memory_item.go` (new)

### 2) Tools
- `internal/tools/memory/memory.go` registering 3 tools
- Pattern after `internal/tools/workspace/workspace.go`:
  - `newMemoryTool(memoryToolConfiguration{ name, description, scopeIdParameterName, resolveScope, afterMutate })`

### 3) Runner / prompt integration (optional but recommended)
- Add a “memory context builder” in `internal/runners/systemprompt.go` that retrieves a small number of relevant memory snippets for the *current query*.
- Initially keep it off by default behind agent config.

### 4) Observability hooks
- Emit structured events after memory actions:
  - counts, sizes, latency
  - retrieved item IDs
- Use these to approximate AgeMem reward components over time.

## Open decisions

1) **Delete vs archive**: prefer archive (soft-delete) to support maintenance + debugging.
2) **Where to store embeddings**: in `memory_items` table (pgvector) vs side-table.
3) **Permission model for `project_memory`**: should validate membership before read/write.
4) **Deduplication policy**: optional background job to merge near-duplicates.

