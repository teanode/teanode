// Package config handles loading and watching the teanode configuration file.
package configs

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/teanode/teanode/internal/prompts"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/util/atomicfile"
	"github.com/teanode/teanode/internal/util/trash"
	"gopkg.in/yaml.v3"
)

//go:embed schema/*.json
var schemaFiles embed.FS

var (
	configSchemaJson        = mustReadSchemaFile("schema/config.schema.json")
	agentConfigSchemaJson   = mustReadSchemaFile("schema/agentConfig.schema.json")
	userConfigSchemaJson    = mustReadSchemaFile("schema/userConfig.schema.json")
	projectConfigSchemaJson = mustReadSchemaFile("schema/projectConfig.schema.json")
)

func mustReadSchemaFile(path string) []byte {
	data, err := schemaFiles.ReadFile(path)
	if err != nil {
		panic(fmt.Sprintf("loading embedded schema %s: %v", path, err))
	}
	return data
}

// ConfigSchema returns the embedded config schema JSON for UI form generation.
func ConfigSchema() json.RawMessage {
	return json.RawMessage(configSchemaJson)
}

// AgentConfigSchema returns the embedded agent config schema JSON for UI form generation.
func AgentConfigSchema() json.RawMessage {
	return json.RawMessage(agentConfigSchemaJson)
}

// UserConfigSchema returns the embedded user config schema JSON for UI form generation.
func UserConfigSchema() json.RawMessage {
	return json.RawMessage(userConfigSchemaJson)
}

// ProjectConfigSchema returns the embedded project config schema JSON for UI form generation.
func ProjectConfigSchema() json.RawMessage {
	return json.RawMessage(projectConfigSchemaJson)
}

// --- Schema-driven defaults ---

// parseSchemaDefaults extracts dot-path→default pairs from a JSON Schema.
// It recursively walks the "properties" tree, building dot-paths from the
// nesting structure, and collects every "default" value it encounters.
func parseSchemaDefaults(schemaJson []byte) map[string]interface{} {
	var schema map[string]interface{}
	json.Unmarshal(schemaJson, &schema)
	defaults := make(map[string]interface{})
	if properties, ok := schema["properties"].(map[string]interface{}); ok {
		collectDefaults(properties, "", defaults)
	}
	return defaults
}

// collectDefaults recursively walks JSON Schema properties, collecting default
// values keyed by their dot-path (e.g. "gateway.port").
func collectDefaults(properties map[string]interface{}, prefix string, defaults map[string]interface{}) {
	for key, raw := range properties {
		property, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		dotPath := key
		if prefix != "" {
			dotPath = prefix + "." + key
		}
		if defaultValue, hasDefault := property["default"]; hasDefault {
			defaults[dotPath] = defaultValue
		}
		if nested, ok := property["properties"].(map[string]interface{}); ok {
			collectDefaults(nested, dotPath, defaults)
		}
	}
}

func schemaInt(defaults map[string]interface{}, key string) int {
	if value, ok := defaults[key]; ok {
		if number, ok := value.(float64); ok {
			return int(number)
		}
	}
	return 0
}

func schemaFloat64(defaults map[string]interface{}, key string) float64 {
	if value, ok := defaults[key]; ok {
		if number, ok := value.(float64); ok {
			return number
		}
	}
	return 0
}

func schemaString(defaults map[string]interface{}, key string) string {
	if value, ok := defaults[key]; ok {
		if text, ok := value.(string); ok {
			return text
		}
	}
	return ""
}

func init() {
	configDefaults := parseSchemaDefaults(configSchemaJson)

	DefaultAgentLimits = AgentLimits{
		MaxToolRounds:         schemaInt(configDefaults, "models.defaultLimits.maxToolRounds"),
		CompressionThreshold:  schemaFloat64(configDefaults, "models.defaultLimits.compressionThreshold"),
		MinKeepMessages:       schemaInt(configDefaults, "models.defaultLimits.minKeepMessages"),
		MinKeepRecentTokens:   schemaInt(configDefaults, "models.defaultLimits.minKeepRecentTokens"),
		MaxToolResultChars:    schemaInt(configDefaults, "models.defaultLimits.maxToolResultChars"),
		MaxWorkspaceFileChars: schemaInt(configDefaults, "models.defaultLimits.maxWorkspaceFileChars"),
	}

	DefaultSummarizerConfig = SummarizerConfig{
		TickInterval:         schemaInt(configDefaults, "summarizer.tickInterval"),
		StartupDelay:         schemaInt(configDefaults, "summarizer.startupDelay"),
		InactivityTime:       schemaInt(configDefaults, "summarizer.inactivityTime"),
		MinMessages:          schemaInt(configDefaults, "summarizer.minMessages"),
		MaxConversationChars: schemaInt(configDefaults, "summarizer.maxConversationChars"),
		MaxMessageChars:      schemaInt(configDefaults, "summarizer.maxMessageChars"),
	}
}

// AgentLimits holds resolved runtime limits for an agent.
type AgentLimits struct {
	MaxToolRounds         int     `json:"maxToolRounds,omitempty" yaml:"maxToolRounds,omitempty"`
	CompressionThreshold  float64 `json:"compressionThreshold,omitempty" yaml:"compressionThreshold,omitempty"`
	MinKeepMessages       int     `json:"minKeepMessages,omitempty" yaml:"minKeepMessages,omitempty"`
	MinKeepRecentTokens   int     `json:"minKeepRecentTokens,omitempty" yaml:"minKeepRecentTokens,omitempty"`
	MaxToolResultChars    int     `json:"maxToolResultChars,omitempty" yaml:"maxToolResultChars,omitempty"`
	MaxWorkspaceFileChars int     `json:"maxWorkspaceFileChars,omitempty" yaml:"maxWorkspaceFileChars,omitempty"`
}

// DefaultAgentLimits contains the default values for all agent limits.
// Populated from schema.json at init time (models.defaultLimits).
var DefaultAgentLimits AgentLimits

func mergeLimits(base, overrides AgentLimits) AgentLimits {
	merged := base
	if overrides.MaxToolRounds > 0 {
		merged.MaxToolRounds = overrides.MaxToolRounds
	}
	if overrides.CompressionThreshold > 0 {
		merged.CompressionThreshold = overrides.CompressionThreshold
	}
	if overrides.MinKeepMessages > 0 {
		merged.MinKeepMessages = overrides.MinKeepMessages
	}
	if overrides.MinKeepRecentTokens > 0 {
		merged.MinKeepRecentTokens = overrides.MinKeepRecentTokens
	}
	if overrides.MaxToolResultChars > 0 {
		merged.MaxToolResultChars = overrides.MaxToolResultChars
	}
	if overrides.MaxWorkspaceFileChars > 0 {
		merged.MaxWorkspaceFileChars = overrides.MaxWorkspaceFileChars
	}
	return merged
}

// SummarizerConfig controls the background session summarizer behavior.
// Time fields are in minutes. Zero values fall back to defaults.
type SummarizerConfig struct {
	TickInterval         int `json:"tickInterval,omitempty" yaml:"tickInterval,omitempty"`                 // how often the background loop runs (minutes)
	StartupDelay         int `json:"startupDelay,omitempty" yaml:"startupDelay,omitempty"`                 // delay before first run (minutes)
	InactivityTime       int `json:"inactivityTime,omitempty" yaml:"inactivityTime,omitempty"`             // session inactivity threshold (minutes)
	MinMessages          int `json:"minMessages,omitempty" yaml:"minMessages,omitempty"`                   // minimum messages required to summarize
	MaxConversationChars int `json:"maxConversationChars,omitempty" yaml:"maxConversationChars,omitempty"` // max chars of conversation text sent to the LLM
	MaxMessageChars      int `json:"maxMessageChars,omitempty" yaml:"maxMessageChars,omitempty"`           // max chars per individual message
}

// DefaultSummarizerConfig contains the default values for all summarizer settings.
// Populated from schema.json at init time.
var DefaultSummarizerConfig SummarizerConfig

// ResolveSummarizerConfig returns a SummarizerConfig with user overrides applied
// on top of the defaults. Zero-value fields fall back to DefaultSummarizerConfig.
func (self *Config) ResolveSummarizerConfig() SummarizerConfig {
	resolved := DefaultSummarizerConfig
	if self.Summarizer == nil {
		return resolved
	}
	if self.Summarizer.TickInterval > 0 {
		resolved.TickInterval = self.Summarizer.TickInterval
	}
	if self.Summarizer.StartupDelay > 0 {
		resolved.StartupDelay = self.Summarizer.StartupDelay
	}
	if self.Summarizer.InactivityTime > 0 {
		resolved.InactivityTime = self.Summarizer.InactivityTime
	}
	if self.Summarizer.MinMessages > 0 {
		resolved.MinMessages = self.Summarizer.MinMessages
	}
	if self.Summarizer.MaxConversationChars > 0 {
		resolved.MaxConversationChars = self.Summarizer.MaxConversationChars
	}
	if self.Summarizer.MaxMessageChars > 0 {
		resolved.MaxMessageChars = self.Summarizer.MaxMessageChars
	}
	return resolved
}

type Config struct {
	Gateway          GatewayConfig      `json:"gateway,omitempty" yaml:"gateway,omitempty"`
	Models           ModelsConfig       `json:"models,omitempty" yaml:"models,omitempty"`
	Tools            ToolsConfig        `json:"tools,omitempty" yaml:"tools,omitempty"`
	Secrets          map[string]string  `json:"secrets,omitempty" yaml:"secrets,omitempty"`
	Integrations     IntegrationsConfig `json:"integrations,omitempty" yaml:"integrations,omitempty"`
	Summarizer       *SummarizerConfig  `json:"summarizer,omitempty" yaml:"summarizer,omitempty"`
	SkillsRegistries []SkillsRegistry   `json:"skillsRegistries,omitempty" yaml:"skillsRegistries,omitempty"`
	Channels         ChannelsConfig     `json:"channels,omitempty" yaml:"channels,omitempty"`
	AgentConfigs     []AgentConfig      `json:"-" yaml:"-"`
}

type IntegrationsConfig struct {
	Browser  *BrowserConfig  `json:"browser,omitempty" yaml:"browser,omitempty"`
	Terminal *TerminalConfig `json:"terminal,omitempty" yaml:"terminal,omitempty"`
}

type BrowserConfig struct {
	CDPEndpoint string `json:"cdpEndpoint,omitempty" yaml:"cdpEndpoint,omitempty"` // e.g. "127.0.0.1:9222"
}

type TerminalConfig struct {
}

type ChannelsConfig struct {
	Discord  *DiscordConfig  `json:"discord,omitempty" yaml:"discord,omitempty"`
	Telegram *TelegramConfig `json:"telegram,omitempty" yaml:"telegram,omitempty"`
}

type DiscordConfig struct {
	Token string `json:"token,omitempty" yaml:"token,omitempty"`
}

type TelegramConfig struct {
	Token string `json:"token,omitempty" yaml:"token,omitempty"`
}

type SkillsRegistry struct {
	ID               string   `json:"id,omitempty" yaml:"id,omitempty"`
	Publisher        string   `json:"publisher,omitempty" yaml:"publisher,omitempty"`
	IndexURL         string   `json:"indexUrl,omitempty" yaml:"indexUrl,omitempty"`
	PublicKeys       []string `json:"publicKeys,omitempty" yaml:"publicKeys,omitempty"`
	IgnoreSignatures bool     `json:"ignoreSignatures,omitempty" yaml:"ignoreSignatures,omitempty"`
	IgnoreUpdates    bool     `json:"ignoreUpdates,omitempty" yaml:"ignoreUpdates,omitempty"`
}

type ToolsConfig struct {
	BraveAPIKey   string               `json:"braveApiKey,omitempty" yaml:"braveApiKey,omitempty"`
	Google        *GoogleConfig        `json:"google,omitempty" yaml:"google,omitempty"`
	GitHub        *GitHubConfig        `json:"github,omitempty" yaml:"github,omitempty"`
	GitLab        *GitLabConfig        `json:"gitlab,omitempty" yaml:"gitlab,omitempty"`
	ClaudeCode    *ClaudeCodeConfig    `json:"claudeCode,omitempty" yaml:"claudeCode,omitempty"`
	Codex         *CodexConfig         `json:"codex,omitempty" yaml:"codex,omitempty"`
	HomeAssistant *HomeAssistantConfig `json:"homeAssistant,omitempty" yaml:"homeAssistant,omitempty"`
	UniFiProtect  *UniFiProtectConfig  `json:"unifiProtect,omitempty" yaml:"unifiProtect,omitempty"`
}

// HomeAssistantConfig controls the Home Assistant smart home tool.
type HomeAssistantConfig struct {
	BaseURL         string   `json:"baseUrl,omitempty" yaml:"baseUrl,omitempty"`                 // e.g. "http://homeassistant.local:8123"
	Token           string   `json:"token,omitempty" yaml:"token,omitempty"`                     // long-lived access token
	ReadOnly        bool     `json:"readOnly,omitempty" yaml:"readOnly,omitempty"`               // block control/scene actions
	AllowedDomains  []string `json:"allowedDomains,omitempty" yaml:"allowedDomains,omitempty"`   // nil = safe defaults
	BlockedDomains  []string `json:"blockedDomains,omitempty" yaml:"blockedDomains,omitempty"`   // nil = safe defaults
	AllowedEntities []string `json:"allowedEntities,omitempty" yaml:"allowedEntities,omitempty"` // empty = all (subject to domain rules)
	TimeoutSeconds  int      `json:"timeoutSeconds,omitempty" yaml:"timeoutSeconds,omitempty"`   // default 10
}

// UniFiProtectConfig controls the UniFi Protect camera integration tool.
type UniFiProtectConfig struct {
	BaseURL               string   `json:"baseUrl,omitempty" yaml:"baseUrl,omitempty"`                             // e.g. "https://protect.local"
	APIKey                string   `json:"apiKey,omitempty" yaml:"apiKey,omitempty"`                               // X-API-Key header value
	Username              string   `json:"username,omitempty" yaml:"username,omitempty"`                           // fallback auth username
	Password              string   `json:"password,omitempty" yaml:"password,omitempty"`                           // fallback auth password
	VerifyTLS             bool     `json:"verifyTls,omitempty" yaml:"verifyTls,omitempty"`                         // verify TLS certificates
	ReadOnly              bool     `json:"readOnly,omitempty" yaml:"readOnly,omitempty"`                           // block write actions
	AllowedCameras        []string `json:"allowedCameras,omitempty" yaml:"allowedCameras,omitempty"`               // nil = all cameras
	AllowDangerousActions []string `json:"allowDangerousActions,omitempty" yaml:"allowDangerousActions,omitempty"` // e.g. ["set_privacy_mode"]
	TimeoutSeconds        int      `json:"timeoutSeconds,omitempty" yaml:"timeoutSeconds,omitempty"`               // default 15
}

// GoogleConfig controls Google Workspace tools powered by the gog CLI.
type GoogleConfig struct {
	BinaryPath string   `json:"binaryPath,omitempty" yaml:"binaryPath,omitempty"` // default: "gog"
	Account    string   `json:"account,omitempty" yaml:"account,omitempty"`       // gog --account flag
	Services   []string `json:"services,omitempty" yaml:"services,omitempty"`     // nil = tier 1 defaults
}

// GitHubConfig controls GitHub tools powered by the gh CLI.
type GitHubConfig struct {
	BinaryPath string   `json:"binaryPath,omitempty" yaml:"binaryPath,omitempty"` // default: "gh"
	Services   []string `json:"services,omitempty" yaml:"services,omitempty"`     // nil = tier 1 defaults
}

// GitLabConfig controls GitLab tools powered by the glab CLI.
type GitLabConfig struct {
	BinaryPath string   `json:"binaryPath,omitempty" yaml:"binaryPath,omitempty"` // default: "glab"
	Services   []string `json:"services,omitempty" yaml:"services,omitempty"`     // nil = tier 1 defaults
}

// ClaudeCodeConfig controls the Claude Code headless tool.
type ClaudeCodeConfig struct {
	BinaryPath            string   `json:"binaryPath,omitempty" yaml:"binaryPath,omitempty"`                       // default: "claude"
	AllowedTools          []string `json:"allowedTools,omitempty" yaml:"allowedTools,omitempty"`                   // tools Claude Code can use; nil = defaults
	Model                 string   `json:"model,omitempty" yaml:"model,omitempty"`                                 // model override for Claude Code
	MaxTurnTimeoutSeconds int      `json:"maxTurnTimeoutSeconds,omitempty" yaml:"maxTurnTimeoutSeconds,omitempty"` // per-call timeout (default 300, max 600)
}

// CodexConfig controls the Codex headless tool.
type CodexConfig struct {
	BinaryPath            string   `json:"binaryPath,omitempty" yaml:"binaryPath,omitempty"`                       // default: "codex"
	AllowedTools          []string `json:"allowedTools,omitempty" yaml:"allowedTools,omitempty"`                   // tools Codex can use; nil = defaults
	Model                 string   `json:"model,omitempty" yaml:"model,omitempty"`                                 // model override for Codex
	ExtraArgs             []string `json:"extraArgs,omitempty" yaml:"extraArgs,omitempty"`                         // additional CLI args appended to each invocation
	MaxTurnTimeoutSeconds int      `json:"maxTurnTimeoutSeconds,omitempty" yaml:"maxTurnTimeoutSeconds,omitempty"` // per-call timeout (default 300, max 1800)
}

type GatewayConfig struct {
	Port         int         `json:"port,omitempty" yaml:"port,omitempty"`
	Bind         string      `json:"bind,omitempty" yaml:"bind,omitempty"` // "loopback" | "lan"
	Auth         *AuthConfig `json:"auth,omitempty" yaml:"auth,omitempty"`
	ForwarderKey string      `json:"forwarderKey,omitempty" yaml:"forwarderKey,omitempty"`
	PublicURL    string      `json:"publicUrl,omitempty" yaml:"publicUrl,omitempty"` // external URL for LLM provider media fetching
}

type AuthConfig struct {
	SessionMaxAgeDays int `json:"sessionMaxAgeDays,omitempty" yaml:"sessionMaxAgeDays,omitempty"` // default 14
}

// ProviderConfig holds connection details for a single provider.
// Name must be one of the supported provider types: "openai", "anthropic", "openrouter".
type ProviderConfig struct {
	Name    string `json:"name,omitempty" yaml:"name,omitempty"` // "openai" | "anthropic" | "openrouter"
	BaseURL string `json:"baseUrl,omitempty" yaml:"baseUrl,omitempty"`
	APIKey  string `json:"apiKey,omitempty" yaml:"apiKey,omitempty"`
}

type ModelsConfig struct {
	Default         string             `json:"default,omitempty" yaml:"default,omitempty"`
	SummarizerModel string             `json:"summarizerModel,omitempty" yaml:"summarizerModel,omitempty"` // model for title + summary generation; defaults to Default
	ContextWindow   int                `json:"contextWindow,omitempty" yaml:"contextWindow,omitempty"`     // max tokens; defaults from model lookup when unset
	DefaultLimits   AgentLimits        `json:"defaultLimits,omitempty" yaml:"defaultLimits,omitempty"`     // default runtime limits applied to all models
	Limits          ModelRuntimeLimits `json:"limits,omitempty" yaml:"limits,omitempty"`                   // per-model runtime limits
	Providers       []ProviderConfig   `json:"providers,omitempty" yaml:"providers,omitempty"`
}

// ModelRuntimeLimitEntry stores per-model runtime limit overrides.
type ModelRuntimeLimitEntry struct {
	Model       string `json:"model,omitempty" yaml:"model,omitempty"`
	AgentLimits `json:",inline" yaml:",inline"`
}

// ModelRuntimeLimits stores per-model runtime limits as a list.
type ModelRuntimeLimits []ModelRuntimeLimitEntry

func (self *ModelRuntimeLimits) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		*self = nil
		return nil
	}

	var list []ModelRuntimeLimitEntry
	if err := json.Unmarshal(data, &list); err != nil {
		return err
	}
	*self = list
	return nil
}

func (self *ModelRuntimeLimits) UnmarshalYAML(value *yaml.Node) error {
	if value == nil || value.Kind == yaml.ScalarNode && (value.Value == "" || value.Value == "null") {
		*self = nil
		return nil
	}

	var list []ModelRuntimeLimitEntry
	if err := value.Decode(&list); err != nil {
		return err
	}
	*self = list
	return nil
}

// ResolvedProviders returns the providers list.
func (self *ModelsConfig) ResolvedProviders() []ProviderConfig {
	return self.Providers
}

// DefaultProviderName returns the name of the default provider.
// If the default model is qualified ("provider:model"), the provider part is
// returned; otherwise the first (or only) configured provider name is used.
func (self *ModelsConfig) DefaultProviderName() string {
	// Check if default model is qualified.
	if index := strings.IndexByte(self.Default, ':'); index >= 0 {
		return self.Default[:index]
	}
	// Legacy single-provider.
	if len(self.Providers) == 0 {
		return "openai"
	}
	// Multi-provider: pick the first entry.
	return self.Providers[0].Name
}

// --- Agent Config Helpers ---

// Agents returns the configured agents.
// It panics when no agents are configured because Load is expected to auto-create
// the default main agent.
func (self *Config) Agents() []AgentConfig {
	if len(self.AgentConfigs) == 0 {
		panic("config has no agents; Load must create default main agent")
	}
	return self.AgentConfigs
}

// DefaultAgentID returns the effective default agent ID.
// The first configured agent's ID is used.
func (self *Config) DefaultAgentID() string {
	for _, agent := range self.Agents() {
		if agent.ID == "main" {
			return agent.ID
		}
	}
	return self.Agents()[0].ID
}

// AgentByID returns the agent config for the given ID, or nil if not found.
func (self *Config) AgentByID(agentId string) *AgentConfig {
	for index := range self.AgentConfigs {
		if self.AgentConfigs[index].ID == agentId {
			return &self.AgentConfigs[index]
		}
	}
	return nil
}

// AgentModel returns the qualified model for an agent. If the agent has a
// per-agent model override it is returned; otherwise the global default is used.
func (self *Config) AgentModel(agentId string) string {
	if agentConfig := self.AgentByID(agentId); agentConfig != nil && agentConfig.Model != "" {
		return agentConfig.Model
	}
	return self.Models.Default
}

func (self *ModelsConfig) modelLimits(model string) (AgentLimits, bool) {
	if len(self.Limits) == 0 || model == "" {
		return AgentLimits{}, false
	}
	limitsByModel := make(map[string]AgentLimits, len(self.Limits))
	for _, entry := range self.Limits {
		if entry.Model == "" {
			continue
		}
		limitsByModel[entry.Model] = mergeLimits(limitsByModel[entry.Model], entry.AgentLimits)
	}
	if limits, ok := limitsByModel[model]; ok {
		return limits, true
	}
	defaultProvider := self.DefaultProviderName()
	providerName, bareModel := providers.ParseQualifiedModel(model, defaultProvider)
	qualifiedModel := providers.QualifyModel(providerName, bareModel)
	if limits, ok := limitsByModel[qualifiedModel]; ok {
		return limits, true
	}
	if limits, ok := limitsByModel[bareModel]; ok {
		return limits, true
	}
	return AgentLimits{}, false
}

// ResolveModelLimits returns runtime limits for a model.
// Merge order: built-in defaults -> models.defaultLimits -> models.limits entry.
func (self *Config) ResolveModelLimits(model string) AgentLimits {
	limits := mergeLimits(DefaultAgentLimits, self.Models.DefaultLimits)
	if modelLimits, ok := self.Models.modelLimits(model); ok {
		limits = mergeLimits(limits, modelLimits)
	}
	return limits
}

// IsAllowed checks whether a name is present in an allow list.
// A nil or empty list means everything is allowed (preserving defaults).
// Only an explicitly populated list restricts access.
func IsAllowed(name string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, entry := range allowed {
		if entry == name {
			return true
		}
	}
	return false
}

// EnsureDirectories creates the base teanode directories if needed.
func EnsureDirectories() error {
	for _, sub := range []string{"skills", "projects", "media", "agents", "users", "sessions", ".trash"} {
		if err := os.MkdirAll(filepath.Join(configDirectory, sub), 0755); err != nil {
			return fmt.Errorf("creating directories: %w", err)
		}
	}
	return nil
}

// EnsureAgentDirectories creates the workspace directory for an agent.
func EnsureAgentDirectories(agentId string) error {
	workspaceDirectory := AgentWorkspaceDirectory(agentId)
	for _, directory := range []string{
		workspaceDirectory,
		filepath.Join(workspaceDirectory, "memory"),
	} {
		if err := os.MkdirAll(directory, 0755); err != nil {
			return fmt.Errorf("creating agent directory %s: %w", directory, err)
		}
	}
	return nil
}

// EnsureUserDirectories creates the standard multi-user directories.
func EnsureUserDirectories(userId string) error {
	userDirectory := UserDirectory(userId)
	workspaceDirectory := UserWorkspaceDirectory(userId)
	conversationsDirectory := UserConversationsDirectory(userId)
	jobsDirectory := UserJobsDirectory(userId)
	for _, directory := range []string{
		userDirectory,
		workspaceDirectory,
		filepath.Join(workspaceDirectory, "memory"),
		conversationsDirectory,
		jobsDirectory,
	} {
		if err := os.MkdirAll(directory, 0755); err != nil {
			return fmt.Errorf("creating user directory %s: %w", directory, err)
		}
	}
	if err := SeedUserWorkspace(userId); err != nil {
		return err
	}
	return nil
}

// SeedAgentWorkspace writes default AGENT.md and MEMORY.md if they don't exist
// in the agent's workspace directory.
func SeedAgentWorkspace(agentId string) error {
	workspaceDirectory := AgentWorkspaceDirectory(agentId)

	seeds := map[string]string{
		"AGENT.md":  prompts.DefaultAgentMarkdown(),
		"MEMORY.md": prompts.DefaultMemoryMarkdown(),
		"SKILLS.md": prompts.DefaultSkillsMarkdown(),
	}
	for name, content := range seeds {
		path := filepath.Join(workspaceDirectory, name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			if err := atomicfile.WriteFile(path, []byte(content)); err != nil {
				return fmt.Errorf("seeding %s: %w", name, err)
			}
		}
	}
	return nil
}

// SeedUserWorkspace writes default user files in the user's workspace directory.
// ONBOARDING.md is only created when USER.md is created in the same operation.
func SeedUserWorkspace(userId string) error {
	workspaceDirectory := UserWorkspaceDirectory(userId)

	userMdPath := filepath.Join(workspaceDirectory, "USER.md")
	userCreated := false
	if _, err := os.Stat(userMdPath); os.IsNotExist(err) {
		if err := atomicfile.WriteFile(userMdPath, []byte(prompts.DefaultUserMarkdown())); err != nil {
			return fmt.Errorf("seeding USER.md: %w", err)
		}
		userCreated = true
	}

	if userCreated {
		onboardingPath := filepath.Join(workspaceDirectory, "ONBOARDING.md")
		if _, err := os.Stat(onboardingPath); os.IsNotExist(err) {
			if err := atomicfile.WriteFile(onboardingPath, []byte(prompts.DefaultOnboardingMarkdown())); err != nil {
				return fmt.Errorf("seeding ONBOARDING.md: %w", err)
			}
		}
	}

	memoryPath := filepath.Join(workspaceDirectory, "MEMORY.md")
	if _, err := os.Stat(memoryPath); os.IsNotExist(err) {
		if err := atomicfile.WriteFile(memoryPath, []byte(prompts.DefaultMemoryMarkdown())); err != nil {
			return fmt.Errorf("seeding MEMORY.md: %w", err)
		}
	}
	return nil
}

// DeleteAgent removes the agents/<agentId>/ directory.
func DeleteAgent(agentId string) error {
	agentsDirectory := AgentsDirectory()
	agentDirectory := filepath.Join(agentsDirectory, agentId)
	if _, err := os.Stat(agentDirectory); os.IsNotExist(err) {
		return fmt.Errorf("agent not found: %s", agentId)
	}
	workspaceDirectory := AgentWorkspaceDirectory(agentId)
	trashDirectory := TrashDirectory()

	paths := []string{agentDirectory, workspaceDirectory}
	usersDirectory := UsersDirectory()
	var err error
	userEntries, err := os.ReadDir(usersDirectory)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, userEntry := range userEntries {
		if !userEntry.IsDir() {
			continue
		}
		paths = append(paths, filepath.Join(usersDirectory, userEntry.Name(), "conversations", agentId))
	}

	for _, path := range paths {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return err
		}
		if err := trash.Move(path, trashDirectory); err != nil {
			return err
		}
	}
	return nil
}

// LoadRaw reads config from ~/.teanode/config.yaml without applying defaults
// or environment overrides. Returns only what the user explicitly set in the file.
func LoadRaw() (*Config, error) {
	configuration := &Config{}
	data, err := os.ReadFile(ConfigFilename())
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	if err == nil {
		if err := yaml.Unmarshal(data, configuration); err != nil {
			return nil, fmt.Errorf("parsing config: %w", err)
		}
	}

	return configuration, nil
}

// Load reads config from ~/.teanode/config.yaml and applies defaults and env overrides.
func Load() (*Config, error) {
	configuration := defaults()

	data, err := os.ReadFile(ConfigFilename())
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	if err == nil {
		if err := yaml.Unmarshal(data, configuration); err != nil {
			return nil, fmt.Errorf("parsing config: %w", err)
		}
	}

	applyDefaults(configuration)
	applyEnv(configuration)

	// Load agents from per-agent files.
	agents, err := LoadAgentConfigs()
	if err != nil {
		return nil, fmt.Errorf("loading agents: %w", err)
	}
	if len(agents) == 0 {
		// Auto-create the default main agent.
		defaultAgent := AgentConfig{ID: "main", Name: "Tea"}
		if err := SaveAgentConfig(defaultAgent.ID, &defaultAgent); err != nil {
			return nil, fmt.Errorf("saving default agent: %w", err)
		}
		agents = []AgentConfig{defaultAgent}
	}
	configuration.AgentConfigs = agents

	return configuration, nil
}

// Save writes the config to ~/.teanode/config.yaml atomically.
func Save(configuration *Config) error {
	data, err := yaml.Marshal(configuration)
	if err != nil {
		return fmt.Errorf("marshalling config: %w", err)
	}
	return atomicfile.WriteFile(ConfigFilename(), data)
}

func defaults() *Config {
	configDefaults := parseSchemaDefaults(configSchemaJson)
	defaultModel := "openai:gpt-5.2"
	return &Config{
		Gateway: GatewayConfig{
			Port: schemaInt(configDefaults, "gateway.port"),
			Bind: schemaString(configDefaults, "gateway.bind"),
		},
		Integrations: IntegrationsConfig{
			Browser: &BrowserConfig{
				CDPEndpoint: schemaString(configDefaults, "integrations.browser.cdpEndpoint"),
			},
		},
		Models: ModelsConfig{
			Default:       defaultModel,
			ContextWindow: defaultContextWindowForModel(defaultModel),
			DefaultLimits: DefaultAgentLimits,
		},
		SkillsRegistries: []SkillsRegistry{
			{
				ID:        "teanode-official",
				Publisher: "github.com/teanode/teanode-skills",
				IndexURL:  "https://raw.githubusercontent.com/teanode/teanode-skills/main/index.json",
				PublicKeys: []string{
					"lPFKUpWbq3G1EykDv6SvsAACW0W/FZUaPiRyFlmEfj4=",
				},
			},
		},
	}
}

// applyDefaults fills in zero-valued required fields with their defaults.
// This runs after YAML unmarshal to guard against config files that explicitly
// set fields to empty strings or zero (e.g. "default: """).
func applyDefaults(configuration *Config) {
	fallback := defaults()

	if configuration.Gateway.Port <= 0 {
		configuration.Gateway.Port = fallback.Gateway.Port
	}
	if configuration.Gateway.Bind == "" {
		configuration.Gateway.Bind = fallback.Gateway.Bind
	}
	if configuration.Models.Default == "" {
		configuration.Models.Default = fallback.Models.Default
	}
	// Default provider name to "openai" if missing.
	for index := range configuration.Models.Providers {
		if configuration.Models.Providers[index].Name == "" {
			configuration.Models.Providers[index].Name = "openai"
		}
	}
	if configuration.Models.ContextWindow <= 0 {
		configuration.Models.ContextWindow = defaultContextWindowForModel(configuration.Models.Default)
	}
	configuration.Models.DefaultLimits = mergeLimits(fallback.Models.DefaultLimits, configuration.Models.DefaultLimits)

	if len(configuration.SkillsRegistries) == 0 {
		configuration.SkillsRegistries = fallback.SkillsRegistries
	}
}

// Model context windows keyed by bare model name (provider-agnostic).
// Example: both "openai:gpt-5.2" and "openrouter:gpt-5.2" resolve via "gpt-5.2".
var modelContextWindowDefaults = map[string]int{
	// OpenAI API model pages
	"gpt-5.2":      272000,
	"gpt-5.1":      272000,
	"gpt-4.1":      1047576,
	"gpt-4.1-mini": 1047576,
	"gpt-4o":       128000,
	"gpt-4o-mini":  128000,

	// Anthropic model overview (default context; 1M is beta for select models with header)
	"claude-opus-4-6":   200000,
	"claude-sonnet-4-5": 200000,
	"claude-haiku-4-5":  200000,

	// Gemini API model pages
	"google/gemini-2.5-pro":        1048576,
	"google/gemini-2.5-flash":      1048576,
	"google/gemini-2.5-flash-lite": 1048576,
}

func defaultContextWindowForModel(model string) int {
	const fallbackContextWindow = 128000
	model = strings.TrimSpace(model)
	if model == "" {
		return fallbackContextWindow
	}
	_, bareModel := providers.ParseQualifiedModel(model, "openai")
	if contextWindow, ok := modelContextWindowDefaults[bareModel]; ok {
		return contextWindow
	}
	return fallbackContextWindow
}

func applyEnv(configuration *Config) {
	if value := os.Getenv("TEANODE_GATEWAY_PORT"); value != "" {
		if port, err := strconv.Atoi(value); err == nil {
			configuration.Gateway.Port = port
		}
	}
	if value := os.Getenv("TEANODE_GATEWAY_BIND"); value != "" {
		configuration.Gateway.Bind = value
	}
	if value := os.Getenv("BRAVE_API_KEY"); value != "" {
		configuration.Tools.BraveAPIKey = value
	}
	if value := os.Getenv("TEANODE_CONTEXT_WINDOW"); value != "" {
		if contextWindow, err := strconv.Atoi(value); err == nil {
			configuration.Models.ContextWindow = contextWindow
		}
	}
	if value := os.Getenv("DISCORD_BOT_TOKEN"); value != "" {
		if configuration.Channels.Discord == nil {
			configuration.Channels.Discord = &DiscordConfig{}
		}
		configuration.Channels.Discord.Token = value
	}
	if value := os.Getenv("TELEGRAM_BOT_TOKEN"); value != "" {
		if configuration.Channels.Telegram == nil {
			configuration.Channels.Telegram = &TelegramConfig{}
		}
		configuration.Channels.Telegram.Token = value
	}
	if value := os.Getenv("GOG_BINARY_PATH"); value != "" {
		if configuration.Tools.Google == nil {
			configuration.Tools.Google = &GoogleConfig{}
		}
		configuration.Tools.Google.BinaryPath = value
	}
	if value := os.Getenv("GH_BINARY_PATH"); value != "" {
		if configuration.Tools.GitHub == nil {
			configuration.Tools.GitHub = &GitHubConfig{}
		}
		configuration.Tools.GitHub.BinaryPath = value
	}
	if value := os.Getenv("GLAB_BINARY_PATH"); value != "" {
		if configuration.Tools.GitLab == nil {
			configuration.Tools.GitLab = &GitLabConfig{}
		}
		configuration.Tools.GitLab.BinaryPath = value
	}
	if value := os.Getenv("CLAUDE_CODE_BINARY_PATH"); value != "" {
		if configuration.Tools.ClaudeCode == nil {
			configuration.Tools.ClaudeCode = &ClaudeCodeConfig{}
		}
		configuration.Tools.ClaudeCode.BinaryPath = value
	}
	if value := os.Getenv("CODEX_BINARY_PATH"); value != "" {
		if configuration.Tools.Codex == nil {
			configuration.Tools.Codex = &CodexConfig{}
		}
		configuration.Tools.Codex.BinaryPath = value
	}
	if value := os.Getenv("OPENAI_API_KEY"); value != "" {
		found := false
		for index := range configuration.Models.Providers {
			if configuration.Models.Providers[index].Name == "openai" {
				configuration.Models.Providers[index].APIKey = value
				if configuration.Models.Providers[index].BaseURL == "" {
					configuration.Models.Providers[index].BaseURL = "https://api.openai.com/v1"
				}
				found = true
				break
			}
		}
		if !found {
			configuration.Models.Providers = append(configuration.Models.Providers, ProviderConfig{
				Name:    "openai",
				BaseURL: "https://api.openai.com/v1",
				APIKey:  value,
			})
		}
	}
	if value := os.Getenv("ANTHROPIC_API_KEY"); value != "" {
		found := false
		for index := range configuration.Models.Providers {
			if configuration.Models.Providers[index].Name == "anthropic" {
				configuration.Models.Providers[index].APIKey = value
				if configuration.Models.Providers[index].BaseURL == "" {
					configuration.Models.Providers[index].BaseURL = "https://api.anthropic.com/v1"
				}
				found = true
				break
			}
		}
		if !found {
			configuration.Models.Providers = append(configuration.Models.Providers, ProviderConfig{
				Name:    "anthropic",
				BaseURL: "https://api.anthropic.com/v1",
				APIKey:  value,
			})
		}
	}
	if value := os.Getenv("OPENROUTER_API_KEY"); value != "" {
		found := false
		for index := range configuration.Models.Providers {
			if configuration.Models.Providers[index].Name == "openrouter" {
				configuration.Models.Providers[index].APIKey = value
				if configuration.Models.Providers[index].BaseURL == "" {
					configuration.Models.Providers[index].BaseURL = "https://openrouter.ai/api/v1"
				}
				found = true
				break
			}
		}
		if !found {
			configuration.Models.Providers = append(configuration.Models.Providers, ProviderConfig{
				Name:    "openrouter",
				BaseURL: "https://openrouter.ai/api/v1",
				APIKey:  value,
			})
		}
	}
	if value := os.Getenv("TEANODE_PUBLIC_URL"); value != "" {
		configuration.Gateway.PublicURL = value
	}
	if value := os.Getenv("HASS_BASE_URL"); value != "" {
		if configuration.Tools.HomeAssistant == nil {
			configuration.Tools.HomeAssistant = &HomeAssistantConfig{}
		}
		configuration.Tools.HomeAssistant.BaseURL = value
	}
	if value := os.Getenv("HASS_TOKEN"); value != "" {
		if configuration.Tools.HomeAssistant == nil {
			configuration.Tools.HomeAssistant = &HomeAssistantConfig{}
		}
		configuration.Tools.HomeAssistant.Token = value
	}
	if value := os.Getenv("TEANODE_CDP_ENDPOINT"); value != "" {
		if configuration.Integrations.Browser == nil {
			configuration.Integrations.Browser = &BrowserConfig{}
		}
		configuration.Integrations.Browser.CDPEndpoint = value
	}
	if value := os.Getenv("UNIFI_PROTECT_BASE_URL"); value != "" {
		if configuration.Tools.UniFiProtect == nil {
			configuration.Tools.UniFiProtect = &UniFiProtectConfig{}
		}
		configuration.Tools.UniFiProtect.BaseURL = value
	}
	if value := os.Getenv("UNIFI_PROTECT_API_KEY"); value != "" {
		if configuration.Tools.UniFiProtect == nil {
			configuration.Tools.UniFiProtect = &UniFiProtectConfig{}
		}
		configuration.Tools.UniFiProtect.APIKey = value
	}
	if value := os.Getenv("UNIFI_PROTECT_USERNAME"); value != "" {
		if configuration.Tools.UniFiProtect == nil {
			configuration.Tools.UniFiProtect = &UniFiProtectConfig{}
		}
		configuration.Tools.UniFiProtect.Username = value
	}
	if value := os.Getenv("UNIFI_PROTECT_PASSWORD"); value != "" {
		if configuration.Tools.UniFiProtect == nil {
			configuration.Tools.UniFiProtect = &UniFiProtectConfig{}
		}
		configuration.Tools.UniFiProtect.Password = value
	}
}
