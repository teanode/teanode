package skills

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
)

func createStoredSkillFromMarkdown(t *testing.T, openedStore store.Store, skillId string, version string, markdown string, enabled bool) {
	t.Helper()

	parsedDefinition := SkillDefinition{}
	promptBody, parseError := parseSkillMarkdown([]byte(markdown), &parsedDefinition)
	if parseError != nil {
		t.Fatalf("parseSkillMarkdown: %v", parseError)
	}
	toolsData, marshalToolsError := json.Marshal(parsedDefinition.Tools)
	if marshalToolsError != nil {
		t.Fatalf("marshaling tools: %v", marshalToolsError)
	}
	tools := make([]map[string]interface{}, 0)
	if unmarshalToolsError := json.Unmarshal(toolsData, &tools); unmarshalToolsError != nil {
		t.Fatalf("unmarshaling tools: %v", unmarshalToolsError)
	}
	httpAuthData, marshalHTTPAuthError := json.Marshal(parsedDefinition.HTTPAuth)
	if marshalHTTPAuthError != nil {
		t.Fatalf("marshaling httpAuth: %v", marshalHTTPAuthError)
	}
	httpAuth := map[string]interface{}{}
	if unmarshalHTTPAuthError := json.Unmarshal(httpAuthData, &httpAuth); unmarshalHTTPAuthError != nil {
		t.Fatalf("unmarshaling httpAuth: %v", unmarshalHTTPAuthError)
	}

	description := strings.TrimSpace(parsedDefinition.Description)
	metadata := map[string]interface{}{
		"description": description,
		"enabled":     enabled,
	}
	name := strings.TrimSpace(parsedDefinition.Name)
	if name == "" {
		name = skillId
	}
	prompt := strings.TrimSpace(promptBody)
	runtimeMinVersion := strings.TrimSpace(parsedDefinition.RuntimeMinVersion)

	createError := openedStore.Transaction(func(transaction store.Transaction) error {
		_, skillCreateError := transaction.CreateSkill(&models.Skill{
			ID:                skillId,
			Name:              ptrto.Value(name),
			Description:       ptrto.TrimmedString(description),
			Version:           &version,
			RuntimeMinVersion: ptrto.TrimmedString(runtimeMinVersion),
			HTTPAuth:          &httpAuth,
			Tools:             &tools,
			Enabled:           &enabled,
			Metadata:          &metadata,
			Prompt:            &prompt,
		}, nil)
		return skillCreateError
	})
	if createError != nil {
		t.Fatalf("creating skill: %v", createError)
	}
}

func TestLoadAllWithEmptyStore(t *testing.T) {
	openedStore := setupSkillStore(t)
	ctx := store.ContextWithStore(context.Background(), openedStore)
	loadedSkills, loadError := LoadAll(ctx, t.TempDir())
	if loadError != nil {
		t.Fatalf("LoadAll: %v", loadError)
	}
	if len(loadedSkills) != 0 {
		t.Fatalf("skill count = %d, want 0", len(loadedSkills))
	}
}

func TestLoadAllFromStore(t *testing.T) {
	openedStore := setupSkillStore(t)
	createStoredSkillFromMarkdown(t, openedStore, "sysinfo", "1.0.0", `---
name: sysinfo
description: System information tools
tools:
  - name: uptime
    description: Show system uptime
    type: shell
    command: ["uptime"]
---
Use these tools to inspect the system.
`, true)

	ctx := store.ContextWithStore(context.Background(), openedStore)
	loadedSkills, loadError := LoadAll(ctx, t.TempDir())
	if loadError != nil {
		t.Fatalf("LoadAll: %v", loadError)
	}
	if len(loadedSkills) != 1 {
		t.Fatalf("skill count = %d, want 1", len(loadedSkills))
	}
	if loadedSkills[0].Name != "sysinfo" {
		t.Fatalf("name = %q, want sysinfo", loadedSkills[0].Name)
	}
	if loadedSkills[0].Prompt != "Use these tools to inspect the system." {
		t.Fatalf("prompt = %q", loadedSkills[0].Prompt)
	}
}

func TestLoadAllSkipsDisabledSkills(t *testing.T) {
	openedStore := setupSkillStore(t)
	createStoredSkillFromMarkdown(t, openedStore, "weather", "1.0.0", `---
name: weather
tools:
  - name: weather_now
    description: Current weather
    type: http
    url: "https://example.com/weather"
---
`, false)

	ctx := store.ContextWithStore(context.Background(), openedStore)
	loadedSkills, loadError := LoadAll(ctx, t.TempDir())
	if loadError != nil {
		t.Fatalf("LoadAll: %v", loadError)
	}
	if len(loadedSkills) != 0 {
		t.Fatalf("skill count = %d, want 0", len(loadedSkills))
	}
}

func TestLoadAllSkipsIncompatibleRuntimeMinVersion(t *testing.T) {
	openedStore := setupSkillStore(t)
	createStoredSkillFromMarkdown(t, openedStore, "future-skill", "1.0.0", `---
name: future-skill
runtimeMinVersion: 9999.0.0
tools:
  - name: do_nothing
    description: noop
    type: shell
    command: ["echo", "ok"]
---
`, true)

	ctx := store.ContextWithStore(context.Background(), openedStore)
	loadedSkills, loadError := LoadAll(ctx, t.TempDir())
	if loadError != nil {
		t.Fatalf("LoadAll: %v", loadError)
	}
	if len(loadedSkills) != 0 {
		t.Fatalf("skill count = %d, want 0", len(loadedSkills))
	}
}

func TestLoadAllSkipsUnknownAuthProfileReference(t *testing.T) {
	openedStore := setupSkillStore(t)
	createStoredSkillFromMarkdown(t, openedStore, "auth-skill", "1.0.0", `---
name: auth-skill
tools:
  - name: get_secure
    description: secure request
    type: http
    url: https://example.com
    auth: missing_profile
---
`, true)

	ctx := store.ContextWithStore(context.Background(), openedStore)
	loadedSkills, loadError := LoadAll(ctx, t.TempDir())
	if loadError != nil {
		t.Fatalf("LoadAll: %v", loadError)
	}
	if len(loadedSkills) != 0 {
		t.Fatalf("skill count = %d, want 0", len(loadedSkills))
	}
}

func TestLoadAllFiltersInvalidTools(t *testing.T) {
	openedStore := setupSkillStore(t)
	createStoredSkillFromMarkdown(t, openedStore, "mixed", "1.0.0", `---
name: mixed
tools:
  - name: good
    description: valid tool
    type: shell
    command: ["echo", "hello"]
  - name: bad_shell
    description: missing command
    type: shell
---
Prompt text.
`, true)

	ctx := store.ContextWithStore(context.Background(), openedStore)
	loadedSkills, loadError := LoadAll(ctx, t.TempDir())
	if loadError != nil {
		t.Fatalf("LoadAll: %v", loadError)
	}
	if len(loadedSkills) != 1 {
		t.Fatalf("skill count = %d, want 1", len(loadedSkills))
	}
	if len(loadedSkills[0].Tools) != 1 {
		t.Fatalf("tool count = %d, want 1", len(loadedSkills[0].Tools))
	}
	if loadedSkills[0].Tools[0].Name != "good" {
		t.Fatalf("tool name = %q, want good", loadedSkills[0].Tools[0].Name)
	}
}
