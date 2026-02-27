package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/op/go-logging"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/runners"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/tools"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/valueor"
)

var log = logging.MustGetLogger("workspace")

func init() {
	tools.RegisterBuiltinTool(createTools)
}

func createTools() []tools.Tool {
	return []tools.Tool{
		newWorkspaceTool(workspaceToolConfig{
			name:        "agent_workspace",
			description: "Persistent per-agent workspace storage shared by users of this agent.",
			resolveScope: func(ctx context.Context, _ string) (models.Scope, string, error) {
				runner := runners.RunnerFromContext(ctx)
				if runner == nil || runner.AgentID == "" {
					return "", "", fmt.Errorf("missing runner context")
				}
				return models.ScopeAgent, runner.AgentID, nil
			},
			afterMutate: func(ctx context.Context, scopeId string) error {
				return store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
					_, modifyError := transaction.ModifyAgent(ctx, scopeId, func(agent *models.Agent) error {
						now := time.Now()
						agent.ModifiedAt = &now
						return nil
					}, nil)
					return modifyError
				})
			},
		}),
		newWorkspaceTool(workspaceToolConfig{
			name:        "user_workspace",
			description: "Persistent per-user workspace storage for user-specific memory and notes.",
			resolveScope: func(ctx context.Context, _ string) (models.Scope, string, error) {
				user := models.UserFromContext(ctx)
				if user == nil || user.ID == "" {
					return "", "", fmt.Errorf("missing user context")
				}
				return models.ScopeUser, user.ID, nil
			},
			afterMutate: func(ctx context.Context, scopeId string) error {
				return store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
					_, modifyError := transaction.ModifyUser(ctx, scopeId, func(user *models.User) error {
						now := time.Now()
						user.ModifiedAt = &now
						return nil
					}, nil)
					return modifyError
				})
			},
		}),
		newWorkspaceTool(workspaceToolConfig{
			name:                        "project_workspace",
			description:                 "Manage files in a shared project's workspace. Use PROJECT.md as the canonical main project document.",
			scopeIdParameterName:        "projectId",
			scopeIdParameterDescription: "Project ID for project's workspace operations.",
			resolveScope: func(_ context.Context, scopeId string) (models.Scope, string, error) {
				if scopeId == "" {
					return "", "", fmt.Errorf("projectId is required")
				}
				return models.ScopeProject, scopeId, nil
			},
			afterMutate: func(ctx context.Context, scopeId string) error {
				return store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
					_, modifyError := transaction.ModifyProject(ctx, scopeId, func(project *models.Project) error {
						now := time.Now()
						project.ModifiedAt = &now
						return nil
					}, nil)
					return modifyError
				})
			},
		}),
	}
}

// --- workspace (consolidated) ---

type workspaceToolConfig struct {
	name                        string
	description                 string
	scopeIdParameterName        string // if non-empty, add as a required parameter and read from arguments
	scopeIdParameterDescription string
	resolveScope                func(ctx context.Context, scopeId string) (models.Scope, string, error)
	afterMutate                 func(ctx context.Context, scopeId string) error // called after write/append/delete/move
}

type workspaceTool struct {
	config workspaceToolConfig
}

func newWorkspaceTool(config workspaceToolConfig) *workspaceTool {
	return &workspaceTool{config: config}
}

func (self *workspaceTool) Definition() providers.ToolDefinition {
	actions := []string{"read", "write", "list", "append", "search", "delete", "move"}
	properties := map[string]interface{}{
		"action": map[string]interface{}{
			"type":        "string",
			"enum":        actions,
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
	}

	required := []string{"action"}

	if self.config.scopeIdParameterName != "" {
		properties[self.config.scopeIdParameterName] = map[string]interface{}{
			"type":        "string",
			"description": self.config.scopeIdParameterDescription,
		}
		required = append(required, self.config.scopeIdParameterName)
	}

	properties["fromPath"] = map[string]interface{}{
		"type":        "string",
		"description": "Source relative path for move.",
	}
	properties["toPath"] = map[string]interface{}{
		"type":        "string",
		"description": "Destination relative path for move.",
	}

	descriptionSuffix := " Actions: read (read a file), write (create/overwrite a file), " +
		"list (list all files), append (append to a file), search (search across files), delete (delete a file), move (move a file)."

	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name:        self.config.name,
			Description: self.config.description + descriptionSuffix,
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": properties,
				"required":   required,
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
		FromPath   string `json:"fromPath"`
		ToPath     string `json:"toPath"`
		Content    string `json:"content"`
		Query      string `json:"query"`
		MaxResults int    `json:"maxResults"`
		// Generic scope ID field — read dynamically below.
		ScopeID string `json:"-"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}

	// Extract scope ID from the named parameter if configured.
	if self.config.scopeIdParameterName != "" {
		var rawMap map[string]json.RawMessage
		if err := json.Unmarshal([]byte(rawArguments), &rawMap); err == nil {
			if raw, ok := rawMap[self.config.scopeIdParameterName]; ok {
				var scopeId string
				if err := json.Unmarshal(raw, &scopeId); err == nil {
					arguments.ScopeID = scopeId
				}
			}
		}
	}

	scope, scopeId, scopeError := self.config.resolveScope(ctx, arguments.ScopeID)
	if scopeError != nil {
		return "", scopeError
	}

	switch arguments.Action {
	case "read":
		return self.executeRead(ctx, scope, scopeId, arguments.Path)
	case "write":
		result, err := self.executeWrite(ctx, scope, scopeId, arguments.Path, arguments.Content)
		if err != nil {
			return "", err
		}
		self.callAfterMutate(ctx, scopeId)
		return result, nil
	case "list":
		return self.executeList(ctx, scope, scopeId)
	case "append":
		result, err := self.executeAppend(ctx, scope, scopeId, arguments.Path, arguments.Content)
		if err != nil {
			return "", err
		}
		self.callAfterMutate(ctx, scopeId)
		return result, nil
	case "search":
		return self.executeSearch(ctx, scope, scopeId, arguments.Query, arguments.MaxResults)
	case "delete":
		result, err := self.executeDelete(ctx, scope, scopeId, arguments.Path)
		if err != nil {
			return "", err
		}
		self.callAfterMutate(ctx, scopeId)
		return result, nil
	case "move":
		result, err := self.executeMove(ctx, scope, scopeId, arguments.FromPath, arguments.ToPath)
		if err != nil {
			return "", err
		}
		self.callAfterMutate(ctx, scopeId)
		return result, nil
	default:
		return "", fmt.Errorf("unknown workspace action: %s", arguments.Action)
	}
}

func (self *workspaceTool) callAfterMutate(ctx context.Context, scopeId string) {
	if self.config.afterMutate != nil {
		if err := self.config.afterMutate(ctx, scopeId); err != nil {
			log.Warningf("failed to call after mutate: %v", err)
		}
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
			if path == "" || !isSearchableFile(path) {
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

func (self *workspaceTool) executeMove(
	ctx context.Context,
	scope models.Scope,
	scopeId string,
	fromPath string,
	toPath string,
) (string, error) {
	normalizedFromPath, fromError := normalizeRelativePath(fromPath)
	if fromError != nil {
		return "", fromError
	}
	normalizedToPath, toError := normalizeRelativePath(toPath)
	if toError != nil {
		return "", toError
	}
	if normalizedFromPath == normalizedToPath {
		output, _ := json.Marshal(map[string]interface{}{"action": "move", "success": true})
		return string(output), nil
	}

	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		sourceFile, getError := transaction.GetWorkspaceFileByPath(ctx, scope, scopeId, normalizedFromPath, nil)
		if getError != nil {
			return getError
		}
		contentBytes := []byte{}
		if sourceFile.Content != nil {
			contentBytes = append(contentBytes, (*sourceFile.Content)...)
		}
		_, createError := transaction.CreateWorkspaceFile(ctx, &models.WorkspaceFile{
			Scope:   ptrto.Value(scope),
			ScopeID: ptrto.Value(scopeId),
			Path:    &normalizedToPath,
			Content: &contentBytes,
		}, nil)
		if createError != nil {
			if createError != store.ErrAlreadyExists {
				return createError
			}
			_, modifyError := transaction.ModifyWorkspaceFileByPath(ctx, scope, scopeId, normalizedToPath, func(existingFile *models.WorkspaceFile) error {
				existingFile.Content = &contentBytes
				return nil
			}, nil)
			if modifyError != nil {
				return modifyError
			}
		}
		return transaction.DeleteWorkspaceFileByPath(ctx, scope, scopeId, normalizedFromPath, nil)
	}); err != nil {
		return "", fmt.Errorf("moving file: %w", err)
	}
	output, _ := json.Marshal(map[string]interface{}{
		"action":  "move",
		"success": true,
	})
	return string(output), nil
}

// isSearchableFile returns true if the file extension indicates a text-based
// file that can be meaningfully searched for content matches.
func isSearchableFile(path string) bool {
	extension := strings.ToLower(filepath.Ext(path))
	switch extension {
	case ".md", ".txt", ".json", ".yaml", ".yml", ".toml", ".xml", ".csv", ".log", ".ini", ".cfg", ".conf", ".env", ".sh", ".bat", ".ps1":
		return true
	default:
		return false
	}
}

func normalizeRelativePath(path string) (string, error) {
	cleanedPath := filepath.Clean(path)
	if cleanedPath == "." || cleanedPath == "" || filepath.IsAbs(cleanedPath) || cleanedPath == ".." || strings.HasPrefix(cleanedPath, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid path: %s", path)
	}
	return cleanedPath, nil
}
