package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ziyan/teanode/internal/provider"
	"github.com/ziyan/teanode/internal/util/atomicfile"
)

// RegisterMemoryTools adds memory tools to the registry.
func RegisterMemoryTools(registry *ToolRegistry, memoryDirectory string) {
	registry.Register(&memoryReadTool{directory: memoryDirectory})
	registry.Register(&memoryWriteTool{directory: memoryDirectory})
	registry.Register(&memoryListTool{directory: memoryDirectory})
	registry.Register(&memoryAppendTool{directory: memoryDirectory})
	registry.Register(&memorySearchTool{directory: memoryDirectory})
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

// --- memory_read ---

type memoryReadTool struct{ directory string }

func (self *memoryReadTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Type: "function",
		Function: provider.FunctionSpec{
			Name:        "memory_read",
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

func (self *memoryReadTool) Execute(_ context.Context, rawArguments string) (string, error) {
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

// --- memory_write ---

type memoryWriteTool struct{ directory string }

func (self *memoryWriteTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Type: "function",
		Function: provider.FunctionSpec{
			Name:        "memory_write",
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

func (self *memoryWriteTool) Execute(_ context.Context, rawArguments string) (string, error) {
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

// --- memory_list ---

type memoryListTool struct{ directory string }

func (self *memoryListTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Type: "function",
		Function: provider.FunctionSpec{
			Name:        "memory_list",
			Description: "List all files in persistent memory storage.",
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
	}
}

func (self *memoryListTool) Execute(_ context.Context, _ string) (string, error) {
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

// --- memory_append ---

type memoryAppendTool struct{ directory string }

func (self *memoryAppendTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Type: "function",
		Function: provider.FunctionSpec{
			Name:        "memory_append",
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

func (self *memoryAppendTool) Execute(_ context.Context, rawArguments string) (string, error) {
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

// --- memory_search ---

type memorySearchTool struct{ directory string }

func (self *memorySearchTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Type: "function",
		Function: provider.FunctionSpec{
			Name:        "memory_search",
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

func (self *memorySearchTool) Execute(_ context.Context, rawArguments string) (string, error) {
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
