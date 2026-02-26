package v1api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/teanode/teanode/internal/coordinators"
	"github.com/teanode/teanode/internal/gw"
	"github.com/teanode/teanode/internal/runners"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/prompts"
	"github.com/teanode/teanode/internal/store"
	storefs "github.com/teanode/teanode/internal/store/fsstore"
)

func newOnboardingTestAPI(t *testing.T, dataDirectory string, seededUsers []models.User) (*v1Api, store.Store) {
	t.Helper()
	openedStore, openError := storefs.Open(storefs.Options{DataDirectory: dataDirectory})
	if openError != nil {
		t.Fatalf("opening store backend: %v", openError)
	}
	if migrateError := openedStore.Migrate(context.Background()); migrateError != nil {
		t.Fatalf("migrating store backend: %v", migrateError)
	}
	t.Cleanup(func() { _ = openedStore.Close() })
	contextWithStore := store.ContextWithStore(context.Background(), openedStore)

	if seedError := openedStore.Transaction(context.Background(), func(ctx context.Context, transaction store.Transaction) error {
		if _, createAgentError := transaction.CreateAgent(ctx, &models.Agent{ID: "main"}, nil, nil); createAgentError != nil && createAgentError != store.ErrAlreadyExists {
			return createAgentError
		}
		for index := range seededUsers {
			user := seededUsers[index]
			if _, createUserError := transaction.CreateUser(context.Background(), &user, nil, nil); createUserError != nil && createUserError != store.ErrAlreadyExists {
				return createUserError
			}
		}
		return nil
	}); seedError != nil {
		t.Fatalf("seeding users into store: %v", seedError)
	}

	api := New(
		gw.New(
			contextWithStore,
			&models.Configuration{},
			coordinators.New(nil, nil),
			runners.NewDefaultConversationManager(contextWithStore),
			nil,
			nil,
			nil,
		),
	)
	return api, openedStore
}

func assertOnboardingSeeded(t *testing.T, api *v1Api, openedStore store.Store, userId string) {
	t.Helper()
	workspaceScope := models.ScopeUser
	for _, filename := range []string{"USER.md", "ONBOARDING.md", "MEMORY.md"} {
		fileName := filename
		getError := openedStore.Transaction(context.Background(), func(ctx context.Context, transaction store.Transaction) error {
			_, fileError := transaction.GetWorkspaceFileByPath(context.Background(), workspaceScope, userId, fileName, nil)
			return fileError
		})
		if getError != nil {
			t.Fatalf("expected %s in workspace: %v", filename, getError)
		}
	}

	agentId := "main"
	var conversations []*models.Conversation
	listError := openedStore.Transaction(context.Background(), func(ctx context.Context, transaction store.Transaction) error {
		items, listConversationsError := transaction.ListConversations(ctx, store.ConversationListOptions{
			UserID:  &userId,
			AgentID: &agentId,
		}, nil)
		if listConversationsError != nil {
			return listConversationsError
		}
		conversations = items
		return nil
	})
	if listError != nil {
		t.Fatalf("list conversations: %v", listError)
	}
	if len(conversations) != 1 {
		t.Fatalf("expected one seeded conversation, got %d", len(conversations))
	}
	conversationId := conversations[0].ID

	defaultConversationId := api.gateway.EnsureDefaultConversation(userId, agentId)
	if defaultConversationId != conversationId {
		t.Fatalf("default conversation id = %q, want %q", defaultConversationId, conversationId)
	}

	var messages []*models.ConversationMessage
	loadError := openedStore.Transaction(context.Background(), func(ctx context.Context, transaction store.Transaction) error {
		items, listMessagesError := transaction.ListConversationMessages(ctx, conversationId, nil)
		if listMessagesError != nil {
			return listMessagesError
		}
		messages = items
		return nil
	})
	if loadError != nil {
		t.Fatalf("load conversation: %v", loadError)
	}
	if len(messages) == 0 {
		t.Fatal("expected seeded assistant message in onboarding conversation")
	}
	if messages[0].Role == nil || *messages[0].Role != "assistant" {
		t.Fatalf("first message role = %v, want assistant", messages[0].Role)
	}
	expectedContent, _ := json.Marshal(prompts.OnboardingSeedMessage)
	if len(messages[0].Content) == 0 || string(messages[0].Content) != string(expectedContent) {
		t.Fatalf("first message content mismatch")
	}
	if len(messages[0].Metadata) != 0 {
		t.Fatal("seeded onboarding message should not contain metadata marker")
	}
}

func TestAuthSetupSeedsOnboarding(t *testing.T) {
	api, openedStore := newOnboardingTestAPI(t, t.TempDir(), nil)

	body := bytes.NewBufferString(`{"username":"admin","password":"password123","name":"Admin User"}`)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/setup", body)
	request = request.WithContext(store.ContextWithStore(request.Context(), openedStore))
	response := httptest.NewRecorder()

	if err := api.handleAuthSetup(response, request); err != nil {
		t.Fatalf("handleAuthSetup failed: %v", err)
	}
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}

	var userId string
	listError := openedStore.Transaction(context.Background(), func(ctx context.Context, transaction store.Transaction) error {
		users, getUsersError := transaction.ListUsers(context.Background(), nil)
		if getUsersError != nil {
			return getUsersError
		}
		for _, user := range users {
			if user.Admin != nil && *user.Admin {
				userId = user.ID
				return nil
			}
		}
		return nil
	})
	if listError != nil {
		t.Fatalf("listing users failed: %v", listError)
	}
	if userId == "" {
		t.Fatal("created admin user id is empty")
	}

	assertOnboardingSeeded(t, api, openedStore, userId)
}

func TestUsersCreateSeedsOnboarding(t *testing.T) {
	username := "admin"
	password := "hash"
	admin := true
	api, openedStore := newOnboardingTestAPI(t, t.TempDir(), []models.User{
		{
			ID:       "user-1",
			Username: &username,
			Password: &password,
			Admin:    &admin,
		},
	})

	connection, clientConnection, cleanup := newRPCWebSocketPair(t, api, openedStore)
	defer cleanup()
	connectionContext := store.ContextWithStore(context.Background(), openedStore)
	connection.ctx = models.ContextWithUserSessionToken(
		connectionContext,
		&models.User{ID: "user-1"},
		&models.Session{ID: "test-session"},
		nil,
	)

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

	assertOnboardingSeeded(t, api, openedStore, payload.User.ID)
}
