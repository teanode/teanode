# MCP Follow-up PR Chain — Progress Log

Status: **IN PROGRESS** — started 2026-06-08.

This file tracks the staged PR chain implementing the follow-up MCP plan on top of
the initial MCP client work (branch `feat/mcp-client`, PR #56).

## Goal / Staged PRs

1. Per-user MCP connection model + auth scaffolding + config/schema/store/API foundations.
2. OAuth / PKCE protected remote MCP auth flow for remote MCP servers.
3. User-aware runner registration and MCP tool availability based on authenticated user context.
4. Frontend completeness: admin management UI + user connect/disconnect/status UX.
5. Hardening: logging/diagnostics, approval UX for high-risk remote tools, retry/timeout/error handling, validation.
6. (If feasible) prompts/resources support; otherwise documented follow-up notes.

## Branch / PR chain

| # | Branch | Base | Commit(s) | PR | Status |
|---|--------|------|-----------|----|--------|
| base | feat/mcp-client | main | 64349e9 | #56 | exists |
| 1 | feat/mcp-connections | feat/mcp-client | 30f421b | [#57](https://github.com/teanode/teanode/pull/57) | PR open |
| 2 | feat/mcp-oauth | feat/mcp-connections | f8c8fff | [#58](https://github.com/teanode/teanode/pull/58) | PR open |
| 3 | feat/mcp-user-runner | feat/mcp-oauth | 107a755 | [#59](https://github.com/teanode/teanode/pull/59) | PR open |
| 4 | feat/mcp-frontend | feat/mcp-user-runner | 0338284 | [#60](https://github.com/teanode/teanode/pull/60) | PR open |
| 5 | feat/mcp-hardening | feat/mcp-frontend | c2494b0 | [#61](https://github.com/teanode/teanode/pull/61) | PR open |
| 6 | feat/mcp-prompts-resources | feat/mcp-hardening | — | — | optional |

## Notes / decisions

- Existing MCP client (`internal/mcp/`): streamable HTTP transport, tools capability
  only, static Authorization header, node-level config under `Tools.MCP.Servers`.
- Adapter treats every remote tool as `ToolPolicyAdminApproval` by default.

## PR1 — Per-user MCP connection foundations (feat/mcp-connections)

Contents:
- New `models.MCPConnection` entity (per-user credential binding to a server) +
  `MCPConnectionStatus` enum. Registered in `models_generate.go`; `models_gen.go`
  regenerated.
- `MCPServerConfiguration.Auth` (`MCPServerAuthMode`: none/static/user) with
  `ResolvedAuthMode()` inference (static if Authorization set, else none).
- Store: `MCPConnectionOperation` interface (list/create/get/getByServer/modify/
  delete) implemented in both `fsstore` (YAML under
  `users/<id>/mcp_connections/`) and `dbstore` (table `mcp_connections`,
  migration `0006_mcp_connections`). Server `Auth` field persisted in fsstore
  config record; dbstore config is a JSON blob so needs no change.
- API RPC (user-scoped): `mcp.servers.list`, `mcp.connections.list`,
  `mcp.connections.create`, `mcp.connections.delete`. Secrets (static
  Authorization, per-user credential) are NEVER returned to clients — dedicated
  secret-free DTOs.
- Config schema: `auth` enum field added for MCP servers.
- mulint.yaml: registered `MCP` acronym (was missing since the base MCP PR);
  renamed pre-existing/new unexported `MCP*` identifiers to `Mcp*` per the
  acronym casing rule.

Tests:
- `internal/models`: `ResolvedAuthMode` table test.
- `internal/store/fsstore`: MCP connection CRUD lifecycle + server Auth round-trip.
- `internal/api`: full RPC lifecycle via a real fsstore + user context, duplicate
  rejection, non-user/unknown server rejection, and a secret-omission assertion.

Validation:
- `go build ./...` clean; `go test ./...` all pass (dbstore entity tests skip
  without `TEANODE_TEST_POSTGRES=1`).
- `gofmt` clean; `golangci-lint run ./internal/...` 0 issues; `mulint` clean on
  changed packages.

## PR2 — OAuth / PKCE remote MCP auth flow (feat/mcp-oauth)

Adds the OAuth 2.1 authorization-code + PKCE flow used to authenticate against
protected remote MCP servers, on top of the per-user connection foundations from
PR1.

Contents:
- New `internal/mcp/oauth` package: a focused OAuth client implementing
  - PKCE (S256) verifier/challenge generation and opaque CSRF state generation;
  - endpoint resolution — explicit authorization/token endpoints take precedence,
    otherwise discovery via RFC 9728 protected-resource metadata → RFC 8414
    authorization-server metadata (OpenID configuration fallback);
  - authorization-URL construction (with RFC 8707 `resource` indicator and
    scopes), authorization-code exchange, and refresh-token exchange;
  - public clients (PKCE only) and confidential clients (`client_secret_post`).
- Config (`MCPServerConfiguration`): new `oauth` auth mode plus
  `OAuthClientID`/`OAuthClientSecret`/`OAuthScopes`/`OAuthAuthorizationURL`/
  `OAuthTokenURL`. Mirrored in the fsstore server record and `config.schema.json`
  (new `oauth` enum value + field definitions). `ResolvedAuthMode` already returns
  the explicit mode, so `oauth` flows through unchanged.
- Model (`MCPConnection`): token material (`AccessToken`, `RefreshToken`,
  `TokenType`, `TokenExpiresAt`, `Scope`) and transient flow state (`OAuthState`,
  `CodeVerifier`). All are secrets and are excluded from the secret-free DTOs.
- Store: both fsstore and dbstore persist the new fields; dbstore migration
  `0007_mcp_oauth` adds the columns plus an index on `oauth_state`.
- API:
  - RPC `mcp.connections.authorize` (`handleMcpConnectionsAuthorize`) resolves the
    server's OAuth config, generates PKCE+state, upserts a pending connection
    holding the transient secrets, and returns the provider authorization URL.
    Discovery happens outside the store transaction.
  - HTTP `GET /api/mcp/oauth/callback` (`handleMcpOAuthCallback` /
    `completeMcpOAuth`) matches the CSRF state to the authenticated user's pending
    connection, exchanges the code for tokens, stores them, clears the one-time
    PKCE/state, and redirects the browser back to the connections page with the
    outcome. Failures are recorded on the connection's `LastError`.
  - `handleMcpServersList` now reports `requiresConnection` for `oauth` servers
    too (previously only `user`).
- mulint.yaml: registered the `PKCE` acronym.

Tests:
- `internal/mcp/oauth`: PKCE generation, authorization-URL parameters, metadata
  discovery, explicit-endpoint bypass, code exchange + refresh, and error-response
  rejection (against an httptest stub authorization server).
- `internal/api`: authorize RPC happy path (authorization URL + persisted pending
  connection + secret omission), rejection for non-oauth servers, full callback
  exchange via `completeMcpOAuth` (tokens stored, transient state cleared), and
  unknown-state rejection.

Validation:
- `go build ./...` clean; `go test ./...` all pass (Postgres-gated dbstore tests
  skip without `TEANODE_TEST_POSTGRES=1`).
- `gofmt -l internal/` clean; `golangci-lint run ./internal/...` 0 issues;
  `mulint` clean on changed packages.

Intentionally deferred:
- Consuming the stored OAuth access token when registering the MCP client for a
  user (and refresh-on-expiry using `oauth.Client.Refresh`) lands in PR3
  (user-aware runner registration), where per-user tools are actually wired up.
- Frontend "Authorize"/status UX is PR4.

## PR3 — User-aware runner registration + token consumption (feat/mcp-user-runner)

Wires the per-user connection model and OAuth tokens from PR1/PR2 into the actual
run: MCP tool availability is now resolved against the authenticated user's
connections, and stored OAuth access tokens are consumed (with refresh-on-expiry)
for per-user tool discovery and invocation.

Contents:
- New `internal/mcp/servers.go` (extracted the server-resolution logic out of
  `manager.go`):
  - `RegisterConfiguredTools` is now user-aware. It loads the configuration and,
    when the node has a user-scoped server and the context carries a user, that
    user's MCP connections, then resolves the request's available servers.
  - `ServersFromConfiguration` now returns only the shared (`none`/`static`)
    servers — the node-level set available to everyone. `static` copies the
    node Authorization; `none` sends no header.
  - `resolveUserServers` resolves the per-user (`user`/`oauth`) servers: a server
    is registered only when the user has a non-disconnected connection with a
    usable credential. Without a connection (or without an authenticated user),
    the server is skipped — so unauthenticated/static servers keep working
    unchanged while user-scoped servers are gated per user.
  - `user` mode uses the connection's stored Authorization header verbatim.
  - `oauth` mode builds a `Bearer <accessToken>` header from the stored token.
    When the access token is within 60s of expiry (or already expired) and a
    refresh token is present, it refreshes via `oauth.Client.Refresh` outside the
    store transaction, persists the new token (`ApplyOAuthToken`), and uses it.
    A still-valid token whose refresh failed is used as-is for the run; a
    hard-expired token that cannot be refreshed causes the server to be skipped
    (and the connection marked `error`) rather than called with a dead token.
- Deduplication of OAuth helpers: `mcp.ServerOAuthConfig` and
  `mcp.ApplyOAuthToken` are now the single home for the config→OAuth mapping and
  token-application; `internal/api` (`serverOAuthConfig`, `applyOAuthToken`)
  delegates to them so the authorize/callback flow and the runner refresh path
  cannot drift.
- The discovery cache key already includes the resolved Authorization, so each
  user's (and each refreshed token's) discovery is isolated and a credential
  change invalidates the cache.

Tests:
- `internal/mcp` (`servers_test.go`): `ServersFromConfiguration` auth-mode
  partitioning; user-auth resolution (connected user gets their credential, users
  without a connection and unauthenticated requests are skipped while shared
  servers remain); disconnected connections excluded; OAuth valid-token bearer
  (no network); OAuth refresh-on-expiry against an httptest token endpoint with
  persistence assertion; OAuth expired-without-refresh and no-access-token skips;
  and an end-to-end `RegisterConfiguredTools` test where the user's stored
  credential must reach a `requireAuth` test MCP server for its tools to register.
- `internal/runners` (`runner_mcp_test.go`): a user-scoped server registers
  through `NewRunner` only for the connected user, exercising the
  authenticated-user path end to end.

Validation:
- `go build ./...` clean; `go test ./...` all pass (Postgres-gated dbstore tests
  skip without `TEANODE_TEST_POSTGRES=1`).
- `gofmt -l internal/` clean; `golangci-lint run` 0 issues on changed packages;
  `mulint` clean on changed packages.

Intentionally deferred:
- Discovery-time connection status updates (marking a connection connected/error
  based on whether discovery succeeded) are left to the hardening pass (PR5) to
  keep run startup free of extra writes; only OAuth refresh outcomes update the
  connection here.
- Frontend connect/disconnect/status UX is PR4.

## PR4 — Frontend completeness (feat/mcp-frontend)

Frontend management and connection UX for the MCP stack, plus the one schema
hint needed to make the admin server editor work. PR [#60](https://github.com/teanode/teanode/pull/60),
commit `0338284`.

User connect/disconnect/status UX:
- New `web/src/routes/settings/connections.tsx` — `/settings/connections`, a
  page visible to every authenticated user that lists the operator-configured
  MCP servers with the current user's per-server connection state (auth-mode
  chip, status chip, last-connected time, last error).
  - `user` auth servers: a dialog collects the `Authorization` header value and
    calls `mcp.connections.create`.
  - `oauth` auth servers: `mcp.connections.authorize` returns the provider
    authorization URL and the browser is navigated to it (full-page so the
    session cookie reaches the callback). Pending/errored connections show
    Reauthorize + Disconnect.
  - `none`/`static` servers render as shared with no action.
  - This page is also the **OAuth callback landing**: the backend redirects to
    `/settings/connections?server=…&mcpConnected=1` (or `&mcpError=…`); the page
    parses those markers, alerts the outcome, and strips them from the URL.
- `web/src/routes/settings/connections.helpers.ts` — pure, unit-tested helpers:
  the four RPC wrappers, the `serverAction` decision (shared/connect/authorize/
  reauthorize/connected), and `parseOAuthCallback`.

Admin MCP server management:
- `web/src/components/SchemaField.tsx` — new generic `objectArray` widget that
  renders an array-of-objects by delegating each item field back to
  `SchemaField`. The admin **Tools** config section can now add/edit/remove MCP
  servers (name, URL, auth mode, static/OAuth secrets as password fields,
  scopes, timeout); previously `mcp.servers` fell through to the scalar
  string-tag editor and could not be edited. `config.schema.json` tags
  `tools.mcp.servers` with `x-widget: "objectArray"`.

Plumbing:
- `web/src/types.ts` — MCP DTO types mirroring the backend secret-free views.
- `web/src/components/SettingsNav.tsx` — "Connections" nav entry (all users).
- `web/src/router.tsx` — `/settings/connections` route.
- `web/src/i18n/locales/en.json` — `mcp.*` strings (zh/ja fall back to en).

Backend: no RPC/endpoint changes were needed. `mcp.servers.list` already returns
per-server connection state, so it is the single source for the page (no
separate `connections.list` call). The only backend file touched is the embedded
`config.schema.json` (the `x-widget` hint).

Tests:
- `web/src/routes/settings/connections.helpers.test.ts` (14 cases): RPC helper
  call shapes, the full `serverAction` matrix, and `parseOAuthCallback`
  success/error/missing-marker cases.

Validation:
- Frontend: `tsc --noEmit` clean; `eslint` clean (one pre-existing
  exhaustive-deps warning in `SettingsNav` unrelated to this change);
  `prettier --check` clean; full `vitest` suite passes (304 tests, 18 files).
- Backend: `go build ./...` clean; `go test ./internal/api/...` passes;
  `gofmt -l internal/` clean; `config.schema.json` parses.

Intentionally deferred:
- Approval UX for high-risk remote tools, retry/timeout/error surfacing in the
  run UI, and discovery-time connection status refresh remain in PR5
  (hardening). The connections page reflects status as recorded by the OAuth
  flow / refresh path, not a live probe.
- zh/ja translations for the new `mcp.*` strings (English fallback applies).

## PR5 — Hardening (feat/mcp-hardening)

Hardening of the remote MCP integration on top of the frontend stack. PR
[#61](https://github.com/teanode/teanode/pull/61), commit `c2494b0`. No new
feature surface — the changes make remote-tool risk and connection health
visible and make discovery resilient.

Contents:
- Discovery resilience (`internal/mcp/manager.go`, `client.go`): tool discovery
  (`initialize` + `tools/list`, idempotent) is retried with bounded exponential
  backoff (3 attempts, 250ms base). Only transient failures retry — transport
  errors and HTTP `408/425/429/5xx`; auth rejections (`401/403`) and JSON-RPC
  application errors do not (the credential will not change between attempts).
  Tool *invocation* is never retried (not idempotent). A typed `httpStatusError`
  carries the status code so retry classification is precise, not string-based.
  Discovery logs distinguish `discovered` vs `cached`.
- Discovery-time connection status: `Manager.RegisterTools` returns a per-server
  `ServerDiscovery` outcome; `RegisterConfiguredTools` reflects **fresh**
  outcomes onto the backing per-user connection — reachable → `connected`
  (stamps `LastConnectedAt`, clears `LastError`); failed → `error` with reason.
  Cached results and shared (`none`/`static`) servers are skipped, so run
  startup performs no extra writes when nothing was probed. This closes the
  status-refresh item deferred from PR3/PR4: the connections page now reflects
  live discovery health.
- Validation tightening (`internal/mcp/servers.go`, `internal/api/rpc_mcp.go`):
  servers whose URL is not an absolute `http(s)` URL with a host are skipped and
  logged; a credentialed server on plaintext `http` to a non-loopback host is
  warned about (loopback http stays allowed for local dev); the per-user
  `Authorization` credential length is bounded in `mcp.connections.create`.
- Approval UX (`web/src/components/ApprovalPanel.tsx`, `mcpTool.ts`): the
  approval panel detects remote MCP tool calls (`mcp__server__tool`) and renders
  an **External** chip plus a warning that the arguments are sent to an external
  server, so high-risk remote calls are visibly distinct from local tools. New
  `mcp.remoteMcp*` i18n strings (zh/ja fall back to en).

Tests:
- `internal/mcp`: retry recovery after transient 503s; give-up after max
  attempts; no-retry on auth failure; cached-outcome reporting; an
  `isRetryableError` matrix; discovery-time `connected`/`error` status writes
  against a real fsstore; invalid-URL skipping.
- `internal/api`: oversized-authorization rejection.
- `web`: `parseMcpToolName` unit tests (4 cases).

Validation:
- `go build ./...` clean; `go test ./...` all pass (Postgres-gated dbstore tests
  skip without `TEANODE_TEST_POSTGRES=1`).
- `gofmt -l` clean; `golangci-lint run` 0 issues on changed packages; `mulint`
  clean on changed files.
- Frontend: `tsc --noEmit` clean; `eslint` clean; `prettier --check` clean;
  `vitest` 308 tests pass (19 files).

Intentionally deferred:
- Live (non-cached) connection probing on demand from the connections page: the
  page still reflects status as recorded by the OAuth flow, refresh path, and
  now discovery, but does not trigger an out-of-band probe on open.
- Reusing a connected MCP session across tool calls (each call still opens a
  fresh session — see the TODO in `adapter.go`).
- zh/ja translations for the new `tool.remoteMcp*` strings (English fallback).
- PR6 (prompts/resources support) not started.

## PR — Stdio (local subprocess) transport (feat/mcp-stdio)

Adds the MCP **stdio transport**: TeaNode can now launch a local subprocess that
speaks newline-delimited JSON-RPC over stdin/stdout (the transport used by
`claude mcp`-style stdio servers) alongside the existing remote streamable HTTP
transport. Configured under the same `tools.mcp.servers` list.

Contents:
- Transport abstraction (`internal/mcp/transport.go`): the session-level `Client`
  now drives the MCP handshake over a `transport` interface
  (`roundTrip`/`notify`/`observeProtocolVersion`/`close`). The existing HTTP code
  moved verbatim into `httpTransport` (`transport_http.go`); `client.go` is now
  transport-agnostic. `Client.Close()` releases transport resources and is
  deferred by the discovery probe and the tool adapter.
- Stdio transport (`internal/mcp/transport_stdio.go`): lazily starts the
  subprocess on first use, frames each JSON-RPC message as one line on stdin, and
  a reader goroutine forwards response-shaped messages (dropping server-initiated
  requests/notifications, matching the HTTP transport's v1 limitation). `close`
  shuts stdin, waits for the reader to drain stdout, and kills the process after a
  2s grace. Subprocess stderr is forwarded to the package logger line by line.
  Env is merged over TeaNode's own environment.
- Config (`models.MCPServerConfiguration`): new `transport` (`http`/`stdio`),
  `command`, `args`, `env`, `workingDir` fields plus `ResolvedTransport()` (infers
  `stdio` from a bare command with no URL). `models_gen.go` regenerated. Mirrored
  in the fsstore server record (round-trip) and `config.schema.json`. dbstore
  stores config as a JSON blob, so it needs no change.
- Resolution (`internal/mcp/servers.go`): stdio servers register as shared
  `none`-auth servers carrying the command/args/sorted-env/working-dir; HTTP
  servers keep their URL validation. The discovery cache signature includes the
  stdio launch fields.
- Frontend: `mcp.servers.list` now reports `transport` and (for stdio) the
  `command` display string; the connections page renders the command in monospace
  for stdio servers instead of a URL. Stdio servers show as shared (no per-user
  connection).

Tests:
- `internal/mcp`: stdio client list/call/empty-tools, missing-command and
  process-exit error paths, and context cancellation, against the test binary
  re-exec'd as a stdio server (hermetic). Stdio config resolution
  (command/args/sorted-env/working-dir, command-required) in `servers_test.go`.
- `internal/models`: `ResolvedTransport` table test.
- `internal/store/fsstore`: stdio fields added to the config round-trip test.
- `internal/runners`: end-to-end `NewRunner` launches a configured stdio server
  (the re-exec'd test binary) and registers its `mcp__local__echo` tool.

Validation:
- `go build ./...` clean; `go test ./...` all pass (Postgres-gated dbstore tests
  skip without `TEANODE_TEST_POSTGRES=1`); `-race` clean on the stdio tests.
- `gofmt -l internal/` clean; `golangci-lint run` 0 issues on changed packages;
  `mulint` clean on changed files.
- Frontend: `tsc --noEmit` clean; `eslint`/`prettier` clean; `vitest` 308 tests
  pass.

Intentionally deferred:
- Reusing a connected stdio session across tool calls (each call still spawns a
  fresh subprocess — same TODO as the HTTP adapter).
- Server-initiated requests (sampling/roots) over stdio remain out of scope.

## Validation log

(per-PR formatter / linter / test results recorded above per PR)

## Blockers / gaps

- dbstore (Postgres) MCP connection operations compile and follow the Token
  pattern but are not runtime-validated here (Postgres-gated tests are skipped in
  this environment). Migration `0006` mirrors existing migrations.
</content>
</invoke>
