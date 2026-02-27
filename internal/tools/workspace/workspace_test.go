package workspace

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/runners"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/store/fsstore"
	"github.com/teanode/teanode/internal/tools"
)

func setupWorkspaceStore(t *testing.T) context.Context {
	t.Helper()
	openedStore, openError := fsstore.Open(fsstore.Options{DataDirectory: t.TempDir()})
	if openError != nil {
		t.Fatalf("open store: %v", openError)
	}
	t.Cleanup(func() { _ = openedStore.Close() })
	return store.ContextWithStore(context.Background(), openedStore)
}

func TestWorkspaceTools(t *testing.T) {
	ctx := setupWorkspaceStore(t)
	ctx = runners.ContextWithRunner(ctx, runners.NewRunner(ctx, "test-agent", "", nil, models.Agent{}))
	registry := tools.NewEmptyToolRegistry()
	for _, tool := range createTools() {
		registry.Register(tool)
	}

	tool := registry.Get("agent_workspace")
	if tool == nil {
		t.Fatal("agent_workspace not registered")
	}

	// Test list on empty dir.
	result, err := tool.Execute(ctx, `{"action":"list"}`)
	if err != nil {
		t.Fatalf("workspace list: %v", err)
	}
	var listResult struct {
		Action string   `json:"action"`
		Files  []string `json:"files"`
	}
	if err := json.Unmarshal([]byte(result), &listResult); err != nil {
		t.Fatalf("parsing list result: %v", err)
	}
	if listResult.Action != "list" {
		t.Errorf("list action = %q, want 'list'", listResult.Action)
	}
	if len(listResult.Files) != 0 {
		t.Errorf("list files = %v, want empty", listResult.Files)
	}

	// Test write.
	result, err = tool.Execute(ctx, `{"action":"write","path":"notes/test.txt","content":"hello world"}`)
	if err != nil {
		t.Fatalf("workspace write: %v", err)
	}
	var writeResult struct {
		Action  string `json:"action"`
		Success bool   `json:"success"`
	}
	if err := json.Unmarshal([]byte(result), &writeResult); err != nil {
		t.Fatalf("parsing write result: %v", err)
	}
	if writeResult.Action != "write" || !writeResult.Success {
		t.Errorf("write result = %+v, want action=write success=true", writeResult)
	}

	// Test read.
	result, err = tool.Execute(ctx, `{"action":"read","path":"notes/test.txt"}`)
	if err != nil {
		t.Fatalf("workspace read: %v", err)
	}
	var readResult struct {
		Action  string `json:"action"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(result), &readResult); err != nil {
		t.Fatalf("parsing read result: %v", err)
	}
	if readResult.Action != "read" || readResult.Content != "hello world" {
		t.Errorf("read result = %+v, want action=read content='hello world'", readResult)
	}

	// Test list with files.
	result, err = tool.Execute(ctx, `{"action":"list"}`)
	if err != nil {
		t.Fatalf("workspace list: %v", err)
	}
	if err := json.Unmarshal([]byte(result), &listResult); err != nil {
		t.Fatalf("parsing list result: %v", err)
	}
	if len(listResult.Files) != 1 || listResult.Files[0] != "notes/test.txt" {
		t.Errorf("list files = %v, want [notes/test.txt]", listResult.Files)
	}

	// Test path traversal rejection.
	_, err = tool.Execute(ctx, `{"action":"read","path":"../../../etc/passwd"}`)
	if err == nil {
		t.Error("expected error for path traversal, got nil")
	}
}

func TestWorkspaceAppendTool(t *testing.T) {
	ctx := setupWorkspaceStore(t)
	ctx = runners.ContextWithRunner(ctx, runners.NewRunner(ctx, "test-agent", "", nil, models.Agent{}))
	registry := tools.NewEmptyToolRegistry()
	for _, tool := range createTools() {
		registry.Register(tool)
	}

	tool := registry.Get("agent_workspace")
	if tool == nil {
		t.Fatal("agent_workspace not registered")
	}

	// Append to a new file.
	result, err := tool.Execute(ctx, `{"action":"append","path":"memory/2025-01-01.md","content":"- learned something"}`)
	if err != nil {
		t.Fatalf("workspace append: %v", err)
	}
	var appendResult struct {
		Action  string `json:"action"`
		Success bool   `json:"success"`
	}
	if err := json.Unmarshal([]byte(result), &appendResult); err != nil {
		t.Fatalf("parsing append result: %v", err)
	}
	if appendResult.Action != "append" || !appendResult.Success {
		t.Errorf("append result = %+v, want action=append success=true", appendResult)
	}

	// Append again.
	_, err = tool.Execute(ctx, `{"action":"append","path":"memory/2025-01-01.md","content":"- learned more"}`)
	if err != nil {
		t.Fatalf("workspace append: %v", err)
	}

	// Read back and verify both entries.
	result, err = tool.Execute(ctx, `{"action":"read","path":"memory/2025-01-01.md"}`)
	if err != nil {
		t.Fatalf("workspace read: %v", err)
	}
	var readResult struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(result), &readResult); err != nil {
		t.Fatalf("parsing read result: %v", err)
	}
	if readResult.Content == "" {
		t.Fatal("read content is empty")
	}
	content := readResult.Content
	if !(strings.Contains(content, "learned something") && strings.Contains(content, "learned more")) {
		t.Errorf("appended content = %q, want both entries", content)
	}

	// Test path traversal rejection.
	_, err = tool.Execute(ctx, `{"action":"append","path":"../../etc/evil","content":"bad"}`)
	if err == nil {
		t.Error("expected error for path traversal, got nil")
	}
}

func TestWorkspaceSearchTool(t *testing.T) {
	ctx := setupWorkspaceStore(t)
	ctx = runners.ContextWithRunner(ctx, runners.NewRunner(ctx, "test-agent", "", nil, models.Agent{}))
	registry := tools.NewEmptyToolRegistry()
	for _, tool := range createTools() {
		registry.Register(tool)
	}

	tool := registry.Get("agent_workspace")
	if tool == nil {
		t.Fatal("agent_workspace not registered")
	}

	// Write some files.
	tool.Execute(ctx, `{"action":"write","path":"notes.md","content":"The user likes cats\nThe user hates spam"}`)
	tool.Execute(ctx, `{"action":"write","path":"memory/2025-01-01.md","content":"Discussed project alpha\nUser prefers dark mode"}`)

	// Search for "cats".
	result, err := tool.Execute(ctx, `{"action":"search","query":"cats"}`)
	if err != nil {
		t.Fatalf("workspace search: %v", err)
	}
	var searchResult struct {
		Action  string `json:"action"`
		Matches []struct {
			Path string `json:"path"`
			Line int    `json:"line"`
			Text string `json:"text"`
		} `json:"matches"`
	}
	if err := json.Unmarshal([]byte(result), &searchResult); err != nil {
		t.Fatalf("parsing search result: %v", err)
	}
	if searchResult.Action != "search" {
		t.Errorf("search action = %q, want 'search'", searchResult.Action)
	}
	if len(searchResult.Matches) != 1 || searchResult.Matches[0].Path != "notes.md" {
		t.Errorf("search matches = %+v, want match in notes.md", searchResult.Matches)
	}
	if !strings.Contains(searchResult.Matches[0].Text, "cats") {
		t.Errorf("search match text = %q, want to contain 'cats'", searchResult.Matches[0].Text)
	}

	// Search for "dark mode".
	result, err = tool.Execute(ctx, `{"action":"search","query":"dark mode"}`)
	if err != nil {
		t.Fatalf("workspace search: %v", err)
	}
	if err := json.Unmarshal([]byte(result), &searchResult); err != nil {
		t.Fatalf("parsing search result: %v", err)
	}
	found := false
	for _, match := range searchResult.Matches {
		if strings.Contains(match.Path, "2025-01-01.md") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("search matches = %+v, want match in daily log", searchResult.Matches)
	}

	// Case-insensitive search.
	result, err = tool.Execute(ctx, `{"action":"search","query":"CATS"}`)
	if err != nil {
		t.Fatalf("workspace search: %v", err)
	}
	if err := json.Unmarshal([]byte(result), &searchResult); err != nil {
		t.Fatalf("parsing search result: %v", err)
	}
	if len(searchResult.Matches) == 0 || !strings.Contains(searchResult.Matches[0].Text, "cats") {
		t.Errorf("case-insensitive search = %+v, want match with 'cats'", searchResult.Matches)
	}

	// No match.
	result, err = tool.Execute(ctx, `{"action":"search","query":"nonexistent"}`)
	if err != nil {
		t.Fatalf("workspace search: %v", err)
	}
	if err := json.Unmarshal([]byte(result), &searchResult); err != nil {
		t.Fatalf("parsing search result: %v", err)
	}
	if len(searchResult.Matches) != 0 {
		t.Errorf("search matches = %+v, want empty", searchResult.Matches)
	}

	// Max results.
	result, err = tool.Execute(ctx, `{"action":"search","query":"user","maxResults":1}`)
	if err != nil {
		t.Fatalf("workspace search: %v", err)
	}
	if err := json.Unmarshal([]byte(result), &searchResult); err != nil {
		t.Fatalf("parsing search result: %v", err)
	}
	if len(searchResult.Matches) != 1 {
		t.Errorf("expected 1 result, got %d: %+v", len(searchResult.Matches), searchResult.Matches)
	}
}

func TestUserWorkspaceTool(t *testing.T) {
	ctx := setupWorkspaceStore(t)

	registry := tools.NewEmptyToolRegistry()
	for _, tool := range createTools() {
		registry.Register(tool)
	}

	userTool := registry.Get("user_workspace")
	if userTool == nil {
		t.Fatal("user_workspace not registered")
	}

	ctx = models.ContextWithUserSessionToken(ctx, &models.User{ID: "user-1"}, nil, nil)
	if _, err := userTool.Execute(ctx, `{"action":"append","path":"memory/2026-02-23.md","content":"remember this"}`); err != nil {
		t.Fatalf("user_workspace append: %v", err)
	}

	result, err := userTool.Execute(ctx, `{"action":"read","path":"memory/2026-02-23.md"}`)
	if err != nil {
		t.Fatalf("user_workspace read: %v", err)
	}
	var payload struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatalf("unmarshal read payload: %v", err)
	}
	if !strings.Contains(payload.Content, "remember this") {
		t.Fatalf("unexpected content: %q", payload.Content)
	}
}
