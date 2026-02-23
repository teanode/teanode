package v1api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/teanode/teanode/internal/agents"
	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/gw"
	"github.com/teanode/teanode/internal/prompts"
	"github.com/teanode/teanode/internal/sessions"
)

func newOnboardingTestAPI(t *testing.T, securityConfig *configs.SecurityConfig) *v1Api {
	t.Helper()
	registry := agents.NewAgentRegistry()
	registry.Register("main", &agents.Runner{AgentID: "main"})

	if securityConfig == nil {
		securityConfig = &configs.SecurityConfig{Users: map[string]configs.SecurityUser{}}
	}
	if securityConfig.Users == nil {
		securityConfig.Users = map[string]configs.SecurityUser{}
	}

	sessionStore := sessions.NewStore(t.TempDir())
	return New(
		gw.New(
			&configs.Config{
				AgentConfigs: []configs.AgentConfig{{ID: "main", Name: "Tea"}},
			},
			securityConfig,
			registry,
			nil,
			nil,
			nil,
			nil,
			nil,
			sessionStore,
		),
		func() {},
	)
}

func assertOnboardingSeeded(t *testing.T, api *v1Api, userId string) {
	t.Helper()

	workspaceDirectory, err := configs.UserWorkspaceDirectory(userId)
	if err != nil {
		t.Fatalf("UserWorkspaceDirectory failed: %v", err)
	}
	for _, filename := range []string{"USER.md", "ONBOARDING.md", "MEMORY.md"} {
		if _, err := os.Stat(filepath.Join(workspaceDirectory, filename)); err != nil {
			t.Fatalf("expected %s in workspace: %v", filename, err)
		}
	}

	agentId := "main"
	store := api.gateway.ConversationStore(userId, agentId)
	if store == nil {
		t.Fatal("conversation store is nil")
	}
	conversationList, err := store.List()
	if err != nil {
		t.Fatalf("list conversations: %v", err)
	}
	if len(conversationList) != 1 {
		t.Fatalf("expected one seeded conversation, got %d", len(conversationList))
	}
	conversationId := conversationList[0].ID

	defaultConversationId := api.gateway.EnsureDefaultConversation(userId, agentId)
	if defaultConversationId != conversationId {
		t.Fatalf("default conversation id = %q, want %q", defaultConversationId, conversationId)
	}

	messages, err := store.Load(conversationId)
	if err != nil {
		t.Fatalf("load conversation: %v", err)
	}
	if len(messages) == 0 {
		t.Fatal("expected seeded assistant message in onboarding conversation")
	}
	if messages[0].Role != "assistant" {
		t.Fatalf("first message role = %q, want assistant", messages[0].Role)
	}
	if messages[0].ContentText() != prompts.OnboardingSeedMessage {
		t.Fatalf("first message content = %q, want %q", messages[0].ContentText(), prompts.OnboardingSeedMessage)
	}
	if len(messages[0].Metadata) != 0 {
		t.Fatal("seeded onboarding message should not contain metadata marker")
	}
}

func TestAuthSetupSeedsOnboarding(t *testing.T) {
	withTempConfigDirectory(t)
	api := newOnboardingTestAPI(t, nil)

	body := bytes.NewBufferString(`{"username":"admin","password":"password123","name":"Admin User"}`)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/setup", body)
	response := httptest.NewRecorder()

	if err := api.handleAuthSetup(response, request); err != nil {
		t.Fatalf("handleAuthSetup failed: %v", err)
	}
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}

	securityConfig := api.gateway.SecurityConfig()
	if len(securityConfig.Users) != 1 {
		t.Fatalf("expected one created user, got %d", len(securityConfig.Users))
	}
	var userId string
	for id := range securityConfig.Users {
		userId = id
		break
	}
	if userId == "" {
		t.Fatal("created user id is empty")
	}

	assertOnboardingSeeded(t, api, userId)
}

func TestUsersCreateSeedsOnboarding(t *testing.T) {
	withTempConfigDirectory(t)
	securityConfig := &configs.SecurityConfig{
		Users: map[string]configs.SecurityUser{
			"user-1": {Username: "admin", Admin: true, PasswordHash: "hash"},
		},
	}
	api := newOnboardingTestAPI(t, securityConfig)

	connection, clientConnection, cleanup := newRPCWebSocketPair(t, api)
	defer cleanup()

	request := requestFrame{
		Type:   "req",
		ID:     "1",
		Method: "users.create",
		Params: json.RawMessage(`{"username":"alice","password":"password123","name":"Alice"}`),
	}
	go connection.dispatch(request)
	response := readRPCResponse(t, clientConnection)
	if !response.OK {
		t.Fatalf("users.create failed: %+v", response.Error)
	}

	rawPayload, err := json.Marshal(response.Payload)
	if err != nil {
		t.Fatalf("marshal response payload: %v", err)
	}
	var payload struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		t.Fatalf("unmarshal response payload: %v", err)
	}
	if payload.User.ID == "" {
		t.Fatal("users.create response missing user id")
	}

	assertOnboardingSeeded(t, api, payload.User.ID)
}
