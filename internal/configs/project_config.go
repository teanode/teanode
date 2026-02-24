package configs

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/teanode/teanode/internal/util/atomicfile"
	"github.com/teanode/teanode/internal/util/timeutil"
	"gopkg.in/yaml.v3"
)

// ProjectConfig stores persistent project metadata at ~/.teanode/projects/<projectId>/project.yaml.
type ProjectConfig struct {
	ID          string             `json:"id" yaml:"id"`
	Name        string             `json:"name" yaml:"name"`
	Description string             `json:"description" yaml:"description"`
	UpdatedAt   timeutil.Timestamp `json:"updatedAt" yaml:"updatedAt"`
}

type projectConfigDisk struct {
	Name        string             `yaml:"name"`
	Description string             `yaml:"description"`
	UpdatedAt   timeutil.Timestamp `yaml:"updatedAt"`
}

func projectDirectory(projectId string) (string, error) {
	directory := ProjectsDirectory()
	return filepath.Join(directory, projectId), nil
}

// ProjectConfigFile returns the project config path (~/.teanode/projects/<projectId>/project.yaml).
func ProjectConfigFile(projectId string) (string, error) {
	directory, err := projectDirectory(projectId)
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "project.yaml"), nil
}

func decodeProjectConfig(data []byte) (ProjectConfig, error) {
	var disk projectConfigDisk
	if err := yaml.Unmarshal(data, &disk); err != nil {
		return ProjectConfig{}, fmt.Errorf("parsing project config: %w", err)
	}
	return ProjectConfig{
		Name:        disk.Name,
		Description: disk.Description,
		UpdatedAt:   disk.UpdatedAt,
	}, nil
}

func encodeProjectConfig(projectConfig ProjectConfig) ([]byte, error) {
	return yaml.Marshal(projectConfigDisk{
		Name:        projectConfig.Name,
		Description: projectConfig.Description,
		UpdatedAt:   projectConfig.UpdatedAt,
	})
}

// LoadProjectConfig reads ~/.teanode/projects/<projectId>/project.yaml.
func LoadProjectConfig(projectId string) (*ProjectConfig, error) {
	path, err := ProjectConfigFile(projectId)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	projectConfig, err := decodeProjectConfig(data)
	if err != nil {
		return nil, err
	}
	projectConfig.ID = projectId
	return &projectConfig, nil
}

// SaveProjectConfig writes ~/.teanode/projects/<projectId>/project.yaml atomically.
func SaveProjectConfig(projectId string, projectConfig *ProjectConfig) error {
	if projectConfig == nil {
		return fmt.Errorf("project config is required")
	}
	if strings.TrimSpace(projectConfig.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if projectConfig.UpdatedAt.IsZero() {
		projectConfig.UpdatedAt = timeutil.Now()
	}
	directory, err := projectDirectory(projectId)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(directory, 0755); err != nil {
		return fmt.Errorf("creating project directory: %w", err)
	}
	copyConfig := *projectConfig
	copyConfig.ID = projectId
	payload, err := encodeProjectConfig(copyConfig)
	if err != nil {
		return fmt.Errorf("marshalling project config: %w", err)
	}
	path, err := ProjectConfigFile(projectId)
	if err != nil {
		return err
	}
	if err := atomicfile.WriteFile(path, payload); err != nil {
		return fmt.Errorf("writing project config: %w", err)
	}
	return nil
}

// LoadProjectConfigs lists project configs from ~/.teanode/projects/*/project.yaml.
func LoadProjectConfigs() ([]ProjectConfig, error) {
	directory := ProjectsDirectory()
	entries, err := os.ReadDir(directory)
	if err != nil {
		if os.IsNotExist(err) {
			return []ProjectConfig{}, nil
		}
		return nil, fmt.Errorf("reading projects directory: %w", err)
	}

	projectConfigs := make([]ProjectConfig, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		projectId := strings.ToLower(entry.Name())
		path := filepath.Join(directory, projectId, "project.yaml")
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		projectConfig, err := decodeProjectConfig(data)
		if err != nil {
			continue
		}
		if strings.TrimSpace(projectConfig.Name) == "" {
			continue
		}
		projectConfig.ID = projectId
		projectConfigs = append(projectConfigs, projectConfig)
	}

	sort.Slice(projectConfigs, func(leftIndex, rightIndex int) bool {
		left := projectConfigs[leftIndex]
		right := projectConfigs[rightIndex]
		if left.UpdatedAt.Time.Equal(right.UpdatedAt.Time) {
			return left.Name < right.Name
		}
		return left.UpdatedAt.Time.After(right.UpdatedAt.Time)
	})
	return projectConfigs, nil
}
