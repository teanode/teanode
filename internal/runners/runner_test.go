package runners

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/prompts"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/store"
	storefs "github.com/teanode/teanode/internal/store/fsstore"
	toolregistry "github.com/teanode/teanode/internal/tools"
	"github.com/teanode/teanode/internal/util/ptrto"
)

// mockProviderRegistry creates a single-provider registry for testing with
// "mock" as the default provider.
func mockProviderRegistry(baseURL string) *providers.ProviderRegistry {
	mockProviders := []*models.ProviderConfiguration{{
		Name:    ptrto.Value("mock"),
		BaseURL: ptrto.Value(baseURL),
		APIKey:  ptrto.Value("test-key"),
	}}
	return providers.NewProviderRegistry(&models.ModelsConfiguration{
		Default:   ptrto.Value("mock:mock-model"),
		Providers: &mockProviders,
	})
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

func contextWithUserAndStore(userId string, persistenceStore store.Store) context.Context {
	ctx := models.ContextWithUserSessionToken(context.Background(), &models.User{ID: userId}, nil, nil)
	return store.ContextWithStore(ctx, persistenceStore)
}

type testConversationStore struct {
	persistenceStore store.Store
}

func newTestConversationStore(testing *testing.T, userId string, agentId string, defaultModel string) testConversationStore {
	testing.Helper()
	dataDirectory := testing.TempDir()
	openedStore, openError := storefs.Open(storefs.Options{DataDirectory: dataDirectory})
	if openError != nil {
		testing.Fatalf("opening store: %v", openError)
	}
	if migrateError := openedStore.Migrate(context.Background()); migrateError != nil {
		testing.Fatalf("migrating store: %v", migrateError)
	}
	if transactionError := openedStore.Transaction(context.Background(), func(ctx context.Context, transaction store.Transaction) error {
		username := userId
		admin := false
		if _, err := transaction.CreateUser(context.Background(), &models.User{
			ID:       userId,
			Username: &username,
			Admin:    &admin,
		}, nil, nil); err != nil && err != store.ErrAlreadyExists {
			return err
		}
		name := agentId
		if _, err := transaction.CreateAgent(context.Background(), &models.Agent{
			ID:   agentId,
			Name: &name,
		}, nil, nil); err != nil && err != store.ErrAlreadyExists {
			return err
		}
		return nil
	}); transactionError != nil {
		testing.Fatalf("seeding user and agent: %v", transactionError)
	}
	// Seed model configuration if a default model is provided.
	if defaultModel != "" {
		if transactionError := openedStore.Transaction(context.Background(), func(ctx context.Context, transaction store.Transaction) error {
			_, modifyError := transaction.ModifyConfiguration(ctx, func(configuration *models.Configuration) error {
				if configuration.Models == nil {
					configuration.Models = &models.ModelsConfiguration{}
				}
				configuration.Models.Default = &defaultModel
				return nil
			}, nil)
			return modifyError
		}); transactionError != nil {
			testing.Fatalf("seeding model configuration: %v", transactionError)
		}
	}
	testing.Cleanup(func() {
		_ = openedStore.Close()
	})
	return testConversationStore{
		persistenceStore: openedStore,
	}
}

func loadTestConversationMessages(testing *testing.T, persistenceStore store.Store, conversationId string) []*models.ConversationMessage {
	testing.Helper()
	var messages []*models.ConversationMessage
	loadError := persistenceStore.Transaction(context.Background(), func(ctx context.Context, transaction store.Transaction) error {
		items, listError := transaction.ListConversationMessages(ctx, conversationId, nil)
		if listError != nil {
			return listError
		}
		messages = items
		return nil
	})
	if loadError != nil {
		testing.Fatalf("loading conversation messages: %v", loadError)
	}
	return messages
}

// seedWorkspaceFile creates a workspace file in the store for testing.
func seedWorkspaceFile(testing *testing.T, openedStore store.Store, scope models.Scope, scopeId string, path string, content string) {
	testing.Helper()
	contentBytes := []byte(content)
	if err := openedStore.Transaction(context.Background(), func(ctx context.Context, transaction store.Transaction) error {
		_, createError := transaction.CreateWorkspaceFile(ctx, &models.WorkspaceFile{
			Scope:   &scope,
			ScopeID: &scopeId,
			Path:    &path,
			Content: &contentBytes,
		}, nil)
		return createError
	}); err != nil {
		testing.Fatalf("seeding workspace file %s: %v", path, err)
	}
}

// newSystemPromptTestContext creates a test context with a store, user, and agent for system prompt tests.
func newSystemPromptTestContext(testing *testing.T, userId string, agentId string) (context.Context, store.Store) {
	testing.Helper()
	openedStore, openError := storefs.Open(storefs.Options{DataDirectory: testing.TempDir()})
	if openError != nil {
		testing.Fatalf("opening store backend: %v", openError)
	}
	if migrateError := openedStore.Migrate(context.Background()); migrateError != nil {
		testing.Fatalf("migrating store backend: %v", migrateError)
	}
	testing.Cleanup(func() { _ = openedStore.Close() })

	admin := true
	username := userId
	if err := openedStore.Transaction(context.Background(), func(ctx context.Context, transaction store.Transaction) error {
		if _, createError := transaction.CreateUser(ctx, &models.User{
			ID:       userId,
			Username: &username,
			Admin:    &admin,
		}, nil, nil); createError != nil && createError != store.ErrAlreadyExists {
			return createError
		}
		name := agentId
		if _, createError := transaction.CreateAgent(ctx, &models.Agent{
			ID:   agentId,
			Name: &name,
		}, nil, nil); createError != nil && createError != store.ErrAlreadyExists {
			return createError
		}
		return nil
	}); err != nil {
		testing.Fatalf("seeding user/agent: %v", err)
	}

	ctx := store.ContextWithStore(context.Background(), openedStore)
	ctx = models.ContextWithUserSessionToken(ctx, &models.User{ID: userId, Username: &username, Admin: &admin}, nil, nil)
	return ctx, openedStore
}

func ptrConversationMessage(message models.ConversationMessage) *models.ConversationMessage {
	return &message
}

func TestRunnerRun(t *testing.T) {
	mockResponse := "Hello! How can I help you today?"
	server := mockOpenAIServer(mockResponse)
	defer server.Close()

	testStore := newTestConversationStore(t, "user-1", "main", "mock:mock-model")

	runner := &Runner{
		AgentID:          "main",
		ConversationID:   "test-run",
		ProviderRegistry: mockProviderRegistry(server.URL),
		ToolRegistry:     toolregistry.NewEmptyToolRegistry(),
	}

	var chunks []string
	result, err := runner.Run(contextWithUserAndStore("user-1", testStore.persistenceStore), RunParams{
		Message: "hi",
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
	} else if result.Usage["totalTokens"] != 15 {
		t.Errorf("usage.totalTokens = %d, want 15", result.Usage["totalTokens"])
	}

	// Verify messages were persisted.
	messages := loadTestConversationMessages(t, testStore.persistenceStore, "test-run")
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
	if conversationMessageRole(*messages[0]) != "user" || conversationMessageContentText(*messages[0]) != "hi" {
		t.Errorf("msg[0] = %+v", messages[0])
	}
	if conversationMessageRole(*messages[1]) != "assistant" || conversationMessageContentText(*messages[1]) != mockResponse {
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

	testStore := newTestConversationStore(t, "user-1", "main", "mock:mock")

	runner := &Runner{
		AgentID:          "main",
		ConversationID:   "abort-test",
		ProviderRegistry: mockProviderRegistry(server.URL),
		ToolRegistry:     toolregistry.NewEmptyToolRegistry(),
	}

	ctx, cancel := context.WithCancel(contextWithUserAndStore("user-1", testStore.persistenceStore))

	gotChunk := make(chan struct{})
	done := make(chan struct{})
	var closeChunk sync.Once
	go func() {
		defer close(done)
		runner.Run(ctx, RunParams{
			Message: "abort me",
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
	var callCount atomic.Int32
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		// Read the request to check if it contains tool results.
		body, _ := io.ReadAll(request.Body)
		var chatRequest providers.ChatRequest
		json.Unmarshal(body, &chatRequest)

		writer.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := writer.(http.Flusher)

		currentCall := callCount.Add(1)
		if currentCall == 1 {
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

	testStore := newTestConversationStore(t, "user-1", "main", "mock:mock-model")

	toolRegistry := toolregistry.NewEmptyToolRegistry()
	toolRegistry.Register(&stubTool{name: "workspace"})

	runner := &Runner{
		AgentID:          "main",
		ConversationID:   "tool-test",
		ProviderRegistry: mockProviderRegistry(server.URL),
		ToolRegistry:     toolRegistry,
	}

	var toolCalls []string
	result, err := runner.Run(contextWithUserAndStore("user-1", testStore.persistenceStore), RunParams{
		Message: "remember hello",
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
	if result.Usage["totalTokens"] != 45 {
		t.Errorf("usage.totalTokens = %d, want 45", result.Usage["totalTokens"])
	}

	// Verify session has user + assistant(tool_call) + tool + assistant(text) = 4 messages.
	messages := loadTestConversationMessages(t, testStore.persistenceStore, "tool-test")
	if len(messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(messages))
	}
	if conversationMessageRole(*messages[0]) != "user" {
		t.Errorf("msg[0].role = %q, want user", conversationMessageRole(*messages[0]))
	}
	if conversationMessageRole(*messages[1]) != "assistant" {
		t.Errorf("msg[1].role = %q, want assistant", conversationMessageRole(*messages[1]))
	}
	if len(conversationMessageToolCalls(*messages[1])) == 0 {
		t.Error("msg[1] should have toolCalls")
	}
	if conversationMessageRole(*messages[2]) != "tool" {
		t.Errorf("msg[2].role = %q, want tool", conversationMessageRole(*messages[2]))
	}
	if messages[2].GetToolCallID() != "call-1" {
		t.Errorf("msg[2].toolCallId = %q, want call-1", messages[2].GetToolCallID())
	}
	if conversationMessageRole(*messages[3]) != "assistant" {
		t.Errorf("msg[3].role = %q, want assistant", conversationMessageRole(*messages[3]))
	}
}

func TestBuildSystemPromptWithWorkspace(t *testing.T) {
	ctx, openedStore := newSystemPromptTestContext(t, "user-1", "main")
	seedWorkspaceFile(t, openedStore, models.ScopeAgent, "main", "AGENT.md", "Be extra helpful")

	prompt := buildSystemPrompt(ctx, buildSystemPromptParameters{
		AgentID: "main",
		Mode:    SystemPromptModeFull,
	})

	// AGENT.md should be embedded in the system prompt.
	if !strings.Contains(prompt, "Be extra helpful") {
		t.Error("prompt should contain AGENT.md content")
	}
	if !strings.Contains(prompt, "Operating Instructions") {
		t.Error("prompt should have AGENT.md section header")
	}
}

func TestBuildSystemPromptWithoutWorkspace(t *testing.T) {
	ctx, _ := newSystemPromptTestContext(t, "user-1", "main")

	prompt := buildSystemPrompt(ctx, buildSystemPromptParameters{
		AgentID: "main",
		Mode:    SystemPromptModeFull,
	})
	if !strings.Contains(prompt, "TeaNode") {
		t.Error("prompt should contain TeaNode identifier")
	}
	if !strings.Contains(prompt, "workspace") {
		t.Error("prompt should mention workspace tool")
	}
}

func TestBuildSystemPromptUsesAgentIdentity(t *testing.T) {
	ctx, _ := newSystemPromptTestContext(t, "user-1", "custom")

	prompt := buildSystemPrompt(ctx, buildSystemPromptParameters{
		IdentityLine: resolveIdentityLine("custom", "Custom Assistant"),
		AgentID:      "custom",
		Mode:         SystemPromptModeFull,
	})
	if !strings.Contains(prompt, "You are 'Custom Assistant' (agent: custom).") {
		t.Error("prompt should contain agent identity suffix")
	}
	if !strings.Contains(prompt, "Workspace Tool") {
		t.Error("prompt should still contain tool documentation sections")
	}
}

func TestBuildSystemPromptTruncation(t *testing.T) {
	ctx, openedStore := newSystemPromptTestContext(t, "user-1", "main")

	big := strings.Repeat("x", 10000)
	seedWorkspaceFile(t, openedStore, models.ScopeAgent, "main", "AGENT.md", big)

	prompt := buildSystemPrompt(ctx, buildSystemPromptParameters{
		AgentID: "main",
		Mode:    SystemPromptModeFull,
	})
	if strings.Contains(prompt, strings.Repeat("x", 10000)) {
		t.Error("prompt should have truncated the large file")
	}
	if !strings.Contains(prompt, "... (truncated)") {
		t.Error("prompt should indicate truncation")
	}
}

func TestBuildSystemPromptIncludesRecentProjects(t *testing.T) {
	ctx, openedStore := newSystemPromptTestContext(t, "user-1", "main")

	if err := openedStore.Transaction(context.Background(), func(ctx context.Context, transaction store.Transaction) error {
		if _, createError := transaction.CreateProject(ctx, &models.Project{
			ID:          "project-roadmap",
			Name:        ptrto.Value("Roadmap"),
			Description: ptrto.Value("Plan roadmap milestones"),
		}, nil, nil); createError != nil {
			return createError
		}
		if _, createError := transaction.CreateProject(ctx, &models.Project{
			ID:          "project-research",
			Name:        ptrto.Value("Research"),
			Description: ptrto.Value("Collect and summarize findings"),
		}, nil, nil); createError != nil {
			return createError
		}
		return nil
	}); err != nil {
		t.Fatalf("project create: %v", err)
	}

	prompt := buildSystemPrompt(ctx, buildSystemPromptParameters{
		AgentID: "main",
		Mode:    SystemPromptModeFull,
	})
	if !strings.Contains(prompt, "Recent Projects") {
		t.Error("prompt should include recent projects section")
	}
	if !strings.Contains(prompt, "Roadmap") || !strings.Contains(prompt, "Research") {
		t.Error("prompt should include project names")
	}
}

func TestBuildSystemPromptIncludesUserWorkspaceFiles(t *testing.T) {
	ctx, openedStore := newSystemPromptTestContext(t, "user-1", "main")
	seedWorkspaceFile(t, openedStore, models.ScopeAgent, "main", "AGENT.md", "Agent operating notes")
	seedWorkspaceFile(t, openedStore, models.ScopeUser, "user-1", "USER.md", "Preferred name: Alex")
	seedWorkspaceFile(t, openedStore, models.ScopeUser, "user-1", "MEMORY.md", "Likes concise summaries")

	prompt := buildSystemPrompt(ctx, buildSystemPromptParameters{
		AgentID: "main",
		Mode:    SystemPromptModeFull,
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
	ctx, openedStore := newSystemPromptTestContext(t, "user-1", "main")

	withOnboarding := buildSystemPrompt(ctx, buildSystemPromptParameters{
		AgentID: "main",
		Mode:    SystemPromptModeFull,
	})
	if strings.Contains(withOnboarding, "Onboarding Notes (ONBOARDING.md)") {
		t.Fatal("prompt should not include ONBOARDING section when file is missing")
	}

	seedWorkspaceFile(t, openedStore, models.ScopeUser, "user-1", "ONBOARDING.md", "Ask about language and timezone")

	withOnboarding = buildSystemPrompt(ctx, buildSystemPromptParameters{
		AgentID: "main",
		Mode:    SystemPromptModeFull,
	})
	if !strings.Contains(withOnboarding, "Onboarding Notes (ONBOARDING.md)") {
		t.Fatal("prompt should include ONBOARDING section when file exists")
	}
	if !strings.Contains(withOnboarding, "Ask about language and timezone") {
		t.Fatal("prompt should include ONBOARDING.md content")
	}
}

func TestBuildMessagesIncludesSeededAssistantOnboardingAndPrompt(t *testing.T) {
	ctx, openedStore := newSystemPromptTestContext(t, "user-1", "default")
	onboardingInstructions := "Collect preferred name, verbosity, language, timezone, and goals."
	seedWorkspaceFile(t, openedStore, models.ScopeUser, "user-1", "ONBOARDING.md", onboardingInstructions)

	history := []*models.ConversationMessage{
		ptrConversationMessage(newTextMessage("assistant", "Welcome! To get started, tell me your preferred name and timezone.")),
		ptrConversationMessage(newTextMessage("user", "I'm Alex, PST timezone.")),
	}

	runner := &Runner{AgentID: "default"}
	messages := runner.buildMessages(
		ctx,
		history,
		"",
		SystemPromptModeFull,
		"",
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
	ctx, openedStore := newSystemPromptTestContext(t, "user-1", "default")
	seedWorkspaceFile(t, openedStore, models.ScopeAgent, "default", "AGENT.md", "Agent instructions")
	seedWorkspaceFile(t, openedStore, models.ScopeAgent, "default", "SKILLS.md", "Skill details")
	seedWorkspaceFile(t, openedStore, models.ScopeUser, "user-1", "USER.md", "Preferred name: Alex")
	seedWorkspaceFile(t, openedStore, models.ScopeUser, "user-1", "ONBOARDING.md", "Ask about timezone")

	prompt := buildSystemPrompt(ctx, buildSystemPromptParameters{
		AgentID:      "default",
		SkillPrompts: "<skill>demo</skill>",
		Mode:         SystemPromptModeMinimal,
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
	ctx, _ := newSystemPromptTestContext(t, "user-1", "default")

	prompt := buildSystemPrompt(ctx, buildSystemPromptParameters{
		AgentID: "default",
		Mode:    SystemPromptModeNone,
	})
	if strings.Contains(prompt, "TeaNode version:") {
		t.Error("none mode should return only identity line")
	}
	if !strings.Contains(prompt, prompts.DefaultIdentityLine) {
		t.Error("none mode should keep identity line")
	}
}

func TestBuildSystemPromptStableForSameInputs(t *testing.T) {
	ctx, openedStore := newSystemPromptTestContext(t, "user-1", "default")
	seedWorkspaceFile(t, openedStore, models.ScopeAgent, "default", "AGENT.md", "Stable agent instructions")
	seedWorkspaceFile(t, openedStore, models.ScopeUser, "user-1", "USER.md", "Preferred name: Alex")

	parameters := buildSystemPromptParameters{
		AgentID:      "default",
		SkillPrompts: "<skill>demo</skill>",
		Mode:         SystemPromptModeFull,
	}

	promptA := buildSystemPrompt(ctx, parameters)
	promptB := buildSystemPrompt(ctx, parameters)

	if promptA != promptB {
		t.Error("system prompt should be deterministic for identical inputs")
	}
}

func TestBuildSystemPromptIncludesOtherUsers(t *testing.T) {
	ctx, openedStore := newSystemPromptTestContext(t, "user-1", "main")

	if transactionError := openedStore.Transaction(context.Background(), func(ctx context.Context, transaction store.Transaction) error {
		bobUsername := "bob"
		bobAdmin := true
		_, createError := transaction.CreateUser(ctx, &models.User{
			ID:       "user-2",
			Username: &bobUsername,
			Admin:    &bobAdmin,
		}, nil, nil)
		return createError
	}); transactionError != nil {
		t.Fatalf("seeding users failed: %v", transactionError)
	}

	prompt := buildSystemPrompt(ctx, buildSystemPromptParameters{
		AgentID: "main",
		Mode:    SystemPromptModeFull,
	})
	if !strings.Contains(prompt, "Other Users") {
		t.Error("prompt should include other users section")
	}
	if !strings.Contains(prompt, "bob") {
		t.Error("prompt should list usernames from store-backed users")
	}
	if !strings.Contains(prompt, "role: admin") {
		t.Error("prompt should indicate admin users")
	}
}

func TestRunnerModelMismatchError(t *testing.T) {
	server := mockOpenAIServer("first response")
	defer server.Close()

	testStore := newTestConversationStore(t, "user-1", "main", "mock:mock-model")

	runner := &Runner{
		AgentID:          "main",
		ConversationID:   "mismatch-test",
		ProviderRegistry: mockProviderRegistry(server.URL),
		ToolRegistry:     toolregistry.NewEmptyToolRegistry(),
	}

	// First run: creates the conversation and locks it to "mock:mock-model".
	_, err := runner.Run(contextWithUserAndStore("user-1", testStore.persistenceStore), RunParams{
		Message: "hello",
	}, nil)
	if err != nil {
		t.Fatalf("first run failed: %v", err)
	}

	// Second run: same conversation, explicitly different model — should error.
	_, err = runner.Run(contextWithUserAndStore("user-1", testStore.persistenceStore), RunParams{
		Message: "hello again",
		Model:   "mock:other-model",
	}, nil)
	if err == nil {
		t.Fatal("expected model mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "model mismatch") {
		t.Errorf("expected 'model mismatch' error, got: %v", err)
	}
}

func TestRunnerNoModelError(t *testing.T) {
	// Create store without seeding any model configuration.
	testStore := newTestConversationStore(t, "user-1", "main", "")

	runner := &Runner{
		AgentID:          "main",
		ConversationID:   "no-model-test",
		ProviderRegistry: providers.NewEmptyProviderRegistry(),
	}

	_, err := runner.Run(contextWithUserAndStore("user-1", testStore.persistenceStore), RunParams{
		Message: "hello",
	}, nil)
	if err == nil {
		t.Fatal("expected 'no model configured' error, got nil")
	}
	if !strings.Contains(err.Error(), "no model configured") {
		t.Errorf("expected 'no model configured' error, got: %v", err)
	}
}

func TestRunnerRunRequiresUserID(t *testing.T) {
	testStore := newTestConversationStore(t, "user-1", "main", "")
	runner := &Runner{
		AgentID:          "main",
		ConversationID:   "missing-user-id",
		ProviderRegistry: providers.NewProviderRegistry(nil),
	}

	_, err := runner.Run(store.ContextWithStore(context.Background(), testStore.persistenceStore), RunParams{
		Message: "hello",
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "userId is required") {
		t.Fatalf("expected userId required error, got: %v", err)
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
