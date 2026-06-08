package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/store/fsstore"
	"github.com/teanode/teanode/internal/tools"
	"github.com/teanode/teanode/internal/util/ptrto"
)

// TestServersFromConfigurationAuthModes verifies that ServersFromConfiguration
// returns only the shared (none/static) servers, copies the static credential,
// and leaves the per-user (user/oauth) servers for resolveUserServers.
func TestServersFromConfigurationAuthModes(t *testing.T) {
	staticAuth := models.MCPServerAuthStatic
	userAuth := models.MCPServerAuthUser
	oauthAuth := models.MCPServerAuthOAuth
	configuration := &models.Configuration{
		Tools: &models.ToolsConfiguration{
			MCP: &models.MCPConfiguration{
				Servers: &[]*models.MCPServerConfiguration{
					{Name: ptrto.Value("open"), URL: ptrto.Value("https://open.example/mcp")},
					{Name: ptrto.Value("shared"), URL: ptrto.Value("https://shared.example/mcp"), Auth: &staticAuth, Authorization: ptrto.Value("Bearer node-secret")},
					{Name: ptrto.Value("private"), URL: ptrto.Value("https://private.example/mcp"), Auth: &userAuth},
					{Name: ptrto.Value("protected"), URL: ptrto.Value("https://protected.example/mcp"), Auth: &oauthAuth},
				},
			},
		},
	}

	servers := ServersFromConfiguration(configuration)
	if len(servers) != 2 {
		t.Fatalf("len(servers) = %d, want 2 (open, shared); got %+v", len(servers), servers)
	}
	open := findServer(servers, "open")
	if open == nil || open.Authorization != "" {
		t.Errorf("open server = %+v, want present with no authorization", open)
	}
	shared := findServer(servers, "shared")
	if shared == nil || shared.Authorization != "Bearer node-secret" {
		t.Errorf("shared server = %+v, want static authorization", shared)
	}
	if findServer(servers, "private") != nil || findServer(servers, "protected") != nil {
		t.Errorf("per-user servers must not be returned by ServersFromConfiguration: %+v", servers)
	}
}

// TestResolveServersUserAuth verifies that a "user" auth server is resolved with
// the connecting user's own credential, skipped for users without a connection,
// and skipped entirely when there is no authenticated user.
func TestResolveServersUserAuth(t *testing.T) {
	openedStore := newMCPTestStore(t)
	userAuth := models.MCPServerAuthUser
	seedMCPConfiguration(t, openedStore, []*models.MCPServerConfiguration{
		{Name: ptrto.Value("open"), URL: ptrto.Value("https://open.example/mcp")},
		{Name: ptrto.Value("private"), URL: ptrto.Value("https://private.example/mcp"), Auth: &userAuth},
	})
	createConnection(t, openedStore, &models.MCPConnection{
		UserID:        ptrto.Value("user-1"),
		ServerName:    ptrto.Value("private"),
		Status:        ptrto.Value(models.MCPConnectionStatusConnected),
		Authorization: ptrto.Value("Bearer user-1-secret"),
	})

	// user-1 has a connection: the private server resolves with their credential.
	servers := resolveServers(contextWithUser(openedStore, "user-1"))
	if open := findServer(servers, "open"); open == nil {
		t.Errorf("shared server should be available to user-1; got %+v", servers)
	}
	private := findServer(servers, "private")
	if private == nil {
		t.Fatalf("private server should resolve for user-1; got %+v", servers)
	}
	if private.Authorization != "Bearer user-1-secret" {
		t.Errorf("private.Authorization = %q, want the user's credential", private.Authorization)
	}

	// user-2 has no connection: the private server is skipped, the shared one is not.
	servers = resolveServers(contextWithUser(openedStore, "user-2"))
	if findServer(servers, "private") != nil {
		t.Errorf("private server must be skipped for a user without a connection")
	}
	if findServer(servers, "open") == nil {
		t.Errorf("shared server should remain available to user-2")
	}

	// No authenticated user: per-user servers are skipped, shared ones remain.
	servers = resolveServers(store.ContextWithStore(context.Background(), openedStore))
	if findServer(servers, "private") != nil {
		t.Errorf("private server must be skipped without an authenticated user")
	}
	if findServer(servers, "open") == nil {
		t.Errorf("shared server should be available without an authenticated user")
	}
}

// TestResolveServersUserAuthDisconnected verifies that a deliberately
// disconnected connection does not provide tools even though it holds a
// credential.
func TestResolveServersUserAuthDisconnected(t *testing.T) {
	openedStore := newMCPTestStore(t)
	userAuth := models.MCPServerAuthUser
	seedMCPConfiguration(t, openedStore, []*models.MCPServerConfiguration{
		{Name: ptrto.Value("private"), URL: ptrto.Value("https://private.example/mcp"), Auth: &userAuth},
	})
	createConnection(t, openedStore, &models.MCPConnection{
		UserID:        ptrto.Value("user-1"),
		ServerName:    ptrto.Value("private"),
		Status:        ptrto.Value(models.MCPConnectionStatusDisconnected),
		Authorization: ptrto.Value("Bearer user-1-secret"),
	})

	servers := resolveServers(contextWithUser(openedStore, "user-1"))
	if findServer(servers, "private") != nil {
		t.Errorf("disconnected connection must not provide tools; got %+v", servers)
	}
}

// TestResolveServersOAuthValidToken verifies that an oauth server with a valid,
// unexpired access token resolves with a bearer header and no network refresh.
func TestResolveServersOAuthValidToken(t *testing.T) {
	openedStore := newMCPTestStore(t)
	seedMCPConfiguration(t, openedStore, []*models.MCPServerConfiguration{oauthServer("protected", "https://protected.example/mcp", "https://unused.example/token")})
	future := time.Now().Add(time.Hour)
	createConnection(t, openedStore, &models.MCPConnection{
		UserID:         ptrto.Value("user-1"),
		ServerName:     ptrto.Value("protected"),
		Status:         ptrto.Value(models.MCPConnectionStatusConnected),
		AccessToken:    ptrto.Value("valid-access"),
		TokenType:      ptrto.Value("Bearer"),
		TokenExpiresAt: ptrto.Value(future),
	})

	servers := resolveServers(contextWithUser(openedStore, "user-1"))
	protected := findServer(servers, "protected")
	if protected == nil {
		t.Fatalf("oauth server should resolve with a valid token; got %+v", servers)
	}
	if protected.Authorization != "Bearer valid-access" {
		t.Errorf("protected.Authorization = %q, want %q", protected.Authorization, "Bearer valid-access")
	}
}

// TestResolveServersOAuthRefresh verifies that an expired access token with a
// refresh token is refreshed against the token endpoint, the new token is used,
// and the refreshed token is persisted back to the connection.
func TestResolveServersOAuthRefresh(t *testing.T) {
	var refreshCalls int
	tokenServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		refreshCalls++
		_ = request.ParseForm()
		if request.FormValue("grant_type") != "refresh_token" || request.FormValue("refresh_token") != "refresh-1" {
			http.Error(writer, "bad refresh", http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]interface{}{
			"access_token":  "refreshed-access",
			"refresh_token": "refresh-2",
			"token_type":    "Bearer",
			"expires_in":    3600,
		})
	}))
	t.Cleanup(tokenServer.Close)

	openedStore := newMCPTestStore(t)
	seedMCPConfiguration(t, openedStore, []*models.MCPServerConfiguration{oauthServer("protected", "https://protected.example/mcp", tokenServer.URL)})
	past := time.Now().Add(-time.Hour)
	connectionId := createConnection(t, openedStore, &models.MCPConnection{
		UserID:         ptrto.Value("user-1"),
		ServerName:     ptrto.Value("protected"),
		Status:         ptrto.Value(models.MCPConnectionStatusConnected),
		AccessToken:    ptrto.Value("stale-access"),
		RefreshToken:   ptrto.Value("refresh-1"),
		TokenType:      ptrto.Value("Bearer"),
		TokenExpiresAt: ptrto.Value(past),
	})

	servers := resolveServers(contextWithUser(openedStore, "user-1"))
	protected := findServer(servers, "protected")
	if protected == nil {
		t.Fatalf("oauth server should resolve after refresh; got %+v", servers)
	}
	if protected.Authorization != "Bearer refreshed-access" {
		t.Errorf("protected.Authorization = %q, want refreshed token", protected.Authorization)
	}
	if refreshCalls != 1 {
		t.Errorf("token endpoint called %d times, want 1", refreshCalls)
	}

	// The refreshed token is persisted: a later run would not need to refresh.
	reloaded := getConnection(t, openedStore, connectionId)
	if reloaded.GetAccessToken() != "refreshed-access" {
		t.Errorf("persisted access token = %q, want refreshed-access", reloaded.GetAccessToken())
	}
	if reloaded.GetRefreshToken() != "refresh-2" {
		t.Errorf("persisted refresh token = %q, want refresh-2", reloaded.GetRefreshToken())
	}
	if reloaded.GetTokenExpiresAt().Before(time.Now()) {
		t.Errorf("persisted expiry %v should be in the future", reloaded.GetTokenExpiresAt())
	}
}

// TestResolveServersOAuthExpiredNoRefresh verifies that an expired token without
// a refresh token causes the server to be skipped rather than called with an
// unusable credential.
func TestResolveServersOAuthExpiredNoRefresh(t *testing.T) {
	openedStore := newMCPTestStore(t)
	seedMCPConfiguration(t, openedStore, []*models.MCPServerConfiguration{oauthServer("protected", "https://protected.example/mcp", "https://unused.example/token")})
	past := time.Now().Add(-time.Hour)
	createConnection(t, openedStore, &models.MCPConnection{
		UserID:         ptrto.Value("user-1"),
		ServerName:     ptrto.Value("protected"),
		Status:         ptrto.Value(models.MCPConnectionStatusConnected),
		AccessToken:    ptrto.Value("stale-access"),
		TokenType:      ptrto.Value("Bearer"),
		TokenExpiresAt: ptrto.Value(past),
	})

	servers := resolveServers(contextWithUser(openedStore, "user-1"))
	if findServer(servers, "protected") != nil {
		t.Errorf("expired oauth server without a refresh token must be skipped; got %+v", servers)
	}
}

// TestResolveServersOAuthNoToken verifies that an oauth connection that never
// completed authorization (no access token) is skipped.
func TestResolveServersOAuthNoToken(t *testing.T) {
	openedStore := newMCPTestStore(t)
	seedMCPConfiguration(t, openedStore, []*models.MCPServerConfiguration{oauthServer("protected", "https://protected.example/mcp", "https://unused.example/token")})
	createConnection(t, openedStore, &models.MCPConnection{
		UserID:     ptrto.Value("user-1"),
		ServerName: ptrto.Value("protected"),
		Status:     ptrto.Value(models.MCPConnectionStatusPending),
	})

	servers := resolveServers(contextWithUser(openedStore, "user-1"))
	if findServer(servers, "protected") != nil {
		t.Errorf("oauth server without an access token must be skipped; got %+v", servers)
	}
}

// TestRegisterConfiguredToolsUserAuthEndToEnd ties the per-user resolution to
// discovery: the user's stored credential must reach the remote server and its
// tools must be registered namespaced.
func TestRegisterConfiguredToolsUserAuthEndToEnd(t *testing.T) {
	remote := newTestMCPServer(t)
	remote.tools = sampleTools()
	remote.requireAuth = "Bearer user-1-secret"

	openedStore := newMCPTestStore(t)
	userAuth := models.MCPServerAuthUser
	seedMCPConfiguration(t, openedStore, []*models.MCPServerConfiguration{
		{Name: ptrto.Value("broker"), URL: ptrto.Value(remote.url()), Auth: &userAuth},
	})
	createConnection(t, openedStore, &models.MCPConnection{
		UserID:        ptrto.Value("user-1"),
		ServerName:    ptrto.Value("broker"),
		Status:        ptrto.Value(models.MCPConnectionStatusConnected),
		Authorization: ptrto.Value("Bearer user-1-secret"),
	})

	// A connected user discovers the broker's tools.
	registry := tools.NewEmptyToolRegistry()
	RegisterConfiguredTools(contextWithUser(openedStore, "user-1"), registry)
	if registry.Get("mcp__broker__get_quote") == nil {
		t.Errorf("expected mcp__broker__get_quote for the connected user; names = %v", registry.Names())
	}

	// A user without a connection never reaches the server, so no tools register.
	otherRegistry := tools.NewEmptyToolRegistry()
	RegisterConfiguredTools(contextWithUser(openedStore, "user-2"), otherRegistry)
	if names := otherRegistry.Names(); len(names) != 0 {
		t.Errorf("expected no tools for a user without a connection, got %v", names)
	}
}

// TestRegisterConfiguredToolsMarksConnectionConnected verifies that a successful
// discovery against a user-scoped server flips the backing connection to
// connected, stamps LastConnectedAt, and clears any prior error.
func TestRegisterConfiguredToolsMarksConnectionConnected(t *testing.T) {
	remote := newTestMCPServer(t)
	remote.tools = sampleTools()
	remote.requireAuth = "Bearer user-1-secret"

	openedStore := newMCPTestStore(t)
	userAuth := models.MCPServerAuthUser
	seedMCPConfiguration(t, openedStore, []*models.MCPServerConfiguration{
		{Name: ptrto.Value("broker"), URL: ptrto.Value(remote.url()), Auth: &userAuth},
	})
	connectionId := createConnection(t, openedStore, &models.MCPConnection{
		UserID:        ptrto.Value("user-1"),
		ServerName:    ptrto.Value("broker"),
		Status:        ptrto.Value(models.MCPConnectionStatusPending),
		LastError:     ptrto.Value("an earlier failure"),
		Authorization: ptrto.Value("Bearer user-1-secret"),
	})

	RegisterConfiguredTools(contextWithUser(openedStore, "user-1"), tools.NewEmptyToolRegistry())

	reloaded := getConnection(t, openedStore, connectionId)
	if reloaded.GetStatus() != models.MCPConnectionStatusConnected {
		t.Errorf("status = %q, want connected", reloaded.GetStatus())
	}
	if reloaded.GetLastError() != "" {
		t.Errorf("lastError = %q, want cleared", reloaded.GetLastError())
	}
	if reloaded.LastConnectedAt == nil {
		t.Errorf("LastConnectedAt should be stamped on a successful discovery")
	}
}

// TestRegisterConfiguredToolsMarksConnectionError verifies that a discovery that
// fails because the server rejects the credential flips the connection to error
// with a recorded reason, so the user can see why their tools are missing.
func TestRegisterConfiguredToolsMarksConnectionError(t *testing.T) {
	remote := newTestMCPServer(t)
	remote.tools = sampleTools()
	remote.requireAuth = "Bearer correct-secret"

	openedStore := newMCPTestStore(t)
	userAuth := models.MCPServerAuthUser
	seedMCPConfiguration(t, openedStore, []*models.MCPServerConfiguration{
		{Name: ptrto.Value("broker"), URL: ptrto.Value(remote.url()), Auth: &userAuth},
	})
	connectionId := createConnection(t, openedStore, &models.MCPConnection{
		UserID:        ptrto.Value("user-1"),
		ServerName:    ptrto.Value("broker"),
		Status:        ptrto.Value(models.MCPConnectionStatusConnected),
		Authorization: ptrto.Value("Bearer wrong-secret"),
	})

	registry := tools.NewEmptyToolRegistry()
	RegisterConfiguredTools(contextWithUser(openedStore, "user-1"), registry)
	if len(registry.Names()) != 0 {
		t.Errorf("expected no tools registered on auth failure, got %v", registry.Names())
	}

	reloaded := getConnection(t, openedStore, connectionId)
	if reloaded.GetStatus() != models.MCPConnectionStatusError {
		t.Errorf("status = %q, want error", reloaded.GetStatus())
	}
	if reloaded.GetLastError() == "" {
		t.Errorf("lastError should record the discovery failure")
	}
}

// TestServersFromConfigurationSkipsInvalidURL verifies that servers with a
// missing or non-http(s) URL are dropped rather than handed to the client.
func TestServersFromConfigurationSkipsInvalidURL(t *testing.T) {
	configuration := &models.Configuration{
		Tools: &models.ToolsConfiguration{
			MCP: &models.MCPConfiguration{
				Servers: &[]*models.MCPServerConfiguration{
					{Name: ptrto.Value("good"), URL: ptrto.Value("https://good.example/mcp")},
					{Name: ptrto.Value("ftp"), URL: ptrto.Value("ftp://bad.example/mcp")},
					{Name: ptrto.Value("scheme-less"), URL: ptrto.Value("bad.example/mcp")},
					{Name: ptrto.Value("hostless"), URL: ptrto.Value("https://")},
				},
			},
		},
	}

	servers := ServersFromConfiguration(configuration)
	if len(servers) != 1 || servers[0].Name != "good" {
		t.Errorf("servers = %+v, want only the https server", servers)
	}
}

// --- helpers ---

func newMCPTestStore(t *testing.T) store.Store {
	t.Helper()
	openedStore, openError := fsstore.Open(fsstore.Options{DataDirectory: t.TempDir()})
	if openError != nil {
		t.Fatalf("opening store: %v", openError)
	}
	t.Cleanup(func() { _ = openedStore.Close() })
	if migrateError := openedStore.Migrate(context.Background()); migrateError != nil {
		t.Fatalf("migrating store: %v", migrateError)
	}
	return openedStore
}

func seedMCPConfiguration(t *testing.T, openedStore store.Store, servers []*models.MCPServerConfiguration) {
	t.Helper()
	if err := openedStore.Transaction(context.Background(), func(ctx context.Context, transaction store.Transaction) error {
		_, modifyError := transaction.ModifyConfiguration(ctx, func(configuration *models.Configuration) error {
			configuration.Tools = &models.ToolsConfiguration{MCP: &models.MCPConfiguration{Servers: &servers}}
			return nil
		}, nil)
		return modifyError
	}); err != nil {
		t.Fatalf("seeding MCP configuration: %v", err)
	}
}

func createConnection(t *testing.T, openedStore store.Store, connection *models.MCPConnection) string {
	t.Helper()
	var id string
	if err := openedStore.Transaction(context.Background(), func(ctx context.Context, transaction store.Transaction) error {
		created, createError := transaction.CreateMCPConnection(ctx, connection, nil)
		if createError != nil {
			return createError
		}
		id = created.ID
		return nil
	}); err != nil {
		t.Fatalf("creating MCP connection: %v", err)
	}
	return id
}

func getConnection(t *testing.T, openedStore store.Store, connectionId string) *models.MCPConnection {
	t.Helper()
	var connection *models.MCPConnection
	if err := openedStore.Transaction(context.Background(), func(ctx context.Context, transaction store.Transaction) error {
		loaded, getError := transaction.GetMCPConnection(ctx, connectionId, nil)
		if getError != nil {
			return getError
		}
		connection = loaded
		return nil
	}); err != nil {
		t.Fatalf("reloading MCP connection: %v", err)
	}
	return connection
}

// contextWithUser returns a context carrying the store and an authenticated user.
func contextWithUser(openedStore store.Store, userId string) context.Context {
	ctx := store.ContextWithStore(context.Background(), openedStore)
	return models.ContextWithUserSessionToken(ctx, &models.User{ID: userId}, nil, nil)
}

// oauthServer builds an oauth-mode server with explicit endpoints so endpoint
// resolution does not require discovery. The authorization URL is unused by the
// refresh path but must be non-empty to bypass discovery.
func oauthServer(name, url, tokenUrl string) *models.MCPServerConfiguration {
	oauthAuth := models.MCPServerAuthOAuth
	return &models.MCPServerConfiguration{
		Name:                  ptrto.Value(name),
		URL:                   ptrto.Value(url),
		Auth:                  &oauthAuth,
		OAuthClientID:         ptrto.Value("client-1"),
		OAuthAuthorizationURL: ptrto.Value("https://auth.example/authorize"),
		OAuthTokenURL:         ptrto.Value(tokenUrl),
	}
}

func findServer(servers []ServerConfiguration, name string) *ServerConfiguration {
	for index := range servers {
		if servers[index].Name == name {
			return &servers[index]
		}
	}
	return nil
}
