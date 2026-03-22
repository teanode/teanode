# Conversations

The `internal/store` package (with backends `fsstore` and `dbstore`) is responsible for storing and retrieving conversational state for TeaNode agents.

## Overview

A **conversation** is a logical session between a user and an agent. It ties together:

- A unique ID.
- Metadata (titles, timestamps, associated user / agent IDs).
- A history of messages.
- Optional summaries for older parts of the history.

High-level responsibilities of the conversation layer:

- Persist conversations so they survive process restarts.
- Provide efficient append/read operations for message history.
- Maintain basic metadata (e.g. title, created/updated times).
- Support listing and lookup by ID.

## Store Interface

Conversation persistence is defined by two interfaces in `internal/store/interfaces.go`:

- `ConversationOperation` – CRUD for conversation metadata (list, create, get, modify, delete, find/set default).
- `ConversationMessageOperation` – append and list messages within a conversation.

These interfaces are part of the unified `Transaction` interface, meaning conversation operations share the same transactional scope as agents, users, jobs, media, sessions, and other domain objects.

## Store Backends

TeaNode ships with two backend implementations:

### `fsstore` (filesystem)

- Conversations are stored as JSONL files under the data directory (by default `~/.teanode/users/<userId>/conversations/<agentId>/<conversationId>.jsonl`).
- Each line is a single JSON object (message record).
- This makes appends cheap and streaming-friendly, while keeping the format simple to inspect and debug.
- Conversation-scoped TODOs are stored in a `<conversationId>.todos/` subdirectory.
- Key files: `filesystem_conversation.go` (metadata), `filesystem_conversation_message.go` (message append/read), `filesystem_conversation_storage.go` (JSONL I/O).

### `dbstore` (database)

- Uses PostgreSQL via GORM.
- Conversations and messages are stored in relational tables.
- Key files: `database_conversation.go` (metadata), `database_conversation_message.go` (messages).

## Types

Conversation and message types live in `internal/models`:

- `conversation.go` – `Conversation` struct (ID, title, timestamps, user/agent associations).
- `conversation_message.go` – `ConversationMessage` struct (role, content, tool calls, metadata).
- `context.go` – context types for messages, tool calls, and conversation state.

## Higher-Level Behaviors

On top of the store layer, other packages implement higher-level behaviors:

- `internal/runners` – context compaction and pruning of old messages while preserving summaries; TODO overlay injection.
- `internal/summarizers` – auto-generating conversation titles and descriptions.
- `internal/coordinators` – orchestrating message send, active run tracking, and broadcasting events.
- `internal/api` – exposing WebSocket RPC methods like `conversations.send`, `conversations.history`, `conversations.list`, `conversations.todos.list`, and `conversations.todos.batch`.

### TODOs

Conversations can have associated TODO items (stored via `TodoOperation` in the store interface). The runner injects a summary of open TODOs into the conversation context via the TODO overlay (`todo_overlay.go`), giving the agent awareness of pending tasks.

This document is intentionally high-level; refer to the Go source in `internal/store` and `internal/models` for exact field names and behaviors.
