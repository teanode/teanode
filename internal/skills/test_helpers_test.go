package skills

import (
	"context"
	"strings"
	"testing"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	storefs "github.com/teanode/teanode/internal/store/fsstore"
	"github.com/teanode/teanode/internal/util/ptrto"
	"gopkg.in/yaml.v3"
)

func setupSkillStore(t *testing.T) store.Store {
	t.Helper()
	openedStore, openError := storefs.Open(storefs.Options{DataDirectory: t.TempDir()})
	if openError != nil {
		t.Fatalf("opening store backend: %v", openError)
	}
	if migrateError := openedStore.Migrate(context.Background()); migrateError != nil {
		t.Fatalf("migrating store: %v", migrateError)
	}
	t.Cleanup(func() { _ = openedStore.Close() })
	return openedStore
}

// skillFrontmatter represents the YAML frontmatter of a skill markdown file.
type skillFrontmatter struct {
	Name                   string                                        `yaml:"name"`
	Description            string                                        `yaml:"description"`
	Tools                  []*models.SkillTool                           `yaml:"tools"`
	AuthenticationProfiles map[string]models.SkillAuthenticationProfiles `yaml:"authenticationProfiles"`
}

func createStoredSkillFromMarkdown(t *testing.T, openedStore store.Store, skillId string, version string, markdown string, enabled bool) {
	t.Helper()

	// Parse YAML frontmatter.
	parts := strings.SplitN(markdown, "---", 3)
	if len(parts) < 3 {
		t.Fatalf("invalid skill markdown: expected ---yaml---body")
	}
	var frontmatter skillFrontmatter
	if err := yaml.Unmarshal([]byte(parts[1]), &frontmatter); err != nil {
		t.Fatalf("parsing skill frontmatter: %v", err)
	}
	prompt := strings.TrimSpace(parts[2])

	skill := &models.Skill{
		ID:      skillId,
		Name:    ptrto.Value(frontmatter.Name),
		Version: ptrto.Value(version),
		Enabled: ptrto.Value(enabled),
		Tools:   &frontmatter.Tools,
	}
	if frontmatter.Description != "" {
		skill.Description = ptrto.Value(frontmatter.Description)
	}
	if prompt != "" {
		skill.Prompt = ptrto.Value(prompt)
	}
	if len(frontmatter.AuthenticationProfiles) > 0 {
		skill.AuthenticationProfiles = &frontmatter.AuthenticationProfiles
	}

	if err := openedStore.Transaction(context.Background(), func(ctx context.Context, transaction store.Transaction) error {
		_, createError := transaction.CreateSkill(ctx, skill, nil)
		return createError
	}); err != nil {
		t.Fatalf("creating skill %s: %v", skillId, err)
	}
}
