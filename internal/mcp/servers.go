package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/teanode/teanode/internal/mcp/oauth"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/tools"
	"github.com/teanode/teanode/internal/util/ptrto"
)

// tokenRefreshLeeway is how far ahead of an OAuth access token's expiry the
// token is proactively refreshed, so a token that is about to expire mid-run is
// renewed before it is used.
const tokenRefreshLeeway = 60 * time.Second

// RegisterConfiguredTools resolves the MCP servers available to the current
// request — node-level shared servers plus any per-user (user/oauth) servers the
// authenticated user has connected — and registers their tools into the registry
// using the shared default manager. It is a no-op when no servers are available.
func RegisterConfiguredTools(ctx context.Context, registry *tools.ToolRegistry) {
	if registry == nil {
		return
	}
	servers := resolveServers(ctx)
	if len(servers) == 0 {
		return
	}
	defaultManager.RegisterTools(ctx, registry, servers)
}

// resolveServers loads the configuration and the current user's MCP connections
// from the store and returns the client-ready server configurations for the
// request: shared servers for everyone plus per-user servers for the
// authenticated user. OAuth tokens are refreshed on expiry (best-effort) before
// they are used.
func resolveServers(ctx context.Context) []ServerConfiguration {
	dataStore := store.StoreFromContextSafe(ctx)
	if dataStore == nil {
		return nil
	}
	userId := ""
	if user := models.UserFromContext(ctx); user != nil {
		userId = user.ID
	}

	var configuration *models.Configuration
	connectionsByServer := map[string]*models.MCPConnection{}
	transactionError := dataStore.Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		loaded, err := transaction.GetConfiguration(ctx, nil)
		if err != nil {
			return err
		}
		configuration = loaded
		// Only pay for the connection lookup when the node actually has a
		// user-scoped server and there is a user to scope it to.
		if userId != "" && configurationHasUserScopedServer(loaded) {
			connections, listError := transaction.ListMCPConnections(ctx, userId, nil)
			if listError != nil {
				return listError
			}
			for _, connection := range connections {
				connectionsByServer[connection.GetServerName()] = connection
			}
		}
		return nil
	})
	if transactionError != nil {
		log.Warningf("mcp: loading configuration: %v", transactionError)
		return nil
	}

	servers := ServersFromConfiguration(configuration)
	servers = append(servers, resolveUserServers(ctx, dataStore, configuration, userId, connectionsByServer)...)
	return servers
}

// ServersFromConfiguration resolves the shared (node-level) MCP servers from a
// configuration into client-ready ServerConfiguration values. Only servers with
// "none" or "static" auth are returned: "user" and "oauth" servers require a
// per-user connection and are resolved by resolveUserServers. Disabled,
// duplicate, and incomplete entries are dropped (and logged) so callers receive
// only usable servers.
func ServersFromConfiguration(configuration *models.Configuration) []ServerConfiguration {
	var servers []ServerConfiguration
	forEachConfiguredServer(configuration, func(base ServerConfiguration, mode models.MCPServerAuthMode, server *models.MCPServerConfiguration) {
		switch mode {
		case models.MCPServerAuthStatic:
			base.Authorization = strings.TrimSpace(server.GetAuthorization())
			servers = append(servers, base)
		case models.MCPServerAuthNone:
			servers = append(servers, base)
		}
		// "user" and "oauth" servers require a per-user credential and are
		// resolved separately.
	})
	return servers
}

// resolveUserServers returns the client-ready configurations for the per-user
// (user/oauth) MCP servers the authenticated user has a usable connection to.
// Servers without a connection, or whose credential is missing or unusable, are
// skipped so a user only ever sees servers they have actually authenticated to.
func resolveUserServers(ctx context.Context, dataStore store.Store, configuration *models.Configuration, userId string, connectionsByServer map[string]*models.MCPConnection) []ServerConfiguration {
	if userId == "" {
		return nil
	}
	var servers []ServerConfiguration
	forEachConfiguredServer(configuration, func(base ServerConfiguration, mode models.MCPServerAuthMode, server *models.MCPServerConfiguration) {
		if mode != models.MCPServerAuthUser && mode != models.MCPServerAuthOAuth {
			return
		}
		connection := connectionsByServer[base.Name]
		if connection == nil {
			log.Infof("mcp: skipping server %q for user %q: no connection", base.Name, userId)
			return
		}
		if connection.GetStatus() == models.MCPConnectionStatusDisconnected {
			log.Infof("mcp: skipping server %q for user %q: connection is disconnected", base.Name, userId)
			return
		}
		authorization, ok := authorizationForConnection(ctx, dataStore, mode, server, connection)
		if !ok {
			return
		}
		base.Authorization = authorization
		servers = append(servers, base)
	})
	return servers
}

// authorizationForConnection resolves the HTTP Authorization header value to use
// for a per-user connection, returning false when no usable credential is
// available (in which case the server is skipped for this user).
func authorizationForConnection(ctx context.Context, dataStore store.Store, mode models.MCPServerAuthMode, server *models.MCPServerConfiguration, connection *models.MCPConnection) (string, bool) {
	switch mode {
	case models.MCPServerAuthUser:
		authorization := strings.TrimSpace(connection.GetAuthorization())
		if authorization == "" {
			log.Infof("mcp: skipping server %q: connection has no credential", connection.GetServerName())
			return "", false
		}
		return authorization, true
	case models.MCPServerAuthOAuth:
		return oauthAuthorization(ctx, dataStore, server, connection)
	default:
		return "", false
	}
}

// oauthAuthorization returns the Authorization header value for an oauth-mode
// connection, refreshing the access token first when it has expired (or is about
// to) and a refresh token is available. A token that is already expired and
// cannot be refreshed yields false so the server is skipped rather than called
// with an unusable token.
func oauthAuthorization(ctx context.Context, dataStore store.Store, server *models.MCPServerConfiguration, connection *models.MCPConnection) (string, bool) {
	accessToken := strings.TrimSpace(connection.GetAccessToken())
	if accessToken == "" {
		log.Infof("mcp: skipping oauth server %q: no access token", connection.GetServerName())
		return "", false
	}
	tokenType := tokenTypeOrBearer(connection.GetTokenType())

	expiresAt := connection.TokenExpiresAt
	now := time.Now()
	if expiresAt != nil && !expiresAt.IsZero() && !now.Add(tokenRefreshLeeway).Before(*expiresAt) {
		// The token is expired or within the refresh leeway of expiring.
		if strings.TrimSpace(connection.GetRefreshToken()) != "" {
			token, refreshError := refreshOAuthToken(ctx, dataStore, server, connection)
			if refreshError == nil {
				return tokenTypeOrBearer(token.TokenType) + " " + token.AccessToken, true
			}
			// The refresh failed. If the existing token has not actually expired
			// yet, keep using it for this run rather than disrupting the user.
			if now.Before(*expiresAt) {
				log.Warningf("mcp: oauth refresh for %q failed but token is still valid: %v", connection.GetServerName(), refreshError)
				return tokenType + " " + accessToken, true
			}
			markConnectionError(ctx, dataStore, connection.ID, "token refresh: "+refreshError.Error())
			log.Warningf("mcp: skipping oauth server %q: token expired and refresh failed: %v", connection.GetServerName(), refreshError)
			return "", false
		}
		// No refresh token. The token is usable only until it actually expires.
		if !now.Before(*expiresAt) {
			log.Warningf("mcp: skipping oauth server %q: access token expired with no refresh token", connection.GetServerName())
			return "", false
		}
	}
	return tokenType + " " + accessToken, true
}

// refreshOAuthToken exchanges the connection's refresh token for a new access
// token and persists it. A persistence failure is logged but does not fail the
// refresh, since the returned token is still usable for the current run.
func refreshOAuthToken(ctx context.Context, dataStore store.Store, server *models.MCPServerConfiguration, connection *models.MCPConnection) (*oauth.Token, error) {
	client := oauth.NewClient(ServerOAuthConfig(server))
	_, tokenEndpoint, endpointsError := client.Endpoints(ctx)
	if endpointsError != nil {
		return nil, fmt.Errorf("mcp: resolving token endpoint: %w", endpointsError)
	}
	token, refreshError := client.Refresh(ctx, tokenEndpoint, strings.TrimSpace(connection.GetRefreshToken()))
	if refreshError != nil {
		return nil, refreshError
	}
	if err := dataStore.Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		_, modifyError := transaction.ModifyMCPConnection(ctx, connection.ID, func(stored *models.MCPConnection) error {
			ApplyOAuthToken(stored, token)
			return nil
		}, nil)
		return modifyError
	}); err != nil {
		log.Warningf("mcp: persisting refreshed token for %q: %v", connection.GetServerName(), err)
	}
	return token, nil
}

// markConnectionError records a failure reason on a connection so the user can
// see why it stopped working. It is best-effort and ignores write errors.
func markConnectionError(ctx context.Context, dataStore store.Store, connectionId, message string) {
	_ = dataStore.Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		_, modifyError := transaction.ModifyMCPConnection(ctx, connectionId, func(connection *models.MCPConnection) error {
			connection.Status = ptrto.Value(models.MCPConnectionStatusError)
			connection.LastError = ptrto.Value(message)
			return nil
		}, nil)
		return modifyError
	})
}

// ApplyOAuthToken stores token material on a connection and marks it connected.
// Fields absent from a refresh response (refresh token, scope, expiry) are left
// unchanged so a refresh that omits them does not erase prior values.
func ApplyOAuthToken(connection *models.MCPConnection, token *oauth.Token) {
	connection.Status = ptrto.Value(models.MCPConnectionStatusConnected)
	connection.AccessToken = ptrto.Value(token.AccessToken)
	connection.TokenType = ptrto.Value(tokenTypeOrBearer(token.TokenType))
	connection.LastError = ptrto.Value("")
	connection.LastConnectedAt = ptrto.TimeNow()
	if token.RefreshToken != "" {
		connection.RefreshToken = ptrto.Value(token.RefreshToken)
	}
	if token.Scope != "" {
		connection.Scope = ptrto.Value(token.Scope)
	}
	if !token.ExpiresAt.IsZero() {
		connection.TokenExpiresAt = ptrto.Value(token.ExpiresAt)
	}
}

// ServerOAuthConfig builds the OAuth client configuration for a server from its
// stored OAuth settings.
func ServerOAuthConfig(server *models.MCPServerConfiguration) oauth.ServerConfig {
	var scopes []string
	if server.OAuthScopes != nil {
		scopes = *server.OAuthScopes
	}
	return oauth.ServerConfig{
		ClientID:         server.GetOAuthClientID(),
		ClientSecret:     server.GetOAuthClientSecret(),
		Scopes:           scopes,
		AuthorizationURL: server.GetOAuthAuthorizationURL(),
		TokenURL:         server.GetOAuthTokenURL(),
		ResourceURL:      server.GetURL(),
	}
}

// tokenTypeOrBearer returns the token type, defaulting to "Bearer" when empty.
func tokenTypeOrBearer(tokenType string) string {
	if trimmed := strings.TrimSpace(tokenType); trimmed != "" {
		return trimmed
	}
	return "Bearer"
}

// configurationHasUserScopedServer reports whether any configured server uses an
// auth mode that requires a per-user connection.
func configurationHasUserScopedServer(configuration *models.Configuration) bool {
	if configuration == nil || configuration.Tools == nil || configuration.Tools.MCP == nil {
		return false
	}
	for _, server := range configuration.Tools.MCP.GetServers() {
		if server == nil {
			continue
		}
		if server.Enabled != nil && !*server.Enabled {
			continue
		}
		mode := server.ResolvedAuthMode()
		if mode == models.MCPServerAuthUser || mode == models.MCPServerAuthOAuth {
			return true
		}
	}
	return false
}

// forEachConfiguredServer invokes register for each enabled, uniquely-named, URL-bearing
// MCP server in the configuration, passing a base ServerConfiguration (with the
// Authorization left unset) and the server's resolved auth mode. Disabled,
// duplicate, and incomplete entries are dropped (and logged).
func forEachConfiguredServer(configuration *models.Configuration, register func(base ServerConfiguration, mode models.MCPServerAuthMode, server *models.MCPServerConfiguration)) {
	if configuration == nil || configuration.Tools == nil || configuration.Tools.MCP == nil {
		return
	}
	seen := make(map[string]bool)
	for _, server := range configuration.Tools.MCP.GetServers() {
		if server == nil {
			continue
		}
		// Enabled defaults to true: a configured server is active unless the
		// operator explicitly disables it.
		if server.Enabled != nil && !*server.Enabled {
			continue
		}
		name := strings.TrimSpace(server.GetName())
		serverUrl := strings.TrimSpace(server.GetURL())
		if name == "" || serverUrl == "" {
			log.Warningf("mcp: skipping server with empty name or url")
			continue
		}
		if seen[name] {
			log.Warningf("mcp: skipping duplicate server name %q", name)
			continue
		}
		seen[name] = true
		timeout := defaultTimeout
		if seconds := server.GetTimeoutSeconds(); seconds > 0 {
			timeout = time.Duration(seconds) * time.Second
		}
		register(ServerConfiguration{Name: name, URL: serverUrl, Timeout: timeout}, server.ResolvedAuthMode(), server)
	}
}
