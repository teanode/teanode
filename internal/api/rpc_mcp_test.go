package api

import (
	"context"
	"encoding/json"
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
