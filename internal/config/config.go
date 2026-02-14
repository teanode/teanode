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

//go:embed default_agents.md
var defaultAgentsMD string

//go:embed default_memory.md
var defaultMemoryMD string

type Config struct {
	Gateway      GatewayConfig   `json:"gateway"`
	Models       ModelsConfig    `json:"models"`
	Tools        ToolsConfig     `json:"tools,omitempty"`
	Browser      *BrowserConfig  `json:"browser,omitempty"`
	SystemPrompt string          `json:"systemPrompt,omitempty"`
	Discord      *DiscordConfig  `json:"discord,omitempty"`
	Telegram     *TelegramConfig `json:"telegram,omitempty"`
}

type BrowserConfig struct {
	CDPEndpoint string `json:"cdpEndpoint,omitempty"` // e.g. "127.0.0.1:9222"
}

type DiscordConfig struct {
	Token        string   `json:"token,omitempty"`
	AllowedUsers []string `json:"allowedUsers,omitempty"` // Discord user IDs
}

type TelegramConfig struct {
	Token        string  `json:"token,omitempty"`
	AllowedUsers []int64 `json:"allowedUsers,omitempty"` // Telegram user IDs
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
func (m *ModelsConfig) ResolvedProviders() map[string]ProviderConfig {
	if len(m.Providers) > 0 {
		return m.Providers
	}
	name := m.Provider
	if name == "" {
		name = "openai"
	}
	return map[string]ProviderConfig{
		name: {BaseURL: m.BaseURL, APIKey: m.APIKey},
	}
}

// DefaultProviderName returns the name of the default provider.
// If the default model is qualified ("provider:model"), the provider part is
// returned; otherwise the first (or only) configured provider name is used.
func (m *ModelsConfig) DefaultProviderName() string {
	// Check if default model is qualified.
	if idx := strings.IndexByte(m.Default, ':'); idx >= 0 {
		return m.Default[:idx]
	}
	// Legacy single-provider.
	if len(m.Providers) == 0 {
		if m.Provider != "" {
			return m.Provider
		}
		return "openai"
	}
	// Multi-provider: pick the first key (deterministic for single-entry maps).
	for name := range m.Providers {
		return name
	}
	return "openai"
}

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

// SessionsDir returns the sessions directory (~/.teanode/sessions).
func SessionsDir() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "sessions"), nil
}

// WorkspaceDir returns the workspace directory (~/.teanode/workspace).
func WorkspaceDir() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "workspace"), nil
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

// EnsureDirs creates the config, sessions, workspace, and media directories if needed.
func EnsureDirs() error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	for _, sub := range []string{"sessions", "workspace", "workspace/memory", "skills", "media"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0755); err != nil {
			return fmt.Errorf("creating directories: %w", err)
		}
	}
	return nil
}

// SeedWorkspace writes default AGENTS.md and MEMORY.md if they don't exist.
func SeedWorkspace() error {
	workspaceDirectory, err := WorkspaceDir()
	if err != nil {
		return err
	}

	seeds := map[string]string{
		"AGENTS.md": defaultAgentsMD,
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
	return configuration, nil
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
