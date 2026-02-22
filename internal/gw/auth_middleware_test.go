package gw

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/teanode/teanode/internal/configs"
)

func runThroughAuthMiddleware(g *gateway, request *http.Request) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	next := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	})
	g.AuthMiddleware()(next).ServeHTTP(recorder, request)
	return recorder
}

func TestAuthMiddleware_ProfileGetRequiresAuthWhenPasswordSet(t *testing.T) {
	t.Parallel()

	g := &gateway{
		config:         &configs.Config{},
		securityConfig: &configs.SecurityConfig{Password: "set"},
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/profile", nil)
	response := runThroughAuthMiddleware(g, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestAuthMiddleware_ProfileGetAllowsSetupFlowWithoutPassword(t *testing.T) {
	t.Parallel()

	g := &gateway{
		config:         &configs.Config{},
		securityConfig: &configs.SecurityConfig{},
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/profile", nil)
	response := runThroughAuthMiddleware(g, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
}

func TestAuthMiddleware_ProfileEndpointsAllowBearerToken(t *testing.T) {
	t.Parallel()

	g := &gateway{
		config:         &configs.Config{},
		securityConfig: &configs.SecurityConfig{Password: "set", Token: "token123"},
	}

	request := httptest.NewRequest(http.MethodPut, "/api/v1/profile", nil)
	request.Header.Set("Authorization", "Bearer token123")
	response := runThroughAuthMiddleware(g, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("profile put status = %d, want %d", response.Code, http.StatusNoContent)
	}

	request = httptest.NewRequest(http.MethodPost, "/api/v1/profile/avatar", nil)
	request.Header.Set("Authorization", "Bearer token123")
	response = runThroughAuthMiddleware(g, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("profile avatar post status = %d, want %d", response.Code, http.StatusNoContent)
	}
}

func TestAuthMiddleware_ProfileEndpointsRejectQueryToken(t *testing.T) {
	t.Parallel()

	g := &gateway{
		config:         &configs.Config{},
		securityConfig: &configs.SecurityConfig{Password: "set", Token: "token123"},
	}

	request := httptest.NewRequest(http.MethodPut, "/api/v1/profile?token=token123", nil)
	response := runThroughAuthMiddleware(g, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("profile put status = %d, want %d", response.Code, http.StatusUnauthorized)
	}

	request = httptest.NewRequest(http.MethodDelete, "/api/v1/profile/avatar?token=token123", nil)
	response = runThroughAuthMiddleware(g, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("profile avatar delete status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestAuthMiddleware_ProfileAvatarRequiresAuth(t *testing.T) {
	t.Parallel()

	g := &gateway{
		config:         &configs.Config{},
		securityConfig: &configs.SecurityConfig{Password: "set"},
	}
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/profile/avatar", nil)
	response := runThroughAuthMiddleware(g, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestAuthMiddleware_WebSocketAllowsBearerToken(t *testing.T) {
	t.Parallel()

	g := &gateway{
		config:         &configs.Config{},
		securityConfig: &configs.SecurityConfig{Password: "set", Token: "token123"},
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
		securityConfig: &configs.SecurityConfig{Password: "set", Token: "token123"},
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/websocket", nil)
	response := runThroughAuthMiddleware(g, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}
