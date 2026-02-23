# TeaNode Multi-User Support Implementation Plan (File-Based)

## 1) Current repo touchpoints (cite real files/modules)

This plan minimizes changes by layering user identity/context over existing gateway/runner/file-store architecture.

- Startup wiring and tool registration:
  - `cmd/gateway.go`
  - Current per-agent runner creation, workspace tool registration, conversation store wiring, channel startup, security/profile/session loading.
- Global path helpers and storage layout:
  - `internal/configs/config.go`
  - `Directory`, `EnsureDirectories`, `AgentWorkspaceDirectory`, `AgentConversationsDirectory`, `StateFile`, `ProjectsDirectory`, etc.
- Security config file model (`~/.teanode/security.yaml`):
  - `internal/configs/security.go`
  - Currently global `Token` + global password hash.
- State persistence (`~/.teanode/state.yaml`):
  - `internal/agents/registry.go`
  - `persistedState`, `LoadState`, `saveState`, default agent/conversation tracking.
- Session persistence (`~/.teanode/sessions/*.yaml`):
  - `internal/sessions/sessions.go`
  - Session model currently has no user identity field.
- Auth + auth middleware + websocket/session handling:
  - `internal/api/v1api/auth.go`
  - `internal/api/v1api/http.go`
  - `internal/api/v1api/websocket.go`
  - `internal/api/v1api/rpc.go`
  - `internal/gw/gateway.go` (`checkBearerToken`, `checkSessionCookie`, `AuthMiddleware`)
  - `internal/gw/gw.go` (gateway interface/signatures)
- Profile storage (`~/.teanode/users/<userId>/user.yaml`):
  - `internal/configs/profile.go`
  - `internal/api/v1api/profile.go`
- Conversation store:
  - `internal/conversations/store.go`
  - Wired today as `~/.teanode/conversations/<agentId>/<conversationId>.jsonl` by `configs.AgentConversationsDirectory`.
- Tools and prompt wiring:
  - `internal/tools/workspace/workspace.go` (current tool name: `workspace`)
  - `internal/tools/projects/projects.go` (includes `project_workspace`)
  - `internal/projects/projects.go` (project workspace path + file ops)
  - `internal/agents/systemprompt.txt`
  - `internal/agents/systemprompt.go`
- Channel integrations:
  - `internal/channels/telegram/bot.go`
  - `internal/channels/discord/bot.go`
  - Current state-based single chat/channel persistence is in `AgentRegistry` (`SetTelegramChatID`, `SetDiscordChannelID`).

## 2) Proposed YAML schemas for state.yaml and security.yaml

Keep file locations global and unchanged:
- `~/.teanode/state.yaml`
- `~/.teanode/security.yaml`

### `state.yaml`

Purpose: per-user runtime defaults only (default agent + per-agent default conversation).

```yaml
users:
  "01J...ULID":
    defaultAgentId: "main"
    defaultConversationIds:
      "main": "01J...ULID"
      "research": "01J...ULID"
```

Notes:
- `users` is keyed by `userId` (plain ULID string).
- Remove channel routing fields from state (`discordChannelId`, `telegramChatId`) in v2.
- Backward compatibility loader accepts v1 and migrates into an initial user entry.

### `security.yaml`

Purpose: per-user auth credentials + per-user tokens + channel identity mapping.

```yaml
users:
  "01J...ULID":
    username: "alice"
    passwordHash: "$2a$..."
    tokens:
      - id: "01J...ULID"
        token: "tn_live_token_plaintext_1"
        createdAt: "2026-02-22T08:00:00.000-08:00"
        lastUsedAt: null
      - id: "01J...ULID"
        token: "tn_live_token_plaintext_2"
        createdAt: "2026-02-22T08:00:00.000-08:00"
        lastUsedAt: null
channelLinks:
  telegram:
    "123456789": "01J...ULID"     # chatId -> userId
  discord:
    "998877665544332211": "01J...ULID"  # discordUserId -> userId
```

Notes:
- Tokens are stored and matched in plaintext exactly as required.
- Password hashes remain bcrypt.
- Username uniqueness enforced case-insensitively in loader/validator.
- Existing v1 `{token,password}` is read and migrated into initial user record.

### Timestamp format policy

- Any timestamp persisted under `~/.teanode` should use local-time ISO datetime format with millisecond precision (example: `2026-02-22T08:00:00.123-08:00`).
- Loaders should remain backward compatible with legacy unix timestamps where applicable.

## 3) Auth data flow (password + session cookie + token) resolving to userId

Introduce a small shared type and keep interfaces mostly intact.

### New core type

- Add `UserContext` (small) in gateway/auth layer:
  - `UserID string`
  - `SessionID string` (optional)
  - `AuthMethod string` (`session` | `bearer`)

### Password flow

- `POST /api/v1/auth/setup`:
  - Create first local user with required `username + password` (bcrypt hash) and generated `userId` ULID.
  - Save in `security.yaml`.
  - Create session with `Session.UserID` set.
- `POST /api/v1/auth/login`:
  - Require username and validate local username/password from `security.yaml` users.
  - Create session file with `userId`.
  - Set `session` cookie unchanged.

### Session cookie flow

- `checkSessionCookie` resolves cookie -> session -> `session.UserID`.
- Session store schema extension:
  - `internal/sessions/sessions.go`: add `UserID` field.
- Middleware attaches resolved `UserContext` to request context.
- WebSocket handshake captures `UserContext`; each connection carries `userId`.

### Token (Bearer) flow

- `Authorization: Bearer <token>`:
  - Iterate `security.yaml` user map and exact-match against `tokens` (plaintext).
  - Resolve to `userId` and construct `UserContext`.
- No legacy global token fallback to an implicit/default user.
- No hashing, no token derivation.

### No implicit default user

- There is no implicit `"default"` user for authenticated API flows.
- Authenticated operations must resolve a real `userId` from session cookie or bearer token.
- All users authenticate via username + password setup/login.

### API/RPC usage

- Update handlers using auth/session data (`internal/api/v1api/auth.go`, `internal/api/v1api/rpc.go`, `internal/api/v1api/websocket.go`) so all operations requiring identity resolve `userId` from `UserContext`.

## 4) Conversation store changes (per-user) and default conversation logic per-user

### File layout

New per-user conversations path:
- `~/.teanode/users/<userId>/conversations/<agentId>/<conversationId>.jsonl`

Implementation approach:
- Keep `internal/conversations/store.go` unchanged (directory-backed JSONL store).
- Change directory resolution in startup/runtime to pass per-user+agent directory rather than global per-agent directory.

### Runner/conversation resolution

- Current runners hold a fixed `Conversations *conversations.Store`.
- Prefer **less churn**: keep runners long-lived per agent, but resolve the conversation store at call time.
- Introduce a small `ConversationStoreResolver` (or equivalent) used by gateway/runner entrypoints:
  - `Store(userId, agentId) *conversations.Store`
- Runner methods that need conversation access should accept `userId` (or `UserContext`) and call the resolver, rather than storing a single global store.
- Avoid creating per-request runners unless unavoidable. This keeps provider/tool setup stable and minimizes refactors.

### Default conversation semantics

- Move default conversation lookup/set from global-agent to per-user+agent:
  - `EnsureDefaultConversation(userId, agentId)`
  - `SetDefaultConversation(userId, agentId, conversationId)`
  - `SetDefaultConversationIfUnset(userId, agentId, conversationId)`
- Persist to `state.yaml` under `users[userId].defaultConversationIds`.

### No cross-user conversations

- All conversation operations (`send`, `history`, `list`, `delete`, `compact`) resolve by authenticated `userId`.
- Same `agentId` remains shared, but conversation stores are isolated by user path.

### Per-user jobs

- Scheduled jobs are user-scoped.
- Jobs are stored at `~/.teanode/users/<userId>/jobs/*.md` (no top-level `~/.teanode/jobs` runtime storage).
- Jobs created via WebSocket RPC or tool calls must persist `userId` and be listed/updated/deleted/triggered only by that same user.
- Scheduler execution must run with that job owner `userId` so conversation resolution/defaults and memory access remain isolated per user.

## 5) Tool changes (agent_workspace rename, user_workspace new, project_workspace move) + system prompt updates

### 5.1 Rename `workspace` -> `agent_workspace`

- Touchpoints:
  - `internal/tools/workspace/workspace.go` (tool name/description)
  - tests in `internal/tools/workspace/workspace_test.go`
  - runner/tool-call tests referencing `workspace` (e.g., `internal/agents/runner_test.go`)
  - system prompt (`internal/agents/systemprompt.txt`)
- New root path:
  - `~/.teanode/agents/<agentId>/workspace`
- Access control:
  - unchanged model (tool instance created for one agent; only its own root).

### 5.2 Add `user_workspace`

- New tool module (or extension of existing workspace package).
- Root path:
  - `~/.teanode/users/<userId>/workspace`
  - must contain `MEMORY.md` and `memory/YYYY-MM-DD.md` convention.
- Access control:
  - tool must resolve `userId` from run context (`UserContext` propagated to run context).
  - agent can access only current authenticated user’s root.

### 5.3 Keep `project_workspace` name, move backing files

- Current code:
  - `internal/tools/projects/projects.go`
  - `internal/projects/projects.go` (`WorkspaceDirectory` currently `~/.teanode/projects/<projectId>`)
- New root:
  - `~/.teanode/projects/<projectId>/workspace`
- Metadata file path:
  - `~/.teanode/projects/<projectId>/project.yaml`
- Metadata timestamps (e.g., `updatedAt`) use local-time ISO datetime format.

### 5.4 User profile storage + memory ownership

- User profile lives at `~/.teanode/users/<userId>/user.yaml`.
- Profile stores identity settings only (`name`, `avatarMediaId`).
- User bio profile field is removed.
- Long-form personal memory belongs in `~/.teanode/users/<userId>/workspace/MEMORY.md`.

### 5.5 System prompt updates

- Update `internal/agents/systemprompt.txt` and related tests:
  - Replace references to `workspace` with `agent_workspace` and `user_workspace` with clear usage guidance.
  - Keep project guidance but update path to `/workspace` subdir.
  - Explicitly instruct memory writes to `user_workspace` (`MEMORY.md`, `memory/YYYY-MM-DD.md`).
  - Remove profile-bio injection from prompt context.

## 6) Channel mapping changes + “not linked” behavior

### Mapping source

- Use `security.yaml` `channelLinks`:
  - telegram `chatId -> userId`
  - discord `platform userId -> userId`

### Telegram/Discord request handling

- Touchpoints:
  - `internal/channels/telegram/bot.go`
  - `internal/channels/discord/bot.go`
- On inbound message:
  - resolve platform identity to `userId` via `security.yaml` mapping.
  - if absent: respond and stop processing (do not run agent).

Required response behavior:
- Telegram: include chatId in message, e.g. `not linked: telegram chatId=<chatId>`.
- Discord: include platform userId in message, e.g. `not linked: discord userId=<userId>`.

### State cleanup

- Remove dependence on `AgentRegistry` single channel fields for routing.
- Channel identities are mapping-driven from `security.yaml`.

## 7) Migration steps (startup), ordering, and rollback strategy

Add startup migrator invoked early in `cmd/gateway.go` after `EnsureDirectories` and before runtime stores/bots are initialized.

### Ordered startup migration (idempotent)

1. Acquire file lock marker (single-process migration guard).
2. Read `security.yaml` and `state.yaml` (accept missing).
3. If `security.yaml` has no `users` map:
   - create initial `userId` ULID.
   - map v1 password/token into first user record (`passwordHash`, one `token` if present).
   - initialize empty channelLinks maps.
4. If `state.yaml` has no `users` map:
   - move global defaults into `users[initialUserId]`.
   - migrate/remove legacy channel fields.
5. Migrate agent workspaces:
   - from `~/.teanode/workspace/<agentId>/` (legacy) and current `~/.teanode/workspaces/<agentId>/`
   - to `~/.teanode/agents/<agentId>/workspace/`
6. Migrate project workspaces + metadata:
   - from `~/.teanode/projects/<projectId>/*`
   - to `~/.teanode/projects/<projectId>/workspace/*`
   - move `~/.teanode/projects/<projectId>.yaml` to `~/.teanode/projects/<projectId>/project.yaml`
   - convert metadata timestamps (e.g., `updatedAt`) to local-time ISO datetime format.
7. Migrate single-user user data into initial user:
   - Clarification: legacy `~/.teanode/workspace/` is primarily an agent workspace root (`workspace/<agentId>/`).
     The migrator must first migrate all child directories as agent workspaces. Only leftover files (non-directories)
     or explicitly recognized legacy user-memory paths should be moved into the initial user’s `users/<userId>/workspace/`.
     Unknown leftovers should be left in `.trash` with a log entry to avoid accidental data loss.
   - profile: `~/.teanode/profile.md` and `~/.teanode/users/<userId>.md` -> `~/.teanode/users/<userId>/user.yaml`
     - preserve `name` and `avatarMediaId`; drop legacy bio body.
   - memory/workspace legacy: `~/.teanode/workspace/*` (non-agent legacy files) -> `~/.teanode/users/<userId>/workspace/*`
   - conversations: `~/.teanode/conversations/<agentId>/*.jsonl` -> `~/.teanode/users/<userId>/conversations/<agentId>/*.jsonl`
   - jobs: `~/.teanode/jobs/*.md` -> `~/.teanode/users/<userId>/jobs/*.md`
8. Ensure per-user directories exist:
   - `~/.teanode/users/<userId>/`
   - `~/.teanode/users/<userId>/workspace/`
   - `~/.teanode/users/<userId>/workspace/memory/`
9. Write `security.yaml` and `state.yaml` atomically.
10. Write migration marker/version file (e.g., `~/.teanode/.migrations/multiuser-v2.done`).
11. Normalize persisted `~/.teanode` metadata timestamps to local-time ISO datetime format (with millisecond precision), while keeping legacy unix parsing in loaders.

### Idempotency and safety

- Every move/copy checks destination existence first.
- Use atomic writes for YAML updates.
- Use move-then-verify where possible; fallback to copy for cross-device.
- Never delete source before successful destination write/verify.
- Re-running migration results in no-op for already migrated paths.

### Rollback strategy

- Before migration, copy `state.yaml` and `security.yaml` to timestamped backups in `~/.teanode/.backup/`.
- For directory migrations, keep moved legacy paths in `.trash` (or suffixed backup paths) until successful startup.
- If migration fails:
  - keep original files untouched when possible,
  - restore YAML from backup,
  - log actionable error and start in read-only fail-fast mode (no partial writes).

## 8) Test plan (unit/integration) referencing existing tests and where to add new ones

### Update existing tests

- Config path and directory expectations:
  - `internal/configs/config_test.go`
  - Update `AgentWorkspaceDirectory`, directory scaffolding expectations, and new user path helpers.
- Workspace tool rename behavior:
  - `internal/tools/workspace/workspace_test.go`
  - rename assertions from `workspace` -> `agent_workspace`; add `user_workspace` coverage.
- Project workspace path behavior:
  - `internal/projects/projects_test.go`
  - assert `WorkspaceDirectory` now includes `/workspace`.
- Gateway default conversation behavior (now per-user):
  - `internal/gw/gateway_conversation_test.go`
  - add tests for isolated defaults across two users on same agent.
- Auth middleware behavior:
  - `internal/gw/auth_middleware_test.go`
  - token resolution by user; session cookie with user-bound session.
- Channel bot tests:
  - `internal/channels/telegram/bot_test.go`
  - `internal/channels/discord/bot_test.go`
  - add `not linked` cases (must not invoke gateway run).

### New tests to add

- `internal/configs/security_test.go`:
  - v1->v2 security migration, username uniqueness, plaintext token persistence/match.
- `internal/configs/profile_test.go`:
  - `user.yaml` path behavior, legacy markdown fallback, and no-bio persistence.
- `internal/agents/registry_userstate_test.go` (or equivalent):
  - v1->v2 state migration and per-user default agent/conversation semantics.
- `internal/sessions/sessions_test.go`:
  - persist/load `Session.UserID` and filtering correctness.
- `internal/migrations/multiuser_test.go` (new package or under `cmd`):
  - idempotent startup migration across repeated runs.
  - legacy path matrix (`workspace/`, `workspaces/`, `conversations/`, `profile.md`, `users/<userId>.md`, `projects/<projectId>.yaml`).
- `internal/api/v1api/auth_multiuser_test.go`:
  - setup/login username+password flow, session issuance with userId, token resolution, and no implicit default-user auth fallback.
- `internal/api/v1api/rpc_multiuser_test.go`:
  - conversation list/history/delete restricted to current user scope.

### Integration scenarios

- Two users, same agent:
  - independent conversations/defaults/memory.
- Shared agents/projects:
  - both users can use same `agentId` and same `project_workspace`.
- Channel mapping:
  - mapped user succeeds, unmapped identity gets `not linked` and no run.

## 9) Rollout plan minimizing disruption

1. **Phase 0 (compatibility loaders):**
   - Add v2 schema structs + v1 reader compatibility (no behavior switch yet).
2. **Phase 1 (startup migration + path helpers):**
   - Introduce migration runner and new directory helpers while keeping old readers as fallback.
3. **Phase 2 (auth/user context cutover):**
   - Add `UserContext` resolution in middleware/websocket and user-bound sessions/tokens.
4. **Phase 3 (conversation and workspace scoping):**
   - Route conversation storage/defaults by user.
   - Enable `agent_workspace` + `user_workspace`; keep project sharing.
5. **Phase 4 (channels and prompts):**
   - Channel identity mapping in `security.yaml` and `not linked` enforcement.
   - Update system prompt/tool docs.
6. **Phase 5 (cleanup):**
   - remove deprecated state fields and legacy path fallback after verification window.

Operational safeguards:
- Ship with one-time backup + migration marker.
- Log migration summary at startup (counts of moved files, users, conversations).
- Keep legacy content recoverable in `.trash` during rollout window.
- Avoid API shape churn where unnecessary; prefer additive fields and backward-compatible loaders.
