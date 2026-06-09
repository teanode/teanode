package models

import (
	"strings"
	"time"
)

// ToolPolicyLevel controls access and approval requirements for a tool action group.
type ToolPolicyLevel string

const (
	ToolPolicyDisabled       ToolPolicyLevel = "disabled"
	ToolPolicyAdminApproval  ToolPolicyLevel = "admin_approval"
	ToolPolicyAdminOnly      ToolPolicyLevel = "admin_only"
	ToolPolicyAnyoneApproval ToolPolicyLevel = "anyone_approval"
	ToolPolicyAnyone         ToolPolicyLevel = "anyone"
)

// ToolPolicyGroup classifies tool actions for policy resolution.
type ToolPolicyGroup string

const (
	ToolPolicyGroupAll   ToolPolicyGroup = "*"
	ToolPolicyGroupRead  ToolPolicyGroup = "read"
	ToolPolicyGroupWrite ToolPolicyGroup = "write"
)

// ToolPolicyConfiguration maps a tool + action group to a policy level.
type ToolPolicyConfiguration struct {
	Tool  *string          `json:"tool,omitempty" yaml:"tool,omitempty"`
	Group *ToolPolicyGroup `json:"group,omitempty" yaml:"group,omitempty"`
	Level *ToolPolicyLevel `json:"level,omitempty" yaml:"level,omitempty"`
}

type Configuration struct {
	ID         string     `json:"id,omitempty" yaml:"id,omitempty"`
	CreatedAt  *time.Time `json:"createdAt,omitempty" yaml:"createdAt,omitempty"`
	ModifiedAt *time.Time `json:"modifiedAt,omitempty" yaml:"modifiedAt,omitempty"`

	Node         *NodeConfiguration          `json:"node,omitempty" yaml:"node,omitempty"`
	Certificate  *CertificateConfiguration   `json:"certificate,omitempty" yaml:"certificate,omitempty"`
	Models       *ModelsConfiguration        `json:"models,omitempty" yaml:"models,omitempty"`
	Tools        *ToolsConfiguration         `json:"tools,omitempty" yaml:"tools,omitempty"`
	Integrations *IntegrationsConfiguration  `json:"integrations,omitempty" yaml:"integrations,omitempty"`
	Channels     *ChannelsConfiguration      `json:"channels,omitempty" yaml:"channels,omitempty"`
	Cloud        *CloudConfiguration         `json:"cloud,omitempty" yaml:"cloud,omitempty"`
	Updating     *UpdateConfiguration        `json:"update,omitempty" yaml:"update,omitempty"`
	Secrets      *[]*SecretConfiguration     `json:"secrets,omitempty" yaml:"secrets,omitempty"`
	ToolPolicies *[]*ToolPolicyConfiguration `json:"toolPolicies,omitempty" yaml:"toolPolicies,omitempty"`
}

type BindMode string

const (
	BindModeLoopback BindMode = "loopback"
	BindModeLAN      BindMode = "lan"
)

type NodeConfiguration struct {
	Port      *int      `json:"port,omitempty" yaml:"port,omitempty"`
	Bind      *BindMode `json:"bind,omitempty" yaml:"bind,omitempty"`
	PublicURL *string   `json:"publicUrl,omitempty" yaml:"publicUrl,omitempty"`
	TLS       *bool     `json:"tls,omitempty" yaml:"tls,omitempty"`
}

type CertificateConfiguration struct {
	ACMEEmail      *string    `json:"acmeEmail,omitempty" yaml:"acmeEmail,omitempty"`
	ACMEAccountKey *string    `json:"acmeAccountKey,omitempty" yaml:"acmeAccountKey,omitempty"`
	Domain         *string    `json:"domain,omitempty" yaml:"domain,omitempty"`
	Certificate    *string    `json:"certificate,omitempty" yaml:"certificate,omitempty"`
	PrivateKey     *string    `json:"privateKey,omitempty" yaml:"privateKey,omitempty"`
	IssuedAt       *time.Time `json:"issuedAt,omitempty" yaml:"issuedAt,omitempty"`
	ExpiresAt      *time.Time `json:"expiresAt,omitempty" yaml:"expiresAt,omitempty"`
}

type ModelsConfiguration struct {
	Default                     *string                   `json:"default,omitempty" yaml:"default,omitempty"`
	SummarizerProviderModelName *string                   `json:"summarizerProviderModelName,omitempty" yaml:"summarizerModel,omitempty"`
	EmbeddingProviderModelName  *string                   `json:"embeddingProviderModelName,omitempty" yaml:"embeddingProviderModelName,omitempty"`
	ContextWindow               *int                      `json:"contextWindow,omitempty" yaml:"contextWindow,omitempty"`
	Providers                   *[]*ProviderConfiguration `json:"providers,omitempty" yaml:"providers,omitempty"`
	DefaultLimits               *map[string]interface{}   `json:"defaultLimits,omitempty" yaml:"defaultLimits,omitempty"`
	Limits                      *[]map[string]interface{} `json:"limits,omitempty" yaml:"limits,omitempty"`
}

type ProviderConfiguration struct {
	Name    *string `json:"name,omitempty" yaml:"name,omitempty"`
	BaseURL *string `json:"baseUrl,omitempty" yaml:"baseUrl,omitempty"`
	APIKey  *string `json:"apiKey,omitempty" yaml:"apiKey,omitempty"`
}

type ToolsConfiguration struct {
	BraveAPIKey   *string                     `json:"braveApiKey,omitempty" yaml:"braveApiKey,omitempty"`
	Google        *GoogleConfiguration        `json:"google,omitempty" yaml:"google,omitempty"`
	GitHub        *GitHubConfiguration        `json:"gitHub,omitempty" yaml:"gitHub,omitempty"`
	GitLab        *GitLabConfiguration        `json:"gitLab,omitempty" yaml:"gitLab,omitempty"`
	Mattermost    *MattermostConfiguration    `json:"mattermost,omitempty" yaml:"mattermost,omitempty"`
	ClaudeCode    *ClaudeCodeConfiguration    `json:"claudeCode,omitempty" yaml:"claudeCode,omitempty"`
	Codex         *CodexConfiguration         `json:"codex,omitempty" yaml:"codex,omitempty"`
	HomeAssistant *HomeAssistantConfiguration `json:"homeAssistant,omitempty" yaml:"homeAssistant,omitempty"`
	UniFiProtect  *UniFiProtectConfiguration  `json:"unifiProtect,omitempty" yaml:"unifiProtect,omitempty"`
	MCP           *MCPConfiguration           `json:"mcp,omitempty" yaml:"mcp,omitempty"`
}

// MCPConfiguration configures Model Context Protocol (MCP) client support.
// TeaNode connects to the configured MCP servers, discovers their tools, and
// exposes them as namespaced TeaNode tools during runs. Both the remote
// streamable HTTP transport and the local stdio (subprocess) transport are
// supported.
type MCPConfiguration struct {
	Servers *[]*MCPServerConfiguration `json:"servers,omitempty" yaml:"servers,omitempty"`
}

// MCPServerTransport selects how TeaNode connects to an MCP server.
type MCPServerTransport string

const (
	// MCPServerTransportHTTP is the streamable HTTP transport: TeaNode connects to
	// a remote URL. This is the default.
	MCPServerTransportHTTP MCPServerTransport = "http"
	// MCPServerTransportStdio launches a local subprocess and speaks
	// newline-delimited JSON-RPC over its stdin/stdout (the transport used by
	// tools such as `claude mcp`-style stdio servers).
	MCPServerTransportStdio MCPServerTransport = "stdio"
)

// MCPServerAuthMode selects how a remote MCP server is authenticated.
type MCPServerAuthMode string

const (
	// MCPServerAuthNone sends no Authorization header.
	MCPServerAuthNone MCPServerAuthMode = "none"
	// MCPServerAuthStatic sends a single node-level Authorization value shared by
	// every user (the original v1 behavior).
	MCPServerAuthStatic MCPServerAuthMode = "static"
	// MCPServerAuthUser requires each user to supply their own credential via a
	// per-user MCPConnection. The server is only available to users who have
	// connected.
	MCPServerAuthUser MCPServerAuthMode = "user"
	// MCPServerAuthOAuth requires each user to authorize via the OAuth 2.1
	// authorization-code flow with PKCE. Tokens are stored per user.
	MCPServerAuthOAuth MCPServerAuthMode = "oauth"
)

// MCPServerConfiguration describes a single MCP server reached over either the
// streamable HTTP transport (URL) or the local stdio transport (Command).
//
// Limitations: only the tools capability is consumed (prompts and resources are
// out of scope). HTTP authentication supports a shared static Authorization
// header value and, for user-scoped servers, a per-user MCPConnection
// credential. Stdio servers run locally and use no HTTP auth.
type MCPServerConfiguration struct {
	// Name identifies the server and namespaces its tools as
	// "mcp__<name>__<tool>". It must be unique across configured servers.
	Name *string `json:"name,omitempty" yaml:"name,omitempty"`
	// Transport selects how TeaNode connects: "http" (streamable HTTP, the
	// default) or "stdio" (a local subprocess). When empty it is inferred:
	// "stdio" if Command is set and URL is empty, otherwise "http".
	Transport *MCPServerTransport `json:"transport,omitempty" yaml:"transport,omitempty"`
	// URL is the streamable HTTP endpoint of the MCP server (http transport).
	URL *string `json:"url,omitempty" yaml:"url,omitempty"`
	// Command is the executable launched for a stdio-transport server.
	Command *string `json:"command,omitempty" yaml:"command,omitempty"`
	// Args are the command-line arguments passed to Command (stdio transport).
	Args *[]string `json:"args,omitempty" yaml:"args,omitempty"`
	// Env are extra environment variables set for the subprocess, merged over
	// TeaNode's own environment. Used to pass a stdio server its secrets (for
	// example an API key).
	Env *map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
	// WorkingDir is the working directory for the subprocess. Empty uses
	// TeaNode's working directory.
	WorkingDir *string `json:"workingDir,omitempty" yaml:"workingDir,omitempty"`
	// Enabled gates the server. A nil value is treated as enabled so that a
	// configured server is active by default; set false to keep it but skip it.
	Enabled *bool `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	// Auth selects the authentication mode. When empty it is inferred: "static"
	// if Authorization is set, otherwise "none". Set to "user" to require each
	// user to supply their own credential via a per-user MCPConnection.
	Auth *MCPServerAuthMode `json:"auth,omitempty" yaml:"auth,omitempty"`
	// Authorization is the verbatim value sent in the HTTP Authorization
	// header (for example "Bearer <token>") for the "static" auth mode. Empty
	// means no auth header. Ignored for the "user" auth mode.
	Authorization *string `json:"authorization,omitempty" yaml:"authorization,omitempty"`
	// TimeoutSeconds bounds each HTTP request to the server. Defaults to 30.
	TimeoutSeconds *int `json:"timeoutSeconds,omitempty" yaml:"timeoutSeconds,omitempty"`

	// OAuth client settings for the "oauth" auth mode.

	// OAuthClientID is the registered OAuth client identifier.
	OAuthClientID *string `json:"oauthClientId,omitempty" yaml:"oauthClientId,omitempty"`
	// OAuthClientSecret authenticates a confidential client (optional; public
	// clients rely on PKCE alone).
	OAuthClientSecret *string `json:"oauthClientSecret,omitempty" yaml:"oauthClientSecret,omitempty"`
	// OAuthScopes are requested during authorization.
	OAuthScopes *[]string `json:"oauthScopes,omitempty" yaml:"oauthScopes,omitempty"`
	// OAuthAuthorizationURL and OAuthTokenURL override discovery when set.
	OAuthAuthorizationURL *string `json:"oauthAuthorizationUrl,omitempty" yaml:"oauthAuthorizationUrl,omitempty"`
	OAuthTokenURL         *string `json:"oauthTokenUrl,omitempty" yaml:"oauthTokenUrl,omitempty"`
}

// ResolvedAuthMode returns the effective auth mode, inferring it from the
// presence of a static Authorization value when Auth is unset.
func (self *MCPServerConfiguration) ResolvedAuthMode() MCPServerAuthMode {
	if self == nil {
		return MCPServerAuthNone
	}
	if self.Auth != nil && *self.Auth != "" {
		return *self.Auth
	}
	if self.Authorization != nil && *self.Authorization != "" {
		return MCPServerAuthStatic
	}
	return MCPServerAuthNone
}

// ResolvedTransport returns the effective transport, inferring "stdio" from a
// server that has a Command but no URL when Transport is unset.
func (self *MCPServerConfiguration) ResolvedTransport() MCPServerTransport {
	if self == nil {
		return MCPServerTransportHTTP
	}
	if self.Transport != nil {
		switch *self.Transport {
		case MCPServerTransportStdio:
			return MCPServerTransportStdio
		case MCPServerTransportHTTP:
			return MCPServerTransportHTTP
		}
	}
	hasCommand := self.Command != nil && strings.TrimSpace(*self.Command) != ""
	hasUrl := self.URL != nil && strings.TrimSpace(*self.URL) != ""
	if hasCommand && !hasUrl {
		return MCPServerTransportStdio
	}
	return MCPServerTransportHTTP
}

type GoogleConfiguration struct {
	BinaryPath *string   `json:"binaryPath,omitempty" yaml:"binaryPath,omitempty"`
	Account    *string   `json:"account,omitempty" yaml:"account,omitempty"`
	Services   *[]string `json:"services,omitempty" yaml:"services,omitempty"`
}

type GitHubConfiguration struct {
	BinaryPath *string   `json:"binaryPath,omitempty" yaml:"binaryPath,omitempty"`
	Services   *[]string `json:"services,omitempty" yaml:"services,omitempty"`
}

type GitLabConfiguration struct {
	BinaryPath *string   `json:"binaryPath,omitempty" yaml:"binaryPath,omitempty"`
	Services   *[]string `json:"services,omitempty" yaml:"services,omitempty"`
}

type MattermostConfiguration struct {
	BinaryPath *string   `json:"binaryPath,omitempty" yaml:"binaryPath,omitempty"`
	Services   *[]string `json:"services,omitempty" yaml:"services,omitempty"`
}

type ClaudeCodeConfiguration struct {
	BinaryPath            *string   `json:"binaryPath,omitempty" yaml:"binaryPath,omitempty"`
	AllowedTools          *[]string `json:"allowedTools,omitempty" yaml:"allowedTools,omitempty"`
	ModelName             *string   `json:"modelName,omitempty" yaml:"model,omitempty"`
	MaxTurnTimeoutSeconds *int      `json:"maxTurnTimeoutSeconds,omitempty" yaml:"maxTurnTimeoutSeconds,omitempty"`
}

type CodexConfiguration struct {
	BinaryPath            *string   `json:"binaryPath,omitempty" yaml:"binaryPath,omitempty"`
	AllowedTools          *[]string `json:"allowedTools,omitempty" yaml:"allowedTools,omitempty"`
	ModelName             *string   `json:"modelName,omitempty" yaml:"model,omitempty"`
	ExtraArguments        *[]string `json:"extraArgs,omitempty" yaml:"extraArgs,omitempty"`
	MaxTurnTimeoutSeconds *int      `json:"maxTurnTimeoutSeconds,omitempty" yaml:"maxTurnTimeoutSeconds,omitempty"`
}

type HomeAssistantConfiguration struct {
	BaseURL         *string   `json:"baseUrl,omitempty" yaml:"baseUrl,omitempty"`
	Token           *string   `json:"token,omitempty" yaml:"token,omitempty"`
	ReadOnly        *bool     `json:"readOnly,omitempty" yaml:"readOnly,omitempty"`
	AllowedDomains  *[]string `json:"allowedDomains,omitempty" yaml:"allowedDomains,omitempty"`
	BlockedDomains  *[]string `json:"blockedDomains,omitempty" yaml:"blockedDomains,omitempty"`
	AllowedEntities *[]string `json:"allowedEntities,omitempty" yaml:"allowedEntities,omitempty"`
	TimeoutSeconds  *int      `json:"timeoutSeconds,omitempty" yaml:"timeoutSeconds,omitempty"`
}

type UniFiProtectConfiguration struct {
	BaseURL               *string   `json:"baseUrl,omitempty" yaml:"baseUrl,omitempty"`
	APIKey                *string   `json:"apiKey,omitempty" yaml:"apiKey,omitempty"`
	Username              *string   `json:"username,omitempty" yaml:"username,omitempty"`
	Password              *string   `json:"password,omitempty" yaml:"password,omitempty"`
	VerifyTLS             *bool     `json:"verifyTls,omitempty" yaml:"verifyTls,omitempty"`
	ReadOnly              *bool     `json:"readOnly,omitempty" yaml:"readOnly,omitempty"`
	AllowedCameras        *[]string `json:"allowedCameras,omitempty" yaml:"allowedCameras,omitempty"`
	AllowDangerousActions *[]string `json:"allowDangerousActions,omitempty" yaml:"allowDangerousActions,omitempty"`
	TimeoutSeconds        *int      `json:"timeoutSeconds,omitempty" yaml:"timeoutSeconds,omitempty"`
}

type IntegrationsConfiguration struct {
	Browser  *BrowserConfiguration  `json:"browser,omitempty" yaml:"browser,omitempty"`
	Terminal *TerminalConfiguration `json:"terminal,omitempty" yaml:"terminal,omitempty"`
}

type BrowserConfiguration struct {
	CDPEndpoint *string `json:"cdpEndpoint,omitempty" yaml:"cdpEndpoint,omitempty"`
}

type TerminalConfiguration struct{}

type ChannelsConfiguration struct {
	Discord  *DiscordConfiguration  `json:"discord,omitempty" yaml:"discord,omitempty"`
	Telegram *TelegramConfiguration `json:"telegram,omitempty" yaml:"telegram,omitempty"`
}

type DiscordConfiguration struct {
	Token *string `json:"token,omitempty" yaml:"token,omitempty"`
}

type TelegramConfiguration struct {
	Token *string `json:"token,omitempty" yaml:"token,omitempty"`
}

type SecretConfiguration struct {
	Key   *string `json:"key,omitempty" yaml:"key,omitempty"`
	Value *string `json:"value,omitempty" yaml:"value,omitempty"`
}

type CloudConfiguration struct {
	URL        *string `json:"url,omitempty" yaml:"url,omitempty"`
	NodeID     *string `json:"nodeId,omitempty" yaml:"nodeId,omitempty"`
	NodeSecret *string `json:"nodeSecret,omitempty" yaml:"nodeSecret,omitempty"`
	UserID     *string `json:"userId,omitempty" yaml:"userId,omitempty"`
}

// UpdatePolicy controls the self-update behavior.
type UpdatePolicy string

const (
	UpdatePolicyDisabled UpdatePolicy = "disabled"
	UpdatePolicyNotify   UpdatePolicy = "notify"
	UpdatePolicyAuto     UpdatePolicy = "auto"
)

type UpdateConfiguration struct {
	Policy             *UpdatePolicy `json:"policy,omitempty" yaml:"policy,omitempty"`
	CheckIntervalHours *int          `json:"checkIntervalHours,omitempty" yaml:"checkIntervalHours,omitempty"`
}
