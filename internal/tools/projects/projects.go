package projects

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/teanode/teanode/internal/agents"
	"github.com/teanode/teanode/internal/configs"
	projectstore "github.com/teanode/teanode/internal/projects"
	"github.com/teanode/teanode/internal/providers"
)

func RegisterTools(registry *agents.ToolRegistry) {
	registry.Register(&projectsTool{})
	registry.Register(&projectWorkspaceTool{})
}

type projectsTool struct{}
type projectWorkspaceTool struct{}

type projectsToolResponse struct {
	Action    string                     `json:"action"`
	ProjectID string                     `json:"projectId,omitempty"`
	Project   *projectstore.Metadata     `json:"project,omitempty"`
	Projects  []projectstore.Metadata    `json:"projects,omitempty"`
	Workspace string                     `json:"workspace,omitempty"`
	Files     []string                   `json:"files,omitempty"`
	Content   string                     `json:"content,omitempty"`
	Matches   []projectstore.SearchMatch `json:"matches,omitempty"`
	Success   bool                       `json:"success,omitempty"`
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
						"description": "Project description (optional for create; generated when omitted).",
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

	action := strings.TrimSpace(arguments.Action)
	switch action {
	case "list":
		items, err := projectstore.List()
		if err != nil {
			return "", err
		}
		output, _ := json.Marshal(projectsToolResponse{Action: "list", Projects: items})
		return string(output), nil
	case "info":
		if strings.TrimSpace(arguments.ProjectID) == "" {
			return "", fmt.Errorf("projectId is required")
		}
		item, err := projectstore.Get(arguments.ProjectID)
		if err != nil {
			return "", err
		}
		output, _ := json.Marshal(projectsToolResponse{Action: "info", Project: item})
		return string(output), nil
	case "create":
		name := strings.TrimSpace(arguments.Name)
		if name == "" {
			return "", fmt.Errorf("name is required")
		}
		description := strings.TrimSpace(arguments.Description)
		if description == "" {
			description = strings.TrimSpace(generateDescription(ctx, name, arguments.Purpose))
		}
		item, err := projectstore.Create(name, description, arguments.Purpose)
		if err != nil {
			return "", err
		}
		workspacePath, _ := projectstore.WorkspaceDirectory(item.ID)
		output, _ := json.Marshal(projectsToolResponse{Action: "create", Project: item, Workspace: workspacePath})
		return string(output), nil
	case "rename":
		if strings.TrimSpace(arguments.ProjectID) == "" {
			return "", fmt.Errorf("projectId is required")
		}
		item, err := projectstore.Rename(arguments.ProjectID, arguments.Name)
		if err != nil {
			return "", err
		}
		output, _ := json.Marshal(projectsToolResponse{Action: "rename", Project: item})
		return string(output), nil
	case "delete":
		if strings.TrimSpace(arguments.ProjectID) == "" {
			return "", fmt.Errorf("projectId is required")
		}
		if err := projectstore.Delete(arguments.ProjectID); err != nil {
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
	if strings.TrimSpace(arguments.ProjectID) == "" {
		return "", fmt.Errorf("projectId is required")
	}

	action := strings.TrimSpace(arguments.Action)
	switch action {
	case "list":
		files, err := projectstore.ListFiles(arguments.ProjectID)
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
		content, err := projectstore.ReadFile(arguments.ProjectID, arguments.Path)
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
		if err := projectstore.WriteFile(arguments.ProjectID, arguments.Path, arguments.Content); err != nil {
			return "", err
		}
		output, _ := json.Marshal(projectsToolResponse{Action: "write", ProjectID: arguments.ProjectID, Success: true})
		return string(output), nil
	case "append":
		if err := projectstore.AppendFile(arguments.ProjectID, arguments.Path, arguments.Content); err != nil {
			return "", err
		}
		output, _ := json.Marshal(projectsToolResponse{Action: "append", ProjectID: arguments.ProjectID, Success: true})
		return string(output), nil
	case "search":
		matches, err := projectstore.SearchFiles(arguments.ProjectID, arguments.Query, arguments.MaxResults)
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
		if strings.TrimSpace(arguments.Path) == "" {
			return "", fmt.Errorf("path is required")
		}
		if err := projectstore.DeleteFile(arguments.ProjectID, arguments.Path); err != nil {
			return "", err
		}
		output, _ := json.Marshal(projectsToolResponse{Action: "delete", ProjectID: arguments.ProjectID, Success: true})
		return string(output), nil
	case "move":
		if err := projectstore.MoveFile(arguments.ProjectID, arguments.FromPath, arguments.ToPath); err != nil {
			return "", err
		}
		output, _ := json.Marshal(projectsToolResponse{Action: "move", ProjectID: arguments.ProjectID, Success: true})
		return string(output), nil
	default:
		return "", fmt.Errorf("unknown project_workspace action: %s", arguments.Action)
	}
}

func generateDescription(ctx context.Context, name, purpose string) string {
	runner := agents.RunnerFromContext(ctx)
	if runner == nil {
		return ""
	}
	userId := agents.UserIDFromContext(ctx)
	configuration, providerRegistry, _, workspaceDirectory, skillPrompts := runner.Snapshot()

	qualifiedModel := configuration.AgentModel(runner.AgentID)
	if qualifiedModel == "" {
		return ""
	}
	provider, bareModel, err := providerRegistry.Resolve(qualifiedModel)
	if err != nil {
		return ""
	}

	limits := configuration.ResolveModelLimits(qualifiedModel)
	userWorkspaceDirectory := ""
	if strings.TrimSpace(userId) != "" {
		if resolvedUserWorkspaceDirectory, resolveErr := configs.UserWorkspaceDirectory(userId); resolveErr == nil {
			userWorkspaceDirectory = resolvedUserWorkspaceDirectory
		}
	}
	systemPrompt := agents.BuildSystemPrompt(
		configuration,
		runner.AgentID,
		userId,
		workspaceDirectory,
		userWorkspaceDirectory,
		skillPrompts,
		limits.MaxWorkspaceFileChars,
		nil,
	)

	request := providers.ChatRequest{
		Model: bareModel,
		Messages: []providers.ChatMessage{
			{Role: "system", Content: systemPrompt},
			{
				Role: "user",
				Content: "Write a 1-2 sentence project description for task routing. " +
					"Include what this project is for and what work belongs here. " +
					"Use plain text only. " +
					"Project name: " + name + ". Purpose: " + strings.TrimSpace(purpose),
			},
		},
	}
	response, err := provider.ChatCompletion(ctx, request)
	if err != nil || len(response.Choices) == 0 {
		return ""
	}
	description := strings.TrimSpace(response.Choices[0].Message.ContentText())
	return description
}
