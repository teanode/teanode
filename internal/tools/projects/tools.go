package projects

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/tools"
)

func init() {
	tools.RegisterBuiltinTool(func() []tools.Tool {
		return []tools.Tool{NewProjectsTool()}
	})
}

func NewProjectsTool() *projectsTool { return &projectsTool{} }

type projectsTool struct{}

type projectsToolResponse struct {
	Action    string            `json:"action"`
	ProjectID string            `json:"projectId,omitempty"`
	Project   *models.Project   `json:"project,omitempty"`
	Projects  []*models.Project `json:"projects,omitempty"`
	Success   bool              `json:"success,omitempty"`
}

func (self *projectsTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name:        "projects",
			Description: "Manage shared projects. Actions: create/list/info/rename/delete projects.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type": "string",
						"enum": []string{"list", "info", "create", "rename", "delete"},
					},
					"projectId": map[string]interface{}{
						"type":        "string",
						"description": "Project ID for project-specific actions.",
					},
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Project name for create or rename.",
					},
					"description": map[string]interface{}{
						"type":        "string",
						"description": "Project description for create.",
					},
					"purpose": map[string]interface{}{
						"type":        "string",
						"description": "Purpose/context for create and PROJECT.md initialization.",
					},
				},
				"required": []string{"action"},
			},
			Returns: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action":    map[string]interface{}{"type": "string"},
					"projectId": map[string]interface{}{"type": "string"},
					"project":   map[string]interface{}{"type": "object"},
					"projects":  map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "object"}},
					"success":   map[string]interface{}{"type": "boolean"},
				},
			},
		},
	}
}

func (self *projectsTool) PolicyGroups() []tools.PolicyGroup {
	return []tools.PolicyGroup{
		{Group: models.ToolPolicyGroupRead, Default: models.ToolPolicyAnyone, Actions: []string{"list", "info"}},
		{Group: models.ToolPolicyGroupWrite, Default: models.ToolPolicyAdminOnly},
	}
}

func (self *projectsTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Action      string `json:"action"`
		ProjectID   string `json:"projectId"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Purpose     string `json:"purpose"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}

	// Mutating project actions require admin access.
	action := arguments.Action
	switch action {
	case "create", "rename", "delete":
		user := models.UserFromContext(ctx)
		if user == nil || !user.GetAdmin() {
			return "", fmt.Errorf("admin access required to %s projects", action)
		}
	}

	switch action {
	case "list":
		items, err := listProjects(ctx)
		if err != nil {
			return "", err
		}
		output, _ := json.Marshal(projectsToolResponse{Action: "list", Projects: items})
		return string(output), nil
	case "info":
		if arguments.ProjectID == "" {
			return "", fmt.Errorf("projectId is required")
		}
		item, err := getProject(ctx, arguments.ProjectID)
		if err != nil {
			return "", err
		}
		output, _ := json.Marshal(projectsToolResponse{Action: "info", Project: item})
		return string(output), nil
	case "create":
		name := arguments.Name
		if name == "" {
			return "", fmt.Errorf("name is required")
		}
		description := arguments.Description
		if description == "" {
			return "", fmt.Errorf("description is required for create")
		}
		item, err := createProject(ctx, name, description, arguments.Purpose)
		if err != nil {
			return "", err
		}
		output, _ := json.Marshal(projectsToolResponse{Action: "create", Project: item})
		return string(output), nil
	case "rename":
		if arguments.ProjectID == "" {
			return "", fmt.Errorf("projectId is required")
		}
		item, err := renameProject(ctx, arguments.ProjectID, arguments.Name)
		if err != nil {
			return "", err
		}
		output, _ := json.Marshal(projectsToolResponse{Action: "rename", Project: item})
		return string(output), nil
	case "delete":
		if arguments.ProjectID == "" {
			return "", fmt.Errorf("projectId is required")
		}
		if err := deleteProject(ctx, arguments.ProjectID); err != nil {
			return "", err
		}
		output, _ := json.Marshal(projectsToolResponse{Action: "delete", Success: true})
		return string(output), nil
	default:
		return "", fmt.Errorf("unknown projects action: %s", arguments.Action)
	}
}
