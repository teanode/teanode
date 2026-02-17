package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// LoadAll reads all *.yaml files from skillsDirectory and returns parsed skills.
// Logs warnings for malformed files and continues.
func LoadAll(skillsDirectory string) ([]SkillDefinition, error) {
	entries, err := os.ReadDir(skillsDirectory)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var skills []SkillDefinition
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}

		path := filepath.Join(skillsDirectory, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			log.Warningf("failed to read %s: %v", entry.Name(), err)
			continue
		}

		var skill SkillDefinition
		if err := yaml.Unmarshal(data, &skill); err != nil {
			log.Warningf("failed to parse %s: %v", entry.Name(), err)
			continue
		}

		if skill.Name == "" {
			log.Warningf("%s missing name, skipping", entry.Name())
			continue
		}

		// Validate tools, keeping only valid ones.
		var validTools []ToolDefinition
		for _, tool := range skill.Tools {
			if err := validateTool(tool); err != nil {
				log.Warningf("skill %s: tool %q invalid: %v", skill.Name, tool.Name, err)
				continue
			}
			validTools = append(validTools, tool)
		}
		skill.Tools = validTools

		if len(skill.Tools) == 0 {
			log.Warningf("skill %s: no valid tools, skipping", skill.Name)
			continue
		}

		skills = append(skills, skill)
	}

	return skills, nil
}

func validateTool(tool ToolDefinition) error {
	if tool.Name == "" {
		return fmt.Errorf("missing name")
	}
	switch tool.Type {
	case "shell":
		if len(tool.Command) == 0 {
			return fmt.Errorf("shell tool %q has empty command", tool.Name)
		}
	case "http":
		if tool.URL == "" {
			return fmt.Errorf("http tool %q has empty url", tool.Name)
		}
	default:
		return fmt.Errorf("unknown type %q", tool.Type)
	}
	return nil
}
