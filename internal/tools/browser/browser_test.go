package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/teanode/teanode/internal/integrations/browsers"
	"github.com/teanode/teanode/internal/models"
)

// --- Mock browser for testing ---

type mockBrowser struct {
	targets       []browsers.ConnectedTarget
	commands      []mockCommand
	responses     map[string]json.RawMessage
	mutex         sync.Mutex
	targetOwners  map[string]string
	sessionOwners map[string]string
}

type mockCommand struct {
	Method     string
	SessionID  string
	Parameters interface{}
}

func newMockBrowser() *mockBrowser {
	return &mockBrowser{
		targets: []browsers.ConnectedTarget{
			{
				SessionID: "session-1",
				TargetID:  "target-1",
				URL:       "https://example.com",
				Title:     "Example",
				Source:    "headless",
			},
		},
		responses:     make(map[string]json.RawMessage),
		targetOwners:  map[string]string{"target-1": "user-1"},
		sessionOwners: map[string]string{"session-1": "user-1"},
	}
}

func (self *mockBrowser) Connected() bool { return true }

func (self *mockBrowser) Targets() []browsers.ConnectedTarget { return self.targets }

func (self *mockBrowser) DefaultTarget() (*browsers.ConnectedTarget, error) {
	if len(self.targets) == 0 {
		return nil, fmt.Errorf("no targets")
	}
	return &self.targets[0], nil
}

func (self *mockBrowser) TargetByConnectionID(connectionId string) (*browsers.ConnectedTarget, error) {
	for index := range self.targets {
		if self.targets[index].SessionID == connectionId {
			return &self.targets[index], nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (self *mockBrowser) SendCDPCommand(_ context.Context, method string, parameters interface{}, sessionId string) (json.RawMessage, error) {
	self.mutex.Lock()
	self.commands = append(self.commands, mockCommand{Method: method, SessionID: sessionId, Parameters: parameters})
	self.mutex.Unlock()
	if response, ok := self.responses[method]; ok {
		return response, nil
	}
	return json.RawMessage(`{}`), nil
}

func (self *mockBrowser) TargetsForUser(userId string) []browsers.ConnectedTarget {
	var result []browsers.ConnectedTarget
	for _, target := range self.targets {
		if self.sessionOwners[target.SessionID] == userId {
			result = append(result, target)
		}
	}
	return result
}

func (self *mockBrowser) DefaultTargetForUser(userId string) (*browsers.ConnectedTarget, error) {
	for index, target := range self.targets {
		if self.sessionOwners[target.SessionID] == userId {
			return &self.targets[index], nil
		}
	}
	return nil, fmt.Errorf("no target for user")
}

func (self *mockBrowser) TargetByConnectionIDForUser(userId, connectionId string) (*browsers.ConnectedTarget, error) {
	for index, target := range self.targets {
		if target.SessionID == connectionId && self.sessionOwners[connectionId] == userId {
			return &self.targets[index], nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (self *mockBrowser) AssignTargetToUser(userId, targetId string) {
	self.targetOwners[targetId] = userId
	for _, target := range self.targets {
		if target.TargetID == targetId {
			self.sessionOwners[target.SessionID] = userId
		}
	}
}

func contextWithUserAndBrowser(browser browsers.Browser) context.Context {
	ctx := context.Background()
	ctx = browsers.ContextWithBrowser(ctx, browser)
	ctx = models.ContextWithUserSessionToken(ctx, &models.User{ID: "user-1"}, nil, nil)
	return ctx
}

// --- Tests ---

func TestBuildAXTreeWithRefs(t *testing.T) {
	nodes := []accessibilityNodeExt{
		{NodeID: "root", Role: accessibilityValue{Value: "WebArea"}, Name: accessibilityValue{Value: "Test Page"}, ChildIDs: []string{"btn", "txt", "heading"}},
		{NodeID: "btn", ParentID: "root", Role: accessibilityValue{Value: "button"}, Name: accessibilityValue{Value: "Submit"}, BackendDOMNodeID: 10},
		{NodeID: "txt", ParentID: "root", Role: accessibilityValue{Value: "textbox"}, Name: accessibilityValue{Value: "Email"}, BackendDOMNodeID: 20, Value: &accessibilityValue{Value: ""}},
		{NodeID: "heading", ParentID: "root", Role: accessibilityValue{Value: "heading"}, Name: accessibilityValue{Value: "Welcome"}, Properties: []accessibilityProperty{{Name: "level", Value: accessibilityValue{Value: float64(1)}}}},
	}

	tree, refs := buildAXTreeWithRefs(nodes)

	// Should have 2 refs (button + textbox), heading is not interactive.
	if len(refs) != 2 {
		t.Errorf("expected 2 refs, got %d", len(refs))
	}

	// Verify ref 1 is the button.
	if refs[1].Role != "button" || refs[1].Name != "Submit" {
		t.Errorf("ref 1 should be button Submit, got %+v", refs[1])
	}

	// Verify ref 2 is the textbox.
	if refs[2].Role != "textbox" || refs[2].Name != "Email" {
		t.Errorf("ref 2 should be textbox Email, got %+v", refs[2])
	}

	// Verify tree output contains ref markers.
	if !containsString(tree, "[ref=1]") {
		t.Error("tree should contain [ref=1]")
	}
	if !containsString(tree, "[ref=2]") {
		t.Error("tree should contain [ref=2]")
	}
	if containsString(tree, "[ref=3]") {
		t.Error("tree should NOT contain [ref=3] (heading is not interactive)")
	}

	// Verify heading has level property.
	if !containsString(tree, "(level 1)") {
		t.Error("tree should contain heading level")
	}
}

func TestBuildAXTreeWithRefsEmpty(t *testing.T) {
	tree, refs := buildAXTreeWithRefs(nil)
	if tree != "(empty accessibility tree)" {
		t.Errorf("expected empty tree message, got %q", tree)
	}
	if refs != nil {
		t.Errorf("expected nil refs, got %v", refs)
	}
}

func TestBuildAXTreeWithRefsIgnored(t *testing.T) {
	nodes := []accessibilityNodeExt{
		{NodeID: "root", Role: accessibilityValue{Value: "WebArea"}, ChildIDs: []string{"btn"}},
		{NodeID: "btn", ParentID: "root", Role: accessibilityValue{Value: "button"}, Name: accessibilityValue{Value: "Hidden"}, BackendDOMNodeID: 10, Ignored: true},
	}

	_, refs := buildAXTreeWithRefs(nodes)
	if len(refs) != 0 {
		t.Errorf("ignored nodes should not get refs, got %d", len(refs))
	}
}

func TestBuildAXTreeWithRefsGenericSkip(t *testing.T) {
	// Generic nodes without names should be transparent (children promoted).
	nodes := []accessibilityNodeExt{
		{NodeID: "root", Role: accessibilityValue{Value: "WebArea"}, ChildIDs: []string{"generic"}},
		{NodeID: "generic", ParentID: "root", Role: accessibilityValue{Value: "generic"}, ChildIDs: []string{"btn"}},
		{NodeID: "btn", ParentID: "generic", Role: accessibilityValue{Value: "button"}, Name: accessibilityValue{Value: "OK"}, BackendDOMNodeID: 5},
	}

	tree, refs := buildAXTreeWithRefs(nodes)
	if len(refs) != 1 {
		t.Errorf("expected 1 ref, got %d", len(refs))
	}
	// The button should appear at the root level (depth 1, not 2)
	// because the generic wrapper is skipped.
	if !containsString(tree, "  [ref=1] button") {
		t.Errorf("button should be at depth 1 after generic skip, got:\n%s", tree)
	}
}

func TestRefStoreLookup(t *testing.T) {
	store := &refStore{sessions: make(map[string]map[int]refEntry)}

	// Lookup on empty store.
	_, err := store.lookup("session-1", 1)
	if err == nil {
		t.Error("expected error on empty store")
	}

	// Store and lookup.
	store.store("session-1", map[int]refEntry{
		1: {BackendDOMNodeID: 10, Role: "button", Name: "OK"},
	})
	entry, err := store.lookup("session-1", 1)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if entry.BackendDOMNodeID != 10 {
		t.Errorf("expected BackendDOMNodeID 10, got %d", entry.BackendDOMNodeID)
	}

	// Missing ref.
	_, err = store.lookup("session-1", 99)
	if err == nil {
		t.Error("expected error for missing ref")
	}

	// Clear.
	store.clear("session-1")
	_, err = store.lookup("session-1", 1)
	if err == nil {
		t.Error("expected error after clear")
	}
}

func TestInstanceStore(t *testing.T) {
	store := &instanceStore{names: make(map[string]map[string]string)}

	// Resolve on empty store.
	_, err := store.resolve("user-1", "dashboard")
	if err == nil {
		t.Error("expected error on empty store")
	}

	// Assign and resolve.
	store.assign("user-1", "dashboard", "conn-123")
	connectionId, err := store.resolve("user-1", "dashboard")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if connectionId != "conn-123" {
		t.Errorf("expected conn-123, got %s", connectionId)
	}

	// List.
	all := store.listForUser("user-1")
	if len(all) != 1 || all["dashboard"] != "conn-123" {
		t.Errorf("unexpected list result: %v", all)
	}

	// Different user sees nothing.
	all2 := store.listForUser("user-2")
	if len(all2) != 0 {
		t.Errorf("user-2 should have no instances, got %v", all2)
	}

	// Remove.
	store.remove("user-1", "dashboard")
	_, err = store.resolve("user-1", "dashboard")
	if err == nil {
		t.Error("expected error after remove")
	}
}

func TestInstanceStoreRemoveByConnectionId(t *testing.T) {
	store := &instanceStore{names: make(map[string]map[string]string)}
	store.assign("user-1", "dashboard", "conn-1")
	store.assign("user-1", "settings", "conn-2")
	store.assign("user-2", "other", "conn-1")

	store.removeByConnectionId("conn-1")

	if _, err := store.resolve("user-1", "dashboard"); err == nil {
		t.Error("expected dashboard to be removed")
	}
	if _, err := store.resolve("user-2", "other"); err == nil {
		t.Error("expected other to be removed")
	}
	connectionId, err := store.resolve("user-1", "settings")
	if err != nil {
		t.Fatalf("expected settings to remain: %v", err)
	}
	if connectionId != "conn-2" {
		t.Errorf("expected conn-2, got %q", connectionId)
	}
}

func TestInteractiveRoles(t *testing.T) {
	interactive := []string{"button", "link", "textbox", "checkbox", "radio", "combobox", "slider", "tab", "menuitem"}
	for _, role := range interactive {
		if !isInteractiveRole(role) {
			t.Errorf("expected %q to be interactive", role)
		}
	}

	nonInteractive := []string{"heading", "paragraph", "WebArea", "generic", "img", "list", "navigation", "banner"}
	for _, role := range nonInteractive {
		if isInteractiveRole(role) {
			t.Errorf("expected %q to NOT be interactive", role)
		}
	}
}

func TestBrowserToolDefinition(t *testing.T) {
	tool := &browserTool{}
	definition := tool.Definition()

	if definition.Function.Name != "browser" {
		t.Errorf("expected tool name 'browser', got %q", definition.Function.Name)
	}

	// Verify all new actions are in the enum.
	parameters := definition.Function.Parameters.(map[string]interface{})
	properties := parameters["properties"].(map[string]interface{})
	actionProperty := properties["action"].(map[string]interface{})
	actionEnum := actionProperty["enum"].([]string)

	expectedActions := []string{
		"navigate", "screenshot", "snapshot",
		"click", "click_ref", "type", "type_ref",
		"hover_ref", "select_option",
		"press_key", "evaluate",
		"wait", "execute_script",
		"intercept_start", "intercept_stop", "get_intercepted",
	}

	for _, expected := range expectedActions {
		found := false
		for _, actual := range actionEnum {
			if actual == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("action %q missing from browser tool definition", expected)
		}
	}

	// Verify new properties exist.
	expectedProperties := []string{"ref", "clearFirst", "waitMode", "timeoutMs", "steps", "optionValue", "optionIndex", "urlPattern"}
	for _, prop := range expectedProperties {
		if _, ok := properties[prop]; !ok {
			t.Errorf("property %q missing from browser tool definition", prop)
		}
	}
}

func TestBrowserTabsToolDefinition(t *testing.T) {
	tool := &browserTabsTool{}
	definition := tool.Definition()

	if definition.Function.Name != "browser_tabs" {
		t.Errorf("expected tool name 'browser_tabs', got %q", definition.Function.Name)
	}

	parameters := definition.Function.Parameters.(map[string]interface{})
	properties := parameters["properties"].(map[string]interface{})
	actionProperty := properties["action"].(map[string]interface{})
	actionEnum := actionProperty["enum"].([]string)

	expectedActions := []string{"list", "open", "close", "activate", "name", "resolve"}
	for _, expected := range expectedActions {
		found := false
		for _, actual := range actionEnum {
			if actual == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("action %q missing from browser_tabs tool definition", expected)
		}
	}

	// Verify name and connectionId properties exist.
	if _, ok := properties["name"]; !ok {
		t.Error("property 'name' missing from browser_tabs tool definition")
	}
	if _, ok := properties["connectionId"]; !ok {
		t.Error("property 'connectionId' missing from browser_tabs tool definition")
	}
}

func TestBrowserToolExecuteSnapshot(t *testing.T) {
	mock := newMockBrowser()

	// Set up the Runtime.evaluate response for the DOM snapshot script.
	// The DOM walker returns an object with tree, refCount, refs, pageUrl, title.
	snapshotValue := map[string]interface{}{
		"tree":     "RootWebArea \"Test Page\"\n  [ref=1] button \"Click Me\"\n  [ref=2] textbox \"Email\" value=\"\"",
		"refCount": 2,
		"refs": []map[string]interface{}{
			{"role": "button", "name": "Click Me"},
			{"role": "textbox", "name": "Email"},
		},
		"pageUrl": "https://example.com",
		"title":   "Test Page",
	}
	evalResponse := map[string]interface{}{
		"result": map[string]interface{}{
			"type":  "object",
			"value": snapshotValue,
		},
	}
	evalData, _ := json.Marshal(evalResponse)
	mock.responses["Runtime.evaluate"] = evalData

	ctx := contextWithUserAndBrowser(mock)
	tool := &browserTool{}
	result, err := tool.Execute(ctx, `{"action":"snapshot"}`)
	if err != nil {
		t.Fatalf("snapshot error: %v", err)
	}

	var snapshot snapshotResult
	if err := json.Unmarshal([]byte(result), &snapshot); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}

	if snapshot.RefCount != 2 {
		t.Errorf("expected 2 refs, got %d", snapshot.RefCount)
	}
	if !containsString(snapshot.Tree, "[ref=1] button") {
		t.Errorf("tree should contain ref marker for button, got:\n%s", snapshot.Tree)
	}
	if !containsString(snapshot.Tree, "[ref=2] textbox") {
		t.Errorf("tree should contain ref marker for textbox, got:\n%s", snapshot.Tree)
	}
	if snapshot.PageURL != "https://example.com" {
		t.Errorf("expected pageUrl 'https://example.com', got %q", snapshot.PageURL)
	}
	if snapshot.Title != "Test Page" {
		t.Errorf("expected title 'Test Page', got %q", snapshot.Title)
	}
}

func TestBrowserToolExecuteUnknownAction(t *testing.T) {
	mock := newMockBrowser()
	ctx := contextWithUserAndBrowser(mock)
	tool := &browserTool{}
	_, err := tool.Execute(ctx, `{"action":"unknown_action"}`)
	if err == nil {
		t.Error("expected error for unknown action")
	}
}

func TestBrowserToolExecuteRefMissing(t *testing.T) {
	mock := newMockBrowser()
	ctx := contextWithUserAndBrowser(mock)
	tool := &browserTool{}

	// click_ref without ref.
	_, err := tool.Execute(ctx, `{"action":"click_ref"}`)
	if err == nil {
		t.Error("expected error when ref is missing for click_ref")
	}

	// type_ref without ref.
	_, err = tool.Execute(ctx, `{"action":"type_ref","text":"hello"}`)
	if err == nil {
		t.Error("expected error when ref is missing for type_ref")
	}

	// hover_ref without ref.
	_, err = tool.Execute(ctx, `{"action":"hover_ref"}`)
	if err == nil {
		t.Error("expected error when ref is missing for hover_ref")
	}

	// select_option without ref.
	_, err = tool.Execute(ctx, `{"action":"select_option","optionValue":"a"}`)
	if err == nil {
		t.Error("expected error when ref is missing for select_option")
	}
}

func TestBrowserTabsNameAndResolve(t *testing.T) {
	mock := newMockBrowser()
	ctx := contextWithUserAndBrowser(mock)
	tool := &browserTabsTool{}

	// Name a tab.
	result, err := tool.Execute(ctx, `{"action":"name","name":"dashboard","connectionId":"session-1"}`)
	if err != nil {
		t.Fatalf("name error: %v", err)
	}
	var nameResult map[string]string
	if err := json.Unmarshal([]byte(result), &nameResult); err != nil {
		t.Fatalf("unmarshal name result: %v", err)
	}
	if nameResult["name"] != "dashboard" {
		t.Errorf("expected name 'dashboard', got %q", nameResult["name"])
	}

	// Resolve the name.
	result, err = tool.Execute(ctx, `{"action":"resolve","name":"dashboard"}`)
	if err != nil {
		t.Fatalf("resolve error: %v", err)
	}
	var resolveResult map[string]string
	if err := json.Unmarshal([]byte(result), &resolveResult); err != nil {
		t.Fatalf("unmarshal resolve result: %v", err)
	}
	if resolveResult["connectionId"] != "session-1" {
		t.Errorf("expected connectionId 'session-1', got %q", resolveResult["connectionId"])
	}

	// Resolve unknown name.
	_, err = tool.Execute(ctx, `{"action":"resolve","name":"unknown"}`)
	if err == nil {
		t.Error("expected error resolving unknown name")
	}

	// Cleanup so subsequent tests aren't polluted.
	globalInstanceStore.remove("user-1", "dashboard")
}

func TestBrowserTabsList(t *testing.T) {
	mock := newMockBrowser()
	ctx := contextWithUserAndBrowser(mock)

	// Name a tab first.
	globalInstanceStore.assign("user-1", "main-tab", "session-1")
	defer globalInstanceStore.remove("user-1", "main-tab")

	tool := &browserTabsTool{}
	result, err := tool.Execute(ctx, `{"action":"list"}`)
	if err != nil {
		t.Fatalf("list error: %v", err)
	}

	var listResult struct {
		Tabs []struct {
			TargetID     string `json:"targetId"`
			Name         string `json:"name"`
			ConnectionID string `json:"connectionId"`
		} `json:"tabs"`
	}
	if err := json.Unmarshal([]byte(result), &listResult); err != nil {
		t.Fatalf("unmarshal list result: %v", err)
	}

	if len(listResult.Tabs) != 1 {
		t.Fatalf("expected 1 tab, got %d", len(listResult.Tabs))
	}
	if listResult.Tabs[0].Name != "main-tab" {
		t.Errorf("expected tab name 'main-tab', got %q", listResult.Tabs[0].Name)
	}
}

func TestBrowserTabsListPrunesStaleNames(t *testing.T) {
	mock := newMockBrowser()
	ctx := contextWithUserAndBrowser(mock)

	globalInstanceStore.assign("user-1", "stale-tab", "session-missing")
	defer globalInstanceStore.remove("user-1", "stale-tab")

	tool := &browserTabsTool{}
	result, err := tool.Execute(ctx, `{"action":"list"}`)
	if err != nil {
		t.Fatalf("list error: %v", err)
	}

	var listResult struct {
		Tabs []struct {
			Name string `json:"name"`
		} `json:"tabs"`
	}
	if err := json.Unmarshal([]byte(result), &listResult); err != nil {
		t.Fatalf("unmarshal list result: %v", err)
	}
	if len(listResult.Tabs) != 1 {
		t.Fatalf("expected 1 tab, got %d", len(listResult.Tabs))
	}
	if listResult.Tabs[0].Name != "" {
		t.Errorf("expected stale name to be pruned, got %q", listResult.Tabs[0].Name)
	}
	if _, err := globalInstanceStore.resolve("user-1", "stale-tab"); err == nil {
		t.Error("expected stale name mapping to be removed")
	}
}

func TestBrowserTabsResolvePrunesStaleNames(t *testing.T) {
	mock := newMockBrowser()
	ctx := contextWithUserAndBrowser(mock)

	globalInstanceStore.assign("user-1", "stale-tab", "session-missing")
	defer globalInstanceStore.remove("user-1", "stale-tab")

	tool := &browserTabsTool{}
	_, err := tool.Execute(ctx, `{"action":"resolve","name":"stale-tab"}`)
	if err == nil {
		t.Fatal("expected resolve to fail for stale name")
	}
	if _, err := globalInstanceStore.resolve("user-1", "stale-tab"); err == nil {
		t.Error("expected stale name mapping to be removed after resolve")
	}
}

func TestBrowserTabsNameRejectsMissingConnectionId(t *testing.T) {
	mock := newMockBrowser()
	ctx := contextWithUserAndBrowser(mock)
	tool := &browserTabsTool{}

	_, err := tool.Execute(ctx, `{"action":"name","name":"dashboard","connectionId":"session-missing"}`)
	if err == nil {
		t.Fatal("expected missing connectionId to be rejected")
	}
	if _, err := globalInstanceStore.resolve("user-1", "dashboard"); err == nil {
		t.Error("expected missing connectionId not to be stored")
	}
}

func TestExecuteScriptEmptySteps(t *testing.T) {
	mock := newMockBrowser()
	ctx := contextWithUserAndBrowser(mock)
	tool := &browserTool{}
	_, err := tool.Execute(ctx, `{"action":"execute_script","steps":[]}`)
	if err == nil {
		t.Error("expected error for empty steps")
	}
}

func TestBrowserTabsNameMissing(t *testing.T) {
	mock := newMockBrowser()
	ctx := contextWithUserAndBrowser(mock)
	tool := &browserTabsTool{}

	_, err := tool.Execute(ctx, `{"action":"name","connectionId":"session-1"}`)
	if err == nil {
		t.Error("expected error when name is missing")
	}

	_, err = tool.Execute(ctx, `{"action":"name","name":"test"}`)
	if err == nil {
		t.Error("expected error when connectionId is missing")
	}

	_, err = tool.Execute(ctx, `{"action":"resolve"}`)
	if err == nil {
		t.Error("expected error when name is missing for resolve")
	}
}

func TestDOMSnapshotUsesRuntimeEvaluate(t *testing.T) {
	// Verify that the DOM snapshot sends Runtime.evaluate (not AX domain commands).
	mock := newMockBrowser()

	snapshotValue := map[string]interface{}{
		"tree":     "RootWebArea \"\"",
		"refCount": 0,
		"refs":     []map[string]interface{}{},
		"pageUrl":  "about:blank",
		"title":    "",
	}
	evalResponse := map[string]interface{}{
		"result": map[string]interface{}{
			"type":  "object",
			"value": snapshotValue,
		},
	}
	evalData, _ := json.Marshal(evalResponse)
	mock.responses["Runtime.evaluate"] = evalData

	ctx := contextWithUserAndBrowser(mock)
	tool := &browserTool{}
	_, err := tool.Execute(ctx, `{"action":"snapshot"}`)
	if err != nil {
		t.Fatalf("snapshot error: %v", err)
	}

	mock.mutex.Lock()
	commands := make([]string, len(mock.commands))
	for index, command := range mock.commands {
		commands[index] = command.Method
	}
	mock.mutex.Unlock()

	// Should use Runtime.evaluate, NOT Accessibility.getFullAXTree.
	foundRuntimeEvaluate := false
	for _, command := range commands {
		if command == "Runtime.evaluate" {
			foundRuntimeEvaluate = true
		}
		if command == "Accessibility.getFullAXTree" {
			t.Error("DOM snapshot should NOT call Accessibility.getFullAXTree")
		}
		if command == "DOM.enable" {
			t.Error("DOM snapshot should NOT call DOM.enable")
		}
		if command == "Accessibility.enable" {
			t.Error("DOM snapshot should NOT call Accessibility.enable")
		}
	}
	if !foundRuntimeEvaluate {
		t.Error("DOM snapshot should call Runtime.evaluate")
	}
}

func TestDOMSnapshotRefStorePopulation(t *testing.T) {
	// Verify that the DOM snapshot populates globalRefStore with metadata.
	mock := newMockBrowser()

	snapshotValue := map[string]interface{}{
		"tree":     "RootWebArea \"Test\"\n  [ref=1] button \"Submit\"\n  [ref=2] textbox \"Name\" value=\"\"",
		"refCount": 2,
		"refs": []map[string]interface{}{
			{"role": "button", "name": "Submit"},
			{"role": "textbox", "name": "Name"},
		},
		"pageUrl": "https://example.com",
		"title":   "Test",
	}
	evalResponse := map[string]interface{}{
		"result": map[string]interface{}{
			"type":  "object",
			"value": snapshotValue,
		},
	}
	evalData, _ := json.Marshal(evalResponse)
	mock.responses["Runtime.evaluate"] = evalData

	ctx := contextWithUserAndBrowser(mock)
	tool := &browserTool{}
	_, err := tool.Execute(ctx, `{"action":"snapshot"}`)
	if err != nil {
		t.Fatalf("snapshot error: %v", err)
	}

	// Verify ref store was populated.
	entry1, err := globalRefStore.lookup("session-1", 1)
	if err != nil {
		t.Fatalf("ref 1 not in store: %v", err)
	}
	if entry1.Role != "button" || entry1.Name != "Submit" {
		t.Errorf("ref 1: expected button/Submit, got %s/%s", entry1.Role, entry1.Name)
	}

	entry2, err := globalRefStore.lookup("session-1", 2)
	if err != nil {
		t.Fatalf("ref 2 not in store: %v", err)
	}
	if entry2.Role != "textbox" || entry2.Name != "Name" {
		t.Errorf("ref 2: expected textbox/Name, got %s/%s", entry2.Role, entry2.Name)
	}

	// Ref 3 should not exist.
	_, err = globalRefStore.lookup("session-1", 3)
	if err == nil {
		t.Error("ref 3 should not exist")
	}

	// Cleanup.
	globalRefStore.clear("session-1")
}

func TestDOMSnapshotWithSelectOptions(t *testing.T) {
	// Verify that select options get their own refs.
	mock := newMockBrowser()

	snapshotValue := map[string]interface{}{
		"tree":     "RootWebArea \"Test\"\n  [ref=1] combobox \"Color\"\n    [ref=2] option \"Red\" selected\n    [ref=3] option \"Blue\"",
		"refCount": 3,
		"refs": []map[string]interface{}{
			{"role": "combobox", "name": "Color"},
			{"role": "option", "name": "Red"},
			{"role": "option", "name": "Blue"},
		},
		"pageUrl": "https://example.com",
		"title":   "Test",
	}
	evalResponse := map[string]interface{}{
		"result": map[string]interface{}{
			"type":  "object",
			"value": snapshotValue,
		},
	}
	evalData, _ := json.Marshal(evalResponse)
	mock.responses["Runtime.evaluate"] = evalData

	ctx := contextWithUserAndBrowser(mock)
	tool := &browserTool{}
	result, err := tool.Execute(ctx, `{"action":"snapshot"}`)
	if err != nil {
		t.Fatalf("snapshot error: %v", err)
	}

	var snapshot snapshotResult
	if err := json.Unmarshal([]byte(result), &snapshot); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}

	if snapshot.RefCount != 3 {
		t.Errorf("expected 3 refs (combobox + 2 options), got %d", snapshot.RefCount)
	}
	if !containsString(snapshot.Tree, "[ref=1] combobox") {
		t.Error("tree should contain combobox ref")
	}
	if !containsString(snapshot.Tree, "[ref=2] option") {
		t.Error("tree should contain first option ref")
	}
	if !containsString(snapshot.Tree, "[ref=3] option") {
		t.Error("tree should contain second option ref")
	}

	// Cleanup.
	globalRefStore.clear("session-1")
}

func TestResolveRefToObjectID(t *testing.T) {
	mock := newMockBrowser()

	// Set up a successful resolution response (element found).
	resolveResponse := map[string]interface{}{
		"result": map[string]interface{}{
			"type":     "object",
			"subtype":  "node",
			"objectId": "node-obj-42",
		},
	}
	resolveData, _ := json.Marshal(resolveResponse)
	mock.responses["Runtime.evaluate"] = resolveData

	ctx := contextWithUserAndBrowser(mock)
	objectID, err := resolveRefToObjectID(ctx, mock, "session-1", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if objectID != "node-obj-42" {
		t.Errorf("expected objectId 'node-obj-42', got %q", objectID)
	}
}

func TestResolveRefToObjectIDUndefined(t *testing.T) {
	mock := newMockBrowser()
	globalRefStore.store("session-1", map[int]refEntry{
		99: {Role: "button", Name: "Missing"},
	})
	defer globalRefStore.clear("session-1")

	// Response when the ref doesn't exist (page navigated, etc).
	undefinedResponse := map[string]interface{}{
		"result": map[string]interface{}{
			"type": "undefined",
		},
	}
	undefinedData, _ := json.Marshal(undefinedResponse)
	mock.responses["Runtime.evaluate"] = undefinedData

	ctx := contextWithUserAndBrowser(mock)
	_, err := resolveRefToObjectID(ctx, mock, "session-1", 99)
	if err == nil {
		t.Error("expected error when ref resolves to undefined")
	}
	if _, err := globalRefStore.lookup("session-1", 99); err == nil {
		t.Error("expected stale session refs to be cleared")
	}
}

func TestTabsOpenAssignsOwnershipAndName(t *testing.T) {
	// Simulate the race condition: target is visible in Targets() but
	// not yet in TargetsForUser() because session ownership hasn't been
	// set. The fix polls Targets() and re-assigns ownership afterward.
	mock := &mockBrowserDelayedOwnership{
		mockBrowser:       *newMockBrowser(),
		createdTargetID:   "target-new",
		createdSessionID:  "session-new",
		ownershipAssigned: false,
	}

	// Set up Target.createTarget response.
	createResponse, _ := json.Marshal(map[string]string{"targetId": "target-new"})
	mock.responses["Target.createTarget"] = createResponse

	ctx := contextWithUserAndBrowser(mock)
	tool := &browserTabsTool{}
	result, err := tool.Execute(ctx, `{"action":"open","url":"https://test.com","name":"test-tab"}`)
	if err != nil {
		t.Fatalf("open error: %v", err)
	}

	var openResult map[string]string
	if err := json.Unmarshal([]byte(result), &openResult); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if openResult["connectionId"] != "session-new" {
		t.Errorf("expected connectionId 'session-new', got %q", openResult["connectionId"])
	}
	if openResult["name"] != "test-tab" {
		t.Errorf("expected name 'test-tab', got %q", openResult["name"])
	}

	// Verify the name was stored and is resolvable.
	connectionId, err := globalInstanceStore.resolve("user-1", "test-tab")
	if err != nil {
		t.Fatalf("resolve error: %v", err)
	}
	if connectionId != "session-new" {
		t.Errorf("expected resolved connectionId 'session-new', got %q", connectionId)
	}

	// Verify ownership was assigned.
	if !mock.ownershipAssigned {
		t.Error("expected AssignTargetToUser to be called")
	}

	// Cleanup.
	globalInstanceStore.remove("user-1", "test-tab")
}

func TestCompositeBrowserAssignTargetToUser(t *testing.T) {
	mock := newMockBrowser()
	composite := browsers.NewCompositeBrowser(mock)

	// Verify CompositeBrowser implements TargetOwnerAssigner.
	assigner, ok := interface{}(composite).(browsers.TargetOwnerAssigner)
	if !ok {
		t.Fatal("CompositeBrowser should implement TargetOwnerAssigner")
	}

	// Assign a new target and verify the backend received it.
	assigner.AssignTargetToUser("user-1", "target-1")
	if mock.targetOwners["target-1"] != "user-1" {
		t.Error("expected target owner to be set on backend")
	}
}

func TestSessionLifecycleCleanup(t *testing.T) {
	globalInstanceStore.assign("user-1", "dashboard", "session-1")
	globalRefStore.store("session-1", map[int]refEntry{
		1: {Role: "button", Name: "Submit"},
	})
	defer globalInstanceStore.remove("user-1", "dashboard")
	defer globalRefStore.clear("session-1")

	browsers.NotifySessionNavigated("session-1", "target-1", "https://example.com/next")
	if _, err := globalRefStore.lookup("session-1", 1); err == nil {
		t.Fatal("expected navigation to clear refs")
	}
	if _, err := globalInstanceStore.resolve("user-1", "dashboard"); err != nil {
		t.Fatalf("expected navigation to preserve tab names: %v", err)
	}

	browsers.NotifySessionClosed("session-1")
	if _, err := globalInstanceStore.resolve("user-1", "dashboard"); err == nil {
		t.Error("expected session close to remove named tab")
	}
}

// mockBrowserDelayedOwnership simulates the race where a newly created
// target is visible in Targets() but not yet in TargetsForUser() because
// session ownership hasn't been set by the async attach flow.
type mockBrowserDelayedOwnership struct {
	mockBrowser
	createdTargetID   string
	createdSessionID  string
	ownershipAssigned bool
}

func (self *mockBrowserDelayedOwnership) Targets() []browsers.ConnectedTarget {
	// Always include the new target in the global list.
	targets := self.mockBrowser.Targets()
	for _, target := range targets {
		if target.TargetID == self.createdTargetID {
			return targets
		}
	}
	return append(targets, browsers.ConnectedTarget{
		SessionID: self.createdSessionID,
		TargetID:  self.createdTargetID,
		URL:       "https://test.com",
		Source:    "headless",
	})
}

func (self *mockBrowserDelayedOwnership) TargetsForUser(userId string) []browsers.ConnectedTarget {
	// Only return user targets after ownership is assigned (simulates the race).
	if !self.ownershipAssigned {
		return self.mockBrowser.TargetsForUser(userId)
	}
	targets := self.mockBrowser.TargetsForUser(userId)
	return append(targets, browsers.ConnectedTarget{
		SessionID: self.createdSessionID,
		TargetID:  self.createdTargetID,
		URL:       "https://test.com",
		Source:    "headless",
	})
}

func (self *mockBrowserDelayedOwnership) AssignTargetToUser(userId, targetId string) {
	self.ownershipAssigned = true
	self.mockBrowser.AssignTargetToUser(userId, targetId)
}

// --- Helpers ---

func containsString(haystack, needle string) bool {
	return len(haystack) >= len(needle) && searchString(haystack, needle)
}

func searchString(haystack, needle string) bool {
	for index := 0; index <= len(haystack)-len(needle); index++ {
		if haystack[index:index+len(needle)] == needle {
			return true
		}
	}
	return false
}
