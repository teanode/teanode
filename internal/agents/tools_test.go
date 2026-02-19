package agents

import (
	"sort"
	"testing"
)

// newTestRegistry returns a registry with three stub tools: "alpha", "beta", "gamma".
// Reuses stubTool from runner_test.go.
func newTestRegistry() *ToolRegistry {
	registry := NewToolRegistry()
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
	for i, d := range definitions {
		names[i] = d.Function.Name
	}
	sort.Strings(names)
	if names[0] != "alpha" || names[1] != "beta" || names[2] != "gamma" {
		t.Errorf("expected [alpha beta gamma], got %v", names)
	}
}
