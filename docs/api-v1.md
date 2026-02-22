# HTTP API v1 Overview

TeaNode exposes an OpenAI-compatible HTTP API under `/api/v1`. This document gives a high-level overview of the current surface and how it maps onto TeaNode internals.

## Endpoints

### `POST /api/v1/chat/completions`

- **Purpose:** OpenAI-compatible Chat Completions endpoint.
- **Backed by:** `internal/api/v1api` handlers and the TeaNode agents layer.
- **Behavior:**
  - Accepts a subset of the OpenAI Chat Completions schema (models, messages, tools, etc.).
  - Routes the request to a configured TeaNode agent and provider.
  - Streams or buffers responses depending on the `stream` flag.
  - Supports tool calls via TeaNode's internal tools registry.

Notes:

- Exact compatibility and any deviations from the OpenAI spec are tracked in `TODO.md` under **Documentation** ("Document OpenAI-compatible API surface and any deviations"). This file is a high-level pointer; consult the Go handlers in `internal/api/v1api` for precise behavior.

### `GET /api/v1/health`

- **Purpose:** Lightweight health check endpoint.
- **Backed by:** `internal/api/v1api` health handler.
- **Behavior (current / minimal):**
  - Returns a simple JSON payload indicating that the gateway process is up.
  - Intended primarily for liveness checks.

Planned enhancements (see `TODO.md` under **Features**):

- Deepen `/health` to check:
  - Workspace availability.
  - Provider reachability.
  - Potentially other core subsystems.

### `GET /api/v1/profile` and `PUT /api/v1/profile`

- **Purpose:** Read and update user profile data used by the frontend and prompt personalization.
- **Backed by:** `internal/api/v1api/profile.go` and `internal/configs/profile.go`.
- **Behavior:**
  - `GET` returns the current profile.
  - `PUT` updates profile fields (`name`, `bio`, `avatarMediaId`).
  - `name` falls back to the OS username when missing/empty.
  - `bio` is treated as raw markdown text.
  - Profile persistence is `~/.teanode/profile.md` with YAML front matter metadata (`name`, `avatarMediaId`) and markdown body for biography.

## Relationship to Internals

- The v1 API is a thin HTTP layer over:
  - `internal/agents` for conversation orchestration and tool calls.
  - `internal/provider` for model-specific HTTP calls.
  - `internal/conversations` for persistent conversation storage.
- Authentication, logging, and error handling are also implemented in `internal/api/v1api` and related middleware.

This document is intentionally high-level and meant as a starting point for navigating the v1 API implementation rather than a full reference.
