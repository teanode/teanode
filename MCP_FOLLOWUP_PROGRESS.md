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
| 2 | feat/mcp-oauth | feat/mcp-connections | — | — | in progress |
| 3 | feat/mcp-user-runner | feat/mcp-oauth | — | — | pending |
| 4 | feat/mcp-frontend | feat/mcp-user-runner | — | — | pending |
| 5 | feat/mcp-hardening | feat/mcp-frontend | — | — | pending |
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

## Validation log

(per-PR formatter / linter / test results recorded above per PR)

## Blockers / gaps

- dbstore (Postgres) MCP connection operations compile and follow the Token
  pattern but are not runtime-validated here (Postgres-gated tests are skipped in
  this environment). Migration `0006` mirrors existing migrations.
</content>
</invoke>
