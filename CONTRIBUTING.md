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
  api/v1api/            HTTP/WebSocket API and OpenAI-compatible endpoint
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

## CI

GitHub Actions runs on all pushes and pull requests:

- **Backend**: format verification, lint (`go vet`), build, tests
- **Frontend**: build, lint (ESLint + TypeScript), format verification (Prettier), tests (Vitest)
