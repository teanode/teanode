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
		return []tools.Tool{NewProjectsTool(), NewProjectWorkspaceTool()}
	})
}

func NewProjectsTool() *projectsTool                 { return &projectsTool{} }
func NewProjectWorkspaceTool() *projectWorkspaceTool { return &projectWorkspaceTool{} }

type projectsTool struct{}
type projectWorkspaceTool struct{}

type projectsToolResponse struct {
	Action    string            `json:"action"`
	ProjectID string            `json:"projectId,omitempty"`
	Project   *models.Project   `json:"project,omitempty"`
	Projects  []*models.Project `json:"projects,omitempty"`
	Files     []string          `json:"files,omitempty"`
	Content   string            `json:"content,omitempty"`
	Matches   []searchMatch     `json:"matches,omitempty"`
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
		},
	}
}

func (self *projectWorkspaceTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name:        "project_workspace",
			Description: "Manage files in a shared project's workspace. Actions: list/read/write/append/search/delete/move files. Use PROJECT.md as the canonical main project document.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type": "string",
						"enum": []string{"list", "read", "write", "append", "search", "delete", "move"},
					},
					"projectId": map[string]interface{}{
						"type":        "string",
						"description": "Project ID for workspace operations.",
					},
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Relative file path inside the project.",
					},
					"fromPath": map[string]interface{}{
						"type":        "string",
						"description": "Source relative path for move.",
					},
					"toPath": map[string]interface{}{
						"type":        "string",
						"description": "Destination relative path for move.",
					},
					"content": map[string]interface{}{
						"type":        "string",
						"description": "Content for write/append.",
					},
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Search query for search action.",
					},
					"maxResults": map[string]interface{}{
						"type":        "integer",
						"description": "Max search results (default 10).",
					},
				},
				"required": []string{"action"},
			},
		},
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

	action := arguments.Action
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

func (self *projectWorkspaceTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Action     string `json:"action"`
		ProjectID  string `json:"projectId"`
		Path       string `json:"path"`
		FromPath   string `json:"fromPath"`
		ToPath     string `json:"toPath"`
		Content    string `json:"content"`
		Query      string `json:"query"`
		MaxResults int    `json:"maxResults"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}
	if arguments.ProjectID == "" {
		return "", fmt.Errorf("projectId is required")
	}

	action := arguments.Action
	switch action {
	case "list":
		files, err := listFiles(ctx, arguments.ProjectID)
		if err != nil {
			return "", err
		}
		output, _ := json.Marshal(projectsToolResponse{
			Action:    "list",
			ProjectID: arguments.ProjectID,
			Files:     files,
		})
		return string(output), nil
	case "read":
		content, err := readFile(ctx, arguments.ProjectID, arguments.Path)
		if err != nil {
			return "", err
		}
		output, _ := json.Marshal(projectsToolResponse{
			Action:    "read",
			ProjectID: arguments.ProjectID,
			Content:   content,
		})
		return string(output), nil
	case "write":
		if err := writeFile(ctx, arguments.ProjectID, arguments.Path, arguments.Content); err != nil {
			return "", err
		}
		output, _ := json.Marshal(projectsToolResponse{Action: "write", ProjectID: arguments.ProjectID, Success: true})
		return string(output), nil
	case "append":
		if err := appendFile(ctx, arguments.ProjectID, arguments.Path, arguments.Content); err != nil {
			return "", err
		}
		output, _ := json.Marshal(projectsToolResponse{Action: "append", ProjectID: arguments.ProjectID, Success: true})
		return string(output), nil
	case "search":
		matches, err := searchFiles(ctx, arguments.ProjectID, arguments.Query, arguments.MaxResults)
		if err != nil {
			return "", err
		}
		output, _ := json.Marshal(projectsToolResponse{
			Action:    "search",
			ProjectID: arguments.ProjectID,
			Matches:   matches,
		})
		return string(output), nil
	case "delete":
		if arguments.Path == "" {
			return "", fmt.Errorf("path is required")
		}
		if err := deleteFile(ctx, arguments.ProjectID, arguments.Path); err != nil {
			return "", err
		}
		output, _ := json.Marshal(projectsToolResponse{Action: "delete", ProjectID: arguments.ProjectID, Success: true})
		return string(output), nil
	case "move":
		if err := moveFile(ctx, arguments.ProjectID, arguments.FromPath, arguments.ToPath); err != nil {
			return "", err
		}
		output, _ := json.Marshal(projectsToolResponse{Action: "move", ProjectID: arguments.ProjectID, Success: true})
		return string(output), nil
	default:
		return "", fmt.Errorf("unknown project_workspace action: %s", arguments.Action)
	}
}
