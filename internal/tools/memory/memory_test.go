package memory

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"testing"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/runners"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/store/dbstore"
	"github.com/teanode/teanode/internal/tools"
)

func setupMemoryStore(t *testing.T) context.Context {
	t.Helper()
	if os.Getenv("TEANODE_TEST_POSTGRES") != "1" {
		t.Skip("set TEANODE_TEST_POSTGRES=1 to run memory tool tests")
	}
	port := uint16(5432)
	if portValue := os.Getenv("TEANODE_TEST_POSTGRES_PORT"); portValue != "" {
		parsedPort, parseError := strconv.ParseUint(portValue, 10, 16)
		if parseError != nil {
			t.Fatalf("invalid TEANODE_TEST_POSTGRES_PORT: %v", parseError)
		}
		port = uint16(parsedPort)
	}
	settings := dbstore.Settings{
		Host:     envOrDefault("TEANODE_TEST_POSTGRES_HOST", "127.0.0.1"),
		Port:     port,
		User:     envOrDefault("TEANODE_TEST_POSTGRES_USER", "teanode"),
		Password: envOrDefault("TEANODE_TEST_POSTGRES_PASSWORD", "teanode"),
		Database: envOrDefault("TEANODE_TEST_POSTGRES_DATABASE", "teanode"),
		SSLMode:  envOrDefault("TEANODE_TEST_POSTGRES_SSLMODE", "disable"),
	}
	openedStore, openError := dbstore.Open(settings)
	if openError != nil {
		t.Fatalf("open store: %v", openError)
	}
	if migrateError := openedStore.Migrate(context.Background()); migrateError != nil {
		t.Fatalf("migrate: %v", migrateError)
	}
	t.Cleanup(func() { _ = openedStore.Close() })
	return store.ContextWithStore(context.Background(), openedStore)
}

func envOrDefault(key string, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

type memoryBatchResponse struct {
	Action  string        `json:"action"`
	Results []batchResult `json:"results"`
	Summary batchSummary  `json:"summary"`
}

type memorySingleResponse struct {
	Action  string                   `json:"action"`
	Item    map[string]interface{}   `json:"item,omitempty"`
	Items   []map[string]interface{} `json:"items,omitempty"`
	Matches []interface{}            `json:"matches,omitempty"`
}

func TestMemoryToolRegistration(t *testing.T) {
	registry := tools.NewEmptyToolRegistry()
	for _, tool := range createTools() {
		registry.Register(tool)
	}

	for _, name := range []string{"agent_memory", "user_memory", "project_memory"} {
		if registry.Get(name) == nil {
			t.Errorf("%s not registered", name)
		}
	}
}

func TestMemoryToolDefinition(t *testing.T) {
	registry := tools.NewEmptyToolRegistry()
	for _, tool := range createTools() {
		registry.Register(tool)
	}

	agentTool := registry.Get("agent_memory")
	def := agentTool.Definition()
	if def.Function.Name != "agent_memory" {
		t.Errorf("name = %q, want agent_memory", def.Function.Name)
	}

	// Verify action enum includes get, list, search, batch.
	params := def.Function.Parameters.(map[string]interface{})
	props := params["properties"].(map[string]interface{})
	actionProp := props["action"].(map[string]interface{})
	actionEnum := actionProp["enum"].([]string)
	expectedActions := []string{"get", "list", "search", "batch"}
	if len(actionEnum) != len(expectedActions) {
		t.Errorf("action enum = %v, want %v", actionEnum, expectedActions)
	} else {
		for i, a := range expectedActions {
			if actionEnum[i] != a {
				t.Errorf("action enum[%d] = %q, want %q", i, actionEnum[i], a)
			}
		}
	}

	// Verify items property exists.
	if _, ok := props["items"]; !ok {
		t.Error("items property should exist in definition")
	}

	// Verify batch items op enum is restricted to add/update/delete/get.
	itemsProp := props["items"].(map[string]interface{})
	itemSchema := itemsProp["items"].(map[string]interface{})
	itemProps := itemSchema["properties"].(map[string]interface{})
	opProp := itemProps["op"].(map[string]interface{})
	opEnum := opProp["enum"].([]string)
	expectedOps := []string{"add", "update", "delete", "get"}
	if len(opEnum) != len(expectedOps) {
		t.Errorf("op enum = %v, want %v", opEnum, expectedOps)
	} else {
		for i, o := range expectedOps {
			if opEnum[i] != o {
				t.Errorf("op enum[%d] = %q, want %q", i, opEnum[i], o)
			}
		}
	}

	// Verify required includes action.
	required := params["required"].([]string)
	hasAction := false
	for _, r := range required {
		if r == "action" {
			hasAction = true
		}
	}
	if !hasAction {
		t.Errorf("required = %v, want action", required)
	}

	projectTool := registry.Get("project_memory")
	pDef := projectTool.Definition()
	pParams := pDef.Function.Parameters.(map[string]interface{})
	pRequired := pParams["required"].([]string)
	found := false
	for _, r := range pRequired {
		if r == "projectId" {
			found = true
		}
	}
	if !found {
		t.Errorf("project_memory should require projectId, required = %v", pRequired)
	}
}

func TestMemoryToolRejectsUnknownAction(t *testing.T) {
	ctx := setupMemoryStore(t)
	ctx = runners.ContextWithRunner(ctx, runners.NewRunner(ctx, "test-agent-reject", "", nil, models.Agent{}))
	registry := tools.NewEmptyToolRegistry()
	for _, tool := range createTools() {
		registry.Register(tool)
	}

	agentTool := registry.Get("agent_memory")

	for _, action := range []string{"add", "update", "delete", "foo"} {
		_, err := agentTool.Execute(ctx, `{"action":"`+action+`"}`)
		if err == nil {
			t.Errorf("action %q should be rejected", action)
		}
	}
}

func TestMemorySingleGet(t *testing.T) {
	ctx := setupMemoryStore(t)
	ctx = runners.ContextWithRunner(ctx, runners.NewRunner(ctx, "test-agent-single-get", "", nil, models.Agent{}))
	registry := tools.NewEmptyToolRegistry()
	for _, tool := range createTools() {
		registry.Register(tool)
	}

	tool := registry.Get("agent_memory")

	// Add via batch first.
	result, err := tool.Execute(ctx, `{"action":"batch","items":[{"op":"add","title":"Single get test","content":"hello world","tags":["test"]}]}`)
	if err != nil {
		t.Fatalf("batch add: %v", err)
	}
	var bResp memoryBatchResponse
	json.Unmarshal([]byte(result), &bResp)
	itemID := bResp.Results[0].Item["id"].(string)

	// Single get.
	result, err = tool.Execute(ctx, `{"action":"get","id":"`+itemID+`"}`)
	if err != nil {
		t.Fatalf("single get: %v", err)
	}
	var resp memorySingleResponse
	json.Unmarshal([]byte(result), &resp)
	if resp.Action != "get" {
		t.Errorf("action = %q, want get", resp.Action)
	}
	if resp.Item["title"] != "Single get test" {
		t.Errorf("title = %v, want 'Single get test'", resp.Item["title"])
	}
	if resp.Item["content"] != "hello world" {
		t.Errorf("content = %v, want 'hello world'", resp.Item["content"])
	}

	// Single get without id should error.
	_, err = tool.Execute(ctx, `{"action":"get"}`)
	if err == nil {
		t.Error("get without id should error")
	}
}

func TestMemorySingleList(t *testing.T) {
	ctx := setupMemoryStore(t)
	ctx = runners.ContextWithRunner(ctx, runners.NewRunner(ctx, "test-agent-single-list", "", nil, models.Agent{}))
	registry := tools.NewEmptyToolRegistry()
	for _, tool := range createTools() {
		registry.Register(tool)
	}

	tool := registry.Get("agent_memory")

	// List on empty scope.
	result, err := tool.Execute(ctx, `{"action":"list"}`)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var resp memorySingleResponse
	json.Unmarshal([]byte(result), &resp)
	if resp.Action != "list" {
		t.Errorf("action = %q, want list", resp.Action)
	}
	if len(resp.Items) != 0 {
		t.Errorf("items = %v, want empty", resp.Items)
	}

	// Add items via batch.
	tool.Execute(ctx, `{"action":"batch","items":[{"op":"add","title":"A","content":"aaa","tags":["x"]},{"op":"add","title":"B","content":"bbb","tags":["y"]}]}`)

	// List all.
	result, err = tool.Execute(ctx, `{"action":"list"}`)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	json.Unmarshal([]byte(result), &resp)
	if len(resp.Items) < 2 {
		t.Errorf("expected at least 2 items, got %d", len(resp.Items))
	}

	// List with tags filter.
	result, err = tool.Execute(ctx, `{"action":"list","tags":["x"]}`)
	if err != nil {
		t.Fatalf("list with tags: %v", err)
	}
	json.Unmarshal([]byte(result), &resp)
	if len(resp.Items) != 1 {
		t.Errorf("expected 1 item with tag x, got %d", len(resp.Items))
	}

	// List with maxResults.
	result, err = tool.Execute(ctx, `{"action":"list","maxResults":1}`)
	if err != nil {
		t.Fatalf("list with maxResults: %v", err)
	}
	json.Unmarshal([]byte(result), &resp)
	if len(resp.Items) > 1 {
		t.Errorf("expected at most 1 item, got %d", len(resp.Items))
	}
}

func TestMemorySingleSearch(t *testing.T) {
	ctx := setupMemoryStore(t)
	ctx = runners.ContextWithRunner(ctx, runners.NewRunner(ctx, "test-agent-single-search", "", nil, models.Agent{}))
	registry := tools.NewEmptyToolRegistry()
	for _, tool := range createTools() {
		registry.Register(tool)
	}

	tool := registry.Get("agent_memory")

	// Add items.
	tool.Execute(ctx, `{"action":"batch","items":[{"op":"add","title":"Pet preferences","content":"The user likes cats and kittens","tags":["pets"]},{"op":"add","title":"Work notes","content":"User prefers dark mode in all editors","tags":["work"]}]}`)

	// Search for "cats".
	result, err := tool.Execute(ctx, `{"action":"search","query":"cats"}`)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	var resp memorySingleResponse
	json.Unmarshal([]byte(result), &resp)
	if resp.Action != "search" {
		t.Errorf("action = %q, want search", resp.Action)
	}
	if len(resp.Matches) == 0 {
		t.Fatal("search for 'cats' should find at least 1 match")
	}

	// Case-insensitive search.
	result, err = tool.Execute(ctx, `{"action":"search","query":"DARK MODE"}`)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	json.Unmarshal([]byte(result), &resp)
	if len(resp.Matches) == 0 {
		t.Error("case-insensitive search for 'DARK MODE' should find a match")
	}

	// Max results.
	result, err = tool.Execute(ctx, `{"action":"search","query":"user","maxResults":1}`)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	json.Unmarshal([]byte(result), &resp)
	if len(resp.Matches) > 1 {
		t.Errorf("expected at most 1 result, got %d", len(resp.Matches))
	}

	// No match.
	result, err = tool.Execute(ctx, `{"action":"search","query":"nonexistent_xyz"}`)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	json.Unmarshal([]byte(result), &resp)
	if len(resp.Matches) != 0 {
		t.Errorf("expected no matches, got %d", len(resp.Matches))
	}

	// Search without query should error.
	_, err = tool.Execute(ctx, `{"action":"search"}`)
	if err == nil {
		t.Error("search without query should error")
	}
}

func TestMemoryBatchCRUD(t *testing.T) {
	ctx := setupMemoryStore(t)
	ctx = runners.ContextWithRunner(ctx, runners.NewRunner(ctx, "test-agent-batch-crud", "", nil, models.Agent{}))
	registry := tools.NewEmptyToolRegistry()
	for _, tool := range createTools() {
		registry.Register(tool)
	}

	tool := registry.Get("agent_memory")
	if tool == nil {
		t.Fatal("agent_memory not registered")
	}

	// Batch: add an item.
	result, err := tool.Execute(ctx, `{"action":"batch","items":[{"op":"add","title":"User preferences","content":"The user likes cats","tags":["prefs","pets"]}]}`)
	if err != nil {
		t.Fatalf("batch add: %v", err)
	}
	var resp memoryBatchResponse
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("parse batch add: %v", err)
	}
	if resp.Action != "batch" {
		t.Errorf("action = %q, want batch", resp.Action)
	}
	if resp.Summary.Succeeded != 1 {
		t.Fatalf("add should succeed, summary = %+v", resp.Summary)
	}
	itemID, ok := resp.Results[0].Item["id"].(string)
	if !ok || itemID == "" {
		t.Fatal("add should return item with id")
	}
	if resp.Results[0].Item["content"] != "The user likes cats" {
		t.Errorf("content = %v, want 'The user likes cats'", resp.Results[0].Item["content"])
	}

	// Batch: get the item.
	result, err = tool.Execute(ctx, `{"action":"batch","items":[{"op":"get","id":"`+itemID+`"}]}`)
	if err != nil {
		t.Fatalf("batch get: %v", err)
	}
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("parse batch get: %v", err)
	}
	if resp.Results[0].Item["title"] != "User preferences" {
		t.Errorf("title = %v, want 'User preferences'", resp.Results[0].Item["title"])
	}

	// Batch: update the item.
	result, err = tool.Execute(ctx, `{"action":"batch","items":[{"op":"update","id":"`+itemID+`","content":"The user likes dogs now"}]}`)
	if err != nil {
		t.Fatalf("batch update: %v", err)
	}
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("parse batch update: %v", err)
	}
	if resp.Results[0].Item["content"] != "The user likes dogs now" {
		t.Errorf("content = %v, want 'The user likes dogs now'", resp.Results[0].Item["content"])
	}

	// Batch: delete the item.
	result, err = tool.Execute(ctx, `{"action":"batch","items":[{"op":"delete","id":"`+itemID+`"}]}`)
	if err != nil {
		t.Fatalf("batch delete: %v", err)
	}
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("parse batch delete: %v", err)
	}
	if !resp.Results[0].Success || resp.Results[0].Op != "delete" {
		t.Errorf("delete result = %+v, want success", resp.Results[0])
	}

	// Batch: get after delete should fail in result, not error.
	result, err = tool.Execute(ctx, `{"action":"batch","items":[{"op":"get","id":"`+itemID+`"}]}`)
	if err != nil {
		t.Fatalf("batch get after delete: %v", err)
	}
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("parse batch get after delete: %v", err)
	}
	if resp.Results[0].Success {
		t.Error("expected get after delete to fail")
	}
	if resp.Results[0].Error == "" {
		t.Error("expected error message for get after delete")
	}
}

func TestMemoryBatchRejectsListSearch(t *testing.T) {
	ctx := setupMemoryStore(t)
	ctx = runners.ContextWithRunner(ctx, runners.NewRunner(ctx, "test-agent-batch-reject", "", nil, models.Agent{}))
	registry := tools.NewEmptyToolRegistry()
	for _, tool := range createTools() {
		registry.Register(tool)
	}

	tool := registry.Get("agent_memory")

	// list in batch should fail per-item.
	result, err := tool.Execute(ctx, `{"action":"batch","items":[{"op":"list"}]}`)
	if err != nil {
		t.Fatalf("batch: %v", err)
	}
	var resp memoryBatchResponse
	json.Unmarshal([]byte(result), &resp)
	if resp.Results[0].Success {
		t.Error("list op in batch should fail")
	}
	if resp.Results[0].Error == "" {
		t.Error("list op in batch should have error message")
	}

	// search in batch should fail per-item.
	result, err = tool.Execute(ctx, `{"action":"batch","items":[{"op":"search"}]}`)
	if err != nil {
		t.Fatalf("batch: %v", err)
	}
	json.Unmarshal([]byte(result), &resp)
	if resp.Results[0].Success {
		t.Error("search op in batch should fail")
	}
}

func TestMemoryBatchMixedOperations(t *testing.T) {
	ctx := setupMemoryStore(t)
	ctx = runners.ContextWithRunner(ctx, runners.NewRunner(ctx, "test-agent-mixed", "", nil, models.Agent{}))
	registry := tools.NewEmptyToolRegistry()
	for _, tool := range createTools() {
		registry.Register(tool)
	}

	tool := registry.Get("agent_memory")

	// Add two items in one batch.
	result, err := tool.Execute(ctx, `{"action":"batch","items":[{"op":"add","title":"Item A","content":"Content A"},{"op":"add","title":"Item B","content":"Content B","tags":["tag1"]}]}`)
	if err != nil {
		t.Fatalf("batch add: %v", err)
	}
	var resp memoryBatchResponse
	json.Unmarshal([]byte(result), &resp)
	if resp.Summary.Total != 2 || resp.Summary.Succeeded != 2 {
		t.Fatalf("expected 2 successes, got %+v", resp.Summary)
	}
	idA := resp.Results[0].Item["id"].(string)
	idB := resp.Results[1].Item["id"].(string)

	// Mixed: get A, update B, add C in one batch.
	mixedArgs, _ := json.Marshal(map[string]interface{}{
		"action": "batch",
		"items": []map[string]interface{}{
			{"op": "get", "id": idA},
			{"op": "update", "id": idB, "content": "Updated B"},
			{"op": "add", "title": "Item C", "content": "Content C"},
		},
	})
	result, err = tool.Execute(ctx, string(mixedArgs))
	if err != nil {
		t.Fatalf("mixed batch: %v", err)
	}
	json.Unmarshal([]byte(result), &resp)
	if resp.Summary.Total != 3 || resp.Summary.Succeeded != 3 {
		t.Fatalf("expected 3 successes, got %+v", resp.Summary)
	}
	if resp.Results[0].Op != "get" || resp.Results[1].Op != "update" || resp.Results[2].Op != "add" {
		t.Errorf("ops = %s/%s/%s, want get/update/add", resp.Results[0].Op, resp.Results[1].Op, resp.Results[2].Op)
	}
}

func TestMemoryBatchPartialFailure(t *testing.T) {
	ctx := setupMemoryStore(t)
	ctx = runners.ContextWithRunner(ctx, runners.NewRunner(ctx, "test-agent-partial", "", nil, models.Agent{}))
	registry := tools.NewEmptyToolRegistry()
	for _, tool := range createTools() {
		registry.Register(tool)
	}

	tool := registry.Get("agent_memory")

	// One good add, one bad add (no content).
	result, err := tool.Execute(ctx, `{"action":"batch","items":[{"op":"add","title":"Good","content":"ok"},{"op":"add","title":"Bad"}]}`)
	if err != nil {
		t.Fatalf("batch: %v", err)
	}
	var resp memoryBatchResponse
	json.Unmarshal([]byte(result), &resp)
	if resp.Summary.Succeeded != 1 || resp.Summary.Failed != 1 {
		t.Errorf("expected 1 success 1 failure, got %+v", resp.Summary)
	}
	if resp.Results[0].Success != true {
		t.Error("first item should succeed")
	}
	if resp.Results[1].Success != false || resp.Results[1].Error == "" {
		t.Error("second item should fail with error")
	}
}

func TestUserMemoryBatch(t *testing.T) {
	ctx := setupMemoryStore(t)
	registry := tools.NewEmptyToolRegistry()
	for _, tool := range createTools() {
		registry.Register(tool)
	}

	userTool := registry.Get("user_memory")
	if userTool == nil {
		t.Fatal("user_memory not registered")
	}

	ctx = models.ContextWithUserSessionToken(ctx, &models.User{ID: "user-mem-1"}, nil, nil)

	result, err := userTool.Execute(ctx, `{"action":"batch","items":[{"op":"add","content":"remember this preference","title":"Test memory"}]}`)
	if err != nil {
		t.Fatalf("user_memory batch add: %v", err)
	}
	var resp memoryBatchResponse
	json.Unmarshal([]byte(result), &resp)
	if resp.Summary.Succeeded != 1 {
		t.Fatalf("expected 1 success, got %+v", resp.Summary)
	}
	itemID := resp.Results[0].Item["id"].(string)

	// Single get on user_memory.
	result, err = userTool.Execute(ctx, `{"action":"get","id":"`+itemID+`"}`)
	if err != nil {
		t.Fatalf("user_memory get: %v", err)
	}
	var sResp memorySingleResponse
	json.Unmarshal([]byte(result), &sResp)
	if sResp.Item["content"] != "remember this preference" {
		t.Errorf("content = %v, want 'remember this preference'", sResp.Item["content"])
	}
}

func TestProjectMemoryToolRequiresProjectId(t *testing.T) {
	registry := tools.NewEmptyToolRegistry()
	for _, tool := range createTools() {
		registry.Register(tool)
	}

	projectTool := registry.Get("project_memory")
	if projectTool == nil {
		t.Fatal("project_memory not registered")
	}

	// Execute without projectId should error.
	_, err := projectTool.Execute(context.Background(), `{"action":"list"}`)
	if err == nil {
		t.Error("expected error when projectId is missing")
	}
}

func TestMemoryBatchValidation(t *testing.T) {
	ctx := setupMemoryStore(t)
	ctx = runners.ContextWithRunner(ctx, runners.NewRunner(ctx, "test-agent-validate", "", nil, models.Agent{}))
	registry := tools.NewEmptyToolRegistry()
	for _, tool := range createTools() {
		registry.Register(tool)
	}

	agentTool := registry.Get("agent_memory")

	// Empty items should error.
	_, err := agentTool.Execute(ctx, `{"action":"batch","items":[]}`)
	if err == nil {
		t.Error("expected error for empty items")
	}
}
