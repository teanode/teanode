package agents

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
	"time"

	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/conversations"
	"github.com/teanode/teanode/internal/providers"
)

// mockRegistry creates a single-provider registry for testing.
func mockRegistry(provider providers.Provider) *providers.Registry {
	registry := providers.NewRegistry("mock")
	registry.Register("mock", provider)
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

// stubTool is a minimal tool for testing the runner tool-call loop.
type stubTool struct{ name string }

func (self *stubTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type:     "function",
		Function: providers.FunctionSpec{Name: self.name},
	}
}

func (self *stubTool) Execute(_ context.Context, _ string) (string, error) {
	return "ok", nil
}

func TestRunnerRun(t *testing.T) {
	mockResponse := "Hello! How can I help you today?"
	server := mockOpenAIServer(mockResponse)
	defer server.Close()

	directory := t.TempDir()
	store := conversations.NewStore(directory)
	configuration := &configs.Config{
		Models: configs.ModelsConfig{
			Default: "mock-model",
			Providers: []configs.ProviderConfig{
				{Name: "mock", BaseURL: server.URL},
			},
		},
	}
	provider := providers.NewClient(server.URL, "")

	runner := &Runner{
		Providers:     mockRegistry(provider),
		Conversations: store,
		Config:        configuration,
	}

	var chunks []string
	result, err := runner.Run(context.Background(), RunParams{
		ConversationID: "test-run",
		Message:        "hi",
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

	directory := t.TempDir()
	store := conversations.NewStore(directory)
	configuration := &configs.Config{
		Models: configs.ModelsConfig{
			Default: "mock",
			Providers: []configs.ProviderConfig{
				{Name: "mock", BaseURL: server.URL},
			},
		},
	}
	provider := providers.NewClient(server.URL, "")

	runner := &Runner{
		Providers:     mockRegistry(provider),
		Conversations: store,
		Config:        configuration,
	}

	ctx, cancel := context.WithCancel(context.Background())

	gotChunk := make(chan struct{})
	go func() {
		runner.Run(ctx, RunParams{
			ConversationID: "abort-test",
			Message:        "abort me",
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
		var chatRequest providers.ChatRequest
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
	server := mockToolCallServer("call-1", "workspace", `{"action":"write","path":"test.txt","content":"hello"}`, "Done! I saved that for you.")
	defer server.Close()

	directory := t.TempDir()
	store := conversations.NewStore(directory)
	configuration := &configs.Config{
		Models: configs.ModelsConfig{
			Default: "mock-model",
			Providers: []configs.ProviderConfig{
				{Name: "mock", BaseURL: server.URL},
			},
		},
	}
	provider := providers.NewClient(server.URL, "")

	tools := NewToolRegistry()
	tools.Register(&stubTool{name: "workspace"})

	runner := &Runner{
		Providers:     mockRegistry(provider),
		Conversations: store,
		Config:        configuration,
		Tools:         tools,
	}

	var toolCalls []string
	result, err := runner.Run(context.Background(), RunParams{
		ConversationID: "tool-test",
		Message:        "remember hello",
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
	if len(toolCalls) != 1 || toolCalls[0] != "workspace" {
		t.Errorf("toolCalls = %v, want [workspace]", toolCalls)
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

func TestBuildSystemPromptWithWorkspace(t *testing.T) {
	workspaceDirectory := t.TempDir()
	configuration := &configs.Config{}

	// Write workspace files.
	os.WriteFile(filepath.Join(workspaceDirectory, "AGENT.md"), []byte("Be extra helpful"), 0644)

	prompt := BuildSystemPrompt(configuration, "", workspaceDirectory, "", configs.DefaultAgentLimits.MaxWorkspaceFileChars)

	// AGENT.md should be embedded in the system prompt (rarely changes).
	if !strings.Contains(prompt, "Be extra helpful") {
		t.Error("prompt should contain AGENT.md content")
	}
	if !strings.Contains(prompt, "Operating Instructions") {
		t.Error("prompt should have AGENT.md section header")
	}

	// MEMORY.md and SKILLS.md are NOT in the system prompt — they're injected
	// as user messages in buildMessages() so they don't break the cache.
	if strings.Contains(prompt, "Long-term Memory") {
		t.Error("prompt should NOT contain MEMORY.md section (injected as user message)")
	}
}

func TestBuildSystemPromptWithoutWorkspace(t *testing.T) {
	configuration := &configs.Config{}

	// Empty workspace dir — no files loaded.
	prompt := BuildSystemPrompt(configuration, "", "", "", configs.DefaultAgentLimits.MaxWorkspaceFileChars)
	if !strings.Contains(prompt, "TeaNode") {
		t.Error("prompt should contain TeaNode identifier")
	}
	if !strings.Contains(prompt, "workspace") {
		t.Error("prompt should mention workspace tool")
	}

	// Should contain today's date and timezone.
	today := time.Now().Format("2006-01-02")
	if !strings.Contains(prompt, today) {
		t.Errorf("prompt should contain today's date %s", today)
	}
	tz := time.Now().Format("MST")
	if !strings.Contains(prompt, tz) {
		t.Errorf("prompt should contain timezone %s", tz)
	}
}

func TestBuildSystemPromptCustomOverride(t *testing.T) {
	configuration := &configs.Config{
		Agents: []configs.AgentConfig{
			{ID: "custom", SystemPrompt: "I am a custom assistant."},
		},
	}
	prompt := BuildSystemPrompt(configuration, "custom", "/some/dir", "", configs.DefaultAgentLimits.MaxWorkspaceFileChars)
	if !strings.Contains(prompt, "I am a custom assistant.") {
		t.Error("prompt should contain custom identity line")
	}
	// The custom SystemPrompt replaces the identity line, not the entire prompt.
	if !strings.Contains(prompt, "Workspace Tool") {
		t.Error("prompt should still contain tool documentation sections")
	}
}

func TestBuildSystemPromptTruncation(t *testing.T) {
	workspaceDirectory := t.TempDir()
	configuration := &configs.Config{}

	// Write a large file (> maxWorkspaceFileChars).
	big := strings.Repeat("x", 10000)
	os.WriteFile(filepath.Join(workspaceDirectory, "AGENT.md"), []byte(big), 0644)

	prompt := BuildSystemPrompt(configuration, "", workspaceDirectory, "", configs.DefaultAgentLimits.MaxWorkspaceFileChars)
	if strings.Contains(prompt, strings.Repeat("x", 10000)) {
		t.Error("prompt should have truncated the large file")
	}
	if !strings.Contains(prompt, "... (truncated)") {
		t.Error("prompt should indicate truncation")
	}
}

func TestRunnerModelMismatchError(t *testing.T) {
	server := mockOpenAIServer("first response")
	defer server.Close()

	directory := t.TempDir()
	store := conversations.NewStore(directory)
	configuration := &configs.Config{
		Models: configs.ModelsConfig{
			Default: "mock-model",
			Providers: []configs.ProviderConfig{
				{Name: "mock", BaseURL: server.URL},
			},
		},
	}
	provider := providers.NewClient(server.URL, "")

	runner := &Runner{
		Providers:     mockRegistry(provider),
		Conversations: store,
		Config:        configuration,
	}

	// First run: creates the conversation and locks it to "mock:mock-model".
	_, err := runner.Run(context.Background(), RunParams{
		ConversationID: "mismatch-test",
		Message:        "hello",
	}, nil)
	if err != nil {
		t.Fatalf("first run failed: %v", err)
	}

	// Second run: same conversation, explicitly different model — should error.
	_, err = runner.Run(context.Background(), RunParams{
		ConversationID: "mismatch-test",
		Message:        "hello again",
		Model:          "mock:other-model",
	}, nil)
	if err == nil {
		t.Fatal("expected model mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "model mismatch") {
		t.Errorf("expected 'model mismatch' error, got: %v", err)
	}
}

func TestRunnerNoModelError(t *testing.T) {
	directory := t.TempDir()
	store := conversations.NewStore(directory)
	configuration := &configs.Config{
		Models: configs.ModelsConfig{
			// No default model set.
		},
	}

	runner := &Runner{
		Providers:     providers.NewRegistry("mock"),
		Conversations: store,
		Config:        configuration,
	}

	_, err := runner.Run(context.Background(), RunParams{
		ConversationID: "no-model-test",
		Message:        "hello",
	}, nil)
	if err == nil {
		t.Fatal("expected 'no model configured' error, got nil")
	}
	if !strings.Contains(err.Error(), "no model configured") {
		t.Errorf("expected 'no model configured' error, got: %v", err)
	}
}
