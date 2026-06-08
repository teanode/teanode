package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/store/fsstore"
	"github.com/teanode/teanode/internal/util/ptrto"
)

// newMCPTestConnection opens an fsstore, creates a user, seeds an MCP server
// configuration, and returns a webSocketConnection whose context carries the
// store and authenticated user.
func newMCPTestConnection(t *testing.T, authMode models.MCPServerAuthMode) (*webSocketConnection, string) {
	t.Helper()
	openedStore, openError := fsstore.Open(fsstore.Options{DataDirectory: t.TempDir()})
	if openError != nil {
		t.Fatalf("opening store: %v", openError)
	}
	t.Cleanup(func() { _ = openedStore.Close() })
	if migrateError := openedStore.Migrate(context.Background()); migrateError != nil {
		t.Fatalf("migrating store: %v", migrateError)
	}

	var userId string
	if err := openedStore.Transaction(context.Background(), func(ctx context.Context, transaction store.Transaction) error {
		user, createError := transaction.CreateUser(ctx, &models.User{
			Username: ptrto.Value("alice"),
			Admin:    ptrto.Value(false),
		}, nil, nil)
		if createError != nil {
			return createError
		}
		userId = user.ID
		mode := authMode
		_, modifyError := transaction.ModifyConfiguration(ctx, func(configuration *models.Configuration) error {
			configuration.Tools = &models.ToolsConfiguration{
				MCP: &models.MCPConfiguration{
					Servers: &[]*models.MCPServerConfiguration{
						{
							Name: ptrto.Value("robinhood"),
							URL:  ptrto.Value("https://example.com/mcp"),
							Auth: &mode,
						},
					},
				},
			}
			return nil
		}, nil)
		return modifyError
	}); err != nil {
		t.Fatalf("seeding store: %v", err)
	}

	ctx := store.ContextWithStore(context.Background(), openedStore)
	ctx = models.ContextWithUserSessionToken(ctx, &models.User{ID: userId, Username: ptrto.Value("alice")}, nil, nil)
	return &webSocketConnection{ctx: ctx}, userId
}

func frameWith(t *testing.T, parameters interface{}) requestFrame {
	t.Helper()
	raw, err := json.Marshal(parameters)
	if err != nil {
		t.Fatalf("marshaling parameters: %v", err)
	}
	return requestFrame{Parameters: raw}
}

// newOAuthMCPTestConnection seeds a store with a user and a single oauth-mode
// MCP server using explicit authorization/token endpoints (so endpoint
// discovery makes no network call) and a configured node public URL. It returns
// a webSocketConnection whose context carries the store and authenticated user.
func newOAuthMCPTestConnection(t *testing.T, authorizationURL, tokenURL string) (*webSocketConnection, string) {
	t.Helper()
	openedStore, openError := fsstore.Open(fsstore.Options{DataDirectory: t.TempDir()})
	if openError != nil {
		t.Fatalf("opening store: %v", openError)
	}
	t.Cleanup(func() { _ = openedStore.Close() })
	if migrateError := openedStore.Migrate(context.Background()); migrateError != nil {
		t.Fatalf("migrating store: %v", migrateError)
	}

	var userId string
	if err := openedStore.Transaction(context.Background(), func(ctx context.Context, transaction store.Transaction) error {
		user, createError := transaction.CreateUser(ctx, &models.User{
			Username: ptrto.Value("alice"),
			Admin:    ptrto.Value(false),
		}, nil, nil)
		if createError != nil {
			return createError
		}
		userId = user.ID
		_, modifyError := transaction.ModifyConfiguration(ctx, func(configuration *models.Configuration) error {
			configuration.Node = &models.NodeConfiguration{PublicURL: ptrto.Value("https://node.example.com")}
			configuration.Tools = &models.ToolsConfiguration{
				MCP: &models.MCPConfiguration{
					Servers: &[]*models.MCPServerConfiguration{
						{
							Name:                  ptrto.Value("robinhood"),
							URL:                   ptrto.Value("https://example.com/mcp"),
							Auth:                  ptrto.Value(models.MCPServerAuthOAuth),
							OAuthClientID:         ptrto.Value("client-123"),
							OAuthScopes:           ptrto.Value([]string{"read", "trade"}),
							OAuthAuthorizationURL: ptrto.Value(authorizationURL),
							OAuthTokenURL:         ptrto.Value(tokenURL),
						},
					},
				},
			}
			return nil
		}, nil)
		return modifyError
	}); err != nil {
		t.Fatalf("seeding store: %v", err)
	}

	ctx := store.ContextWithStore(context.Background(), openedStore)
	ctx = models.ContextWithUserSessionToken(ctx, &models.User{ID: userId, Username: ptrto.Value("alice")}, nil, nil)
	return &webSocketConnection{ctx: ctx}, userId
}

func TestMCPConnectionsAuthorize(t *testing.T) {
	connection, userId := newOAuthMCPTestConnection(t, "https://auth.example.com/authorize", "https://auth.example.com/token")

	result, err := connection.handleMcpConnectionsAuthorize(frameWith(t, mcpConnectionsAuthorizeParameters{ServerName: "robinhood"}))
	if err != nil {
		t.Fatalf("handleMcpConnectionsAuthorize error: %v", err)
	}
	authorizationURL := result.(map[string]interface{})["authorizationUrl"].(string)
	parsed, parseError := url.Parse(authorizationURL)
	if parseError != nil {
		t.Fatalf("parse authorization url: %v", parseError)
	}
	query := parsed.Query()
	if parsed.Scheme+"://"+parsed.Host+parsed.Path != "https://auth.example.com/authorize" {
		t.Errorf("unexpected authorization endpoint: %s", authorizationURL)
	}
	if got := query.Get("client_id"); got != "client-123" {
		t.Errorf("client_id = %q", got)
	}
	if query.Get("code_challenge") == "" || query.Get("code_challenge_method") != "S256" {
		t.Errorf("missing PKCE challenge: %v", query)
	}
	if got := query.Get("redirect_uri"); got != "https://node.example.com"+mcpOAuthCallbackPath {
		t.Errorf("redirect_uri = %q", got)
	}
	if got := query.Get("scope"); got != "read trade" {
		t.Errorf("scope = %q", got)
	}
	stateValue := query.Get("state")
	if stateValue == "" {
		t.Fatal("missing state")
	}

	// A pending connection holding the transient PKCE/state values is persisted.
	var pending *models.MCPConnection
	if err := store.StoreFromContext(connection.ctx).Transaction(connection.ctx, func(ctx context.Context, transaction store.Transaction) error {
		got, getError := transaction.GetMCPConnectionByServer(ctx, userId, "robinhood", nil)
		pending = got
		return getError
	}); err != nil {
		t.Fatalf("loading pending connection: %v", err)
	}
	if pending.GetStatus() != models.MCPConnectionStatusPending {
		t.Errorf("status = %q, want pending", pending.GetStatus())
	}
	if pending.GetOAuthState() != stateValue {
		t.Errorf("stored state %q does not match authorization url state %q", pending.GetOAuthState(), stateValue)
	}
	if pending.GetCodeVerifier() == "" {
		t.Error("expected stored code verifier")
	}

	// The transient secrets must never appear in any list response.
	for _, handler := range []func(requestFrame) (interface{}, error){
		connection.handleMcpServersList,
		connection.handleMcpConnectionsList,
	} {
		listResult, listError := handler(requestFrame{})
		if listError != nil {
			t.Fatalf("handler error: %v", listError)
		}
		raw, marshalError := json.Marshal(listResult)
		if marshalError != nil {
			t.Fatalf("marshal error: %v", marshalError)
		}
		if strings.Contains(string(raw), pending.GetCodeVerifier()) || strings.Contains(string(raw), pending.GetOAuthState()) {
			t.Errorf("response leaked OAuth secret: %s", raw)
		}
	}
}

func TestMCPConnectionsAuthorizeRejectsNonOAuthServer(t *testing.T) {
	connection, _ := newMCPTestConnection(t, models.MCPServerAuthUser)
	if _, err := connection.handleMcpConnectionsAuthorize(frameWith(t, mcpConnectionsAuthorizeParameters{ServerName: "robinhood"})); err == nil {
		t.Error("expected authorize to be rejected for non-oauth server")
	}
}

func TestCompleteMcpOAuthExchangesCode(t *testing.T) {
	tokenRequests := 0
	tokenServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		tokenRequests++
		if parseError := request.ParseForm(); parseError != nil {
			http.Error(writer, "bad form", http.StatusBadRequest)
			return
		}
		if request.Form.Get("grant_type") != "authorization_code" || request.Form.Get("code") != "auth-code-1" || request.Form.Get("code_verifier") != "verifier-1" {
			http.Error(writer, "unexpected token request", http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"access_token":"access-1","refresh_token":"refresh-1","token_type":"Bearer","expires_in":3600,"scope":"read trade"}`))
	}))
	t.Cleanup(tokenServer.Close)

	connection, userId := newOAuthMCPTestConnection(t, "https://auth.example.com/authorize", tokenServer.URL)

	// Seed a pending connection as the authorize step would have left it.
	if err := store.StoreFromContext(connection.ctx).Transaction(connection.ctx, func(ctx context.Context, transaction store.Transaction) error {
		_, createError := transaction.CreateMCPConnection(ctx, &models.MCPConnection{
			UserID:       ptrto.Value(userId),
			ServerName:   ptrto.Value("robinhood"),
			Status:       ptrto.Value(models.MCPConnectionStatusPending),
			OAuthState:   ptrto.Value("state-1"),
			CodeVerifier: ptrto.Value("verifier-1"),
		}, nil)
		return createError
	}); err != nil {
		t.Fatalf("seeding pending connection: %v", err)
	}

	service := &api{}
	serverName, completeError := service.completeMcpOAuth(connection.ctx, userId, "auth-code-1", "state-1")
	if completeError != nil {
		t.Fatalf("completeMcpOAuth error: %v", completeError)
	}
	if serverName != "robinhood" {
		t.Errorf("serverName = %q", serverName)
	}
	if tokenRequests != 1 {
		t.Errorf("expected 1 token request, got %d", tokenRequests)
	}

	var stored *models.MCPConnection
	if err := store.StoreFromContext(connection.ctx).Transaction(connection.ctx, func(ctx context.Context, transaction store.Transaction) error {
		got, getError := transaction.GetMCPConnectionByServer(ctx, userId, "robinhood", nil)
		stored = got
		return getError
	}); err != nil {
		t.Fatalf("loading connection: %v", err)
	}
	if stored.GetStatus() != models.MCPConnectionStatusConnected {
		t.Errorf("status = %q, want connected", stored.GetStatus())
	}
	if stored.GetAccessToken() != "access-1" || stored.GetRefreshToken() != "refresh-1" {
		t.Errorf("unexpected stored tokens: access=%q refresh=%q", stored.GetAccessToken(), stored.GetRefreshToken())
	}
	if stored.TokenExpiresAt == nil || stored.TokenExpiresAt.IsZero() {
		t.Error("expected stored token expiry")
	}
	// One-time PKCE/state values are cleared after a successful exchange.
	if stored.GetOAuthState() != "" || stored.GetCodeVerifier() != "" {
		t.Errorf("expected cleared transient state, got state=%q verifier=%q", stored.GetOAuthState(), stored.GetCodeVerifier())
	}
}

func TestCompleteMcpOAuthUnknownStateFails(t *testing.T) {
	connection, userId := newOAuthMCPTestConnection(t, "https://auth.example.com/authorize", "https://auth.example.com/token")
	service := &api{}
	if _, err := service.completeMcpOAuth(connection.ctx, userId, "code", "no-such-state"); err == nil {
		t.Error("expected error for unknown state")
	}
}

func TestMCPConnectionsLifecycle(t *testing.T) {
	connection, _ := newMCPTestConnection(t, models.MCPServerAuthUser)

	// Initially the server reports it requires a connection and is not connected.
	serversResult, err := connection.handleMcpServersList(requestFrame{})
	if err != nil {
		t.Fatalf("handleMcpServersList error: %v", err)
	}
	servers := serversResult.(map[string]interface{})["servers"].([]mcpServerListItem)
	if len(servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(servers))
	}
	if !servers[0].RequiresConnection || servers[0].Connected {
		t.Errorf("server should require connection and be disconnected: %+v", servers[0])
	}

	// Create a connection.
	createResult, createError := connection.handleMcpConnectionsCreate(frameWith(t, mcpConnectionsCreateParameters{
		ServerName:    "robinhood",
		Authorization: "Bearer user-secret",
	}))
	if createError != nil {
		t.Fatalf("handleMcpConnectionsCreate error: %v", createError)
	}
	connectionId := createResult.(map[string]interface{})["connection"].(mcpConnectionListItem).ID
	if connectionId == "" {
		t.Fatal("expected created connection id")
	}

	// Now the server reports connected.
	serversResult, _ = connection.handleMcpServersList(requestFrame{})
	servers = serversResult.(map[string]interface{})["servers"].([]mcpServerListItem)
	if !servers[0].Connected {
		t.Errorf("server should be connected after create: %+v", servers[0])
	}

	// Creating a second connection for the same server is rejected.
	if _, duplicateError := connection.handleMcpConnectionsCreate(frameWith(t, mcpConnectionsCreateParameters{
		ServerName:    "robinhood",
		Authorization: "Bearer another",
	})); duplicateError == nil {
		t.Error("expected duplicate connection to be rejected")
	}

	// Delete the connection.
	if _, deleteError := connection.handleMcpConnectionsDelete(frameWith(t, mcpConnectionsDeleteParameters{
		ConnectionID: connectionId,
	})); deleteError != nil {
		t.Fatalf("handleMcpConnectionsDelete error: %v", deleteError)
	}

	listResult, _ := connection.handleMcpConnectionsList(requestFrame{})
	connections := listResult.(map[string]interface{})["connections"].([]mcpConnectionListItem)
	if len(connections) != 0 {
		t.Errorf("expected no connections after delete, got %d", len(connections))
	}
}

func TestMCPConnectionsCreateRejectsNonUserServer(t *testing.T) {
	connection, _ := newMCPTestConnection(t, models.MCPServerAuthNone)
	if _, err := connection.handleMcpConnectionsCreate(frameWith(t, mcpConnectionsCreateParameters{
		ServerName:    "robinhood",
		Authorization: "Bearer x",
	})); err == nil {
		t.Error("expected create to be rejected for non-user server")
	}
}

func TestMCPConnectionsCreateRejectsUnknownServer(t *testing.T) {
	connection, _ := newMCPTestConnection(t, models.MCPServerAuthUser)
	if _, err := connection.handleMcpConnectionsCreate(frameWith(t, mcpConnectionsCreateParameters{
		ServerName:    "nonexistent",
		Authorization: "Bearer x",
	})); err == nil {
		t.Error("expected create to be rejected for unknown server")
	}
}

func TestMCPConnectionsCreateRejectsOversizedAuthorization(t *testing.T) {
	connection, _ := newMCPTestConnection(t, models.MCPServerAuthUser)
	if _, err := connection.handleMcpConnectionsCreate(frameWith(t, mcpConnectionsCreateParameters{
		ServerName:    "robinhood",
		Authorization: strings.Repeat("x", maxAuthorizationLength+1),
	})); err == nil {
		t.Error("expected create to be rejected for an oversized authorization value")
	}
}

// TestMCPResponsesOmitSecrets asserts the stored Authorization credential never
// appears in any list response payload.
func TestMCPResponsesOmitSecrets(t *testing.T) {
	connection, _ := newMCPTestConnection(t, models.MCPServerAuthUser)
	if _, err := connection.handleMcpConnectionsCreate(frameWith(t, mcpConnectionsCreateParameters{
		ServerName:    "robinhood",
		Authorization: "Bearer super-secret-value",
	})); err != nil {
		t.Fatalf("create error: %v", err)
	}

	for _, handler := range []func(requestFrame) (interface{}, error){
		connection.handleMcpServersList,
		connection.handleMcpConnectionsList,
	} {
		result, err := handler(requestFrame{})
		if err != nil {
			t.Fatalf("handler error: %v", err)
		}
		raw, marshalError := json.Marshal(result)
		if marshalError != nil {
			t.Fatalf("marshal error: %v", marshalError)
		}
		if strings.Contains(string(raw), "super-secret-value") {
			t.Errorf("response leaked credential: %s", raw)
		}
	}
}
