package skills

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/teanode/teanode/internal/agents"
	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/providers"
	skillregistry "github.com/teanode/teanode/internal/skills/registry"
)

// RegisterTools adds the skills management tool to the registry.
func RegisterTools(registry *agents.ToolRegistry, config *configs.SkillsRegistryConfig, onSkillsChanged func()) {
	registry.Register(&skillsTool{config: config, onSkillsChanged: onSkillsChanged})
}

// --- skills (multi-action) ---

type skillsTool struct {
	config          *configs.SkillsRegistryConfig
	onSkillsChanged func()
}

func (self *skillsTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name: "skills",
			Description: "Browse and manage installable skills. Actions: list_registry (list configured registry sources), " +
				"search (search online registry), install (install skill from registry), " +
				"list_installed (list currently installed skills), update (update installed skills), " +
				"uninstall (remove installed skill).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"list_registry", "search", "install", "list_installed", "update", "uninstall"},
						"description": "The skills action to perform.",
					},
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Search query for registry search.",
					},
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Skill name (required for install/uninstall, optional for update).",
					},
					"version": map[string]interface{}{
						"type":        "string",
						"description": "Exact version to install (optional).",
					},
					"sourceId": map[string]interface{}{
						"type":        "string",
						"description": "Registry source ID to use for install (optional).",
					},
				},
				"required": []string{"action"},
			},
			Returns: map[string]interface{}{
				"type":        "object",
				"description": "Action-dependent result payload.",
			},
		},
	}
}

func (self *skillsTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Action   string `json:"action"`
		Query    string `json:"query"`
		Name     string `json:"name"`
		Version  string `json:"version"`
		SourceID string `json:"sourceId"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}

	switch arguments.Action {
	case "list_registry":
		return self.executeListRegistry()
	case "search":
		return self.executeSearch(ctx, arguments.Query)
	case "install":
		return self.executeInstall(ctx, arguments.SourceID, arguments.Name, arguments.Version)
	case "list_installed":
		return self.executeListInstalled()
	case "update":
		return self.executeUpdate(ctx, arguments.Name)
	case "uninstall":
		return self.executeUninstall(arguments.Name)
	default:
		return "", fmt.Errorf("unknown skills action: %s", arguments.Action)
	}
}

func (self *skillsTool) executeListRegistry() (string, error) {
	if self.config == nil {
		output, _ := json.Marshal(map[string]interface{}{
			"action":  "list_registry",
			"enabled": false,
			"sources": []interface{}{},
		})
		return string(output), nil
	}
	output, _ := json.Marshal(map[string]interface{}{
		"action":  "list_registry",
		"enabled": self.config.Enabled,
		"policy":  self.config.Policy,
		"updates": self.config.Updates,
		"sources": self.config.Sources,
	})
	return string(output), nil
}

func (self *skillsTool) executeSearch(ctx context.Context, query string) (string, error) {
	results, err := skillregistry.Search(ctx, self.config, query)
	if err != nil {
		return "", fmt.Errorf("searching registry: %w", err)
	}
	output, _ := json.Marshal(map[string]interface{}{
		"action":  "search",
		"query":   query,
		"results": results,
	})
	return string(output), nil
}

func (self *skillsTool) executeInstall(ctx context.Context, sourceID string, name string, version string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("name is required for install action")
	}
	installed, err := skillregistry.Install(ctx, self.config, sourceID, name, version)
	if err != nil {
		return "", fmt.Errorf("installing skill: %w", err)
	}
	output, _ := json.Marshal(map[string]interface{}{
		"action":    "install",
		"installed": installed,
	})
	if self.onSkillsChanged != nil {
		self.onSkillsChanged()
	}
	return string(output), nil
}

func (self *skillsTool) executeListInstalled() (string, error) {
	items, err := skillregistry.ListInstalled()
	if err != nil {
		return "", fmt.Errorf("listing installed skills: %w", err)
	}
	output, _ := json.Marshal(map[string]interface{}{
		"action": "list_installed",
		"skills": items,
	})
	return string(output), nil
}

func (self *skillsTool) executeUpdate(ctx context.Context, name string) (string, error) {
	updated, err := skillregistry.Update(ctx, self.config, name)
	if err != nil {
		return "", fmt.Errorf("updating skills: %w", err)
	}
	output, _ := json.Marshal(map[string]interface{}{
		"action":  "update",
		"updated": updated,
	})
	if len(updated) > 0 && self.onSkillsChanged != nil {
		self.onSkillsChanged()
	}
	return string(output), nil
}

func (self *skillsTool) executeUninstall(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("name is required for uninstall action")
	}
	if err := skillregistry.Uninstall(name); err != nil {
		return "", fmt.Errorf("uninstalling skill: %w", err)
	}
	output, _ := json.Marshal(map[string]interface{}{
		"action":      "uninstall",
		"uninstalled": true,
		"name":        name,
	})
	if self.onSkillsChanged != nil {
		self.onSkillsChanged()
	}
	return string(output), nil
}
