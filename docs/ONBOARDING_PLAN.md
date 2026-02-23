# TeaNode WebUI Onboarding Plan

## 1. Goals

- Improve first-time onboarding experience for TeaNode WebUI users.
- Seed a default conversation server-side immediately after password/user setup.
- Seed onboarding artifacts in per-user workspace:
  - `~/.teanode/users/<userId>/workspace/USER.md`
  - `~/.teanode/users/<userId>/workspace/ONBOARDING.md`
- Enforce lifecycle rule:
  - Create `ONBOARDING.md` only when creating `USER.md` during first user initialization.
  - Never create `ONBOARDING.md` by itself.
  - Missing `ONBOARDING.md` means onboarding is done.
- Keep behavior idempotent and safe under retries, reconnects, and concurrent requests.

## 2. Non-goals

- Replacing existing `MEMORY.md` behavior.
- Moving onboarding state exclusively into frontend local state.
- Re-running onboarding for existing users by default.

## 3. Current Baseline (Relevant to This Plan)

- User workspace seeding currently happens via `configs.EnsureUserDirectories()` -> `configs.SeedUserWorkspace()` and creates `USER.md` + `MEMORY.md`.
- `users.create` calls `EnsureUserDirectories`; `/api/v1/auth/setup` currently does not.
- Conversation creation is available server-side via gateway methods (`NewConversation`, `SetDefaultConversationIfUnset`, `ConversationStore.Append`).
- System prompt includes `USER.md` and `MEMORY.md` but not `ONBOARDING.md`.

## 4. Proposed UX Flow

### 4.1 First-run (password setup path)

1. User submits `/api/v1/auth/setup`.
2. Server creates user, initializes user directories/workspace, seeds onboarding state + files, and seeds an onboarding conversation.
3. WebUI redirects to `/` as today.
4. On first `connect`, backend returns onboarding metadata (`status`, `conversationId`).
5. UI auto-opens onboarding conversation and highlights a lightweight “Let’s personalize your assistant” panel.
6. Conversation already contains an assistant greeting + preference questions (seeded by server, not frontend).
7. User answers in chat.
8. Assistant updates `USER.md` from answers and completes onboarding by deleting `ONBOARDING.md` (through tool access).
9. Backend marks onboarding complete and UI returns to normal chat state.

### 4.2 Admin-created user path (`users.create`)

- Same backend initialization and seeding.
- First login for that user lands in seeded onboarding conversation automatically.

### 4.3 Reconnect and multi-tab behavior

- If onboarding is `pending`/`in_progress`, all tabs converge to same seeded onboarding conversation ID.
- No duplicate greeting messages.
- No duplicate onboarding conversation files.

## 5. Backend Design

## 5.1 New onboarding service

Add `internal/onboarding/` (or equivalent) with an orchestration API:

- `InitializeUserOnboarding(ctx, userId, trigger)`
- `GetState(userId)`
- `MarkInProgress(userId)`
- `MarkCompleted(userId, reason)`
- `EnsureSeedConversation(userId)`

This service is the only place allowed to create onboarding files/conversation.

## 5.2 Seeding triggers

Call onboarding initialization from:

- `/api/v1/auth/setup` (after saving user + profile, before returning success)
- `users.create` (after saving user + profile)

Also add a safe fallback at authenticated `connect` time:

- If user directories are missing, run `EnsureUserDirectories` + onboarding initializer.
- Fallback is idempotent and handles historical users created before this feature.

## 5.3 Server-seeded default conversation

When onboarding is initialized:

- Resolve default agent ID.
- Create one conversation if none is recorded in onboarding state.
- Set as default conversation for that user+agent if unset.
- Append seeded assistant message(s) directly to conversation store:
  - Greeting.
  - Preference question set (name, tone, verbosity, goals, constraints, privacy expectations).

No frontend message injection.

## 5.4 Idempotency rules

Initialization must be safe under repeated calls:

- Use per-user lock (`sync.Map` keyed by `userId`) around initialization.
- Persist onboarding state atomically before/after side effects.
- If state already contains `seedConversationId`, do not create another.
- If seeded message marker already exists in that conversation header/message metadata, skip reseeding.

Recommended marker:

- Conversation header extension or first assistant message metadata field:
  - `{"teanode":{"onboardingSeedVersion":1}}`

## 5.5 API surface changes

### HTTP

- Extend `/api/v1/auth/setup` response payload with optional onboarding summary:
  - `onboardingStatus`
  - `onboardingConversationId`

### WebSocket RPC

- Extend `connect` response payload:
  - `onboarding`: `{ status, conversationId, startedAt, completedAt, version }`
- Optional explicit RPC for UI polling/debug:
  - `onboarding.status`

No frontend-authoritative onboarding mutation endpoint is required for v1.

## 6. Data Model and File Formats

## 6.1 `USER.md` (per-user workspace)

Keep markdown-first; add structured block for reliable parsing.

Suggested format:

```markdown
# User Profile

## Identity
- Preferred name:
- Pronouns:

## Communication Preferences
- Tone:
- Verbosity:
- Formatting:

## Goals
- Short-term:
- Long-term:

## Constraints
- Hard boundaries:
- Things to avoid:

## Notes
- 
```

## 6.2 `ONBOARDING.md` (created only with USER.md on first init)

Suggested format:

```markdown
# Onboarding Checklist

version: 1
status: pending

## Required Questions
- [ ] Preferred name and pronouns
- [ ] Preferred response style (tone/verbosity)
- [ ] Primary goals
- [ ] Constraints/boundaries

## Completion Rule
When all required fields are captured in USER.md, remove this file.
```

Rules:

- Only onboarding initializer may create this file.
- Never create `ONBOARDING.md` if `USER.md` already exists.

## 7. Onboarding State Detection Strategy (file-driven)

TeaNode should not persist onboarding status in `user.yaml`. Onboarding state is derived from the per-user workspace.

State resolution:

1. If `~/.teanode/users/<userId>/workspace/ONBOARDING.md` exists: onboarding is active.
2. If `ONBOARDING.md` is missing: onboarding is complete.

Seeding rule (server-side):

- Only seed an onboarding conversation when onboarding is active (i.e., `ONBOARDING.md` exists).
- Ensure seeding is idempotent by using a stable marker in the conversation history.

Recommended idempotency marker:

- Add metadata to the first assistant message:
  - `{"teanode":{"onboardingSeedVersion":1}}`

The initializer should:

- Find an existing conversation that already has the marker (if any) and reuse it.
- Otherwise create exactly one new onboarding conversation and append the seeded greeting/questions.


## 8. System Prompt / Runner Integration

- Load `ONBOARDING.md` content into system prompt when onboarding is active (file exists).
- Add a new section in `systemprompt.txt`:
  - `## Onboarding State`
  - Includes status and checklist guidance.
- Behavioral guidance:
  - If onboarding active, prioritize collecting missing preferences.
  - Write finalized preferences into `USER.md`.
  - Remove `ONBOARDING.md` only after capturing required fields.

Implementation points:

- `internal/agents/systemprompt.go`: load `ONBOARDING.md` conditionally.
- `internal/agents/systemprompt.txt`: add onboarding instructions section.

## 9. Migration and Compatibility

## 9.1 Existing users

- Do not create `ONBOARDING.md` for existing users.
- Preserve existing `USER.md` content untouched.

## 9.2 First-user setup compatibility

- Update `/auth/setup` flow to run `EnsureUserDirectories` and onboarding initialization.
- This closes current inconsistency where setup may not initialize user workspace like `users.create` does.

## 9.3 Backward compatibility with missing new fields

- No `user.yaml` onboarding block exists.
- Pre-feature users simply do not have `ONBOARDING.md`, therefore onboarding is treated as complete.

## 10. Security Considerations

- File-path safety: all onboarding reads/writes must resolve through existing `configs.User*Directory` helpers; no raw user-controlled paths.
- Permissions: user profile remains `0600`; consider writing onboarding artifacts with conservative modes and existing atomic writer.
- Injection resistance: seeded greeting/questions are static templates, not user-input-derived.
- Race safety: lock onboarding init per user to avoid duplicate conversations/messages.
- Privacy boundaries: onboarding data is stored only in per-user workspace (`user_workspace`), never in shared agent workspace.
- Abuse prevention: keep auth setup/login rate limits unchanged; onboarding initialization must not bypass auth/admin checks.

## 11. Testing Strategy

## 11.1 Unit tests

- `configs` tests:
  - first init creates `USER.md` + `ONBOARDING.md` together.
  - never creates `ONBOARDING.md` alone if `USER.md` already exists.
- onboarding service tests:
  - idempotent repeated initialization.
  - missing `ONBOARDING.md` + non-completed state transitions to completed.
  - no duplicate seeded messages.

## 11.2 API tests

- `/auth/setup` integration:
  - creates user directories.
  - seeds onboarding conversation and returns onboarding metadata.
- `users.create` integration:
  - same assertions.
- `connect` payload includes onboarding state and conversation ID.

## 11.3 Conversation/store tests

- Verify seeded assistant greeting/questions are persisted server-side in JSONL.
- Verify reconnect does not reseed.

## 11.4 Prompt/runner tests

- system prompt includes onboarding section when active.
- system prompt omits onboarding section when completed.

## 11.5 WebUI E2E tests

- New install flow redirects `/setup` -> `/` and opens onboarding conversation with pre-seeded assistant messages.
- Completing onboarding (assistant updates `USER.md` and removes `ONBOARDING.md`) clears onboarding banner and persists completed status.

## 12. Rollout Plan

1. Add onboarding state model + migration defaults.
2. Implement initializer and hook into `/auth/setup` + `users.create`.
3. Add server conversation seeding + idempotency markers.
4. Extend `connect` payload and minimal UI onboarding indicators.
5. Integrate prompt/runner onboarding context.
6. Add full test coverage (unit + integration + E2E).
7. Ship behind a config flag if desired (`gateway.onboarding.enabled`, default true for new installs).

## 13. Open Decisions

- Whether completion should be automatic on `ONBOARDING.md` deletion only, or require explicit signal from assistant/tool in addition to deletion.
- Exact seed message wording and number of preference questions.
- Whether to expose admin reset capability (`onboarding.reset`) for a user.
