package skills

import (
	"context"
	"strings"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/tools"
	"github.com/teanode/teanode/internal/util/allowlist"
)

// RegisterSkills loads skills from store and registers their tools,
// filtering by the given allow list. A nil list means all skills are loaded.
// Returns the combined prompt text from all registered skills (empty if none).
func RegisterSkills(ctx context.Context, registry *tools.ToolRegistry, allowed []string) string {
	var skills []*models.Skill
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		var err error
		skills, err = transaction.ListSkills(ctx, nil)
		return err
	}); err != nil {
		log.Warningf("failed to load skills: %v", err)
	}

	var skillPrompts []string
	for _, skill := range skills {
		name := skill.GetName()
		// Check if this skill is in the allow list.
		if !allowlist.IsAllowed(name, allowed) {
			continue
		}

		authenticationProfiles := map[string]models.SkillAuthenticationProfiles{}
		if skill.AuthenticationProfiles != nil {
			authenticationProfiles = *skill.AuthenticationProfiles
		}

		count := 0
		if skill.Tools != nil {
			for _, tool := range *skill.Tools {
				switch tool.Type {
				case models.SkillToolTypeShell:
					registry.Register(&ShellTool{definition: *tool})
					count++
				case models.SkillToolTypeHTTP:
					registry.Register(&HTTPTool{definition: *tool, authenticationProfiles: authenticationProfiles})
					count++
				case models.SkillToolTypeWorkflow:
					registry.Register(&WorkflowTool{definition: *tool, authenticationProfiles: authenticationProfiles})
					count++
				}
			}
		}
		log.Infof("loaded %d tools from %s", count, name)

		prompt := skill.GetPrompt()
		if prompt != "" {
			skillPrompts = append(skillPrompts, prompt)
		}
	}

	return strings.Join(skillPrompts, "\n\n")
}
