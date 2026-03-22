# Getting Started with TeaNode

This guide walks through running TeaNode locally, configuring it, and understanding the minimum you need to hack on it.

It complements `README.md` (quick commands) and `docs/architecture.md` (high-level design).

---

## 1. Install prerequisites

- **Go**: 1.24 or newer
- **LLM provider key**: e.g. OpenAI API key (`OPENAI_API_KEY`)

Optional but useful:

- `make`
- Docker and docker-compose (for containerized runs)

---

## 2. Build and run the node

From the repo root:

### Quick run (no binary)

```sh
export OPENAI_API_KEY=sk-...
go run . node
```

### Build a binary

```sh
go build -o teanode .
```

Then run:

```sh
./teanode node
# or specify a port
./teanode node --port 8080
```

By default TeaNode listens on:

- `http://localhost:8833`

You can also use the provided Docker setup:

```sh
docker-compose up --build
```

(See `Dockerfile` and `docker-compose.yml` for details.)

---

## 3. Basic configuration

TeaNode reads configuration from `~/.teanode/config.yaml`, with environment variables taking precedence.

Common env vars:

| Variable | Description |
| --- | --- |
| `OPENAI_API_KEY` | API key for the LLM provider |
| `TEANODE_NODE_PORT` | Node listen port |
| `TEANODE_NODE_TOKEN` | Bearer token for authentication |

Example `~/.teanode/config.yaml`:

```yaml
node:
  port: 8833
  bind: loopback
models:
  default: gpt-5.1
  provider: openai
  baseUrl: https://api.openai.com/v1
```

On startup, TeaNode also loads:

- Security settings (`security.yaml`)
- Model/provider definitions (`models.yaml`)
- Skills directory configuration
- Channel/node settings

These are documented at a high level in `docs/architecture.md`.

---

## 4. First run and workspace layout

On first run, TeaNode creates its data directory under `~/.teanode/`. Workspace files are scoped per agent, user, and project:

- `agents/<agentId>/workspace/AGENT.md` – agent operating instructions.
- `agents/<agentId>/workspace/MEMORY.md` – long-term, curated memory used by the agent.
- `agents/<agentId>/workspace/SKILLS.md` – human-readable summary of skills/workflows.
- `users/<userId>/workspace/USER.md` – user-specific instructions.
- `users/<userId>/workspace/MEMORY.md` – user-specific memory.

The agent reads workspace files into its system prompt and can update them using built-in workspace tools. In addition, structured memory items (agent-scoped, user-scoped, or project-scoped) are stored as msgpack and support semantic search when an embedding provider is configured. Editing `AGENT.md` is the simplest way to customize behavior without touching Go code.

---

## 5. Talking to TeaNode

TeaNode exposes:

- An **OpenAI-compatible HTTP API** at `/api/chat/completions`.
- A simple **health check** at `/api/health`.

See `docs/api.md` for details, but a minimal `curl` example looks like:

```sh
curl -X POST \
  -H "Authorization: Bearer $TEANODE_NODE_TOKEN" \
  -H "Content-Type: application/json" \
  http://localhost:8833/api/chat/completions \
  -d '{
    "model": "gpt-5.1",
    "messages": [
      {"role": "user", "content": "Hello from TeaNode"}
    ]
  }'
```

Depending on your configuration, TeaNode may also expose a web UI bundled from the `web/` directory and additional channels (Telegram, Discord). Those are wired via `internal/channels` and `internal/frontend`.

---

## 6. Hacking on TeaNode

If you want to modify or extend TeaNode itself, useful docs are:

- `docs/architecture.md` – top-level layout and request flow.
- `docs/agents-and-skills.md` – how agents run and how markdown frontmatter-defined skills work.
- `docs/conversations.md` – how conversations are stored.
- `docs/jobs.md` – background jobs and reminders.
- `docs/autoacme-alpn.md` – automatic TLS certificate management (design doc).

Typical developer workflow:

```sh
# run tests
go test ./...

# run with race detector
go test -race ./...

# format and vet
gofmt -w .
go vet ./...
```

Naming/style conventions are documented in `README.md` (see the **Development** section there).

---

## 7. Extending TeaNode

A few common ways to extend the system:

### Add a skill (no Go code required)

1. Look at `skills.examples/` for sample markdown skill files.
2. Create your own `.md` in the configured skills directory (often `~/.teanode/skills/`).
3. Define one or more `shell` or `http` tools plus an optional `prompt`.
4. Restart TeaNode or trigger a reload so the new skill is picked up.

See `docs/agents-and-skills.md` for the schema and lifecycle.

### Add or customize tools

1. Find the relevant package in `internal/tools/` (e.g. `shell`, `filesystem`, `gitlab`, `google`).
2. Implement or adjust a tool handler (request/response shape, external call).
3. Register it in `internal/tools/tools.go` so agents can see it.

### Adjust agent behavior

1. Edit `~/.teanode/agents/<agentId>/workspace/AGENT.md` to tweak agent guidelines.
2. Use `MEMORY.md` and daily `memory/YYYY-MM-DD.md` logs for stable preferences.
3. Configure additional agents via the agent config YAML files under `~/.teanode/agents/<agentId>/config.yaml`.

---

## 8. Next steps

- Read `docs/architecture.md` to get a mental model of how the node, agents, tools, and jobs fit together.
- Skim `docs/agents-and-skills.md` before adding new skills or tools.
- Use `TODO.md` as a roadmap for unimplemented features and testing gaps.

Once you can build, run, and send a basic request, you are ready to:

- Add skills for your own workflows.
- Integrate external systems via new tools.
- Contribute improvements back to TeaNode.
