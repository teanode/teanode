package tools

import (
	"context"
	"sort"
	"testing"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
)

type stubTool struct{ name string }

func (self *stubTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type:     "function",
		Function: providers.FunctionSpec{Name: self.name},
	}
}

func (self *stubTool) PolicyGroups() []PolicyGroup {
	return []PolicyGroup{
		{Group: models.ToolPolicyGroupAll, Default: models.ToolPolicyAnyone},
	}
}

func (self *stubTool) Execute(_ context.Context, _ string) (string, error) {
	return "ok", nil
}

func newTestRegistry() *ToolRegistry {
	registry := NewEmptyToolRegistry()
	registry.Register(&stubTool{name: "alpha"})
	registry.Register(&stubTool{name: "beta"})
	registry.Register(&stubTool{name: "gamma"})
	return registry
}

func sortedNames(registry *ToolRegistry) []string {
	names := registry.Names()
	sort.Strings(names)
	return names
}

func TestApplyFilter_NilKeepsAll(t *testing.T) {
	registry := newTestRegistry()
	registry.ApplyFilter(nil)

	names := sortedNames(registry)
	if len(names) != 3 {
		t.Fatalf("expected 3 tools, got %d: %v", len(names), names)
	}
}

func TestApplyFilter_EmptySliceKeepsAll(t *testing.T) {
	registry := newTestRegistry()
	registry.ApplyFilter([]string{})

	names := sortedNames(registry)
	if len(names) != 3 {
		t.Fatalf("expected 3 tools (empty slice preserves defaults), got %d: %v", len(names), names)
	}
}

func TestApplyFilter_ExplicitSubset(t *testing.T) {
	registry := newTestRegistry()
	registry.ApplyFilter([]string{"alpha", "gamma"})

	names := sortedNames(registry)
	if len(names) != 2 {
		t.Fatalf("expected 2 tools, got %d: %v", len(names), names)
	}
	if names[0] != "alpha" || names[1] != "gamma" {
		t.Errorf("expected [alpha gamma], got %v", names)
	}
	if registry.Get("beta") != nil {
		t.Error("beta should have been filtered out")
	}
}

func TestApplyFilter_SingleTool(t *testing.T) {
	registry := newTestRegistry()
	registry.ApplyFilter([]string{"beta"})

	names := sortedNames(registry)
	if len(names) != 1 || names[0] != "beta" {
		t.Errorf("expected [beta], got %v", names)
	}
}

func TestApplyFilter_NoMatchRemovesAll(t *testing.T) {
	registry := newTestRegistry()
	registry.ApplyFilter([]string{"nonexistent"})

	if len(registry.Names()) != 0 {
		t.Errorf("expected 0 tools when filter matches nothing, got %v", registry.Names())
	}
}

func TestDefinitions_MatchesRegisteredTools(t *testing.T) {
	registry := newTestRegistry()
	definitions := registry.Definitions()

	if len(definitions) != 3 {
		t.Fatalf("expected 3 definitions, got %d", len(definitions))
	}

	names := make([]string, len(definitions))
	for index, definition := range definitions {
		names[index] = definition.Function.Name
	}
	sort.Strings(names)
	if names[0] != "alpha" || names[1] != "beta" || names[2] != "gamma" {
		t.Errorf("expected [alpha beta gamma], got %v", names)
	}
}

func TestDefinitions_StableOrder(t *testing.T) {
	registry := newTestRegistry()

	// Call Definitions multiple times and verify order is stable.
	for run := 0; run < 10; run++ {
		definitions := registry.Definitions()
		if len(definitions) != 3 {
			t.Fatalf("run %d: expected 3 definitions, got %d", run, len(definitions))
		}
		if definitions[0].Function.Name != "alpha" {
			t.Errorf("run %d: definitions[0] = %q, want alpha", run, definitions[0].Function.Name)
		}
		if definitions[1].Function.Name != "beta" {
			t.Errorf("run %d: definitions[1] = %q, want beta", run, definitions[1].Function.Name)
		}
		if definitions[2].Function.Name != "gamma" {
			t.Errorf("run %d: definitions[2] = %q, want gamma", run, definitions[2].Function.Name)
		}
	}
}

func TestNames_Sorted(t *testing.T) {
	registry := newTestRegistry()
	names := registry.Names()

	if len(names) != 3 {
		t.Fatalf("expected 3 names, got %d", len(names))
	}
	if names[0] != "alpha" || names[1] != "beta" || names[2] != "gamma" {
		t.Errorf("expected [alpha beta gamma], got %v", names)
	}
}
