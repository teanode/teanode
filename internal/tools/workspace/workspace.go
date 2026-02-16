package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/teanode/teanode/internal/agents"
	"github.com/teanode/teanode/internal/provider"
	"github.com/teanode/teanode/internal/util/atomicfile"
)

// RegisterTools adds memory tools to the registry.
func RegisterTools(registry *agents.ToolRegistry, memoryDirectory string) {
	registry.Register(&readTool{directory: memoryDirectory})
	registry.Register(&writeTool{directory: memoryDirectory})
	registry.Register(&listTool{directory: memoryDirectory})
	registry.Register(&appendTool{directory: memoryDirectory})
	registry.Register(&searchTool{directory: memoryDirectory})
	registry.Register(&deleteTool{directory: memoryDirectory})
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

// --- workspace_read ---

type readTool struct{ directory string }

func (self *readTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Type: "function",
		Function: provider.FunctionSpec{
			Name:        "workspace_read",
			Description: "Read a file from persistent memory storage.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Relative path of the file to read.",
					},
				},
				"required": []string{"path"},
			},
		},
	}
}

func (self *readTool) Execute(_ context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}
	full, err := safePath(self.directory, arguments.Path)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return "", fmt.Errorf("reading file: %w", err)
	}
	return string(data), nil
}

// --- workspace_write ---

type writeTool struct{ directory string }

func (self *writeTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Type: "function",
		Function: provider.FunctionSpec{
			Name:        "workspace_write",
			Description: "Write a file to persistent memory storage. Creates parent directories as needed.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Relative path of the file to write.",
					},
					"content": map[string]interface{}{
						"type":        "string",
						"description": "Content to write to the file.",
					},
				},
				"required": []string{"path", "content"},
			},
		},
	}
}

func (self *writeTool) Execute(_ context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}
	full, err := safePath(self.directory, arguments.Path)
	if err != nil {
		return "", err
	}
	if err := atomicfile.WriteFile(full, []byte(arguments.Content)); err != nil {
		return "", fmt.Errorf("writing file: %w", err)
	}
	return "ok", nil
}

// --- workspace_list ---

type listTool struct{ directory string }

func (self *listTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Type: "function",
		Function: provider.FunctionSpec{
			Name:        "workspace_list",
			Description: "List all files in persistent memory storage.",
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
	}
}

func (self *listTool) Execute(_ context.Context, _ string) (string, error) {
	var files []string
	err := filepath.Walk(self.directory, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			relative, _ := filepath.Rel(self.directory, path)
			files = append(files, relative)
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("listing files: %w", err)
	}
	if len(files) == 0 {
		return "no files", nil
	}
	return strings.Join(files, "\n"), nil
}

// --- workspace_append ---

type appendTool struct{ directory string }

func (self *appendTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Type: "function",
		Function: provider.FunctionSpec{
			Name:        "workspace_append",
			Description: "Append text to a file in persistent memory storage. Creates the file and parent directories if needed. Useful for daily logs.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Relative path of the file to append to.",
					},
					"content": map[string]interface{}{
						"type":        "string",
						"description": "Content to append to the file.",
					},
				},
				"required": []string{"path", "content"},
			},
		},
	}
}

func (self *appendTool) Execute(_ context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}
	full, err := safePath(self.directory, arguments.Path)
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
	if _, err := file.WriteString(arguments.Content + "\n"); err != nil {
		return "", fmt.Errorf("appending to file: %w", err)
	}
	return "ok", nil
}

// --- workspace_delete ---

type deleteTool struct{ directory string }

func (self *deleteTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Type: "function",
		Function: provider.FunctionSpec{
			Name:        "workspace_delete",
			Description: "Delete a file from persistent memory storage. If the parent directory becomes empty after deletion, it is removed automatically.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Relative path of the file to delete.",
					},
				},
				"required": []string{"path"},
			},
		},
	}
}

func (self *deleteTool) Execute(_ context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}
	full, err := safePath(self.directory, arguments.Path)
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
	if err := os.Remove(full); err != nil {
		return "", fmt.Errorf("deleting file: %w", err)
	}
	// Remove empty parent directories up to the workspace root.
	directory := filepath.Dir(full)
	for directory != self.directory {
		entries, err := os.ReadDir(directory)
		if err != nil || len(entries) > 0 {
			break
		}
		os.Remove(directory)
		directory = filepath.Dir(directory)
	}
	return "ok", nil
}

// --- workspace_search ---

type searchTool struct{ directory string }

func (self *searchTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Type: "function",
		Function: provider.FunctionSpec{
			Name:        "workspace_search",
			Description: "Search across all workspace files for a query string. Returns matching lines with file path and line number.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Text to search for (case-insensitive substring match).",
					},
					"max_results": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of matching lines to return. Default 10.",
					},
				},
				"required": []string{"query"},
			},
		},
	}
}

func (self *searchTool) Execute(_ context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Query      string `json:"query"`
		MaxResults int    `json:"max_results"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}
	if arguments.Query == "" {
		return "", fmt.Errorf("query is required")
	}
	if arguments.MaxResults <= 0 {
		arguments.MaxResults = 10
	}

	queryLower := strings.ToLower(arguments.Query)
	var matches []string

	err := filepath.Walk(self.directory, func(path string, info os.FileInfo, err error) error {
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
		relative, _ := filepath.Rel(self.directory, path)
		lines := strings.Split(string(data), "\n")
		for index, line := range lines {
			if len(matches) >= arguments.MaxResults {
				return filepath.SkipAll
			}
			if strings.Contains(strings.ToLower(line), queryLower) {
				matches = append(matches, fmt.Sprintf("%s:%d: %s", relative, index+1, line))
			}
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("searching files: %w", err)
	}

	if len(matches) == 0 {
		return "no matches", nil
	}
	return strings.Join(matches, "\n"), nil
}
