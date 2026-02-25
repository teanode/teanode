package configs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/util/atomicfile"
)

// secretSentinel is the placeholder shown to the LLM for masked password fields.
const secretSentinel = "<hidden>"

// ConfigTool is an LLM tool that lets agents inspect and modify the teanode
// configuration at runtime. It implements the agents.Tool interface (Definition
// + Execute) but lives in the configs package to avoid a separate tools/config
// package.
type ConfigTool struct {
	Config *Config
}

func (self *ConfigTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name: "config",
			Description: "View or modify the teanode configuration. Actions: " +
				"get (return the current effective config with defaults applied; secret fields are masked as \"<hidden>\"), " +
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
	// Round-trip config to a generic map so we can mask secrets.
	configData, err := json.Marshal(self.Config)
	if err != nil {
		return "", fmt.Errorf("marshalling config: %w", err)
	}
	var configMap map[string]interface{}
	if err := json.Unmarshal(configData, &configMap); err != nil {
		return "", fmt.Errorf("parsing config: %w", err)
	}

	// Parse the embedded schema for password field discovery.
	var schema map[string]interface{}
	if err := json.Unmarshal(configSchemaJson, &schema); err != nil {
		return "", fmt.Errorf("parsing schema: %w", err)
	}

	maskSecrets(configMap, schema)

	output, err := json.Marshal(map[string]interface{}{
		"action": "get",
		"config": configMap,
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

	// Restore sentinel values from the current config before merging.
	stripSentinels(partialMap, currentMap)

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

	// Create a backup before saving.
	if err := backupConfig(); err != nil {
		return "", fmt.Errorf("creating backup: %w", err)
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

// maskSecrets walks data and schema in parallel, replacing non-empty string
// values that have "format": "password" in the schema with the sentinel.
// It recurses into nested objects and array items.
func maskSecrets(data map[string]interface{}, schema map[string]interface{}) {
	properties, ok := schema["properties"].(map[string]interface{})
	if !ok {
		return
	}
	for key, propertySchema := range properties {
		propertyMap, ok := propertySchema.(map[string]interface{})
		if !ok {
			continue
		}
		dataValue, exists := data[key]
		if !exists {
			continue
		}

		// Check if this property is a password field.
		if format, ok := propertyMap["format"].(string); ok && format == "password" {
			if stringValue, ok := dataValue.(string); ok && stringValue != "" {
				data[key] = secretSentinel
			}
			continue
		}

		// Recurse into nested objects.
		if nestedData, ok := dataValue.(map[string]interface{}); ok {
			maskSecrets(nestedData, propertyMap)
			continue
		}

		// Recurse into array items.
		if arrayData, ok := dataValue.([]interface{}); ok {
			itemsSchema, ok := propertyMap["items"].(map[string]interface{})
			if !ok {
				continue
			}
			for _, element := range arrayData {
				if elementMap, ok := element.(map[string]interface{}); ok {
					maskSecrets(elementMap, itemsSchema)
				}
			}
		}
	}
}

// stripSentinels walks partial and replaces sentinel values with the
// corresponding value from original. If the original has no value for that key,
// the key is deleted from partial (preventing the sentinel from being persisted).
// It recurses into nested maps and array elements matched by index.
func stripSentinels(partial map[string]interface{}, original map[string]interface{}) {
	for key, partialValue := range partial {
		// String sentinel at top level.
		if stringValue, ok := partialValue.(string); ok && stringValue == secretSentinel {
			if originalValue, exists := original[key]; exists {
				partial[key] = originalValue
			} else {
				delete(partial, key)
			}
			continue
		}

		// Recurse into nested maps.
		if partialMap, ok := partialValue.(map[string]interface{}); ok {
			if originalMap, ok := original[key].(map[string]interface{}); ok {
				stripSentinels(partialMap, originalMap)
			} else {
				stripSentinels(partialMap, nil)
			}
			continue
		}

		// Recurse into arrays (match elements by index).
		if partialArray, ok := partialValue.([]interface{}); ok {
			originalArray, _ := original[key].([]interface{})
			for index, element := range partialArray {
				partialElement, ok := element.(map[string]interface{})
				if !ok {
					continue
				}
				var originalElement map[string]interface{}
				if index < len(originalArray) {
					originalElement, _ = originalArray[index].(map[string]interface{})
				}
				if originalElement != nil {
					stripSentinels(partialElement, originalElement)
				} else {
					stripSentinels(partialElement, nil)
				}
			}
		}
	}
}

// backupConfig copies config.yaml to config.yaml.bak before saving changes.
// If no config file exists yet, the backup is silently skipped.
func backupConfig() error {
	configPath := ConfigFilename()
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading config for backup: %w", err)
	}
	backupPath := filepath.Join(configDirectory, ".config.yaml.bak")
	return atomicfile.WriteFile(backupPath, data)
}
