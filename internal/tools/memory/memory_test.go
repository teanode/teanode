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

	// Verify action enum is batch-only.
	params := def.Function.Parameters.(map[string]interface{})
	props := params["properties"].(map[string]interface{})
	actionProp := props["action"].(map[string]interface{})
	actionEnum := actionProp["enum"].([]string)
	if len(actionEnum) != 1 || actionEnum[0] != "batch" {
		t.Errorf("action enum = %v, want [batch]", actionEnum)
	}

	// Verify items property exists.
	if _, ok := props["items"]; !ok {
		t.Error("items property should exist in definition")
	}

	// Verify required includes action and items.
	required := params["required"].([]string)
	hasAction, hasItems := false, false
	for _, r := range required {
		if r == "action" {
			hasAction = true
		}
		if r == "items" {
			hasItems = true
		}
	}
	if !hasAction || !hasItems {
		t.Errorf("required = %v, want action and items", required)
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

func TestMemoryToolRejectsSingleActions(t *testing.T) {
	ctx := setupMemoryStore(t)
	ctx = runners.ContextWithRunner(ctx, runners.NewRunner(ctx, "test-agent-reject", "", nil, models.Agent{}))
	registry := tools.NewEmptyToolRegistry()
	for _, tool := range createTools() {
		registry.Register(tool)
	}

	agentTool := registry.Get("agent_memory")

	for _, action := range []string{"add", "update", "delete", "get", "list", "search"} {
		_, err := agentTool.Execute(ctx, `{"action":"`+action+`"}`)
		if err == nil {
			t.Errorf("action %q should be rejected, only batch is allowed", action)
		}
	}
}

func TestMemoryBatchCRUD(t *testing.T) {
	ctx := setupMemoryStore(t)
	ctx = runners.ContextWithRunner(ctx, runners.NewRunner(ctx, "test-agent", "", nil, models.Agent{}))
	registry := tools.NewEmptyToolRegistry()
	for _, tool := range createTools() {
		registry.Register(tool)
	}

	tool := registry.Get("agent_memory")
	if tool == nil {
		t.Fatal("agent_memory not registered")
	}

	// Batch: list on empty scope.
	result, err := tool.Execute(ctx, `{"action":"batch","items":[{"op":"list"}]}`)
	if err != nil {
		t.Fatalf("batch list: %v", err)
	}
	var resp memoryBatchResponse
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("parse batch list: %v", err)
	}
	if resp.Action != "batch" {
		t.Errorf("action = %q, want batch", resp.Action)
	}
	if resp.Summary.Total != 1 || resp.Summary.Succeeded != 1 {
		t.Errorf("summary = %+v, want total=1 succeeded=1", resp.Summary)
	}
	if !resp.Results[0].Success || resp.Results[0].Op != "list" {
		t.Errorf("list result = %+v, want success", resp.Results[0])
	}
	if len(resp.Results[0].Items) != 0 {
		t.Errorf("items = %v, want empty", resp.Results[0].Items)
	}

	// Batch: add an item.
	result, err = tool.Execute(ctx, `{"action":"batch","items":[{"op":"add","title":"User preferences","content":"The user likes cats","tags":["prefs","pets"]}]}`)
	if err != nil {
		t.Fatalf("batch add: %v", err)
	}
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("parse batch add: %v", err)
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

	// Batch: get after update.
	result, err = tool.Execute(ctx, `{"action":"batch","items":[{"op":"get","id":"`+itemID+`"}]}`)
	if err != nil {
		t.Fatalf("batch get after update: %v", err)
	}
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("parse batch get: %v", err)
	}
	if resp.Results[0].Item["content"] != "The user likes dogs now" {
		t.Errorf("content after update = %v, want 'The user likes dogs now'", resp.Results[0].Item["content"])
	}

	// Batch: list should have at least one item.
	result, err = tool.Execute(ctx, `{"action":"batch","items":[{"op":"list"}]}`)
	if err != nil {
		t.Fatalf("batch list: %v", err)
	}
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("parse batch list: %v", err)
	}
	if len(resp.Results[0].Items) < 1 {
		t.Errorf("list should have at least 1 item, got %d", len(resp.Results[0].Items))
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

func TestMemoryBatchSearch(t *testing.T) {
	ctx := setupMemoryStore(t)
	ctx = runners.ContextWithRunner(ctx, runners.NewRunner(ctx, "test-agent-search", "", nil, models.Agent{}))
	registry := tools.NewEmptyToolRegistry()
	for _, tool := range createTools() {
		registry.Register(tool)
	}

	tool := registry.Get("agent_memory")

	// Add items.
	tool.Execute(ctx, `{"action":"batch","items":[{"op":"add","title":"Pet preferences","content":"The user likes cats and kittens","tags":["pets"]},{"op":"add","title":"Work notes","content":"User prefers dark mode in all editors","tags":["work"]}]}`)

	// Search for "cats".
	result, err := tool.Execute(ctx, `{"action":"batch","items":[{"op":"search","query":"cats"}]}`)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	var resp memoryBatchResponse
	json.Unmarshal([]byte(result), &resp)
	if !resp.Results[0].Success {
		t.Fatalf("search should succeed, error: %s", resp.Results[0].Error)
	}
	matches := resp.Results[0].Matches.([]interface{})
	if len(matches) == 0 {
		t.Fatal("search for 'cats' should find at least 1 match")
	}

	// Case-insensitive search.
	result, err = tool.Execute(ctx, `{"action":"batch","items":[{"op":"search","query":"DARK MODE"}]}`)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	json.Unmarshal([]byte(result), &resp)
	matches = resp.Results[0].Matches.([]interface{})
	if len(matches) == 0 {
		t.Error("case-insensitive search for 'DARK MODE' should find a match")
	}

	// Max results.
	result, err = tool.Execute(ctx, `{"action":"batch","items":[{"op":"search","query":"user","maxResults":1}]}`)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	json.Unmarshal([]byte(result), &resp)
	matches = resp.Results[0].Matches.([]interface{})
	if len(matches) > 1 {
		t.Errorf("expected at most 1 result, got %d", len(matches))
	}

	// No match.
	result, err = tool.Execute(ctx, `{"action":"batch","items":[{"op":"search","query":"nonexistent_xyz"}]}`)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	json.Unmarshal([]byte(result), &resp)
	matches = resp.Results[0].Matches.([]interface{})
	if len(matches) != 0 {
		t.Errorf("expected no matches, got %d", len(matches))
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

	result, err = userTool.Execute(ctx, `{"action":"batch","items":[{"op":"get","id":"`+itemID+`"}]}`)
	if err != nil {
		t.Fatalf("user_memory batch get: %v", err)
	}
	json.Unmarshal([]byte(result), &resp)
	if resp.Results[0].Item["content"] != "remember this preference" {
		t.Errorf("content = %v, want 'remember this preference'", resp.Results[0].Item["content"])
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
	_, err := projectTool.Execute(context.Background(), `{"action":"batch","items":[{"op":"list"}]}`)
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
