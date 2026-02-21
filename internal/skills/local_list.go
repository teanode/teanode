package skills

import (
	"os"
	"path/filepath"
	"strings"
)

// ListLocal returns valid top-level local skills from skillsDirectory (*.md only).
// Installed registry skills under .installed are intentionally excluded.
func ListLocal(skillsDirectory string) ([]SkillDefinition, error) {
	entries, err := os.ReadDir(skillsDirectory)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var definitions []SkillDefinition
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(skillsDirectory, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var definition SkillDefinition
		body, err := parseSkillMarkdown(data, &definition)
		if err != nil {
			continue
		}
		definition.IsLocal = true
		definition.Prompt = strings.TrimSpace(body)
		if definition.Name == "" {
			continue
		}

		var validTools []ToolDefinition
		for _, tool := range definition.Tools {
			if validateTool(tool) == nil {
				validTools = append(validTools, tool)
			}
		}
		if len(validTools) == 0 {
			continue
		}
		definition.Tools = validTools
		definitions = append(definitions, definition)
	}
	return definitions, nil
}
