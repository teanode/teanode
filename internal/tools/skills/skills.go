// Package skills exposes a built-in tool for discovering and managing skills.
package skills

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/skills"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/tools"
	"github.com/teanode/teanode/internal/util/ptrto"
)

func init() {
	tools.RegisterBuiltinTool(func() []tools.Tool {
		return []tools.Tool{&skillsTool{}}
	})
}

type skillsTool struct{}

func (self *skillsTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name:        "skills",
			Description: "Manage the skill library: search and install skills from the official library, list installed skills, enable/disable, update, or uninstall them.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"search", "install", "update", "list_installed", "uninstall", "enable", "disable"},
						"description": "The skill action to perform.",
					},
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Search query (for search). Matches name, description, and tags.",
					},
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Skill name (for install, update, uninstall, enable, disable).",
					},
					"version": map[string]interface{}{
						"type":        "string",
						"description": "Specific version to install (for install). If omitted, installs the latest.",
					},
				},
				"required": []string{"action"},
			},
		},
	}
}

func (self *skillsTool) PolicyGroups() []tools.PolicyGroup {
	return []tools.PolicyGroup{
		{Group: models.ToolPolicyGroupRead, Default: models.ToolPolicyAnyone, Actions: []string{"list_registry", "search", "list_installed"}},
		{Group: models.ToolPolicyGroupWrite, Default: models.ToolPolicyAdminOnly},
	}
}

func (self *skillsTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Action  string `json:"action"`
		Query   string `json:"query"`
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}

	switch arguments.Action {
	case "search":
		return self.executeSearch(ctx, arguments.Query)
	case "install":
		return self.executeInstall(ctx, arguments.Name, arguments.Version)
	case "update":
		return self.executeUpdate(ctx, arguments.Name)
	case "list_installed":
		return self.executeListInstalled(ctx)
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

func (self *skillsTool) executeSearch(ctx context.Context, query string) (string, error) {
	results, err := skills.Search(ctx, query)
	if err != nil {
		return "", err
	}
	output, _ := json.Marshal(map[string]interface{}{
		"action":  "search",
		"results": results,
		"count":   len(results),
	})
	return string(output), nil
}

func (self *skillsTool) executeInstall(ctx context.Context, name, version string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("name is required for install")
	}
	info, err := skills.Install(ctx, name, version)
	if err != nil {
		return "", err
	}
	output, _ := json.Marshal(map[string]interface{}{
		"action":  "install",
		"skill":   info,
		"success": true,
	})
	return string(output), nil
}

func (self *skillsTool) executeUpdate(ctx context.Context, name string) (string, error) {
	updated, err := skills.Update(ctx, name)
	if err != nil {
		return "", err
	}
	output, _ := json.Marshal(map[string]interface{}{
		"action":  "update",
		"updated": updated,
		"count":   len(updated),
	})
	return string(output), nil
}

func (self *skillsTool) executeListInstalled(ctx context.Context) (string, error) {
	var installed []*models.Skill
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		var err error
		installed, err = tx.ListSkills(ctx, nil)
		return err
	}); err != nil {
		return "", err
	}
	type skillInfo struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Version     string `json:"version"`
		Enabled     bool   `json:"enabled"`
		Publisher   string `json:"publisher"`
	}
	infos := make([]skillInfo, 0, len(installed))
	for _, skill := range installed {
		infos = append(infos, skillInfo{
			Name:        skill.GetName(),
			Description: skill.GetDescription(),
			Version:     skill.GetVersion(),
			Enabled:     skill.GetEnabled(),
			Publisher:   skill.GetPublisher(),
		})
	}
	output, _ := json.Marshal(map[string]interface{}{
		"action": "list_installed",
		"skills": infos,
		"count":  len(infos),
	})
	return string(output), nil
}

func (self *skillsTool) executeUninstall(ctx context.Context, name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("name is required for uninstall")
	}
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		return tx.DeleteSkill(ctx, name, nil)
	}); err != nil {
		return "", err
	}
	output, _ := json.Marshal(map[string]interface{}{
		"action":  "uninstall",
		"name":    name,
		"success": true,
	})
	return string(output), nil
}

func (self *skillsTool) executeSetEnabled(ctx context.Context, name string, enabled bool) (string, error) {
	if name == "" {
		return "", fmt.Errorf("name is required for enable/disable")
	}
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		_, err := tx.ModifySkill(ctx, name, func(skill *models.Skill) error {
			skill.Enabled = ptrto.Value(enabled)
			return nil
		}, nil)
		return err
	}); err != nil {
		return "", err
	}
	action := "enable"
	if !enabled {
		action = "disable"
	}
	output, _ := json.Marshal(map[string]interface{}{
		"action":  action,
		"name":    name,
		"enabled": enabled,
		"success": true,
	})
	return string(output), nil
}
