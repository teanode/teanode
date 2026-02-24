package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/teanode/teanode/internal/agents"
	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/util/atomicfile"
	"github.com/teanode/teanode/internal/util/trash"
)

// RegisterTools adds memory tools to the registry.
func RegisterTools(registry *agents.ToolRegistry, agentWorkspaceDirectory string) {
	registry.Register(newWorkspaceTool(
		"agent_workspace",
		"Persistent per-agent workspace storage shared by users of this agent.",
		func(context.Context) (string, error) { return agentWorkspaceDirectory, nil },
	))
	registry.Register(newWorkspaceTool(
		"user_workspace",
		"Persistent per-user workspace storage for user-specific memory and notes.",
		func(ctx context.Context) (string, error) {
			userId := agents.UserIDFromContext(ctx)
			if userId == "" {
				return "", fmt.Errorf("missing user context")
			}
			return configs.UserWorkspaceDirectory(userId), nil
		},
	))
}

// safePath resolves a relative path inside memoryDirectory and rejects traversal.
func safePath(memoryDirectory, rel string) (string, error) {
	cleaned := filepath.Clean(rel)
	if filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, "..") {
		return "", fmt.Errorf("invalid path: %s", rel)
	}
	full := filepath.Join(memoryDirectory, cleaned)
	if !strings.HasPrefix(full, memoryDirectory) {
		return "", fmt.Errorf("path traversal denied: %s", rel)
	}
	return full, nil
}

// --- workspace (consolidated) ---

type workspaceTool struct {
	name             string
	description      string
	resolveDirectory func(ctx context.Context) (string, error)
}

func newWorkspaceTool(name, description string, resolveDirectory func(ctx context.Context) (string, error)) *workspaceTool {
	return &workspaceTool{
		name:             name,
		description:      description,
		resolveDirectory: resolveDirectory,
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

	directory, err := self.resolveDirectory(ctx)
	if err != nil {
		return "", err
	}

	switch arguments.Action {
	case "read":
		return self.executeRead(directory, arguments.Path)
	case "write":
		return self.executeWrite(directory, arguments.Path, arguments.Content)
	case "list":
		return self.executeList(directory)
	case "append":
		return self.executeAppend(directory, arguments.Path, arguments.Content)
	case "search":
		return self.executeSearch(directory, arguments.Query, arguments.MaxResults)
	case "delete":
		return self.executeDelete(directory, arguments.Path)
	default:
		return "", fmt.Errorf("unknown workspace action: %s", arguments.Action)
	}
}

func (self *workspaceTool) executeRead(directory, path string) (string, error) {
	full, err := safePath(directory, path)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return "", fmt.Errorf("reading file: %w", err)
	}
	output, _ := json.Marshal(map[string]interface{}{
		"action":  "read",
		"content": string(data),
	})
	return string(output), nil
}

func (self *workspaceTool) executeWrite(directory, path string, content string) (string, error) {
	full, err := safePath(directory, path)
	if err != nil {
		return "", err
	}
	if err := atomicfile.WriteFile(full, []byte(content)); err != nil {
		return "", fmt.Errorf("writing file: %w", err)
	}
	output, _ := json.Marshal(map[string]interface{}{
		"action":  "write",
		"success": true,
	})
	return string(output), nil
}

func (self *workspaceTool) executeList(directory string) (string, error) {
	var files []string
	err := filepath.Walk(directory, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			relative, _ := filepath.Rel(directory, path)
			files = append(files, relative)
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("listing files: %w", err)
	}
	if files == nil {
		files = []string{}
	}
	output, _ := json.Marshal(map[string]interface{}{
		"action": "list",
		"files":  files,
	})
	return string(output), nil
}

func (self *workspaceTool) executeAppend(directory, path string, content string) (string, error) {
	full, err := safePath(directory, path)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		return "", fmt.Errorf("creating directories: %w", err)
	}
	file, err := os.OpenFile(full, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return "", fmt.Errorf("opening file: %w", err)
	}
	defer file.Close()
	if _, err := file.WriteString(content + "\n"); err != nil {
		return "", fmt.Errorf("appending to file: %w", err)
	}
	output, _ := json.Marshal(map[string]interface{}{
		"action":  "append",
		"success": true,
	})
	return string(output), nil
}

func (self *workspaceTool) executeSearch(directory, query string, maxResults int) (string, error) {
	if query == "" {
		return "", fmt.Errorf("query is required")
	}
	if maxResults <= 0 {
		maxResults = 10
	}

	queryLower := strings.ToLower(query)
	type matchEntry struct {
		Path string `json:"path"`
		Line int    `json:"line"`
		Text string `json:"text"`
	}
	var matches []matchEntry

	err := filepath.Walk(directory, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip errors
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".md") && !strings.HasSuffix(path, ".txt") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		relative, _ := filepath.Rel(directory, path)
		lines := strings.Split(string(data), "\n")
		for index, line := range lines {
			if len(matches) >= maxResults {
				return filepath.SkipAll
			}
			if strings.Contains(strings.ToLower(line), queryLower) {
				matches = append(matches, matchEntry{
					Path: relative,
					Line: index + 1,
					Text: line,
				})
			}
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("searching files: %w", err)
	}

	if matches == nil {
		matches = []matchEntry{}
	}
	output, _ := json.Marshal(map[string]interface{}{
		"action":  "search",
		"matches": matches,
	})
	return string(output), nil
}

func (self *workspaceTool) executeDelete(directory, path string) (string, error) {
	full, err := safePath(directory, path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(full)
	if err != nil {
		return "", fmt.Errorf("file not found: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("cannot delete directories, only files")
	}
	dataDirectory := configs.Directory()
	if isPathInsideDirectory(full, dataDirectory) {
		trashDirectory := configs.TrashDirectory()
		if err := trash.Move(full, trashDirectory); err != nil {
			return "", fmt.Errorf("deleting file: %w", err)
		}
	} else {
		if err := os.Remove(full); err != nil {
			return "", fmt.Errorf("deleting file: %w", err)
		}
	}
	// Remove empty parent directories up to the workspace root.
	currentDirectory := filepath.Dir(full)
	for currentDirectory != directory {
		entries, err := os.ReadDir(currentDirectory)
		if err != nil || len(entries) > 0 {
			break
		}
		os.Remove(currentDirectory)
		currentDirectory = filepath.Dir(currentDirectory)
	}
	output, _ := json.Marshal(map[string]interface{}{
		"action":  "delete",
		"success": true,
	})
	return string(output), nil
}

func isPathInsideDirectory(path, directory string) bool {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	absoluteDirectory, err := filepath.Abs(directory)
	if err != nil {
		return false
	}
	relativePath, err := filepath.Rel(absoluteDirectory, absolutePath)
	if err != nil {
		return false
	}
	return relativePath == "." || (!strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) && relativePath != "..")
}
