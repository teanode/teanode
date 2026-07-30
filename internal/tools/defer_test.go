package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
)

type describedTool struct {
	name        string
	description string
}

func (self *describedTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name:        self.name,
			Description: self.description,
		},
	}
}

func (self *describedTool) PolicyGroups() []PolicyGroup {
	return []PolicyGroup{
		{Group: models.ToolPolicyGroupAll, Default: models.ToolPolicyAnyone},
	}
}

func (self *describedTool) Execute(_ context.Context, _ string) (string, error) {
	return "ok", nil
}

func definitionNames(definitions []providers.ToolDefinition) []string {
	names := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		names = append(names, definition.Function.Name)
	}
	return names
}

func TestDeferWithholdsDefinitionsButKeepsToolsCallable(t *testing.T) {
	registry := newTestRegistry()
	registry.Defer([]string{"alpha"})

	if got := definitionNames(registry.Definitions()); len(got) != 1 || got[0] != "alpha" {
		t.Errorf("Definitions() = %v, want [alpha]", got)
	}
	if got := registry.LoadedNames(); len(got) != 1 || got[0] != "alpha" {
		t.Errorf("LoadedNames() = %v, want [alpha]", got)
	}
	// Names still reports everything: a deferred tool remains registered and
	// callable once activated.
	if got := registry.Names(); len(got) != 3 {
		t.Errorf("Names() = %v, want all three tools", got)
	}
	if registry.Get("beta") == nil {
		t.Error("deferred tool should still resolve through Get")
	}

	catalog := registry.DeferredCatalog()
	if len(catalog) != 2 || catalog[0].Name != "beta" || catalog[1].Name != "gamma" {
		t.Errorf("DeferredCatalog() = %v, want beta and gamma in sorted order", catalog)
	}
}

func TestActivateLoadsDeferredDefinitions(t *testing.T) {
	registry := newTestRegistry()
	registry.Defer([]string{"alpha"})

	activated := registry.Activate([]string{"gamma", "unknown", "alpha"})
	if len(activated) != 1 || activated[0] != "gamma" {
		t.Fatalf("Activate() = %v, want [gamma]: unknown and already-loaded names are ignored", activated)
	}

	if got := definitionNames(registry.Definitions()); len(got) != 2 || got[0] != "alpha" || got[1] != "gamma" {
		t.Errorf("Definitions() = %v, want [alpha gamma]", got)
	}
	if catalog := registry.DeferredCatalog(); len(catalog) != 1 || catalog[0].Name != "beta" {
		t.Errorf("DeferredCatalog() = %v, want only beta to remain", catalog)
	}
	if second := registry.Activate([]string{"gamma"}); len(second) != 0 {
		t.Errorf("re-activating gamma returned %v, want nothing newly activated", second)
	}
}

func TestDeferResetsPreviousActivation(t *testing.T) {
	registry := newTestRegistry()
	registry.Defer([]string{"alpha"})
	registry.Activate([]string{"beta"})
	registry.Defer([]string{"alpha"})

	if got := definitionNames(registry.Definitions()); len(got) != 1 || got[0] != "alpha" {
		t.Errorf("Definitions() = %v, want [alpha] after re-deferring", got)
	}
}

func TestRemoveClearsDeferralState(t *testing.T) {
	registry := newTestRegistry()
	registry.Defer([]string{"alpha"})
	registry.Remove("beta")

	if catalog := registry.DeferredCatalog(); len(catalog) != 1 || catalog[0].Name != "gamma" {
		t.Errorf("DeferredCatalog() = %v, want only gamma", catalog)
	}
	if activated := registry.Activate([]string{"beta"}); len(activated) != 0 {
		t.Errorf("Activate() = %v, want nothing for a removed tool", activated)
	}
}

func TestApplyFilterRemovesDeferredTools(t *testing.T) {
	registry := newTestRegistry()
	registry.Defer([]string{"alpha"})
	registry.ApplyFilter([]string{"alpha", "beta"})

	if catalog := registry.DeferredCatalog(); len(catalog) != 1 || catalog[0].Name != "beta" {
		t.Errorf("DeferredCatalog() = %v, want only beta after filtering gamma out", catalog)
	}
}

func TestDeferredCatalogSummarizesDescriptions(t *testing.T) {
	registry := NewEmptyToolRegistry()
	registry.Register(&describedTool{
		name:        "short",
		description: "Do one thing. Then a second sentence that should be dropped.",
	})
	registry.Register(&describedTool{
		name:        "long",
		description: strings.Repeat("word ", 60),
	})
	registry.Defer(nil)

	catalog := registry.DeferredCatalog()
	summaries := map[string]string{}
	for _, entry := range catalog {
		summaries[entry.Name] = entry.Summary
	}
	if summaries["short"] != "Do one thing." {
		t.Errorf("short summary = %q, want only the first sentence", summaries["short"])
	}
	if len(summaries["long"]) > maxCatalogSummaryCharacters+3 {
		t.Errorf("long summary is %d characters, want it truncated near %d", len(summaries["long"]), maxCatalogSummaryCharacters)
	}
	if !strings.HasSuffix(summaries["long"], "...") {
		t.Errorf("long summary = %q, want a truncation marker", summaries["long"])
	}
}

func TestNilRegistryIsSafeToInspect(t *testing.T) {
	var registry *ToolRegistry
	if got := registry.Names(); got != nil {
		t.Errorf("Names() = %v, want nil", got)
	}
	if got := registry.LoadedNames(); got != nil {
		t.Errorf("LoadedNames() = %v, want nil", got)
	}
	if got := registry.Definitions(); got != nil {
		t.Errorf("Definitions() = %v, want nil", got)
	}
	if got := registry.DeferredCatalog(); got != nil {
		t.Errorf("DeferredCatalog() = %v, want nil", got)
	}
}
