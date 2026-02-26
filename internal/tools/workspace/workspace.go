package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/runners"
	"github.com/teanode/teanode/internal/store"
	toolregistry "github.com/teanode/teanode/internal/tools"
	"github.com/teanode/teanode/internal/util/valueor"
)

// RegisterTools adds memory tools to the registry.
func RegisterTools(registry *toolregistry.ToolRegistry) {
	registry.Register(newWorkspaceTool(
		"agent_workspace",
		"Persistent per-agent workspace storage shared by users of this agent.",
		func(ctx context.Context) (models.Scope, string, error) {
			runner := runners.RunnerFromContext(ctx)
			if runner == nil || runner.AgentID == "" {
				return "", "", fmt.Errorf("missing runner context")
			}
			return models.ScopeAgent, runner.AgentID, nil
		},
	))
	registry.Register(newWorkspaceTool(
		"user_workspace",
		"Persistent per-user workspace storage for user-specific memory and notes.",
		func(ctx context.Context) (models.Scope, string, error) {
			user := models.UserFromContext(ctx)
			if user == nil || user.ID == "" {
				return "", "", fmt.Errorf("missing user context")
			}
			return models.ScopeUser, user.ID, nil
		},
	))
}

// --- workspace (consolidated) ---

type workspaceTool struct {
	name         string
	description  string
	resolveScope func(ctx context.Context) (models.Scope, string, error)
}

func newWorkspaceTool(
	name string,
	description string,
	resolveScope func(ctx context.Context) (models.Scope, string, error),
) *workspaceTool {
	return &workspaceTool{
		name:         name,
		description:  description,
		resolveScope: resolveScope,
	}
}

func (self *workspaceTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name: self.name,
			Description: self.description + " Actions: read (read a file), write (create/overwrite a file), " +
				"list (list all files), append (append to a file), search (search across files), delete (delete a file).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"read", "write", "list", "append", "search", "delete"},
						"description": "The workspace action to perform.",
					},
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Relative path of the file (for read, write, append, delete actions).",
					},
					"content": map[string]interface{}{
						"type":        "string",
						"description": "Content to write or append (for write, append actions).",
					},
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Text to search for, case-insensitive substring match (for search action).",
					},
					"maxResults": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of matching lines to return, default 10 (for search action).",
					},
				},
				"required": []string{"action"},
			},
			Returns: map[string]interface{}{
				"type":        "object",
				"description": "Action-dependent result. read: {action, content}. write: {action, success}. list: {action, files}. append: {action, success}. search: {action, matches}. delete: {action, success}.",
				"properties": map[string]interface{}{
					"action":  map[string]interface{}{"type": "string", "description": "The action that was performed"},
					"success": map[string]interface{}{"type": "boolean", "description": "Whether the action succeeded (write, append, delete)"},
					"content": map[string]interface{}{"type": "string", "description": "File content (read)"},
					"files": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "List of file paths (list)",
					},
					"matches": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"path": map[string]interface{}{"type": "string"},
								"line": map[string]interface{}{"type": "integer"},
								"text": map[string]interface{}{"type": "string"},
							},
						},
						"description": "Search matches (search)",
					},
				},
			},
		},
	}
}

func (self *workspaceTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Action     string `json:"action"`
		Path       string `json:"path"`
		Content    string `json:"content"`
		Query      string `json:"query"`
		MaxResults int    `json:"maxResults"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}
	scope, scopeId, scopeError := self.resolveScope(ctx)
	if scopeError != nil {
		return "", scopeError
	}
	switch arguments.Action {
	case "read":
		return self.executeRead(ctx, scope, scopeId, arguments.Path)
	case "write":
		return self.executeWrite(ctx, scope, scopeId, arguments.Path, arguments.Content)
	case "list":
		return self.executeList(ctx, scope, scopeId)
	case "append":
		return self.executeAppend(ctx, scope, scopeId, arguments.Path, arguments.Content)
	case "search":
		return self.executeSearch(ctx, scope, scopeId, arguments.Query, arguments.MaxResults)
	case "delete":
		return self.executeDelete(ctx, scope, scopeId, arguments.Path)
	default:
		return "", fmt.Errorf("unknown workspace action: %s", arguments.Action)
	}
}

func (self *workspaceTool) executeRead(
	ctx context.Context,
	scope models.Scope,
	scopeId string,
	path string,
) (string, error) {
	normalizedPath, err := normalizeRelativePath(path)
	if err != nil {
		return "", err
	}
	content := ""
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		file, err := transaction.GetWorkspaceFileByPath(ctx, scope, scopeId, normalizedPath, nil)
		if err != nil {
			return err
		}
		if file.Content != nil {
			content = string(*file.Content)
		}
		return nil
	}); err != nil {
		return "", fmt.Errorf("reading file: %w", err)
	}
	output, _ := json.Marshal(map[string]interface{}{
		"action":  "read",
		"content": content,
	})
	return string(output), nil
}

func (self *workspaceTool) executeWrite(
	ctx context.Context,
	scope models.Scope,
	scopeId string,
	path string,
	content string,
) (string, error) {
	normalizedPath, err := normalizeRelativePath(path)
	if err != nil {
		return "", err
	}
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		contentBytes := []byte(content)
		_, modifyError := transaction.ModifyWorkspaceFileByPath(ctx, scope, scopeId, normalizedPath, func(file *models.WorkspaceFile) error {
			file.Content = &contentBytes
			return nil
		}, nil)
		if modifyError == nil {
			return nil
		}
		if modifyError != store.ErrNotFound {
			return modifyError
		}
		_, createError := transaction.CreateWorkspaceFile(ctx, &models.WorkspaceFile{
			Scope:   &scope,
			ScopeID: &scopeId,
			Path:    &normalizedPath,
			Content: &contentBytes,
		}, nil)
		return createError
	}); err != nil {
		return "", fmt.Errorf("writing file: %w", err)
	}
	output, _ := json.Marshal(map[string]interface{}{
		"action":  "write",
		"success": true,
	})
	return string(output), nil
}

func (self *workspaceTool) executeList(
	ctx context.Context,
	scope models.Scope,
	scopeId string,
) (string, error) {
	files := []string{}
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		workspaceFiles, err := transaction.ListWorkspaceFilesByPath(ctx, scope, scopeId, "", nil)
		if err != nil {
			return err
		}
		files = make([]string, 0, len(workspaceFiles))
		for _, file := range workspaceFiles {
			path := file.GetPath()
			if path != "" {
				files = append(files, path)
			}
		}
		return nil
	}); err != nil {
		return "", fmt.Errorf("listing files: %w", err)
	}
	output, _ := json.Marshal(map[string]interface{}{
		"action": "list",
		"files":  files,
	})
	return string(output), nil
}

func (self *workspaceTool) executeAppend(
	ctx context.Context,
	scope models.Scope,
	scopeId string,
	path string,
	content string,
) (string, error) {
	normalizedPath, err := normalizeRelativePath(path)
	if err != nil {
		return "", err
	}
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		existingContent := ""
		file, getError := transaction.GetWorkspaceFileByPath(ctx, scope, scopeId, normalizedPath, nil)
		if getError == nil && file.Content != nil {
			existingContent = string(*file.Content)
		}
		nextContent := existingContent + content + "\n"
		contentBytes := []byte(nextContent)
		if getError == nil {
			_, modifyError := transaction.ModifyWorkspaceFileByPath(ctx, scope, scopeId, normalizedPath, func(file *models.WorkspaceFile) error {
				file.Content = &contentBytes
				return nil
			}, nil)
			return modifyError
		}
		if getError != store.ErrNotFound {
			return getError
		}
		_, createError := transaction.CreateWorkspaceFile(ctx, &models.WorkspaceFile{
			Scope:   &scope,
			ScopeID: &scopeId,
			Path:    &normalizedPath,
			Content: &contentBytes,
		}, nil)
		return createError
	}); err != nil {
		return "", fmt.Errorf("appending to file: %w", err)
	}
	output, _ := json.Marshal(map[string]interface{}{
		"action":  "append",
		"success": true,
	})
	return string(output), nil
}

func (self *workspaceTool) executeSearch(
	ctx context.Context,
	scope models.Scope,
	scopeId string,
	query string,
	maxResults int,
) (string, error) {
	if query == "" {
		return "", fmt.Errorf("query is required")
	}
	if maxResults <= 0 {
		maxResults = 10
	}
	type matchEntry struct {
		Path string `json:"path"`
		Line int    `json:"line"`
		Text string `json:"text"`
	}
	matches := []matchEntry{}
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		limit := uint64(maxResults)
		includeContent := true
		results, err := transaction.SearchWorkspaceFiles(ctx, scope, scopeId, query, store.WorkspaceSearchOptions{
			Limit:          &limit,
			IncludeContent: &includeContent,
		}, nil)
		if err != nil {
			return err
		}
		for _, result := range results {
			path := valueor.Zero(result.Path)
			if path == "" || (!strings.HasSuffix(path, ".md") && !strings.HasSuffix(path, ".txt")) {
				continue
			}
			lines := valueor.Zero(result.MatchedLines)
			for index, line := range lines {
				matches = append(matches, matchEntry{Path: path, Line: index + 1, Text: line})
				if len(matches) >= maxResults {
					return nil
				}
			}
		}
		return nil
	}); err != nil {
		return "", fmt.Errorf("searching files: %w", err)
	}
	output, _ := json.Marshal(map[string]interface{}{
		"action":  "search",
		"matches": matches,
	})
	return string(output), nil
}

func (self *workspaceTool) executeDelete(
	ctx context.Context,
	scope models.Scope,
	scopeId string,
	path string,
) (string, error) {
	normalizedPath, err := normalizeRelativePath(path)
	if err != nil {
		return "", err
	}
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		return transaction.DeleteWorkspaceFileByPath(ctx, scope, scopeId, normalizedPath, nil)
	}); err != nil {
		return "", fmt.Errorf("deleting file: %w", err)
	}
	output, _ := json.Marshal(map[string]interface{}{
		"action":  "delete",
		"success": true,
	})
	return string(output), nil
}

func normalizeRelativePath(path string) (string, error) {
	cleanedPath := filepath.Clean(path)
	if cleanedPath == "." || cleanedPath == "" || filepath.IsAbs(cleanedPath) || cleanedPath == ".." || strings.HasPrefix(cleanedPath, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid path: %s", path)
	}
	return cleanedPath, nil
}
