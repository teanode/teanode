package configs

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/teanode/teanode/internal/util/atomicfile"
	"github.com/teanode/teanode/internal/util/timeutil"
	"gopkg.in/yaml.v3"
)

// AgentConfig defines persisted configuration for an agent in agents/<id>/agent.yaml.
type AgentConfig struct {
	ID                   string             `json:"id" yaml:"id"`                             // unique; "main" is default
	Name                 string             `json:"name,omitempty" yaml:"name,omitempty"`     // friendly display name
	Model                string             `json:"model,omitempty" yaml:"model,omitempty"`   // qualified model override (e.g. "openai:gpt-5.2")
	Skills               []string           `json:"skills,omitempty" yaml:"skills,omitempty"` // skill allow list (nil = all)
	Tools                []string           `json:"tools,omitempty" yaml:"tools,omitempty"`   // tool allow list (nil = all)
	Description          string             `json:"description,omitempty" yaml:"description,omitempty"`
	DescriptionUpdatedAt timeutil.Timestamp `json:"descriptionUpdatedAt,omitempty" yaml:"descriptionUpdatedAt,omitempty"`
	AvatarMediaID        string             `json:"avatarMediaId,omitempty" yaml:"avatarMediaId,omitempty"`
}

func loadAgentConfigFromPath(path string) (*AgentConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	agentConfig := &AgentConfig{}
	if err := yaml.Unmarshal(data, agentConfig); err != nil {
		return nil, err
	}
	return agentConfig, nil
}

func saveAgentConfigToPath(path string, agentConfig *AgentConfig) error {
	if agentConfig == nil {
		agentConfig = &AgentConfig{}
	}
	data, err := yaml.Marshal(agentConfig)
	if err != nil {
		return err
	}
	return atomicfile.WriteFile(path, data)
}

// LoadAgentConfigs walks agents/*/agent.yaml and returns all agent configs.
func LoadAgentConfigs() ([]AgentConfig, error) {
	agentsDirectory := AgentsDirectory()

	entries, err := os.ReadDir(agentsDirectory)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading agents directory: %w", err)
	}

	agentConfigs := make([]AgentConfig, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		agentId := entry.Name()
		path := AgentConfigFile(agentId)
		agentConfig, err := loadAgentConfigFromPath(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("reading agent file %s: %w", agentId, err)
		}
		agentConfig.ID = agentId
		agentConfigs = append(agentConfigs, *agentConfig)
	}
	return agentConfigs, nil
}

// LoadAgentConfig reads agents/<id>/agent.yaml.
// Missing files return an empty config with ID set.
func LoadAgentConfig(agentId string) (*AgentConfig, error) {
	path := AgentConfigFile(agentId)
	agentConfig, err := loadAgentConfigFromPath(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &AgentConfig{ID: agentId}, nil
		}
		return nil, fmt.Errorf("parsing agent config %s: %w", agentId, err)
	}
	agentConfig.ID = agentId
	return agentConfig, nil
}

// SaveAgentConfig writes agents/<id>/agent.yaml atomically.
func SaveAgentConfig(agentId string, agentConfig *AgentConfig) error {
	if agentConfig == nil {
		agentConfig = &AgentConfig{}
	}
	agentsDirectory := AgentsDirectory()
	agentDirectory := filepath.Join(agentsDirectory, agentId)
	if err := os.MkdirAll(agentDirectory, 0755); err != nil {
		return fmt.Errorf("creating agent directory: %w", err)
	}
	path := AgentConfigFile(agentId)
	copyConfig := *agentConfig
	copyConfig.ID = agentId
	if err := saveAgentConfigToPath(path, &copyConfig); err != nil {
		return fmt.Errorf("saving agent file %s: %w", agentId, err)
	}
	return nil
}
