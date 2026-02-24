package configs

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/teanode/teanode/internal/util/atomicfile"
	"gopkg.in/yaml.v3"
)

// UserConfig stores user-facing identity information.
type UserConfig struct {
	Name          string `json:"name" yaml:"name"`
	Description   string `json:"description,omitempty" yaml:"description,omitempty"`
	AvatarMediaID string `json:"avatarMediaId,omitempty" yaml:"avatarMediaId,omitempty"`
}

// OSUsername returns the current process username, falling back to environment
// variables and finally "teanode" if unavailable.
func OSUsername() string {
	if current, err := user.Current(); err == nil {
		name := strings.TrimSpace(current.Username)
		if name != "" {
			return name
		}
	}
	for _, key := range []string{"USER", "USERNAME"} {
		name := strings.TrimSpace(os.Getenv(key))
		if name != "" {
			return name
		}
	}
	return "teanode"
}

// LoadUserConfig reads ~/.teanode/users/<userId>/user.yaml.
func LoadUserConfig(userId string) (*UserConfig, error) {
	path := UserConfigFile(strings.TrimSpace(userId))
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &UserConfig{Name: OSUsername()}, nil
		}
		return nil, fmt.Errorf("reading user config: %w", err)
	}

	userConfig := &UserConfig{}
	if err := yaml.Unmarshal(data, userConfig); err != nil {
		return nil, fmt.Errorf("parsing user config: %w", err)
	}
	userConfig.Name = strings.TrimSpace(userConfig.Name)
	if userConfig.Name == "" {
		userConfig.Name = OSUsername()
	}
	userConfig.AvatarMediaID = strings.TrimSpace(userConfig.AvatarMediaID)
	userConfig.Description = strings.TrimSpace(userConfig.Description)
	return userConfig, nil
}

// SaveUserConfig writes ~/.teanode/users/<userId>/user.yaml with mode 0600.
func SaveUserConfig(userId string, userConfig *UserConfig) error {
	if userConfig == nil {
		return fmt.Errorf("user config is required")
	}
	name := strings.TrimSpace(userConfig.Name)
	if name == "" {
		name = OSUsername()
	}
	normalized := &UserConfig{
		Name:          name,
		Description:   strings.TrimSpace(userConfig.Description),
		AvatarMediaID: strings.TrimSpace(userConfig.AvatarMediaID),
	}

	path := UserConfigFile(strings.TrimSpace(userId))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating user config directory: %w", err)
	}
	payload, err := yaml.Marshal(normalized)
	if err != nil {
		return fmt.Errorf("marshalling user config: %w", err)
	}
	return atomicfile.WriteFileWithMode(path, payload, 0600)
}
