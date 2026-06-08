package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestGeneratePKCE(t *testing.T) {
	pkce, err := GeneratePKCE()
	if err != nil {
		t.Fatalf("GeneratePKCE error: %v", err)
	}
	if pkce.Method != "S256" {
		t.Errorf("method = %q, want S256", pkce.Method)
	}
	if len(pkce.Verifier) < 43 {
		t.Errorf("verifier too short: %d", len(pkce.Verifier))
	}
	sum := sha256.Sum256([]byte(pkce.Verifier))
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	if pkce.Challenge != want {
		t.Errorf("challenge mismatch: got %q want %q", pkce.Challenge, want)
	}
	// Two generations must differ.
	other, _ := GeneratePKCE()
	if other.Verifier == pkce.Verifier {
		t.Error("expected distinct verifiers")
	}
}

func TestAuthorizationURL(t *testing.T) {
	client := NewClient(ServerConfig{
		ClientID:    "client-123",
		Scopes:      []string{"read", "write"},
		ResourceURL: "https://mcp.example.com/mcp",
	})
	authorizationURL, err := client.AuthorizationURL("https://auth.example.com/authorize", "challenge-xyz", "state-abc", "https://node.example.com/api/mcp/oauth/callback")
	if err != nil {
		t.Fatalf("AuthorizationURL error: %v", err)
	}
	parsed, parseError := url.Parse(authorizationURL)
	if parseError != nil {
		t.Fatalf("parse error: %v", parseError)
	}
	query := parsed.Query()
	cases := map[string]string{
		"response_type":         "code",
		"client_id":             "client-123",
		"code_challenge":        "challenge-xyz",
		"code_challenge_method": "S256",
		"state":                 "state-abc",
		"scope":                 "read write",
		"resource":              "https://mcp.example.com/mcp",
		"redirect_uri":          "https://node.example.com/api/mcp/oauth/callback",
	}
	for key, want := range cases {
		if got := query.Get(key); got != want {
			t.Errorf("query[%q] = %q, want %q", key, got, want)
		}
	}
}

// newStubAuthorizationServer serves discovery metadata and a token endpoint.
func newStubAuthorizationServer(t *testing.T) (*httptest.Server, *int) {
	t.Helper()
	exchanges := 0
	mux := http.NewServeMux()
	var serverURL string
	mux.HandleFunc("/.well-known/oauth-protected-resource", func(writer http.ResponseWriter, request *http.Request) {
		_ = json.NewEncoder(writer).Encode(protectedResourceMetadata{
			Resource:             serverURL + "/mcp",
			AuthorizationServers: []string{serverURL},
		})
	})
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(writer http.ResponseWriter, request *http.Request) {
		_ = json.NewEncoder(writer).Encode(Metadata{
			Issuer:                serverURL,
			AuthorizationEndpoint: serverURL + "/authorize",
			TokenEndpoint:         serverURL + "/token",
		})
	})
	mux.HandleFunc("/token", func(writer http.ResponseWriter, request *http.Request) {
		exchanges++
		if parseError := request.ParseForm(); parseError != nil {
			http.Error(writer, "bad form", http.StatusBadRequest)
			return
		}
		switch request.Form.Get("grant_type") {
		case "authorization_code":
			if request.Form.Get("code_verifier") == "" {
				http.Error(writer, "missing verifier", http.StatusBadRequest)
				return
			}
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"access_token":"access-1","refresh_token":"refresh-1","token_type":"Bearer","expires_in":3600,"scope":"read"}`))
		case "refresh_token":
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"access_token":"access-2","refresh_token":"refresh-2","token_type":"Bearer","expires_in":3600}`))
		default:
			http.Error(writer, "unsupported grant", http.StatusBadRequest)
		}
	})
	server := httptest.NewServer(mux)
	serverURL = server.URL
	t.Cleanup(server.Close)
	return server, &exchanges
}

func TestEndpointsDiscovery(t *testing.T) {
	server, _ := newStubAuthorizationServer(t)
	client := NewClient(ServerConfig{ClientID: "c", ResourceURL: server.URL + "/mcp"})
	authorizationEndpoint, tokenEndpoint, err := client.Endpoints(context.Background())
	if err != nil {
		t.Fatalf("Endpoints error: %v", err)
	}
	if authorizationEndpoint != server.URL+"/authorize" {
		t.Errorf("authorization endpoint = %q", authorizationEndpoint)
	}
	if tokenEndpoint != server.URL+"/token" {
		t.Errorf("token endpoint = %q", tokenEndpoint)
	}
}

func TestEndpointsExplicitBypassDiscovery(t *testing.T) {
	client := NewClient(ServerConfig{
		ClientID:         "c",
		AuthorizationURL: "https://auth/authorize",
		TokenURL:         "https://auth/token",
		ResourceURL:      "https://unreachable.invalid/mcp",
	})
	authorizationEndpoint, tokenEndpoint, err := client.Endpoints(context.Background())
	if err != nil {
		t.Fatalf("Endpoints error: %v", err)
	}
	if authorizationEndpoint != "https://auth/authorize" || tokenEndpoint != "https://auth/token" {
		t.Errorf("explicit endpoints not used: %q %q", authorizationEndpoint, tokenEndpoint)
	}
}

func TestExchangeCodeAndRefresh(t *testing.T) {
	server, exchanges := newStubAuthorizationServer(t)
	client := NewClient(ServerConfig{ClientID: "c", ResourceURL: server.URL + "/mcp"})

	token, err := client.ExchangeCode(context.Background(), server.URL+"/token", "code-1", "verifier-1", "https://node/callback")
	if err != nil {
		t.Fatalf("ExchangeCode error: %v", err)
	}
	if token.AccessToken != "access-1" || token.RefreshToken != "refresh-1" {
		t.Errorf("unexpected token: %+v", token)
	}
	if token.TokenType != "Bearer" {
		t.Errorf("token type = %q", token.TokenType)
	}
	if token.ExpiresAt.IsZero() {
		t.Error("expected non-zero ExpiresAt")
	}

	refreshed, refreshError := client.Refresh(context.Background(), server.URL+"/token", "refresh-1")
	if refreshError != nil {
		t.Fatalf("Refresh error: %v", refreshError)
	}
	if refreshed.AccessToken != "access-2" {
		t.Errorf("refreshed access token = %q", refreshed.AccessToken)
	}
	if *exchanges != 2 {
		t.Errorf("expected 2 token exchanges, got %d", *exchanges)
	}
}

func TestDiscoverMetadataIncludesRegistrationEndpoint(t *testing.T) {
	var serverURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(writer http.ResponseWriter, request *http.Request) {
		_ = json.NewEncoder(writer).Encode(Metadata{
			Issuer:                serverURL,
			AuthorizationEndpoint: serverURL + "/authorize",
			TokenEndpoint:         serverURL + "/token",
			RegistrationEndpoint:  serverURL + "/register",
		})
	})
	server := httptest.NewServer(mux)
	serverURL = server.URL
	t.Cleanup(server.Close)

	client := NewClient(ServerConfig{ResourceURL: server.URL + "/mcp"})
	metadata, err := client.DiscoverMetadata(context.Background())
	if err != nil {
		t.Fatalf("DiscoverMetadata error: %v", err)
	}
	if metadata.RegistrationEndpoint != server.URL+"/register" {
		t.Errorf("registration endpoint = %q", metadata.RegistrationEndpoint)
	}
	if metadata.TokenEndpoint != server.URL+"/token" {
		t.Errorf("token endpoint = %q", metadata.TokenEndpoint)
	}
}

// TestDiscoverMetadataPathAwareWellKnown reproduces servers (e.g. Robinhood's
// MCP at /mcp/trading) that publish RFC 9728 protected-resource metadata at the
// path-aware well-known location rather than the host root.
func TestDiscoverMetadataPathAwareWellKnown(t *testing.T) {
	var serverURL string
	mux := http.NewServeMux()
	// Only the path-aware location is served; the host-root form 404s.
	mux.HandleFunc("/.well-known/oauth-protected-resource/mcp/trading", func(writer http.ResponseWriter, request *http.Request) {
		_ = json.NewEncoder(writer).Encode(protectedResourceMetadata{
			Resource:             serverURL + "/mcp/trading",
			AuthorizationServers: []string{serverURL},
		})
	})
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(writer http.ResponseWriter, request *http.Request) {
		_ = json.NewEncoder(writer).Encode(Metadata{
			Issuer:                serverURL,
			AuthorizationEndpoint: serverURL + "/authorize",
			TokenEndpoint:         serverURL + "/token",
		})
	})
	server := httptest.NewServer(mux)
	serverURL = server.URL
	t.Cleanup(server.Close)

	client := NewClient(ServerConfig{ResourceURL: server.URL + "/mcp/trading"})
	metadata, err := client.DiscoverMetadata(context.Background())
	if err != nil {
		t.Fatalf("DiscoverMetadata error: %v", err)
	}
	if metadata.TokenEndpoint != server.URL+"/token" {
		t.Errorf("token endpoint = %q", metadata.TokenEndpoint)
	}
}

// TestDiscoverMetadataSurfacesScopes verifies that scopes_supported advertised
// by the resource (RFC 9728) is surfaced even when the authorization-server
// metadata omits its own list. Robinhood's trading MCP requires the "internal"
// scope advertised this way.
func TestDiscoverMetadataSurfacesScopes(t *testing.T) {
	var serverURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/oauth-protected-resource/mcp/trading", func(writer http.ResponseWriter, request *http.Request) {
		_ = json.NewEncoder(writer).Encode(protectedResourceMetadata{
			Resource:             serverURL + "/mcp/trading",
			AuthorizationServers: []string{serverURL},
			ScopesSupported:      []string{"internal"},
		})
	})
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(writer http.ResponseWriter, request *http.Request) {
		// No scopes_supported here; it must fall back to the resource metadata.
		_ = json.NewEncoder(writer).Encode(Metadata{
			Issuer:                serverURL,
			AuthorizationEndpoint: serverURL + "/authorize",
			TokenEndpoint:         serverURL + "/token",
		})
	})
	server := httptest.NewServer(mux)
	serverURL = server.URL
	t.Cleanup(server.Close)

	client := NewClient(ServerConfig{ResourceURL: server.URL + "/mcp/trading"})
	metadata, err := client.DiscoverMetadata(context.Background())
	if err != nil {
		t.Fatalf("DiscoverMetadata error: %v", err)
	}
	if len(metadata.ScopesSupported) != 1 || metadata.ScopesSupported[0] != "internal" {
		t.Errorf("scopes_supported = %v, want [internal]", metadata.ScopesSupported)
	}
}

// TestDiscoverMetadataPathAwareAuthorizationServer covers servers that publish
// the authorization-server metadata itself under the resource path with no
// protected-resource metadata redirect.
func TestDiscoverMetadataPathAwareAuthorizationServer(t *testing.T) {
	var serverURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/oauth-authorization-server/mcp/trading", func(writer http.ResponseWriter, request *http.Request) {
		_ = json.NewEncoder(writer).Encode(Metadata{
			Issuer:                serverURL + "/mcp/trading",
			AuthorizationEndpoint: serverURL + "/authorize",
			TokenEndpoint:         serverURL + "/token",
		})
	})
	server := httptest.NewServer(mux)
	serverURL = server.URL
	t.Cleanup(server.Close)

	client := NewClient(ServerConfig{ResourceURL: server.URL + "/mcp/trading"})
	metadata, err := client.DiscoverMetadata(context.Background())
	if err != nil {
		t.Fatalf("DiscoverMetadata error: %v", err)
	}
	if metadata.TokenEndpoint != server.URL+"/token" {
		t.Errorf("token endpoint = %q", metadata.TokenEndpoint)
	}
}

func TestPublicClientRegistrationRequest(t *testing.T) {
	client := NewClient(ServerConfig{Scopes: []string{"read", "trade"}})
	request := client.PublicClientRegistrationRequest("TeaNode", "https://node.example.com/api/mcp/oauth/callback")
	if request.ClientName != "TeaNode" {
		t.Errorf("client name = %q", request.ClientName)
	}
	if request.TokenEndpointAuthMethod != "none" {
		t.Errorf("token endpoint auth method = %q, want none (public client)", request.TokenEndpointAuthMethod)
	}
	if len(request.RedirectURIs) != 1 || request.RedirectURIs[0] != "https://node.example.com/api/mcp/oauth/callback" {
		t.Errorf("redirect uris = %v", request.RedirectURIs)
	}
	if request.Scope != "read trade" {
		t.Errorf("scope = %q", request.Scope)
	}
	wantGrants := map[string]bool{"authorization_code": true, "refresh_token": true}
	for _, grant := range request.GrantTypes {
		if !wantGrants[grant] {
			t.Errorf("unexpected grant type %q", grant)
		}
		delete(wantGrants, grant)
	}
	if len(wantGrants) != 0 {
		t.Errorf("missing grant types: %v", wantGrants)
	}
}

func TestRegisterPublicClient(t *testing.T) {
	var captured ClientRegistrationRequest
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			http.Error(writer, "want POST", http.StatusMethodNotAllowed)
			return
		}
		if decodeError := json.NewDecoder(request.Body).Decode(&captured); decodeError != nil {
			http.Error(writer, "bad body", http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusCreated)
		_, _ = writer.Write([]byte(`{"client_id":"registered-abc","client_id_issued_at":1700000000}`))
	}))
	t.Cleanup(server.Close)

	client := NewClient(ServerConfig{Scopes: []string{"read"}, ResourceURL: "https://mcp.example.com/mcp"})
	registration, err := client.Register(context.Background(), server.URL, client.PublicClientRegistrationRequest("TeaNode", "https://node/callback"))
	if err != nil {
		t.Fatalf("Register error: %v", err)
	}
	if registration.ClientID != "registered-abc" {
		t.Errorf("client id = %q", registration.ClientID)
	}
	if registration.ClientSecret != "" {
		t.Errorf("expected no client secret for public client, got %q", registration.ClientSecret)
	}
	if captured.TokenEndpointAuthMethod != "none" {
		t.Errorf("server received token_endpoint_auth_method = %q", captured.TokenEndpointAuthMethod)
	}
	if len(captured.RedirectURIs) != 1 || captured.RedirectURIs[0] != "https://node/callback" {
		t.Errorf("server received redirect uris = %v", captured.RedirectURIs)
	}
}

func TestRegisterRejectsErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = writer.Write([]byte(`{"error":"invalid_redirect_uri","error_description":"bad uri"}`))
	}))
	t.Cleanup(server.Close)
	client := NewClient(ServerConfig{})
	if _, err := client.Register(context.Background(), server.URL, ClientRegistrationRequest{}); err == nil || !strings.Contains(err.Error(), "invalid_redirect_uri") {
		t.Errorf("expected invalid_redirect_uri error, got %v", err)
	}
}

func TestRegisterMissingClientIDFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"client_name":"TeaNode"}`))
	}))
	t.Cleanup(server.Close)
	client := NewClient(ServerConfig{})
	if _, err := client.Register(context.Background(), server.URL, ClientRegistrationRequest{}); err == nil || !strings.Contains(err.Error(), "missing client_id") {
		t.Errorf("expected missing client_id error, got %v", err)
	}
}

func TestExchangeCodeRejectsErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = writer.Write([]byte(`{"error":"invalid_grant","error_description":"bad code"}`))
	}))
	t.Cleanup(server.Close)
	client := NewClient(ServerConfig{ClientID: "c"})
	if _, err := client.ExchangeCode(context.Background(), server.URL, "x", "y", "z"); err == nil || !strings.Contains(err.Error(), "invalid_grant") {
		t.Errorf("expected invalid_grant error, got %v", err)
	}
}
