// Package oauth implements the subset of the OAuth 2.1 authorization-code flow
// with PKCE needed to authenticate against protected remote MCP servers.
//
// Scope and limitations:
//
//   - Grant: authorization code with PKCE (S256) only. Implicit and password
//     grants are intentionally unsupported.
//   - Endpoint resolution: explicit authorization/token endpoints take
//     precedence; otherwise they are discovered from the resource server via
//     RFC 9728 protected-resource metadata followed by RFC 8414 authorization
//     server metadata (with the OpenID Connect discovery document as a
//     fallback).
//   - Client authentication: public clients (PKCE, no secret) and confidential
//     clients (client_secret_post) are supported.
//   - Dynamic client registration: when the authorization server advertises a
//     registration endpoint (RFC 8414 registration_endpoint) and no client is
//     pre-configured, a public client can be registered on the fly (RFC 7591).
//
// Tokens and PKCE verifiers handled here are secrets; callers must never expose
// them to API clients.
package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/op/go-logging"
)

// log satisfies the project's per-package logger convention. The OAuth client
// surfaces failures through returned errors; richer diagnostics are layered on
// in the hardening pass.
var log = logging.MustGetLogger("oauth") //nolint:unused

// defaultTimeout bounds a single OAuth-related HTTP request.
const defaultTimeout = 30 * time.Second

// ServerConfig holds the OAuth client configuration for one MCP server.
type ServerConfig struct {
	// ClientID is the registered OAuth client identifier.
	ClientID string
	// ClientSecret is sent via client_secret_post for confidential clients.
	// Empty selects a public client that relies on PKCE alone.
	ClientSecret string
	// Scopes requested during authorization.
	Scopes []string
	// AuthorizationURL and TokenURL, when set, bypass discovery.
	AuthorizationURL string
	TokenURL         string
	// ResourceURL is the MCP server endpoint, used both as the discovery anchor
	// and as the RFC 8707 resource indicator.
	ResourceURL string
}

// Client performs OAuth requests for a single server configuration.
type Client struct {
	config     ServerConfig
	httpClient *http.Client
}

// NewClient builds a Client for the given configuration.
func NewClient(config ServerConfig) *Client {
	return &Client{
		config:     config,
		httpClient: &http.Client{Timeout: defaultTimeout},
	}
}

// PKCE holds a generated code verifier and its S256 challenge.
type PKCE struct {
	Verifier  string
	Challenge string
	Method    string
}

// GeneratePKCE returns a fresh PKCE verifier/challenge pair using the S256
// method as required by OAuth 2.1.
func GeneratePKCE() (PKCE, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return PKCE{}, fmt.Errorf("oauth: generating verifier: %w", err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	return PKCE{Verifier: verifier, Challenge: challenge, Method: "S256"}, nil
}

// GenerateState returns an unguessable opaque value for CSRF protection.
func GenerateState() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("oauth: generating state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// Metadata is the subset of authorization-server metadata this client consumes.
type Metadata struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	// RegistrationEndpoint is the optional RFC 7591 dynamic client registration
	// endpoint, advertised by RFC 8414 metadata. Empty when the server does not
	// support dynamic registration.
	RegistrationEndpoint string `json:"registration_endpoint"`
}

// protectedResourceMetadata is the subset of RFC 9728 metadata used to locate
// the authorization server for a resource.
type protectedResourceMetadata struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
}

// Endpoints resolves the authorization and token endpoints for the server,
// using explicit configuration when present and otherwise discovering them.
func (self *Client) Endpoints(ctx context.Context) (authorizationEndpoint string, tokenEndpoint string, err error) {
	if self.config.AuthorizationURL != "" && self.config.TokenURL != "" {
		return self.config.AuthorizationURL, self.config.TokenURL, nil
	}
	metadata, discoverError := self.discover(ctx)
	if discoverError != nil {
		return "", "", discoverError
	}
	authorizationEndpoint = self.config.AuthorizationURL
	if authorizationEndpoint == "" {
		authorizationEndpoint = metadata.AuthorizationEndpoint
	}
	tokenEndpoint = self.config.TokenURL
	if tokenEndpoint == "" {
		tokenEndpoint = metadata.TokenEndpoint
	}
	if authorizationEndpoint == "" || tokenEndpoint == "" {
		return "", "", fmt.Errorf("oauth: could not resolve authorization/token endpoints for %q", self.config.ResourceURL)
	}
	return authorizationEndpoint, tokenEndpoint, nil
}

// DiscoverMetadata resolves and returns the authorization-server metadata for
// the configured resource, including the optional dynamic client registration
// endpoint. It always performs discovery (explicit endpoint configuration does
// not advertise a registration endpoint).
func (self *Client) DiscoverMetadata(ctx context.Context) (Metadata, error) {
	return self.discover(ctx)
}

// discover locates the authorization server for the configured resource and
// returns its metadata.
func (self *Client) discover(ctx context.Context) (Metadata, error) {
	issuer := self.resourceOrigin()
	// Best-effort RFC 9728 protected-resource metadata to redirect us to the
	// authorization server when it differs from the resource origin.
	if resourceMetadata, resourceError := self.fetchProtectedResourceMetadata(ctx); resourceError == nil {
		for _, server := range resourceMetadata.AuthorizationServers {
			if strings.TrimSpace(server) != "" {
				issuer = strings.TrimRight(strings.TrimSpace(server), "/")
				break
			}
		}
	}
	for _, candidate := range authorizationServerMetadataURLs(issuer) {
		metadata, fetchError := self.fetchMetadata(ctx, candidate)
		if fetchError == nil && metadata.TokenEndpoint != "" {
			return metadata, nil
		}
	}
	return Metadata{}, fmt.Errorf("oauth: no authorization server metadata found for %q", issuer)
}

func (self *Client) fetchProtectedResourceMetadata(ctx context.Context) (protectedResourceMetadata, error) {
	metadataUrl := self.resourceOrigin() + "/.well-known/oauth-protected-resource"
	body, err := self.fetch(ctx, metadataUrl)
	if err != nil {
		return protectedResourceMetadata{}, err
	}
	var metadata protectedResourceMetadata
	if unmarshalError := json.Unmarshal(body, &metadata); unmarshalError != nil {
		return protectedResourceMetadata{}, unmarshalError
	}
	return metadata, nil
}

func (self *Client) fetchMetadata(ctx context.Context, metadataUrl string) (Metadata, error) {
	body, err := self.fetch(ctx, metadataUrl)
	if err != nil {
		return Metadata{}, err
	}
	var metadata Metadata
	if unmarshalError := json.Unmarshal(body, &metadata); unmarshalError != nil {
		return Metadata{}, unmarshalError
	}
	return metadata, nil
}

func (self *Client) fetch(ctx context.Context, fetchUrl string) ([]byte, error) {
	request, requestError := http.NewRequestWithContext(ctx, http.MethodGet, fetchUrl, nil)
	if requestError != nil {
		return nil, requestError
	}
	request.Header.Set("Accept", "application/json")
	response, err := self.httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode >= 400 {
		return nil, fmt.Errorf("oauth: %s returned status %d", fetchUrl, response.StatusCode)
	}
	return io.ReadAll(io.LimitReader(response.Body, 1<<20))
}

// resourceOrigin returns the scheme://host[:port] of the resource URL.
func (self *Client) resourceOrigin() string {
	parsed, err := url.Parse(self.config.ResourceURL)
	if err != nil || parsed.Host == "" {
		return strings.TrimRight(self.config.ResourceURL, "/")
	}
	return parsed.Scheme + "://" + parsed.Host
}

// authorizationServerMetadataURLs returns the candidate metadata document URLs
// for an issuer, in priority order.
func authorizationServerMetadataURLs(issuer string) []string {
	issuer = strings.TrimRight(issuer, "/")
	return []string{
		issuer + "/.well-known/oauth-authorization-server",
		issuer + "/.well-known/openid-configuration",
	}
}

// AuthorizationURL builds the authorization request URL the user is redirected
// to.
func (self *Client) AuthorizationURL(authorizationEndpoint, challenge, state, redirectUri string) (string, error) {
	parsed, err := url.Parse(authorizationEndpoint)
	if err != nil {
		return "", fmt.Errorf("oauth: parsing authorization endpoint: %w", err)
	}
	query := parsed.Query()
	query.Set("response_type", "code")
	query.Set("client_id", self.config.ClientID)
	query.Set("redirect_uri", redirectUri)
	query.Set("code_challenge", challenge)
	query.Set("code_challenge_method", "S256")
	query.Set("state", state)
	if len(self.config.Scopes) > 0 {
		query.Set("scope", strings.Join(self.config.Scopes, " "))
	}
	if self.config.ResourceURL != "" {
		query.Set("resource", self.config.ResourceURL)
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

// Token is a normalized OAuth token response.
type Token struct {
	AccessToken  string
	RefreshToken string
	TokenType    string
	Scope        string
	ExpiresAt    time.Time
}

// tokenResponse mirrors the JSON token endpoint response.
type tokenResponse struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	TokenType        string `json:"token_type"`
	ExpiresIn        int64  `json:"expires_in"`
	Scope            string `json:"scope"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// ExchangeCode trades an authorization code for tokens.
func (self *Client) ExchangeCode(ctx context.Context, tokenEndpoint, code, verifier, redirectUri string) (*Token, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectUri)
	form.Set("client_id", self.config.ClientID)
	form.Set("code_verifier", verifier)
	if self.config.ResourceURL != "" {
		form.Set("resource", self.config.ResourceURL)
	}
	return self.postToken(ctx, tokenEndpoint, form)
}

// Refresh exchanges a refresh token for a new access token.
func (self *Client) Refresh(ctx context.Context, tokenEndpoint, refreshToken string) (*Token, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", self.config.ClientID)
	if self.config.ResourceURL != "" {
		form.Set("resource", self.config.ResourceURL)
	}
	return self.postToken(ctx, tokenEndpoint, form)
}

func (self *Client) postToken(ctx context.Context, tokenEndpoint string, form url.Values) (*Token, error) {
	if self.config.ClientSecret != "" {
		form.Set("client_secret", self.config.ClientSecret)
	}
	request, requestError := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, strings.NewReader(form.Encode()))
	if requestError != nil {
		return nil, fmt.Errorf("oauth: building token request: %w", requestError)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response, err := self.httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	body, readError := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if readError != nil {
		return nil, readError
	}
	var parsed tokenResponse
	if unmarshalError := json.Unmarshal(body, &parsed); unmarshalError != nil {
		return nil, fmt.Errorf("oauth: decoding token response (status %d): %w", response.StatusCode, unmarshalError)
	}
	if parsed.Error != "" {
		if parsed.ErrorDescription != "" {
			return nil, fmt.Errorf("oauth: token endpoint error %q: %s", parsed.Error, parsed.ErrorDescription)
		}
		return nil, fmt.Errorf("oauth: token endpoint error %q", parsed.Error)
	}
	if response.StatusCode >= 400 {
		return nil, fmt.Errorf("oauth: token endpoint returned status %d", response.StatusCode)
	}
	if parsed.AccessToken == "" {
		return nil, fmt.Errorf("oauth: token response missing access_token")
	}
	tokenType := parsed.TokenType
	if tokenType == "" {
		tokenType = "Bearer"
	}
	token := &Token{
		AccessToken:  parsed.AccessToken,
		RefreshToken: parsed.RefreshToken,
		TokenType:    tokenType,
		Scope:        parsed.Scope,
	}
	if parsed.ExpiresIn > 0 {
		token.ExpiresAt = time.Now().Add(time.Duration(parsed.ExpiresIn) * time.Second)
	}
	return token, nil
}

// ClientRegistrationRequest is the subset of RFC 7591 client metadata TeaNode
// sends when dynamically registering an OAuth client.
type ClientRegistrationRequest struct {
	ClientName              string   `json:"client_name,omitempty"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types,omitempty"`
	ResponseTypes           []string `json:"response_types,omitempty"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
	Scope                   string   `json:"scope,omitempty"`
}

// ClientRegistration is the subset of an RFC 7591 registration response that
// TeaNode persists. ClientSecret is empty for public clients.
type ClientRegistration struct {
	ClientID     string
	ClientSecret string
}

// PublicClientRegistrationRequest builds an RFC 7591 registration request for a
// public client (PKCE, no client secret) that supports the authorization-code
// and refresh-token grants. The configured scopes are requested at registration
// time so the issued client is allowed to ask for them.
func (self *Client) PublicClientRegistrationRequest(clientName, redirectUri string) ClientRegistrationRequest {
	return ClientRegistrationRequest{
		ClientName:              clientName,
		RedirectURIs:            []string{redirectUri},
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		ResponseTypes:           []string{"code"},
		TokenEndpointAuthMethod: "none",
		Scope:                   strings.Join(self.config.Scopes, " "),
	}
}

// clientRegistrationResponse mirrors the JSON registration endpoint response.
type clientRegistrationResponse struct {
	ClientID         string `json:"client_id"`
	ClientSecret     string `json:"client_secret"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// Register performs RFC 7591 dynamic client registration against the given
// registration endpoint and returns the issued client identifier (and secret,
// when the server insists on a confidential client).
func (self *Client) Register(ctx context.Context, registrationEndpoint string, registration ClientRegistrationRequest) (*ClientRegistration, error) {
	payload, marshalError := json.Marshal(registration)
	if marshalError != nil {
		return nil, fmt.Errorf("oauth: encoding registration request: %w", marshalError)
	}
	request, requestError := http.NewRequestWithContext(ctx, http.MethodPost, registrationEndpoint, strings.NewReader(string(payload)))
	if requestError != nil {
		return nil, fmt.Errorf("oauth: building registration request: %w", requestError)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := self.httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	body, readError := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if readError != nil {
		return nil, readError
	}
	var parsed clientRegistrationResponse
	if unmarshalError := json.Unmarshal(body, &parsed); unmarshalError != nil {
		return nil, fmt.Errorf("oauth: decoding registration response (status %d): %w", response.StatusCode, unmarshalError)
	}
	if parsed.Error != "" {
		if parsed.ErrorDescription != "" {
			return nil, fmt.Errorf("oauth: registration endpoint error %q: %s", parsed.Error, parsed.ErrorDescription)
		}
		return nil, fmt.Errorf("oauth: registration endpoint error %q", parsed.Error)
	}
	if response.StatusCode >= 400 {
		return nil, fmt.Errorf("oauth: registration endpoint returned status %d", response.StatusCode)
	}
	if parsed.ClientID == "" {
		return nil, fmt.Errorf("oauth: registration response missing client_id")
	}
	return &ClientRegistration{ClientID: parsed.ClientID, ClientSecret: parsed.ClientSecret}, nil
}
