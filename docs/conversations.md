# Conversations

The `internal/conversations` package is responsible for storing and retrieving conversational state for TeaNode agents.

## Overview

A **conversation** is a logical session between a user (or channel) and an agent. It ties together:

- A unique ID.
- Metadata (titles, timestamps, associated channel / agent IDs).
- A history of messages.
- Optional summaries for older parts of the history.

High-level responsibilities of `internal/conversations`:

- Persist conversations to disk so they survive process restarts.
- Provide efficient append/read operations for message history.
- Maintain basic metadata (e.g. title, created/updated times).
- Support listing and lookup by ID.

## Storage Model

TeaNode uses a **JSONL-based** storage format for conversations (see also `docs/architecture.md` and TODO section "Conversation Management"):

- Each conversation is stored as one or more JSONL files under a workspace directory.
- Each line is a single JSON object (message or summary record).
- This makes appends cheap and streaming-friendly, while keeping the format simple to inspect and debug.

The `store.go` implementation under `internal/conversations` handles:

- Opening/creating files for a given conversation ID.
- Appending new records (messages, summaries, metadata updates).
- Reading back history in order when requested.
- Caching or indexing minimal metadata for fast `list` operations.

## Types and Responsibilities

Key types (see `types.go`):

- Conversation identifiers and metadata structs.
- Message and summary record types written to/read from JSONL.

Key components:

- `store.go` – file-backed store implementation for conversations.
- `conversation.go` – small helpers/types for working with a single conversation instance.

On top of this package, the agents and API layers implement higher-level behaviors such as:

- Auto-generating conversation titles and summaries.
- Pruning or compacting old messages while preserving summaries.
- Exposing RPC methods like `conversations.send`, `conversations.history`, and `conversations.list`.

This document is intentionally high-level and describes the intended model; refer to the Go source in `internal/conversations` for exact field names and behaviors.
