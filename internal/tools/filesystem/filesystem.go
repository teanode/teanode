// Package filesystem exposes tools for filesystem operations.
package filesystem

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/tools"
	"github.com/teanode/teanode/internal/util/atomicfile"
)

const (
	maxReadBytes        = 512 * 1024 // 512 KB
	maxDirectoryEntries = 1000
)

func init() {
	tools.RegisterBuiltinTool(func() []tools.Tool {
		return []tools.Tool{&filesystemTool{}}
	})
}

type filesystemTool struct{}

func (self *filesystemTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name: "filesystem",
			Description: "Interact with the local filesystem. Actions: read (read file contents), write (write file contents), " +
				"list (list directory entries), info (get file/directory metadata), mkdir (create directory), " +
				"delete (delete file or directory), move (move/rename file or directory), " +
				"search (find files by glob pattern).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"read", "write", "list", "info", "mkdir", "delete", "move", "search"},
						"description": "The filesystem action to perform.",
					},
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Absolute path to the file or directory.",
					},
					"content": map[string]interface{}{
						"type":        "string",
						"description": "Content to write (for write action).",
					},
					"destination": map[string]interface{}{
						"type":        "string",
						"description": "Destination path (for move action).",
					},
					"pattern": map[string]interface{}{
						"type":        "string",
						"description": "Glob pattern to match file names (for search action).",
					},
					"offset": map[string]interface{}{
						"type":        "integer",
						"description": "Byte offset to start reading from (for read action, default 0).",
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum bytes to read (for read action, default 524288).",
					},
					"recursive": map[string]interface{}{
						"type":        "boolean",
						"description": "Create parent directories (for mkdir) or delete recursively (for delete). Default false.",
					},
				},
				"required": []string{"action", "path"},
			},
			Returns: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"description": "The action that was performed.",
					},
					"content": map[string]interface{}{
						"type":        "string",
						"description": "File content (for read action).",
					},
					"size": map[string]interface{}{
						"type":        "integer",
						"description": "File size in bytes.",
					},
					"truncated": map[string]interface{}{
						"type":        "boolean",
						"description": "Whether the output was truncated.",
					},
					"entries": map[string]interface{}{
						"type":        "array",
						"description": "Directory entries (for list action).",
					},
					"success": map[string]interface{}{
						"type":        "boolean",
						"description": "Whether the action succeeded.",
					},
					"isDirectory": map[string]interface{}{
						"type":        "boolean",
						"description": "Whether the path is a directory (for info action).",
					},
					"permissions": map[string]interface{}{
						"type":        "string",
						"description": "File permissions (for info action).",
					},
					"modifiedAt": map[string]interface{}{
						"type":        "string",
						"description": "Last modification time in RFC3339 format.",
					},
				},
			},
		},
	}
}

func (self *filesystemTool) PolicyGroups() []tools.PolicyGroup {
	return []tools.PolicyGroup{
		{Group: models.ToolPolicyGroupRead, Default: models.ToolPolicyAdminOnly, Actions: []string{"read", "list", "info", "search"}},
		{Group: models.ToolPolicyGroupWrite, Default: models.ToolPolicyAdminApproval},
	}
}

func (self *filesystemTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Action      string `json:"action"`
		Path        string `json:"path"`
		Content     string `json:"content"`
		Destination string `json:"destination"`
		Pattern     string `json:"pattern"`
		Offset      int64  `json:"offset"`
		Limit       int64  `json:"limit"`
		Recursive   bool   `json:"recursive"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("filesystem: parsing arguments: %w", err)
	}
	if arguments.Path == "" {
		return "", fmt.Errorf("filesystem: path is required")
	}

	switch arguments.Action {
	case "read":
		return executeRead(arguments.Path, arguments.Offset, arguments.Limit)
	case "write":
		return executeWrite(arguments.Path, arguments.Content)
	case "list":
		return executeList(arguments.Path)
	case "info":
		return executeInfo(arguments.Path)
	case "mkdir":
		return executeMkdir(arguments.Path, arguments.Recursive)
	case "delete":
		return self.executeDelete(arguments.Path, arguments.Recursive)
	case "move":
		return executeMove(arguments.Path, arguments.Destination)
	case "search":
		return executeSearch(arguments.Path, arguments.Pattern)
	default:
		return "", fmt.Errorf("filesystem: unknown filesystem action: %s", arguments.Action)
	}
}

func executeRead(path string, offset, limit int64) (string, error) {
	if limit <= 0 {
		limit = maxReadBytes
	}
	if limit > maxReadBytes {
		limit = maxReadBytes
	}

	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("filesystem: opening file: %w", err)
	}
	defer func() { _ = file.Close() }()

	fileInformation, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("filesystem: getting file info: %w", err)
	}

	if offset > 0 {
		if _, err := file.Seek(offset, io.SeekStart); err != nil {
			return "", fmt.Errorf("filesystem: seeking file: %w", err)
		}
	}

	limitedReader := io.LimitReader(file, limit+1)
	data, err := io.ReadAll(limitedReader)
	if err != nil {
		return "", fmt.Errorf("filesystem: reading file: %w", err)
	}

	truncated := int64(len(data)) > limit
	if truncated {
		data = data[:limit]
	}

	result, err := json.Marshal(map[string]interface{}{
		"action":    "read",
		"content":   string(data),
		"size":      fileInformation.Size(),
		"truncated": truncated,
	})
	if err != nil {
		return "", fmt.Errorf("filesystem: marshaling result: %w", err)
	}
	return string(result), nil
}

func executeWrite(path, content string) (string, error) {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0755); err != nil {
		return "", fmt.Errorf("filesystem: creating parent directories: %w", err)
	}

	data := []byte(content)
	if err := atomicfile.WriteFile(path, data); err != nil {
		return "", fmt.Errorf("filesystem: writing file: %w", err)
	}

	result, err := json.Marshal(map[string]interface{}{
		"action":  "write",
		"success": true,
		"size":    len(data),
	})
	if err != nil {
		return "", fmt.Errorf("filesystem: marshaling result: %w", err)
	}
	return string(result), nil
}

func executeList(path string) (string, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return "", fmt.Errorf("filesystem: reading directory: %w", err)
	}

	truncated := len(entries) > maxDirectoryEntries
	if truncated {
		entries = entries[:maxDirectoryEntries]
	}

	type directoryEntry struct {
		Name       string `json:"name"`
		Type       string `json:"type"`
		Size       int64  `json:"size"`
		ModifiedAt string `json:"modifiedAt"`
	}

	outputEntries := make([]directoryEntry, 0, len(entries))
	for _, entry := range entries {
		entryType := "file"
		if entry.IsDir() {
			entryType = "directory"
		} else if entry.Type()&os.ModeSymlink != 0 {
			entryType = "symlink"
		}

		var size int64
		var modifiedAt string
		if information, err := entry.Info(); err == nil {
			size = information.Size()
			modifiedAt = information.ModTime().Format(time.RFC3339)
		}

		outputEntries = append(outputEntries, directoryEntry{
			Name:       entry.Name(),
			Type:       entryType,
			Size:       size,
			ModifiedAt: modifiedAt,
		})
	}

	result, err := json.Marshal(map[string]interface{}{
		"action":    "list",
		"entries":   outputEntries,
		"truncated": truncated,
	})
	if err != nil {
		return "", fmt.Errorf("filesystem: marshaling result: %w", err)
	}
	return string(result), nil
}

func executeInfo(path string) (string, error) {
	information, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("filesystem: getting file info: %w", err)
	}

	result, err := json.Marshal(map[string]interface{}{
		"action":      "info",
		"size":        information.Size(),
		"isDirectory": information.IsDir(),
		"permissions": information.Mode().String(),
		"modifiedAt":  information.ModTime().Format(time.RFC3339),
	})
	if err != nil {
		return "", fmt.Errorf("filesystem: marshaling result: %w", err)
	}
	return string(result), nil
}

func executeMkdir(path string, recursive bool) (string, error) {
	var err error
	if recursive {
		err = os.MkdirAll(path, 0755)
	} else {
		err = os.Mkdir(path, 0755)
	}
	if err != nil {
		return "", fmt.Errorf("filesystem: creating directory: %w", err)
	}

	result, err := json.Marshal(map[string]interface{}{
		"action":  "mkdir",
		"success": true,
	})
	if err != nil {
		return "", fmt.Errorf("filesystem: marshaling result: %w", err)
	}
	return string(result), nil
}

func (self *filesystemTool) executeDelete(path string, recursive bool) (string, error) {
	information, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("filesystem: deleting path: %w", err)
	}
	if information.IsDir() && !recursive {
		entries, readError := os.ReadDir(path)
		if readError != nil {
			return "", fmt.Errorf("filesystem: deleting path: %w", readError)
		}
		if len(entries) > 0 {
			return "", fmt.Errorf("filesystem: deleting path: directory not empty")
		}
	}

	if recursive {
		err = os.RemoveAll(path)
	} else {
		err = os.Remove(path)
	}
	if err != nil {
		return "", fmt.Errorf("filesystem: deleting path: %w", err)
	}

	result, err := json.Marshal(map[string]interface{}{
		"action":  "delete",
		"success": true,
	})
	if err != nil {
		return "", fmt.Errorf("filesystem: marshaling result: %w", err)
	}
	return string(result), nil
}

func executeSearch(root, pattern string) (string, error) {
	if pattern == "" {
		return "", fmt.Errorf("filesystem: pattern is required for search action")
	}

	var matches []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip inaccessible paths
		}
		matched, matchErr := filepath.Match(pattern, info.Name())
		if matchErr != nil {
			return fmt.Errorf("filesystem: invalid pattern: %w", matchErr)
		}
		if matched {
			matches = append(matches, path)
			if len(matches) >= maxDirectoryEntries {
				return filepath.SkipAll
			}
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("filesystem: searching: %w", err)
	}

	truncated := len(matches) >= maxDirectoryEntries
	result, err := json.Marshal(map[string]interface{}{
		"action":    "search",
		"matches":   matches,
		"truncated": truncated,
	})
	if err != nil {
		return "", fmt.Errorf("filesystem: marshaling result: %w", err)
	}
	return string(result), nil
}

func executeMove(path, destination string) (string, error) {
	if destination == "" {
		return "", fmt.Errorf("filesystem: destination is required for move action")
	}

	if err := os.Rename(path, destination); err != nil {
		return "", fmt.Errorf("filesystem: moving path: %w", err)
	}

	result, err := json.Marshal(map[string]interface{}{
		"action":  "move",
		"success": true,
	})
	if err != nil {
		return "", fmt.Errorf("filesystem: marshaling result: %w", err)
	}
	return string(result), nil
}
