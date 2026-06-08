package api

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/teanode/teanode/internal/mcp"
	"github.com/teanode/teanode/internal/mcp/oauth"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
)

// --- MCP server / connection RPC handlers ---
//
// These endpoints let a user see which remote MCP servers the operator has
// configured and manage their own per-user connection credentials. Server
// credentials (the static node-level Authorization value and any per-user
// connection secret) are never returned to clients.

// maxAuthorizationLength bounds the per-user credential value accepted for a
// user-scoped MCP connection. HTTP Authorization header values are far smaller
// than this; the cap simply rejects clearly-invalid input.
const maxAuthorizationLength = 8192

// mcpServerListItem is the user-facing view of an admin-configured MCP server,
// combined with the current user's connection state for that server.
type mcpServerListItem struct {
	Name               string     `json:"name"`
	URL                string     `json:"url"`
	AuthMode           string     `json:"authMode"`
	Enabled            bool       `json:"enabled"`
	RequiresConnection bool       `json:"requiresConnection"`
	Connected          bool       `json:"connected"`
	ConnectionID       string     `json:"connectionId,omitempty"`
	Status             string     `json:"status,omitempty"`
	LastError          string     `json:"lastError,omitempty"`
	LastConnectedAt    *time.Time `json:"lastConnectedAt,omitempty"`
}

// mcpConnectionListItem is the secret-free view of a per-user MCP connection.
type mcpConnectionListItem struct {
	ID              string     `json:"id"`
	ServerName      string     `json:"serverName"`
	Status          string     `json:"status"`
	LastError       string     `json:"lastError,omitempty"`
	CreatedAt       *time.Time `json:"createdAt,omitempty"`
	LastConnectedAt *time.Time `json:"lastConnectedAt,omitempty"`
}

func toMcpConnectionListItem(connection *models.MCPConnection) mcpConnectionListItem {
	return mcpConnectionListItem{
		ID:              connection.ID,
		ServerName:      connection.GetServerName(),
		Status:          string(connection.GetStatus()),
		LastError:       connection.GetLastError(),
		CreatedAt:       connection.CreatedAt,
		LastConnectedAt: connection.LastConnectedAt,
	}
}

// handleMcpServersList returns the configured MCP servers and whether the
// current user has connected to each user-scoped server.
func (self *webSocketConnection) handleMcpServersList(frame requestFrame) (interface{}, error) {
	items := make([]mcpServerListItem, 0)
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		configuration, err := transaction.GetConfiguration(ctx, nil)
		if err != nil {
			return err
		}
		connectionsByServer := map[string]*models.MCPConnection{}
		connections, listError := transaction.ListMCPConnections(ctx, self.userId(), nil)
		if listError != nil {
			return listError
		}
		for _, connection := range connections {
			connectionsByServer[connection.GetServerName()] = connection
		}
		if configuration.Tools == nil || configuration.Tools.MCP == nil {
			return nil
		}
		seen := map[string]bool{}
		for _, server := range configuration.Tools.MCP.GetServers() {
			if server == nil {
				continue
			}
			name := strings.TrimSpace(server.GetName())
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			authMode := server.ResolvedAuthMode()
			item := mcpServerListItem{
				Name:               name,
				URL:                server.GetURL(),
				AuthMode:           string(authMode),
				Enabled:            server.Enabled == nil || *server.Enabled,
				RequiresConnection: authMode == models.MCPServerAuthUser || authMode == models.MCPServerAuthOAuth,
			}
			if connection := connectionsByServer[name]; connection != nil {
				item.Connected = connection.GetStatus() == models.MCPConnectionStatusConnected
				item.ConnectionID = connection.ID
				item.Status = string(connection.GetStatus())
				item.LastError = connection.GetLastError()
				item.LastConnectedAt = connection.LastConnectedAt
			}
			items = append(items, item)
		}
		return nil
	}); err != nil {
		return nil, rpcError(500, "failed to list MCP servers")
	}
	sort.Slice(items, func(left, right int) bool {
		return items[left].Name < items[right].Name
	})
	return map[string]interface{}{"servers": items}, nil
}

// handleMcpConnectionsList returns the current user's MCP connections without
// exposing any stored credential.
func (self *webSocketConnection) handleMcpConnectionsList(frame requestFrame) (interface{}, error) {
	items := make([]mcpConnectionListItem, 0)
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		connections, err := transaction.ListMCPConnections(ctx, self.userId(), nil)
		if err != nil {
			return err
		}
		for _, connection := range connections {
			items = append(items, toMcpConnectionListItem(connection))
		}
		return nil
	}); err != nil {
		return nil, rpcError(500, "failed to list MCP connections")
	}
	sort.Slice(items, func(left, right int) bool {
		return items[left].ServerName < items[right].ServerName
	})
	return map[string]interface{}{"connections": items}, nil
}

type mcpConnectionsCreateParameters struct {
	ServerName    string `json:"serverName"`
	Authorization string `json:"authorization"`
}

// handleMcpConnectionsCreate stores a per-user credential for a user-scoped MCP
// server. The credential is sent verbatim as the HTTP Authorization header.
func (self *webSocketConnection) handleMcpConnectionsCreate(frame requestFrame) (interface{}, error) {
	parameters, err := unmarshalParameters[mcpConnectionsCreateParameters](frame)
	if err != nil {
		return nil, err
	}
	serverName := strings.TrimSpace(parameters.ServerName)
	authorization := strings.TrimSpace(parameters.Authorization)
	if serverName == "" {
		return nil, rpcError(400, "serverName is required")
	}
	if authorization == "" {
		return nil, rpcError(400, "authorization is required")
	}
	// Bound the credential size so a malformed or pasted-document value cannot be
	// stored as an Authorization header (real header values are well under this).
	if len(authorization) > maxAuthorizationLength {
		return nil, rpcError(400, "authorization is too long")
	}

	var created *models.MCPConnection
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		configuration, configError := transaction.GetConfiguration(ctx, nil)
		if configError != nil {
			return configError
		}
		server := findConfiguredMcpServer(configuration, serverName)
		if server == nil {
			return web400("unknown MCP server")
		}
		if server.ResolvedAuthMode() != models.MCPServerAuthUser {
			return web400("server does not accept per-user connections")
		}
		if _, existingError := transaction.GetMCPConnectionByServer(ctx, self.userId(), serverName, nil); existingError == nil {
			return web400("a connection for this server already exists")
		} else if !errors.Is(existingError, store.ErrNotFound) {
			return existingError
		}
		connection, createError := transaction.CreateMCPConnection(ctx, &models.MCPConnection{
			UserID:        ptrto.Value(self.userId()),
			ServerName:    ptrto.Value(serverName),
			Status:        ptrto.Value(models.MCPConnectionStatusConnected),
			Authorization: ptrto.Value(authorization),
		}, nil)
		if createError != nil {
			return createError
		}
		created = connection
		return nil
	}); err != nil {
		var badRequest *badRequestError
		if errors.As(err, &badRequest) {
			return nil, rpcError(400, badRequest.message)
		}
		return nil, rpcError(500, "failed to create MCP connection")
	}
	return map[string]interface{}{"connection": toMcpConnectionListItem(created)}, nil
}

type mcpConnectionsDeleteParameters struct {
	ConnectionID string `json:"connectionId"`
}

// handleMcpConnectionsDelete removes one of the current user's MCP connections.
func (self *webSocketConnection) handleMcpConnectionsDelete(frame requestFrame) (interface{}, error) {
	parameters, err := unmarshalParameters[mcpConnectionsDeleteParameters](frame)
	if err != nil {
		return nil, err
	}
	connectionId := strings.TrimSpace(parameters.ConnectionID)
	if connectionId == "" {
		return nil, rpcError(400, "connectionId is required")
	}
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		connection, getError := transaction.GetMCPConnection(ctx, connectionId, nil)
		if getError != nil {
			return getError
		}
		if connection.GetUserID() != self.userId() {
			return store.ErrNotFound
		}
		return transaction.DeleteMCPConnection(ctx, connectionId, nil)
	}); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, rpcError(404, "connection not found")
		}
		return nil, rpcError(500, "failed to delete MCP connection")
	}
	return map[string]interface{}{"deleted": true, "connectionId": connectionId}, nil
}

type mcpConnectionsAuthorizeParameters struct {
	ServerName string `json:"serverName"`
}

// handleMcpConnectionsAuthorize starts the OAuth authorization-code flow for an
// oauth-mode server and returns the authorization URL the user must visit. A
// pending connection holding the PKCE verifier and CSRF state is persisted so
// the callback can complete the exchange.
func (self *webSocketConnection) handleMcpConnectionsAuthorize(frame requestFrame) (interface{}, error) {
	parameters, err := unmarshalParameters[mcpConnectionsAuthorizeParameters](frame)
	if err != nil {
		return nil, err
	}
	serverName := strings.TrimSpace(parameters.ServerName)
	if serverName == "" {
		return nil, rpcError(400, "serverName is required")
	}

	// Resolve the server configuration and redirect URI in a read-only step so
	// the (potentially slow) discovery network call happens outside the store
	// transaction.
	var oauthConfig oauth.ServerConfig
	var redirectUri string
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		configuration, configError := transaction.GetConfiguration(ctx, nil)
		if configError != nil {
			return configError
		}
		server := findConfiguredMcpServer(configuration, serverName)
		if server == nil {
			return web400("unknown MCP server")
		}
		if server.ResolvedAuthMode() != models.MCPServerAuthOAuth {
			return web400("server is not configured for OAuth")
		}
		if strings.TrimSpace(server.GetOAuthClientID()) == "" {
			return web400("server is missing an OAuth client id")
		}
		redirectUri = mcpOAuthRedirectUri(configuration)
		if redirectUri == "" {
			return web400("node public URL must be configured for OAuth connections")
		}
		oauthConfig = serverOAuthConfig(server)
		return nil
	}); err != nil {
		return nil, mcpBadRequestOrInternal(err, "failed to start authorization")
	}

	oauthClient := oauth.NewClient(oauthConfig)
	authorizationEndpoint, _, endpointsError := oauthClient.Endpoints(self.ctx)
	if endpointsError != nil {
		return nil, rpcError(502, "failed to resolve OAuth endpoints: "+endpointsError.Error())
	}
	pkce, pkceError := oauth.GeneratePKCE()
	if pkceError != nil {
		return nil, rpcError(500, "failed to prepare authorization")
	}
	state, stateError := oauth.GenerateState()
	if stateError != nil {
		return nil, rpcError(500, "failed to prepare authorization")
	}
	authorizationUrl, urlError := oauthClient.AuthorizationURL(authorizationEndpoint, pkce.Challenge, state, redirectUri)
	if urlError != nil {
		return nil, rpcError(500, "failed to build authorization URL")
	}

	// Upsert a pending connection holding the transient PKCE/state values.
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		existing, getError := transaction.GetMCPConnectionByServer(ctx, self.userId(), serverName, nil)
		if getError == nil {
			_, modifyError := transaction.ModifyMCPConnection(ctx, existing.ID, func(connection *models.MCPConnection) error {
				connection.Status = ptrto.Value(models.MCPConnectionStatusPending)
				connection.OAuthState = ptrto.Value(state)
				connection.CodeVerifier = ptrto.Value(pkce.Verifier)
				connection.LastError = ptrto.Value("")
				return nil
			}, nil)
			return modifyError
		}
		if !errors.Is(getError, store.ErrNotFound) {
			return getError
		}
		_, createError := transaction.CreateMCPConnection(ctx, &models.MCPConnection{
			UserID:       ptrto.Value(self.userId()),
			ServerName:   ptrto.Value(serverName),
			Status:       ptrto.Value(models.MCPConnectionStatusPending),
			OAuthState:   ptrto.Value(state),
			CodeVerifier: ptrto.Value(pkce.Verifier),
		}, nil)
		return createError
	}); err != nil {
		return nil, rpcError(500, "failed to persist authorization state")
	}

	return map[string]interface{}{"authorizationUrl": authorizationUrl}, nil
}

// serverOAuthConfig builds the OAuth client configuration for a server. It
// delegates to the mcp package so the authorize flow and the per-user runner
// registration resolve OAuth settings identically.
func serverOAuthConfig(server *models.MCPServerConfiguration) oauth.ServerConfig {
	return mcp.ServerOAuthConfig(server)
}

// mcpOAuthRedirectUri derives the OAuth callback URL from the node public URL.
func mcpOAuthRedirectUri(configuration *models.Configuration) string {
	if configuration == nil || configuration.Node == nil {
		return ""
	}
	publicUrl := strings.TrimSpace(configuration.Node.GetPublicURL())
	if publicUrl == "" {
		return ""
	}
	return strings.TrimRight(publicUrl, "/") + mcpOAuthCallbackPath
}

// mcpBadRequestOrInternal maps a transaction error to a 400 when it is a
// badRequestError and a 500 otherwise.
func mcpBadRequestOrInternal(err error, internalMessage string) *rpcHandlerError {
	var badRequest *badRequestError
	if errors.As(err, &badRequest) {
		return rpcError(400, badRequest.message)
	}
	return rpcError(500, internalMessage)
}

// findConfiguredMcpServer returns the configured server with the given name, or
// nil when no enabled server matches.
func findConfiguredMcpServer(configuration *models.Configuration, name string) *models.MCPServerConfiguration {
	if configuration == nil || configuration.Tools == nil || configuration.Tools.MCP == nil {
		return nil
	}
	for _, server := range configuration.Tools.MCP.GetServers() {
		if server == nil {
			continue
		}
		if strings.TrimSpace(server.GetName()) == name {
			return server
		}
	}
	return nil
}

// badRequestError lets a transaction closure signal a 400-class validation
// failure that should surface to the client rather than a generic 500.
type badRequestError struct {
	message string
}

func (self *badRequestError) Error() string {
	return self.message
}

func web400(message string) error {
	return &badRequestError{message: message}
}
