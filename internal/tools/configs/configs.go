// Package configs exposes tools for reading and updating configuration.
package configs

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/schemas"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/tools"
)

func init() {
	tools.RegisterBuiltinTool(func() []tools.Tool {
		return []tools.Tool{newConfigurationTool()}
	})
}

const secretSentinel = "<hidden>"

type configurationTool struct{}

func newConfigurationTool() *configurationTool {
	return &configurationTool{}
}

func (self *configurationTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name: "config",
			Description: "View or modify the teanode configuration. Actions: " +
				"get (return the current effective config with defaults applied; secret fields are masked as \"<hidden>\"), " +
				"set (deep-merge a partial JSON config into the stored config and save), " +
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

func (self *configurationTool) PolicyGroups() []tools.PolicyGroup {
	return []tools.PolicyGroup{
		{Group: models.ToolPolicyGroupRead, Default: models.ToolPolicyAnyone, Actions: []string{"get", "schema"}},
		{Group: models.ToolPolicyGroupWrite, Default: models.ToolPolicyAdminOnly},
	}
}

func (self *configurationTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	user := models.UserFromContext(ctx)
	if user == nil || !user.GetAdmin() {
		return "", fmt.Errorf("configs: admin access required to use the config tool")
	}

	var arguments struct {
		Action string          `json:"action"`
		Config json.RawMessage `json:"config"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("configs: parsing arguments: %w", err)
	}

	switch arguments.Action {
	case "get":
		return self.executeGet(ctx)
	case "set":
		return self.executeSet(ctx, arguments.Config)
	case "schema":
		return self.executeSchema()
	default:
		return "", fmt.Errorf("configs: unknown config action: %s", arguments.Action)
	}
}

func (self *configurationTool) executeGet(ctx context.Context) (string, error) {
	var configuration *models.Configuration
	if transactionError := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		loadedConfiguration, loadError := transaction.GetConfiguration(ctx, nil)
		if loadError != nil {
			return loadError
		}
		configuration = loadedConfiguration
		return nil
	}); transactionError != nil {
		return "", fmt.Errorf("configs: loading config: %w", transactionError)
	}

	configurationData, marshalError := json.Marshal(configuration)
	if marshalError != nil {
		return "", fmt.Errorf("configs: marshalling config: %w", marshalError)
	}
	configurationMap := map[string]interface{}{}
	if unmarshalError := json.Unmarshal(configurationData, &configurationMap); unmarshalError != nil {
		return "", fmt.Errorf("configs: parsing config: %w", unmarshalError)
	}

	schema := map[string]interface{}{}
	if unmarshalError := json.Unmarshal(schemas.ConfigSchema(), &schema); unmarshalError != nil {
		return "", fmt.Errorf("configs: parsing schema: %w", unmarshalError)
	}
	maskSecrets(configurationMap, schema)

	output, marshalError := json.Marshal(map[string]interface{}{
		"action": "get",
		"config": configurationMap,
	})
	if marshalError != nil {
		return "", fmt.Errorf("configs: marshalling config: %w", marshalError)
	}
	return string(output), nil
}

func (self *configurationTool) executeSet(ctx context.Context, partial json.RawMessage) (string, error) {
	if len(partial) == 0 {
		return "", fmt.Errorf("configs: config is required for set action")
	}
	partialMap := map[string]interface{}{}
	if unmarshalError := json.Unmarshal(partial, &partialMap); unmarshalError != nil {
		return "", fmt.Errorf("configs: invalid config object: %w", unmarshalError)
	}

	if transactionError := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		_, modifyError := transaction.ModifyConfiguration(ctx, func(configuration *models.Configuration) error {
			if configuration == nil {
				return fmt.Errorf("configs: configuration is required")
			}
			// Round-trip current config and partial to maps for sentinel stripping.
			currentData, marshalError := json.Marshal(configuration)
			if marshalError != nil {
				return fmt.Errorf("configs: marshalling config: %w", marshalError)
			}
			currentMap := map[string]interface{}{}
			if unmarshalError := json.Unmarshal(currentData, &currentMap); unmarshalError != nil {
				return fmt.Errorf("configs: parsing config: %w", unmarshalError)
			}

			partialCopyData, marshalPartialError := json.Marshal(partialMap)
			if marshalPartialError != nil {
				return fmt.Errorf("configs: marshalling partial config: %w", marshalPartialError)
			}
			partialCopy := map[string]interface{}{}
			if unmarshalPartialError := json.Unmarshal(partialCopyData, &partialCopy); unmarshalPartialError != nil {
				return fmt.Errorf("configs: parsing partial config: %w", unmarshalPartialError)
			}

			// Restore masked sentinel values from the original config.
			stripSentinels(partialCopy, currentMap)

			// Unmarshal the stripped partial into a typed struct and deep merge
			// via generated Update() — only non-nil fields are applied, nested
			// structs are recursively merged.
			strippedData, marshalStrippedError := json.Marshal(partialCopy)
			if marshalStrippedError != nil {
				return fmt.Errorf("configs: marshalling stripped config: %w", marshalStrippedError)
			}
			var partialConfiguration models.Configuration
			if unmarshalStrippedError := json.Unmarshal(strippedData, &partialConfiguration); unmarshalStrippedError != nil {
				return fmt.Errorf("configs: parsing stripped config: %w", unmarshalStrippedError)
			}
			configuration.Update(&partialConfiguration)
			return nil
		}, nil)
		return modifyError
	}); transactionError != nil {
		return "", fmt.Errorf("configs: saving config: %w", transactionError)
	}

	output, _ := json.Marshal(map[string]interface{}{
		"action": "set",
		"ok":     true,
	})
	return string(output), nil
}

func (self *configurationTool) executeSchema() (string, error) {
	output, _ := json.Marshal(map[string]interface{}{
		"action": "schema",
		"schema": schemas.ConfigSchema(),
	})
	return string(output), nil
}

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

		if format, ok := propertyMap["format"].(string); ok && format == "password" {
			if stringValue, ok := dataValue.(string); ok && stringValue != "" {
				data[key] = secretSentinel
			}
			continue
		}

		if nestedData, ok := dataValue.(map[string]interface{}); ok {
			maskSecrets(nestedData, propertyMap)
			continue
		}

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

func stripSentinels(partial map[string]interface{}, original map[string]interface{}) {
	for key, partialValue := range partial {
		if stringValue, ok := partialValue.(string); ok && stringValue == secretSentinel {
			if originalValue, exists := original[key]; exists {
				partial[key] = originalValue
			} else {
				delete(partial, key)
			}
			continue
		}

		if partialMap, ok := partialValue.(map[string]interface{}); ok {
			if originalMap, ok := original[key].(map[string]interface{}); ok {
				stripSentinels(partialMap, originalMap)
			} else {
				stripSentinels(partialMap, nil)
			}
			continue
		}

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
