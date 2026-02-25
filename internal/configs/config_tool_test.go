package configs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// --- 1. maskSecrets ---

func TestMaskSecrets_TopLevelPassword(t *testing.T) {
	data := map[string]interface{}{
		"token": "super-secret",
		"name":  "test",
	}
	schema := map[string]interface{}{
		"properties": map[string]interface{}{
			"token": map[string]interface{}{"type": "string", "format": "password"},
			"name":  map[string]interface{}{"type": "string"},
		},
	}
	maskSecrets(data, schema)

	if data["token"] != secretSentinel {
		t.Errorf("token = %q, want %q", data["token"], secretSentinel)
	}
	if data["name"] != "test" {
		t.Errorf("name = %q, want test", data["name"])
	}
}

func TestMaskSecrets_EmptySecretNotMasked(t *testing.T) {
	data := map[string]interface{}{
		"token": "",
	}
	schema := map[string]interface{}{
		"properties": map[string]interface{}{
			"token": map[string]interface{}{"type": "string", "format": "password"},
		},
	}
	maskSecrets(data, schema)

	if data["token"] != "" {
		t.Errorf("empty token should not be masked, got %q", data["token"])
	}
}

func TestMaskSecrets_NestedPassword(t *testing.T) {
	data := map[string]interface{}{
		"gateway": map[string]interface{}{
			"forwarderKey": "secret-key",
			"port":         float64(8833),
		},
	}
	schema := map[string]interface{}{
		"properties": map[string]interface{}{
			"gateway": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"forwarderKey": map[string]interface{}{"type": "string", "format": "password"},
					"port":         map[string]interface{}{"type": "number"},
				},
			},
		},
	}
	maskSecrets(data, schema)

	gateway := data["gateway"].(map[string]interface{})
	if gateway["forwarderKey"] != secretSentinel {
		t.Errorf("forwarderKey = %q, want %q", gateway["forwarderKey"], secretSentinel)
	}
	if gateway["port"] != float64(8833) {
		t.Errorf("port = %v, want 8833", gateway["port"])
	}
}

func TestMaskSecrets_ArrayItems(t *testing.T) {
	data := map[string]interface{}{
		"providers": []interface{}{
			map[string]interface{}{"name": "openai", "apiKey": "sk-123"},
			map[string]interface{}{"name": "anthropic", "apiKey": ""},
		},
	}
	schema := map[string]interface{}{
		"properties": map[string]interface{}{
			"providers": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"name":   map[string]interface{}{"type": "string"},
						"apiKey": map[string]interface{}{"type": "string", "format": "password"},
					},
				},
			},
		},
	}
	maskSecrets(data, schema)

	providers := data["providers"].([]interface{})
	first := providers[0].(map[string]interface{})
	second := providers[1].(map[string]interface{})

	if first["apiKey"] != secretSentinel {
		t.Errorf("providers[0].apiKey = %q, want %q", first["apiKey"], secretSentinel)
	}
	if first["name"] != "openai" {
		t.Errorf("providers[0].name = %q, want openai", first["name"])
	}
	if second["apiKey"] != "" {
		t.Errorf("providers[1].apiKey = %q, want empty (not masked)", second["apiKey"])
	}
}

func TestMaskSecrets_MissingKeyInData(t *testing.T) {
	data := map[string]interface{}{
		"name": "test",
	}
	schema := map[string]interface{}{
		"properties": map[string]interface{}{
			"token": map[string]interface{}{"type": "string", "format": "password"},
			"name":  map[string]interface{}{"type": "string"},
		},
	}
	// Should not panic on missing keys.
	maskSecrets(data, schema)

	if _, exists := data["token"]; exists {
		t.Error("should not create missing keys")
	}
}

// --- 2. stripSentinels ---

func TestStripSentinels_RestoresFromOriginal(t *testing.T) {
	partial := map[string]interface{}{
		"token": secretSentinel,
		"name":  "new-name",
	}
	original := map[string]interface{}{
		"token": "real-secret",
		"name":  "old-name",
	}
	stripSentinels(partial, original)

	if partial["token"] != "real-secret" {
		t.Errorf("token = %q, want real-secret", partial["token"])
	}
	if partial["name"] != "new-name" {
		t.Errorf("name = %q, want new-name (unchanged)", partial["name"])
	}
}

func TestStripSentinels_DeletesWhenNoOriginal(t *testing.T) {
	partial := map[string]interface{}{
		"token": secretSentinel,
	}
	original := map[string]interface{}{}
	stripSentinels(partial, original)

	if _, exists := partial["token"]; exists {
		t.Error("sentinel should be deleted when no original value exists")
	}
}

func TestStripSentinels_PreservesRealNewValues(t *testing.T) {
	partial := map[string]interface{}{
		"token": "brand-new-key",
	}
	original := map[string]interface{}{
		"token": "old-key",
	}
	stripSentinels(partial, original)

	if partial["token"] != "brand-new-key" {
		t.Errorf("token = %q, want brand-new-key", partial["token"])
	}
}

func TestStripSentinels_PreservesIntentionalClear(t *testing.T) {
	partial := map[string]interface{}{
		"token": "",
	}
	original := map[string]interface{}{
		"token": "old-key",
	}
	stripSentinels(partial, original)

	if partial["token"] != "" {
		t.Errorf("token = %q, want empty string (intentional clear)", partial["token"])
	}
}

func TestStripSentinels_NestedMaps(t *testing.T) {
	partial := map[string]interface{}{
		"gateway": map[string]interface{}{
			"forwarderKey": secretSentinel,
			"port":         float64(9999),
		},
	}
	original := map[string]interface{}{
		"gateway": map[string]interface{}{
			"forwarderKey": "real-forwarder-key",
			"port":         float64(8833),
		},
	}
	stripSentinels(partial, original)

	gateway := partial["gateway"].(map[string]interface{})
	if gateway["forwarderKey"] != "real-forwarder-key" {
		t.Errorf("forwarderKey = %q, want real-forwarder-key", gateway["forwarderKey"])
	}
	if gateway["port"] != float64(9999) {
		t.Errorf("port = %v, want 9999", gateway["port"])
	}
}

func TestStripSentinels_ArrayElements(t *testing.T) {
	partial := map[string]interface{}{
		"providers": []interface{}{
			map[string]interface{}{"name": "openai", "apiKey": secretSentinel},
			map[string]interface{}{"name": "anthropic", "apiKey": "new-key"},
		},
	}
	original := map[string]interface{}{
		"providers": []interface{}{
			map[string]interface{}{"name": "openai", "apiKey": "sk-original"},
			map[string]interface{}{"name": "anthropic", "apiKey": "sk-old"},
		},
	}
	stripSentinels(partial, original)

	providers := partial["providers"].([]interface{})
	first := providers[0].(map[string]interface{})
	second := providers[1].(map[string]interface{})

	if first["apiKey"] != "sk-original" {
		t.Errorf("providers[0].apiKey = %q, want sk-original", first["apiKey"])
	}
	if second["apiKey"] != "new-key" {
		t.Errorf("providers[1].apiKey = %q, want new-key", second["apiKey"])
	}
}

func TestStripSentinels_ArrayLongerThanOriginal(t *testing.T) {
	partial := map[string]interface{}{
		"providers": []interface{}{
			map[string]interface{}{"name": "openai", "apiKey": secretSentinel},
			map[string]interface{}{"name": "new-provider", "apiKey": secretSentinel},
		},
	}
	original := map[string]interface{}{
		"providers": []interface{}{
			map[string]interface{}{"name": "openai", "apiKey": "sk-original"},
		},
	}
	stripSentinels(partial, original)

	providers := partial["providers"].([]interface{})
	first := providers[0].(map[string]interface{})
	second := providers[1].(map[string]interface{})

	if first["apiKey"] != "sk-original" {
		t.Errorf("providers[0].apiKey = %q, want sk-original", first["apiKey"])
	}
	// Second element has no original — sentinel should be deleted.
	if _, exists := second["apiKey"]; exists {
		t.Errorf("providers[1].apiKey should be deleted (no original), got %q", second["apiKey"])
	}
}

func TestStripSentinels_NilOriginal(t *testing.T) {
	partial := map[string]interface{}{
		"token": secretSentinel,
		"name":  "test",
	}
	stripSentinels(partial, nil)

	if _, exists := partial["token"]; exists {
		t.Error("sentinel should be deleted when original is nil")
	}
	if partial["name"] != "test" {
		t.Errorf("name = %q, want test", partial["name"])
	}
}

// --- 3. backupConfig ---

func TestBackupConfig_CreatesBackup(t *testing.T) {
	directory := withTempDir(t)

	configPath := filepath.Join(directory, "config.yaml")
	if err := os.WriteFile(configPath, []byte("gateway:\n  port: 8833\n"), 0644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	if err := backupConfig(); err != nil {
		t.Fatalf("backupConfig() error: %v", err)
	}

	backupPath := filepath.Join(directory, ".config.yaml.bak")
	data, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("backup file not found: %v", err)
	}
	if string(data) != "gateway:\n  port: 8833\n" {
		t.Errorf("backup content = %q, want original config content", string(data))
	}
}

func TestBackupConfig_NoConfigFile(t *testing.T) {
	withTempDir(t)

	// Should succeed silently when no config file exists.
	if err := backupConfig(); err != nil {
		t.Errorf("backupConfig() should not error when no config file exists, got: %v", err)
	}
}

// --- 4. Integration: executeGet masks secrets ---

func TestExecuteGet_MasksSecrets(t *testing.T) {
	withTempDir(t)

	configuration := &Config{
		Gateway: GatewayConfig{Port: 8833, ForwarderKey: "my-forwarder-secret"},
		Tools:   ToolsConfig{BraveAPIKey: "brave-secret-key"},
		Models: ModelsConfig{
			Providers: []ProviderConfig{
				{Name: "openai", APIKey: "sk-secret-123"},
			},
		},
		Channels: ChannelsConfig{
			Discord:  &DiscordConfig{Token: "discord-secret"},
			Telegram: &TelegramConfig{Token: "telegram-secret"},
		},
	}
	tool := &ConfigTool{Config: configuration}

	result, err := tool.Execute(nil, `{"action":"get"}`)
	if err != nil {
		t.Fatalf("executeGet() error: %v", err)
	}

	var response map[string]interface{}
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("parsing response: %v", err)
	}

	configMap := response["config"].(map[string]interface{})

	// Gateway forwarder key should be masked.
	gateway := configMap["gateway"].(map[string]interface{})
	if gateway["forwarderKey"] != secretSentinel {
		t.Errorf("gateway.forwarderKey = %q, want %q", gateway["forwarderKey"], secretSentinel)
	}
	// Port should be unmasked.
	if gateway["port"] != float64(8833) {
		t.Errorf("gateway.port = %v, want 8833", gateway["port"])
	}

	// Brave API key should be masked.
	tools := configMap["tools"].(map[string]interface{})
	if tools["braveApiKey"] != secretSentinel {
		t.Errorf("tools.braveApiKey = %q, want %q", tools["braveApiKey"], secretSentinel)
	}

	// Provider API key should be masked.
	models := configMap["models"].(map[string]interface{})
	providers := models["providers"].([]interface{})
	firstProvider := providers[0].(map[string]interface{})
	if firstProvider["apiKey"] != secretSentinel {
		t.Errorf("models.providers[0].apiKey = %q, want %q", firstProvider["apiKey"], secretSentinel)
	}
	if firstProvider["name"] != "openai" {
		t.Errorf("models.providers[0].name = %q, want openai", firstProvider["name"])
	}

	// Channel tokens should be masked.
	channels := configMap["channels"].(map[string]interface{})
	discord := channels["discord"].(map[string]interface{})
	if discord["token"] != secretSentinel {
		t.Errorf("channels.discord.token = %q, want %q", discord["token"], secretSentinel)
	}
	telegram := channels["telegram"].(map[string]interface{})
	if telegram["token"] != secretSentinel {
		t.Errorf("channels.telegram.token = %q, want %q", telegram["token"], secretSentinel)
	}
}

func TestExecuteGet_EmptySecretsNotMasked(t *testing.T) {
	withTempDir(t)

	configuration := &Config{
		Gateway: GatewayConfig{Port: 8833},
		Tools:   ToolsConfig{BraveAPIKey: ""},
	}
	tool := &ConfigTool{Config: configuration}

	result, err := tool.Execute(nil, `{"action":"get"}`)
	if err != nil {
		t.Fatalf("executeGet() error: %v", err)
	}

	var response map[string]interface{}
	json.Unmarshal([]byte(result), &response)
	configMap := response["config"].(map[string]interface{})

	// Empty brave key should not appear (omitempty) or be empty, not masked.
	if tools, ok := configMap["tools"].(map[string]interface{}); ok {
		if braveKey, exists := tools["braveApiKey"]; exists && braveKey != "" {
			t.Errorf("tools.braveApiKey = %q, want empty (not masked)", braveKey)
		}
	}
}

// --- 5. Integration: executeSet strips sentinels and creates backup ---

func TestExecuteSet_StripsSentinels(t *testing.T) {
	directory := withTempDir(t)

	if err := EnsureDirectories(); err != nil {
		t.Fatalf("EnsureDirectories() error: %v", err)
	}

	// Save an initial config with a real secret.
	initial := &Config{
		Gateway: GatewayConfig{Port: 8833, ForwarderKey: "original-secret"},
	}
	if err := Save(initial); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	tool := &ConfigTool{Config: initial}

	// LLM sends back the sentinel (echoing masked value) along with a real change.
	partialJSON := `{"gateway":{"forwarderKey":"<hidden>","port":9999}}`
	result, err := tool.Execute(nil, `{"action":"set","config":`+partialJSON+`}`)
	if err != nil {
		t.Fatalf("executeSet() error: %v", err)
	}

	var response map[string]interface{}
	json.Unmarshal([]byte(result), &response)
	if response["ok"] != true {
		t.Errorf("ok = %v, want true", response["ok"])
	}

	// Reload and verify: secret should be preserved, port should be updated.
	loaded, err := LoadRaw()
	if err != nil {
		t.Fatalf("LoadRaw() error: %v", err)
	}
	if loaded.Gateway.ForwarderKey != "original-secret" {
		t.Errorf("ForwarderKey = %q, want original-secret (preserved)", loaded.Gateway.ForwarderKey)
	}
	if loaded.Gateway.Port != 9999 {
		t.Errorf("Port = %d, want 9999", loaded.Gateway.Port)
	}

	// Verify backup was created.
	backupPath := filepath.Join(directory, ".config.yaml.bak")
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		t.Error(".config.yaml.bak should have been created")
	}
}

func TestExecuteSet_AllowsNewSecretValues(t *testing.T) {
	withTempDir(t)

	if err := EnsureDirectories(); err != nil {
		t.Fatalf("EnsureDirectories() error: %v", err)
	}

	// Start with an existing config.
	initial := &Config{
		Gateway: GatewayConfig{Port: 8833, ForwarderKey: "old-secret"},
	}
	if err := Save(initial); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	tool := &ConfigTool{Config: initial}

	// LLM sends a genuinely new secret value.
	partialJSON := `{"gateway":{"forwarderKey":"brand-new-secret"}}`
	_, err := tool.Execute(nil, `{"action":"set","config":`+partialJSON+`}`)
	if err != nil {
		t.Fatalf("executeSet() error: %v", err)
	}

	loaded, err := LoadRaw()
	if err != nil {
		t.Fatalf("LoadRaw() error: %v", err)
	}
	if loaded.Gateway.ForwarderKey != "brand-new-secret" {
		t.Errorf("ForwarderKey = %q, want brand-new-secret", loaded.Gateway.ForwarderKey)
	}
}

func TestExecuteSet_ProviderArraySentinelRestore(t *testing.T) {
	withTempDir(t)

	if err := EnsureDirectories(); err != nil {
		t.Fatalf("EnsureDirectories() error: %v", err)
	}

	// Save a config with provider API keys.
	initial := &Config{
		Models: ModelsConfig{
			Providers: []ProviderConfig{
				{Name: "openai", APIKey: "sk-real-openai"},
				{Name: "anthropic", APIKey: "sk-real-anthropic"},
			},
		},
	}
	if err := Save(initial); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	tool := &ConfigTool{Config: initial}

	// LLM echoes back sentinels for both provider API keys (array replacement via deepMerge).
	partialJSON := `{"models":{"providers":[{"name":"openai","apiKey":"<hidden>"},{"name":"anthropic","apiKey":"<hidden>"}]}}`
	_, err := tool.Execute(nil, `{"action":"set","config":`+partialJSON+`}`)
	if err != nil {
		t.Fatalf("executeSet() error: %v", err)
	}

	loaded, err := LoadRaw()
	if err != nil {
		t.Fatalf("LoadRaw() error: %v", err)
	}
	if len(loaded.Models.Providers) != 2 {
		t.Fatalf("len(providers) = %d, want 2", len(loaded.Models.Providers))
	}
	if loaded.Models.Providers[0].APIKey != "sk-real-openai" {
		t.Errorf("providers[0].apiKey = %q, want sk-real-openai", loaded.Models.Providers[0].APIKey)
	}
	if loaded.Models.Providers[1].APIKey != "sk-real-anthropic" {
		t.Errorf("providers[1].apiKey = %q, want sk-real-anthropic", loaded.Models.Providers[1].APIKey)
	}
}
