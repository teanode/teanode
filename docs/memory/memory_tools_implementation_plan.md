# Memory Tools Implementation Plan

## 1. Overview & Goals

Add **AgeMem-inspired scoped memory tools** to TeaNode, giving agents explicit CRUD + search operations over durable, ID-addressed memory items. The implementation mirrors the existing workspace feature end-to-end: same `(scope, scope_id)` model, same tool-factory pattern, same store interface shape, same test structure.

**Goals:**

1. Let agents `add`, `update`, `delete`, `get`, `list`, and `search` durable memory items scoped to agent / user / project.
2. Reuse the `workspaceTool` instantiation pattern — one implementation, three tool instances.
3. Keep retrieval keyword-first in v1; leave schema room for embeddings/semantic retrieval later.
4. Emit structured observability events after every memory action to approximate AgeMem reward components.
5. Keep v1 minimal: dbstore only, no fsstore parity, no embeddings, no STM actions.

---

## 2. Tool Surface

Three tools, each an instance of a single `memoryTool` (mirrors `workspaceTool` in `internal/tools/workspace/workspace.go`):

| Tool | Scope | Scope ID Source | Extra Required Param |
|---|---|---|---|
| `agent_memory` | `models.ScopeAgent` | `runners.RunnerFromContext(ctx).AgentID` | — |
| `user_memory` | `models.ScopeUser` | `models.UserFromContext(ctx).ID` | — |
| `project_memory` | `models.ScopeProject` | input `projectId` | `projectId` |

### Actions (v1)

| Action | Category | Required Params | Optional Params | Output |
|---|---|---|---|---|
| `add` | LTM write | `content` | `title`, `tags` | `{ action, item }` |
| `update` | LTM write | `id` + at least one of `title`/`content`/`tags` | — | `{ action, item }` |
| `delete` | LTM write | `id` | — | `{ action, success }` |
| `get` | LTM read | `id` | — | `{ action, item }` |
| `list` | LTM read | — | `tags`, `maxResults` | `{ action, items }` |
| `search` | LTM read | `query` | `maxResults` | `{ action, matches }` |

**Deferred to v2:** `retrieve` (ranked context injection), `summary`, `filter` (STM operations requiring conversation-state extensions).

### Input Schema (shared, all three tools)

```json
{
  "type": "object",
  "properties": {
    "action":     { "type": "string", "enum": ["add","update","delete","get","list","search"] },
    "id":         { "type": "string", "description": "Memory item ID (get/update/delete)." },
    "title":      { "type": "string", "description": "Optional title for a memory item." },
    "content":    { "type": "string", "description": "Content to store or update." },
    "tags":       { "type": "array", "items": { "type": "string" }, "description": "Tags for organization and filtering." },
    "query":      { "type": "string", "description": "Search query string (search action)." },
    "maxResults": { "type": "integer", "description": "Maximum results to return, default 10 (list/search)." }
  },
  "required": ["action"]
}
```

`project_memory` adds `"projectId"` to `properties` and `required` (same pattern as `project_workspace`).

### Output Schema

```json
{
  "action":  "string",
  "success": "boolean",
  "item": {
    "id": "string", "title": "string", "content": "string",
    "tags": ["string"], "createdAt": "string", "modifiedAt": "string"
  },
  "items": [
    { "id": "string", "title": "string", "tags": ["string"],
      "createdAt": "string", "modifiedAt": "string" }
  ],
  "matches": [
    { "id": "string", "title": "string", "snippet": "string",
      "tags": ["string"] }
  ]
}
```

---

## 3. Data Model & Store Interface

### 3.1 Model: `MemoryItem`

New file: `internal/models/memory_item.go`

```go
package models

import "time"

type MemoryItem struct {
    ID         string     `json:"id,omitempty" yaml:"id,omitempty"`
    CreatedAt  *time.Time `json:"createdAt,omitempty" yaml:"createdAt,omitempty"`
    ModifiedAt *time.Time `json:"modifiedAt,omitempty" yaml:"modifiedAt,omitempty"`

    Scope   *Scope  `json:"scope,omitempty" yaml:"scope,omitempty"`
    ScopeID *string `json:"scopeId,omitempty" yaml:"scopeId,omitempty"`

    Title   *string   `json:"title,omitempty" yaml:"title,omitempty"`
    Content *string   `json:"content,omitempty" yaml:"content,omitempty"`
    Tags    *[]string `json:"tags,omitempty" yaml:"tags,omitempty"`

    ArchivedAt *time.Time `json:"archivedAt,omitempty" yaml:"archivedAt,omitempty"`
}
```

Design notes:
- `Content` is `*string` for text-based memory content, stored as TEXT in Postgres.
- `Tags` stored as JSONB in Postgres.
- `ArchivedAt` enables soft-delete.
- Uses existing `models.Scope` constants.
- Optional fields are pointers — same convention as `WorkspaceFile`.

### 3.2 Store Interface: `MemoryItemOperation`

Added to `internal/store/interfaces.go`, embedded in `Transaction`:

```go
type Transaction interface {
    // ... existing operations ...
    WorkspaceFileOperation
    MemoryItemOperation
    // ...
}

type MemoryItemOperation interface {
    CreateMemoryItem(ctx context.Context, item *models.MemoryItem, options *Option) (*models.MemoryItem, error)
    GetMemoryItem(ctx context.Context, memoryItemID string, options *Option) (*models.MemoryItem, error)
    ModifyMemoryItem(ctx context.Context, memoryItemID string, modifier func(*models.MemoryItem) error, options *Option) (*models.MemoryItem, error)
    DeleteMemoryItem(ctx context.Context, memoryItemID string, options *Option) error
    ListMemoryItems(ctx context.Context, scope models.Scope, scopeID string, listOptions MemoryItemListOptions, options *Option) ([]*models.MemoryItem, error)
    SearchMemoryItems(ctx context.Context, scope models.Scope, scopeID string, query string, searchOptions MemoryItemSearchOptions, options *Option) ([]MemoryItemSearchResult, error)
}
```

### 3.3 Options & Result Types

Added to `internal/store/types.go`:

```go
type MemoryItemListOptions struct {
    Tags            *[]string
    IncludeArchived *bool
    Limit           *uint64
}

type MemoryItemSearchOptions struct {
    Limit           *uint64
    IncludeContent  *bool
    CaseSensitive   *bool
    IncludeArchived *bool
}

type MemoryItemSearchResult struct {
    MemoryItemID *string
    Scope        *models.Scope
    ScopeID      *string
    Title        *string
    Tags         *[]string
    MatchedLines *[]string
    Score        *float64
}
```

---

## 4. DB Migration & fsstore Decision

### 4.1 Migration: `0004_memory_items.sql`

New file: `internal/store/dbstore/dbmigrations/0004_memory_items.sql`

```sql
CREATE TABLE IF NOT EXISTS memory_items (
    id          VARCHAR(32) PRIMARY KEY,
    scope       VARCHAR(32) NOT NULL,
    scope_id    VARCHAR(32) NOT NULL,
    title       TEXT NULL,
    content     BYTEA NOT NULL DEFAULT ''::bytea,
    tags        JSONB NULL,
    archived_at TIMESTAMPTZ NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    modified_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS memory_items_scope_scope_id_index
    ON memory_items (scope, scope_id);
CREATE INDEX IF NOT EXISTS memory_items_scope_scope_id_modified_at_index
    ON memory_items (scope, scope_id, modified_at DESC);
```

Reverse migration: `0004_memory_items.reverse.sql`

```sql
DROP TABLE IF EXISTS memory_items;
```

Alignment with `workspace_files` table:
- Same `(scope, scope_id)` columns and composite index pattern.
- Same `BYTEA content`, `TIMESTAMPTZ` timestamp columns.
- No unique constraint on `(scope, scope_id, title)` — memory items are ID-keyed, not path-keyed.
- `tags JSONB` — add GIN index in a future migration if tag-filtered queries become slow.

### 4.2 fsstore Parity: NOT in v1

Memory items are ID-keyed (not path-keyed), making filesystem storage less natural than for workspace files. Memory tools will be **dbstore-only** in v1.

The fsstore `fileSystemTransaction` must still satisfy the `Transaction` interface. Add stub methods that return a new `store.ErrNotImplemented` sentinel error. This matches how other store-specific features would be gated.

If fsstore parity is needed later, store items as JSON files: `<dataDir>/memory_items/<scope>/<scopeId>/<id>.json`.

---

## 5. File-by-File Change List

### New Files

| Path | Purpose |
|---|---|
| `internal/models/memory_item.go` | `MemoryItem` struct |
| `internal/store/dbstore/database_memory.go` | `databaseMemoryItemRecord` struct + all `MemoryItemOperation` implementations |
| `internal/store/dbstore/dbmigrations/0004_memory_items.sql` | Forward migration |
| `internal/store/dbstore/dbmigrations/0004_memory_items.reverse.sql` | Reverse migration |
| `internal/tools/memory/memory.go` | `memoryTool`, `memoryToolConfiguration`, `createTools`, `init`, all `execute*` methods |
| `internal/tools/memory/memory_test.go` | Tool-level tests |

### Modified Files

| Path | Change |
|---|---|
| `internal/store/interfaces.go` | Add `MemoryItemOperation` interface; embed it in `Transaction` |
| `internal/store/types.go` | Add `MemoryItemListOptions`, `MemoryItemSearchOptions`, `MemoryItemSearchResult` |
| `internal/store/fsstore/filesystem_workspace.go` | Add stub implementations of all `MemoryItemOperation` methods returning `store.ErrNotImplemented` |
| `internal/store/errors.go` (or equivalent) | Add `ErrNotImplemented` sentinel if it doesn't already exist |

### NOT Changed in v1

| Path | Reason |
|---|---|
| Runner/systemprompt files | Auto-retrieval into context deferred to v2 |
| Embedding/vector infrastructure | Deferred to v2 |

---

## 6. Sequenced Milestones / PRs

### PR 1: Store Layer + Migration

**Scope:** Data model, store interface, DB migration, dbstore implementation, fsstore stubs.

Files touched:
- `internal/models/memory_item.go` (new)
- `internal/store/interfaces.go` (modify — add `MemoryItemOperation` to `Transaction`)
- `internal/store/types.go` (modify — add option/result types)
- `internal/store/dbstore/database_memory.go` (new)
- `internal/store/dbstore/dbmigrations/0004_memory_items.sql` (new)
- `internal/store/dbstore/dbmigrations/0004_memory_items.reverse.sql` (new)
- `internal/store/fsstore/filesystem_workspace.go` (modify — add stubs)

Implementation details for `database_memory.go`:
- **`databaseMemoryItemRecord`**: gorm-tagged struct mapping to `memory_items` table. `Tags` stored as `*string` (JSON-serialized `[]string`).
- **`CreateMemoryItem`**: generate ID via `security.NewULID()`, set `CreatedAt`/`ModifiedAt` to `time.Now()`, validate required fields (`Scope`, `ScopeID`, `Content`), insert (no upsert — unlike workspace, memory items must not silently overwrite).
- **`GetMemoryItem`**: by ID, `WHERE archived_at IS NULL`. Return `store.ErrNotFound` if missing or archived.
- **`ModifyMemoryItem`**: get → apply modifier → save. Same read-modify-write as `ModifyWorkspaceFileByPath`.
- **`DeleteMemoryItem`**: `UPDATE memory_items SET archived_at = NOW() WHERE id = ?` (soft delete).
- **`ListMemoryItems`**: `WHERE scope = ? AND scope_id = ? AND archived_at IS NULL ORDER BY modified_at DESC`. Apply optional tag filter (`tags @> ?` JSONB containment) and limit.
- **`SearchMemoryItems`**: list candidates in scope, scan `title` + `content` with `bufio.Scanner` line matching (same algorithm as `SearchWorkspaceFiles` in `database_workspace.go:146-196`). Return `MemoryItemSearchResult` with matched lines.
- **`memoryRecordToModel`**: conversion function mirroring `workspaceRecordToModel`.

Tests: store-level contract tests (see Section 7).

### PR 2: Memory Tools

**Scope:** Tool definitions, action handlers, tool registration.

Files touched:
- `internal/tools/memory/memory.go` (new)
- `internal/tools/memory/memory_test.go` (new)

Implementation details for `memory.go`:

```go
type memoryToolConfiguration struct {
    name                        string
    description                 string
    scopeIDParameterName        string
    scopeIDParameterDescription string
    resolveScope                func(ctx context.Context, scopeID string) (models.Scope, string, error)
    afterMutate                 func(ctx context.Context, scopeID string) error
}

type memoryTool struct {
    configuration memoryToolConfiguration
}
```

- `init()` calls `tools.RegisterBuiltinTool(createTools)`.
- `createTools()` returns 3 `memoryTool` instances with scope resolution identical to workspace:
  - `agent_memory`: resolveScope from `runners.RunnerFromContext`, afterMutate updates `Agent.ModifiedAt`.
  - `user_memory`: resolveScope from `models.UserFromContext`, afterMutate updates `User.ModifiedAt`.
  - `project_memory`: `scopeIDParameterName: "projectId"`, afterMutate updates `Project.ModifiedAt`.
- `Definition()` returns `providers.ToolDefinition` with input/output schema from Section 2.
- `Execute()` parses JSON arguments (same dynamic `scopeIDParameterName` extraction as workspace), dispatches on `action` to `executeAdd`, `executeUpdate`, `executeDelete`, `executeGet`, `executeList`, `executeSearch`.
- `callAfterMutate()` called after `add`/`update`/`delete` (same pattern as workspace).

### PR 3 (Optional): Observability Hooks

Add structured event emission after each memory action (Section 8).

### PR 4 (Future): Embeddings + Semantic Retrieval

- Migration `0005_memory_item_embeddings.sql`: add `embedding_model VARCHAR(128) NULL`, `embedding VECTOR(1536) NULL`.
- Extend `SearchMemoryItems` with `mode: "hybrid" | "semantic"`.
- Implement `retrieve` tool action.

### PR 5 (Future): STM Actions

- `summary` and `filter` actions requiring conversation-state extensions.

---

## 7. Test Plan

### 7.1 Tool Tests (`internal/tools/memory/memory_test.go`)

Patterned directly after `internal/tools/workspace/workspace_test.go`.

**Setup helper:**

```go
func setupMemoryStore(t *testing.T) context.Context {
    t.Helper()
    // Use a test Postgres instance or an in-memory gorm DB
    // Return context with store attached via store.ContextWithStore
}
```

**`TestMemoryTools`** (core CRUD via `agent_memory`):
- Setup: test DB + `runners.ContextWithRunner(ctx, runners.NewRunner(ctx, "test-agent", "", nil, models.Agent{}))`.
- `list` on empty scope → `{"action":"list","items":[]}`.
- `add` with `content` + `title` + `tags` → returns `item` with generated `id`.
- `get` by returned `id` → returns matching `item` with correct fields.
- `update` by `id` changing `content` → returns updated `item`.
- `get` after update → reflects new content.
- `delete` by `id` → `{"action":"delete","success":true}`.
- `get` after delete → returns error (item archived).
- `list` after delete → archived item excluded.

**`TestMemorySearchTool`** (search via `agent_memory`):
- Add 2+ items with distinct content.
- `search` for substring → returns match with correct snippet.
- Case-insensitive search works.
- `maxResults:1` → at most 1 result.
- Non-matching query → empty `matches`.

**`TestUserMemoryTool`**:
- Setup with `models.ContextWithUserSessionToken(ctx, &models.User{ID: "user-1"}, nil, nil)`.
- `add` + `get` round-trip via `user_memory`.
- Scope isolation: items added via `user_memory` not visible via `agent_memory`.

**`TestProjectMemoryTool`**:
- `add` via `project_memory` with `projectId`.
- Omitting `projectId` → error.

### 7.2 Store Contract Tests

Add to existing store test infrastructure:

- **`TestCreateMemoryItem`**: creates item, verifies ULID generated, timestamps set, returned model matches.
- **`TestGetMemoryItem`**: existing → returns item; non-existent → `ErrNotFound`.
- **`TestModifyMemoryItem`**: modifies content/tags, verifies `ModifiedAt` updated.
- **`TestDeleteMemoryItem`**: soft-deletes, `ArchivedAt` set; subsequent `Get` → `ErrNotFound`.
- **`TestListMemoryItems`**: multiple items in same scope listed; different scope excluded; archived excluded by default; `IncludeArchived` works; limit works.
- **`TestSearchMemoryItems`**: substring match on title + content; case-insensitive; limit respected.
- **`TestListMemoryItemsTagFilter`**: items with matching tags returned, others excluded.
- **`TestCrossScopeIsolation`**: items in `(agent, "a1")` not visible in `(agent, "a2")` or `(user, "u1")`.

### 7.3 Integration / Smoke

- Migration runs cleanly on fresh Postgres.
- Reverse migration drops the table.
- Tools appear in tool registry after `init()`.

---

## 8. Observability Hooks (AgeMem Reward Approximation)

Emit structured log events after each memory action to approximate AgeMem reward components. These are telemetry for monitoring, not RL signals.

### Events

| Event | Fields | Approximates |
|---|---|---|
| `memory.item.created` | `scope`, `scopeID`, `itemID`, `contentLength`, `tagCount`, `latencyMs` | Knowledge accumulation |
| `memory.item.updated` | `scope`, `scopeID`, `itemID`, `fieldsChanged`, `latencyMs` | Maintenance effort |
| `memory.item.deleted` | `scope`, `scopeID`, `itemID`, `latencyMs` | Maintenance effort |
| `memory.items.listed` | `scope`, `scopeID`, `resultCount`, `latencyMs` | Recall frequency |
| `memory.items.searched` | `scope`, `scopeID`, `query`, `resultCount`, `latencyMs` | Retrieval relevance |

### Implementation

In the tool's `Execute()` method, after the store call completes. Use TeaNode's existing `go-logging` logger:

```go
log.Infof("memory.item.created scope=%s scopeID=%s itemID=%s contentLength=%d tagCount=%d latencyMs=%d",
    scope, scopeID, item.ID, len(*item.Content), tagCount, time.Since(start).Milliseconds())
```

### Derived Metrics (offline analysis)

- **Memory utilization**: items per scope, total content size.
- **Maintenance ratio**: (updates + deletes) / adds — AgeMem's "maintenance" signal.
- **Retrieval hit rate**: searches returning >0 results / total searches.
- **Context efficiency**: retrieved content size vs. conversation length (requires conversation telemetry, v2).

---

## Appendix: Key Design Decisions

| Decision | Choice | Rationale |
|---|---|---|
| ID generation | ULID via `security.NewULID()` | Matches all other entities |
| Delete semantics | Soft-delete (`archived_at`) | Enables undo, audit, maintenance metrics |
| Create semantics | No upsert | Memory items are ID-keyed; silent overwrite would be surprising |
| Tags storage | JSONB column | Simple, queryable, avoids join table for v1 |
| fsstore | Deferred | ID-keyed data; DB is natural fit |
| STM actions | Deferred to v2 | Require conversation-state extensions |
| Embeddings | Deferred to v2 | Schema can add nullable column later |
| Naming | `MemoryItem`, `MemoryItemOperation`, `memory_items` | TeaNode conventions: PascalCase Go types, snake_case DB |
| Content size | 64 KB max per item (enforced in tool layer) | Generous for text, prevents abuse |
