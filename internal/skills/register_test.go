package skills

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/teanode/teanode/internal/agents"
	"github.com/teanode/teanode/internal/configs"
)

func TestRegisterSkills(t *testing.T) {
	directory := t.TempDir()
	content := `---
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
`
	os.WriteFile(filepath.Join(directory, "deploy.md"), []byte(content), 0644)

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
	skill1 := `---
name: alpha
tools:
  - name: alpha_tool
    description: Alpha
    type: shell
    command: ["echo", "alpha"]
---
Alpha instructions.
`
	skill2 := `---
name: beta
tools:
  - name: beta_tool
    description: Beta
    type: shell
    command: ["echo", "beta"]
---
Beta instructions.
`
	skill3 := `---
name: gamma
tools:
  - name: gamma_tool
    description: Gamma
    type: shell
    command: ["echo", "gamma"]
---
`
	os.WriteFile(filepath.Join(directory, "alpha.md"), []byte(skill1), 0644)
	os.WriteFile(filepath.Join(directory, "beta.md"), []byte(skill2), 0644)
	os.WriteFile(filepath.Join(directory, "gamma.md"), []byte(skill3), 0644)

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

	t.Run("empty allow list loads all (preserves defaults)", func(t *testing.T) {
		registry := agents.NewToolRegistry()
		RegisterSkillsFiltered(registry, directory, []string{})

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
	skill1 := `---
name: deploy
tools:
  - name: deploy_tool
    description: Deploy
    type: shell
    command: ["echo"]
---
`
	skill2 := `---
name: monitor
tools:
  - name: monitor_tool
    description: Monitor
    type: http
    url: "http://example.com"
---
`
	os.WriteFile(filepath.Join(directory, "deploy.md"), []byte(skill1), 0644)
	os.WriteFile(filepath.Join(directory, "monitor.md"), []byte(skill2), 0644)

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

func TestRegisterSkillsFilteredLocalSkillTakesPrecedenceOverInstalled(t *testing.T) {
	directory := t.TempDir()

	localEnabled := `---
name: overlap-enabled
tools:
  - name: local_enabled_tool
    description: local
    type: shell
    command: ["echo", "local"]
---`
	localDefault := `---
name: overlap-default
tools:
  - name: local_default_tool
    description: local
    type: shell
    command: ["echo", "local"]
---`
	installedEnabled := `---
name: overlap-enabled
tools:
  - name: installed_enabled_tool
    description: installed
    type: shell
    command: ["echo", "installed"]
---`
	installedDefault := `---
name: overlap-default
tools:
  - name: installed_default_tool
    description: installed
    type: shell
    command: ["echo", "installed"]
---`

	_ = os.WriteFile(filepath.Join(directory, "overlap-enabled.md"), []byte(localEnabled), 0644)
	_ = os.WriteFile(filepath.Join(directory, "overlap-default.md"), []byte(localDefault), 0644)
	_ = os.MkdirAll(filepath.Join(directory, "skills", ".installed", "overlap-enabled", "1.0.0"), 0755)
	_ = os.MkdirAll(filepath.Join(directory, "skills", ".installed", "overlap-default", "1.0.0"), 0755)
	_ = os.WriteFile(filepath.Join(directory, "skills", ".installed", "overlap-enabled", "1.0.0", "skill.md"), []byte(installedEnabled), 0644)
	_ = os.WriteFile(filepath.Join(directory, "skills", ".installed", "overlap-default", "1.0.0", "skill.md"), []byte(installedDefault), 0644)

	registry := agents.NewToolRegistry()
	RegisterSkillsFiltered(registry, directory, nil)

	if registry.Get("local_enabled_tool") == nil {
		t.Fatal("local overlap-enabled skill should be registered")
	}
	if registry.Get("local_default_tool") == nil {
		t.Fatal("local overlap-default skill should be registered")
	}
	if registry.Get("installed_enabled_tool") != nil {
		t.Fatal("installed overlap-enabled skill should not register when local skill with same name exists")
	}
	if registry.Get("installed_default_tool") != nil {
		t.Fatal("installed overlap-default skill should not register when local skill with same name exists")
	}
}

func TestRegisterSkillsFilteredInstalledSkillRespectsSetEnabled(t *testing.T) {
	directory := t.TempDir()
	configs.SetDirectory(directory)
	t.Cleanup(func() { configs.SetDirectory("") })

	skillsDirectory := filepath.Join(directory, "skills")
	installDirectory := filepath.Join(skillsDirectory, ".installed", "weather", "1.0.0")
	if err := os.MkdirAll(installDirectory, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	installed := `---
name: weather
tools:
  - name: get_weather
    description: weather
    type: shell
    command: ["echo", "weather"]
---
Weather prompt
`
	if err := os.WriteFile(filepath.Join(installDirectory, "skill.md"), []byte(installed), 0644); err != nil {
		t.Fatalf("skill write: %v", err)
	}

	manifest := installManifest{Name: "weather", Version: "1.0.0"}
	manifestBytes, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(installDirectory, "manifest.json"), manifestBytes, 0644); err != nil {
		t.Fatalf("manifest write: %v", err)
	}

	registry := agents.NewToolRegistry()
	prompt := RegisterSkillsFiltered(registry, skillsDirectory, nil)
	if registry.Get("get_weather") == nil {
		t.Fatal("installed skill tool should be registered before disable")
	}
	if !strings.Contains(prompt, "Weather prompt") {
		t.Fatalf("prompt missing installed skill text: %q", prompt)
	}

	if err := SetInstalledSkillEnabled("weather", false); err != nil {
		t.Fatalf("SetInstalledSkillEnabled(false): %v", err)
	}

	registry = agents.NewToolRegistry()
	prompt = RegisterSkillsFiltered(registry, skillsDirectory, nil)
	if registry.Get("get_weather") != nil {
		t.Fatal("installed skill tool should not be registered when disabled")
	}
	if prompt != "" {
		t.Fatalf("prompt = %q, want empty when installed skill disabled", prompt)
	}

	if err := SetInstalledSkillEnabled("weather", true); err != nil {
		t.Fatalf("SetInstalledSkillEnabled(true): %v", err)
	}

	registry = agents.NewToolRegistry()
	prompt = RegisterSkillsFiltered(registry, skillsDirectory, nil)
	if registry.Get("get_weather") == nil {
		t.Fatal("installed skill tool should be registered after re-enable")
	}
	if !strings.Contains(prompt, "Weather prompt") {
		t.Fatalf("prompt missing installed skill text after re-enable: %q", prompt)
	}
}
