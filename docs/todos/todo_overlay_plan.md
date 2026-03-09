# Conversation TODO Overlay — Implementation Plan

## Goal

Inject a dynamic TODO summary as a **late message** (appended after conversation history, before the LLM call) so the model is always aware of outstanding tasks for the current conversation. This keeps the overlay out of the system prompt, making it cache-friendly.

---

## Design Decisions

1. **Late message, not system prompt.** The overlay is appended as the last `"system"` role message in the `[]providers.ChatMessage` slice returned by `buildMessages`, after all conversation history and after `fixInterruptedToolCalls`. This avoids invalidating the system-prompt cache on every call.

2. **Direct store access.** The runner already uses `store.StoreFromContext(ctx)` (see `runner.go:484,619,816`). The overlay builder will use the same pattern inside a read-only transaction to fetch todos — no tool calls involved.

3. **Scoped by ConversationID.** `TodoListOptions{ConversationID: &self.ConversationID}` guarantees no cross-conversation or cross-user leakage (the conversation itself is already user-scoped).

4. **Top-10 open, sorted by priority then recency.** The existing DB query in `database_todo.go:43` already sorts open todos first, then by priority (high > medium > low), then by `created_at DESC`. We will filter to status=open in Go after fetching, take the first 10, and also count totals.

---

## Message Format

```
<todos>
Open: 7 | Done: 3

# Open TODOs (top 10 by priority)
1. [HIGH] <todoId> — Title here
   Description truncated to 120 chars…
2. [MEDIUM] <todoId> — Another task
3. [LOW] <todoId> — Low-pri task

Reminder: mark completed todos done via the todo tool. Prune old done items when the list gets noisy.
</todos>
```

If there are zero todos (open + done == 0), no overlay message is appended.

---

## File-by-File Changes

### 1. `internal/runners/todo_overlay.go` (new file)

Create a new file containing a single exported function:

```
func buildTodoOverlay(ctx context.Context, conversationID string) (string, error)
```

**Logic:**
- Call `store.StoreFromContext(ctx).Transaction(...)` with a read-only callback.
- Inside the transaction, call `transaction.ListTodos(ctx, store.TodoListOptions{ConversationID: &conversationID}, nil)`.
- Iterate the returned `[]*models.Todo` slice:
  - Count open todos (`status == TodoStatusOpen`).
  - Count done todos (`status == TodoStatusDone`).
  - Collect up to 10 open todos (they come pre-sorted by the DB query: open first, then priority desc, then created_at desc — see `database_todo.go:43`).
- If openCount + doneCount == 0, return `""` (no overlay).
- Format the overlay string using the template above:
  - Include `todo.ID` and `*todo.Title`.
  - If `todo.Description != nil`, truncate to 120 characters and append.
  - Wrap in `<todos>...</todos>` tags.
- Return the formatted string.

**Key references:**
- `store.StoreFromContext` — `internal/store/context.go:14`
- `store.TodoListOptions` — `internal/store/types.go:36-39`
- `store.TodoOperation.ListTodos` — `internal/store/interfaces.go:125`
- `models.Todo` struct — `internal/models/todo.go:20-33`
- `models.TodoStatusOpen`, `models.TodoStatusDone` — `internal/models/todo.go:6-12`
- `models.TodoPriorityHigh/Medium/Low` — `internal/models/todo.go:14-19`

### 2. `internal/runners/runner.go` — modify `buildMessages()`

**Location:** `runner.go:631-720`

After `fixInterruptedToolCalls` (line 717) and before the `return messages` (line 719), insert:

```go
// Append TODO overlay as a late message.
if todoOverlay, err := buildTodoOverlay(ctx, self.ConversationID); err == nil && todoOverlay != "" {
    messages = append(messages, providers.ChatMessage{
        Role:    "system",
        Content: todoOverlay,
    })
}
```

If the overlay errors (store unavailable, etc.), silently skip — the overlay is best-effort and must never block a model call.

**Why here:** Placing it after `fixInterruptedToolCalls` ensures:
- It's the very last message the model sees (high salience).
- It doesn't interfere with tool-call repair logic.
- It's after history truncation/compression, so it's always present even in compressed contexts.

### 3. `internal/store/dbstore/database_todo.go` — no changes required

The existing `ListTodos` query (lines 32-53) already:
- Filters by `ConversationID` when provided.
- Sorts by status (open first), priority (high > medium > low), then `created_at DESC`.

This ordering is exactly what we need. No new query or index is required.

### 4. `internal/store/types.go` — no changes required

`TodoListOptions` already has `ConversationID *string` (line 38).

---

## Scoping & Isolation

- **ConversationID filter:** `ListTodos` filters at the DB level via `WHERE conversation_id = ?`. Todos with a different `conversation_id` (or `project_id` instead) are never returned.
- **User isolation:** Conversations are themselves scoped to `(user_id, agent_id)` pairs (`database_conversation.go:30-51`). The runner's `ConversationID` is set from the authenticated user's session, so there is no path for user A to see user B's conversation todos.
- **No project todos:** The overlay intentionally queries only by `ConversationID`, excluding project-scoped todos. This prevents leaking todos from shared projects into a single user's conversation context.

---

## Test Plan

### Unit Tests: `internal/runners/todo_overlay_test.go` (new file)

| # | Test Case | Description |
|---|-----------|-------------|
| 1 | **No todos** | Empty store returns `""`. Verify no message is appended. |
| 2 | **Only done todos** | 3 done, 0 open. Overlay shows "Open: 0 \| Done: 3" with no list items, just the counts and reminder. |
| 3 | **Mixed open/done** | 5 open, 2 done. Verify counts are correct, all 5 open listed. |
| 4 | **More than 10 open** | 15 open todos with varying priorities. Verify only top 10 are listed; counts still show 15 open. |
| 5 | **Priority ordering** | 3 low, 3 medium, 3 high. Verify high items appear first, then medium, then low. |
| 6 | **Description truncation** | Todo with 200-char description. Verify it's truncated to 120 chars with "…". |
| 7 | **Nil description** | Todo with nil description. Verify no description line is rendered. |
| 8 | **XML escaping** | Todo title containing `<`, `>`, `&`. Verify these are escaped or handled safely. |

### Integration Test: `internal/runners/runner_test.go` (extend existing)

| # | Test Case | Description |
|---|-----------|-------------|
| 9 | **Overlay appears in messages** | Set up a conversation with todos, call `buildMessages`, verify the last message has role `"system"` and contains `<todos>`. |
| 10 | **No overlay when no todos** | Call `buildMessages` for a conversation with no todos. Verify no extra system message is appended beyond the initial system prompt (and optional summary). |
| 11 | **Isolation** | Create todos for conversation A and conversation B. Build messages for conversation A. Verify only A's todos appear. |

### Manual Verification

- Start a conversation, create several todos via the todo tool, then observe the raw LLM request logs to confirm the overlay appears as the last message.
- Verify that creating/completing todos in one conversation does not affect the overlay in another.
- Confirm no measurable latency increase on the model call path (the `ListTodos` query is indexed and bounded).

---

## Risks & Mitigations

| Risk | Mitigation |
|------|-----------|
| Overlay adds tokens to every call | Bounded to ~10 items × ~50 tokens ≈ 500 tokens max. Negligible vs typical context. |
| Store error blocks model call | Silently skip overlay on error (best-effort). |
| Stale overlay after mid-run todo changes | Overlay is rebuilt on every round of the tool-call loop (line 186 calls `buildMessages` each iteration), so it refreshes naturally. |
| Priority ties produce unstable ordering | DB sorts by `created_at DESC` as tiebreaker, which is deterministic. |
