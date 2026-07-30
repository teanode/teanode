package runners

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/tools"
	"github.com/teanode/teanode/internal/tools/toolsearch"
	"github.com/teanode/teanode/internal/util/ptrto"
)

// The deferral tests register stub tools whose definitions are negligible, so
// the static prefix is essentially the system prompt (~1,400 tokens). These
// windows put that prefix either side of the 25% budget.
const (
	smallContextWindow = 4000
	largeContextWindow = 1000000
)

// stubRegistryTool is a minimal registry entry for deferral tests.
type stubRegistryTool struct{ name string }

func (self *stubRegistryTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name:        self.name,
			Description: "A stub tool.",
		},
	}
}

func (self *stubRegistryTool) PolicyGroups() []tools.PolicyGroup {
	return []tools.PolicyGroup{
		{Group: models.ToolPolicyGroupAll, Default: models.ToolPolicyAnyone},
	}
}

func (self *stubRegistryTool) Execute(_ context.Context, _ string) (string, error) {
	return "ok", nil
}

func TestResolvePromptProfile(t *testing.T) {
	cases := []struct {
		name               string
		contextWindow      int
		staticPrefixTokens int
		want               PromptProfile
	}{
		{name: "unknown window", contextWindow: 0, staticPrefixTokens: 22000, want: PromptProfileFull},
		{name: "unknown prefix", contextWindow: 8192, staticPrefixTokens: 0, want: PromptProfileFull},
		{name: "every tool on a small window", contextWindow: 8192, staticPrefixTokens: 22000, want: PromptProfileCompact},
		// The case that motivated the budget rule: a local model at 40k with
		// every integration registered spends 55% of its window before the
		// conversation starts.
		{name: "every tool on a 40k window", contextWindow: 40000, staticPrefixTokens: 22209, want: PromptProfileCompact},
		// A node with only a few tools has room to spare at the same window.
		{name: "few tools on a 40k window", contextWindow: 40000, staticPrefixTokens: 5000, want: PromptProfileFull},
		{name: "exactly at the budget", contextWindow: 40000, staticPrefixTokens: 10000, want: PromptProfileFull},
		{name: "one token over the budget", contextWindow: 40000, staticPrefixTokens: 10001, want: PromptProfileCompact},
		{name: "every tool on a large window", contextWindow: 200000, staticPrefixTokens: 22209, want: PromptProfileFull},
	}
	for _, testCase := range cases {
		got := resolvePromptProfile(testCase.contextWindow, testCase.staticPrefixTokens)
		if got != testCase.want {
			t.Errorf("%s: resolvePromptProfile(%d, %d) = %q, want %q",
				testCase.name, testCase.contextWindow, testCase.staticPrefixTokens, got, testCase.want)
		}
	}
}

func TestWorkspaceFileCharacters(t *testing.T) {
	if got := workspaceFileCharacters(PromptProfileFull); got != fullWorkspaceFileCharacters {
		t.Errorf("full profile cap = %d, want %d", got, fullWorkspaceFileCharacters)
	}
	if got := workspaceFileCharacters(PromptProfileCompact); got != compactWorkspaceFileCharacters {
		t.Errorf("compact profile cap = %d, want %d", got, compactWorkspaceFileCharacters)
	}
}

func TestStripToolReturns(t *testing.T) {
	definitions := []providers.ToolDefinition{{
		Type: "function",
		Function: providers.FunctionSpec{
			Name:        "example",
			Description: "An example.",
			Parameters:  map[string]interface{}{"type": "object"},
			Returns:     map[string]interface{}{"type": "string"},
		},
	}}

	stripped := stripToolReturns(definitions)
	if stripped[0].Function.Returns != nil {
		t.Error("stripToolReturns should clear the returns schema")
	}
	if stripped[0].Function.Parameters == nil || stripped[0].Function.Name != "example" {
		t.Error("stripToolReturns should leave the rest of the definition intact")
	}
	if definitions[0].Function.Returns == nil {
		t.Error("stripToolReturns should not modify the input definitions")
	}
}

func TestBuildSystemPromptOmitsGuidanceForUnregisteredTools(t *testing.T) {
	ctx, _ := newSystemPromptTestContext(t, "user-1", "main")

	withTools := buildSystemPrompt(ctx, buildSystemPromptParameters{
		AgentID:   "main",
		Mode:      SystemPromptModeFull,
		ToolNames: []string{"user_memory", "tab"},
	})
	if !strings.Contains(withTools, "Memory and Workspace") {
		t.Error("prompt should keep the memory section when a memory tool is registered")
	}
	if !strings.Contains(withTools, "## Browser") {
		t.Error("prompt should keep the browser section when the tab tool is registered")
	}

	withoutTools := buildSystemPrompt(ctx, buildSystemPromptParameters{
		AgentID:   "main",
		Mode:      SystemPromptModeFull,
		ToolNames: []string{"shell"},
	})
	if strings.Contains(withoutTools, "Memory and Workspace") {
		t.Error("prompt should drop the memory section when no memory tool is registered")
	}
	if strings.Contains(withoutTools, "## Browser") {
		t.Error("prompt should drop the browser section when no browser tool is registered")
	}
}

func TestBuildSystemPromptKeepsEverySectionWhenToolSetIsUnknown(t *testing.T) {
	ctx, _ := newSystemPromptTestContext(t, "user-1", "main")

	prompt := buildSystemPrompt(ctx, buildSystemPromptParameters{
		AgentID: "main",
		Mode:    SystemPromptModeFull,
	})
	if !strings.Contains(prompt, "Memory and Workspace") || !strings.Contains(prompt, "## Browser") {
		t.Error("a prompt built without a tool set should keep all tool sections")
	}
}

func TestBuildSystemPromptListsDeferredTools(t *testing.T) {
	ctx, _ := newSystemPromptTestContext(t, "user-1", "main")

	prompt := buildSystemPrompt(ctx, buildSystemPromptParameters{
		AgentID:   "main",
		Mode:      SystemPromptModeFull,
		Profile:   PromptProfileCompact,
		ToolNames: []string{"shell"},
		DeferredCatalog: []tools.CatalogEntry{
			{Name: "gitlab_issues", Summary: "Interact with GitLab issues."},
		},
	})
	if !strings.Contains(prompt, "## Additional Tools") {
		t.Error("prompt should include the deferred tool catalog section")
	}
	if !strings.Contains(prompt, "- gitlab_issues: Interact with GitLab issues.") {
		t.Error("prompt should list the deferred tool with its summary")
	}
	if !strings.Contains(prompt, "tool_search") {
		t.Error("prompt should tell the model how to load a deferred tool")
	}
}

func TestBuildSystemPromptCompactProfileIsShorter(t *testing.T) {
	ctx, _ := newSystemPromptTestContext(t, "user-1", "main")

	full := buildSystemPrompt(ctx, buildSystemPromptParameters{
		AgentID: "main",
		Mode:    SystemPromptModeFull,
		Profile: PromptProfileFull,
	})
	compact := buildSystemPrompt(ctx, buildSystemPromptParameters{
		AgentID: "main",
		Mode:    SystemPromptModeFull,
		Profile: PromptProfileCompact,
	})
	if len(compact) >= len(full) {
		t.Errorf("compact prompt is %d characters, want less than the full prompt's %d", len(compact), len(full))
	}
	// The compact profile trims guidance, it does not drop whole features.
	for _, marker := range []string{"Safety", "Charts", "Artifacts", "Suggested Replies"} {
		if !strings.Contains(compact, marker) {
			t.Errorf("compact prompt should still cover %q", marker)
		}
	}
}

// deferrableRegistry holds a core tool plus enough extra tools to clear the
// minimumDeferrableTools threshold.
func deferrableRegistry() *tools.ToolRegistry {
	registry := tools.NewEmptyToolRegistry()
	registry.Register(&stubRegistryTool{name: "shell"})
	for _, name := range []string{"gitlab_issues", "google_calendar", "home_assistant", "jobs"} {
		registry.Register(&stubRegistryTool{name: name})
	}
	return registry
}

func TestApplyPromptProfileDefersOnlyForCompact(t *testing.T) {
	ctx, _ := newSystemPromptTestContext(t, "user-1", "main")

	fullRunner := &Runner{AgentID: "main", toolRegistry: deferrableRegistry()}
	fullRunner.applyPromptProfile(ctx, largeContextWindow)
	if got := len(fullRunner.toolRegistry.DeferredCatalog()); got != 0 {
		t.Errorf("full profile deferred %d tools, want none", got)
	}
	if fullRunner.toolRegistry.Get(toolsearch.ToolName) != nil {
		t.Error("full profile should not register tool_search")
	}

	compactRunner := &Runner{AgentID: "main", toolRegistry: deferrableRegistry()}
	compactRunner.applyPromptProfile(ctx, smallContextWindow)
	if compactRunner.toolRegistry.Get(toolsearch.ToolName) == nil {
		t.Fatal("compact profile should register tool_search")
	}
	loaded := compactRunner.toolRegistry.LoadedNames()
	if len(loaded) != 2 || loaded[0] != "shell" || loaded[1] != toolsearch.ToolName {
		t.Errorf("LoadedNames() = %v, want the core tool plus tool_search", loaded)
	}
	if got := len(compactRunner.toolRegistry.DeferredCatalog()); got != 4 {
		t.Errorf("compact profile deferred %d tools, want 4", got)
	}
}

func TestApplyPromptProfileKeepsActivationsAcrossTurns(t *testing.T) {
	ctx, _ := newSystemPromptTestContext(t, "user-1", "main")

	runner := &Runner{AgentID: "main", toolRegistry: deferrableRegistry()}
	runner.applyPromptProfile(ctx, smallContextWindow)
	runner.toolRegistry.Activate([]string{"jobs"})

	// A follow-up message in the same conversation reuses the runner.
	runner.applyPromptProfile(ctx, smallContextWindow)

	loaded := runner.toolRegistry.LoadedNames()
	found := false
	for _, name := range loaded {
		if name == "jobs" {
			found = true
		}
	}
	if !found {
		t.Errorf("LoadedNames() = %v, want the previously loaded jobs tool to stay loaded", loaded)
	}
}

func TestApplyPromptProfileSkipsDeferralForSmallToolSets(t *testing.T) {
	ctx, _ := newSystemPromptTestContext(t, "user-1", "main")

	registry := tools.NewEmptyToolRegistry()
	registry.Register(&stubRegistryTool{name: "shell"})
	registry.Register(&stubRegistryTool{name: "jobs"})
	runner := &Runner{AgentID: "main", toolRegistry: registry}
	runner.applyPromptProfile(ctx, smallContextWindow)

	if got := len(registry.DeferredCatalog()); got != 0 {
		t.Errorf("deferred %d tools, want none: the catalog would cost more than it saves", got)
	}
	if registry.Get(toolsearch.ToolName) != nil {
		t.Error("tool_search should not be registered when nothing is deferred")
	}
}

func TestResolveCoreToolsPrefersConfiguration(t *testing.T) {
	ctx, openedStore := newSystemPromptTestContext(t, "user-1", "main")

	if got := resolveCoreTools(ctx); len(got) != len(tools.DefaultCoreTools) {
		t.Fatalf("resolveCoreTools() = %v, want the built-in default set", got)
	}

	if err := openedStore.Transaction(context.Background(), func(ctx context.Context, transaction store.Transaction) error {
		_, err := transaction.ModifyConfiguration(ctx, func(configuration *models.Configuration) error {
			if configuration.Tools == nil {
				configuration.Tools = &models.ToolsConfiguration{}
			}
			configuration.Tools.CoreTools = ptrto.Value([]string{"shell"})
			return nil
		}, nil)
		return err
	}); err != nil {
		t.Fatalf("configuring core tools: %v", err)
	}

	if got := resolveCoreTools(ctx); len(got) != 1 || got[0] != "shell" {
		t.Errorf("resolveCoreTools() = %v, want [shell]", got)
	}
}

// capturingMockServer serves the same stream as mockOpenAIServer while
// recording each decoded request body.
func capturingMockServer(t *testing.T, requests *[]map[string]interface{}) *httptest.Server {
	t.Helper()
	inner := mockOpenAIServer("ok")
	t.Cleanup(inner.Close)
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("reading request body: %v", err)
		}
		var decoded map[string]interface{}
		if err := json.Unmarshal(body, &decoded); err != nil {
			t.Errorf("decoding request body: %v", err)
		}
		*requests = append(*requests, decoded)

		proxied, err := http.NewRequestWithContext(request.Context(), request.Method, inner.URL+request.URL.Path, bytes.NewReader(body))
		if err != nil {
			t.Errorf("building proxied request: %v", err)
			return
		}
		response, err := http.DefaultClient.Do(proxied)
		if err != nil {
			t.Errorf("proxying request: %v", err)
			return
		}
		defer func() { _ = response.Body.Close() }()
		writer.Header().Set("Content-Type", response.Header.Get("Content-Type"))
		_, _ = io.Copy(writer, response.Body)
	}))
}

func setContextWindow(t *testing.T, openedStore store.Store, contextWindow int) {
	t.Helper()
	if err := openedStore.Transaction(context.Background(), func(ctx context.Context, transaction store.Transaction) error {
		_, err := transaction.ModifyConfiguration(ctx, func(configuration *models.Configuration) error {
			if configuration.Models == nil {
				configuration.Models = &models.ModelsConfiguration{}
			}
			configuration.Models.ContextWindow = ptrto.Value(contextWindow)
			return nil
		}, nil)
		return err
	}); err != nil {
		t.Fatalf("setting context window: %v", err)
	}
}

func requestToolNames(t *testing.T, request map[string]interface{}) []string {
	t.Helper()
	rawTools, _ := request["tools"].([]interface{})
	names := make([]string, 0, len(rawTools))
	for _, rawTool := range rawTools {
		definition, _ := rawTool.(map[string]interface{})
		function, _ := definition["function"].(map[string]interface{})
		name, _ := function["name"].(string)
		names = append(names, name)
	}
	return names
}

func requestHasToolReturns(request map[string]interface{}) bool {
	rawTools, _ := request["tools"].([]interface{})
	for _, rawTool := range rawTools {
		definition, _ := rawTool.(map[string]interface{})
		function, _ := definition["function"].(map[string]interface{})
		if _, ok := function["returns"]; ok {
			return true
		}
	}
	return false
}

func runWithContextWindow(t *testing.T, contextWindow int, conversationId string) map[string]interface{} {
	t.Helper()
	var requests []map[string]interface{}
	server := capturingMockServer(t, &requests)
	defer server.Close()

	testStore := newTestConversationStore(t, "user-1", "main", "mock:mock-model")
	setContextWindow(t, testStore.persistenceStore, contextWindow)

	registry := tools.NewEmptyToolRegistry()
	registry.Register(&stubRegistryTool{name: "shell"})
	for _, name := range []string{"gitlab_issues", "google_calendar", "home_assistant", "jobs"} {
		registry.Register(&stubRegistryTool{name: name})
	}
	registry.Register(&returningTool{name: "user_memory"})

	runner := &Runner{
		AgentID:          "main",
		ConversationID:   conversationId,
		providerRegistry: mockProviderRegistry(server.URL),
		toolRegistry:     registry,
		promptProfile:    PromptProfileFull,
	}
	if _, err := runner.Run(contextWithUserAndStore("user-1", testStore.persistenceStore), RunParameters{Message: "hi"}, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(requests) == 0 {
		t.Fatal("mock provider received no request")
	}
	return requests[0]
}

// returningTool carries a returns schema, which only the full profile sends.
type returningTool struct{ name string }

func (self *returningTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name:        self.name,
			Description: "A stub tool with an output schema.",
			Parameters:  map[string]interface{}{"type": "object"},
			Returns:     map[string]interface{}{"type": "string"},
		},
	}
}

func (self *returningTool) PolicyGroups() []tools.PolicyGroup {
	return []tools.PolicyGroup{
		{Group: models.ToolPolicyGroupAll, Default: models.ToolPolicyAnyone},
	}
}

func (self *returningTool) Execute(_ context.Context, _ string) (string, error) {
	return "ok", nil
}

func TestRunSendsEveryToolForALargeContextWindow(t *testing.T) {
	request := runWithContextWindow(t, largeContextWindow, "large-window")

	if got := len(requestToolNames(t, request)); got != 6 {
		t.Errorf("request carried %d tools, want all 6", got)
	}
	if !requestHasToolReturns(request) {
		t.Error("full profile should keep the returns schemas")
	}
	systemPrompt, _ := request["messages"].([]interface{})
	if len(systemPrompt) == 0 {
		t.Fatal("request carried no messages")
	}
	first, _ := systemPrompt[0].(map[string]interface{})
	content, _ := first["content"].(string)
	if strings.Contains(content, "Additional Tools") {
		t.Error("full profile should not advertise a deferred tool catalog")
	}
}

func TestRunDefersToolsForASmallContextWindow(t *testing.T) {
	request := runWithContextWindow(t, smallContextWindow, "small-window")

	names := requestToolNames(t, request)
	sort.Strings(names)
	want := []string{"shell", toolsearch.ToolName, "user_memory"}
	if len(names) != len(want) {
		t.Fatalf("request carried tools %v, want %v", names, want)
	}
	for index, name := range want {
		if names[index] != name {
			t.Fatalf("request carried tools %v, want %v", names, want)
		}
	}
	if requestHasToolReturns(request) {
		t.Error("compact profile should drop the returns schemas")
	}

	messages, _ := request["messages"].([]interface{})
	first, _ := messages[0].(map[string]interface{})
	content, _ := first["content"].(string)
	if !strings.Contains(content, "## Additional Tools") {
		t.Error("compact profile should advertise the deferred tools in the system prompt")
	}
	for _, deferredTool := range []string{"gitlab_issues", "google_calendar", "home_assistant", "jobs"} {
		if !strings.Contains(content, deferredTool) {
			t.Errorf("catalog should list %q", deferredTool)
		}
	}
}

func TestBuildSystemPromptCompactProfileTruncatesWorkspaceFiles(t *testing.T) {
	ctx, openedStore := newSystemPromptTestContext(t, "user-1", "main")
	seedWorkspaceFile(t, openedStore, models.ScopeAgent, "main", "AGENT.md", strings.Repeat("x", fullWorkspaceFileCharacters))

	compact := buildSystemPrompt(ctx, buildSystemPromptParameters{
		AgentID: "main",
		Mode:    SystemPromptModeFull,
		Profile: PromptProfileCompact,
	})
	if strings.Contains(compact, strings.Repeat("x", compactWorkspaceFileCharacters+1)) {
		t.Error("compact profile should truncate AGENT.md to the compact cap")
	}
	if !strings.Contains(compact, "... (truncated)") {
		t.Error("compact profile should mark the truncation")
	}
}
