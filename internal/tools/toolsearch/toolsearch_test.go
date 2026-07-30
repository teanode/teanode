package toolsearch

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/tools"
)

type stubTool struct {
	name        string
	description string
}

func (self *stubTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name:        self.name,
			Description: self.description,
		},
	}
}

func (self *stubTool) PolicyGroups() []tools.PolicyGroup {
	return []tools.PolicyGroup{
		{Group: models.ToolPolicyGroupAll, Default: models.ToolPolicyAnyone},
	}
}

func (self *stubTool) Execute(_ context.Context, _ string) (string, error) {
	return "ok", nil
}

func newTestRegistry(coreTools ...string) *tools.ToolRegistry {
	registry := tools.NewEmptyToolRegistry()
	registry.Register(&stubTool{name: "core", description: "Always loaded."})
	registry.Register(&stubTool{name: "gitlab_issues", description: "Interact with GitLab issues."})
	registry.Register(&stubTool{name: "gitlab_merge_requests", description: "Interact with GitLab merge requests."})
	registry.Register(&stubTool{name: "google_calendar", description: "Interact with Google Calendar."})
	registry.Register(&stubTool{name: "home_assistant", description: "Interact with Home Assistant smart home."})
	registry.Register(New(registry))
	registry.Defer(append([]string{ToolName}, coreTools...))
	return registry
}

func execute(t *testing.T, registry *tools.ToolRegistry, arguments string) searchResult {
	t.Helper()
	tool := New(registry)
	raw, err := tool.Execute(context.Background(), arguments)
	if err != nil {
		t.Fatalf("Execute(%s): %v", arguments, err)
	}
	var result searchResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("decoding result %q: %v", raw, err)
	}
	return result
}

func TestExecuteLoadsToolsByExactName(t *testing.T) {
	registry := newTestRegistry("core")

	result := execute(t, registry, `{"names":["gitlab_issues"]}`)
	if len(result.Loaded) != 1 || result.Loaded[0] != "gitlab_issues" {
		t.Fatalf("Loaded = %v, want [gitlab_issues]", result.Loaded)
	}

	loaded := registry.LoadedNames()
	found := false
	for _, name := range loaded {
		if name == "gitlab_issues" {
			found = true
		}
	}
	if !found {
		t.Errorf("LoadedNames() = %v, want gitlab_issues to be loaded", loaded)
	}
	if result.Remaining != 3 {
		t.Errorf("Remaining = %d, want 3", result.Remaining)
	}
}

func TestExecuteRanksNameMatchesAboveSummaryMatches(t *testing.T) {
	registry := newTestRegistry("core")

	result := execute(t, registry, `{"query":"gitlab"}`)
	if len(result.Loaded) != 2 {
		t.Fatalf("Loaded = %v, want both gitlab tools", result.Loaded)
	}
	if result.Matched[0].Name != "gitlab_issues" && result.Matched[0].Name != "gitlab_merge_requests" {
		t.Errorf("Matched[0] = %q, want a gitlab tool first", result.Matched[0].Name)
	}
}

func TestExecuteMatchesSummaryText(t *testing.T) {
	registry := newTestRegistry("core")

	result := execute(t, registry, `{"query":"smart home"}`)
	if len(result.Loaded) != 1 || result.Loaded[0] != "home_assistant" {
		t.Fatalf("Loaded = %v, want [home_assistant]", result.Loaded)
	}
}

func TestExecuteCapsHowManyToolsOneCallLoads(t *testing.T) {
	registry := tools.NewEmptyToolRegistry()
	for index := 0; index < maxActivationsPerCall+3; index++ {
		registry.Register(&stubTool{
			name:        fmt.Sprintf("interact_%d", index),
			description: "Interact with a remote service.",
		})
	}
	registry.Register(New(registry))
	registry.Defer([]string{ToolName})

	result := execute(t, registry, `{"query":"interact"}`)
	if len(result.Loaded) != maxActivationsPerCall {
		t.Fatalf("Loaded %d tools, want the cap of %d", len(result.Loaded), maxActivationsPerCall)
	}
	if result.Remaining != 3 {
		t.Errorf("Remaining = %d, want 3", result.Remaining)
	}
}

func TestExecuteReportsNoMatch(t *testing.T) {
	registry := newTestRegistry("core")

	result := execute(t, registry, `{"query":"nonexistent"}`)
	if len(result.Loaded) != 0 {
		t.Fatalf("Loaded = %v, want nothing", result.Loaded)
	}
	if result.Message == "" {
		t.Error("Message should explain that nothing matched")
	}
}

func TestExecuteWithoutArgumentsLoadsNothing(t *testing.T) {
	registry := newTestRegistry("core")

	result := execute(t, registry, "")
	if len(result.Loaded) != 0 {
		t.Fatalf("Loaded = %v, want nothing for an empty request", result.Loaded)
	}
}

func TestExecuteRejectsMalformedArguments(t *testing.T) {
	tool := New(newTestRegistry("core"))
	if _, err := tool.Execute(context.Background(), "{not json"); err == nil {
		t.Error("Execute should fail on malformed arguments")
	}
}
