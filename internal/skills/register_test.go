package skills

import (
	"context"
	"testing"

	"github.com/teanode/teanode/internal/store"
	toolregistry "github.com/teanode/teanode/internal/tools"
)

func TestRegisterSkills(t *testing.T) {
	openedStore := setupSkillStore(t)
	createStoredSkillFromMarkdown(t, openedStore, "deploy", "1.0.0", `---
name: deploy
tools:
  - name: deploy_status
    description: Check deployment status
    type: shell
    command: ["echo", "deployed"]
  - name: deploy_api
    description: Call deploy API
    type: http
    url: "http://example.com/deploy"
---
Use deploy tools carefully.
`, true)

	registry := toolregistry.NewEmptyToolRegistry()
	ctx := store.ContextWithStore(context.Background(), openedStore)
	prompt := RegisterSkills(ctx, registry, nil)

	if registry.Get("deploy_status") == nil {
		t.Fatal("deploy_status not registered")
	}
	if registry.Get("deploy_api") == nil {
		t.Fatal("deploy_api not registered")
	}
	if prompt != "Use deploy tools carefully." {
		t.Fatalf("prompt = %q, want %q", prompt, "Use deploy tools carefully.")
	}
}

func TestRegisterSkillsFiltered(t *testing.T) {
	openedStore := setupSkillStore(t)
	createStoredSkillFromMarkdown(t, openedStore, "alpha", "1.0.0", `---
name: alpha
tools:
  - name: alpha_tool
    description: Alpha
    type: shell
    command: ["echo", "alpha"]
---
Alpha instructions.
`, true)
	createStoredSkillFromMarkdown(t, openedStore, "beta", "1.0.0", `---
name: beta
tools:
  - name: beta_tool
    description: Beta
    type: shell
    command: ["echo", "beta"]
---
Beta instructions.
`, true)
	createStoredSkillFromMarkdown(t, openedStore, "gamma", "1.0.0", `---
name: gamma
tools:
  - name: gamma_tool
    description: Gamma
    type: shell
    command: ["echo", "gamma"]
---
`, true)

	registry := toolregistry.NewEmptyToolRegistry()
	ctx := store.ContextWithStore(context.Background(), openedStore)
	prompt := RegisterSkills(ctx, registry, []string{"alpha", "gamma"})
	if registry.Get("alpha_tool") == nil {
		t.Fatal("alpha_tool not registered")
	}
	if registry.Get("beta_tool") != nil {
		t.Fatal("beta_tool should not be registered")
	}
	if registry.Get("gamma_tool") == nil {
		t.Fatal("gamma_tool not registered")
	}
	if prompt != "Alpha instructions." {
		t.Fatalf("prompt = %q, want %q", prompt, "Alpha instructions.")
	}
}
