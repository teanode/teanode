# Contributing to TeaNode

## Prerequisites

- Go 1.25+
- Node.js 20+
- `make` (optional but recommended)

## Development Setup

```sh
# install frontend dependencies
cd web && npm install && cd ..

# run in development mode
export OPENAI_API_KEY=sk-...
go run . node

# frontend dev server (with hot reload)
cd web && npm run dev
```

## Build

```sh
make all        # frontend + backend
make teanode    # backend only (static binary)
make web        # frontend only
make clean      # remove build artifacts
```

## Testing

### Backend

```sh
go test ./...           # run all tests
go test -v ./...        # verbose output
go test -race ./...     # with race detector
make test               # run tests with coverage summary
make coverage           # generate HTML coverage report
```

### Frontend

```sh
cd web
npm test                # run tests (Vitest)
npx tsc --noEmit        # type check
```

## Formatting and Linting

Run formatters and linters for both backend and frontend before committing.

### Backend

```sh
gofmt -w .              # format
go vet ./...            # lint
```

Or with make:

```sh
make format
make lint
```

### Frontend

```sh
cd web
npm run format          # format (Prettier)
npm run lint            # lint (ESLint + TypeScript check)
```

## Vendoring

After changing Go dependencies:

```sh
go mod tidy
go mod vendor
```

## Naming Conventions

### Acronyms

When the first alphabetical character is capitalized, also capitalize acronyms:

- `ReferenceURI`
- `URL`
- `ID`
- `SessionID`
- `GetFTPID`
- `_CreateSessionID`

When the first alphabetical character is not capitalized, capitalize only the first letter of an acronym:

- `referenceUri`
- `url`
- `id`
- `sessionId`
- `getFtpId`

### General Rules

- Do not abbreviate — prefer `command` over `cmd`, `response` over `resp`, `request` over `req`
- Package names are the exception: they should be brief
- Errors should be named `err` whenever possible
- Avoid single-letter variables
- Name things consistently — do not give different names to the same thing
- Use `self` for struct receiver names in Go

## Project Structure

```
cmd/                    CLI commands (node, start, stop, status, terminal, tools)
internal/
  api/                  HTTP/WebSocket API and OpenAI-compatible endpoint
  providers/            LLM provider abstraction (OpenAI, Anthropic, Gemini, etc.)
  runners/              Conversation execution engine
  tools/                Built-in tools (30+ categories)
  coordinators/         Message routing and broadcasting
  store/                Storage (fsstore for filesystem, dbstore for PostgreSQL)
  models/               Data types (Agent, Conversation, User, Job, Session)
  jobs/                 Cron scheduler and background automations
  skills/               Markdown-based skill loader
  channels/             Discord and Telegram integrations
  voice/                Voice session runtime
  integrations/         Browser relay, terminal relay
  summarizers/          Conversation title/description generation
  frontend/             Embedded SPA serving
  embeddings/           Semantic search via embedding providers
  autoacme/             Automatic TLS certificate management
  util/                 Utilities (security, crypto, time, data structures)
web/                    React/TypeScript frontend
  src/components/       Reusable UI components
  src/hooks/            Custom hooks (useBackend, useAudioRecorder, useTTS)
  src/routes/           Page components and routing
  src/i18n/             Internationalization
docs/                   Documentation
skills.examples/        Example skill definitions
```

## Extending TeaNode

### Add a Skill (No Go Code)

1. Look at `skills.examples/` for samples
2. Create a `.md` file in `~/.teanode/skills/`
3. Define `shell`, `http`, or `workflow` tools with an optional prompt
4. TeaNode hot-reloads skills automatically

See `docs/agents-and-skills.md` for the schema.

### Add or Customize Tools

1. Find the relevant package in `internal/tools/`
2. Implement or adjust a tool handler
3. Register it in `internal/tools/tools.go`

### Adjust Agent Behavior

1. Edit `~/.teanode/agents/<agentId>/workspace/AGENT.md`
2. Use `MEMORY.md` for stable preferences
3. Configure agents via `~/.teanode/agents/<agentId>/config.yaml`

## Release Workflow

Releases are tagged automatically when pull requests merge to `main`. You only
need to keep `CHANGELOG.md` accurate; the workflows do the rest.

### What Each PR Must Do

- Add at least one bullet under the `## [Unreleased]` section of `CHANGELOG.md`,
  filed under the appropriate Keep-a-Changelog subsection:
  - `### Added` — new behavior
  - `### Changed` — observable behavior change
  - `### Removed` — removed behavior
  - `### Deprecated` — deprecation
  - `### Fixed` — bug fix
  - `### Security` — security fix
- The `Changelog Guard` workflow enforces this on every PR.
- For PRs that genuinely have no user-visible impact (CI-only edits, doc typos,
  pure refactors), apply the `skip-changelog` label to bypass the guard.

### How Auto-Release Decides the Bump

When the PR merges to `main`, `Auto Release` parses `## [Unreleased]` and:

- Bumps **minor** if any bullet exists under `Added`, `Changed`, `Removed`, or
  `Deprecated`.
- Bumps **patch** if only `Fixed` and/or `Security` bullets exist.
- Does nothing if `## [Unreleased]` is empty.

It then rewrites `CHANGELOG.md` (replacing `## [Unreleased]` with a new dated
`## [X.Y.Z]` section above an empty Unreleased), commits as
`chore(release): vX.Y.Z`, and pushes the `vX.Y.Z` tag. The existing
`Release` workflow takes over from the tag push to build artifacts and publish
the GitHub Release.

### Major Releases (Manual Only)

Major versions never bump automatically. To cut a `vX.0.0` release:

1. Make sure `## [Unreleased]` contains the entries you want included.
2. Go to **Actions → Major Release → Run workflow**.
3. Type `MAJOR` in the confirmation input.

The workflow follows the same commit-and-tag flow as auto-release.

### Required Setup

- Repository secret `RELEASE_TOKEN` — a fine-grained PAT with
  `Contents: read & write` on this repo. The release workflows use it both to
  push past the `main` branch restriction and to make the tag push trigger the
  `Release` workflow (the default `GITHUB_TOKEN` cannot do either).

### Tag Format

- Release tags are `vMAJOR.MINOR.PATCH` (for example `v0.1.14`).
- The publishing workflow also accepts bare numeric tags for compatibility,
  but new releases always use the `v`-prefixed form.

### How Release Notes Are Generated

- The `Release` workflow extracts the matching `## [X.Y.Z]` section from
  `CHANGELOG.md` and uses it as the GitHub Release body.
- If no matching section is found, the workflow falls back to a minimal
  `Release X.Y.Z` body — but this should never happen in normal flow because
  the auto-release commit guarantees the section exists.

## CI

GitHub Actions runs on all pushes and pull requests:

- **Backend**: format verification, lint (`go vet`), build, tests
- **Frontend**: build, lint (ESLint + TypeScript), format verification (Prettier), tests (Vitest)
