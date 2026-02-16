// Package config handles loading and watching the teanode configuration file.
package configs

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/teanode/teanode/internal/util/atomicfile"
	"gopkg.in/yaml.v3"
)

//go:embed default_agent.md
var defaultAgentMD string

//go:embed default_memory.md
var defaultMemoryMD string

//go:embed default_skills.md
var defaultSkillsMD string

//go:embed schema.json
var configSchemaJSON []byte

//go:embed agent_schema.json
var agentSchemaJSON []byte

// ConfigSchema returns the embedded config schema JSON for UI form generation.
func ConfigSchema() json.RawMessage {
	return json.RawMessage(configSchemaJSON)
}

// AgentConfigSchema returns the embedded agent config schema JSON for UI form generation.
func AgentConfigSchema() json.RawMessage {
	return json.RawMessage(agentSchemaJSON)
}

// --- Schema-driven defaults ---

// parseSchemaDefaults extracts dot-path→default pairs from a JSON Schema.
// It recursively walks the "properties" tree, building dot-paths from the
// nesting structure, and collects every "default" value it encounters.
func parseSchemaDefaults(schemaJSON []byte) map[string]interface{} {
	var schema map[string]interface{}
	json.Unmarshal(schemaJSON, &schema)
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
	configDefaults := parseSchemaDefaults(configSchemaJSON)
	agentDefaults := parseSchemaDefaults(agentSchemaJSON)

	DefaultAgentLimits = AgentLimits{
		MaxToolRounds:         schemaInt(agentDefaults, "maxToolRounds"),
		CompressionThreshold:  schemaFloat64(agentDefaults, "compressionThreshold"),
		MinKeepMessages:       schemaInt(agentDefaults, "minKeepMessages"),
		MaxToolResultChars:    schemaInt(agentDefaults, "maxToolResultChars"),
		MaxWorkspaceFileChars: schemaInt(agentDefaults, "maxWorkspaceFileChars"),
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

// DefaultAgentID is the ID of the default agent when no agents are configured.
const DefaultAgentID = "main"

// AgentConfig defines a single agent in the multi-agent system.
type AgentConfig struct {
	ID                    string        `json:"id" yaml:"id"`                                                           // unique; "main" is default
	Name                  string        `json:"name,omitempty" yaml:"name,omitempty"`                                   // friendly display name
	Model                 string        `json:"model,omitempty" yaml:"model,omitempty"`                                 // qualified model override (e.g. "openai:gpt-4o")
	SystemPrompt          string        `json:"systemPrompt,omitempty" yaml:"systemPrompt,omitempty"`                   // per-agent identity line override
	Skills                []string `json:"skills,omitempty" yaml:"skills,omitempty"`                                    // skill allow list (nil = all)
	Tools                 []string `json:"tools,omitempty" yaml:"tools,omitempty"`                                     // tool allow list (nil = all)
	CanMessage            []string      `json:"canMessage,omitempty" yaml:"canMessage,omitempty"`                       // agent IDs this agent can talk to; "*" = all
	MaxToolRounds         int           `json:"maxToolRounds,omitempty" yaml:"maxToolRounds,omitempty"`                 // max tool-call loop iterations
	CompressionThreshold  float64       `json:"compressionThreshold,omitempty" yaml:"compressionThreshold,omitempty"`   // context compression ratio (0-1)
	MinKeepMessages       int           `json:"minKeepMessages,omitempty" yaml:"minKeepMessages,omitempty"`             // min recent messages to preserve
	MaxToolResultChars    int           `json:"maxToolResultChars,omitempty" yaml:"maxToolResultChars,omitempty"`       // max chars per old tool result
	MaxWorkspaceFileChars int           `json:"maxWorkspaceFileChars,omitempty" yaml:"maxWorkspaceFileChars,omitempty"` // max chars per workspace file in prompt
}

// AgentLimits holds resolved runtime limits for an agent.
type AgentLimits struct {
	MaxToolRounds         int
	CompressionThreshold  float64
	MinKeepMessages       int
	MaxToolResultChars    int
	MaxWorkspaceFileChars int
}

// DefaultAgentLimits contains the default values for all agent limits.
// Populated from agent_schema.json at init time.
var DefaultAgentLimits AgentLimits

// ResolveLimits returns an AgentLimits with per-agent overrides applied on top
// of the defaults. Zero-value fields fall back to DefaultAgentLimits.
func (self *AgentConfig) ResolveLimits() AgentLimits {
	limits := DefaultAgentLimits
	if self.MaxToolRounds > 0 {
		limits.MaxToolRounds = self.MaxToolRounds
	}
	if self.CompressionThreshold > 0 {
		limits.CompressionThreshold = self.CompressionThreshold
	}
	if self.MinKeepMessages > 0 {
		limits.MinKeepMessages = self.MinKeepMessages
	}
	if self.MaxToolResultChars > 0 {
		limits.MaxToolResultChars = self.MaxToolResultChars
	}
	if self.MaxWorkspaceFileChars > 0 {
		limits.MaxWorkspaceFileChars = self.MaxWorkspaceFileChars
	}
	return limits
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
	Gateway      GatewayConfig      `json:"gateway" yaml:"gateway"`
	Models       ModelsConfig       `json:"models" yaml:"models"`
	Tools        ToolsConfig        `json:"tools,omitempty" yaml:"tools,omitempty"`
	Integrations IntegrationsConfig `json:"integrations,omitempty" yaml:"integrations,omitempty"`
	Summarizer   *SummarizerConfig  `json:"summarizer,omitempty" yaml:"summarizer,omitempty"`
	SystemPrompt string             `json:"systemPrompt,omitempty" yaml:"systemPrompt,omitempty"`
	DefaultAgent string             `json:"defaultAgent,omitempty" yaml:"defaultAgent,omitempty"` // defaults to first configured agent
	Channels     ChannelsConfig     `json:"channels,omitempty" yaml:"channels,omitempty"`
	Agents       []AgentConfig      `json:"-" yaml:"-"`
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
	Token        string   `json:"token,omitempty" yaml:"token,omitempty"`
	AllowedUsers []string `json:"allowedUsers,omitempty" yaml:"allowedUsers,omitempty"` // Discord user IDs
	AgentID      string   `json:"agentId,omitempty" yaml:"agentId,omitempty"`           // defaults to the configured default agent
}

type TelegramConfig struct {
	Token        string  `json:"token,omitempty" yaml:"token,omitempty"`
	AllowedUsers []int64 `json:"allowedUsers,omitempty" yaml:"allowedUsers,omitempty"` // Telegram user IDs
	AgentID      string  `json:"agentId,omitempty" yaml:"agentId,omitempty"`           // defaults to the configured default agent
}

type ToolsConfig struct {
	BraveAPIKey string `json:"braveApiKey,omitempty" yaml:"braveApiKey,omitempty"`
}

type GatewayConfig struct {
	Port int         `json:"port" yaml:"port"`
	Bind string      `json:"bind" yaml:"bind"` // "loopback" | "lan"
	Auth *AuthConfig `json:"auth,omitempty" yaml:"auth,omitempty"`
}

type AuthConfig struct {
	Token    string `json:"token,omitempty" yaml:"token,omitempty"`
	Password string `json:"password,omitempty" yaml:"password,omitempty"`
}

// ProviderConfig holds connection details for a single provider.
type ProviderConfig struct {
	BaseURL string `json:"baseUrl" yaml:"baseUrl"`
	APIKey  string `json:"apiKey,omitempty" yaml:"apiKey,omitempty"`
}

type ModelsConfig struct {
	Default         string                    `json:"default" yaml:"default"`
	SummarizerModel string                    `json:"summarizerModel,omitempty" yaml:"summarizerModel,omitempty"` // model for title + summary generation; defaults to Default
	ContextWindow   int                       `json:"contextWindow,omitempty" yaml:"contextWindow,omitempty"`     // max tokens; default 128000
	Providers       map[string]ProviderConfig `json:"providers,omitempty" yaml:"providers,omitempty"`             // multi-provider config

	// Legacy single-provider fields (used if Providers is empty)
	Provider string `json:"provider,omitempty" yaml:"provider,omitempty"`
	BaseURL  string `json:"baseUrl,omitempty" yaml:"baseUrl,omitempty"`
	APIKey   string `json:"apiKey,omitempty" yaml:"apiKey,omitempty"`
}

// ResolvedProviders returns the providers map. If the new Providers field is
// populated it is returned directly; otherwise a single-entry map is
// synthesized from the legacy Provider/BaseURL/APIKey fields.
func (self *ModelsConfig) ResolvedProviders() map[string]ProviderConfig {
	if len(self.Providers) > 0 {
		return self.Providers
	}
	name := self.Provider
	if name == "" {
		name = "openai"
	}
	return map[string]ProviderConfig{
		name: {BaseURL: self.BaseURL, APIKey: self.APIKey},
	}
}

// DefaultProviderName returns the name of the default provider.
// If the default model is qualified ("provider:model"), the provider part is
// returned; otherwise the first (or only) configured provider name is used.
func (self *ModelsConfig) DefaultProviderName() string {
	// Check if default model is qualified.
	if idx := strings.IndexByte(self.Default, ':'); idx >= 0 {
		return self.Default[:idx]
	}
	// Legacy single-provider.
	if len(self.Providers) == 0 {
		if self.Provider != "" {
			return self.Provider
		}
		return "openai"
	}
	// Multi-provider: pick the first key (deterministic for single-entry maps).
	for name := range self.Providers {
		return name
	}
	return "openai"
}

// --- Agent Config Helpers ---

// ResolveAgents returns the configured agents, or a default single "main" agent
// if no agents are explicitly configured.
func (self *Config) ResolveAgents() []AgentConfig {
	if len(self.Agents) > 0 {
		return self.Agents
	}
	return []AgentConfig{{ID: DefaultAgentID, CanMessage: []string{"*"}}}
}

// AgentByID returns the agent config for the given ID, or nil if not found.
func (self *Config) AgentByID(agentId string) *AgentConfig {
	for index := range self.Agents {
		if self.Agents[index].ID == agentId {
			return &self.Agents[index]
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

// ResolveDefaultAgent returns the effective default agent ID.
// If DefaultAgent is set and matches a configured agent, it is returned.
// Otherwise, the first configured agent's ID is used.
// If no agents are configured, DefaultAgentID is returned as a fallback.
func (self *Config) ResolveDefaultAgent() string {
	if self.DefaultAgent != "" {
		for _, agentConfig := range self.Agents {
			if agentConfig.ID == self.DefaultAgent {
				return self.DefaultAgent
			}
		}
	}
	if len(self.Agents) > 0 {
		return self.Agents[0].ID
	}
	return DefaultAgentID
}

// IsAllowed checks whether a name is present in an allow list.
// A nil list means everything is allowed.
func IsAllowed(name string, allowed []string) bool {
	if allowed == nil {
		return true
	}
	for _, entry := range allowed {
		if entry == name {
			return true
		}
	}
	return false
}

// --- Directory Functions ---

var overrideDirectory string

// SetDirectory overrides the data directory. Must be called before any other
// config functions (EnsureDirs, Load, etc.).
func SetDirectory(directory string) {
	overrideDirectory = directory
}

// Directory returns the teanode data directory (default ~/.teanode).
func Directory() (string, error) {
	if overrideDirectory != "" {
		return overrideDirectory, nil
	}
	if value := os.Getenv("TEANODE_DIR"); value != "" {
		return value, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".teanode"), nil
}

// JobsDirectory returns the path to the jobs directory (~/.teanode/jobs/).
func JobsDirectory() (string, error) {
	directory, err := Directory()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "jobs"), nil
}

// AgentsDirectory returns the agents config directory (~/.teanode/agents/).
func AgentsDirectory() (string, error) {
	directory, err := Directory()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "agents"), nil
}

// AgentWorkspaceDirectory returns the workspace directory for an agent (~/.teanode/workspaces/<agentId>/).
func AgentWorkspaceDirectory(agentId string) (string, error) {
	directory, err := Directory()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "workspaces", agentId), nil
}

// AgentConversationsDirectory returns the conversations directory for an agent (~/.teanode/conversations/<agentId>/).
func AgentConversationsDirectory(agentId string) (string, error) {
	directory, err := Directory()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "conversations", agentId), nil
}

// SkillsDirectory returns the skills directory (~/.teanode/skills).
func SkillsDirectory() (string, error) {
	directory, err := Directory()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "skills"), nil
}

// ModelsFile returns the path to the models cache file (~/.teanode/models.yaml).
func ModelsFile() (string, error) {
	directory, err := Directory()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "models.yaml"), nil
}

// MediaDirectory returns the media directory (~/.teanode/media).
func MediaDirectory() (string, error) {
	directory, err := Directory()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "media"), nil
}

// EnsureDirectories creates the base teanode directories if needed.
func EnsureDirectories() error {
	directory, err := Directory()
	if err != nil {
		return err
	}
	for _, sub := range []string{"conversations", "workspaces", "skills", "media", "agents", "jobs"} {
		if err := os.MkdirAll(filepath.Join(directory, sub), 0755); err != nil {
			return fmt.Errorf("creating directories: %w", err)
		}
	}
	return nil
}

// EnsureAgentDirectories creates the workspace and conversations directories for an agent.
func EnsureAgentDirectories(agentId string) error {
	workspaceDirectory, err := AgentWorkspaceDirectory(agentId)
	if err != nil {
		return err
	}
	conversationsDirectory, err := AgentConversationsDirectory(agentId)
	if err != nil {
		return err
	}
	for _, directory := range []string{
		workspaceDirectory,
		filepath.Join(workspaceDirectory, "memory"),
		conversationsDirectory,
	} {
		if err := os.MkdirAll(directory, 0755); err != nil {
			return fmt.Errorf("creating agent directory %s: %w", directory, err)
		}
	}
	return nil
}

// SeedAgentWorkspace writes default AGENT.md and MEMORY.md if they don't exist
// in the agent's workspace directory.
func SeedAgentWorkspace(agentId string) error {
	workspaceDirectory, err := AgentWorkspaceDirectory(agentId)
	if err != nil {
		return err
	}

	seeds := map[string]string{
		"AGENT.md":  defaultAgentMD,
		"MEMORY.md": defaultMemoryMD,
		"SKILLS.md": defaultSkillsMD,
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

// --- Per-Agent File Operations ---

// LoadAgents walks agents/*/config.yaml and returns all agent configs.
func LoadAgents() ([]AgentConfig, error) {
	agentsDirectory, err := AgentsDirectory()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(agentsDirectory)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading agents directory: %w", err)
	}

	var agents []AgentConfig
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		configPath := filepath.Join(agentsDirectory, entry.Name(), "config.yaml")
		data, err := os.ReadFile(configPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("reading agent config %s: %w", entry.Name(), err)
		}
		var agentConfig AgentConfig
		if err := yaml.Unmarshal(data, &agentConfig); err != nil {
			return nil, fmt.Errorf("parsing agent config %s: %w", entry.Name(), err)
		}
		// Ensure the ID matches the directory name.
		agentConfig.ID = entry.Name()
		agents = append(agents, agentConfig)
	}
	return agents, nil
}

// SaveAgent writes an agent config to agents/<id>/config.yaml atomically.
func SaveAgent(agentConfig AgentConfig) error {
	agentsDirectory, err := AgentsDirectory()
	if err != nil {
		return err
	}
	agentDirectory := filepath.Join(agentsDirectory, agentConfig.ID)
	if err := os.MkdirAll(agentDirectory, 0755); err != nil {
		return fmt.Errorf("creating agent directory: %w", err)
	}
	data, err := yaml.Marshal(agentConfig)
	if err != nil {
		return fmt.Errorf("marshalling agent config: %w", err)
	}
	return atomicfile.WriteFile(filepath.Join(agentDirectory, "config.yaml"), data)
}

// DeleteAgent removes the agents/<agentId>/ directory.
func DeleteAgent(agentId string) error {
	agentsDirectory, err := AgentsDirectory()
	if err != nil {
		return err
	}
	agentDirectory := filepath.Join(agentsDirectory, agentId)
	if _, err := os.Stat(agentDirectory); os.IsNotExist(err) {
		return fmt.Errorf("agent not found: %s", agentId)
	}
	return os.RemoveAll(agentDirectory)
}

// LoadRaw reads config from ~/.teanode/config.yaml without applying defaults
// or environment overrides. Returns only what the user explicitly set in the file.
func LoadRaw() (*Config, error) {
	directory, err := Directory()
	if err != nil {
		return nil, err
	}

	configuration := &Config{}
	data, err := os.ReadFile(filepath.Join(directory, "config.yaml"))
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

	directory, err := Directory()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(filepath.Join(directory, "config.yaml"))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	if err == nil {
		if err := yaml.Unmarshal(data, configuration); err != nil {
			return nil, fmt.Errorf("parsing config: %w", err)
		}
	}

	applyEnv(configuration)

	// Load agents from per-agent files.
	agents, err := LoadAgents()
	if err != nil {
		return nil, fmt.Errorf("loading agents: %w", err)
	}
	if len(agents) == 0 {
		// Auto-create the default main agent with full messaging permissions.
		defaultAgent := AgentConfig{ID: DefaultAgentID, CanMessage: []string{"*"}}
		if err := SaveAgent(defaultAgent); err != nil {
			return nil, fmt.Errorf("saving default agent: %w", err)
		}
		agents = []AgentConfig{defaultAgent}
	}
	configuration.Agents = agents

	return configuration, nil
}

// Save writes the config to ~/.teanode/config.yaml atomically.
func Save(configuration *Config) error {
	directory, err := Directory()
	if err != nil {
		return err
	}
	data, err := yaml.Marshal(configuration)
	if err != nil {
		return fmt.Errorf("marshalling config: %w", err)
	}
	return atomicfile.WriteFile(filepath.Join(directory, "config.yaml"), data)
}

func defaults() *Config {
	configDefaults := parseSchemaDefaults(configSchemaJSON)
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
			Default:  "gpt-5.1",
			Provider: "openai",
			BaseURL:  "https://api.openai.com/v1",
		},
	}
}

func applyEnv(configuration *Config) {
	if value := os.Getenv("OPENAI_API_KEY"); value != "" {
		configuration.Models.APIKey = value
	}
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
	if value := os.Getenv("TEANODE_GATEWAY_TOKEN"); value != "" {
		if configuration.Gateway.Auth == nil {
			configuration.Gateway.Auth = &AuthConfig{}
		}
		configuration.Gateway.Auth.Token = value
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
	if value := os.Getenv("TEANODE_CDP_ENDPOINT"); value != "" {
		if configuration.Integrations.Browser == nil {
			configuration.Integrations.Browser = &BrowserConfig{}
		}
		configuration.Integrations.Browser.CDPEndpoint = value
	}
}
