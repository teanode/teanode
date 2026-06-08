package runners

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/store/fsstore"
	"github.com/teanode/teanode/internal/util/ptrto"
)

// startInlineMCPServer starts a minimal MCP streamable HTTP server that
// advertises a single tool. It is intentionally tiny (JSON responses only) so
// the runner integration test stays self-contained.
func startInlineMCPServer(t *testing.T) string {
	t.Helper()
	handler := func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		var message struct {
			ID     *int64 `json:"id"`
			Method string `json:"method"`
		}
		_ = json.Unmarshal(body, &message)
		if message.ID == nil {
			writer.WriteHeader(http.StatusAccepted)
			return
		}
		var result interface{}
		switch message.Method {
		case "initialize":
			writer.Header().Set("Mcp-Session-Id", "s1")
			result = map[string]interface{}{"protocolVersion": "2025-06-18", "capabilities": map[string]interface{}{"tools": map[string]interface{}{}}}
		case "tools/list":
			result = map[string]interface{}{"tools": []map[string]interface{}{
				{"name": "echo", "description": "echo back"},
			}}
		default:
			http.Error(writer, "unknown method", http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		payload, _ := json.Marshal(map[string]interface{}{"jsonrpc": "2.0", "id": *message.ID, "result": result})
		_, _ = writer.Write(payload)
	}
	server := httptest.NewServer(http.HandlerFunc(handler))
	t.Cleanup(server.Close)
	return server.URL
}

// TestNewRunnerRegistersMCPTools verifies that NewRunner discovers configured
// remote MCP servers and registers their tools (namespaced) alongside builtin
// tools, without disturbing the builtin set.
func TestNewRunnerRegistersMCPTools(t *testing.T) {
	serverURL := startInlineMCPServer(t)

	openedStore, openError := fsstore.Open(fsstore.Options{DataDirectory: t.TempDir()})
	if openError != nil {
		t.Fatalf("opening store: %v", openError)
	}
	t.Cleanup(func() { _ = openedStore.Close() })
	if migrateError := openedStore.Migrate(context.Background()); migrateError != nil {
		t.Fatalf("migrating store: %v", migrateError)
	}

	if transactionError := openedStore.Transaction(context.Background(), func(ctx context.Context, transaction store.Transaction) error {
		_, modifyError := transaction.ModifyConfiguration(ctx, func(configuration *models.Configuration) error {
			configuration.Tools = &models.ToolsConfiguration{
				MCP: &models.MCPConfiguration{
					Servers: &[]*models.MCPServerConfiguration{
						{Name: ptrto.Value("inline"), URL: ptrto.Value(serverURL)},
					},
				},
			}
			return nil
		}, nil)
		return modifyError
	}); transactionError != nil {
		t.Fatalf("seeding MCP configuration: %v", transactionError)
	}

	ctx := store.ContextWithStore(context.Background(), openedStore)

	// With no agent allow-list, the discovered MCP tool is available.
	runner := NewRunner(ctx, "agent-1", "conversation-1", providers.NewProviderRegistry(nil), models.Agent{ID: "agent-1"})
	if runner.toolRegistry.Get("mcp__inline__echo") == nil {
		t.Errorf("expected mcp__inline__echo to be registered; names = %v", runner.toolRegistry.Names())
	}

	// MCP tools are registered before the agent allow-list is applied, so an
	// allow-list that omits them filters them out like any other tool.
	filteredRunner := NewRunner(ctx, "agent-1", "conversation-1", providers.NewProviderRegistry(nil), models.Agent{
		ID:    "agent-1",
		Tools: ptrto.Value([]string{"datetime"}),
	})
	if filteredRunner.toolRegistry.Get("mcp__inline__echo") != nil {
		t.Errorf("MCP tool should be filtered out by the agent allow-list")
	}
}
