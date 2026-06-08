package fsstore_test

import (
	"context"
	"errors"
	"testing"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
)

// TestMCPConnectionCRUD exercises the per-user MCP connection store operations
// through a create / get / list / modify / delete lifecycle.
func TestMCPConnectionCRUD(t *testing.T) {
	openedStore := openFileSystemStore(t)
	ctx := context.Background()

	var connectionId string
	transactionError := openedStore.Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		created, createError := transaction.CreateMCPConnection(ctx, &models.MCPConnection{
			UserID:        ptrto.Value("user-1"),
			ServerName:    ptrto.Value("robinhood"),
			Status:        ptrto.Value(models.MCPConnectionStatusConnected),
			Authorization: ptrto.Value("Bearer secret-token"),
		}, nil)
		if createError != nil {
			return createError
		}
		connectionId = created.ID
		if connectionId == "" {
			t.Fatal("expected generated connection id")
		}
		if created.GetStatus() != models.MCPConnectionStatusConnected {
			t.Errorf("status = %q, want connected", created.GetStatus())
		}
		if created.GetAuthorization() != "Bearer secret-token" {
			t.Errorf("authorization not stored, got %q", created.GetAuthorization())
		}
		return nil
	})
	if transactionError != nil {
		t.Fatalf("create transaction error: %v", transactionError)
	}

	// GetMCPConnectionByServer returns the stored credential.
	mustTransaction(t, openedStore, func(ctx context.Context, transaction store.Transaction) error {
		byServer, err := transaction.GetMCPConnectionByServer(ctx, "user-1", "robinhood", nil)
		if err != nil {
			t.Fatalf("GetMCPConnectionByServer error: %v", err)
		}
		if byServer.ID != connectionId {
			t.Errorf("byServer id = %q, want %q", byServer.ID, connectionId)
		}
		if byServer.GetAuthorization() != "Bearer secret-token" {
			t.Errorf("authorization round-trip failed: %q", byServer.GetAuthorization())
		}
		return nil
	})

	// A different user does not see the connection.
	mustTransaction(t, openedStore, func(ctx context.Context, transaction store.Transaction) error {
		connections, err := transaction.ListMCPConnections(ctx, "user-2", nil)
		if err != nil {
			t.Fatalf("ListMCPConnections error: %v", err)
		}
		if len(connections) != 0 {
			t.Errorf("expected no connections for user-2, got %d", len(connections))
		}
		return nil
	})

	// Modify updates status and error.
	mustTransaction(t, openedStore, func(ctx context.Context, transaction store.Transaction) error {
		_, err := transaction.ModifyMCPConnection(ctx, connectionId, func(connection *models.MCPConnection) error {
			connection.Status = ptrto.Value(models.MCPConnectionStatusError)
			connection.LastError = ptrto.Value("token expired")
			return nil
		}, nil)
		return err
	})
	mustTransaction(t, openedStore, func(ctx context.Context, transaction store.Transaction) error {
		reloaded, err := transaction.GetMCPConnection(ctx, connectionId, nil)
		if err != nil {
			t.Fatalf("GetMCPConnection error: %v", err)
		}
		if reloaded.GetStatus() != models.MCPConnectionStatusError {
			t.Errorf("status = %q, want error", reloaded.GetStatus())
		}
		if reloaded.GetLastError() != "token expired" {
			t.Errorf("lastError = %q", reloaded.GetLastError())
		}
		return nil
	})

	// Delete removes it.
	mustTransaction(t, openedStore, func(ctx context.Context, transaction store.Transaction) error {
		return transaction.DeleteMCPConnection(ctx, connectionId, nil)
	})
	mustTransaction(t, openedStore, func(ctx context.Context, transaction store.Transaction) error {
		_, err := transaction.GetMCPConnection(ctx, connectionId, nil)
		if !errors.Is(err, store.ErrNotFound) {
			t.Errorf("expected ErrNotFound after delete, got %v", err)
		}
		return nil
	})
}

// mustTransaction runs a store transaction and fails the test if it returns an
// error, keeping the lifecycle test readable.
func mustTransaction(t *testing.T, openedStore store.Store, body func(context.Context, store.Transaction) error) {
	t.Helper()
	if err := openedStore.Transaction(context.Background(), body); err != nil {
		t.Fatalf("transaction error: %v", err)
	}
}

// TestMCPServerAuthRoundTrip verifies the server Auth mode survives the
// configuration model -> record -> model round trip.
func TestMCPServerAuthRoundTrip(t *testing.T) {
	openedStore := openFileSystemStore(t)
	ctx := context.Background()

	userAuth := models.MCPServerAuthUser
	if err := openedStore.Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		_, modifyError := transaction.ModifyConfiguration(ctx, func(configuration *models.Configuration) error {
			configuration.Tools = &models.ToolsConfiguration{
				MCP: &models.MCPConfiguration{
					Servers: &[]*models.MCPServerConfiguration{
						{
							Name: ptrto.Value("per-user"),
							URL:  ptrto.Value("https://example.com/mcp"),
							Auth: &userAuth,
						},
					},
				},
			}
			return nil
		}, nil)
		return modifyError
	}); err != nil {
		t.Fatalf("ModifyConfiguration error: %v", err)
	}

	if err := openedStore.Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		configuration, err := transaction.GetConfiguration(ctx, nil)
		if err != nil {
			return err
		}
		servers := configuration.Tools.MCP.GetServers()
		if len(servers) != 1 {
			t.Fatalf("expected 1 server, got %d", len(servers))
		}
		if servers[0].GetAuth() != models.MCPServerAuthUser {
			t.Errorf("auth mode = %q, want user", servers[0].GetAuth())
		}
		if servers[0].ResolvedAuthMode() != models.MCPServerAuthUser {
			t.Errorf("resolved auth mode = %q, want user", servers[0].ResolvedAuthMode())
		}
		return nil
	}); err != nil {
		t.Fatalf("GetConfiguration error: %v", err)
	}
}
