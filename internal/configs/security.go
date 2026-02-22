package configs

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/teanode/teanode/internal/util/atomicfile"
	"gopkg.in/yaml.v3"
)

// SecurityConfig holds sensitive auth credentials stored separately from the
// main config in ~/.teanode/security.yaml.
type SecurityConfig struct {
	Token    string `json:"token,omitempty" yaml:"token,omitempty"`
	Password string `json:"password,omitempty" yaml:"password,omitempty"` // bcrypt hash
}

// SecurityFile returns the path to ~/.teanode/security.yaml.
func SecurityFile() (string, error) {
	directory, err := Directory()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "security.yaml"), nil
}

// LoadSecurity reads and unmarshals security.yaml. Returns an empty config if
// the file does not exist.
func LoadSecurity() (*SecurityConfig, error) {
	securityFile, err := SecurityFile()
	if err != nil {
		return nil, err
	}

	config := &SecurityConfig{}
	data, err := os.ReadFile(securityFile)
	if err != nil {
		if os.IsNotExist(err) {
			return config, nil
		}
		return nil, fmt.Errorf("reading security config: %w", err)
	}
	if err := yaml.Unmarshal(data, config); err != nil {
		return nil, fmt.Errorf("parsing security config: %w", err)
	}
	return config, nil
}

// SaveSecurity writes the security config to ~/.teanode/security.yaml atomically.
func SaveSecurity(config *SecurityConfig) error {
	securityFile, err := SecurityFile()
	if err != nil {
		return err
	}
	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("marshalling security config: %w", err)
	}
	return atomicfile.WriteFileWithMode(securityFile, data, 0600)
}
