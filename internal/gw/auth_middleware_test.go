package gw

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/sessions"
)

func testSecurityConfigWithBearer(token string) *configs.SecurityConfig {
	return &configs.SecurityConfig{
		Users: map[string]configs.SecurityUser{
			"user-1": {
				Username:     "alice",
				PasswordHash: "set",
				Tokens: []configs.SecurityToken{
					{
						ID:        "token-1",
						Token:     token,
						CreatedAt: time.Now(),
					},
				},
			},
		},
	}
}

func runThroughAuthMiddleware(g *gateway, request *http.Request) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	next := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	})
	g.AuthMiddleware()(next).ServeHTTP(recorder, request)
	return recorder
}

func runThroughAuthMiddlewareWithNext(g *gateway, request *http.Request, next http.Handler) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	g.AuthMiddleware()(next).ServeHTTP(recorder, request)
	return recorder
}

func TestAuthMiddleware_WebSocketAllowsBearerToken(t *testing.T) {
	t.Parallel()

	g := &gateway{
		config:         &configs.Config{},
		securityConfig: testSecurityConfigWithBearer("token123"),
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/websocket", nil)
	request.Header.Set("Authorization", "Bearer token123")
	response := runThroughAuthMiddleware(g, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("websocket status = %d, want %d", response.Code, http.StatusNoContent)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/v1/websocket?token=token123", nil)
	response = runThroughAuthMiddleware(g, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("websocket query-token status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestAuthMiddleware_WebSocketRequiresAuth(t *testing.T) {
	t.Parallel()

	g := &gateway{
		config:         &configs.Config{},
		securityConfig: testSecurityConfigWithBearer("token123"),
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/websocket", nil)
	response := runThroughAuthMiddleware(g, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestAuthMiddleware_WebSocketBearerSetsUserContext(t *testing.T) {
	t.Parallel()

	g := &gateway{
		config:         &configs.Config{},
		securityConfig: testSecurityConfigWithBearer("token123"),
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/websocket", nil)
	request.Header.Set("Authorization", "Bearer token123")

	next := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		userContext := UserFromContext(request.Context())
		if userContext == nil {
			t.Fatal("expected user context")
		}
		if userContext.UserID != "user-1" {
			t.Fatalf("user id = %q, want %q", userContext.UserID, "user-1")
		}
		if userContext.AuthMethod != AuthMethodToken {
			t.Fatalf("auth method = %q, want %q", userContext.AuthMethod, AuthMethodToken)
		}
		if userContext.SessionID != "" {
			t.Fatalf("session id = %q, want empty", userContext.SessionID)
		}
		writer.WriteHeader(http.StatusNoContent)
	})

	response := runThroughAuthMiddlewareWithNext(g, request, next)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
}

func TestAuthMiddleware_WebSocketSessionSetsUserContext(t *testing.T) {
	t.Parallel()

	store := sessions.NewStore(t.TempDir())
	session, err := store.Create("user-1", "test-agent", "127.0.0.1", 24*time.Hour)
	if err != nil {
		t.Fatalf("creating session: %v", err)
	}

	g := &gateway{
		config:         &configs.Config{},
		securityConfig: &configs.SecurityConfig{},
		sessionStore:   store,
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/websocket", nil)
	request.AddCookie(&http.Cookie{Name: "session", Value: session.ID})

	next := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		userContext := UserFromContext(request.Context())
		if userContext == nil {
			t.Fatal("expected user context")
		}
		if userContext.UserID != "user-1" {
			t.Fatalf("user id = %q, want %q", userContext.UserID, "user-1")
		}
		if userContext.AuthMethod != AuthMethodSession {
			t.Fatalf("auth method = %q, want %q", userContext.AuthMethod, AuthMethodSession)
		}
		if userContext.SessionID != session.ID {
			t.Fatalf("session id = %q, want %q", userContext.SessionID, session.ID)
		}
		writer.WriteHeader(http.StatusNoContent)
	})

	response := runThroughAuthMiddlewareWithNext(g, request, next)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
}
