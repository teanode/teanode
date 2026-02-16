package skill

import (
	"strings"

	"github.com/teanode/teanode/internal/agents"
	"github.com/teanode/teanode/internal/configs"
)

// RegisterSkills loads skills from the directory and registers their tools.
// Returns the combined prompt text from all loaded skills (empty if none).
func RegisterSkills(registry *agents.ToolRegistry, skillsDirectory string) string {
	return RegisterSkillsFiltered(registry, skillsDirectory, nil)
}

// RegisterSkillsFiltered loads skills from the directory and registers their tools,
// filtering by the given skills filter. Returns the combined prompt text from
// all registered skills (empty if none).
func RegisterSkillsFiltered(registry *agents.ToolRegistry, skillsDirectory string, filter *configs.FilterConfig) string {
	skills, err := LoadAll(skillsDirectory)
	if err != nil {
		log.Warningf("failed to load skills: %v", err)
		return ""
	}

	var skillPrompts []string
	for _, skill := range skills {
		// Check if this skill is allowed by the filter.
		if !configs.IsAllowed(skill.Name, filter) {
			continue
		}

		count := 0
		for index := range skill.Tools {
			tool := &skill.Tools[index]
			switch tool.Type {
			case "shell":
				registry.Register(&ShellTool{definition: *tool})
				count++
			case "http":
				registry.Register(&HTTPTool{definition: *tool})
				count++
			}
		}
		log.Infof("loaded %d tools from %s", count, skill.Name)

		if skill.Prompt != "" {
			skillPrompts = append(skillPrompts, skill.Prompt)
		}
	}

	return strings.Join(skillPrompts, "\n\n")
}
