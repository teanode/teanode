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

// UserProfile stores user-facing identity information.
type UserProfile struct {
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

// LoadUserProfile reads ~/.teanode/users/<userId>/user.yaml.
func LoadUserProfile(userId string) (*UserProfile, error) {
	path, err := UserProfileFile(strings.TrimSpace(userId))
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &UserProfile{Name: OSUsername()}, nil
		}
		return nil, fmt.Errorf("reading user profile: %w", err)
	}

	profile := &UserProfile{}
	if err := yaml.Unmarshal(data, profile); err != nil {
		return nil, fmt.Errorf("parsing user profile: %w", err)
	}
	profile.Name = strings.TrimSpace(profile.Name)
	if profile.Name == "" {
		profile.Name = OSUsername()
	}
	profile.AvatarMediaID = strings.TrimSpace(profile.AvatarMediaID)
	profile.Description = strings.TrimSpace(profile.Description)
	return profile, nil
}

// SaveUserProfile writes ~/.teanode/users/<userId>/user.yaml with mode 0600.
func SaveUserProfile(userId string, profile *UserProfile) error {
	if profile == nil {
		return fmt.Errorf("user profile is required")
	}
	name := strings.TrimSpace(profile.Name)
	if name == "" {
		name = OSUsername()
	}
	normalized := &UserProfile{
		Name:          name,
		Description:   strings.TrimSpace(profile.Description),
		AvatarMediaID: strings.TrimSpace(profile.AvatarMediaID),
	}

	path, err := UserProfileFile(strings.TrimSpace(userId))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating user profile directory: %w", err)
	}
	payload, err := yaml.Marshal(normalized)
	if err != nil {
		return fmt.Errorf("marshalling user profile: %w", err)
	}
	return atomicfile.WriteFileWithMode(path, payload, 0600)
}
