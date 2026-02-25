package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	storefs "github.com/teanode/teanode/internal/store/fsstore"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
)

func testStoreWithBearer(t *testing.T, token string) store.Store {
	t.Helper()
	persistenceStore, err := storefs.Open(storefs.Options{DataDirectory: t.TempDir()})
	if err != nil {
		t.Fatalf("opening store: %v", err)
	}
	username := "alice"
	password := "set"
	admin := true
	if err := persistenceStore.Transaction(context.Background(), func(ctx context.Context, transaction store.Transaction) error {
		if _, err := transaction.CreateUser(context.Background(), &models.User{
			ID:       "user-1",
			Username: &username,
			Password: &password,
			Admin:    &admin,
		}, nil, nil); err != nil {
			return err
		}
		tokenValue := token
		_, err := transaction.CreateToken(context.Background(), &models.Token{
			ID:     "token-1",
			UserID: ptrto.Value("user-1"),
			Token:  &tokenValue,
		}, nil)
		return err
	}); err != nil {
		t.Fatalf("seeding store: %v", err)
	}
	return persistenceStore
}

func runThroughAuthMiddleware(persistenceStore store.Store, request *http.Request) *httptest.ResponseRecorder {
	request = request.WithContext(store.ContextWithStore(request.Context(), persistenceStore))
	recorder := httptest.NewRecorder()
	next := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	})
	MakeAuthenticationMiddleware()(next).ServeHTTP(recorder, request)
	return recorder
}

func runThroughAuthMiddlewareWithNext(persistenceStore store.Store, request *http.Request, next http.Handler) *httptest.ResponseRecorder {
	request = request.WithContext(store.ContextWithStore(request.Context(), persistenceStore))
	recorder := httptest.NewRecorder()
	MakeAuthenticationMiddleware()(next).ServeHTTP(recorder, request)
	return recorder
}

func TestAuthMiddleware_WebSocketAllowsBearerToken(t *testing.T) {
	persistenceStore := testStoreWithBearer(t, "token123")

	request := httptest.NewRequest(http.MethodGet, "/api/v1/websocket", nil)
	request.Header.Set("Authorization", "Bearer token123")
	response := runThroughAuthMiddleware(persistenceStore, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("websocket status = %d, want %d", response.Code, http.StatusNoContent)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/v1/websocket?token=token123", nil)
	response = runThroughAuthMiddleware(persistenceStore, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("websocket query-token status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestAuthMiddleware_WebSocketRequiresAuth(t *testing.T) {
	persistenceStore := testStoreWithBearer(t, "token123")
	request := httptest.NewRequest(http.MethodGet, "/api/v1/websocket", nil)
	response := runThroughAuthMiddleware(persistenceStore, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestAuthMiddleware_WebSocketBearerSetsUserContext(t *testing.T) {
	persistenceStore := testStoreWithBearer(t, "token123")

	request := httptest.NewRequest(http.MethodGet, "/api/v1/websocket", nil)
	request.Header.Set("Authorization", "Bearer token123")

	next := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		user := models.UserFromContext(request.Context())
		if user == nil {
			t.Fatal("expected user context")
		}
		if user.ID != "user-1" {
			t.Fatalf("user id = %q, want %q", user.ID, "user-1")
		}
		if session := models.SessionFromContext(request.Context()); session != nil {
			t.Fatalf("session = %+v, want nil", session)
		}
		writer.WriteHeader(http.StatusNoContent)
	})

	response := runThroughAuthMiddlewareWithNext(persistenceStore, request, next)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
}

func TestAuthMiddleware_WebSocketSessionSetsUserContext(t *testing.T) {
	persistenceStore, openError := storefs.Open(storefs.Options{DataDirectory: t.TempDir()})
	if openError != nil {
		t.Fatalf("opening store: %v", openError)
	}
	sessionId := security.NewULID()
	now := time.Now()
	expiresAt := now.Add(24 * time.Hour)
	err := persistenceStore.Transaction(context.Background(), func(ctx context.Context, transaction store.Transaction) error {
		username := "alice"
		password := "set"
		admin := true
		if _, createUserError := transaction.CreateUser(context.Background(), &models.User{
			ID:       "user-1",
			Username: &username,
			Password: &password,
			Admin:    &admin,
		}, nil, nil); createUserError != nil {
			return createUserError
		}
		_, createError := transaction.CreateSession(context.Background(), &models.Session{
			ID:            sessionId,
			UserID:        ptrto.Value("user-1"),
			UserAgent:     ptrto.Value("test-agent"),
			RemoteAddress: ptrto.Value("127.0.0.1"),
			ExpiresAt:     &expiresAt,
			CreatedAt:     &now,
			ModifiedAt:    &now,
		}, nil)
		return createError
	})
	if err != nil {
		t.Fatalf("creating session: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/websocket", nil)
	request.AddCookie(&http.Cookie{Name: "session", Value: sessionId})

	next := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		user := models.UserFromContext(request.Context())
		if user == nil {
			t.Fatal("expected user context")
		}
		if user.ID != "user-1" {
			t.Fatalf("user id = %q, want %q", user.ID, "user-1")
		}
		session := models.SessionFromContext(request.Context())
		if session == nil {
			t.Fatal("expected session context")
		}
		if session.ID != sessionId {
			t.Fatalf("session id = %q, want %q", session.ID, sessionId)
		}
		writer.WriteHeader(http.StatusNoContent)
	})

	response := runThroughAuthMiddlewareWithNext(persistenceStore, request, next)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
}
