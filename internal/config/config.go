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

// schemaField represents a single field entry in a schema JSON section.
type schemaField struct {
	Key     string      `json:"key"`
	Default interface{} `json:"default"`
}

// parseSchemaDefaults extracts key→default pairs from an embedded schema JSON.
func parseSchemaDefaults(schemaJSON []byte) map[string]interface{} {
	var schema struct {
		Sections []struct {
			Fields []schemaField `json:"fields"`
		} `json:"sections"`
	}
	json.Unmarshal(schemaJSON, &schema)
	defaults := make(map[string]interface{})
	for _, section := range schema.Sections {
		for _, field := range section.Fields {
			if field.Default != nil {
				defaults[field.Key] = field.Default
			}
		}
	}
	return defaults
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
	ID                    string        `json:"id" yaml:"id"`                                                        // unique; "main" is default
	Name                  string        `json:"name,omitempty" yaml:"name,omitempty"`                                 // friendly display name
	Model                 string        `json:"model,omitempty" yaml:"model,omitempty"`                               // qualified model override (e.g. "openai:gpt-4o")
	SystemPrompt          string        `json:"systemPrompt,omitempty" yaml:"systemPrompt,omitempty"`                 // per-agent identity line override
	Skills                *FilterConfig `json:"skills,omitempty" yaml:"skills,omitempty"`                             // skill allow/deny filter
	Tools                 *FilterConfig `json:"tools,omitempty" yaml:"tools,omitempty"`                               // builtin tool allow/deny filter
	CanMessage            []string      `json:"canMessage,omitempty" yaml:"canMessage,omitempty"`                     // agent IDs this agent can talk to; "*" = all
	MaxToolRounds         int           `json:"maxToolRounds,omitempty" yaml:"maxToolRounds,omitempty"`               // max tool-call loop iterations
	CompressionThreshold  float64       `json:"compressionThreshold,omitempty" yaml:"compressionThreshold,omitempty"` // context compression ratio (0-1)
	MinKeepMessages       int           `json:"minKeepMessages,omitempty" yaml:"minKeepMessages,omitempty"`           // min recent messages to preserve
	MaxToolResultChars    int           `json:"maxToolResultChars,omitempty" yaml:"maxToolResultChars,omitempty"`      // max chars per old tool result
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

// FilterConfig defines allow/deny lists for filtering tools or skills.
// Deny wins over allow. A nil Allow list means all are allowed.
type FilterConfig struct {
	Allow []string `json:"allow,omitempty" yaml:"allow,omitempty"` // nil = all allowed; [] = none allowed
	Deny  []string `json:"deny,omitempty" yaml:"deny,omitempty"`  // deny wins over allow
}

// SummarizerConfig controls the background session summarizer behavior.
// Time fields are in minutes. Zero values fall back to defaults.
type SummarizerConfig struct {
	TickInterval          int `json:"tickInterval,omitempty" yaml:"tickInterval,omitempty"`                   // how often the background loop runs (minutes)
	StartupDelay          int `json:"startupDelay,omitempty" yaml:"startupDelay,omitempty"`                   // delay before first run (minutes)
	InactivityTime        int `json:"inactivityTime,omitempty" yaml:"inactivityTime,omitempty"`               // session inactivity threshold (minutes)
	MinMessages           int `json:"minMessages,omitempty" yaml:"minMessages,omitempty"`                     // minimum messages required to summarize
	MaxConversationChars  int `json:"maxConversationChars,omitempty" yaml:"maxConversationChars,omitempty"`   // max chars of conversation text sent to the LLM
	MaxMessageChars       int `json:"maxMessageChars,omitempty" yaml:"maxMessageChars,omitempty"`             // max chars per individual message
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
	Browser      *BrowserConfig     `json:"browser,omitempty" yaml:"browser,omitempty"`
	Summarizer   *SummarizerConfig  `json:"summarizer,omitempty" yaml:"summarizer,omitempty"`
	SystemPrompt string             `json:"systemPrompt,omitempty" yaml:"systemPrompt,omitempty"`
	DefaultAgent string             `json:"defaultAgent,omitempty" yaml:"defaultAgent,omitempty"` // defaults to first configured agent
	Discord      *DiscordConfig     `json:"discord,omitempty" yaml:"discord,omitempty"`
	Telegram     *TelegramConfig    `json:"telegram,omitempty" yaml:"telegram,omitempty"`
	Agents       []AgentConfig      `json:"-" yaml:"-"`
}

type BrowserConfig struct {
	CDPEndpoint string `json:"cdpEndpoint,omitempty" yaml:"cdpEndpoint,omitempty"` // e.g. "127.0.0.1:9222"
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
	Default       string                    `json:"default" yaml:"default"`
	SummarizerModel string                    `json:"summarizerModel,omitempty" yaml:"summarizerModel,omitempty"` // model for title + summary generation; defaults to Default
	ContextWindow   int                       `json:"contextWindow,omitempty" yaml:"contextWindow,omitempty"`   // max tokens; default 128000
	Providers     map[string]ProviderConfig `json:"providers,omitempty" yaml:"providers,omitempty"`         // multi-provider config

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

// CronsFile returns the path to the crons file (~/.teanode/crons.yaml).
func CronsFile() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "crons.yaml"), nil
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

// ModelsFile returns the path to the models cache file (~/.teanode/models.yaml).
func ModelsFile() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "models.yaml"), nil
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

// LoadAgents walks agents/*/config.yaml and returns all agent configs.
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
	agentsDirectory, err := AgentsDir()
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

// MigrateAgentsToFiles moves agents from config.yaml into per-agent files.
// Safe to call multiple times (no-op if agents key is absent or empty).
func MigrateAgentsToFiles() error {
	dir, err := Dir()
	if err != nil {
		return err
	}

	configPath := filepath.Join(dir, "config.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading config for migration: %w", err)
	}

	var rawConfig map[string]interface{}
	if err := yaml.Unmarshal(data, &rawConfig); err != nil {
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
		agentBytes, err := yaml.Marshal(agentRaw)
		if err != nil {
			return fmt.Errorf("marshalling agent for migration: %w", err)
		}
		var agentConfig AgentConfig
		if err := yaml.Unmarshal(agentBytes, &agentConfig); err != nil {
			return fmt.Errorf("parsing agent for migration: %w", err)
		}
		if agentConfig.ID == "" {
			continue
		}
		if err := SaveAgent(agentConfig); err != nil {
			return fmt.Errorf("saving agent %s during migration: %w", agentConfig.ID, err)
		}
	}

	// Remove agents key from config.yaml and re-write.
	delete(rawConfig, "agents")
	updatedData, err := yaml.Marshal(rawConfig)
	if err != nil {
		return fmt.Errorf("marshalling config after migration: %w", err)
	}
	return atomicfile.WriteFile(configPath, updatedData)
}

// LoadRaw reads config from ~/.teanode/config.yaml without applying defaults
// or environment overrides. Returns only what the user explicitly set in the file.
func LoadRaw() (*Config, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}

	configuration := &Config{}
	data, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
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

	dir, err := Dir()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
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
	dir, err := Dir()
	if err != nil {
		return err
	}
	data, err := yaml.Marshal(configuration)
	if err != nil {
		return fmt.Errorf("marshalling config: %w", err)
	}
	return atomicfile.WriteFile(filepath.Join(dir, "config.yaml"), data)
}

func defaults() *Config {
	configDefaults := parseSchemaDefaults(configSchemaJSON)
	return &Config{
		Gateway: GatewayConfig{
			Port: schemaInt(configDefaults, "gateway.port"),
			Bind: schemaString(configDefaults, "gateway.bind"),
		},
		Browser: &BrowserConfig{
			CDPEndpoint: schemaString(configDefaults, "browser.cdpEndpoint"),
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
