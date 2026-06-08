package api

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/teanode/teanode/internal/mcp"
	"github.com/teanode/teanode/internal/mcp/oauth"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/web"
)

// mcpOAuthCallbackPath is the redirect URI registered with OAuth providers.
const mcpOAuthCallbackPath = "/api/mcp/oauth/callback"

// frontendConnectionsPath is where the browser is sent after the callback
// completes so the user sees the outcome.
const frontendConnectionsPath = "/settings/connections"

// handleMcpOAuthCallback completes the OAuth authorization-code flow. The OAuth
// provider redirects the user's browser here with a code and the CSRF state
// that binds the request to a pending connection. The browser carries the
// user's session cookie, so the authentication middleware has already resolved
// the user.
func (self *api) handleMcpOAuthCallback(writer http.ResponseWriter, request *http.Request) error {
	query := request.URL.Query()
	if providerError := strings.TrimSpace(query.Get("error")); providerError != "" {
		redirectToConnections(writer, request, "", providerError)
		return nil
	}
	code := strings.TrimSpace(query.Get("code"))
	state := strings.TrimSpace(query.Get("state"))
	if code == "" || state == "" {
		redirectToConnections(writer, request, "", "missing code or state")
		return nil
	}

	user := models.UserFromContext(request.Context())
	if user == nil || user.ID == "" {
		return web.Error(401, "authentication required")
	}

	serverName, callbackError := self.completeMcpOAuth(request.Context(), user.ID, code, state)
	if callbackError != nil {
		redirectToConnections(writer, request, serverName, callbackError.Error())
		return nil
	}
	redirectToConnections(writer, request, serverName, "")
	return nil
}

// completeMcpOAuth resolves the pending connection for the state, exchanges the
// code for tokens, and stores them. It returns the server name (when known) so
// the redirect can reference it.
func (self *api) completeMcpOAuth(ctx context.Context, userId, code, state string) (string, error) {
	dataStore := store.StoreFromContext(ctx)

	var pending *models.MCPConnection
	var oauthConfig oauth.ServerConfig
	var redirectUri string
	if err := dataStore.Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		connections, listError := transaction.ListMCPConnections(ctx, userId, nil)
		if listError != nil {
			return listError
		}
		for _, connection := range connections {
			if connection.GetOAuthState() == state && connection.GetCodeVerifier() != "" {
				pending = connection
				break
			}
		}
		if pending == nil {
			return web.Error(404, "no pending authorization matches this request")
		}
		configuration, configError := transaction.GetConfiguration(ctx, nil)
		if configError != nil {
			return configError
		}
		server := findConfiguredMcpServer(configuration, pending.GetServerName())
		if server == nil {
			return web.Error(400, "the server for this authorization no longer exists")
		}
		// Overlay any dynamically-registered client stored on the pending
		// connection so the token exchange uses the same client id the
		// authorization request was issued with.
		oauthConfig = serverOAuthConfigForConnection(server, pending)
		redirectUri = mcpOAuthRedirectUri(configuration)
		return nil
	}); err != nil {
		serverName := ""
		if pending != nil {
			serverName = pending.GetServerName()
		}
		return serverName, err
	}

	serverName := pending.GetServerName()
	oauthClient := oauth.NewClient(oauthConfig)
	_, tokenEndpoint, endpointsError := oauthClient.Endpoints(ctx)
	if endpointsError != nil {
		self.markMcpConnectionError(ctx, pending.ID, "resolving token endpoint: "+endpointsError.Error())
		return serverName, endpointsError
	}
	token, exchangeError := oauthClient.ExchangeCode(ctx, tokenEndpoint, code, pending.GetCodeVerifier(), redirectUri)
	if exchangeError != nil {
		self.markMcpConnectionError(ctx, pending.ID, "exchanging code: "+exchangeError.Error())
		return serverName, exchangeError
	}

	if err := dataStore.Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		_, modifyError := transaction.ModifyMCPConnection(ctx, pending.ID, func(connection *models.MCPConnection) error {
			applyOAuthToken(connection, token)
			return nil
		}, nil)
		return modifyError
	}); err != nil {
		return serverName, err
	}
	return serverName, nil
}

// applyOAuthToken stores token material on a connection and clears the transient
// authorization state. The token material is applied via the shared
// mcp.ApplyOAuthToken so the callback and the runner refresh path stay in sync.
func applyOAuthToken(connection *models.MCPConnection, token *oauth.Token) {
	mcp.ApplyOAuthToken(connection, token)
	// Clear the one-time PKCE/state values now that the exchange succeeded.
	connection.OAuthState = ptrto.Value("")
	connection.CodeVerifier = ptrto.Value("")
}

// markMcpConnectionError records a failure on the pending connection so the user
// can see why the authorization did not complete.
func (self *api) markMcpConnectionError(ctx context.Context, connectionId, message string) {
	_ = store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		_, modifyError := transaction.ModifyMCPConnection(ctx, connectionId, func(connection *models.MCPConnection) error {
			connection.Status = ptrto.Value(models.MCPConnectionStatusError)
			connection.LastError = ptrto.Value(message)
			connection.OAuthState = ptrto.Value("")
			connection.CodeVerifier = ptrto.Value("")
			return nil
		}, nil)
		return modifyError
	})
}

// redirectToConnections sends the browser back to the connections settings page
// with the outcome encoded in the query string.
func redirectToConnections(writer http.ResponseWriter, request *http.Request, serverName, errorMessage string) {
	query := url.Values{}
	if serverName != "" {
		query.Set("server", serverName)
	}
	if errorMessage != "" {
		query.Set("mcpError", errorMessage)
	} else {
		query.Set("mcpConnected", "1")
	}
	http.Redirect(writer, request, frontendConnectionsPath+"?"+query.Encode(), http.StatusFound)
}
