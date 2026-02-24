# Store Refactor Plan (internal/configs -> interface-based transactional storage)

## Objectives

- Replace the current `internal/configs` file-centric API with a `store.Store` abstraction.
- Move store domain model definitions into `internal/models` and use `models.*` types across store interfaces.
- Provide two implementations:
  - Filesystem-backed implementation using the existing `~/.teanode` layout.
  - Postgres-backed implementation modeled after `/home/ziyan/projects/ziyan/teanode/backend/db` transaction patterns.
- Make storage access transactional and interface-driven so backend behavior can be switched at startup via CLI flags.
- Remove legacy `configs.Load*` / `configs.Save*` style call paths instead of preserving dual APIs.

## Explicit Non-goals

- No migration of existing user data/configs from filesystem to Postgres.
- No compatibility layer to preserve old function signatures or old import-level globals.
- No datastore ownership of PID file lifecycle. PID lock remains fully in `main.go`.
- No model catalog cache persistence. Model listing is uncached and fetched directly from providers.
- No filesystem watcher reload mechanism. Remove `internal/watcher` as part of this refactor.

## Target Architecture

### 1. Storage package structure

- Add new package: `internal/store`.
- Move persistence contracts out of `internal/configs`.
- Move persistence/domain models into `internal/models`.
- Move configuration overlay resolution into `internal/configurations` (plural) and keep it independent from store implementations.

Proposed layout:

- `internal/models/*.go`
- `internal/configurations/*.go`
- `internal/store/interfaces.go`
- `internal/store/types.go`
- `internal/store/errors.go`
- `internal/store/fs/*.go`
- `internal/store/db/*.go`
- `internal/store/db/migrations/*.sql|*.go`

### 1.1 Model inventory (`internal/models`)

All persisted/domain structs are defined in `internal/models`.
Model convention: every field except `ID` is pointer-typed to support sparse loads and partial updates.
Model convention: every persisted entity struct includes `CreatedAt` and `ModifiedAt`.

```go
package models

import "time"

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type Scope string

const (
	ScopeAgent   Scope = "agent"
	ScopeUser    Scope = "user"
	ScopeProject Scope = "project"
)

type StopReason string

const (
	StopReasonUnknown        StopReason = "unknown"
	StopReasonEndTurn        StopReason = "end_turn"
	StopReasonMaxTokens      StopReason = "max_tokens"
	StopReasonToolUse        StopReason = "tool_use"
	StopReasonContextSummary StopReason = "context_summary"
	StopReasonCancelled      StopReason = "cancelled"
	StopReasonError          StopReason = "error"
)

// singleton
// Raw persisted configuration payload. Overlay resolution is done outside store.
type Configuration struct {
	Gateway          *GatewayConfiguration
	Models           *ModelsConfiguration
	Tools            *ToolsConfiguration
	Integrations     *IntegrationsConfiguration
	Channels         *ChannelsConfiguration
	Secrets          *[]SecretConfiguration
	SkillsRegistries *[]SkillRegistryConfiguration
	CreatedAt        *time.Time
	ModifiedAt       *time.Time
}

type GatewayConfiguration struct {
	Port           *int
	Bind           *string
	Security       *GatewaySecurityConfiguration // the old auth config
	PublicURL      *string
}

type GatewaySecurityConfiguration struct {
	SessionMaxAgeDays *int
	ForwarderKey      *string // intentionally moved under gateway security
}

type ModelsConfiguration struct {
	Default         *string
	SummarizerModel *string
	ContextWindow   *int
	DefaultLimits   *map[string]interface{} // model runtime default limits (TODO: remove this feature, hardcode in golang code)
	Limits          *[]map[string]interface{} // per-model limit entries (TODO: remove this feature, hardcode in golang code)
	Providers       *[]ProviderConfiguration
}

type ProviderConfiguration struct {
	Name    *string
	BaseURL *string
	APIKey  *string
}

type ToolsConfiguration struct {
	BraveAPIKey   *string
	Google        *GoogleConfiguration
	GitHub        *GitHubConfiguration
	GitLab        *GitLabConfiguration
	ClaudeCode    *ClaudeCodeConfiguration
	Codex         *CodexConfiguration
	HomeAssistant *HomeAssistantConfiguration
	UniFiProtect  *UniFiProtectConfiguration
}

type GoogleConfiguration struct {
	BinaryPath *string
	Account    *string
	Services   *[]string
}

type GitHubConfiguration struct {
	BinaryPath *string
	Services   *[]string
}

type GitLabConfiguration struct {
	BinaryPath *string
	Services   *[]string
}

type ClaudeCodeConfiguration struct {
	BinaryPath            *string
	AllowedTools          *[]string
	Model                 *string
	MaxTurnTimeoutSeconds *int
}

type CodexConfiguration struct {
	BinaryPath            *string
	AllowedTools          *[]string
	Model                 *string
	ExtraArgs             *[]string
	MaxTurnTimeoutSeconds *int
}

type HomeAssistantConfiguration struct {
	BaseURL         *string
	Token           *string
	ReadOnly        *bool
	AllowedDomains  *[]string
	BlockedDomains  *[]string
	AllowedEntities *[]string
	TimeoutSeconds  *int
}

type UniFiProtectConfiguration struct {
	BaseURL               *string
	APIKey                *string
	Username              *string
	Password              *string
	VerifyTLS             *bool
	ReadOnly              *bool
	AllowedCameras        *[]string
	AllowDangerousActions *[]string
	TimeoutSeconds        *int
}

type IntegrationsConfiguration struct {
	Browser  *BrowserConfiguration
	Terminal *TerminalConfiguration
}

type BrowserConfiguration struct {
	CDPEndpoint *string
}

type TerminalConfiguration struct{}

type ChannelsConfiguration struct {
	Discord  *DiscordConfiguration
	Telegram *TelegramConfiguration
}

type DiscordConfiguration struct {
	Token *string
}

type TelegramConfiguration struct {
	Token *string
}

type SecretConfiguration struct {
	Key   *string
	Value *string
}

type SkillRegistryConfiguration struct {
	ID               *string
	Publisher        *string
	IndexURL         *string
	PublicKeys       *[]string
	IgnoreSignatures *bool
	IgnoreUpdates    *bool
}

type Agent struct {
	ID            string // globally unique
	Name          *string
	Model         *string
	Skills        *[]string
	Tools         *[]string
	Description   *string
	AvatarMediaID *string
	CreatedAt     *time.Time
	ModifiedAt    *time.Time
}

type User struct {
	ID              string // globally unique
	Username        *string
	Password        *string // hashed password
	Admin           *bool
	DefaultAgentID  *string // foreign key to Agent.ID
	TelegramChatID  *int64
	DiscordUserID   *string
	AvatarMediaID   *string
	Description     *string
	CreatedAt       *time.Time
	ModifiedAt      *time.Time
}

type Project struct {
	ID          string // globally unique
	Name        *string
	Description *string
	CreatedAt   *time.Time
	ModifiedAt  *time.Time
}

type Token struct {
	ID         string // globally unique
	UserID     *string
	Token      *string
	LastUsedAt *time.Time
	CreatedAt  *time.Time
	ModifiedAt *time.Time
}

// Workspace file document for agent/user/project scopes.
type File struct {
	ID          string // globally unique
	Scope       *Scope
	ScopeID     *string
	Path        *string // relative path
	Content     *[]byte
	ContentType *string
	CreatedAt   *time.Time
	ModifiedAt  *time.Time
}

type Conversation struct {
	ID         string // globally unique
	UserID     *string
	AgentID    *string
	Default    *bool // true means default for this UserID + AgentID pair
	Title      *string
	Summary    *string
	CreatedAt  *time.Time
	ModifiedAt *time.Time
}

type ConversationMessage struct {
	ID             string // globally unique
	ConversationID *string
	Role           *Role
	Content        *[]byte // raw JSON payload
	Metadata       *[]byte // raw JSON payload
	StopReason     *StopReason
	Model          *string
	Provider       *string
	ToolCallID     *string
	ToolName       *string
	Sequence       *int64 // monotonically increasing per conversation
	CreatedAt      *time.Time
	ModifiedAt     *time.Time
}

type Job struct {
	ID             string // globally unique
	UserID         *string
	AgentID        *string // optional
	ConversationID *string // optional foreign key to Conversation.ID
	Name           *string
	Schedule       *string // cron-like recurring schedule
	Prompt         *string // execution message/prompt
	Enabled        *bool
	RunAt          *time.Time // one-shot fire time; one-shot is implied when set
	LastRunAt      *time.Time
	CreatedAt      *time.Time
	ModifiedAt     *time.Time
}

type Session struct {
	ID            string // globally unique
	UserID        *string
	UserAgent     *string
	RemoteAddress *string
	ExpiresAt     *time.Time
	CreatedAt     *time.Time
	ModifiedAt    *time.Time
}

type Media struct {
	ID             string // globally unique
	UserID         *string
	Format         *string
	ContentType    *string
	Source         *string
	SourceAgentID  *string
	ConversationID *string
	ToolName       *string
	ToolCallID     *string
	OriginalName   *string
	Size           *int64
	CreatedAt      *time.Time
	ModifiedAt     *time.Time
}

type Skill struct {
	ID         string // globally unique
	Name       *string
	Version    *string
	Source     *string
	Manifest   *map[string]interface{} // full manifest document
	Metadata   *map[string]interface{} // full skill metadata
	Prompt     *string // full skill prompt content
	CreatedAt  *time.Time
	ModifiedAt *time.Time
}
```

### 2. Core interfaces

```go
package store

type Store interface {
	Close() error
	Transaction(func(Transaction) error) error
}

type Transaction interface {
	Commit() error

	ConfigurationOperation
	AgentOperation
	UserOperation
	ProjectOperation
	TokenOperation
	WorkspaceOperation
	ConversationOperation
	ConversationMessageOperation
	JobOperation
	SessionOperation
	MediaOperation
	SkillOperation
}

// Pagination and generic query controls.
type Option struct {
	Limit  *uint64
	Offset *uint64
}

type ResolveConfigurationOptions struct {
	CLIFlags            *map[string]string
	Environment         *map[string]string
	ApplySchemaDefaults *bool
}

type WorkspaceSearchOptions struct {
	Limit          *uint64
	CaseSensitive  *bool
	PathPrefix     *string
	IncludeContent *bool
}

type WorkspaceSearchResult struct {
	FileID       *string
	Scope        *models.Scope
	ScopeID      *string
	Path         *string
	MatchedLines *[]string
}

type ConversationListOptions struct {
	UserID  *string
	AgentID *string
	Default *bool
}

type MediaListOptions struct {
	UserID         *string
	ConversationID *string
	Source         *string
	ToolName       *string
}
```

### 3. Transaction semantics

- `Transaction(...)` guarantees begin/rollback/commit semantics.
- `Commit()` supports partial commit semantics aligned with backend/db pattern.
- Filesystem transaction uses staged writes + atomic renames + rollback cleanup.
- Postgres transaction uses `BEGIN/COMMIT/ROLLBACK`.

## Domain Method Inventory (minimum required)

### ConfigurationOperation

- `GetConfiguration(options *store.Option) (*models.Configuration, error)`
- `ModifyConfiguration(modifier func(*models.Configuration) error, options *store.Option) (*models.Configuration, error)`

Configuration resolution outside store:

- `ResolveConfiguration(configuration *models.Configuration, options ResolveConfigurationOptions) (*models.Configuration, error)`
- `ResolveConfiguration` returns a copied/enriched configuration with:
  - schema defaults
  - environment variable overrides
  - CLI flag overrides

### AgentOperation

- `ListAgents(options *store.Option) ([]models.Agent, error)`
- `CreateAgent(agent *models.Agent, files []models.File, options *store.Option) (*models.Agent, error)`
- `GetAgent(agentID string, options *store.Option) (*models.Agent, error)`
- `ModifyAgent(agentID string, modifier func(*models.Agent) error, options *store.Option) (*models.Agent, error)`
- `DeleteAgent(agentID string, options *store.Option) error`

### UserOperation

- `ListUsers(options *store.Option) ([]models.User, error)`
- `CreateUser(user *models.User, files []models.File, options *store.Option) (*models.User, error)`
- `GetUser(userID string, options *store.Option) (*models.User, error)`
- `GetUserByUsername(username string, options *store.Option) (string, *models.User, bool)`
- `GetUserByTelegramChatID(telegramChatID int64, options *store.Option) (string, *models.User, bool)`
- `GetUserByDiscordUserID(discordUserID string, options *store.Option) (string, *models.User, bool)`
- `ModifyUser(userID string, modifier func(*models.User) error, options *store.Option) (*models.User, error)`
- `DeleteUser(userID string, options *store.Option) error`

### ProjectOperation

- `ListProjects(options *store.Option) ([]models.Project, error)`
- `CreateProject(project *models.Project, files []models.File, options *store.Option) (*models.Project, error)`
- `GetProject(projectID string, options *store.Option) (*models.Project, error)`
- `ModifyProject(projectID string, modifier func(*models.Project) error, options *store.Option) (*models.Project, error)`
- `DeleteProject(projectID string, options *store.Option) error`

### TokenOperation

- `ListTokens(userID string, options *store.Option) ([]models.Token, error)`
- `CreateToken(token *models.Token, options *store.Option) (*models.Token, error)`
- `GetToken(tokenID string, options *store.Option) (*models.Token, error)`
- `GetTokenByToken(token string, options *store.Option) (string, *models.Token, bool)`
- `ModifyToken(tokenID string, modifier func(*models.Token) error, options *store.Option) (*models.Token, error)`
- `DeleteToken(tokenID string, options *store.Option) error`

### WorkspaceOperation

- `CreateWorkspaceFile(file *models.File, options *store.Option) (*models.File, error)`
- `GetWorkspaceFile(scope models.Scope, scopeID string, relativePath string, options *store.Option) (*models.File, error)`
- `ModifyWorkspaceFile(scope models.Scope, scopeID string, relativePath string, modifier func(*models.File) error, options *store.Option) (*models.File, error)`
- `DeleteWorkspaceFile(scope models.Scope, scopeID string, relativePath string, options *store.Option) error`
- `ListWorkspaceFiles(scope models.Scope, scopeID string, relativePath string, options *store.Option) ([]models.File, error)`
- `SearchWorkspace(scope models.Scope, scopeID string, query string, searchOptions WorkspaceSearchOptions, options *store.Option) ([]WorkspaceSearchResult, error)`

### ConversationOperation

- `ListConversations(listOptions ConversationListOptions, options *store.Option) ([]models.Conversation, error)`
- `CreateConversation(conversation *models.Conversation, options *store.Option) (*models.Conversation, error)`
- `GetConversation(conversationID string, options *store.Option) (*models.Conversation, error)`
- `FindDefaultConversation(userID string, agentID string, options *store.Option) (*models.Conversation, error)`
- `ModifyConversation(conversationID string, modifier func(*models.Conversation) error, options *store.Option) (*models.Conversation, error)`
- `DeleteConversation(conversationID string, options *store.Option) error`

### ConversationMessageOperation

- `ListConversationMessages(conversationID string, options *store.Option) ([]models.ConversationMessage, error)`
- `CreateConversationMessage(message *models.ConversationMessage, options *store.Option) (*models.ConversationMessage, error)`
- `GetConversationMessage(messageID string, options *store.Option) (*models.ConversationMessage, error)`
- `ModifyConversationMessage(messageID string, modifier func(*models.ConversationMessage) error, options *store.Option) (*models.ConversationMessage, error)`
- `DeleteConversationMessage(messageID string, options *store.Option) error`

### JobOperation

- `ListJobs(userID string, options *store.Option) ([]models.Job, error)`
- `CreateJob(job *models.Job, options *store.Option) (*models.Job, error)`
- `GetJob(jobID string, options *store.Option) (*models.Job, error)`
- `ModifyJob(jobID string, modifier func(*models.Job) error, options *store.Option) (*models.Job, error)`
- `DeleteJob(jobID string, options *store.Option) error`

### SessionOperation

- `ListSessions(options *store.Option) ([]models.Session, error)`
- `CreateSession(session *models.Session, options *store.Option) (*models.Session, error)`
- `GetSession(sessionID string, options *store.Option) (*models.Session, error)`
- `ModifySession(sessionID string, modifier func(*models.Session) error, options *store.Option) (*models.Session, error)`
- `DeleteSession(sessionID string, options *store.Option) error`

### MediaOperation

- `ListMedia(listOptions MediaListOptions, options *store.Option) ([]models.Media, error)`
- `CreateMedia(content io.Reader, metadata *models.Media, options *store.Option) (*models.Media, error)`
- `GetMedia(mediaID string, options *store.Option) ([]byte, *models.Media, error)`
- `OpenMedia(mediaID string, options *store.Option) (io.ReadCloser, *models.Media, error)`
- `ModifyMedia(mediaID string, modifier func(*models.Media) error, options *store.Option) (*models.Media, error)`
- `DeleteMedia(mediaID string, options *store.Option) error`

### SkillOperation

- `ListSkills(options *store.Option) ([]models.Skill, error)`
- `CreateSkill(skill *models.Skill, options *store.Option) (*models.Skill, error)`
- `GetSkill(skillID string, options *store.Option) (*models.Skill, error)`
- `ModifySkill(skillID string, modifier func(*models.Skill) error, options *store.Option) (*models.Skill, error)`
- `DeleteSkill(skillID string, options *store.Option) error`

## Filesystem access policy

- End-state rule: no direct filesystem operations outside `internal/store/**`.
- Explicit exceptions:
  - `internal/tools/shell/**` (shell tool implementation).
  - `internal/tools/filesystem/**` (filesystem tool implementation).
  - Skill execution path that invokes shell commands through the shell tool contract.
  - `main.go` PID lock file handling (`gateway.pid`) as an operational runtime concern.
- Client-side terminal PTY code is not part of backend datastore isolation scope.
- Datastore does not expose raw path getters; callers must use domain operations only.

## Workspace tools and datastore

- Agent/user/project workspace tools must read/write through `WorkspaceOperation` only.
- Directory existence is implicit:
  - `fs` mode creates directories only when needed by file writes.
  - `db` mode does not store directories as entities.
- Workspace scope is typed by `models.Scope` (`agent`, `user`, `project`).
- All workspace operations are transactional under store transactions.

## Filesystem implementation plan

- Keep current on-disk structure and file formats (`yaml`, workspace markdown).
- In `fs` implementation, create directories on demand during write/create operations; no explicit ensure/seed operations in API surface.
- Replace immediate writes with transaction-scoped staged writes:
  - Build list of write/mkdir/move operations in transaction context.
  - Apply in commit order.
  - On rollback: discard staged temp files and no-op unapplied operations.
- Reuse `atomicfile` for final writes.
- Preserve permission behavior (`0600` for sensitive files like user/token auth records).

## Postgres implementation plan

### Data model

Create normalized tables for:

- `configuration` (singleton config blob + version)
- `agents`
- `users`
- `projects`
- `tokens`
- `workspace_files`
- `conversations`
- `conversation_messages`
- `jobs`
- `sessions`
- `media`
- Postgres large objects (binary payloads referenced by `media`)
- `skills`

Use typed columns where practical and JSONB for extensible fields.

### Indexes and constraints

- Unique index on `tokens.token` for `GetTokenByToken`.
- Index on `users.telegram_chat_id` for `GetUserByTelegramChatID`.
- Index on `users.discord_user_id` for `GetUserByDiscordUserID`.
- Partial unique index for default conversations: one `default=true` per (`user_id`, `agent_id`).
- Unique index on `conversation_messages` by (`conversation_id`, `sequence`).

### Behavior mapping

- Config, agent, user, project, token operations map to CRUD over tables.
- Agent/user/project creation accepts initial `[]models.File` and persists them transactionally.
- Workspace file tree maps to `workspace_files` keyed by `(scope, scope_id, path)`.
- Conversation operations map to `conversations`; message operations map to `conversation_messages`.
- Jobs map to `jobs` with one-shot semantics implied by `run_at`.
- If both `jobs.run_at` and recurring schedule are present, `run_at` takes priority.
- Sessions map to `sessions`.
- Media operations map to `media` metadata plus Postgres large objects for binary content.
- Postgres large object identifiers remain internal to `internal/store/db` and are not exposed in `models.Media`.
- Skills operations map to `skills` with full manifest/metadata/prompt payload.
- Agent defaults use `users.default_agent_id`; default conversation uses `conversations.default` and `FindDefaultConversation`.
- Username/password/admin and channel linkage for auth/chat routing are read from `users`.
- Token validation and lifecycle are handled by `TokenOperation`.
- Backend must read persistence-backed state from store on each request path; do not keep datastore query-result caches in process memory.

### Migrations

- Include store-specific migration runner in `internal/store/db`.
- No import from external backend; replicate pattern only.

## Startup and dependency injection

### New CLI flags

- `--store` values: `filesystem` (default), `postgres`.
- Postgres connection flags/env:
  - `--store-postgres-host` (default `127.0.0.1`)
  - `--store-postgres-port` (default `5432`)
  - `--store-postgres-user` (default `teanode`)
  - `--store-postgres-password` (default `teanode`)
  - `--store-postgres-database` (default `teanode`)
  - `--store-postgres-sslmode` (default `disable`)

### Bootstrap flow changes

In `main.go` / `cmd/gateway.go`:

1. Build `store.Store` from CLI/environment.
2. `defer store.Close()`.
3. Replace all `configs.*` persistence calls with `store.Transaction(...)` calls.
4. Pass `store.Store` (or narrower operation interfaces) into services.
5. Keep PID lock lifecycle completely in `main.go`.

## Call-site refactor plan (by package)

Primary packages with direct persistence coupling:

- `cmd/gateway.go`
- `internal/api/v1api/*`
- `internal/onboarding/*`
- `internal/agents/*`
- `internal/conversations/*`
- `internal/jobs/*`
- `internal/projects/*`
- `internal/skills/*`
- `internal/sessions/*`
- `internal/media/*`
- `internal/watcher/*` (remove package and integration points)
- `cmd/terminal.go`

Key behavior updates:

- `internal/agents/registry.go`: replace `state.yaml` persistence with `UserOperation` + `ConversationOperation.FindDefaultConversation(...)`.
- `internal/gw/gateway.go`: remove disk model cache (`models.yaml`), always query providers, and remove persistence-result in-memory caching.
- `internal/api/v1api/auth.go`, `internal/gw/auth_middleware.go`: use `UserOperation` and `TokenOperation` (including `GetTokenByToken`).
- `internal/channels/telegram/*`, `internal/channels/discord/*`: use user lookups by channel linkage fields on `users`.
- `internal/tools/workspace/*`: replace direct file IO with `WorkspaceOperation`.
- `internal/conversations/*`, `internal/jobs/*`, `internal/sessions/*`, `internal/media/*`, `internal/skills/*`: switch to store operations.
- `cmd/gateway.go`: remove watcher bootstrap/wiring; runtime reload is explicit and driven by store-backed writes or explicit command paths.
- `cmd/terminal.go`: remove token fallback lookup from store/config files; require token via CLI flag or `TEANODE_GATEWAY_TOKEN`.

## Execution phases

### Phase 1: Contract and bootstrap

- Introduce `internal/store` interfaces/types/errors.
- Add store factory and CLI flag wiring.
- Keep behavior via filesystem implementation first.

### Phase 2: Filesystem implementation

- Move current persistence IO into `internal/store/fs`.
- Make writes transaction-scoped.
- Update tests to run through store interfaces.

### Phase 3: Postgres implementation

- Add schema + migration runner.
- Implement all required operations transactionally.
- Add integration tests with ephemeral Postgres.

### Phase 4: Cutover and cleanup

- Remove legacy `internal/configs` persistence globals.
- Move config resolution ownership to `internal/configurations`; remaining `internal/configs` scope is eliminated or reduced to transitional adapters only.
- Ensure no non-exception package does direct persistence IO.
- Remove `internal/watcher` package and all startup/runtime wiring to it.

## Testing strategy

- Contract tests run against both `fs` and `db` implementations for:
  - Configuration get/modify and `internal/configurations.ResolveConfiguration` behavior.
  - Agent/user/project/token CRUD, including create-time workspace files.
  - Workspace file CRUD/list/search by scope.
  - Conversation + conversation message lifecycle.
  - Job CRUD with recurring and one-shot (`RunAt`) behavior.
  - Session CRUD including `UserAgent` and `RemoteAddress` metadata.
  - Media create/get/open/modify/delete with metadata and stream handling.
  - Skill CRUD with full payload integrity.
  - User lookups (`username`, `telegramChatID`, `discordUserID`) and token lookup by raw token value.
  - Transaction atomicity and rollback behavior.
- CI checks for postgres mode must fail if code writes persistence files under local data directories (for example `~/.teanode` equivalents), including `.trash`.

## Acceptance criteria

- Runtime store selection via CLI works without code changes.
- Existing functional domains are store-backed: configuration, agents, users, tokens, projects, workspace, conversations, conversation messages, jobs, sessions, media, skills.
- No direct filesystem persistence IO outside `internal/store/**`, except explicit exception paths.
- Filesystem mode preserves current user-visible behavior.
- Postgres mode supports equivalent behavior with required indexes and constraints.
- `internal/watcher` is removed from codebase and not used at runtime.
- Backend request paths do not cache datastore query results in memory; requests hit store as source of truth.
- No data migration logic exists in this refactor.
