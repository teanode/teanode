# Memory store schema aligned with `workspace_files` (TeaNode plan)

This document specifies a **memory store** for TeaNode that aligns closely with how **workspace files** are implemented today.

Reference implementation patterns:

- Data model: `internal/models/workspace_file.go`
- Store interface: `internal/store/interfaces.go` (`WorkspaceFileOperation`)
- DB schema: `internal/store/dbstore/dbmigrations/0000_initial.sql` (`workspace_files` table + indexes)
- DB implementation: `internal/store/dbstore/database_workspace.go`
- FS implementation: `internal/store/fsstore/filesystem_workspace.go`

## Design goals

1. **Same scoping model** as workspace: `(scope, scope_id)` where scope ∈ {agent,user,project}.
2. **Same operational shape** as workspace: create/get/modify/delete/list/search.
3. Avoid overfitting to embeddings/RAG initially; keep retrieval **keyword-first**, but make room for semantic later.
4. Provide stable IDs for `update`/`delete` operations (AgeMem-style maintenance).

## Proposed model: `MemoryItem`

Create `internal/models/memory_item.go` modeled after `WorkspaceFile`.

```go
package models

import "time"

type MemoryItem struct {
    ID         string     `json:"id,omitempty" yaml:"id,omitempty"`
    CreatedAt  *time.Time `json:"createdAt,omitempty" yaml:"createdAt,omitempty"`
    ModifiedAt *time.Time `json:"modifiedAt,omitempty" yaml:"modifiedAt,omitempty"`

    Scope   *Scope  `json:"scope,omitempty" yaml:"scope,omitempty"`
    ScopeID *string `json:"scopeId,omitempty" yaml:"scopeId,omitempty"`

    Title   *string  `json:"title,omitempty" yaml:"title,omitempty"`
    Content *string  `json:"content,omitempty" yaml:"content,omitempty"`
    Tags    *[]string `json:"tags,omitempty" yaml:"tags,omitempty"`

    // Similar to workspace file `ContentType`, but for memory semantics.
    ItemType *string `json:"itemType,omitempty" yaml:"itemType,omitempty"` // e.g. note/fact/recipe/api

    // Optional origin tracking (not required for v1).
    SourceType *string `json:"sourceType,omitempty" yaml:"sourceType,omitempty"` // conversation/tool/workspace/manual
    SourceRef  *string `json:"sourceRef,omitempty" yaml:"sourceRef,omitempty"`

    ArchivedAt *time.Time `json:"archivedAt,omitempty" yaml:"archivedAt,omitempty"`
}
```

Notes:
- `Content` uses `string` for text-based memory items, stored as TEXT in Postgres.
- Tags are stored as JSONB in Postgres for simplicity (matches other JSONB usage).
- `ArchivedAt` supports “maintenance” without hard-delete.

## Store interface: `MemoryItemOperation`

Extend `internal/store/interfaces.go` with a new operation block and add it to `Transaction`.

```go
type Transaction interface {
    ...
    WorkspaceFileOperation
    MemoryItemOperation
    ...
}

type MemoryItemOperation interface {
    CreateMemoryItem(ctx context.Context, item *models.MemoryItem, options *Option) (*models.MemoryItem, error)
    GetMemoryItem(ctx context.Context, memoryItemId string, options *Option) (*models.MemoryItem, error)

    // Modify-by-ID supports UPDATE semantics cleanly.
    ModifyMemoryItem(ctx context.Context, memoryItemId string, modifier func(*models.MemoryItem) error, options *Option) (*models.MemoryItem, error)

    // Delete-by-ID: prefer archive semantics internally, but expose as delete.
    DeleteMemoryItem(ctx context.Context, memoryItemId string, options *Option) error

    // List in a scope (like ListWorkspaceFilesByPath).
    ListMemoryItems(ctx context.Context, scope models.Scope, scopeId string, listOptions MemoryItemListOptions, options *Option) ([]*models.MemoryItem, error)

    // Search within a scope (like SearchWorkspaceFiles).
    SearchMemoryItems(ctx context.Context, scope models.Scope, scopeId string, query string, searchOptions MemoryItemSearchOptions, options *Option) ([]MemoryItemSearchResult, error)
}
```

Define options structs in `internal/store/` mirroring workspace search patterns:

```go
type MemoryItemListOptions struct {
    // Optional filters (all optional for v1)
    Tags *[]string
    ItemType *string
    IncludeArchived *bool
    Limit *uint64
}

type MemoryItemSearchOptions struct {
    Limit *uint64
    IncludeContent *bool // mirror WorkspaceSearchOptions.IncludeContent
    CaseSensitive *bool  // optional
    IncludeArchived *bool
}

type MemoryItemSearchResult struct {
    MemoryItemID *string
    Scope *models.Scope
    ScopeID *string

    Title *string
    ItemType *string
    Tags *[]string

    // Like workspace search: return matched lines/snippets.
    MatchedLines *[]string

    // Optional for future ranking.
    Score *float64
}
```

### Why not `GetMemoryItemByKey` or path-based addressing?
Workspace uses `(scope, scope_id, path)` as a stable key.

For memory, AgeMem-style `update/delete` wants a stable handle. A path-like key is brittle unless you invent one.

So:
- workspace: keyed by path
- memory: keyed by ID, but still *scoped* for listing/search

## DB schema: `memory_items` table (Postgres)

Add a migration similar to the `workspace_files` block in `0000_initial.sql`.

```sql
CREATE TABLE IF NOT EXISTS memory_items (
    id VARCHAR(32) PRIMARY KEY,
    scope VARCHAR(32) NOT NULL,
    scope_id VARCHAR(32) NOT NULL,

    title TEXT NULL,
    content TEXT NOT NULL DEFAULT '',

    tags JSONB NULL,
    item_type VARCHAR(64) NULL,

    source_type VARCHAR(32) NULL,
    source_ref TEXT NULL,

    archived_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    modified_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS memory_items_scope_scope_id_index ON memory_items (scope, scope_id);
CREATE INDEX IF NOT EXISTS memory_items_scope_scope_id_modified_at_index ON memory_items (scope, scope_id, modified_at DESC);

-- Optional: tag search index later (GIN over tags JSONB)
-- CREATE INDEX ... ON memory_items USING GIN (tags);
```

Alignment points with `workspace_files`:
- same `(scope, scope_id)` columns + index
- same `TEXT content` + created/modified timestamps

## DB implementation: `internal/store/dbstore/database_memory.go`

Mirror `database_workspace.go` structure:

- `databaseMemoryItemRecord` struct with gorm tags
- `CreateMemoryItem` uses ULID generation (`security.NewULID()`), sets timestamps, upsert semantics if desired
  - **Unlike workspace**, do *not* upsert by `(scope,scope_id,title)`; memory should not silently overwrite.
- `GetMemoryItem` by ID
- `ModifyMemoryItem` reads by ID, applies modifier, then saves
- `DeleteMemoryItem`
  - recommended v1: set `archived_at = now()` instead of hard delete
- `ListMemoryItems` filters by scope/scopeId, optional archived, ordering by modified_at desc
- `SearchMemoryItems`
  - v1: fetch candidates in scope, scan `title` and/or `content` similar to `SearchWorkspaceFiles`
  - output matched lines/snippets

## FS store implementation (optional v1)

TeaNode has both dbstore and fsstore for workspace.

If you want close alignment, implement a filesystem-backed memory store too:

- Directory layout mirroring workspace scoping:
  - `~/.teanode/agents/<agentId>/memory_items/<id>.json`
  - `~/.teanode/users/<userId>/memory_items/<id>.json`
  - `~/.teanode/projects/<projectId>/memory_items/<id>.json`

But it’s reasonable to start with dbstore only if fsstore isn’t required for your deployments.

## Tool-layer alignment

Once the store exists, implement tools as in `internal/tools/workspace/workspace.go`:

- `agent_memory`: resolve `(scope=agent, scopeId=runner.AgentID)`
- `user_memory`: resolve `(scope=user, scopeId=user.ID)`
- `project_memory`: resolve `(scope=project, scopeId=projectId)`

And keep an `afterMutate` hook identical to workspace:
- updating `Agent.ModifiedAt` / `User.ModifiedAt` / `Project.ModifiedAt`

## Forward-compat: embeddings

To align with AgeMem’s “semantic relevance” reward, we likely want embeddings later.

DB-forward-compatible approach:

- Add nullable columns:
  - `embedding_model VARCHAR(128) NULL`
  - `embedding VECTOR(<dim>) NULL` (pgvector)

Or use a side table `memory_item_embeddings(memory_item_id, model, vector)`.

Do **not** block v1 on this.

