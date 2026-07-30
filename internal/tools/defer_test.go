package tools

import (
	"context"
	"fmt"
	"strings"
	"sync"
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

func TestIsLoadedTracksDeferralAndActivation(t *testing.T) {
	registry := newTestRegistry()
	if !registry.IsLoaded("beta") {
		t.Error("a tool is loaded until something defers it")
	}

	registry.Defer([]string{"alpha"})
	if !registry.IsLoaded("alpha") {
		t.Error("a core tool stays loaded")
	}
	if registry.IsLoaded("beta") {
		t.Error("a deferred tool is not loaded, so the model has not seen its schema")
	}

	registry.Activate([]string{"beta"})
	if !registry.IsLoaded("beta") {
		t.Error("an activated tool is loaded")
	}
	if registry.IsLoaded("missing") {
		t.Error("an unknown tool is not loaded")
	}
}

// TestConcurrentAccessIsRaceFree exercises the registry the way a run does:
// the runner reads definitions between rounds while tool_search activates
// deferred tools from inside tool execution. Run with -race.
func TestConcurrentAccessIsRaceFree(t *testing.T) {
	registry := NewEmptyToolRegistry()
	names := make([]string, 0, 32)
	for index := 0; index < 32; index++ {
		name := fmt.Sprintf("tool_%02d", index)
		names = append(names, name)
		registry.Register(&describedTool{name: name, description: "A tool."})
	}
	registry.Defer(names[:4])

	var waitGroup sync.WaitGroup
	for _, name := range names[4:] {
		waitGroup.Add(1)
		go func(toolName string) {
			defer waitGroup.Done()
			registry.Activate([]string{toolName})
		}(name)
	}
	for index := 0; index < 16; index++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			registry.Definitions()
			registry.LoadedNames()
			registry.DeferredCatalog()
			registry.Names()
			registry.Get("tool_00")
			registry.IsLoaded("tool_10")
			registry.ToolActionGroups()
		}()
	}
	waitGroup.Wait()

	if got := len(registry.LoadedNames()); got != len(names) {
		t.Errorf("LoadedNames() has %d tools, want all %d activated", got, len(names))
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
