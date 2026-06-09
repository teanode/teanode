package api

import (
	"context"
	"testing"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/store/fsstore"
	"github.com/teanode/teanode/internal/util/ptrto"
)

// newAdminToolPolicyConnection builds an admin webSocketConnection backed by a
// fresh store, for exercising the tool-policy RPCs.
func newAdminToolPolicyConnection(t *testing.T) (*webSocketConnection, store.Store) {
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
			Username: ptrto.Value("admin"),
			Admin:    ptrto.Value(true),
		}, nil, nil)
		if createError != nil {
			return createError
		}
		userId = user.ID
		return nil
	}); err != nil {
		t.Fatalf("seeding store: %v", err)
	}
	ctx := store.ContextWithStore(context.Background(), openedStore)
	ctx = models.ContextWithUserSessionToken(ctx, &models.User{ID: userId, Username: ptrto.Value("admin"), Admin: ptrto.Value(true)}, nil, nil)
	return &webSocketConnection{ctx: ctx}, openedStore
}

// TestToolPoliciesUpdateAcceptsMCPToolName verifies that a policy for a
// namespaced MCP tool is accepted and persisted even though no MCP server is
// configured/discoverable, so a transient outage cannot drop the setting.
func TestToolPoliciesUpdateAcceptsMCPToolName(t *testing.T) {
	connection, dataStore := newAdminToolPolicyConnection(t)

	_, err := connection.handleToolPoliciesUpdate(frameWith(t, map[string]interface{}{
		"policies": []map[string]interface{}{
			{"tool": "mcp__robinhood__get_quote", "group": "*", "level": "anyone"},
		},
	}))
	if err != nil {
		t.Fatalf("handleToolPoliciesUpdate error: %v", err)
	}

	var policies []*models.ToolPolicyConfiguration
	if loadErr := dataStore.Transaction(connection.ctx, func(ctx context.Context, transaction store.Transaction) error {
		configuration, getErr := transaction.GetConfiguration(ctx, nil)
		if getErr != nil {
			return getErr
		}
		if configuration.ToolPolicies != nil {
			policies = *configuration.ToolPolicies
		}
		return nil
	}); loadErr != nil {
		t.Fatalf("loading configuration: %v", loadErr)
	}
	if len(policies) != 1 || policies[0].GetTool() != "mcp__robinhood__get_quote" || policies[0].GetLevel() != models.ToolPolicyAnyone {
		t.Errorf("persisted policies = %+v, want one mcp__robinhood__get_quote=anyone", policies)
	}
}

// TestToolPoliciesUpdateRejectsUnknownTool verifies that a non-MCP tool name
// that is neither builtin nor a skill tool is still rejected.
func TestToolPoliciesUpdateRejectsUnknownTool(t *testing.T) {
	connection, _ := newAdminToolPolicyConnection(t)

	if _, err := connection.handleToolPoliciesUpdate(frameWith(t, map[string]interface{}{
		"policies": []map[string]interface{}{
			{"tool": "definitely_not_a_real_tool", "group": "*", "level": "anyone"},
		},
	})); err == nil {
		t.Error("expected rejection for unknown non-MCP tool")
	}
}
