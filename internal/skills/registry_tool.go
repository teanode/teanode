package skills

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/teanode/teanode/internal/agents"
	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/providers"
)

// RegisterTools adds the skills management tool to the registry.
func RegisterTools(registry *agents.ToolRegistry, registries []configs.SkillsRegistry, onSkillsChanged func()) {
	registry.Register(&skillsTool{registries: registries, onSkillsChanged: onSkillsChanged})
}

// --- skills (multi-action) ---

type skillsTool struct {
	registries      []configs.SkillsRegistry
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
				"uninstall (remove installed skill), enable (enable an installed skill), disable (disable an installed skill).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"list_registry", "search", "install", "list_installed", "update", "uninstall", "enable", "disable"},
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
		return self.executeListInstalled(ctx)
	case "update":
		return self.executeUpdate(ctx, arguments.Name)
	case "uninstall":
		return self.executeUninstall(ctx, arguments.Name)
	case "enable":
		return self.executeSetEnabled(ctx, arguments.Name, true)
	case "disable":
		return self.executeSetEnabled(ctx, arguments.Name, false)
	default:
		return "", fmt.Errorf("unknown skills action: %s", arguments.Action)
	}
}

func (self *skillsTool) executeListRegistry() (string, error) {
	if len(self.registries) == 0 {
		output, _ := json.Marshal(map[string]interface{}{
			"action":     "list_registry",
			"registries": []interface{}{},
		})
		return string(output), nil
	}
	output, _ := json.Marshal(map[string]interface{}{
		"action":     "list_registry",
		"registries": self.registries,
	})
	return string(output), nil
}

func (self *skillsTool) executeSearch(ctx context.Context, query string) (string, error) {
	results, err := Search(ctx, self.registries, query)
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

func (self *skillsTool) executeInstall(ctx context.Context, sourceId string, name string, version string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("name is required for install action")
	}
	installed, err := Install(ctx, self.registries, sourceId, name, version)
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

func (self *skillsTool) executeListInstalled(ctx context.Context) (string, error) {
	items, err := ListInstalled(ctx)
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
	updated, err := Update(ctx, self.registries, name)
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

func (self *skillsTool) executeUninstall(ctx context.Context, name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("name is required for uninstall action")
	}
	if err := Uninstall(ctx, name); err != nil {
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

func (self *skillsTool) executeSetEnabled(ctx context.Context, name string, enabled bool) (string, error) {
	if name == "" {
		return "", fmt.Errorf("name is required for enable/disable action")
	}
	if err := SetInstalledSkillEnabled(ctx, name, enabled); err != nil {
		return "", fmt.Errorf("setting installed skill enabled state: %w", err)
	}
	items, err := ListInstalled(ctx)
	if err != nil {
		return "", fmt.Errorf("listing installed skills: %w", err)
	}
	var selected *InstalledSkill
	for index := range items {
		if items[index].Name == name {
			skill := items[index]
			selected = &skill
			break
		}
	}
	output, _ := json.Marshal(map[string]interface{}{
		"action":  "set_enabled",
		"name":    name,
		"enabled": enabled,
		"skill":   selected,
		"skills":  items,
	})
	if self.onSkillsChanged != nil {
		self.onSkillsChanged()
	}
	return string(output), nil
}
