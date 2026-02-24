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
	"sync"
	"testing"

	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/conversations"
	projectstore "github.com/teanode/teanode/internal/projects"
	"github.com/teanode/teanode/internal/prompts"
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

func testResolveUserConfig(_ string) (*configs.UserConfig, error) {
	return &configs.UserConfig{Name: "Test User"}, nil
}

func testResolveConversations(store *conversations.Store) func(userId, agentId string) *conversations.Store {
	return func(userId, agentId string) *conversations.Store {
		return store
	}
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
		Providers:            mockRegistry(provider),
		ResolveConversations: testResolveConversations(store),
		ResolveUserConfig:    testResolveUserConfig,
		Config:               configuration,
	}

	var chunks []string
	result, err := runner.Run(ContextWithUserID(context.Background(), "user-1"), RunParams{
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
		Providers:            mockRegistry(provider),
		ResolveConversations: testResolveConversations(store),
		ResolveUserConfig:    testResolveUserConfig,
		Config:               configuration,
	}

	ctx, cancel := context.WithCancel(ContextWithUserID(context.Background(), "user-1"))

	gotChunk := make(chan struct{})
	done := make(chan struct{})
	var closeChunk sync.Once
	go func() {
		defer close(done)
		runner.Run(ctx, RunParams{
			ConversationID: "abort-test",
			Message:        "abort me",
		}, &RunCallbacks{
			OnTextDelta: func(text string) {
				closeChunk.Do(func() { close(gotChunk) })
			},
		})
	}()

	// Wait for first chunk, then cancel.
	<-gotChunk
	cancel()
	<-done
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
		Providers:            mockRegistry(provider),
		ResolveConversations: testResolveConversations(store),
		ResolveUserConfig:    testResolveUserConfig,
		Config:               configuration,
		Tools:                tools,
	}

	var toolCalls []string
	result, err := runner.Run(ContextWithUserID(context.Background(), "user-1"), RunParams{
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

	prompt := buildSystemPrompt(buildSystemPromptParameters{
		Configuration:           configuration,
		AgentWorkspaceDirectory: workspaceDirectory,
		MaxWorkspaceFileChars:   configs.DefaultAgentLimits.MaxWorkspaceFileChars,
		Mode:                    SystemPromptModeFull,
	})

	// AGENT.md should be embedded in the system prompt (rarely changes).
	if !strings.Contains(prompt, "Be extra helpful") {
		t.Error("prompt should contain AGENT.md content")
	}
	if !strings.Contains(prompt, "Operating Instructions") {
		t.Error("prompt should have AGENT.md section header")
	}

	// MEMORY.md is not inlined in the system prompt.
	if strings.Contains(prompt, "Long-term Memory") {
		t.Error("prompt should NOT contain MEMORY.md section (injected as user message)")
	}
}

func TestBuildSystemPromptWithoutWorkspace(t *testing.T) {
	configuration := &configs.Config{}

	// Empty workspace dir — no files loaded.
	prompt := buildSystemPrompt(buildSystemPromptParameters{
		Configuration:         configuration,
		MaxWorkspaceFileChars: configs.DefaultAgentLimits.MaxWorkspaceFileChars,
		Mode:                  SystemPromptModeFull,
	})
	if !strings.Contains(prompt, "TeaNode") {
		t.Error("prompt should contain TeaNode identifier")
	}
	if !strings.Contains(prompt, "workspace") {
		t.Error("prompt should mention workspace tool")
	}

	if strings.Contains(prompt, "Today's date:") {
		t.Error("prompt should not include dynamic date/time fields")
	}
}

func TestBuildSystemPromptUsesAgentIdentity(t *testing.T) {
	configuration := &configs.Config{
		AgentConfigs: []configs.AgentConfig{
			{ID: "custom", Name: "Custom Assistant"},
		},
	}
	prompt := buildSystemPrompt(buildSystemPromptParameters{
		Configuration:           configuration,
		AgentID:                 "custom",
		AgentWorkspaceDirectory: "/some/dir",
		MaxWorkspaceFileChars:   configs.DefaultAgentLimits.MaxWorkspaceFileChars,
		Mode:                    SystemPromptModeFull,
	})
	if !strings.Contains(prompt, "You are 'Custom Assistant' (agent: custom).") {
		t.Error("prompt should contain agent identity suffix")
	}
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

	prompt := buildSystemPrompt(buildSystemPromptParameters{
		Configuration:           configuration,
		AgentWorkspaceDirectory: workspaceDirectory,
		MaxWorkspaceFileChars:   configs.DefaultAgentLimits.MaxWorkspaceFileChars,
		Mode:                    SystemPromptModeFull,
	})
	if strings.Contains(prompt, strings.Repeat("x", 10000)) {
		t.Error("prompt should have truncated the large file")
	}
	if !strings.Contains(prompt, "... (truncated)") {
		t.Error("prompt should indicate truncation")
	}
}

func TestBuildSystemPromptIncludesRecentProjects(t *testing.T) {
	configs.SetDirectory(t.TempDir())
	t.Cleanup(func() { configs.SetDirectory("") })

	if _, err := projectstore.CreateProject("Roadmap", "Plan roadmap milestones", ""); err != nil {
		t.Fatalf("project create: %v", err)
	}
	if _, err := projectstore.CreateProject("Research", "Collect and summarize findings", ""); err != nil {
		t.Fatalf("project create: %v", err)
	}

	prompt := buildSystemPrompt(buildSystemPromptParameters{
		Configuration:         &configs.Config{},
		MaxWorkspaceFileChars: configs.DefaultAgentLimits.MaxWorkspaceFileChars,
		Mode:                  SystemPromptModeFull,
	})
	if !strings.Contains(prompt, "Recent Projects") {
		t.Error("prompt should include recent projects section")
	}
	if !strings.Contains(prompt, "Roadmap") || !strings.Contains(prompt, "Research") {
		t.Error("prompt should include project names")
	}
}

func TestBuildSystemPromptIncludesUserWorkspaceFiles(t *testing.T) {
	agentWorkspaceDirectory := t.TempDir()
	userWorkspaceDirectory := t.TempDir()
	configuration := &configs.Config{}

	if err := os.WriteFile(filepath.Join(agentWorkspaceDirectory, "AGENT.md"), []byte("Agent operating notes"), 0644); err != nil {
		t.Fatalf("write AGENT.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(userWorkspaceDirectory, "USER.md"), []byte("Preferred name: Alex"), 0644); err != nil {
		t.Fatalf("write USER.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(userWorkspaceDirectory, "MEMORY.md"), []byte("Likes concise summaries"), 0644); err != nil {
		t.Fatalf("write user MEMORY.md: %v", err)
	}

	prompt := buildSystemPrompt(buildSystemPromptParameters{
		Configuration:           configuration,
		AgentWorkspaceDirectory: agentWorkspaceDirectory,
		UserWorkspaceDirectory:  userWorkspaceDirectory,
		MaxWorkspaceFileChars:   configs.DefaultAgentLimits.MaxWorkspaceFileChars,
		Mode:                    SystemPromptModeFull,
	})
	if !strings.Contains(prompt, "Agent operating notes") {
		t.Error("prompt should include AGENT.md content")
	}
	if !strings.Contains(prompt, "User Profile (USER.md)") || !strings.Contains(prompt, "Preferred name: Alex") {
		t.Error("prompt should include USER.md section content")
	}
	if !strings.Contains(prompt, "Recall workflow") {
		t.Error("prompt should include memory recall workflow guidance")
	}
	if strings.Contains(prompt, "User Long-term Memory (MEMORY.md)") {
		t.Error("prompt should not inline user MEMORY.md content")
	}
}

func TestBuildSystemPromptIncludesOnboardingOnlyWhenPresent(t *testing.T) {
	configuration := &configs.Config{}
	agentWorkspaceDirectory := t.TempDir()
	userWorkspaceDirectory := t.TempDir()

	withOnboarding := buildSystemPrompt(buildSystemPromptParameters{
		Configuration:           configuration,
		AgentWorkspaceDirectory: agentWorkspaceDirectory,
		UserWorkspaceDirectory:  userWorkspaceDirectory,
		MaxWorkspaceFileChars:   configs.DefaultAgentLimits.MaxWorkspaceFileChars,
		Mode:                    SystemPromptModeFull,
	})
	if strings.Contains(withOnboarding, "Onboarding Notes (ONBOARDING.md)") {
		t.Fatal("prompt should not include ONBOARDING section when file is missing")
	}

	if err := os.WriteFile(filepath.Join(userWorkspaceDirectory, "ONBOARDING.md"), []byte("Ask about language and timezone"), 0644); err != nil {
		t.Fatalf("write ONBOARDING.md: %v", err)
	}

	withOnboarding = buildSystemPrompt(buildSystemPromptParameters{
		Configuration:           configuration,
		AgentWorkspaceDirectory: agentWorkspaceDirectory,
		UserWorkspaceDirectory:  userWorkspaceDirectory,
		MaxWorkspaceFileChars:   configs.DefaultAgentLimits.MaxWorkspaceFileChars,
		Mode:                    SystemPromptModeFull,
	})
	if !strings.Contains(withOnboarding, "Onboarding Notes (ONBOARDING.md)") {
		t.Fatal("prompt should include ONBOARDING section when file exists")
	}
	if !strings.Contains(withOnboarding, "Ask about language and timezone") {
		t.Fatal("prompt should include ONBOARDING.md content")
	}
}

func TestBuildMessagesIncludesSeededAssistantOnboardingAndPrompt(t *testing.T) {
	configuration := &configs.Config{}
	userWorkspaceDirectory := t.TempDir()
	onboardingInstructions := "Collect preferred name, verbosity, language, timezone, and goals."
	if err := os.WriteFile(filepath.Join(userWorkspaceDirectory, "ONBOARDING.md"), []byte(onboardingInstructions), 0644); err != nil {
		t.Fatalf("write ONBOARDING.md: %v", err)
	}

	history := []conversations.Message{
		conversations.NewTextMessage("assistant", "Welcome! To get started, tell me your preferred name and timezone.", 1),
		conversations.NewTextMessage("user", "I'm Alex, PST timezone.", 2),
	}

	runner := &Runner{AgentID: "default"}
	messages := runner.buildMessages(
		history,
		configs.DefaultAgentLimits,
		"",
		SystemPromptModeFull,
		configuration,
		"user-1",
		"",
		userWorkspaceDirectory,
		"",
		&configs.UserConfig{Name: "Alex"},
	)

	if len(messages) < 3 {
		t.Fatalf("expected at least 3 provider messages (system + history), got %d", len(messages))
	}
	if messages[0].Role != "system" {
		t.Fatalf("messages[0].role = %q, want system", messages[0].Role)
	}
	systemPrompt := messages[0].ContentText()
	if !strings.Contains(systemPrompt, "Onboarding Notes (ONBOARDING.md)") {
		t.Fatal("system prompt should include onboarding section when ONBOARDING.md exists")
	}
	if !strings.Contains(systemPrompt, onboardingInstructions) {
		t.Fatal("system prompt should include ONBOARDING.md content")
	}

	if messages[1].Role != "assistant" {
		t.Fatalf("messages[1].role = %q, want assistant", messages[1].Role)
	}
	if !strings.Contains(messages[1].ContentText(), "tell me your preferred name and timezone") {
		t.Fatalf("messages[1] content = %q, expected seeded onboarding question", messages[1].ContentText())
	}

	if messages[2].Role != "user" {
		t.Fatalf("messages[2].role = %q, want user", messages[2].Role)
	}
}

func TestBuildSystemPromptModeMinimal(t *testing.T) {
	agentWorkspaceDirectory := t.TempDir()
	userWorkspaceDirectory := t.TempDir()
	configuration := &configs.Config{}

	if err := os.WriteFile(filepath.Join(agentWorkspaceDirectory, "AGENT.md"), []byte("Agent instructions"), 0644); err != nil {
		t.Fatalf("write AGENT.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentWorkspaceDirectory, "SKILLS.md"), []byte("Skill details"), 0644); err != nil {
		t.Fatalf("write SKILLS.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(userWorkspaceDirectory, "USER.md"), []byte("Preferred name: Alex"), 0644); err != nil {
		t.Fatalf("write USER.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(userWorkspaceDirectory, "ONBOARDING.md"), []byte("Ask about timezone"), 0644); err != nil {
		t.Fatalf("write ONBOARDING.md: %v", err)
	}

	prompt := buildSystemPrompt(buildSystemPromptParameters{
		Configuration:           configuration,
		AgentID:                 "default",
		CurrentUserID:           "user-1",
		AgentWorkspaceDirectory: agentWorkspaceDirectory,
		UserWorkspaceDirectory:  userWorkspaceDirectory,
		SkillPrompts:            "<skill>demo</skill>",
		MaxWorkspaceFileChars:   configs.DefaultAgentLimits.MaxWorkspaceFileChars,
		Profile:                 &configs.UserConfig{Name: "Alex", Description: "Prefers concise responses"},
		Mode:                    SystemPromptModeMinimal,
	})

	if strings.Contains(prompt, "Current User Profile") {
		t.Error("minimal mode should omit current user profile section")
	}
	if strings.Contains(prompt, "User Profile (USER.md)") {
		t.Error("minimal mode should omit USER.md section")
	}
	if strings.Contains(prompt, "Onboarding Notes (ONBOARDING.md)") {
		t.Error("minimal mode should omit onboarding section")
	}
	if strings.Contains(prompt, "Learned Skills (SKILLS.md)") {
		t.Error("minimal mode should omit inlined SKILLS.md section")
	}
	if strings.Contains(prompt, "## Skills") {
		t.Error("minimal mode should omit skills prompt section")
	}
	if !strings.Contains(prompt, "Operating Instructions (AGENT.md)") {
		t.Error("minimal mode should keep AGENT.md section")
	}
	if !strings.Contains(prompt, "Workspace Tools") {
		t.Error("minimal mode should keep workspace tool guidance")
	}
}

func TestBuildSystemPromptModeNone(t *testing.T) {
	prompt := buildSystemPrompt(buildSystemPromptParameters{
		Configuration:         &configs.Config{},
		AgentID:               "default",
		MaxWorkspaceFileChars: configs.DefaultAgentLimits.MaxWorkspaceFileChars,
		Mode:                  SystemPromptModeNone,
	})
	if strings.Contains(prompt, "TeaNode version:") {
		t.Error("none mode should return only identity line")
	}
	if !strings.Contains(prompt, prompts.DefaultIdentityLine) {
		t.Error("none mode should keep identity line")
	}
}

func TestBuildSystemPromptStableForSameInputs(t *testing.T) {
	agentWorkspaceDirectory := t.TempDir()
	userWorkspaceDirectory := t.TempDir()
	configuration := &configs.Config{}

	if err := os.WriteFile(filepath.Join(agentWorkspaceDirectory, "AGENT.md"), []byte("Stable agent instructions"), 0644); err != nil {
		t.Fatalf("write AGENT.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(userWorkspaceDirectory, "USER.md"), []byte("Preferred name: Alex"), 0644); err != nil {
		t.Fatalf("write USER.md: %v", err)
	}

	promptA := buildSystemPrompt(buildSystemPromptParameters{
		Configuration:           configuration,
		AgentID:                 "default",
		CurrentUserID:           "user-1",
		AgentWorkspaceDirectory: agentWorkspaceDirectory,
		UserWorkspaceDirectory:  userWorkspaceDirectory,
		SkillPrompts:            "<skill>demo</skill>",
		MaxWorkspaceFileChars:   configs.DefaultAgentLimits.MaxWorkspaceFileChars,
		Profile:                 &configs.UserConfig{Name: "Alex"},
		Mode:                    SystemPromptModeFull,
	})
	promptB := buildSystemPrompt(buildSystemPromptParameters{
		Configuration:           configuration,
		AgentID:                 "default",
		CurrentUserID:           "user-1",
		AgentWorkspaceDirectory: agentWorkspaceDirectory,
		UserWorkspaceDirectory:  userWorkspaceDirectory,
		SkillPrompts:            "<skill>demo</skill>",
		MaxWorkspaceFileChars:   configs.DefaultAgentLimits.MaxWorkspaceFileChars,
		Profile:                 &configs.UserConfig{Name: "Alex"},
		Mode:                    SystemPromptModeFull,
	})

	if promptA != promptB {
		t.Error("system prompt should be deterministic for identical inputs")
	}
}

func TestBuildSystemPromptIncludesOtherUsers(t *testing.T) {
	configs.SetDirectory(t.TempDir())
	t.Cleanup(func() { configs.SetDirectory("") })

	securityConfig := &configs.SecurityConfig{
		Users: map[string]configs.SecurityUser{
			"user-1": {Username: "alice", Admin: true},
			"user-2": {Username: "bob"},
		},
	}
	if err := configs.SaveSecurity(securityConfig); err != nil {
		t.Fatalf("SaveSecurity failed: %v", err)
	}

	prompt := buildSystemPrompt(buildSystemPromptParameters{
		Configuration:         &configs.Config{},
		MaxWorkspaceFileChars: configs.DefaultAgentLimits.MaxWorkspaceFileChars,
		Mode:                  SystemPromptModeFull,
	})
	if !strings.Contains(prompt, "Other Users") {
		t.Error("prompt should include other users section")
	}
	if !strings.Contains(prompt, "alice") || !strings.Contains(prompt, "bob") {
		t.Error("prompt should list usernames from security config")
	}
	if !strings.Contains(prompt, "role: admin") {
		t.Error("prompt should indicate admin users")
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
		Providers:            mockRegistry(provider),
		ResolveConversations: testResolveConversations(store),
		ResolveUserConfig:    testResolveUserConfig,
		Config:               configuration,
	}

	// First run: creates the conversation and locks it to "mock:mock-model".
	_, err := runner.Run(ContextWithUserID(context.Background(), "user-1"), RunParams{
		ConversationID: "mismatch-test",
		Message:        "hello",
	}, nil)
	if err != nil {
		t.Fatalf("first run failed: %v", err)
	}

	// Second run: same conversation, explicitly different model — should error.
	_, err = runner.Run(ContextWithUserID(context.Background(), "user-1"), RunParams{
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
		Providers:            providers.NewRegistry("mock"),
		ResolveConversations: testResolveConversations(store),
		ResolveUserConfig:    testResolveUserConfig,
		Config:               configuration,
	}

	_, err := runner.Run(ContextWithUserID(context.Background(), "user-1"), RunParams{
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

func TestRunnerRunRequiresUserID(t *testing.T) {
	directory := t.TempDir()
	store := conversations.NewStore(directory)
	runner := &Runner{
		ResolveConversations: testResolveConversations(store),
		ResolveUserConfig:    testResolveUserConfig,
		Config:               &configs.Config{},
		Providers:            providers.NewRegistry("mock"),
	}

	_, err := runner.Run(context.Background(), RunParams{
		ConversationID: "missing-user-id",
		Message:        "hello",
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "userId is required") {
		t.Fatalf("expected userId required error, got: %v", err)
	}
}

func TestRunnerRunRequiresResolveUserConfig(t *testing.T) {
	directory := t.TempDir()
	store := conversations.NewStore(directory)
	runner := &Runner{
		ResolveConversations: testResolveConversations(store),
		Config:               &configs.Config{},
		Providers:            providers.NewRegistry("mock"),
	}

	_, err := runner.Run(ContextWithUserID(context.Background(), "user-1"), RunParams{
		ConversationID: "missing-resolver",
		Message:        "hello",
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "ResolveUserConfig is required") {
		t.Fatalf("expected ResolveUserConfig required error, got: %v", err)
	}
}

func TestValidateToolAuthorizationNonAdminShellDenied(t *testing.T) {
	err := validateToolAuthorization("shell", `{"command":"ls -la"}`, false)
	if err == nil || !strings.Contains(err.Error(), "admin access required") {
		t.Fatalf("expected admin access required error, got: %v", err)
	}
}

func TestValidateToolAuthorizationNonAdminGatewayDenied(t *testing.T) {
	err := validateToolAuthorization("gateway", `{"action":"restart"}`, false)
	if err == nil || !strings.Contains(err.Error(), "admin access required") {
		t.Fatalf("expected admin access required error, got: %v", err)
	}
}

func TestValidateToolAuthorizationNonAdminFilesystemReadOnly(t *testing.T) {
	allowedActions := []string{"read", "list", "info"}
	for _, action := range allowedActions {
		err := validateToolAuthorization("filesystem", `{"action":"`+action+`","path":"/tmp/x"}`, false)
		if err != nil {
			t.Fatalf("expected filesystem.%s allowed for non-admin, got: %v", action, err)
		}
	}

	deniedActions := []string{"write", "mkdir", "delete", "move"}
	for _, action := range deniedActions {
		err := validateToolAuthorization("filesystem", `{"action":"`+action+`","path":"/tmp/x"}`, false)
		if err == nil {
			t.Fatalf("expected filesystem.%s denied for non-admin", action)
		}
	}
}
