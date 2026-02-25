package skills

import (
	"context"
	"strings"

	"github.com/teanode/teanode/internal/agents"
	"github.com/teanode/teanode/internal/configs"
)

// RegisterSkills loads skills from the directory and registers their tools.
// Returns the combined prompt text from all loaded skills (empty if none).
func RegisterSkills(ctx context.Context, registry *agents.ToolRegistry, skillsDirectory string) string {
	return RegisterSkillsFiltered(ctx, registry, skillsDirectory, nil)
}

// RegisterSkillsFiltered loads skills from the directory and registers their tools,
// filtering by the given allow list. A nil list means all skills are loaded.
// Returns the combined prompt text from all registered skills (empty if none).
func RegisterSkillsFiltered(ctx context.Context, registry *agents.ToolRegistry, skillsDirectory string, allowed []string) string {
	skills, err := LoadAll(ctx, skillsDirectory)
	if err != nil {
		log.Warningf("failed to load skills: %v", err)
		return ""
	}

	var skillPrompts []string
	for _, skill := range skills {
		// Check if this skill is in the allow list.
		if !configs.IsAllowed(skill.Name, allowed) {
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
				registry.Register(&HTTPTool{definition: *tool, httpAuthProfiles: skill.HTTPAuth})
				count++
			case "workflow":
				registry.Register(&WorkflowTool{definition: *tool, httpAuthProfiles: skill.HTTPAuth})
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

// Names returns the names of all valid skills in the directory.
func Names(ctx context.Context, skillsDirectory string) []string {
	skills, err := LoadAll(ctx, skillsDirectory)
	if err != nil {
		return nil
	}
	names := make([]string, len(skills))
	for index, definition := range skills {
		names[index] = definition.Name
	}
	return names
}
