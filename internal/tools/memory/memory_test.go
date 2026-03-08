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

	projectTool := registry.Get("project_memory")
	pDef := projectTool.Definition()
	params := pDef.Function.Parameters.(map[string]interface{})
	required := params["required"].([]string)
	found := false
	for _, r := range required {
		if r == "projectId" {
			found = true
		}
	}
	if !found {
		t.Errorf("project_memory should require projectId, required = %v", required)
	}
}

func TestMemoryTools(t *testing.T) {
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

	// List on empty scope.
	result, err := tool.Execute(ctx, `{"action":"list"}`)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var listResult struct {
		Action string                   `json:"action"`
		Items  []map[string]interface{} `json:"items"`
	}
	if err := json.Unmarshal([]byte(result), &listResult); err != nil {
		t.Fatalf("parse list: %v", err)
	}
	if listResult.Action != "list" {
		t.Errorf("action = %q, want list", listResult.Action)
	}
	if len(listResult.Items) != 0 {
		t.Errorf("items = %v, want empty", listResult.Items)
	}

	// Add an item.
	result, err = tool.Execute(ctx, `{"action":"add","title":"User preferences","content":"The user likes cats","tags":["prefs","pets"]}`)
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	var addResult struct {
		Action string                 `json:"action"`
		Item   map[string]interface{} `json:"item"`
	}
	if err := json.Unmarshal([]byte(result), &addResult); err != nil {
		t.Fatalf("parse add: %v", err)
	}
	if addResult.Action != "add" {
		t.Errorf("action = %q, want add", addResult.Action)
	}
	itemID, ok := addResult.Item["id"].(string)
	if !ok || itemID == "" {
		t.Fatal("add should return item with id")
	}
	if addResult.Item["content"] != "The user likes cats" {
		t.Errorf("content = %v, want 'The user likes cats'", addResult.Item["content"])
	}

	// Get the item.
	result, err = tool.Execute(ctx, `{"action":"get","id":"`+itemID+`"}`)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	var getResult struct {
		Action string                 `json:"action"`
		Item   map[string]interface{} `json:"item"`
	}
	if err := json.Unmarshal([]byte(result), &getResult); err != nil {
		t.Fatalf("parse get: %v", err)
	}
	if getResult.Action != "get" {
		t.Errorf("action = %q, want get", getResult.Action)
	}
	if getResult.Item["title"] != "User preferences" {
		t.Errorf("title = %v, want 'User preferences'", getResult.Item["title"])
	}

	// Update the item.
	result, err = tool.Execute(ctx, `{"action":"update","id":"`+itemID+`","content":"The user likes dogs now"}`)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	var updateResult struct {
		Action string                 `json:"action"`
		Item   map[string]interface{} `json:"item"`
	}
	if err := json.Unmarshal([]byte(result), &updateResult); err != nil {
		t.Fatalf("parse update: %v", err)
	}
	if updateResult.Item["content"] != "The user likes dogs now" {
		t.Errorf("content = %v, want 'The user likes dogs now'", updateResult.Item["content"])
	}

	// Get after update.
	result, err = tool.Execute(ctx, `{"action":"get","id":"`+itemID+`"}`)
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if err := json.Unmarshal([]byte(result), &getResult); err != nil {
		t.Fatalf("parse get: %v", err)
	}
	if getResult.Item["content"] != "The user likes dogs now" {
		t.Errorf("content after update = %v, want 'The user likes dogs now'", getResult.Item["content"])
	}

	// List should have one item.
	result, err = tool.Execute(ctx, `{"action":"list"}`)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if err := json.Unmarshal([]byte(result), &listResult); err != nil {
		t.Fatalf("parse list: %v", err)
	}
	if len(listResult.Items) < 1 {
		t.Errorf("list should have at least 1 item, got %d", len(listResult.Items))
	}

	// Delete the item.
	result, err = tool.Execute(ctx, `{"action":"delete","id":"`+itemID+`"}`)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	var deleteResult struct {
		Action  string `json:"action"`
		Success bool   `json:"success"`
	}
	if err := json.Unmarshal([]byte(result), &deleteResult); err != nil {
		t.Fatalf("parse delete: %v", err)
	}
	if deleteResult.Action != "delete" || !deleteResult.Success {
		t.Errorf("delete result = %+v, want action=delete success=true", deleteResult)
	}

	// Get after delete should error.
	_, err = tool.Execute(ctx, `{"action":"get","id":"`+itemID+`"}`)
	if err == nil {
		t.Error("expected error getting deleted item")
	}
}

func TestMemorySearchTool(t *testing.T) {
	ctx := setupMemoryStore(t)
	ctx = runners.ContextWithRunner(ctx, runners.NewRunner(ctx, "test-agent-search", "", nil, models.Agent{}))
	registry := tools.NewEmptyToolRegistry()
	for _, tool := range createTools() {
		registry.Register(tool)
	}

	tool := registry.Get("agent_memory")

	// Add items.
	tool.Execute(ctx, `{"action":"add","title":"Pet preferences","content":"The user likes cats and kittens","tags":["pets"]}`)
	tool.Execute(ctx, `{"action":"add","title":"Work notes","content":"User prefers dark mode in all editors","tags":["work"]}`)

	// Search for "cats".
	result, err := tool.Execute(ctx, `{"action":"search","query":"cats"}`)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	var searchResult struct {
		Action  string `json:"action"`
		Matches []struct {
			ID      string   `json:"id"`
			Title   string   `json:"title"`
			Snippet string   `json:"snippet"`
			Tags    []string `json:"tags"`
		} `json:"matches"`
	}
	if err := json.Unmarshal([]byte(result), &searchResult); err != nil {
		t.Fatalf("parse search: %v", err)
	}
	if searchResult.Action != "search" {
		t.Errorf("action = %q, want search", searchResult.Action)
	}
	if len(searchResult.Matches) == 0 {
		t.Fatal("search for 'cats' should find at least 1 match")
	}

	// Case-insensitive search.
	result, err = tool.Execute(ctx, `{"action":"search","query":"DARK MODE"}`)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if err := json.Unmarshal([]byte(result), &searchResult); err != nil {
		t.Fatalf("parse search: %v", err)
	}
	if len(searchResult.Matches) == 0 {
		t.Error("case-insensitive search for 'DARK MODE' should find a match")
	}

	// Max results.
	result, err = tool.Execute(ctx, `{"action":"search","query":"user","maxResults":1}`)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if err := json.Unmarshal([]byte(result), &searchResult); err != nil {
		t.Fatalf("parse search: %v", err)
	}
	if len(searchResult.Matches) > 1 {
		t.Errorf("expected at most 1 result, got %d", len(searchResult.Matches))
	}

	// No match.
	result, err = tool.Execute(ctx, `{"action":"search","query":"nonexistent_xyz"}`)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if err := json.Unmarshal([]byte(result), &searchResult); err != nil {
		t.Fatalf("parse search: %v", err)
	}
	if len(searchResult.Matches) != 0 {
		t.Errorf("expected no matches, got %d", len(searchResult.Matches))
	}
}

func TestUserMemoryTool(t *testing.T) {
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

	result, err := userTool.Execute(ctx, `{"action":"add","content":"remember this preference","title":"Test memory"}`)
	if err != nil {
		t.Fatalf("user_memory add: %v", err)
	}
	var addResult struct {
		Item map[string]interface{} `json:"item"`
	}
	if err := json.Unmarshal([]byte(result), &addResult); err != nil {
		t.Fatalf("parse add: %v", err)
	}
	itemID := addResult.Item["id"].(string)

	result, err = userTool.Execute(ctx, `{"action":"get","id":"`+itemID+`"}`)
	if err != nil {
		t.Fatalf("user_memory get: %v", err)
	}
	var getResult struct {
		Item map[string]interface{} `json:"item"`
	}
	if err := json.Unmarshal([]byte(result), &getResult); err != nil {
		t.Fatalf("parse get: %v", err)
	}
	if getResult.Item["content"] != "remember this preference" {
		t.Errorf("content = %v, want 'remember this preference'", getResult.Item["content"])
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
