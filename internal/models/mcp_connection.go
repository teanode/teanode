package models

import "time"

// MCPConnectionStatus describes the state of a per-user connection to a remote
// MCP server.
type MCPConnectionStatus string

const (
	// MCPConnectionStatusPending means the connection has been created but not
	// yet successfully used against the server.
	MCPConnectionStatusPending MCPConnectionStatus = "pending"
	// MCPConnectionStatusConnected means the stored credential last authenticated
	// successfully.
	MCPConnectionStatusConnected MCPConnectionStatus = "connected"
	// MCPConnectionStatusError means the last attempt to use the connection
	// failed (see LastError).
	MCPConnectionStatusError MCPConnectionStatus = "error"
	// MCPConnectionStatusDisconnected means the connection exists but is
	// deliberately inactive.
	MCPConnectionStatusDisconnected MCPConnectionStatus = "disconnected"
)

// MCPConnection is a per-user authentication binding to a single admin-configured
// remote MCP server. It holds the credential a user supplies (or obtains via
// OAuth) so that user-scoped MCP servers can be reached with that user's own
// authority rather than a shared node-level token.
//
// The Authorization field is a secret: it is sent verbatim in the HTTP
// Authorization header to the server and must never be returned to API clients.
type MCPConnection struct {
	ID         string     `json:"id,omitempty" yaml:"id,omitempty"`
	CreatedAt  *time.Time `json:"createdAt,omitempty" yaml:"createdAt,omitempty"`
	ModifiedAt *time.Time `json:"modifiedAt,omitempty" yaml:"modifiedAt,omitempty"`

	// UserID owns the connection.
	UserID *string `json:"userId,omitempty" yaml:"userId,omitempty"`
	// ServerName references the admin-configured MCP server by its unique name.
	ServerName *string `json:"serverName,omitempty" yaml:"serverName,omitempty"`
	// Status reflects the last known connection state.
	Status *MCPConnectionStatus `json:"status,omitempty" yaml:"status,omitempty"`
	// Authorization is the secret Authorization header value (for example
	// "Bearer <token>") sent to the server on behalf of the user. Never returned
	// to clients.
	Authorization *string `json:"authorization,omitempty" yaml:"authorization,omitempty"`
	// LastError captures the most recent failure reason when Status is error.
	LastError *string `json:"lastError,omitempty" yaml:"lastError,omitempty"`
	// LastConnectedAt records the last time the credential authenticated
	// successfully.
	LastConnectedAt *time.Time `json:"lastConnectedAt,omitempty" yaml:"lastConnectedAt,omitempty"`

	// OAuth token material for the "oauth" auth mode. All fields are secrets and
	// must never be returned to clients.

	// AccessToken is the current OAuth access token.
	AccessToken *string `json:"accessToken,omitempty" yaml:"accessToken,omitempty"`
	// RefreshToken is used to obtain a new access token when it expires.
	RefreshToken *string `json:"refreshToken,omitempty" yaml:"refreshToken,omitempty"`
	// TokenType is the access token type (typically "Bearer").
	TokenType *string `json:"tokenType,omitempty" yaml:"tokenType,omitempty"`
	// TokenExpiresAt is when the access token expires (zero when unknown).
	TokenExpiresAt *time.Time `json:"tokenExpiresAt,omitempty" yaml:"tokenExpiresAt,omitempty"`
	// Scope is the granted OAuth scope.
	Scope *string `json:"scope,omitempty" yaml:"scope,omitempty"`

	// Transient authorization-code-flow state, set while an authorization is in
	// flight and cleared once tokens are obtained. Secrets.

	// OAuthState is the CSRF state value binding the authorization callback.
	OAuthState *string `json:"oauthState,omitempty" yaml:"oauthState,omitempty"`
	// CodeVerifier is the PKCE verifier awaiting the token exchange.
	CodeVerifier *string `json:"codeVerifier,omitempty" yaml:"codeVerifier,omitempty"`
}
