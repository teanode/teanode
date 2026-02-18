# TeaNode

Personal AI assistant gateway. Exposes an OpenAI-compatible API that proxies to configurable LLM providers, with persistent memory and a self-improving agent workspace.

## Prerequisites

- Go 1.24+
- An OpenAI API key (or compatible provider)

## Quick start

```sh
export OPENAI_API_KEY=sk-...
go run . gateway
```

The gateway listens on `http://localhost:8833` by default.

## Development

### Naming Convention

When first alphabetical character is capitalized, also capitalize acronyms:

- `ReferenceURI`
- `URL`
- `ID`
- `SessionID`
- `GetFTPID`
- `_CreateSessionID`

When first alphabetical character is not capitalized, capitalize **first** letter of an acronym:

- `referenceUri`
- `url`
- `id`
- `sessionId`
- `getFtpId`
- `__deleted__`
- `__somethingElse__`

Do not abbreviate, spell things out clearly. For example:

- prefer "command" over "cmd"
- prefer "response" over "resp"
- prefer "request" over "req"

Package names being the exception, they should be brief.

Avoid single letter variables.

Name things consistently everywhere, do not give different name to the same thing.

When writing member function of a struct in Golang, use `self` to refer to the instance.

### Build

```sh
go build -o teanode .
```

### Run

```sh
./teanode gateway
./teanode gateway --port 8080
```

### Test

```sh
go test ./...
```

With verbose output:

```sh
go test -v ./...
```

With race detector:

```sh
go test -race ./...
```

### Format

```sh
gofmt -w .
```

### Lint

```sh
go vet ./...
```

### Vendor

After changing dependencies:

```sh
go mod tidy
go mod vendor
```

## Configuration

TeaNode reads config from `~/.teanode/config.json`. Environment variables take precedence:

| Variable | Description |
|---|---|
| `OPENAI_API_KEY` | API key for the LLM provider |
| `TEANODE_GATEWAY_PORT` | Gateway listen port |
| `TEANODE_GATEWAY_TOKEN` | Bearer token for authentication |

Example `config.json`:

```json
{
  "gateway": {
    "port": 8833,
    "bind": "loopback"
  },
  "models": {
    "default": "gpt-5.1",
    "provider": "openai",
    "baseUrl": "https://api.openai.com/v1"
  }
}
```

## Workspace

On first run, TeaNode creates `~/.teanode/workspace/` with:

- `AGENTS.md` -- agent operating instructions (editable by the agent or user)
- `MEMORY.md` -- long-term curated memories
- `memory/` -- daily logs (`YYYY-MM-DD.md`)

These files are injected into the system prompt each session. The agent can read, write, append, and search workspace files using its built-in memory tools.
