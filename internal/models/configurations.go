package models

import "time"

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
