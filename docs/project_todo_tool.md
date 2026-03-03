# Todo Tools — Design Document

## Overview

Two TeaNode tools for structured task tracking at different scopes:

- **`project_todo`** — todos scoped to a project. Shared across all conversations within that project.
- **`conversation_todo`** — todos scoped to a single conversation (agentId + conversationId). Private to the conversation that created them.

Both tools are persisted as first-class entities in the TeaNode store layer (dbstore/fsstore), with dedicated tables/files and proper CRUD interfaces — not as workspace files.

---

## Tool Definitions

### `project_todo`

- **Name:** `project_todo`
- **Package:** `internal/tools/projects/` (alongside existing `tools.go` and `projects.go`)
- **Registration:** `init()` → `tools.RegisterBuiltinTool(...)`, returning a `*projectTodoTool`

### `conversation_todo`

- **Name:** `conversation_todo`
- **Package:** `internal/tools/conversations/` (new package)
- **Registration:** `init()` → `tools.RegisterBuiltinTool(...)`, returning a `*conversationTodoTool`

### Actions (shared by both tools)

| Action     | Required params                   | Optional params                              | Description                            |
|------------|-----------------------------------|----------------------------------------------|----------------------------------------|
| `list`     | scope id*                         | `status`, `priority`, `tag`                  | List todos, optionally filtered        |
| `add`      | scope id*, `title`                | `description`, `priority`, `tags`            | Create a new todo                      |
| `update`   | scope id*, `todoId`               | `title`, `description`, `priority`, `tags`   | Update fields on an existing todo      |
| `complete` | scope id*, `todoId`               |                                              | Mark a todo as done                    |
| `reopen`   | scope id*, `todoId`               |                                              | Mark a done todo back to open          |
| `delete`   | scope id*, `todoId`               |                                              | Permanently remove a todo              |

\* For `project_todo`: `projectId` (or `projectName`). For `conversation_todo`: `conversationId`.

### Parameters — `project_todo`

```json
{
  "type": "object",
  "properties": {
    "action":      { "type": "string", "enum": ["list", "add", "update", "complete", "reopen", "delete"] },
    "projectId":   { "type": "string", "description": "Project ID (required for all actions)." },
    "projectName": { "type": "string", "description": "Project name — resolved to projectId if projectId is omitted." },
    "todoId":      { "type": "string", "description": "Todo ID (for update, complete, reopen, delete)." },
    "title":       { "type": "string", "description": "Todo title (for add, update)." },
    "description": { "type": "string", "description": "Optional longer description (for add, update)." },
    "priority":    { "type": "string", "enum": ["low", "medium", "high"], "description": "Priority level (for add, update, list filter)." },
    "tags":        { "type": "array", "items": { "type": "string" }, "description": "Labels/tags (for add, update)." },
    "tag":         { "type": "string", "description": "Filter by tag (for list)." },
    "status":      { "type": "string", "enum": ["open", "done"], "description": "Filter by status (for list). Default: returns all." }
  },
  "required": ["action"]
}
```

**Note:** Either `projectId` or `projectName` must be provided. If only `projectName` is given, the tool resolves it to a project ID by listing projects and matching by name (case-insensitive). If both are given, `projectId` takes precedence.

### Parameters — `conversation_todo`

```json
{
  "type": "object",
  "properties": {
    "action":         { "type": "string", "enum": ["list", "add", "update", "complete", "reopen", "delete"] },
    "conversationId": { "type": "string", "description": "Conversation ID (required for all actions)." },
    "todoId":         { "type": "string", "description": "Todo ID (for update, complete, reopen, delete)." },
    "title":          { "type": "string", "description": "Todo title (for add, update)." },
    "description":    { "type": "string", "description": "Optional longer description (for add, update)." },
    "priority":       { "type": "string", "enum": ["low", "medium", "high"], "description": "Priority level (for add, update, list filter)." },
    "tags":           { "type": "array", "items": { "type": "string" }, "description": "Labels/tags (for add, update)." },
    "tag":            { "type": "string", "description": "Filter by tag (for list)." },
    "status":         { "type": "string", "enum": ["open", "done"], "description": "Filter by status (for list). Default: returns all." }
  },
  "required": ["action"]
}
```

**Note:** `conversationId` is required for all actions. The tool may also infer it from runner context if available, but explicit is preferred.

---

## Response Format

Both tools return JSON matching existing tool conventions. The response shape is identical — only the scope identifier differs.

### `list` response

```json
{
  "action": "list",
  "todos": [
    {
      "id": "01JARQ...",
      "projectId": "01JBCDE...",
      "title": "Implement auth",
      "description": "Add JWT-based authentication",
      "status": "open",
      "priority": "high",
      "tags": ["backend", "security"],
      "createdAt": "2026-02-28T10:00:00Z",
      "updatedAt": "2026-02-28T10:00:00Z"
    }
  ],
  "totalCount": 5,
  "openCount": 3,
  "doneCount": 2
}
```

For `conversation_todo`, the todo object contains `conversationId` instead of `projectId`.

### Mutation responses

**`add` / `update` / `complete` / `reopen`:**

```json
{
  "action": "add",
  "todo": {
    "id": "01JARQ...",
    "projectId": "01JBCDE...",
    "title": "Implement auth",
    "status": "open",
    "priority": "high",
    "tags": ["backend"],
    "createdAt": "2026-02-28T10:00:00Z",
    "updatedAt": "2026-02-28T10:00:00Z"
  }
}
```

**`delete`:**

```json
{
  "action": "delete",
  "success": true
}
```

---

## Data Models

### `internal/models/todo.go`

```go
package models

import "time"

// Todo represents a task item scoped to either a project or a conversation.
type Todo struct {
    ID             string     `json:"id,omitempty"             yaml:"id,omitempty"`
    ProjectID      *string    `json:"projectId,omitempty"      yaml:"projectId,omitempty"`
    ConversationID *string    `json:"conversationId,omitempty" yaml:"conversationId,omitempty"`
    Title          *string    `json:"title,omitempty"          yaml:"title,omitempty"`
    Description    *string    `json:"description,omitempty"    yaml:"description,omitempty"`
    Status         *string    `json:"status,omitempty"         yaml:"status,omitempty"`
    Priority       *string    `json:"priority,omitempty"       yaml:"priority,omitempty"`
    Tags           *[]string  `json:"tags,omitempty"           yaml:"tags,omitempty"`
    CompletedAt    *time.Time `json:"completedAt,omitempty"    yaml:"completedAt,omitempty"`
    CreatedAt      *time.Time `json:"createdAt,omitempty"      yaml:"createdAt,omitempty"`
    ModifiedAt     *time.Time `json:"modifiedAt,omitempty"     yaml:"modifiedAt,omitempty"`
}
```

**Design notes:**

- A single `Todo` model serves both scopes. Exactly one of `ProjectID` or `ConversationID` is non-nil.
- Follows the existing model conventions: pointer fields, `omitempty`, dual `json`/`yaml` tags.
- `Status`: `"open"` | `"done"`. Default `"open"` on creation.
- `Priority`: `"low"` | `"medium"` | `"high"`. Default `"medium"` if omitted.
- `Tags`: empty slice default, stored as JSON array (dbstore) or YAML sequence (fsstore).
- `ID`: generated via `security.NewULID()`.

---

## Store Interface

### `internal/store/interfaces.go`

Add a new `TodoOperation` interface and compose it into `Transaction`:

```go
type TodoListOptions struct {
    ProjectID      *string
    ConversationID *string
}

type TodoOperation interface {
    ListTodos(ctx context.Context, listOptions TodoListOptions, options *Option) ([]*models.Todo, error)
    CreateTodo(ctx context.Context, todo *models.Todo, options *Option) (*models.Todo, error)
    GetTodo(ctx context.Context, todoId string, options *Option) (*models.Todo, error)
    ModifyTodo(ctx context.Context, todoId string, modifier func(*models.Todo) error, options *Option) (*models.Todo, error)
    DeleteTodo(ctx context.Context, todoId string, options *Option) error
}

type Transaction interface {
    // ... existing operations ...
    TodoOperation
}
```

**Why a unified interface (not separate ProjectTodoOperation / ConversationTodoOperation)?**

- The underlying data model is identical — only the scope FK differs.
- `TodoListOptions` distinguishes the scope at query time (set `ProjectID` or `ConversationID`).
- A single table/file set avoids duplicated code in both store backends.
- The tools enforce scope correctness at the tool layer — the store simply persists and queries.

---

## Database Store (dbstore)

### Migration: `0001_todos.sql`

```sql
CREATE TABLE IF NOT EXISTS todos (
    id VARCHAR(32) PRIMARY KEY,
    project_id VARCHAR(32) NULL,
    conversation_id VARCHAR(32) NULL,
    title VARCHAR(512) NOT NULL,
    description TEXT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'open',
    priority VARCHAR(16) NOT NULL DEFAULT 'medium',
    tags JSONB NOT NULL DEFAULT '[]'::jsonb,
    completed_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    modified_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT todos_project_id_fkey FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
    CONSTRAINT todos_conversation_id_fkey FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE,
    CONSTRAINT todos_scope_check CHECK (
        (project_id IS NOT NULL AND conversation_id IS NULL)
        OR (project_id IS NULL AND conversation_id IS NOT NULL)
    )
);

-- List todos by project
CREATE INDEX IF NOT EXISTS todos_project_id_index ON todos (project_id) WHERE project_id IS NOT NULL;

-- List todos by conversation
CREATE INDEX IF NOT EXISTS todos_conversation_id_index ON todos (conversation_id) WHERE conversation_id IS NOT NULL;

-- Filter by status within a scope
CREATE INDEX IF NOT EXISTS todos_project_id_status_index ON todos (project_id, status) WHERE project_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS todos_conversation_id_status_index ON todos (conversation_id, status) WHERE conversation_id IS NOT NULL;
```

### Reverse migration: `0001_todos.reverse.sql`

```sql
DROP TABLE IF EXISTS todos;
```

### Implementation: `internal/store/dbstore/database_todo.go`

```go
type databaseTodoRecord struct {
    ID             string     `gorm:"column:id;type:varchar(32);primaryKey"`
    ProjectID      *string    `gorm:"column:project_id;type:varchar(32)"`
    ConversationID *string    `gorm:"column:conversation_id;type:varchar(32)"`
    Title          string     `gorm:"column:title;type:varchar(512);not null"`
    Description    *string    `gorm:"column:description;type:text"`
    Status         string     `gorm:"column:status;type:varchar(16);not null"`
    Priority       string     `gorm:"column:priority;type:varchar(16);not null"`
    Tags           []byte     `gorm:"column:tags;type:jsonb;not null"`
    CompletedAt    *time.Time `gorm:"column:completed_at"`
    CreatedAt      time.Time  `gorm:"column:created_at;not null"`
    ModifiedAt     time.Time  `gorm:"column:modified_at;not null"`
}
```

**Query patterns:**

- `ListTodos` with `ProjectID` set → `WHERE project_id = ?`, ordered by `priority DESC, created_at DESC`.
- `ListTodos` with `ConversationID` set → `WHERE conversation_id = ?`, same ordering.
- `ListTodos` with neither set → error (`store.ErrInvalidOptions`).
- `GetTodo` → `WHERE id = ?`.
- `ModifyTodo` → `SELECT ... FOR UPDATE WHERE id = ?`, apply modifier, `UPDATE`.
- `DeleteTodo` → `DELETE WHERE id = ?`.
- `CreateTodo` → `INSERT` with `NOW()` timestamps.

**Cascade deletes:** When a project or conversation is deleted, all associated todos are automatically removed via `ON DELETE CASCADE`.

---

## Filesystem Store (fsstore)

### Layout

```
dataDirectory/
├── projects/
│   └── {projectId}/
│       ├── project.yaml
│       ├── workspace/
│       └── todos/
│           └── {todoId}.yaml          # one file per todo
├── users/
│   └── {userId}/
│       └── conversations/
│           └── {agentId}/
│               ├── {conversationId}.jsonl
│               └── {conversationId}.todos/
│                   └── {todoId}.yaml  # one file per todo
```

**Why one-file-per-todo (not a single todos.yaml)?**

- Avoids read-modify-write on a monolithic file for every mutation.
- File-per-record matches the fsstore pattern used for agents, users, projects, jobs (each entity is its own YAML file).
- Deletion is a simple `os.Remove`, no file rewriting.
- The fsstore global mutex already serializes transactions, so no additional concurrency concerns.

### Todo YAML format

```yaml
id: "01JARQ..."
projectId: "01JBCDE..."
title: "Implement auth"
description: "Add JWT-based authentication"
status: "open"
priority: "high"
tags:
  - backend
  - security
createdAt: 2026-02-28T10:00:00Z
modifiedAt: 2026-02-28T10:00:00Z
```

### Implementation: `internal/store/fsstore/filesystem_todo.go`

**Path helpers** (added to `filesystem_paths.go`):

```go
func projectTodosDirectory(projectId string) string
    // returns "projects/{projectId}/todos"

func projectTodoFilePath(projectId, todoId string) string
    // returns "projects/{projectId}/todos/{todoId}.yaml"

func conversationTodosDirectory(userId, agentId, conversationId string) string
    // returns "users/{userId}/conversations/{agentId}/{conversationId}.todos"

func conversationTodoFilePath(userId, agentId, conversationId, todoId string) string
    // returns "users/{userId}/conversations/{agentId}/{conversationId}.todos/{todoId}.yaml"
```

**ListTodos:**

- For project scope: read all `.yaml` files in `projects/{projectId}/todos/`.
- For conversation scope: resolve the conversation to find `userId` and `agentId`, then read all `.yaml` files in the conversation todos directory.
- Unmarshal each, collect into slice, sort by priority then createdAt.

**Conversation resolution note:** The conversation todos directory path requires `userId` and `agentId` (since fsstore organizes conversations under `users/{userId}/conversations/{agentId}/`). When `ListTodos` or `CreateTodo` is called with a `ConversationID`, the fsstore implementation must first `GetConversation` to resolve these parent IDs. This is an internal detail — the store interface only exposes `ConversationID`.

**Cascade deletes:** When `DeleteProject` or `DeleteConversation` is called, the fsstore already moves the parent directory to `.trash/`. This automatically captures the `todos/` subdirectory, so no special cascade logic is needed.

---

## Indexing and Query Patterns

### Primary access patterns

| Query | dbstore | fsstore |
|-------|---------|---------|
| List project todos | `WHERE project_id = ?` (indexed) | Read `projects/{pid}/todos/*.yaml` |
| List conversation todos | `WHERE conversation_id = ?` (indexed) | Read `.../{cid}.todos/*.yaml` |
| Get single todo | `WHERE id = ?` (PK) | Direct file read by path |
| Filter by status | `AND status = ?` (composite index) | In-memory filter after read |
| Filter by priority | `AND priority = ?` | In-memory filter after read |
| Filter by tag | `AND tags @> '["tag"]'::jsonb` (GIN possible) | In-memory filter after read |

### Not supported (by design)

- Cross-project todo search: no use case. Agents work within a single project context.
- Cross-conversation todo search: no use case. Each conversation's todos are independent.
- Full-text search on title/description: deferred. Can be added via `SearchTodos` later if needed.

### GIN index on tags (optional, deferred)

If tag-based filtering becomes performance-critical:

```sql
CREATE INDEX IF NOT EXISTS todos_tags_gin_index ON todos USING GIN (tags);
```

Not included in the initial migration — JSONB containment queries are fast enough for the expected data volumes (tens to low hundreds of todos per scope).

---

## Permissions and Multi-User Implications

### `project_todo` permissions

Projects are shared resources — all users can see all projects. Todo permissions follow the same pattern:

| Role | list | add | update | complete | reopen | delete |
|------|------|-----|--------|----------|--------|--------|
| Admin | yes | yes | yes | yes | yes | yes |
| Non-admin | yes | no | no | no | no | no |

This matches the existing `project_workspace` permission model. Non-admin users can read project todos but cannot mutate them.

### `conversation_todo` permissions

Conversations are user-scoped — a conversation belongs to a specific user. Todo permissions leverage this:

| Check | Rule |
|-------|------|
| Ownership | The requesting user must own the conversation (`conversation.UserID == user.ID`), OR be an admin. |
| list | Allowed if ownership check passes. |
| add/update/complete/reopen/delete | Allowed if ownership check passes. |

This means:
- A user can freely manage todos in their own conversations.
- An admin can manage todos in any conversation.
- A user cannot see or modify todos in another user's conversation.

### Authorization implementation

**Layer 1: Runner-level (`validateToolAuthorization` in `runner.go`)**

```go
case "project_todo":
    action := parseToolAction(arguments)
    if action != "list" {
        return fmt.Errorf("admin access required for project_todo.%s", action)
    }

case "conversation_todo":
    // All actions allowed at runner level — ownership check happens in tool.
    // (Runner doesn't have conversationId context to check ownership.)
```

**Layer 2: Tool-level (defense-in-depth)**

`project_todo`:
```go
case "add", "update", "complete", "reopen", "delete":
    user := models.UserFromContext(ctx)
    if user == nil || !user.GetAdmin() {
        return "", fmt.Errorf("admin access required to %s project todos", action)
    }
```

`conversation_todo`:
```go
// All actions: verify ownership
conversation, err := tx.GetConversation(ctx, conversationId, nil)
user := models.UserFromContext(ctx)
if user == nil {
    return "", fmt.Errorf("authentication required")
}
if valueor.Value(conversation.UserID) != user.ID && !valueor.Value(user.Admin) {
    return "", fmt.Errorf("access denied: conversation belongs to another user")
}
```

### After-mutation callbacks

**`project_todo` mutations** bump `Project.ModifiedAt`:

```go
func afterMutateProject(ctx context.Context, projectId string) {
    _ = store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
        _, err := tx.ModifyProject(ctx, projectId, func(p *models.Project) error {
            now := time.Now()
            p.ModifiedAt = &now
            return nil
        }, nil)
        return err
    })
}
```

**`conversation_todo` mutations** bump `Conversation.ModifiedAt`:

```go
func afterMutateConversation(ctx context.Context, conversationId string) {
    _ = store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
        _, err := tx.ModifyConversation(ctx, conversationId, func(c *models.Conversation) error {
            now := time.Now()
            c.ModifiedAt = &now
            return nil
        }, nil)
        return err
    })
}
```

---

## Project Resolution by Name

When `projectName` is provided without `projectId` (for `project_todo` only):

```go
func resolveProjectId(ctx context.Context, projectId, projectName string) (string, error) {
    if projectId != "" {
        return projectId, nil
    }
    if projectName == "" {
        return "", fmt.Errorf("projectId or projectName is required")
    }
    projects, err := listProjects(ctx)
    if err != nil {
        return "", err
    }
    lowerName := strings.ToLower(projectName)
    for _, p := range projects {
        if p.Name != nil && strings.ToLower(*p.Name) == lowerName {
            return p.ID, nil
        }
    }
    return "", fmt.Errorf("project not found: %s", projectName)
}
```

This reuses the existing `listProjects()` helper in the `projects` package.

---

## Implementation Outline

### New files

| File | Contents |
|------|----------|
| `internal/models/todo.go` | `Todo` struct |
| `internal/store/interfaces.go` | `TodoOperation` interface, `TodoListOptions` struct (added to existing file) |
| `internal/store/dbstore/database_todo.go` | dbstore implementation of `TodoOperation` |
| `internal/store/dbstore/dbmigrations/0001_todos.sql` | Forward migration |
| `internal/store/dbstore/dbmigrations/0001_todos.reverse.sql` | Reverse migration |
| `internal/store/fsstore/filesystem_todo.go` | fsstore implementation of `TodoOperation` |
| `internal/tools/projects/todo.go` | `project_todo` tool |
| `internal/tools/conversations/todo.go` | `conversation_todo` tool |
| `internal/tools/conversations/conversations.go` | Helper functions (conversation lookup) |

### Modified files

| File | Change |
|------|--------|
| `internal/store/interfaces.go` | Add `TodoOperation` to `Transaction` interface |
| `internal/store/fsstore/filesystem_paths.go` | Add todo path helpers |
| `internal/runners/runner.go` | Add `project_todo` and `conversation_todo` to `validateToolAuthorization` |

### `internal/tools/projects/todo.go`

```
package projects

// init() — register via tools.RegisterBuiltinTool
// projectTodoTool struct
// Definition() — returns tool definition with JSON schema
// Execute() — parse args, resolve project, check auth, dispatch action
// executeTodoList() — query store, filter, return
// executeTodoAdd() — create via store, afterMutateProject
// executeTodoUpdate() — modify via store, afterMutateProject
// executeTodoComplete() — modify status=done+completedAt, afterMutateProject
// executeTodoReopen() — modify status=open+clear completedAt, afterMutateProject
// executeTodoDelete() — delete via store, afterMutateProject
```

### `internal/tools/conversations/todo.go`

```
package conversations

// init() — register via tools.RegisterBuiltinTool
// conversationTodoTool struct
// Definition() — returns tool definition with JSON schema
// Execute() — parse args, verify ownership, dispatch action
// executeTodoList() — query store, filter, return
// executeTodoAdd() — create via store, afterMutateConversation
// executeTodoUpdate() — modify via store, afterMutateConversation
// executeTodoComplete() — modify status=done+completedAt, afterMutateConversation
// executeTodoReopen() — modify status=open+clear completedAt, afterMutateConversation
// executeTodoDelete() — delete via store, afterMutateConversation
```

---

## Web UI Suggestions

### Project Todos

- Add a "Todos" tab or collapsible section in the project detail view (alongside existing workspace file browser).
- Render todos as a checklist grouped by status (open first, done below).
- Show priority as colored indicators (high=red, medium=yellow, low=gray).
- Tags rendered as small chips/badges.
- Sort: by priority (high→low), then by `createdAt` (newest first).
- Checkbox toggle to complete/reopen. Inline editing for title/description. Delete with confirmation.

### Conversation Todos

See the dedicated [Conversation Todos — Frontend Design](#conversation-todos--frontend-design) section below.

### REST endpoints (deferred)

For initial implementation, todos are accessed through the agent tools only. Dedicated REST endpoints can be added later:

```
GET    /api/v1/projects/:projectId/todos
POST   /api/v1/projects/:projectId/todos
GET    /api/v1/conversations/:conversationId/todos
POST   /api/v1/conversations/:conversationId/todos
PATCH  /api/v1/todos/:todoId
DELETE /api/v1/todos/:todoId
```

### i18n

Add keys to `web/src/i18n/locales/en.json`:

```json
{
  "project.todos.title": "Todos",
  "project.todos.add": "Add Todo",
  "project.todos.empty": "No todos yet",
  "project.todos.status.open": "Open",
  "project.todos.status.done": "Done",
  "project.todos.priority.low": "Low",
  "project.todos.priority.medium": "Medium",
  "project.todos.priority.high": "High",
  "conversation.todos.title": "Todos",
  "conversation.todos.add": "Add Todo",
  "conversation.todos.empty": "No todos yet"
}
```

---

## Conversation Todos — Frontend Design

### Overview

The conversation todo list is displayed inline within the conversation view as a collapsible panel. During an active run, the UI updates in near-real-time as the agent adds, updates, and completes items via the `conversation_todo` tool. Users can also manually toggle items or add their own todos.

---

### Data Flow: Backend → Frontend

#### Event-driven updates (primary path)

Todo mutations — whether from the agent's `conversation_todo` tool or future user REST actions — emit a pubsub event that reaches all connected WebSocket clients.

**New pubsub event type:**

```go
// internal/pubsub/pubsub.go
const EventTypeConversationTodos EventType = "conversation_todos"
```

**Emitted after every `conversation_todo` mutation** (add, update, complete, reopen, delete):

```go
// internal/tools/conversations/todo.go — after successful mutation
func emitTodoEvent(ctx context.Context, pubsub *pubsub.PubSub, conversationId, userId string, todo *models.Todo, action string) {
    pubsub.Broadcast(pubsub.EventTypeConversationTodos, map[string]interface{}{
        "conversationId": conversationId,
        "userId":         userId,
        "action":         action,     // "add" | "update" | "complete" | "reopen" | "delete"
        "todo":           todo,       // full todo object (nil for delete)
        "todoId":         todo.ID,    // always present, even for delete
    })
}
```

The tool accesses pubsub through the coordinator, which is available via the runner context (same pattern as `afterMutateConversation`).

**Wire format (EventFrame):**

```json
{
  "type": "event",
  "event": "conversation_todos",
  "payload": {
    "conversationId": "01JXYZ...",
    "userId": "01JABC...",
    "action": "complete",
    "todoId": "01JARQ...",
    "todo": {
      "id": "01JARQ...",
      "conversationId": "01JXYZ...",
      "title": "Implement auth",
      "status": "done",
      "priority": "high",
      "tags": ["backend"],
      "completedAt": "2026-02-28T12:00:00Z",
      "createdAt": "2026-02-28T10:00:00Z",
      "modifiedAt": "2026-02-28T12:00:00Z"
    }
  }
}
```

**Why a separate event type (not a new `ConversationEventState`)?**

- `ConversationEventState` values (`delta`, `tool_call`, `final`, etc.) are scoped to a run and carry a `runId`. Todo updates are conversation-level state changes that can also originate outside of runs (user manual edits via REST).
- A separate `EventType` avoids overloading `handleEvent`'s run-scoped logic and allows independent event routing.
- Follows the pattern of `EventTypeConversations` (conversation list changed) — a parallel for conversation-scoped sub-resources.

#### RPC method for initial fetch

**New RPC method: `conversations.todos.list`**

```typescript
// Request
{ method: "conversations.todos.list", params: { conversationId: string } }

// Response
{
  todos: Todo[],
  openCount: number,
  doneCount: number
}
```

Handled in `websocket.go`'s RPC dispatch, delegates to `store.ListTodos` with `ConversationID` filter. Returns todos sorted by priority (high→low), then `createdAt` (newest first).

#### RPC methods for user manual edits

Users can manage todos directly from the UI without going through the agent:

| Method | Params | Description |
|--------|--------|-------------|
| `conversations.todos.add` | `{ conversationId, title, priority?, tags? }` | Create a new todo |
| `conversations.todos.complete` | `{ conversationId, todoId }` | Mark done |
| `conversations.todos.reopen` | `{ conversationId, todoId }` | Mark open |
| `conversations.todos.update` | `{ conversationId, todoId, title?, priority?, tags? }` | Edit fields |
| `conversations.todos.delete` | `{ conversationId, todoId }` | Remove |

All RPC handlers emit the same `EventTypeConversationTodos` pubsub event after mutation, so all connected clients stay in sync.

#### Sequence diagram: agent completes a todo during a run

```
Agent (LLM)         Runner/Coordinator        Store         PubSub         Frontend
    |                      |                     |              |               |
    |-- tool_call -------->|                     |              |               |
    |   conversation_todo  |                     |              |               |
    |   action: complete   |                     |              |               |
    |                      |-- ModifyTodo ------>|              |               |
    |                      |<--- updated todo ---|              |               |
    |                      |                     |              |               |
    |                      |-- Broadcast --------|---> event -->|               |
    |                      |   conversation_todos|              |-- handleEvent |
    |                      |                     |              |   update todo |
    |                      |                     |              |   state in UI |
    |                      |                     |              |               |
    |                      |-- Broadcast (tool_call event) --->|               |
    |                      |                     |              |-- show tool   |
    |                      |                     |              |   invocation  |
    |<-- tool result ------|                     |              |               |
    |                      |-- Broadcast (tool_result event) ->|               |
    |                      |                     |              |-- show tool   |
    |                      |                     |              |   result      |
```

The `conversation_todos` event arrives **before** the `tool_result` event, so the todo panel updates immediately as the agent works — the user does not have to wait for the tool result to render.

---

### TypeScript Types

```typescript
// web/src/types.ts

export interface Todo {
  id: string;
  conversationId?: string;
  projectId?: string;
  title: string;
  description?: string;
  status: "open" | "done";
  priority: "low" | "medium" | "high";
  tags: string[];
  completedAt?: string;   // ISO 8601
  createdAt: string;
  modifiedAt: string;
}

export interface ConversationTodosEvent {
  conversationId: string;
  userId: string;
  action: "add" | "update" | "complete" | "reopen" | "delete";
  todoId: string;
  todo?: Todo;            // present for all actions except delete
}

export interface ConversationTodosListResult {
  todos: Todo[];
  openCount: number;
  doneCount: number;
}
```

---

### State Management: `useBackend.ts`

Todo state is managed alongside existing conversation state in `useBackend`. It follows the same patterns: React state for UI reactivity, refs for event handler access, buffered events during history load.

#### New state

```typescript
// React state
const [todos, setTodos] = useState<Todo[]>([]);
const [todosLoaded, setTodosLoaded] = useState(false);

// Ref for event handler access (avoids stale closures)
const todosRef = useRef<Todo[]>([]);
```

#### Event handling

Add a new branch in `handleEvent` for the `conversation_todos` event type:

```typescript
const handleEvent = useCallback((frame: EventFrame) => {
  // ... existing "defaultAgent", "conversations", etc. handlers ...

  if (frame.event === "conversation_todos") {
    const payload = frame.payload as ConversationTodosEvent;
    // Only process events for the currently viewed conversation
    if (payload.conversationId !== conversationIdRef.current) return;

    setTodos(prev => {
      let next: Todo[];
      switch (payload.action) {
        case "add":
          // Append new todo, re-sort
          next = [...prev, payload.todo!];
          break;
        case "update":
        case "complete":
        case "reopen":
          // Replace the updated todo in-place
          next = prev.map(t => t.id === payload.todoId ? payload.todo! : t);
          break;
        case "delete":
          next = prev.filter(t => t.id !== payload.todoId);
          break;
        default:
          return prev;
      }
      // Maintain sort: open before done, then priority desc, then createdAt desc
      next.sort(todoSortComparator);
      todosRef.current = next;
      return next;
    });
    return;
  }

  // ... existing "conversation" event handler ...
}, []);
```

#### Sort comparator

```typescript
const PRIORITY_ORDER: Record<string, number> = { high: 0, medium: 1, low: 2 };

function todoSortComparator(a: Todo, b: Todo): number {
  // Open items first
  if (a.status !== b.status) return a.status === "open" ? -1 : 1;
  // Then by priority (high → low)
  const pa = PRIORITY_ORDER[a.priority] ?? 1;
  const pb = PRIORITY_ORDER[b.priority] ?? 1;
  if (pa !== pb) return pa - pb;
  // Then by createdAt (newest first)
  return b.createdAt.localeCompare(a.createdAt);
}
```

#### Conversation switch

When `switchConversation()` is called, fetch the todo list alongside the conversation history:

```typescript
async function switchConversation(conversationId: string, agentId: string) {
  // ... existing: clear streaming state, set historyLoadedRef = false ...

  // Clear stale todo state immediately
  setTodos([]);
  setTodosLoaded(false);
  todosRef.current = [];

  // Fetch history and todos in parallel
  const [historyResult, todosResult] = await Promise.all([
    sendRpc<ConversationHistoryResult>("conversations.history", { conversationId, agentId }),
    sendRpc<ConversationTodosListResult>("conversations.todos.list", { conversationId }),
  ]);

  // ... existing: process historyResult ...

  // Apply todo state
  const sorted = todosResult.todos.sort(todoSortComparator);
  todosRef.current = sorted;
  setTodos(sorted);
  setTodosLoaded(true);

  // ... existing: set historyLoadedRef = true, replay buffered events ...
}
```

#### Event buffering during load

Todo events must be buffered alongside conversation events when `historyLoadedRef` is false, to prevent events arriving between the RPC call and its response from being dropped:

```typescript
if (!historyLoadedRef.current) {
  pendingEventsRef.current.push(frame);
  return;
}
```

This already applies to all events in `handleEvent` — the `conversation_todos` branch is placed after this guard, so it inherits the buffering behavior.

#### Exposed API

`useBackend` returns the todo state and mutation helpers:

```typescript
return {
  // ... existing fields ...
  todos,
  todosLoaded,
  addTodo: (title: string, priority?: string) =>
    sendRpc("conversations.todos.add", {
      conversationId: conversationIdRef.current, title, priority,
    }),
  completeTodo: (todoId: string) =>
    sendRpc("conversations.todos.complete", {
      conversationId: conversationIdRef.current, todoId,
    }),
  reopenTodo: (todoId: string) =>
    sendRpc("conversations.todos.reopen", {
      conversationId: conversationIdRef.current, todoId,
    }),
  deleteTodo: (todoId: string) =>
    sendRpc("conversations.todos.delete", {
      conversationId: conversationIdRef.current, todoId,
    }),
};
```

Mutations are fire-and-forget on the UI side — the pubsub event will update state. This ensures that manual user edits and agent edits follow the same code path.

---

### UI Components

#### `TodoPanel` — collapsible inline panel

**Location:** Rendered inside the conversation view (`$conversationId.tsx`), positioned between the `MessageList` and the `InputArea`.

```
┌─────────────────────────────────────────────┐
│  MessageList (virtualized, scrollable)      │
│  ...                                        │
│  [assistant message]                        │
│  [tool invocation: conversation_todo]       │
│  [tool result]                              │
│  [assistant message]                        │
└─────────────────────────────────────────────┘
┌─────────────────────────────────────────────┐
│ ▾ Todos  2/5                         [+ Add]│ ← collapsible header
│─────────────────────────────────────────────│
│ ☐ Implement auth middleware          high   │ ← open item
│ ☐ Add rate limiting                  medium │
│ ☑ Set up project structure           low    │ ← done item (dimmed)
│ ☑ Create database schema             high   │
│ ...                                         │
└─────────────────────────────────────────────┘
┌─────────────────────────────────────────────┐
│  InputArea                                  │
└─────────────────────────────────────────────┘
```

**Component:** `web/src/components/TodoPanel.tsx`

```typescript
interface TodoPanelProps {
  todos: Todo[];
  todosLoaded: boolean;
  isRunning: boolean;
  onComplete: (todoId: string) => void;
  onReopen: (todoId: string) => void;
  onAdd: (title: string, priority?: string) => void;
  onDelete: (todoId: string) => void;
}
```

**Behavior:**

- **Hidden** when `todosLoaded` is true and `todos` is empty and there is no active run. The panel only appears once the agent (or user) creates the first todo.
- **Auto-expand** when the first todo is added during a run (transition from empty to non-empty).
- **Collapsible** via clicking the header. Collapsed state stored in `localStorage` (`teanode-todosPanelCollapsed`). When collapsed, the header still shows the count badge ("2/5" = 2 open / 5 total).
- **Auto-collapse** when all items are done and the run completes (no open items remain).

#### Header

```
▾ Todos  2/5                                [+ Add]
```

- `▾`/`▸`: collapse toggle chevron.
- "Todos": i18n label (`conversation.todos.title`).
- "2/5": open count / total count. Green when all done ("0/5 ✓"). Omitted when empty.
- `[+ Add]`: opens an inline input row for manual todo creation. Only shown when panel is expanded.

#### Todo item row

Each todo is a single row with:

```
☐ Implement auth middleware                   high
```

| Element | Behavior |
|---------|----------|
| Checkbox (`☐`/`☑`) | Click → `onComplete(todoId)` or `onReopen(todoId)`. Disabled while an RPC is in-flight for this item (optimistic UI: toggle immediately, revert on error). |
| Title | Plain text. Click to expand description (if present). No inline editing for v1 — edit via agent or future REST UI. |
| Priority | Colored indicator: `high` = red dot/text, `medium` = amber, `low` = gray. |
| Delete | Hidden by default, shown on hover (trash icon). Confirms via native `confirm()` dialog. |

**Styling for done items:** Dimmed text (`opacity: 0.5`), strikethrough on title. Done items sorted below open items.

#### Agent-driven updates (animation)

When the agent adds or completes a todo during an active run:

- **New item**: Slides in with a brief highlight animation (subtle background flash, ~300ms). This draws the user's eye to the change without being disruptive.
- **Completed item**: Checkbox animates to checked, then the item dims and slides to the "done" section after a short delay (~500ms).
- **`isRunning` indicator**: When a run is active and todos exist, a small pulsing dot appears next to the "Todos" header to indicate the agent may be updating the list.

#### Empty state

When `todosLoaded && todos.length === 0`:

- During a run: panel is hidden (agent may or may not use todos — no need to show an empty panel).
- No active run: panel is hidden. It appears only when content exists.

#### Manual "Add Todo" flow

Clicking `[+ Add]` inserts an inline input row at the top of the list:

```
┌─────────────────────────────────────────┐
│ [Enter todo title...        ] [medium ▾]│
└─────────────────────────────────────────┘
```

- Text input with Enter to submit, Escape to cancel.
- Priority dropdown (defaults to `medium`).
- On submit: calls `onAdd(title, priority)`. The RPC event will add it to the list.

---

### State Reconciliation on Refresh (Rehydration)

When the user refreshes the page or reconnects after a disconnect, the todo state must be reconstructed accurately.

#### Flow

```
1. Page load / WebSocket reconnect
2. useWebSocket → connect() → onOpen callback
3. useBackend → send RPC "connect" → get capabilities, agents, etc.
4. useBackend → switchConversation(currentConversationId)
     4a. Send "conversations.history" RPC (existing)
     4b. Send "conversations.todos.list" RPC (new, parallel with 4a)
     4c. Set historyLoadedRef = false during fetch (buffers all incoming events)
5. Both RPCs resolve:
     5a. Process history result → set messages (existing)
     5b. Process todos result → set todos
     5c. Set historyLoadedRef = true
6. Replay buffered events (pendingEventsRef):
     6a. conversation events → update messages (existing)
     6b. conversation_todos events → update todos (new)
7. UI is now consistent with backend state
```

#### Why this is safe

- **No missed events**: Events that arrive between the RPC call and its response are buffered in `pendingEventsRef` (existing mechanism). The `conversation_todos` event handler is placed after the buffering guard, so todo events are also buffered.
- **Idempotent replay**: Todo event handlers use `setTodos(prev => ...)` with deduplication by `todoId`. Replaying an `add` event for a todo that already exists in the initial fetch is handled by checking if the ID is already present:

```typescript
case "add":
  if (prev.some(t => t.id === payload.todo!.id)) return prev; // already present from initial fetch
  next = [...prev, payload.todo!];
  break;
```

- **Ordering**: The initial fetch returns all todos. Buffered events apply deltas on top. Since events are replayed in order, the final state is consistent.
- **Stale event filtering**: Events for a different conversation are ignored (checked via `conversationIdRef.current`).

#### Edge case: run completes during reconnect

If a run was active when the user disconnected and completes before reconnect:

1. `conversations.todos.list` returns the final state (all mutations already applied).
2. `conversations.history` returns `activeRunId: undefined` (run completed).
3. No buffered events to replay. UI shows the correct final state.

If the run is still active during reconnect:

1. `conversations.todos.list` returns the current snapshot.
2. `conversations.history` returns `activeRunId: "01..."` (run still going).
3. Buffered `conversation_todos` events from the ongoing run are replayed, applying any mutations that happened after the snapshot.

---

### Integration with Existing Components

#### `$conversationId.tsx` (conversation route)

```tsx
const { todos, todosLoaded, isRunning, addTodo, completeTodo, reopenTodo, deleteTodo } = useApp().backend;

return (
  <div className="conversation-view">
    <MessageList ... />
    <TodoPanel
      todos={todos}
      todosLoaded={todosLoaded}
      isRunning={isRunning}
      onComplete={completeTodo}
      onReopen={reopenTodo}
      onAdd={addTodo}
      onDelete={deleteTodo}
    />
    <InputArea ... />
  </div>
);
```

#### `MessageList.tsx` (no changes needed)

Tool invocations for `conversation_todo` render via existing `ToolInvoke` / `ToolResult` components. The user sees both the tool call in the message stream and the live panel update — these are complementary views. The message stream shows *what the agent did*; the panel shows *current state*.

#### `context.tsx`

Add `todosPanelCollapsed` preference (boolean, default `false`) to `AppContext`, persisted in `localStorage` with key `teanode-todosPanelCollapsed`.

---

### i18n Keys

Add to `web/src/i18n/locales/en.json`:

```json
{
  "conversation.todos.title": "Todos",
  "conversation.todos.add": "Add Todo",
  "conversation.todos.addPlaceholder": "Enter todo title...",
  "conversation.todos.empty": "No todos yet",
  "conversation.todos.allDone": "All done",
  "conversation.todos.openCount": "{{open}}/{{total}}",
  "conversation.todos.deleteConfirm": "Delete this todo?",
  "conversation.todos.status.open": "Open",
  "conversation.todos.status.done": "Done",
  "conversation.todos.priority.low": "Low",
  "conversation.todos.priority.medium": "Medium",
  "conversation.todos.priority.high": "High"
}
```

---

### Summary: What Changes Where

| Layer | File | Change |
|-------|------|--------|
| Backend: pubsub | `internal/pubsub/pubsub.go` | Add `EventTypeConversationTodos` constant |
| Backend: tool | `internal/tools/conversations/todo.go` | Emit `conversation_todos` event after every mutation |
| Backend: RPC | `internal/api/v1api/websocket.go` | Handle `conversations.todos.*` RPC methods |
| Frontend: types | `web/src/types.ts` | Add `Todo`, `ConversationTodosEvent`, `ConversationTodosListResult` |
| Frontend: state | `web/src/hooks/useBackend.ts` | Add todo state, event handler, RPC calls, expose mutation helpers |
| Frontend: context | `web/src/context.tsx` | Add `todosPanelCollapsed` preference |
| Frontend: component | `web/src/components/TodoPanel.tsx` | New component (panel + items + add form) |
| Frontend: route | `web/src/routes/conversations/$agentId/$conversationId.tsx` | Render `TodoPanel` between MessageList and InputArea |
| Frontend: i18n | `web/src/i18n/locales/en.json` | Add `conversation.todos.*` keys |

---

## Testing Strategy

### Unit tests — `project_todo`

**File:** `internal/tools/projects/todo_test.go`

Follow the pattern from `tools_test.go`: `setupProjectsToolStore(t)` → fsstore with temp dir, migrate, cleanup. Context with store + admin user (via `models.ContextWithUserSessionToken`). Pre-create a project using the `projects` tool's `create` action.

| Test | Description |
|------|-------------|
| `TestProjectTodoAddAndList` | Add a todo, list todos, verify it appears with correct fields |
| `TestProjectTodoUpdate` | Add, update title/priority/tags, verify changes |
| `TestProjectTodoComplete` | Add, complete, verify status=done and completedAt is set |
| `TestProjectTodoReopen` | Add, complete, reopen, verify status=open, completedAt cleared |
| `TestProjectTodoDelete` | Add, delete, list returns empty |
| `TestProjectTodoListFilters` | Add multiple with different status/priority/tags, test filters |
| `TestProjectTodoAddMissingTitle` | Attempt add without title, expect error |
| `TestProjectTodoUpdateNotFound` | Attempt update with invalid todoId, expect error |
| `TestProjectTodoDeleteNotFound` | Attempt delete with invalid todoId, expect error |
| `TestProjectTodoNonAdminReadOnly` | List succeeds, add/update/delete fail for non-admin user |
| `TestProjectTodoProjectNameLookup` | Use projectName instead of projectId, verify resolution |
| `TestProjectTodoProjectModifiedAt` | Verify project.ModifiedAt is bumped after mutation |

### Unit tests — `conversation_todo`

**File:** `internal/tools/conversations/todo_test.go`

Setup: fsstore with temp dir, migrate. Create a user, an agent, and a conversation. Context with store + user.

| Test | Description |
|------|-------------|
| `TestConvTodoAddAndList` | Add a todo, list todos, verify it appears with correct fields |
| `TestConvTodoUpdate` | Add, update title/priority/tags, verify changes |
| `TestConvTodoComplete` | Add, complete, verify status=done and completedAt is set |
| `TestConvTodoReopen` | Add, complete, reopen, verify status=open, completedAt cleared |
| `TestConvTodoDelete` | Add, delete, list returns empty |
| `TestConvTodoListFilters` | Add multiple with different status/priority/tags, test filters |
| `TestConvTodoOwnershipDenied` | User A cannot access todos in User B's conversation |
| `TestConvTodoAdminCrossAccess` | Admin can access todos in any user's conversation |
| `TestConvTodoInvalidConversation` | Attempt actions with non-existent conversationId, expect error |
| `TestConvTodoConversationModifiedAt` | Verify conversation.ModifiedAt is bumped after mutation |

### Store tests

**File:** `internal/store/fsstore/filesystem_todo_test.go`

| Test | Description |
|------|-------------|
| `TestTodoCreateAndGet` | Create a todo, get by ID, verify all fields round-trip |
| `TestTodoListByProjectId` | Create todos across two projects, list by each, verify isolation |
| `TestTodoListByConversationId` | Create todos across two conversations, list by each, verify isolation |
| `TestTodoModify` | Create, modify title+status, verify changes persisted |
| `TestTodoDelete` | Create, delete, get returns `ErrNotFound` |
| `TestTodoDeleteNotFound` | Delete non-existent ID, expect `ErrNotFound` |
| `TestTodoListEmpty` | List on scope with no todos, returns empty slice (not nil) |
| `TestTodoCascadeOnProjectDelete` | Create project + todos, delete project, verify todos are gone |
| `TestTodoCascadeOnConversationDelete` | Create conversation + todos, delete conversation, verify todos are gone |

**File:** `internal/store/dbstore/database_todo_test.go` — mirror of the above, run against a test PostgreSQL instance (if CI provides one, otherwise skip with build tag).

### Runner authorization tests

Add cases to existing runner authorization tests:

| Test | Description |
|------|-------------|
| Non-admin + `project_todo` + `action: "list"` | Allowed |
| Non-admin + `project_todo` + `action: "add"` | Denied |
| Admin + `project_todo` + any action | Allowed |
| Any user + `conversation_todo` + any action | Allowed at runner level (ownership checked in tool) |

### Edge cases

- Empty scope (no todos yet) — `list` returns empty array, `add` creates first todo.
- Concurrent tool calls — serialized by store transaction (fsstore mutex / dbstore row locking).
- Large todo lists — verify no truncation up to hundreds of items.
- Todo with no tags — `tags` field defaults to `[]`, not `null`.
- Todo with empty title — rejected at tool level with clear error.
- Conversation deleted while todos exist — cascade removes todos (dbstore FK, fsstore directory removal).
- Project deleted while todos exist — same cascade behavior.

---

## Migration / Rollout

- **Database migration required:** `0001_todos.sql` creates the `todos` table with indexes and constraints.
- **Filesystem store:** No migration — directories are created lazily on first `CreateTodo`.
- **No config changes required.**
- Tools become available immediately upon import of their packages (via `init()` registration).
- Existing projects and conversations gain todo support automatically — first `add` action creates the first todo record.
- Tools should be added to the default tool allowlist for agents that need them, or left available to all agents (matching `project_workspace` behavior).

---

## Open Questions

1. **Todo ordering:** Should todos support explicit ordering/position, or is sort-by-priority-then-date sufficient?
2. **Assignee field:** Should todos support an optional `assignee` (user ID)? Useful for multi-user project todos but adds complexity.
3. **Due dates:** Should todos support an optional `dueDate` field?
4. **Sub-tasks:** Should todos support a `parentId` for hierarchical task breakdown?
5. **REST API:** Should dedicated REST endpoints be added from the start, or deferred until UI work begins?
6. **Conversation todo auto-population:** Should the agent's system prompt include a summary of open conversation todos? This would give the agent awareness of pending tasks without an explicit `list` call.
7. **Cross-scope visibility:** Should `project_todo` list be able to include a roll-up of conversation todos related to that project? (Requires linking conversations to projects, which is not currently modeled.)

These can be addressed in follow-up iterations. The initial implementation should focus on the core CRUD actions with the fields defined above.
