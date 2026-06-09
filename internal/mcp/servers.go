package mcp

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"sort"
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
	outcomes := defaultManager.RegisterTools(ctx, registry, servers)
	recordDiscoveryOutcomes(ctx, outcomes)
}

// ToolPolicyEntry describes a discovered remote MCP tool for tool-policy
// management: the namespaced registry name plus the server and bare tool name
// (for hierarchical display), and the policy groups the tool exposes.
type ToolPolicyEntry struct {
	Name       string
	ServerName string
	ToolName   string
	Groups     []tools.PolicyGroup
}

// ConfiguredToolPolicyEntries discovers the tools of every MCP server available
// in ctx (shared servers plus the authenticated user's connected user/oauth
// servers) and returns one entry per tool. Discovery uses the shared manager's
// cache, so repeated calls are cheap; servers that fail discovery are skipped so
// one unreachable server does not hide the rest.
func ConfiguredToolPolicyEntries(ctx context.Context) []ToolPolicyEntry {
	servers := resolveServers(ctx)
	var entries []ToolPolicyEntry
	for _, server := range servers {
		remoteTools, _, err := defaultManager.discover(ctx, server)
		if err != nil {
			log.Warningf("mcp: policy discovery for %q: %v", server.Name, err)
			continue
		}
		for _, remote := range remoteTools {
			if strings.TrimSpace(remote.Name) == "" {
				continue
			}
			adapter := newToolAdapter(server, remote)
			entries = append(entries, ToolPolicyEntry{
				Name:       adapter.displayName,
				ServerName: server.Name,
				ToolName:   remote.Name,
				Groups:     adapter.PolicyGroups(),
			})
		}
	}
	return entries
}

// recordDiscoveryOutcomes reflects fresh discovery results onto the per-user
// connection status: a server reachable with the user's credential is marked
// connected, and one that fails (bad credential, unreachable) is marked error so
// the user can see why their tools are missing. Cached results and shared
// (non-connection) servers are skipped so run startup stays cheap.
func recordDiscoveryOutcomes(ctx context.Context, outcomes []ServerDiscovery) {
	dataStore := store.StoreFromContextSafe(ctx)
	if dataStore == nil {
		return
	}
	for _, outcome := range outcomes {
		connectionId := outcome.Server.ConnectionID
		if connectionId == "" || outcome.Cached {
			continue
		}
		if outcome.Err != nil {
			markConnectionError(ctx, dataStore, connectionId, "discovery: "+outcome.Err.Error())
			continue
		}
		markConnectionConnected(ctx, dataStore, connectionId)
	}
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
		base.ConnectionID = connection.ID
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
	client := oauth.NewClient(ServerOAuthConfigForConnection(server, connection))
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

// markConnectionConnected records a successful discovery against a connection:
// it clears any prior error and stamps the last-connected time. It is
// best-effort and ignores write errors. A deliberately disconnected connection
// is never resurrected here because such connections are not offered to
// discovery in the first place.
func markConnectionConnected(ctx context.Context, dataStore store.Store, connectionId string) {
	_ = dataStore.Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		_, modifyError := transaction.ModifyMCPConnection(ctx, connectionId, func(connection *models.MCPConnection) error {
			connection.Status = ptrto.Value(models.MCPConnectionStatusConnected)
			connection.LastError = ptrto.Value("")
			connection.LastConnectedAt = ptrto.TimeNow()
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

// ServerOAuthConfigForConnection builds the OAuth client configuration for a
// server, overlaying any client credentials a user obtained via dynamic client
// registration (RFC 7591) and stored on their per-user connection. The admin
// server configuration always wins; the connection only supplies a client when
// the operator did not configure one.
func ServerOAuthConfigForConnection(server *models.MCPServerConfiguration, connection *models.MCPConnection) oauth.ServerConfig {
	config := ServerOAuthConfig(server)
	if connection == nil {
		return config
	}
	if strings.TrimSpace(config.ClientID) == "" {
		if clientId := strings.TrimSpace(connection.GetOAuthClientID()); clientId != "" {
			config.ClientID = clientId
			// Only adopt a connection-scoped secret when the connection also
			// supplied the client id, so a dynamically-registered confidential
			// client authenticates with its matching secret.
			config.ClientSecret = strings.TrimSpace(connection.GetOAuthClientSecret())
		}
	}
	return config
}

// tokenTypeOrBearer returns the token type, defaulting to "Bearer" when empty.
func tokenTypeOrBearer(tokenType string) string {
	if trimmed := strings.TrimSpace(tokenType); trimmed != "" {
		return trimmed
	}
	return "Bearer"
}

// validateServerUrl parses and sanity-checks a configured MCP server URL,
// requiring an absolute http(s) URL with a host. It returns the parsed URL so
// callers can inspect the scheme and host without re-parsing.
func validateServerUrl(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("mcp: parsing server url: %w", err)
	}
	switch parsed.Scheme {
	case "http", "https":
	default:
		return nil, fmt.Errorf("mcp: server url has unsupported scheme %q (want http or https)", parsed.Scheme)
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("mcp: server url is missing a host")
	}
	return parsed, nil
}

// isLoopbackHost reports whether host names the local machine, for which
// plaintext HTTP carries no network exposure.
func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
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

// forEachConfiguredServer invokes register for each enabled, uniquely-named MCP
// server in the configuration, passing a base ServerConfiguration (with the
// Authorization left unset) and the server's resolved auth mode. HTTP servers
// require a valid URL; stdio servers require a command. Disabled, duplicate, and
// incomplete entries are dropped (and logged).
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
		if name == "" {
			log.Warningf("mcp: skipping server with empty name")
			continue
		}
		if seen[name] {
			log.Warningf("mcp: skipping duplicate server name %q", name)
			continue
		}
		timeout := defaultTimeout
		if seconds := server.GetTimeoutSeconds(); seconds > 0 {
			timeout = time.Duration(seconds) * time.Second
		}

		if server.ResolvedTransport() == models.MCPServerTransportStdio {
			command := strings.TrimSpace(server.GetCommand())
			if command == "" {
				log.Warningf("mcp: skipping stdio server %q: no command", name)
				continue
			}
			seen[name] = true
			// Stdio servers run as a local subprocess and use no HTTP auth; they
			// are shared across users like a "none"-auth server.
			register(ServerConfiguration{
				Name:        name,
				Transport:   TransportStdio,
				Command:     command,
				Arguments:   append([]string(nil), server.GetArgs()...),
				Environment: stdioEnvEntries(server.GetEnv()),
				WorkingDir:  strings.TrimSpace(server.GetWorkingDir()),
				Timeout:     timeout,
			}, models.MCPServerAuthNone, server)
			continue
		}

		serverUrl := strings.TrimSpace(server.GetURL())
		if serverUrl == "" {
			log.Warningf("mcp: skipping server %q with empty url", name)
			continue
		}
		parsedUrl, urlError := validateServerUrl(serverUrl)
		if urlError != nil {
			log.Warningf("mcp: skipping server %q: %v", name, urlError)
			continue
		}
		seen[name] = true
		mode := server.ResolvedAuthMode()
		// A credential travelling over plaintext HTTP to a non-loopback host is a
		// real exposure: warn so the operator notices, but still register the
		// server (loopback HTTP is a normal local-development setup).
		if mode != models.MCPServerAuthNone && parsedUrl.Scheme == "http" && !isLoopbackHost(parsedUrl.Hostname()) {
			log.Warningf("mcp: server %q sends credentials over plaintext http to %q; use https", name, parsedUrl.Host)
		}
		register(ServerConfiguration{Name: name, Transport: TransportHTTP, URL: serverUrl, Timeout: timeout}, mode, server)
	}
}

// stdioEnvEntries flattens a server's environment map into the sorted
// "KEY=VALUE" entries the subprocess transport expects. Sorting keeps the
// discovery cache signature stable across runs (map iteration order is random).
func stdioEnvEntries(environment map[string]string) []string {
	if len(environment) == 0 {
		return nil
	}
	entries := make([]string, 0, len(environment))
	for key, value := range environment {
		entries = append(entries, key+"="+value)
	}
	sort.Strings(entries)
	return entries
}
