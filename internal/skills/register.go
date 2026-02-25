package skills

import (
	"context"
	"strings"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	toolregistry "github.com/teanode/teanode/internal/tools"
	"github.com/teanode/teanode/internal/util/allowlist"
)

// RegisterSkills loads skills from store and registers their tools.
// Returns the combined prompt text from all loaded skills (empty if none).
func RegisterSkills(ctx context.Context, registry *toolregistry.ToolRegistry) string {
	return RegisterSkillsFiltered(ctx, registry, nil)
}

// RegisterSkillsFiltered loads skills from store and registers their tools,
// filtering by the given allow list. A nil list means all skills are loaded.
// Returns the combined prompt text from all registered skills (empty if none).
func RegisterSkillsFiltered(ctx context.Context, registry *toolregistry.ToolRegistry, allowed []string) string {
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

		httpAuth := map[string]models.SkillAuthenticationProfiles{}
		if skill.AuthenticationProfiles != nil {
			httpAuth = *skill.AuthenticationProfiles
		}

		count := 0
		if skill.Tools != nil {
			for _, tool := range *skill.Tools {
				switch tool.Type {
				case "shell":
					registry.Register(&ShellTool{definition: *tool})
					count++
				case "http":
					registry.Register(&HTTPTool{definition: *tool, httpAuthProfiles: httpAuth})
					count++
				case "workflow":
					registry.Register(&WorkflowTool{definition: *tool, httpAuthProfiles: httpAuth})
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

// Names returns the names of all skills in the store.
func Names(ctx context.Context) []string {
	var skills []*models.Skill
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		var err error
		skills, err = transaction.ListSkills(ctx, nil)
		return err
	}); err != nil {
		return nil
	}
	names := make([]string, 0, len(skills))
	for _, skill := range skills {
		name := skill.GetName()
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}
