package configs

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/teanode/teanode/internal/provider"
)

// ConfigTool is an LLM tool that lets agents inspect and modify the teanode
// configuration at runtime. It implements the agents.Tool interface (Definition
// + Execute) but lives in the configs package to avoid a separate tools/config
// package.
type ConfigTool struct {
	Config *Config
}

func (self *ConfigTool) Definition() provider.ToolDefinition {
	return provider.ToolDefinition{
		Type: "function",
		Function: provider.FunctionSpec{
			Name: "config",
			Description: "View or modify the teanode configuration. Actions: get (return the current effective config with defaults applied), " +
				"set (deep-merge a partial JSON config into the on-disk config and save; triggers hot-reload), " +
				"schema (return the JSON schema describing all config fields, types, and defaults).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"get", "set", "schema"},
						"description": "The config action to perform.",
					},
					"config": map[string]interface{}{
						"type":        "object",
						"description": "Partial config object to deep-merge into the current config (for set action).",
					},
				},
				"required": []string{"action"},
			},
			Returns: map[string]interface{}{
				"type":        "object",
				"description": "Action-dependent result. get: {action, config}. set: {action, ok}. schema: {action, schema}.",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{"type": "string", "description": "The action that was performed"},
					"config": map[string]interface{}{"type": "object", "description": "The current effective config (get)"},
					"ok":     map[string]interface{}{"type": "boolean", "description": "Whether the set action succeeded"},
					"schema": map[string]interface{}{"type": "object", "description": "The config JSON schema (schema)"},
				},
			},
		},
	}
}

func (self *ConfigTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Action string          `json:"action"`
		Config json.RawMessage `json:"config"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}

	switch arguments.Action {
	case "get":
		return self.executeGet()
	case "set":
		return self.executeSet(arguments.Config)
	case "schema":
		return self.executeSchema()
	default:
		return "", fmt.Errorf("unknown config action: %s", arguments.Action)
	}
}

func (self *ConfigTool) executeGet() (string, error) {
	output, err := json.Marshal(map[string]interface{}{
		"action": "get",
		"config": self.Config,
	})
	if err != nil {
		return "", fmt.Errorf("marshalling config: %w", err)
	}
	return string(output), nil
}

func (self *ConfigTool) executeSet(partial json.RawMessage) (string, error) {
	if len(partial) == 0 {
		return "", fmt.Errorf("config is required for set action")
	}

	// Load raw config from disk (no defaults, no env overrides).
	rawConfig, err := LoadRaw()
	if err != nil {
		return "", fmt.Errorf("loading config: %w", err)
	}

	// Round-trip raw config to a map for merging.
	currentData, err := json.Marshal(rawConfig)
	if err != nil {
		return "", fmt.Errorf("marshalling config: %w", err)
	}
	var currentMap map[string]interface{}
	if err := json.Unmarshal(currentData, &currentMap); err != nil {
		return "", fmt.Errorf("parsing config: %w", err)
	}

	// Parse the incoming partial config.
	var partialMap map[string]interface{}
	if err := json.Unmarshal(partial, &partialMap); err != nil {
		return "", fmt.Errorf("invalid config object: %w", err)
	}

	// Deep merge: recursively merge maps so nested values are preserved.
	deepMerge(currentMap, partialMap)

	// Unmarshal merged map back to Config struct.
	mergedData, err := json.Marshal(currentMap)
	if err != nil {
		return "", fmt.Errorf("marshalling merged config: %w", err)
	}
	var mergedConfig Config
	if err := json.Unmarshal(mergedData, &mergedConfig); err != nil {
		return "", fmt.Errorf("parsing merged config: %w", err)
	}

	// Save to disk. The file watcher will trigger hot-reload.
	if err := Save(&mergedConfig); err != nil {
		return "", fmt.Errorf("saving config: %w", err)
	}

	output, _ := json.Marshal(map[string]interface{}{
		"action": "set",
		"ok":     true,
	})
	return string(output), nil
}

func (self *ConfigTool) executeSchema() (string, error) {
	output, _ := json.Marshal(map[string]interface{}{
		"action": "schema",
		"schema": ConfigSchema(),
	})
	return string(output), nil
}

// deepMerge recursively merges source into destination. For keys where both
// sides are maps, it recurses. Otherwise the source value replaces the
// destination value.
func deepMerge(destination map[string]interface{}, source map[string]interface{}) {
	for key, sourceValue := range source {
		if sourceMap, ok := sourceValue.(map[string]interface{}); ok {
			if destinationMap, ok := destination[key].(map[string]interface{}); ok {
				deepMerge(destinationMap, sourceMap)
				continue
			}
		}
		destination[key] = sourceValue
	}
}
