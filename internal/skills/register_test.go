package skills

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/teanode/teanode/internal/agents"
)

func TestRegisterSkills(t *testing.T) {
	directory := t.TempDir()
	content := `
name: deploy
prompt: Use deploy tools carefully.
tools:
  - name: deploy_status
    description: Check deployment status
    type: shell
    command: ["echo", "deployed"]
  - name: deploy_api
    description: Call deploy API
    type: http
    url: "http://example.com/deploy"
`
	os.WriteFile(filepath.Join(directory, "deploy.yaml"), []byte(content), 0644)

	registry := agents.NewToolRegistry()
	prompt := RegisterSkills(registry, directory)

	// Both tools should be registered.
	if registry.Get("deploy_status") == nil {
		t.Error("deploy_status not registered")
	}
	if registry.Get("deploy_api") == nil {
		t.Error("deploy_api not registered")
	}

	// Prompt should be returned.
	if prompt != "Use deploy tools carefully." {
		t.Errorf("prompt = %q, want %q", prompt, "Use deploy tools carefully.")
	}
}

func TestRegisterSkillsFiltered(t *testing.T) {
	directory := t.TempDir()
	skill1 := `
name: alpha
prompt: Alpha instructions.
tools:
  - name: alpha_tool
    description: Alpha
    type: shell
    command: ["echo", "alpha"]
`
	skill2 := `
name: beta
prompt: Beta instructions.
tools:
  - name: beta_tool
    description: Beta
    type: shell
    command: ["echo", "beta"]
`
	skill3 := `
name: gamma
tools:
  - name: gamma_tool
    description: Gamma
    type: shell
    command: ["echo", "gamma"]
`
	os.WriteFile(filepath.Join(directory, "alpha.yaml"), []byte(skill1), 0644)
	os.WriteFile(filepath.Join(directory, "beta.yaml"), []byte(skill2), 0644)
	os.WriteFile(filepath.Join(directory, "gamma.yaml"), []byte(skill3), 0644)

	t.Run("nil allow list loads all", func(t *testing.T) {
		registry := agents.NewToolRegistry()
		RegisterSkillsFiltered(registry, directory, nil)

		if registry.Get("alpha_tool") == nil {
			t.Error("alpha_tool not registered")
		}
		if registry.Get("beta_tool") == nil {
			t.Error("beta_tool not registered")
		}
		if registry.Get("gamma_tool") == nil {
			t.Error("gamma_tool not registered")
		}
	})

	t.Run("filter to subset", func(t *testing.T) {
		registry := agents.NewToolRegistry()
		RegisterSkillsFiltered(registry, directory, []string{"alpha", "gamma"})

		if registry.Get("alpha_tool") == nil {
			t.Error("alpha_tool not registered")
		}
		if registry.Get("beta_tool") != nil {
			t.Error("beta_tool should not be registered")
		}
		if registry.Get("gamma_tool") == nil {
			t.Error("gamma_tool not registered")
		}
	})

	t.Run("prompts combined from allowed skills", func(t *testing.T) {
		registry := agents.NewToolRegistry()
		prompt := RegisterSkillsFiltered(registry, directory, []string{"alpha", "beta"})

		if !strings.Contains(prompt, "Alpha instructions.") {
			t.Errorf("prompt missing alpha instructions: %q", prompt)
		}
		if !strings.Contains(prompt, "Beta instructions.") {
			t.Errorf("prompt missing beta instructions: %q", prompt)
		}
	})

	t.Run("skills without prompt contribute nothing", func(t *testing.T) {
		registry := agents.NewToolRegistry()
		prompt := RegisterSkillsFiltered(registry, directory, []string{"gamma"})

		if prompt != "" {
			t.Errorf("prompt = %q, want empty for skill without prompt", prompt)
		}
	})
}

func TestRegisterSkillsNonExistentDirectory(t *testing.T) {
	registry := agents.NewToolRegistry()
	prompt := RegisterSkills(registry, "/nonexistent/skills/dir")

	if prompt != "" {
		t.Errorf("prompt = %q, want empty for missing directory", prompt)
	}
	if len(registry.Names()) != 0 {
		t.Errorf("expected no tools registered, got %v", registry.Names())
	}
}

func TestNames(t *testing.T) {
	directory := t.TempDir()
	skill1 := `
name: deploy
tools:
  - name: deploy_tool
    description: Deploy
    type: shell
    command: ["echo"]
`
	skill2 := `
name: monitor
tools:
  - name: monitor_tool
    description: Monitor
    type: http
    url: "http://example.com"
`
	os.WriteFile(filepath.Join(directory, "deploy.yaml"), []byte(skill1), 0644)
	os.WriteFile(filepath.Join(directory, "monitor.yaml"), []byte(skill2), 0644)

	names := Names(directory)
	if len(names) != 2 {
		t.Fatalf("expected 2 names, got %d", len(names))
	}

	sort.Strings(names)
	if names[0] != "deploy" || names[1] != "monitor" {
		t.Errorf("names = %v, want [deploy, monitor]", names)
	}
}

func TestNamesNonExistentDirectory(t *testing.T) {
	names := Names("/nonexistent/skills/dir")
	if len(names) != 0 {
		t.Errorf("expected empty names for missing directory, got %v", names)
	}
}

func TestNamesEmptyDirectory(t *testing.T) {
	directory := t.TempDir()
	names := Names(directory)
	if len(names) != 0 {
		t.Errorf("expected empty names for empty directory, got %v", names)
	}
}
