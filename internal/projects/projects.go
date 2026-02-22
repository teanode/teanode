package projects

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/util/atomicfile"
	"github.com/teanode/teanode/internal/util/security"
	"github.com/teanode/teanode/internal/util/trash"
	"gopkg.in/yaml.v3"
)

const defaultProjectDocumentName = "PROJECT.md"

var validProjectIDPattern = regexp.MustCompile(`(?i)^[0-9a-hjkmnp-tv-z]{26}$`)

// Metadata stores persistent project metadata at ~/.teanode/projects/<projectId>.yaml.
type Metadata struct {
	ID          string `json:"id" yaml:"id"`
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description" yaml:"description"`
	UpdatedAt   int64  `json:"updatedAt" yaml:"updatedAt"`
}

func Directory() (string, error) {
	return configs.ProjectsDirectory()
}

func MetadataPath(projectID string) (string, error) {
	return metadataPath(projectID)
}

func WorkspaceDirectory(projectID string) (string, error) {
	return workspaceDirectory(projectID)
}

func ValidateProjectID(projectID string) error {
	projectID = strings.TrimSpace(projectID)
	if !validProjectIDPattern.MatchString(projectID) {
		return fmt.Errorf("invalid projectId: %s", projectID)
	}
	return nil
}

func normalizeProjectID(projectID string) (string, error) {
	projectID = strings.TrimSpace(projectID)
	if err := ValidateProjectID(projectID); err != nil {
		return "", err
	}
	return strings.ToLower(projectID), nil
}

func metadataPath(projectID string) (string, error) {
	normalizedProjectID, err := normalizeProjectID(projectID)
	if err != nil {
		return "", err
	}
	directory, err := Directory()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, normalizedProjectID+".yaml"), nil
}

func workspaceDirectory(projectID string) (string, error) {
	normalizedProjectID, err := normalizeProjectID(projectID)
	if err != nil {
		return "", err
	}
	directory, err := Directory()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, normalizedProjectID), nil
}

func safeProjectPath(projectID, relPath string) (string, string, error) {
	workspace, err := workspaceDirectory(projectID)
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

func ensureDirectory() (string, error) {
	directory, err := Directory()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(directory, 0755); err != nil {
		return "", fmt.Errorf("creating projects directory: %w", err)
	}
	return directory, nil
}

func nowMillis() int64 {
	return time.Now().UnixMilli()
}

func writeMetadata(path string, metadata Metadata) error {
	payload, err := yaml.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("marshalling metadata: %w", err)
	}
	if err := atomicfile.WriteFile(path, payload); err != nil {
		return fmt.Errorf("writing metadata: %w", err)
	}
	return nil
}

type metadataDisk struct {
	ID            string `yaml:"id"`
	Name          string `yaml:"name"`
	Description   string `yaml:"description"`
	UpdatedAt     int64  `yaml:"updatedAt"`
	LegacyUpdated int64  `yaml:"updated"`
}

func decodeMetadata(data []byte) (Metadata, error) {
	var disk metadataDisk
	if err := yaml.Unmarshal(data, &disk); err != nil {
		return Metadata{}, fmt.Errorf("parsing metadata: %w", err)
	}
	updatedAt := disk.UpdatedAt
	if updatedAt <= 0 {
		updatedAt = disk.LegacyUpdated
	}
	return Metadata{
		ID:          disk.ID,
		Name:        disk.Name,
		Description: disk.Description,
		UpdatedAt:   updatedAt,
	}, nil
}

func List() ([]Metadata, error) {
	directory, err := Directory()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		if os.IsNotExist(err) {
			return []Metadata{}, nil
		}
		return nil, fmt.Errorf("reading projects directory: %w", err)
	}

	items := make([]Metadata, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		metadata, err := decodeMetadata(data)
		if err != nil {
			continue
		}
		if metadata.ID == "" {
			metadata.ID = strings.TrimSuffix(entry.Name(), ".yaml")
		}
		metadata.ID = strings.ToLower(metadata.ID)
		if metadata.ID == "" || strings.TrimSpace(metadata.Name) == "" {
			continue
		}
		items = append(items, metadata)
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].UpdatedAt == items[j].UpdatedAt {
			return items[i].Name < items[j].Name
		}
		return items[i].UpdatedAt > items[j].UpdatedAt
	})
	return items, nil
}

func Get(projectID string) (*Metadata, error) {
	normalizedProjectID, err := normalizeProjectID(projectID)
	if err != nil {
		return nil, err
	}
	path, err := metadataPath(projectID)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	metadata, err := decodeMetadata(data)
	if err != nil {
		return nil, err
	}
	if metadata.ID == "" {
		metadata.ID = normalizedProjectID
	} else {
		metadata.ID = strings.ToLower(metadata.ID)
	}
	return &metadata, nil
}

func Save(metadata Metadata) error {
	normalizedProjectID, err := normalizeProjectID(metadata.ID)
	if err != nil {
		return err
	}
	metadata.ID = normalizedProjectID
	if strings.TrimSpace(metadata.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if metadata.UpdatedAt <= 0 {
		metadata.UpdatedAt = nowMillis()
	}
	if _, err := ensureDirectory(); err != nil {
		return err
	}
	path, err := metadataPath(metadata.ID)
	if err != nil {
		return err
	}
	return writeMetadata(path, metadata)
}

func initializeProjectFile(workspace string, metadata Metadata, purpose string) error {
	path := filepath.Join(workspace, defaultProjectDocumentName)
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	content := "# " + metadata.Name + "\n\n"
	content += "Project ID: " + metadata.ID + "\n\n"
	content += "## Description\n\n" + metadata.Description + "\n"
	if strings.TrimSpace(purpose) != "" {
		content += "\n## Purpose\n\n" + strings.TrimSpace(purpose) + "\n"
	}
	return atomicfile.WriteFile(path, []byte(content))
}

func Create(name, description, purpose string) (*Metadata, error) {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}

	if _, err := ensureDirectory(); err != nil {
		return nil, err
	}

	metadata := Metadata{
		ID:          security.NewULID(),
		Name:        name,
		Description: description,
		UpdatedAt:   nowMillis(),
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
	if err := Save(metadata); err != nil {
		return nil, err
	}
	return &metadata, nil
}

func Rename(projectID, name string) (*Metadata, error) {
	metadata, err := Get(projectID)
	if err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	metadata.Name = name
	metadata.UpdatedAt = nowMillis()
	if err := Save(*metadata); err != nil {
		return nil, err
	}
	return metadata, nil
}

func Delete(projectID string) error {
	workspace, err := workspaceDirectory(projectID)
	if err != nil {
		return err
	}
	metadata, err := metadataPath(projectID)
	if err != nil {
		return err
	}

	root, err := configs.Directory()
	if err != nil {
		return err
	}
	trashDirectory, err := configs.TrashDirectory()
	if err != nil {
		return err
	}

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

func Touch(projectID string) error {
	metadata, err := Get(projectID)
	if err != nil {
		return err
	}
	metadata.UpdatedAt = nowMillis()
	return Save(*metadata)
}

func ListFiles(projectID string) ([]string, error) {
	workspace, err := workspaceDirectory(projectID)
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

func ReadFile(projectID, path string) (string, error) {
	_, full, err := safeProjectPath(projectID, path)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return "", fmt.Errorf("reading file: %w", err)
	}
	return string(data), nil
}

func WriteFile(projectID, path, content string) error {
	_, full, err := safeProjectPath(projectID, path)
	if err != nil {
		return err
	}
	if err := atomicfile.WriteFile(full, []byte(content)); err != nil {
		return fmt.Errorf("writing file: %w", err)
	}
	return Touch(projectID)
}

func AppendFile(projectID, path, content string) error {
	_, full, err := safeProjectPath(projectID, path)
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
	return Touch(projectID)
}

type SearchMatch struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

func SearchFiles(projectID, query string, maxResults int) ([]SearchMatch, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("query is required")
	}
	if maxResults <= 0 {
		maxResults = 10
	}
	workspace, err := workspaceDirectory(projectID)
	if err != nil {
		return nil, err
	}

	queryLower := strings.ToLower(query)
	matches := []SearchMatch{}
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
				matches = append(matches, SearchMatch{Path: rel, Line: index + 1, Text: line})
			}
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("searching project files: %w", err)
	}
	return matches, nil
}

func DeleteFile(projectID, path string) error {
	workspace, full, err := safeProjectPath(projectID, path)
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

	root, err := configs.Directory()
	if err != nil {
		return err
	}
	if isPathInsideDirectory(full, root) {
		trashDirectory, err := configs.TrashDirectory()
		if err != nil {
			return err
		}
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
	return Touch(projectID)
}

func MoveFile(projectID, fromPath, toPath string) error {
	_, source, err := safeProjectPath(projectID, fromPath)
	if err != nil {
		return err
	}
	workspace, target, err := safeProjectPath(projectID, toPath)
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
	return Touch(projectID)
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
