package skills

import (
	"context"
	"sort"
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

	registry := toolregistry.NewToolRegistry()
	ctx := store.ContextWithStore(context.Background(), openedStore)
	prompt := RegisterSkills(ctx, registry)

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

	registry := toolregistry.NewToolRegistry()
	ctx := store.ContextWithStore(context.Background(), openedStore)
	prompt := RegisterSkillsFiltered(ctx, registry, []string{"alpha", "gamma"})
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

func TestNames(t *testing.T) {
	openedStore := setupSkillStore(t)
	createStoredSkillFromMarkdown(t, openedStore, "deploy", "1.0.0", `---
name: deploy
tools:
  - name: deploy_tool
    description: Deploy
    type: shell
    command: ["echo"]
---
`, true)
	createStoredSkillFromMarkdown(t, openedStore, "monitor", "1.0.0", `---
name: monitor
tools:
  - name: monitor_tool
    description: Monitor
    type: http
    url: "http://example.com"
---
`, true)

	ctx := store.ContextWithStore(context.Background(), openedStore)
	names := Names(ctx)
	sort.Strings(names)
	if len(names) != 2 {
		t.Fatalf("name count = %d, want 2", len(names))
	}
	if names[0] != "deploy" || names[1] != "monitor" {
		t.Fatalf("names = %v, want [deploy monitor]", names)
	}
}
