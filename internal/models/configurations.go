package models

import "time"

type Configuration struct {
	ID         string     `json:"id,omitempty" yaml:"id,omitempty"`
	CreatedAt  *time.Time `json:"createdAt,omitempty" yaml:"createdAt,omitempty"`
	ModifiedAt *time.Time `json:"modifiedAt,omitempty" yaml:"modifiedAt,omitempty"`

	Gateway          *GatewayConfiguration         `json:"gateway,omitempty" yaml:"gateway,omitempty"`
	Models           *ModelsConfiguration          `json:"models,omitempty" yaml:"models,omitempty"`
	Tools            *ToolsConfiguration           `json:"tools,omitempty" yaml:"tools,omitempty"`
	Integrations     *IntegrationsConfiguration    `json:"integrations,omitempty" yaml:"integrations,omitempty"`
	Channels         *ChannelsConfiguration        `json:"channels,omitempty" yaml:"channels,omitempty"`
	Secrets          *[]SecretConfiguration        `json:"secrets,omitempty" yaml:"secrets,omitempty"`
	SkillsRegistries *[]SkillRegistryConfiguration `json:"skillsRegistries,omitempty" yaml:"skillsRegistries,omitempty"`
}

type GatewayConfiguration struct {
	Port      *int                          `json:"port,omitempty" yaml:"port,omitempty"`
	Bind      *string                       `json:"bind,omitempty" yaml:"bind,omitempty"`
	Security  *GatewaySecurityConfiguration `json:"security,omitempty" yaml:"security,omitempty"`
	PublicURL *string                       `json:"publicUrl,omitempty" yaml:"publicUrl,omitempty"`
}

type GatewaySecurityConfiguration struct {
	SessionMaxAgeDays *int    `json:"sessionMaxAgeDays,omitempty" yaml:"sessionMaxAgeDays,omitempty"`
	ForwarderKey      *string `json:"forwarderKey,omitempty" yaml:"forwarderKey,omitempty"`
}

type ModelsConfiguration struct {
	Default         *string                   `json:"default,omitempty" yaml:"default,omitempty"`
	SummarizerModel *string                   `json:"summarizerModel,omitempty" yaml:"summarizerModel,omitempty"`
	ContextWindow   *int                      `json:"contextWindow,omitempty" yaml:"contextWindow,omitempty"`
	DefaultLimits   *map[string]interface{}   `json:"defaultLimits,omitempty" yaml:"defaultLimits,omitempty"`
	Limits          *[]map[string]interface{} `json:"limits,omitempty" yaml:"limits,omitempty"`
	Providers       *[]ProviderConfiguration  `json:"providers,omitempty" yaml:"providers,omitempty"`
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
	ClaudeCode    *ClaudeCodeConfiguration    `json:"claudeCode,omitempty" yaml:"claudeCode,omitempty"`
	Codex         *CodexConfiguration         `json:"codex,omitempty" yaml:"codex,omitempty"`
	HomeAssistant *HomeAssistantConfiguration `json:"homeAssistant,omitempty" yaml:"homeAssistant,omitempty"`
	UniFiProtect  *UniFiProtectConfiguration  `json:"uniFiProtect,omitempty" yaml:"uniFiProtect,omitempty"`
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

type ClaudeCodeConfiguration struct {
	BinaryPath            *string   `json:"binaryPath,omitempty" yaml:"binaryPath,omitempty"`
	AllowedTools          *[]string `json:"allowedTools,omitempty" yaml:"allowedTools,omitempty"`
	Model                 *string   `json:"model,omitempty" yaml:"model,omitempty"`
	MaxTurnTimeoutSeconds *int      `json:"maxTurnTimeoutSeconds,omitempty" yaml:"maxTurnTimeoutSeconds,omitempty"`
}

type CodexConfiguration struct {
	BinaryPath            *string   `json:"binaryPath,omitempty" yaml:"binaryPath,omitempty"`
	AllowedTools          *[]string `json:"allowedTools,omitempty" yaml:"allowedTools,omitempty"`
	Model                 *string   `json:"model,omitempty" yaml:"model,omitempty"`
	ExtraArgs             *[]string `json:"extraArgs,omitempty" yaml:"extraArgs,omitempty"`
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
	VerifyTLS             *bool     `json:"verifyTLS,omitempty" yaml:"verifyTLS,omitempty"`
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

type SkillRegistryConfiguration struct {
	ID               *string   `json:"id,omitempty" yaml:"id,omitempty"`
	Publisher        *string   `json:"publisher,omitempty" yaml:"publisher,omitempty"`
	IndexURL         *string   `json:"indexUrl,omitempty" yaml:"indexUrl,omitempty"`
	PublicKeys       *[]string `json:"publicKeys,omitempty" yaml:"publicKeys,omitempty"`
	IgnoreSignatures *bool     `json:"ignoreSignatures,omitempty" yaml:"ignoreSignatures,omitempty"`
	IgnoreUpdates    *bool     `json:"ignoreUpdates,omitempty" yaml:"ignoreUpdates,omitempty"`
}
