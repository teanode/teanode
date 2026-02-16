package workspace

import (
	"context"
	"strings"
	"testing"

	"github.com/teanode/teanode/internal/agents"
)

func TestWorkspaceTools(t *testing.T) {
	memoryDirectory := t.TempDir()
	registry := agents.NewToolRegistry()
	RegisterTools(registry, memoryDirectory)

	ctx := context.Background()

	// Test workspace_list on empty dir.
	listTool := registry.Get("workspace_list")
	if listTool == nil {
		t.Fatal("workspace_list not registered")
	}
	result, err := listTool.Execute(ctx, "{}")
	if err != nil {
		t.Fatalf("workspace_list: %v", err)
	}
	if result != "no files" {
		t.Errorf("workspace_list = %q, want 'no files'", result)
	}

	// Test workspace_write.
	writeTool := registry.Get("workspace_write")
	if writeTool == nil {
		t.Fatal("workspace_write not registered")
	}
	result, err = writeTool.Execute(ctx, `{"path":"notes/test.txt","content":"hello world"}`)
	if err != nil {
		t.Fatalf("workspace_write: %v", err)
	}
	if result != "ok" {
		t.Errorf("workspace_write = %q, want 'ok'", result)
	}

	// Test workspace_read.
	readTool := registry.Get("workspace_read")
	if readTool == nil {
		t.Fatal("workspace_read not registered")
	}
	result, err = readTool.Execute(ctx, `{"path":"notes/test.txt"}`)
	if err != nil {
		t.Fatalf("workspace_read: %v", err)
	}
	if result != "hello world" {
		t.Errorf("workspace_read = %q, want 'hello world'", result)
	}

	// Test workspace_list with files.
	result, err = listTool.Execute(ctx, "{}")
	if err != nil {
		t.Fatalf("workspace_list: %v", err)
	}
	if result != "notes/test.txt" {
		t.Errorf("workspace_list = %q, want 'notes/test.txt'", result)
	}

	// Test path traversal rejection.
	_, err = readTool.Execute(ctx, `{"path":"../../../etc/passwd"}`)
	if err == nil {
		t.Error("expected error for path traversal, got nil")
	}
}

func TestWorkspaceAppendTool(t *testing.T) {
	memoryDirectory := t.TempDir()
	registry := agents.NewToolRegistry()
	RegisterTools(registry, memoryDirectory)

	ctx := context.Background()
	appendTool := registry.Get("workspace_append")
	if appendTool == nil {
		t.Fatal("workspace_append not registered")
	}

	// Append to a new file.
	result, err := appendTool.Execute(ctx, `{"path":"memory/2025-01-01.md","content":"- learned something"}`)
	if err != nil {
		t.Fatalf("workspace_append: %v", err)
	}
	if result != "ok" {
		t.Errorf("workspace_append = %q, want 'ok'", result)
	}

	// Append again.
	result, err = appendTool.Execute(ctx, `{"path":"memory/2025-01-01.md","content":"- learned more"}`)
	if err != nil {
		t.Fatalf("workspace_append: %v", err)
	}

	// Read back and verify both entries.
	readTool := registry.Get("workspace_read")
	result, err = readTool.Execute(ctx, `{"path":"memory/2025-01-01.md"}`)
	if err != nil {
		t.Fatalf("workspace_read: %v", err)
	}
	if !strings.Contains(result, "learned something") || !strings.Contains(result, "learned more") {
		t.Errorf("appended content = %q, want both entries", result)
	}

	// Test path traversal rejection.
	_, err = appendTool.Execute(ctx, `{"path":"../../etc/evil","content":"bad"}`)
	if err == nil {
		t.Error("expected error for path traversal, got nil")
	}
}

func TestWorkspaceSearchTool(t *testing.T) {
	memoryDirectory := t.TempDir()
	registry := agents.NewToolRegistry()
	RegisterTools(registry, memoryDirectory)

	ctx := context.Background()
	writeTool := registry.Get("workspace_write")
	searchTool := registry.Get("workspace_search")
	if searchTool == nil {
		t.Fatal("workspace_search not registered")
	}

	// Write some files.
	writeTool.Execute(ctx, `{"path":"notes.md","content":"The user likes cats\nThe user hates spam"}`)
	writeTool.Execute(ctx, `{"path":"memory/2025-01-01.md","content":"Discussed project alpha\nUser prefers dark mode"}`)

	// Search for "cats".
	result, err := searchTool.Execute(ctx, `{"query":"cats"}`)
	if err != nil {
		t.Fatalf("workspace_search: %v", err)
	}
	if !strings.Contains(result, "notes.md:1:") || !strings.Contains(result, "cats") {
		t.Errorf("search result = %q, want match in notes.md", result)
	}

	// Search for "dark mode".
	result, err = searchTool.Execute(ctx, `{"query":"dark mode"}`)
	if err != nil {
		t.Fatalf("workspace_search: %v", err)
	}
	if !strings.Contains(result, "2025-01-01.md") {
		t.Errorf("search result = %q, want match in daily log", result)
	}

	// Case-insensitive search.
	result, err = searchTool.Execute(ctx, `{"query":"CATS"}`)
	if err != nil {
		t.Fatalf("workspace_search: %v", err)
	}
	if !strings.Contains(result, "cats") {
		t.Errorf("case-insensitive search = %q, want match", result)
	}

	// No match.
	result, err = searchTool.Execute(ctx, `{"query":"nonexistent"}`)
	if err != nil {
		t.Fatalf("workspace_search: %v", err)
	}
	if result != "no matches" {
		t.Errorf("search result = %q, want 'no matches'", result)
	}

	// Max results.
	result, err = searchTool.Execute(ctx, `{"query":"user","max_results":1}`)
	if err != nil {
		t.Fatalf("workspace_search: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(result), "\n")
	if len(lines) != 1 {
		t.Errorf("expected 1 result, got %d: %q", len(lines), result)
	}
}
