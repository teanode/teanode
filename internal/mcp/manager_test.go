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

// newTestManager returns a Manager whose retry backoff does not actually sleep,
// so retry tests run instantly.
func newTestManager() *Manager {
	manager := NewManager()
	manager.sleep = func(ctx context.Context, _ time.Duration) bool { return ctx.Err() == nil }
	return manager
}

func TestManagerRetriesTransientDiscoveryFailure(t *testing.T) {
	server := newTestMCPServer(t)
	server.tools = sampleTools()
	server.failTimes = 2 // first two requests return 503, then succeed

	manager := newTestManager()
	registry := tools.NewEmptyToolRegistry()
	outcomes := manager.RegisterTools(context.Background(), registry, []ServerConfiguration{
		{Name: "flaky", URL: server.url(), Timeout: 2 * time.Second},
	})

	if registry.Get("mcp__flaky__get_quote") == nil {
		t.Errorf("expected tools to register after transient failures recovered")
	}
	if server.failuresServed != 2 {
		t.Errorf("failuresServed = %d, want 2", server.failuresServed)
	}
	if len(outcomes) != 1 || outcomes[0].Err != nil {
		t.Errorf("outcomes = %+v, want one successful outcome", outcomes)
	}
	if outcomes[0].Cached {
		t.Errorf("first discovery should not be reported as cached")
	}
}

func TestManagerGivesUpAfterMaxAttempts(t *testing.T) {
	server := newTestMCPServer(t)
	server.tools = sampleTools()
	server.failTimes = 100 // always fail

	manager := newTestManager()
	registry := tools.NewEmptyToolRegistry()
	outcomes := manager.RegisterTools(context.Background(), registry, []ServerConfiguration{
		{Name: "down", URL: server.url(), Timeout: 2 * time.Second},
	})

	if len(registry.Names()) != 0 {
		t.Errorf("expected no tools registered, got %v", registry.Names())
	}
	if server.requestCount != manager.attempts {
		t.Errorf("requestCount = %d, want %d (one per attempt)", server.requestCount, manager.attempts)
	}
	if len(outcomes) != 1 || outcomes[0].Err == nil {
		t.Errorf("expected one failed outcome, got %+v", outcomes)
	}
}

func TestManagerDoesNotRetryAuthFailure(t *testing.T) {
	server := newTestMCPServer(t)
	server.tools = sampleTools()
	server.requireAuth = "Bearer good"

	manager := newTestManager()
	registry := tools.NewEmptyToolRegistry()
	// Connect with the wrong credential: the server replies 401, which must not
	// be retried because the credential will not change between attempts.
	outcomes := manager.RegisterTools(context.Background(), registry, []ServerConfiguration{
		{Name: "secure", URL: server.url(), Authorization: "Bearer wrong", Timeout: 2 * time.Second},
	})

	if len(registry.Names()) != 0 {
		t.Errorf("expected no tools registered on auth failure, got %v", registry.Names())
	}
	if server.requestCount != 1 {
		t.Errorf("requestCount = %d, want 1 (auth failure must not retry)", server.requestCount)
	}
	if len(outcomes) != 1 || outcomes[0].Err == nil {
		t.Errorf("expected one failed outcome, got %+v", outcomes)
	}
}

func TestManagerReportsCachedOutcome(t *testing.T) {
	server := newTestMCPServer(t)
	server.tools = sampleTools()

	manager := newTestManager()
	serverConfiguration := ServerConfiguration{Name: "robinhood", URL: server.url(), Timeout: 2 * time.Second}

	first := manager.RegisterTools(context.Background(), tools.NewEmptyToolRegistry(), []ServerConfiguration{serverConfiguration})
	if first[0].Cached || first[0].ToolCount != 2 {
		t.Errorf("first outcome = %+v, want fresh with 2 tools", first[0])
	}

	second := manager.RegisterTools(context.Background(), tools.NewEmptyToolRegistry(), []ServerConfiguration{serverConfiguration})
	if !second[0].Cached {
		t.Errorf("second outcome should be served from cache, got %+v", second[0])
	}
	if second[0].ToolCount != 2 {
		t.Errorf("cached outcome ToolCount = %d, want 2", second[0].ToolCount)
	}
}

func TestIsRetryableError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"context canceled", context.Canceled, false},
		{"deadline", context.DeadlineExceeded, false},
		{"503", &httpStatusError{StatusCode: 503}, true},
		{"429", &httpStatusError{StatusCode: 429}, true},
		{"500", &httpStatusError{StatusCode: 500}, true},
		{"401", &httpStatusError{StatusCode: 401}, false},
		{"403", &httpStatusError{StatusCode: 403}, false},
		{"404", &httpStatusError{StatusCode: 404}, false},
		{"jsonrpc error", &jsonrpcError{Code: -32000, Message: "boom"}, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := isRetryableError(testCase.err); got != testCase.want {
				t.Errorf("isRetryableError(%v) = %v, want %v", testCase.err, got, testCase.want)
			}
		})
	}
}

func TestManagerSkipsUnreachableServer(t *testing.T) {
	manager := newTestManager()
	registry := tools.NewEmptyToolRegistry()
	// A registry with one local tool name to confirm it survives a bad server.
	manager.RegisterTools(context.Background(), registry, []ServerConfiguration{
		{Name: "broken", URL: "http://127.0.0.1:1/mcp", Timeout: 500 * time.Millisecond},
	})
	if names := registry.Names(); len(names) != 0 {
		t.Errorf("expected no tools registered from unreachable server, got %v", names)
	}
}
