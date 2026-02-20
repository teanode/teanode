package configs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// withTempDir sets the config directory to a temp dir and restores it on cleanup.
func withTempDir(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	SetDirectory(directory)
	t.Cleanup(func() { SetDirectory("") })
	return directory
}

// --- 1. Schema Defaults ---

func TestDefaultAgentLimits(t *testing.T) {
	if DefaultAgentLimits.MaxToolRounds != 250 {
		t.Errorf("MaxToolRounds = %d, want 250", DefaultAgentLimits.MaxToolRounds)
	}
	if DefaultAgentLimits.CompressionThreshold != 0.80 {
		t.Errorf("CompressionThreshold = %f, want 0.80", DefaultAgentLimits.CompressionThreshold)
	}
	if DefaultAgentLimits.MinKeepMessages != 10 {
		t.Errorf("MinKeepMessages = %d, want 10", DefaultAgentLimits.MinKeepMessages)
	}
	if DefaultAgentLimits.MaxToolResultChars != 8000 {
		t.Errorf("MaxToolResultChars = %d, want 8000", DefaultAgentLimits.MaxToolResultChars)
	}
	if DefaultAgentLimits.MaxWorkspaceFileChars != 8000 {
		t.Errorf("MaxWorkspaceFileChars = %d, want 8000", DefaultAgentLimits.MaxWorkspaceFileChars)
	}
}

func TestDefaultSummarizerConfig(t *testing.T) {
	if DefaultSummarizerConfig.TickInterval != 1 {
		t.Errorf("TickInterval = %d, want 1", DefaultSummarizerConfig.TickInterval)
	}
	if DefaultSummarizerConfig.StartupDelay != 1 {
		t.Errorf("StartupDelay = %d, want 1", DefaultSummarizerConfig.StartupDelay)
	}
	if DefaultSummarizerConfig.InactivityTime != 3 {
		t.Errorf("InactivityTime = %d, want 3", DefaultSummarizerConfig.InactivityTime)
	}
	if DefaultSummarizerConfig.MinMessages != 1 {
		t.Errorf("MinMessages = %d, want 1", DefaultSummarizerConfig.MinMessages)
	}
	if DefaultSummarizerConfig.MaxConversationChars != 8000 {
		t.Errorf("MaxConversationChars = %d, want 8000", DefaultSummarizerConfig.MaxConversationChars)
	}
	if DefaultSummarizerConfig.MaxMessageChars != 2000 {
		t.Errorf("MaxMessageChars = %d, want 2000", DefaultSummarizerConfig.MaxMessageChars)
	}
}

func TestConfigSchema(t *testing.T) {
	raw := ConfigSchema()
	var parsed map[string]interface{}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("ConfigSchema() returned invalid JSON: %v", err)
	}
	if _, ok := parsed["properties"]; !ok {
		t.Error("ConfigSchema() missing 'properties' key")
	}
}

func TestAgentConfigSchema(t *testing.T) {
	raw := AgentConfigSchema()
	var parsed map[string]interface{}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("AgentConfigSchema() returned invalid JSON: %v", err)
	}
	if _, ok := parsed["properties"]; !ok {
		t.Error("AgentConfigSchema() missing 'properties' key")
	}
}

// --- 2. IsAllowed ---

func TestIsAllowed(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		allowed  []string
		expected bool
	}{
		{"nil list allows everything", "anything", nil, true},
		{"empty list allows everything (preserves defaults)", "anything", []string{}, true},
		{"match found", "shell", []string{"browser", "shell", "search"}, true},
		{"match not found", "delete", []string{"browser", "shell", "search"}, false},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			result := IsAllowed(testCase.input, testCase.allowed)
			if result != testCase.expected {
				t.Errorf("IsAllowed(%q, %v) = %v, want %v", testCase.input, testCase.allowed, result, testCase.expected)
			}
		})
	}
}

// --- 3. Model/Agent Limits Resolution ---

func TestResolveModelLimits_Defaults(t *testing.T) {
	configuration := &Config{}
	limits := configuration.ResolveModelLimits("openai:gpt-5.1")

	if limits != DefaultAgentLimits {
		t.Errorf("ResolveModelLimits should return DefaultAgentLimits, got %+v", limits)
	}
}

func TestResolveModelLimits_DefaultAndPerModelOverrides(t *testing.T) {
	configuration := &Config{
		Models: ModelsConfig{
			Default: "openai:gpt-5.1",
			DefaultLimits: AgentLimits{
				MaxToolRounds: 123,
			},
			Limits: map[string]AgentLimits{
				"openai:gpt-5.1": {
					MinKeepMessages: 5,
				},
			},
		},
	}
	limits := configuration.ResolveModelLimits("openai:gpt-5.1")

	if limits.MaxToolRounds != 123 {
		t.Errorf("MaxToolRounds = %d, want 123", limits.MaxToolRounds)
	}
	if limits.MinKeepMessages != 5 {
		t.Errorf("MinKeepMessages = %d, want 5", limits.MinKeepMessages)
	}
	if limits.CompressionThreshold != DefaultAgentLimits.CompressionThreshold {
		t.Errorf("CompressionThreshold = %f, want default %f", limits.CompressionThreshold, DefaultAgentLimits.CompressionThreshold)
	}
}

func TestResolveModelLimits_BareModelLookup(t *testing.T) {
	configuration := &Config{
		Models: ModelsConfig{
			Default: "openai:gpt-5.1",
			Limits: map[string]AgentLimits{
				"gpt-5.1": {MaxToolResultChars: 2222},
			},
		},
	}
	limits := configuration.ResolveModelLimits("openai:gpt-5.1")
	if limits.MaxToolResultChars != 2222 {
		t.Errorf("MaxToolResultChars = %d, want 2222", limits.MaxToolResultChars)
	}
}

func TestResolveModelLimits_IgnoresAgentConfig(t *testing.T) {
	configuration := &Config{
		Models: ModelsConfig{
			Default: "openai:gpt-5.1",
			Limits: map[string]AgentLimits{
				"openai:gpt-5.1": {MaxToolRounds: 300},
			},
		},
		Agents: []AgentConfig{
			{ID: "main", Model: "openai:gpt-5.1"},
		},
	}
	limits := configuration.ResolveModelLimits("openai:gpt-5.1")
	if limits.MaxToolRounds != 300 {
		t.Errorf("MaxToolRounds = %d, want 300 (model-level override)", limits.MaxToolRounds)
	}
}

// --- 4. Config.ResolveSummarizerConfig ---

func TestResolveSummarizerConfig_NilSummarizer(t *testing.T) {
	configuration := &Config{}
	resolved := configuration.ResolveSummarizerConfig()

	if resolved != DefaultSummarizerConfig {
		t.Errorf("nil summarizer should return defaults, got %+v", resolved)
	}
}

func TestResolveSummarizerConfig_PartialOverrides(t *testing.T) {
	configuration := &Config{
		Summarizer: &SummarizerConfig{
			TickInterval:    5,
			MaxMessageChars: 500,
		},
	}
	resolved := configuration.ResolveSummarizerConfig()

	if resolved.TickInterval != 5 {
		t.Errorf("TickInterval = %d, want 5", resolved.TickInterval)
	}
	if resolved.MaxMessageChars != 500 {
		t.Errorf("MaxMessageChars = %d, want 500", resolved.MaxMessageChars)
	}
	// Non-overridden fields should keep defaults.
	if resolved.StartupDelay != DefaultSummarizerConfig.StartupDelay {
		t.Errorf("StartupDelay = %d, want default %d", resolved.StartupDelay, DefaultSummarizerConfig.StartupDelay)
	}
	if resolved.InactivityTime != DefaultSummarizerConfig.InactivityTime {
		t.Errorf("InactivityTime = %d, want default %d", resolved.InactivityTime, DefaultSummarizerConfig.InactivityTime)
	}
	if resolved.MinMessages != DefaultSummarizerConfig.MinMessages {
		t.Errorf("MinMessages = %d, want default %d", resolved.MinMessages, DefaultSummarizerConfig.MinMessages)
	}
	if resolved.MaxConversationChars != DefaultSummarizerConfig.MaxConversationChars {
		t.Errorf("MaxConversationChars = %d, want default %d", resolved.MaxConversationChars, DefaultSummarizerConfig.MaxConversationChars)
	}
}

// --- 5. Agent Config Helpers ---

func TestResolveAgents_Configured(t *testing.T) {
	configuration := &Config{
		Agents: []AgentConfig{{ID: "alpha"}, {ID: "beta"}},
	}
	agents := configuration.ResolveAgents()

	if len(agents) != 2 {
		t.Fatalf("len(agents) = %d, want 2", len(agents))
	}
	if agents[0].ID != "alpha" || agents[1].ID != "beta" {
		t.Errorf("agents = %v, want [alpha, beta]", agents)
	}
}

func TestResolveAgents_DefaultMain(t *testing.T) {
	configuration := &Config{}
	agents := configuration.ResolveAgents()

	if len(agents) != 1 {
		t.Fatalf("len(agents) = %d, want 1", len(agents))
	}
	if agents[0].ID != DefaultAgentID {
		t.Errorf("agent ID = %q, want %q", agents[0].ID, DefaultAgentID)
	}
}

func TestAgentByID(t *testing.T) {
	configuration := &Config{
		Agents: []AgentConfig{{ID: "alpha"}, {ID: "beta"}},
	}

	found := configuration.AgentByID("beta")
	if found == nil || found.ID != "beta" {
		t.Errorf("AgentByID(beta) = %v, want &{ID:beta}", found)
	}

	notFound := configuration.AgentByID("gamma")
	if notFound != nil {
		t.Errorf("AgentByID(gamma) = %v, want nil", notFound)
	}
}

func TestAgentModel(t *testing.T) {
	configuration := &Config{
		Models: ModelsConfig{Default: "openai:gpt-5.1"},
		Agents: []AgentConfig{
			{ID: "alpha", Model: "anthropic:claude-4"},
			{ID: "beta"},
		},
	}

	if model := configuration.AgentModel("alpha"); model != "anthropic:claude-4" {
		t.Errorf("AgentModel(alpha) = %q, want anthropic:claude-4", model)
	}
	if model := configuration.AgentModel("beta"); model != "openai:gpt-5.1" {
		t.Errorf("AgentModel(beta) = %q, want openai:gpt-5.1 (global default)", model)
	}
	if model := configuration.AgentModel("missing"); model != "openai:gpt-5.1" {
		t.Errorf("AgentModel(missing) = %q, want openai:gpt-5.1 (global default)", model)
	}
}

func TestResolveDefaultAgent(t *testing.T) {
	t.Run("first agent", func(t *testing.T) {
		configuration := &Config{
			Agents: []AgentConfig{{ID: "alpha"}, {ID: "beta"}},
		}
		if result := configuration.ResolveDefaultAgent(); result != "alpha" {
			t.Errorf("got %q, want alpha", result)
		}
	})

	t.Run("no agents returns DefaultAgentID", func(t *testing.T) {
		configuration := &Config{}
		if result := configuration.ResolveDefaultAgent(); result != DefaultAgentID {
			t.Errorf("got %q, want %q", result, DefaultAgentID)
		}
	})
}

// --- 6. Provider Config ---

func TestResolvedProviders_PopulatedList(t *testing.T) {
	models := &ModelsConfig{
		Providers: []ProviderConfig{
			{Name: "openai", BaseURL: "https://api.openai.com/v1", APIKey: "sk-123"},
			{Name: "anthropic", BaseURL: "https://api.anthropic.com", APIKey: "sk-456"},
		},
	}
	resolved := models.ResolvedProviders()

	if len(resolved) != 2 {
		t.Fatalf("len(providers) = %d, want 2", len(resolved))
	}
	if resolved[0].Name != "openai" || resolved[0].APIKey != "sk-123" {
		t.Errorf("providers[0] = %+v, want Name=openai APIKey=sk-123", resolved[0])
	}
}

func TestResolvedProviders_SingleProvider(t *testing.T) {
	models := &ModelsConfig{
		Providers: []ProviderConfig{
			{Name: "anthropic", BaseURL: "https://api.anthropic.com", APIKey: "sk-abc"},
		},
	}
	resolved := models.ResolvedProviders()

	if len(resolved) != 1 {
		t.Fatalf("len(providers) = %d, want 1", len(resolved))
	}
	if resolved[0].Name != "anthropic" {
		t.Errorf("provider name = %q, want anthropic", resolved[0].Name)
	}
	if resolved[0].BaseURL != "https://api.anthropic.com" || resolved[0].APIKey != "sk-abc" {
		t.Errorf("provider = %+v, want BaseURL=https://api.anthropic.com APIKey=sk-abc", resolved[0])
	}
}

func TestResolvedProviders_EmptyReturnsNil(t *testing.T) {
	models := &ModelsConfig{}
	resolved := models.ResolvedProviders()

	if len(resolved) != 0 {
		t.Errorf("expected empty providers, got %+v", resolved)
	}
}

func TestDefaultProviderName(t *testing.T) {
	t.Run("qualified model", func(t *testing.T) {
		models := &ModelsConfig{Default: "anthropic:claude-4"}
		if name := models.DefaultProviderName(); name != "anthropic" {
			t.Errorf("got %q, want anthropic", name)
		}
	})

	t.Run("provider list returns first name", func(t *testing.T) {
		models := &ModelsConfig{
			Providers: []ProviderConfig{{Name: "anthropic"}},
		}
		if name := models.DefaultProviderName(); name != "anthropic" {
			t.Errorf("got %q, want anthropic", name)
		}
	})

	t.Run("empty fallback", func(t *testing.T) {
		models := &ModelsConfig{}
		if name := models.DefaultProviderName(); name != "openai" {
			t.Errorf("got %q, want openai", name)
		}
	})
}

// --- 7. Directory Functions ---

func TestSetDirectoryAndDirectory(t *testing.T) {
	directory := withTempDir(t)

	result, err := Directory()
	if err != nil {
		t.Fatalf("Directory() error: %v", err)
	}
	if result != directory {
		t.Errorf("Directory() = %q, want %q", result, directory)
	}
}

func TestDirectory_EnvVar(t *testing.T) {
	SetDirectory("")
	t.Cleanup(func() { SetDirectory("") })

	envDirectory := t.TempDir()
	t.Setenv("TEANODE_DIR", envDirectory)

	result, err := Directory()
	if err != nil {
		t.Fatalf("Directory() error: %v", err)
	}
	if result != envDirectory {
		t.Errorf("Directory() = %q, want %q", result, envDirectory)
	}
}

func TestDirectory_DefaultHome(t *testing.T) {
	SetDirectory("")
	t.Cleanup(func() { SetDirectory("") })
	t.Setenv("TEANODE_DIR", "")

	result, err := Directory()
	if err != nil {
		t.Fatalf("Directory() error: %v", err)
	}
	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".teanode")
	if result != expected {
		t.Errorf("Directory() = %q, want %q", result, expected)
	}
}

func TestPathHelpers(t *testing.T) {
	directory := withTempDir(t)

	tests := []struct {
		name     string
		function func() (string, error)
		expected string
	}{
		{"JobsDirectory", JobsDirectory, filepath.Join(directory, "jobs")},
		{"AgentsDirectory", AgentsDirectory, filepath.Join(directory, "agents")},
		{"SkillsDirectory", SkillsDirectory, filepath.Join(directory, "skills")},
		{"ModelsFile", ModelsFile, filepath.Join(directory, "models.yaml")},
		{"MediaDirectory", MediaDirectory, filepath.Join(directory, "media")},
		{"StateFile", StateFile, filepath.Join(directory, "state.yaml")},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			result, err := testCase.function()
			if err != nil {
				t.Fatalf("error: %v", err)
			}
			if result != testCase.expected {
				t.Errorf("got %q, want %q", result, testCase.expected)
			}
		})
	}

	// Agent-specific path helpers.
	workspaceDirectory, err := AgentWorkspaceDirectory("alpha")
	if err != nil {
		t.Fatalf("AgentWorkspaceDirectory error: %v", err)
	}
	if workspaceDirectory != filepath.Join(directory, "workspaces", "alpha") {
		t.Errorf("AgentWorkspaceDirectory = %q, want %q", workspaceDirectory, filepath.Join(directory, "workspaces", "alpha"))
	}

	conversationsDirectory, err := AgentConversationsDirectory("alpha")
	if err != nil {
		t.Fatalf("AgentConversationsDirectory error: %v", err)
	}
	if conversationsDirectory != filepath.Join(directory, "conversations", "alpha") {
		t.Errorf("AgentConversationsDirectory = %q, want %q", conversationsDirectory, filepath.Join(directory, "conversations", "alpha"))
	}
}

func TestEnsureDirectories(t *testing.T) {
	directory := withTempDir(t)

	if err := EnsureDirectories(); err != nil {
		t.Fatalf("EnsureDirectories() error: %v", err)
	}

	expectedSubdirectories := []string{"conversations", "workspaces", "skills", "media", "agents", "jobs"}
	for _, subdirectory := range expectedSubdirectories {
		path := filepath.Join(directory, subdirectory)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("expected directory %q not found: %v", subdirectory, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("%q is not a directory", subdirectory)
		}
	}
}

func TestEnsureAgentDirectories(t *testing.T) {
	directory := withTempDir(t)

	if err := EnsureAgentDirectories("alpha"); err != nil {
		t.Fatalf("EnsureAgentDirectories() error: %v", err)
	}

	expectedPaths := []string{
		filepath.Join(directory, "workspaces", "alpha"),
		filepath.Join(directory, "workspaces", "alpha", "memory"),
		filepath.Join(directory, "conversations", "alpha"),
	}
	for _, path := range expectedPaths {
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("expected directory %q not found: %v", path, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("%q is not a directory", path)
		}
	}
}

// --- 8. Per-Agent File Operations ---

func TestSaveAndLoadAgents(t *testing.T) {
	withTempDir(t)

	if err := EnsureDirectories(); err != nil {
		t.Fatalf("EnsureDirectories() error: %v", err)
	}

	original := AgentConfig{
		ID:          "alpha",
		Name:        "Alpha Agent",
		Description: "Test agent",
		Model:       "openai:gpt-5.1",
	}
	if err := SaveAgent(original); err != nil {
		t.Fatalf("SaveAgent() error: %v", err)
	}

	agents, err := LoadAgents()
	if err != nil {
		t.Fatalf("LoadAgents() error: %v", err)
	}
	if len(agents) != 1 {
		t.Fatalf("len(agents) = %d, want 1", len(agents))
	}
	if agents[0].ID != "alpha" {
		t.Errorf("agent ID = %q, want alpha", agents[0].ID)
	}
	if agents[0].Name != "Alpha Agent" {
		t.Errorf("agent Name = %q, want Alpha Agent", agents[0].Name)
	}
}

func TestLoadAgents_IDFromDirectoryName(t *testing.T) {
	withTempDir(t)

	if err := EnsureDirectories(); err != nil {
		t.Fatalf("EnsureDirectories() error: %v", err)
	}

	// Save with ID "alpha", then the config file will have id: alpha.
	// But LoadAgents uses the directory name, so if we rename, ID follows dir.
	agentConfig := AgentConfig{ID: "alpha", Name: "Test"}
	if err := SaveAgent(agentConfig); err != nil {
		t.Fatalf("SaveAgent() error: %v", err)
	}

	// Rename directory from alpha to beta.
	agentsDirectory, _ := AgentsDirectory()
	if err := os.Rename(filepath.Join(agentsDirectory, "alpha"), filepath.Join(agentsDirectory, "beta")); err != nil {
		t.Fatalf("rename error: %v", err)
	}

	agents, err := LoadAgents()
	if err != nil {
		t.Fatalf("LoadAgents() error: %v", err)
	}
	if len(agents) != 1 || agents[0].ID != "beta" {
		t.Errorf("expected agent ID=beta from directory name, got %v", agents)
	}
}

func TestLoadAgents_NoDirectory(t *testing.T) {
	withTempDir(t)
	// Don't create agents directory at all.

	agents, err := LoadAgents()
	if err != nil {
		t.Fatalf("LoadAgents() error: %v", err)
	}
	if agents != nil {
		t.Errorf("expected nil when agents directory doesn't exist, got %v", agents)
	}
}

func TestLoadAgents_SkipsNonDirsAndMissingConfig(t *testing.T) {
	withTempDir(t)

	if err := EnsureDirectories(); err != nil {
		t.Fatalf("EnsureDirectories() error: %v", err)
	}
	agentsDirectory, _ := AgentsDirectory()

	// Create a regular file (should be skipped).
	if err := os.WriteFile(filepath.Join(agentsDirectory, "not-a-dir"), []byte("hello"), 0644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	// Create a directory without config.yaml (should be skipped).
	if err := os.MkdirAll(filepath.Join(agentsDirectory, "no-config"), 0755); err != nil {
		t.Fatalf("MkdirAll error: %v", err)
	}

	// Create a valid agent.
	if err := SaveAgent(AgentConfig{ID: "valid", Name: "Valid"}); err != nil {
		t.Fatalf("SaveAgent() error: %v", err)
	}

	agents, err := LoadAgents()
	if err != nil {
		t.Fatalf("LoadAgents() error: %v", err)
	}
	if len(agents) != 1 || agents[0].ID != "valid" {
		t.Errorf("expected only the valid agent, got %v", agents)
	}
}

func TestDeleteAgent(t *testing.T) {
	withTempDir(t)

	if err := EnsureDirectories(); err != nil {
		t.Fatalf("EnsureDirectories() error: %v", err)
	}
	if err := SaveAgent(AgentConfig{ID: "alpha"}); err != nil {
		t.Fatalf("SaveAgent() error: %v", err)
	}

	if err := DeleteAgent("alpha"); err != nil {
		t.Fatalf("DeleteAgent() error: %v", err)
	}

	agents, err := LoadAgents()
	if err != nil {
		t.Fatalf("LoadAgents() error: %v", err)
	}
	if len(agents) != 0 {
		t.Errorf("expected no agents after delete, got %v", agents)
	}
}

func TestDeleteAgent_NotFound(t *testing.T) {
	withTempDir(t)

	if err := EnsureDirectories(); err != nil {
		t.Fatalf("EnsureDirectories() error: %v", err)
	}

	err := DeleteAgent("nonexistent")
	if err == nil {
		t.Error("expected error for missing agent, got nil")
	}
}

// --- 9. Workspace Seeding ---

func TestSeedAgentWorkspace(t *testing.T) {
	withTempDir(t)

	if err := EnsureAgentDirectories("alpha"); err != nil {
		t.Fatalf("EnsureAgentDirectories() error: %v", err)
	}

	if err := SeedAgentWorkspace("alpha"); err != nil {
		t.Fatalf("SeedAgentWorkspace() error: %v", err)
	}

	workspaceDirectory, _ := AgentWorkspaceDirectory("alpha")
	for _, filename := range []string{"AGENT.md", "MEMORY.md", "SKILLS.md"} {
		path := filepath.Join(workspaceDirectory, filename)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("expected file %q not found: %v", filename, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("expected file %q to have content", filename)
		}
	}
}

func TestSeedAgentWorkspace_SkipsExisting(t *testing.T) {
	withTempDir(t)

	if err := EnsureAgentDirectories("alpha"); err != nil {
		t.Fatalf("EnsureAgentDirectories() error: %v", err)
	}

	workspaceDirectory, _ := AgentWorkspaceDirectory("alpha")
	customContent := []byte("custom content")
	agentMDPath := filepath.Join(workspaceDirectory, "AGENT.md")
	if err := os.WriteFile(agentMDPath, customContent, 0644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	if err := SeedAgentWorkspace("alpha"); err != nil {
		t.Fatalf("SeedAgentWorkspace() error: %v", err)
	}

	data, err := os.ReadFile(agentMDPath)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	if string(data) != "custom content" {
		t.Errorf("AGENT.md was overwritten, got %q", string(data))
	}
}

// --- 10. Config Load/Save Pipeline ---

func TestSaveAndLoadRaw(t *testing.T) {
	withTempDir(t)

	original := &Config{
		Gateway: GatewayConfig{Port: 9999, Bind: "lan"},
		Models:  ModelsConfig{Default: "anthropic:claude-4"},
	}
	if err := Save(original); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	loaded, err := LoadRaw()
	if err != nil {
		t.Fatalf("LoadRaw() error: %v", err)
	}
	if loaded.Gateway.Port != 9999 {
		t.Errorf("Port = %d, want 9999", loaded.Gateway.Port)
	}
	if loaded.Gateway.Bind != "lan" {
		t.Errorf("Bind = %q, want lan", loaded.Gateway.Bind)
	}
	if loaded.Models.Default != "anthropic:claude-4" {
		t.Errorf("Default = %q, want anthropic:claude-4", loaded.Models.Default)
	}
}

func TestLoadRaw_NoFile(t *testing.T) {
	withTempDir(t)

	loaded, err := LoadRaw()
	if err != nil {
		t.Fatalf("LoadRaw() error: %v", err)
	}
	// Should return empty config, not error.
	if loaded.Gateway.Port != 0 {
		t.Errorf("expected zero-value config, got Port=%d", loaded.Gateway.Port)
	}
}

func TestLoad_AppliesDefaults(t *testing.T) {
	directory := withTempDir(t)

	// Write an empty config file.
	if err := os.WriteFile(filepath.Join(directory, "config.yaml"), []byte("{}"), 0644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if loaded.Gateway.Port != 8833 {
		t.Errorf("Port = %d, want 8833 (default)", loaded.Gateway.Port)
	}
	if loaded.Gateway.Bind != "loopback" {
		t.Errorf("Bind = %q, want loopback (default)", loaded.Gateway.Bind)
	}
	if loaded.Models.Default != "openai:gpt-5.1" {
		t.Errorf("Default = %q, want openai:gpt-5.1 (default)", loaded.Models.Default)
	}
	if loaded.Models.ContextWindow != 128000 {
		t.Errorf("ContextWindow = %d, want 128000 (default)", loaded.Models.ContextWindow)
	}
	if loaded.Models.DefaultLimits != DefaultAgentLimits {
		t.Errorf("DefaultLimits = %+v, want %+v", loaded.Models.DefaultLimits, DefaultAgentLimits)
	}
}

func TestLoad_AutoCreatesDefaultAgent(t *testing.T) {
	directory := withTempDir(t)

	if err := EnsureDirectories(); err != nil {
		t.Fatalf("EnsureDirectories() error: %v", err)
	}
	// Write a minimal config.
	if err := os.WriteFile(filepath.Join(directory, "config.yaml"), []byte("{}"), 0644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if len(loaded.Agents) != 1 {
		t.Fatalf("len(Agents) = %d, want 1", len(loaded.Agents))
	}
	if loaded.Agents[0].ID != DefaultAgentID {
		t.Errorf("agent ID = %q, want %q", loaded.Agents[0].ID, DefaultAgentID)
	}
}

func TestApplyDefaults_PreservesUserValues(t *testing.T) {
	configuration := &Config{
		Gateway: GatewayConfig{Port: 7777, Bind: "lan"},
		Models: ModelsConfig{
			Default:       "mymodel",
			ContextWindow: 64000,
			DefaultLimits: AgentLimits{
				MaxToolRounds: 111,
			},
			Providers: []ProviderConfig{{Name: "openai", BaseURL: "https://custom.api"}},
		},
	}
	applyDefaults(configuration)

	if configuration.Gateway.Port != 7777 {
		t.Errorf("Port = %d, want 7777 (user-set)", configuration.Gateway.Port)
	}
	if configuration.Gateway.Bind != "lan" {
		t.Errorf("Bind = %q, want lan (user-set)", configuration.Gateway.Bind)
	}
	if configuration.Models.Default != "mymodel" {
		t.Errorf("Default = %q, want mymodel (user-set)", configuration.Models.Default)
	}
	if configuration.Models.Providers[0].BaseURL != "https://custom.api" {
		t.Errorf("BaseURL = %q, want https://custom.api (user-set)", configuration.Models.Providers[0].BaseURL)
	}
	if configuration.Models.ContextWindow != 64000 {
		t.Errorf("ContextWindow = %d, want 64000 (user-set)", configuration.Models.ContextWindow)
	}
	if configuration.Models.DefaultLimits.MaxToolRounds != 111 {
		t.Errorf("DefaultLimits.MaxToolRounds = %d, want 111 (user-set)", configuration.Models.DefaultLimits.MaxToolRounds)
	}
}

func TestApplyDefaults_FillsZeroValues(t *testing.T) {
	configuration := &Config{}
	applyDefaults(configuration)

	if configuration.Gateway.Port != 8833 {
		t.Errorf("Port = %d, want 8833", configuration.Gateway.Port)
	}
	if configuration.Gateway.Bind != "loopback" {
		t.Errorf("Bind = %q, want loopback", configuration.Gateway.Bind)
	}
	if configuration.Models.Default != "openai:gpt-5.1" {
		t.Errorf("Default = %q, want openai:gpt-5.1", configuration.Models.Default)
	}
	if configuration.Models.ContextWindow != 128000 {
		t.Errorf("ContextWindow = %d, want 128000", configuration.Models.ContextWindow)
	}
	if configuration.Models.DefaultLimits != DefaultAgentLimits {
		t.Errorf("DefaultLimits = %+v, want %+v", configuration.Models.DefaultLimits, DefaultAgentLimits)
	}
}

func TestApplyDefaults_PreservesProviders(t *testing.T) {
	configuration := &Config{
		Models: ModelsConfig{
			Providers: []ProviderConfig{
				{Name: "openai", BaseURL: "https://custom.api"},
			},
		},
	}
	applyDefaults(configuration)

	if len(configuration.Models.Providers) != 1 {
		t.Fatalf("len(Providers) = %d, want 1", len(configuration.Models.Providers))
	}
	if configuration.Models.Providers[0].Name != "openai" {
		t.Errorf("Providers[0].Name = %q, want openai", configuration.Models.Providers[0].Name)
	}
	if configuration.Models.Providers[0].BaseURL != "https://custom.api" {
		t.Errorf("Providers[0].BaseURL = %q, want https://custom.api", configuration.Models.Providers[0].BaseURL)
	}
}

func TestApplyEnv(t *testing.T) {
	t.Run("OPENAI_API_KEY", func(t *testing.T) {
		t.Setenv("OPENAI_API_KEY", "sk-test-key")
		configuration := &Config{}
		applyEnv(configuration)
		found := false
		for _, provider := range configuration.Models.Providers {
			if provider.Name == "openai" && provider.APIKey == "sk-test-key" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected openai provider with APIKey sk-test-key, got %+v", configuration.Models.Providers)
		}
	})

	t.Run("TEANODE_GATEWAY_PORT", func(t *testing.T) {
		t.Setenv("TEANODE_GATEWAY_PORT", "9090")
		configuration := &Config{}
		applyEnv(configuration)
		if configuration.Gateway.Port != 9090 {
			t.Errorf("Port = %d, want 9090", configuration.Gateway.Port)
		}
	})

	t.Run("DISCORD_BOT_TOKEN initializes nil Discord", func(t *testing.T) {
		t.Setenv("DISCORD_BOT_TOKEN", "discord-token")
		configuration := &Config{}
		applyEnv(configuration)
		if configuration.Channels.Discord == nil {
			t.Fatal("Discord is nil, expected initialized")
		}
		if configuration.Channels.Discord.Token != "discord-token" {
			t.Errorf("Token = %q, want discord-token", configuration.Channels.Discord.Token)
		}
	})

	t.Run("TELEGRAM_BOT_TOKEN initializes nil Telegram", func(t *testing.T) {
		t.Setenv("TELEGRAM_BOT_TOKEN", "tg-token")
		configuration := &Config{}
		applyEnv(configuration)
		if configuration.Channels.Telegram == nil {
			t.Fatal("Telegram is nil, expected initialized")
		}
		if configuration.Channels.Telegram.Token != "tg-token" {
			t.Errorf("Token = %q, want tg-token", configuration.Channels.Telegram.Token)
		}
	})

	t.Run("BRAVE_API_KEY", func(t *testing.T) {
		t.Setenv("BRAVE_API_KEY", "brave-key")
		configuration := &Config{}
		applyEnv(configuration)
		if configuration.Tools.BraveAPIKey != "brave-key" {
			t.Errorf("BraveAPIKey = %q, want brave-key", configuration.Tools.BraveAPIKey)
		}
	})

	t.Run("TEANODE_CONTEXT_WINDOW", func(t *testing.T) {
		t.Setenv("TEANODE_CONTEXT_WINDOW", "64000")
		configuration := &Config{}
		applyEnv(configuration)
		if configuration.Models.ContextWindow != 64000 {
			t.Errorf("ContextWindow = %d, want 64000", configuration.Models.ContextWindow)
		}
	})

	t.Run("TEANODE_GATEWAY_BIND", func(t *testing.T) {
		t.Setenv("TEANODE_GATEWAY_BIND", "lan")
		configuration := &Config{}
		applyEnv(configuration)
		if configuration.Gateway.Bind != "lan" {
			t.Errorf("Bind = %q, want lan", configuration.Gateway.Bind)
		}
	})

	t.Run("TEANODE_CDP_ENDPOINT initializes nil Browser", func(t *testing.T) {
		t.Setenv("TEANODE_CDP_ENDPOINT", "192.168.1.1:9222")
		configuration := &Config{}
		applyEnv(configuration)
		if configuration.Integrations.Browser == nil {
			t.Fatal("Browser is nil, expected initialized")
		}
		if configuration.Integrations.Browser.CDPEndpoint != "192.168.1.1:9222" {
			t.Errorf("CDPEndpoint = %q, want 192.168.1.1:9222", configuration.Integrations.Browser.CDPEndpoint)
		}
	})

	t.Run("GOG_BINARY_PATH initializes nil Google", func(t *testing.T) {
		t.Setenv("GOG_BINARY_PATH", "/usr/local/bin/gog")
		configuration := &Config{}
		applyEnv(configuration)
		if configuration.Tools.Google == nil {
			t.Fatal("Google is nil, expected initialized")
		}
		if configuration.Tools.Google.BinaryPath != "/usr/local/bin/gog" {
			t.Errorf("BinaryPath = %q, want /usr/local/bin/gog", configuration.Tools.Google.BinaryPath)
		}
	})

	t.Run("GH_BINARY_PATH initializes nil GitHub", func(t *testing.T) {
		t.Setenv("GH_BINARY_PATH", "/usr/local/bin/gh")
		configuration := &Config{}
		applyEnv(configuration)
		if configuration.Tools.GitHub == nil {
			t.Fatal("GitHub is nil, expected initialized")
		}
		if configuration.Tools.GitHub.BinaryPath != "/usr/local/bin/gh" {
			t.Errorf("BinaryPath = %q, want /usr/local/bin/gh", configuration.Tools.GitHub.BinaryPath)
		}
	})

	t.Run("GLAB_BINARY_PATH initializes nil GitLab", func(t *testing.T) {
		t.Setenv("GLAB_BINARY_PATH", "/usr/local/bin/glab")
		configuration := &Config{}
		applyEnv(configuration)
		if configuration.Tools.GitLab == nil {
			t.Fatal("GitLab is nil, expected initialized")
		}
		if configuration.Tools.GitLab.BinaryPath != "/usr/local/bin/glab" {
			t.Errorf("BinaryPath = %q, want /usr/local/bin/glab", configuration.Tools.GitLab.BinaryPath)
		}
	})
}
