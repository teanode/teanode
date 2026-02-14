// Package config handles loading and watching the teanode configuration file.
package config

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/teanode/teanode/internal/util/atomicfile"
)

//go:embed default_agent.md
var defaultAgentMD string

//go:embed default_memory.md
var defaultMemoryMD string

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

// DefaultAgentID is the ID of the default agent when no agents are configured.
const DefaultAgentID = "main"

// AgentConfig defines a single agent in the multi-agent system.
type AgentConfig struct {
	ID                    string        `json:"id"`                              // unique; "main" is default
	Model                 string        `json:"model,omitempty"`                 // qualified model override (e.g. "openai:gpt-4o")
	SystemPrompt          string        `json:"systemPrompt,omitempty"`          // per-agent identity line override
	Skills                *FilterConfig `json:"skills,omitempty"`                // skill allow/deny filter
	Tools                 *FilterConfig `json:"tools,omitempty"`                 // builtin tool allow/deny filter
	CanMessage            []string      `json:"canMessage,omitempty"`            // agent IDs this agent can talk to; "*" = all
	MaxToolRounds         int           `json:"maxToolRounds,omitempty"`         // max tool-call loop iterations
	CompressionThreshold  float64       `json:"compressionThreshold,omitempty"`  // context compression ratio (0-1)
	MinKeepMessages       int           `json:"minKeepMessages,omitempty"`       // min recent messages to preserve
	MaxToolResultChars    int           `json:"maxToolResultChars,omitempty"`    // max chars per old tool result
	MaxWorkspaceFileChars int           `json:"maxWorkspaceFileChars,omitempty"` // max chars per workspace file in prompt
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
var DefaultAgentLimits = AgentLimits{
	MaxToolRounds:         100,
	CompressionThreshold:  0.80,
	MinKeepMessages:       10,
	MaxToolResultChars:    8000,
	MaxWorkspaceFileChars: 8000,
}

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

// FilterConfig defines allow/deny lists for filtering tools or skills.
// Deny wins over allow. A nil Allow list means all are allowed.
type FilterConfig struct {
	Allow []string `json:"allow,omitempty"` // nil = all allowed; [] = none allowed
	Deny  []string `json:"deny,omitempty"`  // deny wins over allow
}

type Config struct {
	Gateway      GatewayConfig   `json:"gateway"`
	Models       ModelsConfig    `json:"models"`
	Tools        ToolsConfig     `json:"tools,omitempty"`
	Browser      *BrowserConfig  `json:"browser,omitempty"`
	SystemPrompt string          `json:"systemPrompt,omitempty"`
	Discord      *DiscordConfig  `json:"discord,omitempty"`
	Telegram     *TelegramConfig `json:"telegram,omitempty"`
	Agents       []AgentConfig   `json:"-"`
}

type BrowserConfig struct {
	CDPEndpoint string `json:"cdpEndpoint,omitempty"` // e.g. "127.0.0.1:9222"
}

type DiscordConfig struct {
	Token        string   `json:"token,omitempty"`
	AllowedUsers []string `json:"allowedUsers,omitempty"` // Discord user IDs
	AgentID      string   `json:"agentId,omitempty"`      // defaults to "main"
}

type TelegramConfig struct {
	Token        string  `json:"token,omitempty"`
	AllowedUsers []int64 `json:"allowedUsers,omitempty"` // Telegram user IDs
	AgentID      string  `json:"agentId,omitempty"`      // defaults to "main"
}

type ToolsConfig struct {
	BraveAPIKey string `json:"braveApiKey,omitempty"`
}

type GatewayConfig struct {
	Port int         `json:"port"`
	Bind string      `json:"bind"` // "loopback" | "lan"
	Auth *AuthConfig `json:"auth,omitempty"`
}

type AuthConfig struct {
	Token    string `json:"token,omitempty"`
	Password string `json:"password,omitempty"`
}

// ProviderConfig holds connection details for a single provider.
type ProviderConfig struct {
	BaseURL string `json:"baseUrl"`
	APIKey  string `json:"apiKey,omitempty"`
}

type ModelsConfig struct {
	Default       string                    `json:"default"`
	TitleModel    string                    `json:"titleModel,omitempty"`    // model for title summarization; defaults to Default
	ContextWindow int                       `json:"contextWindow,omitempty"` // max tokens; default 128000
	SummaryModel  string                    `json:"summaryModel,omitempty"`  // model for context summarization; defaults to TitleModel
	Providers     map[string]ProviderConfig `json:"providers,omitempty"`     // multi-provider config

	// Legacy single-provider fields (used if Providers is empty)
	Provider string `json:"provider,omitempty"`
	BaseURL  string `json:"baseUrl,omitempty"`
	APIKey   string `json:"apiKey,omitempty"`
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

// IsAllowed checks whether a name passes a filter. Deny wins over allow.
// A nil filter means everything is allowed. A nil Allow list means all allowed;
// an empty Allow list means none allowed.
func IsAllowed(name string, filter *FilterConfig) bool {
	if filter == nil {
		return true
	}
	// Deny wins.
	for _, denied := range filter.Deny {
		if denied == name {
			return false
		}
	}
	// Nil allow = all allowed.
	if filter.Allow == nil {
		return true
	}
	// Explicit allow list.
	for _, allowed := range filter.Allow {
		if allowed == name {
			return true
		}
	}
	return false
}

// --- Directory Functions ---

var overrideDir string

// SetDir overrides the data directory. Must be called before any other
// config functions (EnsureDirs, Load, etc.).
func SetDir(dir string) {
	overrideDir = dir
}

// Dir returns the teanode data directory (default ~/.teanode).
func Dir() (string, error) {
	if overrideDir != "" {
		return overrideDir, nil
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

// CronsFile returns the path to the crons file (~/.teanode/crons.json).
func CronsFile() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "crons.json"), nil
}

// AgentsDir returns the agents config directory (~/.teanode/agents/).
func AgentsDir() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "agents"), nil
}

// AgentWorkspaceDir returns the workspace directory for an agent (~/.teanode/workspaces/<agentId>/).
func AgentWorkspaceDir(agentId string) (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "workspaces", agentId), nil
}

// AgentSessionsDir returns the sessions directory for an agent (~/.teanode/sessions/<agentId>/).
func AgentSessionsDir(agentId string) (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "sessions", agentId), nil
}

// SkillsDir returns the skills directory (~/.teanode/skills).
func SkillsDir() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "skills"), nil
}

// ModelsFile returns the path to the models cache file (~/.teanode/models.json).
func ModelsFile() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "models.json"), nil
}

// MediaDir returns the media directory (~/.teanode/media).
func MediaDir() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "media"), nil
}

// EnsureDirs creates the base teanode directories if needed.
func EnsureDirs() error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	for _, sub := range []string{"sessions", "workspaces", "skills", "media", "agents"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0755); err != nil {
			return fmt.Errorf("creating directories: %w", err)
		}
	}
	return nil
}

// EnsureAgentDirs creates the workspace and sessions directories for an agent.
func EnsureAgentDirs(agentId string) error {
	workspaceDirectory, err := AgentWorkspaceDir(agentId)
	if err != nil {
		return err
	}
	sessionsDirectory, err := AgentSessionsDir(agentId)
	if err != nil {
		return err
	}
	for _, directory := range []string{
		workspaceDirectory,
		filepath.Join(workspaceDirectory, "memory"),
		sessionsDirectory,
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
	workspaceDirectory, err := AgentWorkspaceDir(agentId)
	if err != nil {
		return err
	}

	seeds := map[string]string{
		"AGENT.md":  defaultAgentMD,
		"MEMORY.md": defaultMemoryMD,
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

// MigrateToAgentDirs migrates the old single-agent directory layout to the new
// multi-agent layout. Safe to call multiple times (no-op if already migrated).
func MigrateToAgentDirs() error {
	dir, err := Dir()
	if err != nil {
		return err
	}

	// Migrate workspace/ → workspaces/main/
	oldWorkspace := filepath.Join(dir, "workspace")
	newWorkspace := filepath.Join(dir, "workspaces", DefaultAgentID)
	if info, err := os.Stat(oldWorkspace); err == nil && info.IsDir() {
		if _, err := os.Stat(newWorkspace); os.IsNotExist(err) {
			if err := os.MkdirAll(filepath.Dir(newWorkspace), 0755); err != nil {
				return fmt.Errorf("creating workspaces directory: %w", err)
			}
			if err := os.Rename(oldWorkspace, newWorkspace); err != nil {
				return fmt.Errorf("migrating workspace to workspaces/main: %w", err)
			}
		}
	}

	// Migrate sessions/*.jsonl → sessions/main/*.jsonl
	oldSessions := filepath.Join(dir, "sessions")
	newSessions := filepath.Join(dir, "sessions", DefaultAgentID)
	if info, err := os.Stat(oldSessions); err == nil && info.IsDir() {
		// Check if there are .jsonl files directly in sessions/ (old layout).
		entries, err := os.ReadDir(oldSessions)
		if err != nil {
			return fmt.Errorf("reading sessions directory: %w", err)
		}
		var jsonlFiles []os.DirEntry
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".jsonl") {
				jsonlFiles = append(jsonlFiles, entry)
			}
		}
		if len(jsonlFiles) > 0 {
			if _, err := os.Stat(newSessions); os.IsNotExist(err) {
				if err := os.MkdirAll(newSessions, 0755); err != nil {
					return fmt.Errorf("creating sessions/main directory: %w", err)
				}
			}
			for _, entry := range jsonlFiles {
				oldPath := filepath.Join(oldSessions, entry.Name())
				newPath := filepath.Join(newSessions, entry.Name())
				if err := os.Rename(oldPath, newPath); err != nil {
					return fmt.Errorf("migrating session %s: %w", entry.Name(), err)
				}
			}
		}
	}

	// Rename AGENTS.md → AGENT.md in all workspace directories.
	workspacesDir := filepath.Join(dir, "workspaces")
	if info, err := os.Stat(workspacesDir); err == nil && info.IsDir() {
		entries, err := os.ReadDir(workspacesDir)
		if err == nil {
			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}
				agentWorkspace := filepath.Join(workspacesDir, entry.Name())
				oldAgentMD := filepath.Join(agentWorkspace, "AGENTS.md")
				newAgentMD := filepath.Join(agentWorkspace, "AGENT.md")
				if _, err := os.Stat(oldAgentMD); err == nil {
					if _, err := os.Stat(newAgentMD); os.IsNotExist(err) {
						os.Rename(oldAgentMD, newAgentMD)
					}
				}
			}
		}
	}

	return nil
}

// --- Per-Agent File Operations ---

// LoadAgents walks agents/*/config.json and returns all agent configs.
func LoadAgents() ([]AgentConfig, error) {
	agentsDirectory, err := AgentsDir()
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
		configPath := filepath.Join(agentsDirectory, entry.Name(), "config.json")
		data, err := os.ReadFile(configPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("reading agent config %s: %w", entry.Name(), err)
		}
		var agentConfig AgentConfig
		if err := json.Unmarshal(data, &agentConfig); err != nil {
			return nil, fmt.Errorf("parsing agent config %s: %w", entry.Name(), err)
		}
		// Ensure the ID matches the directory name.
		agentConfig.ID = entry.Name()
		agents = append(agents, agentConfig)
	}
	return agents, nil
}

// SaveAgent writes an agent config to agents/<id>/config.json atomically.
func SaveAgent(agentConfig AgentConfig) error {
	agentsDirectory, err := AgentsDir()
	if err != nil {
		return err
	}
	agentDirectory := filepath.Join(agentsDirectory, agentConfig.ID)
	if err := os.MkdirAll(agentDirectory, 0755); err != nil {
		return fmt.Errorf("creating agent directory: %w", err)
	}
	data, err := json.MarshalIndent(agentConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling agent config: %w", err)
	}
	return atomicfile.WriteFile(filepath.Join(agentDirectory, "config.json"), data)
}

// DeleteAgent removes the agents/<agentId>/ directory.
func DeleteAgent(agentId string) error {
	agentsDirectory, err := AgentsDir()
	if err != nil {
		return err
	}
	agentDirectory := filepath.Join(agentsDirectory, agentId)
	if _, err := os.Stat(agentDirectory); os.IsNotExist(err) {
		return fmt.Errorf("agent not found: %s", agentId)
	}
	return os.RemoveAll(agentDirectory)
}

// MigrateAgentsToFiles moves agents from config.json into per-agent files.
// Safe to call multiple times (no-op if agents key is absent or empty).
func MigrateAgentsToFiles() error {
	dir, err := Dir()
	if err != nil {
		return err
	}

	configPath := filepath.Join(dir, "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading config for migration: %w", err)
	}

	var rawConfig map[string]interface{}
	if err := json.Unmarshal(data, &rawConfig); err != nil {
		return fmt.Errorf("parsing config for migration: %w", err)
	}

	agentsRaw, exists := rawConfig["agents"]
	if !exists {
		return nil
	}

	agentsSlice, ok := agentsRaw.([]interface{})
	if !ok || len(agentsSlice) == 0 {
		return nil
	}

	// Parse each agent and write to its own file.
	for _, agentRaw := range agentsSlice {
		agentBytes, err := json.Marshal(agentRaw)
		if err != nil {
			return fmt.Errorf("marshalling agent for migration: %w", err)
		}
		var agentConfig AgentConfig
		if err := json.Unmarshal(agentBytes, &agentConfig); err != nil {
			return fmt.Errorf("parsing agent for migration: %w", err)
		}
		if agentConfig.ID == "" {
			continue
		}
		if err := SaveAgent(agentConfig); err != nil {
			return fmt.Errorf("saving agent %s during migration: %w", agentConfig.ID, err)
		}
	}

	// Remove agents key from config.json and re-write.
	delete(rawConfig, "agents")
	updatedData, err := json.MarshalIndent(rawConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling config after migration: %w", err)
	}
	return atomicfile.WriteFile(configPath, updatedData)
}

// Load reads config from ~/.teanode/config.json and applies env overrides.
func Load() (*Config, error) {
	configuration := defaults()

	dir, err := Dir()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	if err == nil {
		if err := json.Unmarshal(data, configuration); err != nil {
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

// Save writes the config to ~/.teanode/config.json atomically.
func Save(configuration *Config) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(configuration, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling config: %w", err)
	}
	return atomicfile.WriteFile(filepath.Join(dir, "config.json"), data)
}

func defaults() *Config {
	return &Config{
		Gateway: GatewayConfig{
			Port: 8833,
			Bind: "loopback",
		},
		Browser: &BrowserConfig{
			CDPEndpoint: "127.0.0.1:9222",
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
		if configuration.Discord == nil {
			configuration.Discord = &DiscordConfig{}
		}
		configuration.Discord.Token = value
	}
	if value := os.Getenv("TELEGRAM_BOT_TOKEN"); value != "" {
		if configuration.Telegram == nil {
			configuration.Telegram = &TelegramConfig{}
		}
		configuration.Telegram.Token = value
	}
	if value := os.Getenv("TEANODE_CDP_ENDPOINT"); value != "" {
		if configuration.Browser == nil {
			configuration.Browser = &BrowserConfig{}
		}
		configuration.Browser.CDPEndpoint = value
	}
}
