package projects

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/prompts"
	"github.com/teanode/teanode/internal/util/atomicfile"
	"github.com/teanode/teanode/internal/util/security"
	"github.com/teanode/teanode/internal/util/timeutil"
	"github.com/teanode/teanode/internal/util/trash"
)

const defaultProjectDocumentName = "PROJECT.md"

func WorkspaceDirectory(projectId string) (string, error) {
	return workspaceDirectory(projectId)
}

func projectConfigPath(projectId string) (string, error) {
	directory, err := projectDirectory(projectId)
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "project.yaml"), nil
}

func projectDirectory(projectId string) (string, error) {
	directory := configs.ProjectsDirectory()
	return filepath.Join(directory, projectId), nil
}

func workspaceDirectory(projectId string) (string, error) {
	directory := configs.ProjectsDirectory()
	return filepath.Join(directory, projectId, "workspace"), nil
}

func safeProjectPath(projectId, relPath string) (string, string, error) {
	workspace, err := workspaceDirectory(projectId)
	if err != nil {
		return "", "", err
	}
	cleaned := filepath.Clean(relPath)
	if cleaned == "." || cleaned == "" || filepath.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("invalid path: %s", relPath)
	}
	full := filepath.Join(workspace, cleaned)
	if err := ensureWithinRoot(workspace, full); err != nil {
		return "", "", fmt.Errorf("path traversal denied: %s", relPath)
	}
	if err := ensureNoSymlinkComponents(workspace, full); err != nil {
		return "", "", err
	}
	return workspace, full, nil
}

func initializeProjectFile(workspace string, metadata configs.ProjectConfig, purpose string) error {
	path := filepath.Join(workspace, defaultProjectDocumentName)
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	content, err := prompts.BuildProjectMarkdown(metadata.Name, metadata.ID, metadata.Description, purpose)
	if err != nil {
		return err
	}
	return atomicfile.WriteFile(path, []byte(content))
}

func CreateProject(name, description, purpose string) (*configs.ProjectConfig, error) {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}

	projectsDirectory := configs.ProjectsDirectory()
	if err := os.MkdirAll(projectsDirectory, 0755); err != nil {
		return nil, fmt.Errorf("creating projects directory: %w", err)
	}

	metadata := configs.ProjectConfig{
		ID:          security.NewULID(),
		Name:        name,
		Description: description,
		UpdatedAt:   timeutil.Now(),
	}
	workspace, err := workspaceDirectory(metadata.ID)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(workspace, 0755); err != nil {
		return nil, fmt.Errorf("creating project workspace: %w", err)
	}
	if err := initializeProjectFile(workspace, metadata, purpose); err != nil {
		return nil, fmt.Errorf("initializing PROJECT.md: %w", err)
	}
	if err := configs.SaveProjectConfig(metadata.ID, &metadata); err != nil {
		return nil, err
	}
	return &metadata, nil
}

func RenameProject(projectId, name string) (*configs.ProjectConfig, error) {
	metadata, err := configs.LoadProjectConfig(projectId)
	if err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	metadata.Name = name
	metadata.UpdatedAt = timeutil.Now()
	if err := configs.SaveProjectConfig(metadata.ID, metadata); err != nil {
		return nil, err
	}
	return metadata, nil
}

func DeleteProject(projectId string) error {
	workspace, err := workspaceDirectory(projectId)
	if err != nil {
		return err
	}
	metadata, err := projectConfigPath(projectId)
	if err != nil {
		return err
	}
	root := configs.Directory()
	trashDirectory := configs.TrashDirectory()

	if _, err := os.Stat(metadata); err == nil {
		if isPathInsideDirectory(metadata, root) {
			if err := trash.Move(metadata, trashDirectory); err != nil {
				return fmt.Errorf("deleting project metadata: %w", err)
			}
		} else {
			if err := os.Remove(metadata); err != nil {
				return fmt.Errorf("deleting project metadata: %w", err)
			}
		}
	}
	if _, err := os.Stat(workspace); err == nil {
		if isPathInsideDirectory(workspace, root) {
			if err := trash.Move(workspace, trashDirectory); err != nil {
				return fmt.Errorf("deleting project workspace: %w", err)
			}
		} else {
			if err := os.RemoveAll(workspace); err != nil {
				return fmt.Errorf("deleting project workspace: %w", err)
			}
		}
	}
	return nil
}

func touch(projectId string) error {
	metadata, err := configs.LoadProjectConfig(projectId)
	if err != nil {
		return err
	}
	metadata.UpdatedAt = timeutil.Now()
	return configs.SaveProjectConfig(metadata.ID, metadata)
}

func listFiles(projectId string) ([]string, error) {
	workspace, err := workspaceDirectory(projectId)
	if err != nil {
		return nil, err
	}
	files := []string{}
	err = filepath.Walk(workspace, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		rel, err := filepath.Rel(workspace, path)
		if err != nil {
			return err
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("listing project files: %w", err)
	}
	return files, nil
}

func readFile(projectId, path string) (string, error) {
	_, full, err := safeProjectPath(projectId, path)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return "", fmt.Errorf("reading file: %w", err)
	}
	return string(data), nil
}

func writeFile(projectId, path, content string) error {
	_, full, err := safeProjectPath(projectId, path)
	if err != nil {
		return err
	}
	if err := atomicfile.WriteFile(full, []byte(content)); err != nil {
		return fmt.Errorf("writing file: %w", err)
	}
	return touch(projectId)
}

func appendFile(projectId, path, content string) error {
	_, full, err := safeProjectPath(projectId, path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		return fmt.Errorf("creating parent directory: %w", err)
	}
	file, err := os.OpenFile(full, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("opening file: %w", err)
	}
	defer file.Close()
	if _, err := file.WriteString(content + "\n"); err != nil {
		return fmt.Errorf("appending file: %w", err)
	}
	return touch(projectId)
}

type searchMatch struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

func searchFiles(projectId, query string, maxResults int) ([]searchMatch, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("query is required")
	}
	if maxResults <= 0 {
		maxResults = 10
	}
	workspace, err := workspaceDirectory(projectId)
	if err != nil {
		return nil, err
	}

	queryLower := strings.ToLower(query)
	matches := []searchMatch{}
	err = filepath.Walk(workspace, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		if !strings.HasSuffix(path, ".md") && !strings.HasSuffix(path, ".txt") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(workspace, path)
		if err != nil {
			return nil
		}
		lines := strings.Split(string(data), "\n")
		for index, line := range lines {
			if len(matches) >= maxResults {
				return filepath.SkipAll
			}
			if strings.Contains(strings.ToLower(line), queryLower) {
				matches = append(matches, searchMatch{Path: rel, Line: index + 1, Text: line})
			}
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("searching project files: %w", err)
	}
	return matches, nil
}

func deleteFile(projectId, path string) error {
	workspace, full, err := safeProjectPath(projectId, path)
	if err != nil {
		return err
	}
	info, err := os.Stat(full)
	if err != nil {
		return fmt.Errorf("file not found: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("cannot delete directories, only files")
	}

	root := configs.Directory()
	if isPathInsideDirectory(full, root) {
		trashDirectory := configs.TrashDirectory()
		if err := trash.Move(full, trashDirectory); err != nil {
			return fmt.Errorf("deleting file: %w", err)
		}
	} else {
		if err := os.Remove(full); err != nil {
			return fmt.Errorf("deleting file: %w", err)
		}
	}

	directory := filepath.Dir(full)
	for directory != workspace {
		entries, err := os.ReadDir(directory)
		if err != nil || len(entries) > 0 {
			break
		}
		os.Remove(directory)
		directory = filepath.Dir(directory)
	}
	return touch(projectId)
}

func moveFile(projectId, fromPath, toPath string) error {
	_, source, err := safeProjectPath(projectId, fromPath)
	if err != nil {
		return err
	}
	workspace, target, err := safeProjectPath(projectId, toPath)
	if err != nil {
		return err
	}
	if source == target {
		return nil
	}
	info, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("source not found: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("cannot move directories, only files")
	}
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return fmt.Errorf("creating destination directory: %w", err)
	}
	if err := os.Rename(source, target); err != nil {
		return fmt.Errorf("moving file: %w", err)
	}

	directory := filepath.Dir(source)
	for directory != workspace {
		entries, err := os.ReadDir(directory)
		if err != nil || len(entries) > 0 {
			break
		}
		os.Remove(directory)
		directory = filepath.Dir(directory)
	}
	return touch(projectId)
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

func ensureWithinRoot(root string, path string) error {
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	relativePath, err := filepath.Rel(absoluteRoot, absolutePath)
	if err != nil {
		return err
	}
	if relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
		return fmt.Errorf("invalid path")
	}
	return nil
}

func ensureNoSymlinkComponents(root string, path string) error {
	info, err := os.Lstat(root)
	if err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("invalid path")
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	relativePath, err := filepath.Rel(root, path)
	if err != nil {
		return err
	}
	current := root
	for _, part := range strings.Split(relativePath, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		componentInfo, componentErr := os.Lstat(current)
		if componentErr != nil {
			if os.IsNotExist(componentErr) {
				continue
			}
			return componentErr
		}
		if componentInfo.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("invalid path")
		}
	}
	return nil
}
