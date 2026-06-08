package fsstore_test

import (
	"context"
	"testing"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
)

// TestConfigurationMCPRoundTrip verifies that MCP server configuration survives
// a model -> store record -> YAML -> model round trip with all fields intact,
// including the tri-state Enabled pointer.
func TestConfigurationMCPRoundTrip(t *testing.T) {
	openedStore := openFileSystemStore(t)

	disabled := false
	transactionError := openedStore.Transaction(context.Background(), func(ctx context.Context, transaction store.Transaction) error {
		_, modifyError := transaction.ModifyConfiguration(ctx, func(configuration *models.Configuration) error {
			configuration.Tools = &models.ToolsConfiguration{
				MCP: &models.MCPConfiguration{
					Servers: &[]*models.MCPServerConfiguration{
						{
							Name:           ptrto.Value("robinhood"),
							URL:            ptrto.Value("https://example.com/mcp"),
							Authorization:  ptrto.Value("Bearer secret-token"),
							TimeoutSeconds: ptrto.Value(45),
						},
						{
							Name:    ptrto.Value("disabled-server"),
							URL:     ptrto.Value("https://disabled.example.com/mcp"),
							Enabled: &disabled,
						},
					},
				},
			}
			return nil
		}, nil)
		return modifyError
	})
	if transactionError != nil {
		t.Fatalf("ModifyConfiguration error: %v", transactionError)
	}

	var loaded *models.Configuration
	loadError := openedStore.Transaction(context.Background(), func(ctx context.Context, transaction store.Transaction) error {
		configuration, err := transaction.GetConfiguration(ctx, nil)
		if err != nil {
			return err
		}
		loaded = configuration
		return nil
	})
	if loadError != nil {
		t.Fatalf("GetConfiguration error: %v", loadError)
	}

	if loaded.Tools == nil || loaded.Tools.MCP == nil {
		t.Fatalf("MCP configuration was not persisted")
	}
	servers := loaded.Tools.MCP.GetServers()
	if len(servers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(servers))
	}

	first := servers[0]
	if first.GetName() != "robinhood" {
		t.Errorf("server[0] name = %q, want robinhood", first.GetName())
	}
	if first.GetURL() != "https://example.com/mcp" {
		t.Errorf("server[0] url = %q", first.GetURL())
	}
	if first.GetAuthorization() != "Bearer secret-token" {
		t.Errorf("server[0] authorization not preserved: %q", first.GetAuthorization())
	}
	if first.GetTimeoutSeconds() != 45 {
		t.Errorf("server[0] timeout = %d, want 45", first.GetTimeoutSeconds())
	}

	second := servers[1]
	if second.Enabled == nil || *second.Enabled {
		t.Errorf("server[1] Enabled should round-trip as false, got %v", second.Enabled)
	}
}
