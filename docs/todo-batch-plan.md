# Batch Operations for `conversation_todo` and `project_todo`

**Status:** Draft
**Date:** 2026-03-02
**Scope:** Planning only — no implementation

---

## 1. Motivation

Today both `conversation_todo` and `project_todo` accept exactly one action per
tool call.  An LLM that needs to create a five-item checklist, or mark three
tasks done at once, must issue five or three sequential calls.  This is:

* **Slow** — each call is a full tool-use round-trip.
* **Fragile** — a failure midway leaves the list half-updated with no rollback.
* **Token-heavy** — repeated boilerplate for each item.

Batch operations let the LLM send a single call containing multiple items,
receive a single structured response, and handle partial failures explicitly.

---

## 2. Design Principles

| Principle | Detail |
|-----------|--------|
| **No backward compatibility** | We do not care about backward compat. The existing single-item actions are **replaced** by batch-only semantics. The `action` enum is simplified — old callers must update. |
| **Partial-failure by default** | Each item in a batch succeeds or fails independently; the response reports per-item status. No all-or-nothing transaction. |
| **Minimal schema** | Keep the tool schema as flat and small as possible. No idempotency keys, no filtering, no pagination — just CRUD in batches. |
| **LLM ergonomics** | Flat where possible, avoids deeply nested objects, keeps required fields to a minimum. |

---

## 3. Proposed API Shape

### 3.1 Simplified Actions

The tools have **three** actions:

| Action | Purpose |
|--------|---------|
| `list` | List all todos (unchanged from today) |
| `batch` | Create, update, complete, reopen, or delete one or more todos |
| `prune` | Remove all completed todos (unchanged from today) |

All other single-item actions (`add`, `update`, `complete`, `reopen`, `delete`)
are **removed**.  Callers always use `batch` with an `items` array, even for a
single item.

### 3.2 `action: "batch"` — Request

```
action: "batch"          (required)
projectId / projectName: (project_todo only, required — scope)
conversationId:          (conversation_todo only, optional — defaults to current)
items: [                 (required, 1–50 items)
  {
    op:          string  (required — "add" | "update" | "complete" | "reopen" | "delete")
    todoId:      string  (required for update/complete/reopen/delete)
    title:       string  (required for add, optional for update)
    description: string  (optional)
    priority:    string  (optional — "low" | "medium" | "high")
    tags:        [str]   (optional)
  },
  ...
]
```

**Validation rules:**

* `items` must contain 1–50 entries.
* Each item is validated independently against the rules of its `op`:
  - `add` requires `title`.
  - `update`, `complete`, `reopen`, `delete` require `todoId`.

### 3.3 `action: "batch"` — Response

```jsonc
{
  "action": "batch",
  "results": [
    {
      "index": 0,
      "op": "add",
      "success": true,
      "todo": { /* full Todo object */ }
    },
    {
      "index": 1,
      "op": "complete",
      "success": true,
      "todo": { /* updated Todo object */ }
    },
    {
      "index": 2,
      "op": "delete",
      "success": false,
      "error": "todo not found",
      "todoId": "01abc..."
    }
  ],
  "summary": {
    "total": 3,
    "succeeded": 2,
    "failed": 1
  },
  "totalCount": 12,
  "openCount": 8,
  "doneCount": 4
}
```

**Key semantics:**

* `results` preserves the input order via `index`.
* A failed item does not abort subsequent items.
* Every result carries `success: bool`.  Failed results include `error: string`.
* Aggregate counts are computed once after all items are processed.

---

## 4. Full JSON Schema (Tool Definition)

The tool input schema is **replaced** (not patched):

```jsonc
{
  "properties": {
    "action": {
      "type": "string",
      "enum": ["list", "batch", "prune"]
    },
    "items": {
      "type": "array",
      "minItems": 1,
      "maxItems": 50,
      "description": "Required when action is 'batch'. Each element describes one operation.",
      "items": {
        "type": "object",
        "required": ["op"],
        "properties": {
          "op": {
            "type": "string",
            "enum": ["add", "update", "complete", "reopen", "delete"]
          },
          "todoId":      { "type": "string" },
          "title":       { "type": "string", "maxLength": 512 },
          "description": { "type": "string" },
          "priority":    { "type": "string", "enum": ["low", "medium", "high"] },
          "tags":        { "type": "array", "items": { "type": "string" } }
        }
      }
    }
  }
}
```

---

## 5. Example Payloads

### 5.1 `project_todo` — Batch Add + Complete

**Request:**
```json
{
  "action": "batch",
  "projectName": "teanode",
  "items": [
    {
      "op": "add",
      "title": "Implement OAuth2 refresh token rotation",
      "priority": "high",
      "tags": ["auth", "security"]
    },
    {
      "op": "add",
      "title": "Add structured logging to API layer",
      "priority": "medium",
      "tags": ["observability"]
    },
    {
      "op": "complete",
      "todoId": "01kjq2y7mkrgv28g12g4qcv4ba"
    }
  ]
}
```

**Response:**
```json
{
  "action": "batch",
  "results": [
    {
      "index": 0,
      "op": "add",
      "success": true,
      "todo": {
        "id": "01kxm3a9npqrs...",
        "projectId": "01kjkh2czp7vsqeyhyc6m917j3",
        "title": "Implement OAuth2 refresh token rotation",
        "status": "open",
        "priority": "high",
        "tags": ["auth", "security"],
        "createdAt": "2026-03-02T10:00:00Z",
        "modifiedAt": "2026-03-02T10:00:00Z"
      }
    },
    {
      "index": 1,
      "op": "add",
      "success": true,
      "todo": {
        "id": "01kxm3a9npqrt...",
        "projectId": "01kjkh2czp7vsqeyhyc6m917j3",
        "title": "Add structured logging to API layer",
        "status": "open",
        "priority": "medium",
        "tags": ["observability"],
        "createdAt": "2026-03-02T10:00:01Z",
        "modifiedAt": "2026-03-02T10:00:01Z"
      }
    },
    {
      "index": 2,
      "op": "complete",
      "success": true,
      "todo": {
        "id": "01kjq2y7mkrgv28g12g4qcv4ba",
        "projectId": "01kjkh2czp7vsqeyhyc6m917j3",
        "title": "Set up CI pipeline",
        "status": "done",
        "priority": "high",
        "tags": ["infra"],
        "completedAt": "2026-03-02T10:00:01Z",
        "createdAt": "2026-02-28T09:00:00Z",
        "modifiedAt": "2026-03-02T10:00:01Z"
      }
    }
  ],
  "summary": { "total": 3, "succeeded": 3, "failed": 0 },
  "totalCount": 15,
  "openCount": 12,
  "doneCount": 3
}
```

### 5.2 `conversation_todo` — Batch with Partial Failure

**Request:**
```json
{
  "action": "batch",
  "items": [
    {
      "op": "complete",
      "todoId": "01kxexists00000000000000aa"
    },
    {
      "op": "delete",
      "todoId": "01kxnotfound0000000000000bb"
    },
    {
      "op": "add",
      "title": "Summarize meeting notes"
    },
    {
      "op": "update",
      "todoId": "01kxexists00000000000000cc",
      "priority": "high",
      "tags": ["urgent", "review"]
    }
  ]
}
```

**Response:**
```json
{
  "action": "batch",
  "results": [
    {
      "index": 0,
      "op": "complete",
      "success": true,
      "todo": { "id": "01kxexists00000000000000aa", "status": "done", "..." : "..." }
    },
    {
      "index": 1,
      "op": "delete",
      "success": true,
      "todoId": "01kxnotfound0000000000000bb"
    },
    {
      "index": 2,
      "op": "add",
      "success": true,
      "todo": { "id": "01kxm3newid...", "title": "Summarize meeting notes", "..." : "..." }
    },
    {
      "index": 3,
      "op": "update",
      "success": false,
      "error": "todo not found",
      "todoId": "01kxexists00000000000000cc"
    }
  ],
  "summary": { "total": 4, "succeeded": 3, "failed": 1 },
  "totalCount": 5,
  "openCount": 3,
  "doneCount": 2
}
```

---

## 6. Changes by Layer

### 6.1 Model (`models/todo.go`)

No model changes needed.

### 6.2 Store Interface (`store/interfaces.go`)

No new store methods needed. The tool handler iterates the `items` array and
calls existing single-item store methods (`CreateTodo`, `ModifyTodo`,
`DeleteTodo`) in a loop, collecting per-item results.

If performance becomes a concern later, batch store methods can be added as an
optimization without changing the tool API.

### 6.3 Tool Handlers (`tools/projects/todo.go`, `tools/conversations/todo.go`)

Replace the existing action switch:

* Remove individual `add`, `update`, `complete`, `reopen`, `delete` cases.
* Add `case "batch":` that:
  1. Validates the `items` array (length, per-item `op` + required fields).
  2. Iterates items, calling existing store methods per item.
  3. Collects results in input order.
  4. Calls the parent-scope ModifiedAt updater once.
  5. (conversation_todo) Emits a single pubsub event.
* Keep `list` and `prune` unchanged.

### 6.4 Tool Input Schema

Replace the schema entirely with the simplified version from Section 4.

### 6.5 WebSocket API (`api/v1api/rpc_todos.go`)

Add one new RPC endpoint:

* `conversations.todos.batch` — mirrors the batch tool action.

Reuse the same request/response types.

### 6.6 Pubsub Events

For batch operations on conversation todos, emit a single event:

```jsonc
{
  "type": "conversation.todos",
  "conversationId": "...",
  "userId": "...",
  "action": "batch",
  "results": [ /* same as tool response results */ ]
}
```

---

## 7. Testing Plan

### 7.1 Unit Tests (tool handler)

| Test | Covers |
|------|--------|
| `TestBatch_AddMultiple` | Create 5 todos, verify all returned with correct fields |
| `TestBatch_CompleteMultiple` | Complete 3 todos at once |
| `TestBatch_DeleteMultiple` | Delete 3 todos at once |
| `TestBatch_MixedOps` | One batch with add + complete + delete + update |
| `TestBatch_PartialFailure` | One item fails (bad todoId), others succeed |
| `TestBatch_AddMissingTitle` | Add without title → per-item error |
| `TestBatch_UpdateMissingId` | Update without todoId → per-item error |
| `TestBatch_MaxItems` | 50 items succeeds; 51 returns validation error |
| `TestBatch_EmptyItems` | `items: []` → validation error |
| `TestBatch_CompleteAlreadyDone` | Idempotent — success |
| `TestBatch_ReopenAlreadyOpen` | Idempotent — success |
| `TestBatch_SingleItem` | Batch with 1 item works fine |

### 7.2 WebSocket API Tests

* `TestRpcBatchTodos_HappyPath`
* `TestRpcBatchTodos_PartialFailure`
* `TestRpcBatchTodos_Unauthorized`

---

## 8. Implementation Plan

### Phase 1: Tool Handlers

1. Replace action switch in both tool handlers.
2. Update tool input schema.
3. Unit test tool handlers.

### Phase 2: WebSocket API

1. Register new RPC endpoint.
2. Update pubsub event handling.
3. API tests.

### Phase 3: Documentation

1. Update tool description text to explain batch usage.
2. Update `docs/project_todo_tool.md` design doc.

No migration needed. No model changes. No new store methods.
