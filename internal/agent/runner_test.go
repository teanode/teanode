package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teanode/teanode/internal/config"
	"github.com/teanode/teanode/internal/provider"
	"github.com/teanode/teanode/internal/session"
)

// mockRegistry creates a single-provider registry for testing.
func mockRegistry(client *provider.Client) *provider.Registry {
	registry := provider.NewRegistry("mock")
	registry.Register("mock", client)
	return registry
}

// mockOpenAIServer returns an httptest.Server that serves a streaming chat completion.
func mockOpenAIServer(responseText string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := writer.(http.Flusher)

		// Send the response as a series of word chunks.
		words := strings.Fields(responseText)
		for index, word := range words {
			if index > 0 {
				word = " " + word
			}
			chunk := fmt.Sprintf(`{"id":"chatcmpl-test","model":"mock-model","choices":[{"index":0,"delta":{"content":%q},"finish_reason":null}]}`, word)
			fmt.Fprintf(writer, "data: %s\n\n", chunk)
			flusher.Flush()
		}

		// Final chunk with finish_reason.
		fmt.Fprintf(writer, "data: %s\n\n", `{"id":"chatcmpl-test","model":"mock-model","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`)
		fmt.Fprintf(writer, "data: [DONE]\n\n")
		flusher.Flush()
	}))
}

func TestRunnerRun(t *testing.T) {
	mockResponse := "Hello! How can I help you today?"
	server := mockOpenAIServer(mockResponse)
	defer server.Close()

	dir := t.TempDir()
	store := session.NewStore(dir)
	configuration := &config.Config{
		Models: config.ModelsConfig{
			Default:  "mock-model",
			Provider: "mock",
			BaseURL:  server.URL,
		},
	}
	providerClient := provider.NewClient(server.URL, "")

	runner := &Runner{
		Providers: mockRegistry(providerClient),
		Sessions:  store,
		Config:    configuration,
	}

	var chunks []string
	result, err := runner.Run(context.Background(), RunParams{
		SessionKey: "test-run",
		Message:    "hi",
	}, &RunCallbacks{
		OnTextDelta: func(text string) {
			chunks = append(chunks, text)
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if result.Response != mockResponse {
		t.Errorf("response = %q, want %q", result.Response, mockResponse)
	}
	if result.StopReason != "stop" {
		t.Errorf("stopReason = %q, want %q", result.StopReason, "stop")
	}
	if result.Model != "mock-model" {
		t.Errorf("model = %q, want %q", result.Model, "mock-model")
	}
	if len(chunks) == 0 {
		t.Error("expected chunks, got none")
	}
	if result.Usage == nil {
		t.Error("expected usage, got nil")
	} else if result.Usage.Total != 15 {
		t.Errorf("usage.total = %d, want 15", result.Usage.Total)
	}

	// Verify messages were persisted.
	messages, err := store.Load("test-run")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
	if messages[0].Role != "user" || messages[0].ContentText() != "hi" {
		t.Errorf("msg[0] = %+v", messages[0])
	}
	if messages[1].Role != "assistant" || messages[1].ContentText() != mockResponse {
		t.Errorf("msg[1] = %+v", messages[1])
	}
}

func TestRunnerRunAbort(t *testing.T) {
	// Create a server that blocks until context is cancelled.
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := writer.(http.Flusher)

		fmt.Fprintf(writer, "data: %s\n\n", `{"id":"chatcmpl-test","model":"mock","choices":[{"index":0,"delta":{"content":"partial"},"finish_reason":null}]}`)
		flusher.Flush()

		// Block until request context is done (simulating a long response).
		<-request.Context().Done()
	}))
	defer server.Close()

	dir := t.TempDir()
	store := session.NewStore(dir)
	configuration := &config.Config{
		Models: config.ModelsConfig{
			Default: "mock",
			BaseURL: server.URL,
		},
	}
	providerClient := provider.NewClient(server.URL, "")

	runner := &Runner{
		Providers: mockRegistry(providerClient),
		Sessions:  store,
		Config:    configuration,
	}

	ctx, cancel := context.WithCancel(context.Background())

	gotChunk := make(chan struct{})
	go func() {
		runner.Run(ctx, RunParams{
			SessionKey: "abort-test",
			Message:    "abort me",
		}, &RunCallbacks{
			OnTextDelta: func(text string) {
				close(gotChunk)
			},
		})
	}()

	// Wait for first chunk, then cancel.
	<-gotChunk
	cancel()
}

// mockToolCallServer simulates an LLM that makes a tool call then responds with text.
func mockToolCallServer(toolCallId, toolName, toolArgs, finalText string) *httptest.Server {
	callCount := 0
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		// Read the request to check if it contains tool results.
		body, _ := io.ReadAll(request.Body)
		var chatRequest provider.ChatRequest
		json.Unmarshal(body, &chatRequest)

		writer.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := writer.(http.Flusher)

		callCount++
		if callCount == 1 {
			// First call: return a tool call
			chunk := fmt.Sprintf(`{"id":"chatcmpl-1","model":"mock-model","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":%q,"type":"function","function":{"name":%q,"arguments":%q}}]},"finish_reason":null}]}`,
				toolCallId, toolName, toolArgs)
			fmt.Fprintf(writer, "data: %s\n\n", chunk)
			fmt.Fprintf(writer, "data: %s\n\n", `{"id":"chatcmpl-1","model":"mock-model","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`)
			fmt.Fprintf(writer, "data: [DONE]\n\n")
			flusher.Flush()
		} else {
			// Second call: return text response after tool results
			chunk := fmt.Sprintf(`{"id":"chatcmpl-2","model":"mock-model","choices":[{"index":0,"delta":{"content":%q},"finish_reason":null}]}`, finalText)
			fmt.Fprintf(writer, "data: %s\n\n", chunk)
			fmt.Fprintf(writer, "data: %s\n\n", `{"id":"chatcmpl-2","model":"mock-model","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":20,"completion_tokens":10,"total_tokens":30}}`)
			fmt.Fprintf(writer, "data: [DONE]\n\n")
			flusher.Flush()
		}
	}))
}

func TestRunnerToolCallLoop(t *testing.T) {
	server := mockToolCallServer("call-1", "memory_write", `{"path":"test.txt","content":"hello"}`, "Done! I saved that for you.")
	defer server.Close()

	dir := t.TempDir()
	memoryDirectory := t.TempDir()
	store := session.NewStore(dir)
	configuration := &config.Config{
		Models: config.ModelsConfig{
			Default:  "mock-model",
			Provider: "mock",
			BaseURL:  server.URL,
		},
	}
	providerClient := provider.NewClient(server.URL, "")

	tools := NewToolRegistry()
	RegisterMemoryTools(tools, memoryDirectory)

	runner := &Runner{
		Providers: mockRegistry(providerClient),
		Sessions:  store,
		Config:    configuration,
		Tools:     tools,
	}

	var toolCalls []string
	result, err := runner.Run(context.Background(), RunParams{
		SessionKey: "tool-test",
		Message:    "remember hello",
	}, &RunCallbacks{
		OnToolCall: func(name string, args string) {
			toolCalls = append(toolCalls, name)
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if result.Response != "Done! I saved that for you." {
		t.Errorf("response = %q", result.Response)
	}
	if len(toolCalls) != 1 || toolCalls[0] != "memory_write" {
		t.Errorf("toolCalls = %v, want [memory_write]", toolCalls)
	}

	// Usage should be accumulated across rounds.
	if result.Usage == nil {
		t.Fatal("expected usage, got nil")
	}
	if result.Usage.Total != 45 {
		t.Errorf("usage.total = %d, want 45", result.Usage.Total)
	}

	// Verify session has user + assistant(tool_call) + tool + assistant(text) = 4 messages.
	messages, err := store.Load("tool-test")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(messages))
	}
	if messages[0].Role != "user" {
		t.Errorf("msg[0].role = %q, want user", messages[0].Role)
	}
	if messages[1].Role != "assistant" {
		t.Errorf("msg[1].role = %q, want assistant", messages[1].Role)
	}
	if len(messages[1].ToolCalls) == 0 {
		t.Error("msg[1] should have toolCalls")
	}
	if messages[2].Role != "tool" {
		t.Errorf("msg[2].role = %q, want tool", messages[2].Role)
	}
	if messages[2].ToolCallID != "call-1" {
		t.Errorf("msg[2].toolCallId = %q, want call-1", messages[2].ToolCallID)
	}
	if messages[3].Role != "assistant" {
		t.Errorf("msg[3].role = %q, want assistant", messages[3].Role)
	}
}

func TestMemoryTools(t *testing.T) {
	memoryDirectory := t.TempDir()
	registry := NewToolRegistry()
	RegisterMemoryTools(registry, memoryDirectory)

	ctx := context.Background()

	// Test memory_list on empty dir.
	listTool := registry.Get("memory_list")
	if listTool == nil {
		t.Fatal("memory_list not registered")
	}
	result, err := listTool.Execute(ctx, "{}")
	if err != nil {
		t.Fatalf("memory_list: %v", err)
	}
	if result != "no files" {
		t.Errorf("memory_list = %q, want 'no files'", result)
	}

	// Test memory_write.
	writeTool := registry.Get("memory_write")
	if writeTool == nil {
		t.Fatal("memory_write not registered")
	}
	result, err = writeTool.Execute(ctx, `{"path":"notes/test.txt","content":"hello world"}`)
	if err != nil {
		t.Fatalf("memory_write: %v", err)
	}
	if result != "ok" {
		t.Errorf("memory_write = %q, want 'ok'", result)
	}

	// Test memory_read.
	readTool := registry.Get("memory_read")
	if readTool == nil {
		t.Fatal("memory_read not registered")
	}
	result, err = readTool.Execute(ctx, `{"path":"notes/test.txt"}`)
	if err != nil {
		t.Fatalf("memory_read: %v", err)
	}
	if result != "hello world" {
		t.Errorf("memory_read = %q, want 'hello world'", result)
	}

	// Test memory_list with files.
	result, err = listTool.Execute(ctx, "{}")
	if err != nil {
		t.Fatalf("memory_list: %v", err)
	}
	if result != "notes/test.txt" {
		t.Errorf("memory_list = %q, want 'notes/test.txt'", result)
	}

	// Test path traversal rejection.
	_, err = readTool.Execute(ctx, `{"path":"../../../etc/passwd"}`)
	if err == nil {
		t.Error("expected error for path traversal, got nil")
	}
}

func TestMemoryAppendTool(t *testing.T) {
	memoryDirectory := t.TempDir()
	registry := NewToolRegistry()
	RegisterMemoryTools(registry, memoryDirectory)

	ctx := context.Background()
	appendTool := registry.Get("memory_append")
	if appendTool == nil {
		t.Fatal("memory_append not registered")
	}

	// Append to a new file.
	result, err := appendTool.Execute(ctx, `{"path":"memory/2025-01-01.md","content":"- learned something"}`)
	if err != nil {
		t.Fatalf("memory_append: %v", err)
	}
	if result != "ok" {
		t.Errorf("memory_append = %q, want 'ok'", result)
	}

	// Append again.
	result, err = appendTool.Execute(ctx, `{"path":"memory/2025-01-01.md","content":"- learned more"}`)
	if err != nil {
		t.Fatalf("memory_append: %v", err)
	}

	// Read back and verify both entries.
	readTool := registry.Get("memory_read")
	result, err = readTool.Execute(ctx, `{"path":"memory/2025-01-01.md"}`)
	if err != nil {
		t.Fatalf("memory_read: %v", err)
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

func TestMemorySearchTool(t *testing.T) {
	memoryDirectory := t.TempDir()
	registry := NewToolRegistry()
	RegisterMemoryTools(registry, memoryDirectory)

	ctx := context.Background()
	writeTool := registry.Get("memory_write")
	searchTool := registry.Get("memory_search")
	if searchTool == nil {
		t.Fatal("memory_search not registered")
	}

	// Write some files.
	writeTool.Execute(ctx, `{"path":"notes.md","content":"The user likes cats\nThe user hates spam"}`)
	writeTool.Execute(ctx, `{"path":"memory/2025-01-01.md","content":"Discussed project alpha\nUser prefers dark mode"}`)

	// Search for "cats".
	result, err := searchTool.Execute(ctx, `{"query":"cats"}`)
	if err != nil {
		t.Fatalf("memory_search: %v", err)
	}
	if !strings.Contains(result, "notes.md:1:") || !strings.Contains(result, "cats") {
		t.Errorf("search result = %q, want match in notes.md", result)
	}

	// Search for "dark mode".
	result, err = searchTool.Execute(ctx, `{"query":"dark mode"}`)
	if err != nil {
		t.Fatalf("memory_search: %v", err)
	}
	if !strings.Contains(result, "2025-01-01.md") {
		t.Errorf("search result = %q, want match in daily log", result)
	}

	// Case-insensitive search.
	result, err = searchTool.Execute(ctx, `{"query":"CATS"}`)
	if err != nil {
		t.Fatalf("memory_search: %v", err)
	}
	if !strings.Contains(result, "cats") {
		t.Errorf("case-insensitive search = %q, want match", result)
	}

	// No match.
	result, err = searchTool.Execute(ctx, `{"query":"nonexistent"}`)
	if err != nil {
		t.Fatalf("memory_search: %v", err)
	}
	if result != "no matches" {
		t.Errorf("search result = %q, want 'no matches'", result)
	}

	// Max results.
	result, err = searchTool.Execute(ctx, `{"query":"user","max_results":1}`)
	if err != nil {
		t.Fatalf("memory_search: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(result), "\n")
	if len(lines) != 1 {
		t.Errorf("expected 1 result, got %d: %q", len(lines), result)
	}
}

func TestBuildSystemPromptWithWorkspace(t *testing.T) {
	workspaceDirectory := t.TempDir()
	configuration := &config.Config{}

	// Write workspace files.
	os.WriteFile(filepath.Join(workspaceDirectory, "AGENT.md"), []byte("Be extra helpful"), 0644)
	os.WriteFile(filepath.Join(workspaceDirectory, "MEMORY.md"), []byte("User likes Go"), 0644)

	prompt := BuildSystemPrompt(configuration, "", workspaceDirectory, "", config.DefaultAgentLimits.MaxWorkspaceFileChars)

	if !strings.Contains(prompt, "Be extra helpful") {
		t.Error("prompt should contain AGENT.md content")
	}
	if !strings.Contains(prompt, "User likes Go") {
		t.Error("prompt should contain MEMORY.md content")
	}
	if !strings.Contains(prompt, "Operating Instructions") {
		t.Error("prompt should have AGENT.md section header")
	}
	if !strings.Contains(prompt, "Long-term Memory") {
		t.Error("prompt should have MEMORY.md section header")
	}
}

func TestBuildSystemPromptWithoutWorkspace(t *testing.T) {
	configuration := &config.Config{}

	// Empty workspace dir — no files loaded.
	prompt := BuildSystemPrompt(configuration, "", "", "", config.DefaultAgentLimits.MaxWorkspaceFileChars)
	if !strings.Contains(prompt, "TeaNode") {
		t.Error("prompt should contain TeaNode identifier")
	}
	if !strings.Contains(prompt, "memory_append") {
		t.Error("prompt should mention memory_append tool")
	}
}

func TestBuildSystemPromptCustomOverride(t *testing.T) {
	configuration := &config.Config{
		SystemPrompt: "I am a custom assistant.",
	}
	prompt := BuildSystemPrompt(configuration, "", "/some/dir", "", config.DefaultAgentLimits.MaxWorkspaceFileChars)
	if !strings.Contains(prompt, "I am a custom assistant.") {
		t.Error("prompt should contain custom identity line")
	}
	// The custom SystemPrompt replaces the identity line, not the entire prompt.
	if !strings.Contains(prompt, "Memory Tools") {
		t.Error("prompt should still contain tool documentation sections")
	}
}

func TestBuildSystemPromptTruncation(t *testing.T) {
	workspaceDirectory := t.TempDir()
	configuration := &config.Config{}

	// Write a large file (> maxWorkspaceFileChars).
	big := strings.Repeat("x", 10000)
	os.WriteFile(filepath.Join(workspaceDirectory, "AGENT.md"), []byte(big), 0644)

	prompt := BuildSystemPrompt(configuration, "", workspaceDirectory, "", config.DefaultAgentLimits.MaxWorkspaceFileChars)
	if strings.Contains(prompt, strings.Repeat("x", 10000)) {
		t.Error("prompt should have truncated the large file")
	}
	if !strings.Contains(prompt, "... (truncated)") {
		t.Error("prompt should indicate truncation")
	}
}
