package memory

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"testing"
	"time"

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
	expectedActions := []string{"get", "list", "search", "batch", "retrieve", "summary", "filter"}
	if len(actionEnum) != len(expectedActions) {
		t.Errorf("action enum = %v, want %v", actionEnum, expectedActions)
	} else {
		for i, expected := range expectedActions {
			if actionEnum[i] != expected {
				t.Errorf("action enum[%d] = %q, want %q", i, actionEnum[i], expected)
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
		for i, expected := range expectedOps {
			if opEnum[i] != expected {
				t.Errorf("op enum[%d] = %q, want %q", i, opEnum[i], expected)
			}
		}
	}

	// Verify required includes action.
	required := params["required"].([]string)
	hasAction := false
	for _, field := range required {
		if field == "action" {
			hasAction = true
		}
	}
	if !hasAction {
		t.Errorf("required = %v, want action", required)
	}

	projectTool := registry.Get("project_memory")
	projectDefinition := projectTool.Definition()
	projectParameters := projectDefinition.Function.Parameters.(map[string]interface{})
	projectRequired := projectParameters["required"].([]string)
	found := false
	for _, field := range projectRequired {
		if field == "projectId" {
			found = true
		}
	}
	if !found {
		t.Errorf("project_memory should require projectId, required = %v", projectRequired)
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
	var batchResponse memoryBatchResponse
	if err := json.Unmarshal([]byte(result), &batchResponse); err != nil {
		t.Fatalf("unmarshal batch response: %v", err)
	}
	itemId := batchResponse.Results[0].Item["id"].(string)

	// Single get.
	result, err = tool.Execute(ctx, `{"action":"get","id":"`+itemId+`"}`)
	if err != nil {
		t.Fatalf("single get: %v", err)
	}
	var response memorySingleResponse
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("unmarshal list response: %v", err)
	}
	if response.Action != "get" {
		t.Errorf("action = %q, want get", response.Action)
	}
	if response.Item["title"] != "Single get test" {
		t.Errorf("title = %v, want 'Single get test'", response.Item["title"])
	}
	if response.Item["content"] != "hello world" {
		t.Errorf("content = %v, want 'hello world'", response.Item["content"])
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
	var response memorySingleResponse
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("unmarshal list response: %v", err)
	}
	if response.Action != "list" {
		t.Errorf("action = %q, want list", response.Action)
	}
	if len(response.Items) != 0 {
		t.Errorf("items = %v, want empty", response.Items)
	}

	// Add items via batch.
	if _, err := tool.Execute(ctx, `{"action":"batch","items":[{"op":"add","title":"A","content":"aaa","tags":["x"]},{"op":"add","title":"B","content":"bbb","tags":["y"]}]}`); err != nil {
		t.Fatalf("batch add: %v", err)
	}

	// List all.
	result, err = tool.Execute(ctx, `{"action":"list"}`)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("unmarshal list response: %v", err)
	}
	if len(response.Items) < 2 {
		t.Errorf("expected at least 2 items, got %d", len(response.Items))
	}

	// List with tags filter.
	result, err = tool.Execute(ctx, `{"action":"list","tags":["x"]}`)
	if err != nil {
		t.Fatalf("list with tags: %v", err)
	}
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("unmarshal filtered list response: %v", err)
	}
	if len(response.Items) != 1 {
		t.Errorf("expected 1 item with tag x, got %d", len(response.Items))
	}

	// List with maxResults.
	result, err = tool.Execute(ctx, `{"action":"list","maxResults":1}`)
	if err != nil {
		t.Fatalf("list with maxResults: %v", err)
	}
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("unmarshal limited list response: %v", err)
	}
	if len(response.Items) > 1 {
		t.Errorf("expected at most 1 item, got %d", len(response.Items))
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
	if _, err := tool.Execute(ctx, `{"action":"batch","items":[{"op":"add","title":"Pet preferences","content":"The user likes cats and kittens","tags":["pets"]},{"op":"add","title":"Work notes","content":"User prefers dark mode in all editors","tags":["work"]}]}`); err != nil {
		t.Fatalf("batch add: %v", err)
	}

	// Search for "cats".
	result, err := tool.Execute(ctx, `{"action":"search","query":"cats"}`)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	var response memorySingleResponse
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("unmarshal search response: %v", err)
	}
	if response.Action != "search" {
		t.Errorf("action = %q, want search", response.Action)
	}
	if len(response.Matches) == 0 {
		t.Fatal("search for 'cats' should find at least 1 match")
	}

	// Case-insensitive search.
	result, err = tool.Execute(ctx, `{"action":"search","query":"DARK MODE"}`)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("unmarshal search response: %v", err)
	}
	if len(response.Matches) == 0 {
		t.Error("case-insensitive search for 'DARK MODE' should find a match")
	}

	// Max results.
	result, err = tool.Execute(ctx, `{"action":"search","query":"user","maxResults":1}`)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("unmarshal search response: %v", err)
	}
	if len(response.Matches) > 1 {
		t.Errorf("expected at most 1 result, got %d", len(response.Matches))
	}

	// No match.
	result, err = tool.Execute(ctx, `{"action":"search","query":"nonexistent_xyz"}`)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("unmarshal search response: %v", err)
	}
	if len(response.Matches) != 0 {
		t.Errorf("expected no matches, got %d", len(response.Matches))
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
	var response memoryBatchResponse
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("parse batch add: %v", err)
	}
	if response.Action != "batch" {
		t.Errorf("action = %q, want batch", response.Action)
	}
	if response.Summary.Succeeded != 1 {
		t.Fatalf("add should succeed, summary = %+v", response.Summary)
	}
	itemId, ok := response.Results[0].Item["id"].(string)
	if !ok || itemId == "" {
		t.Fatal("add should return item with id")
	}
	if response.Results[0].Item["content"] != "The user likes cats" {
		t.Errorf("content = %v, want 'The user likes cats'", response.Results[0].Item["content"])
	}

	// Batch: get the item.
	result, err = tool.Execute(ctx, `{"action":"batch","items":[{"op":"get","id":"`+itemId+`"}]}`)
	if err != nil {
		t.Fatalf("batch get: %v", err)
	}
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("parse batch get: %v", err)
	}
	if response.Results[0].Item["title"] != "User preferences" {
		t.Errorf("title = %v, want 'User preferences'", response.Results[0].Item["title"])
	}

	// Batch: update the item.
	result, err = tool.Execute(ctx, `{"action":"batch","items":[{"op":"update","id":"`+itemId+`","content":"The user likes dogs now"}]}`)
	if err != nil {
		t.Fatalf("batch update: %v", err)
	}
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("parse batch update: %v", err)
	}
	if response.Results[0].Item["content"] != "The user likes dogs now" {
		t.Errorf("content = %v, want 'The user likes dogs now'", response.Results[0].Item["content"])
	}

	// Batch: delete the item.
	result, err = tool.Execute(ctx, `{"action":"batch","items":[{"op":"delete","id":"`+itemId+`"}]}`)
	if err != nil {
		t.Fatalf("batch delete: %v", err)
	}
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("parse batch delete: %v", err)
	}
	if !response.Results[0].Success || response.Results[0].Op != "delete" {
		t.Errorf("delete result = %+v, want success", response.Results[0])
	}

	// Batch: get after delete should fail in result, not error.
	result, err = tool.Execute(ctx, `{"action":"batch","items":[{"op":"get","id":"`+itemId+`"}]}`)
	if err != nil {
		t.Fatalf("batch get after delete: %v", err)
	}
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("parse batch get after delete: %v", err)
	}
	if response.Results[0].Success {
		t.Error("expected get after delete to fail")
	}
	if response.Results[0].Error == "" {
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
	var response memoryBatchResponse
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("unmarshal batch response: %v", err)
	}
	if response.Results[0].Success {
		t.Error("list op in batch should fail")
	}
	if response.Results[0].Error == "" {
		t.Error("list op in batch should have error message")
	}

	// search in batch should fail per-item.
	result, err = tool.Execute(ctx, `{"action":"batch","items":[{"op":"search"}]}`)
	if err != nil {
		t.Fatalf("batch: %v", err)
	}
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("unmarshal batch response: %v", err)
	}
	if response.Results[0].Success {
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
	var response memoryBatchResponse
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("unmarshal batch response: %v", err)
	}
	if response.Summary.Total != 2 || response.Summary.Succeeded != 2 {
		t.Fatalf("expected 2 successes, got %+v", response.Summary)
	}
	idA := response.Results[0].Item["id"].(string)
	idB := response.Results[1].Item["id"].(string)

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
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("unmarshal batch response: %v", err)
	}
	if response.Summary.Total != 3 || response.Summary.Succeeded != 3 {
		t.Fatalf("expected 3 successes, got %+v", response.Summary)
	}
	if response.Results[0].Op != "get" || response.Results[1].Op != "update" || response.Results[2].Op != "add" {
		t.Errorf("ops = %s/%s/%s, want get/update/add", response.Results[0].Op, response.Results[1].Op, response.Results[2].Op)
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
	var response memoryBatchResponse
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("unmarshal batch response: %v", err)
	}
	if response.Summary.Succeeded != 1 || response.Summary.Failed != 1 {
		t.Errorf("expected 1 success 1 failure, got %+v", response.Summary)
	}
	if response.Results[0].Success != true {
		t.Error("first item should succeed")
	}
	if response.Results[1].Success != false || response.Results[1].Error == "" {
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
	var response memoryBatchResponse
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("unmarshal user batch response: %v", err)
	}
	if response.Summary.Succeeded != 1 {
		t.Fatalf("expected 1 success, got %+v", response.Summary)
	}
	itemId := response.Results[0].Item["id"].(string)

	// Single get on user_memory.
	result, err = userTool.Execute(ctx, `{"action":"get","id":"`+itemId+`"}`)
	if err != nil {
		t.Fatalf("user_memory get: %v", err)
	}
	var singleResponse memorySingleResponse
	if err := json.Unmarshal([]byte(result), &singleResponse); err != nil {
		t.Fatalf("unmarshal user single response: %v", err)
	}
	if singleResponse.Item["content"] != "remember this preference" {
		t.Errorf("content = %v, want 'remember this preference'", singleResponse.Item["content"])
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

// --- Retrieve tests ---

func TestRetrieveBasic(t *testing.T) {
	ctx := setupMemoryStore(t)
	ctx = runners.ContextWithRunner(ctx, runners.NewRunner(ctx, "test-agent-retrieve", "", nil, models.Agent{}))
	registry := tools.NewEmptyToolRegistry()
	for _, tool := range createTools() {
		registry.Register(tool)
	}
	tool := registry.Get("agent_memory")

	// Add items.
	if _, err := tool.Execute(ctx, `{"action":"batch","items":[{"op":"add","title":"Golang notes","content":"Go is a statically typed language.\nIt has goroutines for concurrency.\nChannels are used for communication.","tags":["dev"]},{"op":"add","title":"Python notes","content":"Python is dynamically typed.\nIt has great libraries for data science.","tags":["dev"]}]}`); err != nil {
		t.Fatalf("batch add: %v", err)
	}

	// Retrieve "goroutines".
	result, err := tool.Execute(ctx, `{"action":"retrieve","query":"goroutines"}`)
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	var response struct {
		Action       string `json:"action"`
		TotalMatches int    `json:"totalMatches"`
		Snippets     []struct {
			ItemID  string   `json:"itemId"`
			Title   string   `json:"title"`
			Snippet string   `json:"snippet"`
			Score   float64  `json:"score"`
			Tags    []string `json:"tags"`
		} `json:"snippets"`
	}
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("unmarshal retrieve response: %v", err)
	}
	if response.Action != "retrieve" {
		t.Errorf("action = %q, want retrieve", response.Action)
	}
	if len(response.Snippets) == 0 {
		t.Fatal("expected at least 1 snippet")
	}
	if response.Snippets[0].Score <= 0 {
		t.Errorf("expected positive score, got %f", response.Snippets[0].Score)
	}
	if response.TotalMatches < 1 {
		t.Errorf("expected totalMatches >= 1, got %d", response.TotalMatches)
	}
}

func TestRetrieveTagFilter(t *testing.T) {
	ctx := setupMemoryStore(t)
	ctx = runners.ContextWithRunner(ctx, runners.NewRunner(ctx, "test-agent-retrieve-tag", "", nil, models.Agent{}))
	registry := tools.NewEmptyToolRegistry()
	for _, tool := range createTools() {
		registry.Register(tool)
	}
	tool := registry.Get("agent_memory")

	// Add items with different tags.
	if _, err := tool.Execute(ctx, `{"action":"batch","items":[{"op":"add","title":"Tagged A","content":"keyword alpha beta gamma","tags":["alpha"]},{"op":"add","title":"Tagged B","content":"keyword alpha beta gamma","tags":["beta"]}]}`); err != nil {
		t.Fatalf("batch add: %v", err)
	}

	// Retrieve with tag filter should only get items with that tag.
	result, err := tool.Execute(ctx, `{"action":"retrieve","query":"keyword alpha","tags":["alpha"]}`)
	if err != nil {
		t.Fatalf("retrieve with tags: %v", err)
	}
	var response struct {
		Snippets []struct {
			Tags []string `json:"tags"`
		} `json:"snippets"`
	}
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("unmarshal retrieve response: %v", err)
	}
	for _, snippet := range response.Snippets {
		found := false
		for _, tag := range snippet.Tags {
			if tag == "alpha" {
				found = true
			}
		}
		if !found {
			t.Errorf("snippet should have tag alpha, got %v", snippet.Tags)
		}
	}
}

func TestRetrieveEmptyQuery(t *testing.T) {
	ctx := setupMemoryStore(t)
	ctx = runners.ContextWithRunner(ctx, runners.NewRunner(ctx, "test-agent-retrieve-empty", "", nil, models.Agent{}))
	registry := tools.NewEmptyToolRegistry()
	for _, tool := range createTools() {
		registry.Register(tool)
	}
	tool := registry.Get("agent_memory")

	_, err := tool.Execute(ctx, `{"action":"retrieve","query":""}`)
	if err == nil {
		t.Error("retrieve with empty query should error")
	}
}

func TestRetrieveMaxResults(t *testing.T) {
	ctx := setupMemoryStore(t)
	ctx = runners.ContextWithRunner(ctx, runners.NewRunner(ctx, "test-agent-retrieve-max", "", nil, models.Agent{}))
	registry := tools.NewEmptyToolRegistry()
	for _, tool := range createTools() {
		registry.Register(tool)
	}
	tool := registry.Get("agent_memory")

	// Add item with many matching lines.
	if _, err := tool.Execute(ctx, `{"action":"batch","items":[{"op":"add","title":"Many lines","content":"match line one\nmatch line two\nmatch line three\nmatch line four\nmatch line five"}]}`); err != nil {
		t.Fatalf("batch add: %v", err)
	}

	result, err := tool.Execute(ctx, `{"action":"retrieve","query":"match line","maxResults":2,"contextLines":0}`)
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	var response struct {
		Snippets []struct{} `json:"snippets"`
	}
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("unmarshal retrieve response: %v", err)
	}
	if len(response.Snippets) > 2 {
		t.Errorf("expected at most 2 snippets, got %d", len(response.Snippets))
	}
}

// --- Summary tests ---

// stubSynthesizer returns a fixed JSON response for testing.
type stubSynthesizer struct {
	response string
	err      error
}

func (self *stubSynthesizer) Synthesize(_ context.Context, _ string, _ string) (string, error) {
	if self.err != nil {
		return "", self.err
	}
	return self.response, nil
}

const stubSummaryJSON = `{"summary":"User asked about Go programming language.","criticalFacts":{"decisions":["Use Go for the project"],"todos":["Learn goroutines"],"constraints":["Must support Go 1.21+"],"userPreferences":["Prefers concise answers"],"openQuestions":["Which IDE to use?"]}}`

func installStubSynthesizer(t *testing.T) {
	t.Helper()
	original := synthesizer
	synthesizer = &stubSynthesizer{response: stubSummaryJSON}
	t.Cleanup(func() { synthesizer = original })
}

func TestSummaryNoConversation(t *testing.T) {
	ctx := setupMemoryStore(t)
	installStubSynthesizer(t)
	// Runner with empty conversationId.
	ctx = runners.ContextWithRunner(ctx, runners.NewRunner(ctx, "test-agent-summary-noconv", "", nil, models.Agent{}))
	registry := tools.NewEmptyToolRegistry()
	for _, tool := range createTools() {
		registry.Register(tool)
	}
	tool := registry.Get("agent_memory")

	_, err := tool.Execute(ctx, `{"action":"summary"}`)
	if err == nil {
		t.Error("summary without conversationId should error")
	}
}

func TestSummaryBasic(t *testing.T) {
	ctx := setupMemoryStore(t)
	installStubSynthesizer(t)
	conversationId := "conv-summary-basic-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	ctx = runners.ContextWithRunner(ctx, runners.NewRunner(ctx, "test-agent-summary-basic", conversationId, nil, models.Agent{}))
	registry := tools.NewEmptyToolRegistry()
	for _, tool := range createTools() {
		registry.Register(tool)
	}

	// Create conversation messages.
	createTestMessages(t, ctx, conversationId, []testMessage{
		{role: "user", content: "Hello there"},
		{role: "assistant", content: "Hi! How can I help you today?"},
		{role: "user", content: "Tell me about Go"},
		{role: "assistant", content: "Go is a programming language. It was created at Google."},
	})

	tool := registry.Get("agent_memory")
	result, err := tool.Execute(ctx, `{"action":"summary"}`)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	var response struct {
		Action         string        `json:"action"`
		ConversationID string        `json:"conversationId"`
		MessageCount   int           `json:"messageCount"`
		Summary        string        `json:"summary"`
		CriticalFacts  criticalFacts `json:"criticalFacts"`
	}
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("unmarshal summary response: %v", err)
	}
	if response.Action != "summary" {
		t.Errorf("action = %q, want summary", response.Action)
	}
	if response.ConversationID != conversationId {
		t.Errorf("conversationId = %q, want %q", response.ConversationID, conversationId)
	}
	if response.MessageCount != 4 {
		t.Errorf("messageCount = %d, want 4", response.MessageCount)
	}
	if response.Summary == "" {
		t.Error("summary should not be empty")
	}
	if len(response.CriticalFacts.Decisions) == 0 {
		t.Error("criticalFacts.decisions should not be empty")
	}
	if len(response.CriticalFacts.Todos) == 0 {
		t.Error("criticalFacts.todos should not be empty")
	}
}

func TestSummaryRoleFilter(t *testing.T) {
	ctx := setupMemoryStore(t)
	installStubSynthesizer(t)
	conversationId := "conv-summary-roles-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	ctx = runners.ContextWithRunner(ctx, runners.NewRunner(ctx, "test-agent-summary-roles", conversationId, nil, models.Agent{}))
	registry := tools.NewEmptyToolRegistry()
	for _, tool := range createTools() {
		registry.Register(tool)
	}

	createTestMessages(t, ctx, conversationId, []testMessage{
		{role: "user", content: "Hello"},
		{role: "assistant", content: "Hi!"},
		{role: "user", content: "Bye"},
	})

	tool := registry.Get("agent_memory")
	result, err := tool.Execute(ctx, `{"action":"summary","roles":["user"]}`)
	if err != nil {
		t.Fatalf("summary with roles: %v", err)
	}
	var response struct {
		MessageCount int `json:"messageCount"`
	}
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("unmarshal summary response: %v", err)
	}
	if response.MessageCount != 2 {
		t.Errorf("messageCount = %d, want 2 (user only)", response.MessageCount)
	}
}

func TestSummaryPersistCompact(t *testing.T) {
	ctx := setupMemoryStore(t)
	installStubSynthesizer(t)
	conversationId := "conv-summary-persist-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	ctx = runners.ContextWithRunner(ctx, runners.NewRunner(ctx, "test-agent-summary-persist", conversationId, nil, models.Agent{}))
	registry := tools.NewEmptyToolRegistry()
	for _, tool := range createTools() {
		registry.Register(tool)
	}

	createTestMessages(t, ctx, conversationId, []testMessage{
		{role: "user", content: "Hello"},
		{role: "assistant", content: "Hi there!"},
	})

	tool := registry.Get("agent_memory")
	result, err := tool.Execute(ctx, `{"action":"summary","persist":{"title":"Test summary","tags":["summary"]}}`)
	if err != nil {
		t.Fatalf("summary with persist: %v", err)
	}

	var response struct {
		Action    string `json:"action"`
		Persisted struct {
			ItemID string `json:"itemId"`
		} `json:"persisted"`
	}
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("unmarshal summary persist response: %v", err)
	}
	if response.Persisted.ItemID == "" {
		t.Fatal("persisted.itemId should not be empty")
	}

	// Fetch the persisted item and verify it contains compact semantic summary.
	getResult, err := tool.Execute(ctx, `{"action":"get","id":"`+response.Persisted.ItemID+`"}`)
	if err != nil {
		t.Fatalf("get persisted item: %v", err)
	}
	var getResponse struct {
		Item map[string]interface{} `json:"item"`
	}
	if err := json.Unmarshal([]byte(getResult), &getResponse); err != nil {
		t.Fatalf("unmarshal persisted item response: %v", err)
	}
	content, ok := getResponse.Item["content"].(string)
	if !ok || content == "" {
		t.Fatal("persisted item content should not be empty")
	}

	// Verify the persisted content is valid JSON with summary + criticalFacts.
	var persisted structuredSummaryResult
	if err := json.Unmarshal([]byte(content), &persisted); err != nil {
		t.Fatalf("persisted content should be valid structured summary JSON: %v", err)
	}
	if persisted.Summary == "" {
		t.Error("persisted summary should not be empty")
	}
	if len(persisted.CriticalFacts.Decisions) == 0 {
		t.Error("persisted criticalFacts.decisions should not be empty")
	}
	if len(persisted.CriticalFacts.UserPreferences) == 0 {
		t.Error("persisted criticalFacts.userPreferences should not be empty")
	}
}

// --- Filter tests ---

func TestFilterNoConversation(t *testing.T) {
	ctx := setupMemoryStore(t)
	ctx = runners.ContextWithRunner(ctx, runners.NewRunner(ctx, "test-agent-filter-noconv", "", nil, models.Agent{}))
	registry := tools.NewEmptyToolRegistry()
	for _, tool := range createTools() {
		registry.Register(tool)
	}
	tool := registry.Get("agent_memory")

	_, err := tool.Execute(ctx, `{"action":"filter"}`)
	if err == nil {
		t.Error("filter without conversationId should error")
	}
}

func TestFilterByRole(t *testing.T) {
	ctx := setupMemoryStore(t)
	conversationId := "conv-filter-role-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	ctx = runners.ContextWithRunner(ctx, runners.NewRunner(ctx, "test-agent-filter-role", conversationId, nil, models.Agent{}))
	registry := tools.NewEmptyToolRegistry()
	for _, tool := range createTools() {
		registry.Register(tool)
	}

	createTestMessages(t, ctx, conversationId, []testMessage{
		{role: "user", content: "Hello"},
		{role: "assistant", content: "Hi there!"},
		{role: "user", content: "How are you?"},
		{role: "assistant", content: "I'm doing well."},
	})

	tool := registry.Get("agent_memory")
	result, err := tool.Execute(ctx, `{"action":"filter","roles":["user"]}`)
	if err != nil {
		t.Fatalf("filter: %v", err)
	}
	var response struct {
		Action       string `json:"action"`
		TotalMatched int    `json:"totalMatched"`
		Messages     []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("unmarshal filter response: %v", err)
	}
	if response.Action != "filter" {
		t.Errorf("action = %q, want filter", response.Action)
	}
	if response.TotalMatched != 2 {
		t.Errorf("totalMatched = %d, want 2", response.TotalMatched)
	}
	for _, message := range response.Messages {
		if message.Role != "user" {
			t.Errorf("expected role user, got %q", message.Role)
		}
	}
}

func TestFilterByKeyword(t *testing.T) {
	ctx := setupMemoryStore(t)
	conversationId := "conv-filter-kw-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	ctx = runners.ContextWithRunner(ctx, runners.NewRunner(ctx, "test-agent-filter-kw", conversationId, nil, models.Agent{}))
	registry := tools.NewEmptyToolRegistry()
	for _, tool := range createTools() {
		registry.Register(tool)
	}

	createTestMessages(t, ctx, conversationId, []testMessage{
		{role: "user", content: "I love cats"},
		{role: "assistant", content: "Cats are great pets!"},
		{role: "user", content: "I also like dogs"},
	})

	tool := registry.Get("agent_memory")
	result, err := tool.Execute(ctx, `{"action":"filter","keyword":"cats"}`)
	if err != nil {
		t.Fatalf("filter: %v", err)
	}
	var response struct {
		TotalMatched int `json:"totalMatched"`
		Messages     []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("unmarshal filter response: %v", err)
	}
	if response.TotalMatched != 2 {
		t.Errorf("totalMatched = %d, want 2", response.TotalMatched)
	}
}

func TestFilterMaxResults(t *testing.T) {
	ctx := setupMemoryStore(t)
	conversationId := "conv-filter-max-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	ctx = runners.ContextWithRunner(ctx, runners.NewRunner(ctx, "test-agent-filter-max", conversationId, nil, models.Agent{}))
	registry := tools.NewEmptyToolRegistry()
	for _, tool := range createTools() {
		registry.Register(tool)
	}

	createTestMessages(t, ctx, conversationId, []testMessage{
		{role: "user", content: "msg one"},
		{role: "user", content: "msg two"},
		{role: "user", content: "msg three"},
	})

	tool := registry.Get("agent_memory")
	result, err := tool.Execute(ctx, `{"action":"filter","maxResults":1}`)
	if err != nil {
		t.Fatalf("filter: %v", err)
	}
	var response struct {
		TotalMatched int        `json:"totalMatched"`
		Messages     []struct{} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("unmarshal filter response: %v", err)
	}
	if response.TotalMatched != 3 {
		t.Errorf("totalMatched = %d, want 3", response.TotalMatched)
	}
	if len(response.Messages) != 1 {
		t.Errorf("messages = %d, want 1 (truncated)", len(response.Messages))
	}
}

func TestFilterNoMatches(t *testing.T) {
	ctx := setupMemoryStore(t)
	conversationId := "conv-filter-none-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	ctx = runners.ContextWithRunner(ctx, runners.NewRunner(ctx, "test-agent-filter-none", conversationId, nil, models.Agent{}))
	registry := tools.NewEmptyToolRegistry()
	for _, tool := range createTools() {
		registry.Register(tool)
	}

	createTestMessages(t, ctx, conversationId, []testMessage{
		{role: "user", content: "Hello"},
	})

	tool := registry.Get("agent_memory")
	result, err := tool.Execute(ctx, `{"action":"filter","keyword":"nonexistent_xyz_999"}`)
	if err != nil {
		t.Fatalf("filter: %v", err)
	}
	var response struct {
		TotalMatched int        `json:"totalMatched"`
		Messages     []struct{} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("unmarshal filter response: %v", err)
	}
	if response.TotalMatched != 0 {
		t.Errorf("totalMatched = %d, want 0", response.TotalMatched)
	}
}

// --- Content extraction tests ---

func TestExtractTextContent(t *testing.T) {
	// Plain string.
	text := extractTextContent(json.RawMessage(`"Hello world"`))
	if text != "Hello world" {
		t.Errorf("plain string: got %q, want %q", text, "Hello world")
	}

	// Array of content blocks.
	blocks := `[{"type":"text","text":"Part one"},{"type":"image","url":"x.png"},{"type":"text","text":"Part two"}]`
	text = extractTextContent(json.RawMessage(blocks))
	if text != "Part one\nPart two" {
		t.Errorf("content blocks: got %q, want %q", text, "Part one\nPart two")
	}

	// Empty / null.
	text = extractTextContent(nil)
	if text != "" {
		t.Errorf("nil: got %q, want empty", text)
	}
	text = extractTextContent(json.RawMessage("null"))
	if text != "" {
		t.Errorf("null: got %q, want empty", text)
	}
}

// --- Test helpers ---

type testMessage struct {
	role    string
	content string
}

func createTestMessages(t *testing.T, ctx context.Context, conversationId string, msgs []testMessage) {
	t.Helper()
	err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		for _, message := range msgs {
			role := models.Role(message.role)
			contentJSON, _ := json.Marshal(message.content)
			now := time.Now()
			_, err := tx.CreateConversationMessage(ctx, &models.ConversationMessage{
				ConversationID: &conversationId,
				Role:           &role,
				Content:        json.RawMessage(contentJSON),
				CreatedAt:      &now,
			}, nil)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("create test messages: %v", err)
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
