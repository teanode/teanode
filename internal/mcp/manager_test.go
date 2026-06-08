package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/tools"
	"github.com/teanode/teanode/internal/util/ptrto"
)

func TestServersFromConfiguration(t *testing.T) {
	disabled := false
	enabled := true
	configuration := &models.Configuration{
		Tools: &models.ToolsConfiguration{
			MCP: &models.MCPConfiguration{
				Servers: &[]*models.MCPServerConfiguration{
					{Name: ptrto.Value("alpha"), URL: ptrto.Value("https://a.example/mcp"), TimeoutSeconds: ptrto.Value(12)},
					{Name: ptrto.Value("beta"), URL: ptrto.Value("https://b.example/mcp"), Enabled: &enabled},
					{Name: ptrto.Value("off"), URL: ptrto.Value("https://c.example/mcp"), Enabled: &disabled},
					{Name: ptrto.Value("alpha"), URL: ptrto.Value("https://dup.example/mcp")}, // duplicate name
					{Name: ptrto.Value(""), URL: ptrto.Value("https://noname.example/mcp")},   // missing name
					{Name: ptrto.Value("nourl")}, // missing url
				},
			},
		},
	}

	servers := ServersFromConfiguration(configuration)
	if len(servers) != 2 {
		t.Fatalf("len(servers) = %d, want 2 (alpha, beta)", len(servers))
	}
	if servers[0].Name != "alpha" || servers[0].URL != "https://a.example/mcp" {
		t.Errorf("servers[0] = %+v", servers[0])
	}
	if servers[0].Timeout != 12*time.Second {
		t.Errorf("servers[0].Timeout = %v, want 12s", servers[0].Timeout)
	}
	if servers[1].Name != "beta" {
		t.Errorf("servers[1].Name = %q, want beta", servers[1].Name)
	}
	// beta has no explicit timeout: it should fall back to the default.
	if servers[1].Timeout != defaultTimeout {
		t.Errorf("servers[1].Timeout = %v, want default %v", servers[1].Timeout, defaultTimeout)
	}
}

func TestServersFromConfigurationEmpty(t *testing.T) {
	if servers := ServersFromConfiguration(nil); servers != nil {
		t.Errorf("nil configuration should yield no servers, got %v", servers)
	}
	if servers := ServersFromConfiguration(&models.Configuration{}); servers != nil {
		t.Errorf("empty configuration should yield no servers, got %v", servers)
	}
}

func TestManagerRegisterToolsNamespacesIntoRegistry(t *testing.T) {
	server := newTestMCPServer(t)
	server.tools = sampleTools()

	manager := NewManager()
	registry := tools.NewEmptyToolRegistry()
	manager.RegisterTools(context.Background(), registry, []ServerConfiguration{
		{Name: "robinhood", URL: server.url(), Timeout: 5 * time.Second},
	})

	if registry.Get("mcp__robinhood__get_quote") == nil {
		t.Errorf("expected mcp__robinhood__get_quote to be registered")
	}
	if registry.Get("mcp__robinhood__list_positions") == nil {
		t.Errorf("expected mcp__robinhood__list_positions to be registered")
	}
}

func TestManagerCachesDiscovery(t *testing.T) {
	server := newTestMCPServer(t)
	server.tools = sampleTools()

	manager := NewManager()
	serverConfiguration := ServerConfiguration{Name: "robinhood", URL: server.url(), Timeout: 5 * time.Second}

	manager.RegisterTools(context.Background(), tools.NewEmptyToolRegistry(), []ServerConfiguration{serverConfiguration})
	manager.RegisterTools(context.Background(), tools.NewEmptyToolRegistry(), []ServerConfiguration{serverConfiguration})

	if server.listCount != 1 {
		t.Errorf("listCount = %d, want 1 (second discovery should hit the cache)", server.listCount)
	}

	// Expiring the cache forces a fresh discovery.
	manager.now = func() time.Time { return time.Now().Add(2 * discoveryTtl) }
	manager.RegisterTools(context.Background(), tools.NewEmptyToolRegistry(), []ServerConfiguration{serverConfiguration})
	if server.listCount != 2 {
		t.Errorf("listCount = %d, want 2 after cache expiry", server.listCount)
	}
}

func TestManagerSkipsUnreachableServer(t *testing.T) {
	manager := NewManager()
	registry := tools.NewEmptyToolRegistry()
	// A registry with one local tool name to confirm it survives a bad server.
	manager.RegisterTools(context.Background(), registry, []ServerConfiguration{
		{Name: "broken", URL: "http://127.0.0.1:1/mcp", Timeout: 500 * time.Millisecond},
	})
	if names := registry.Names(); len(names) != 0 {
		t.Errorf("expected no tools registered from unreachable server, got %v", names)
	}
}
