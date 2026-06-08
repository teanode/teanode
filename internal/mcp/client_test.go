package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func testServerConfiguration(url string) ServerConfiguration {
	return ServerConfiguration{Name: "test", URL: url, Timeout: 5 * time.Second}
}

func TestClientConnectAndListTools(t *testing.T) {
	server := newTestMCPServer(t)
	server.tools = sampleTools()

	client := NewClient(testServerConfiguration(server.url()))
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	// Connect is idempotent and must not re-initialize.
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("second Connect: %v", err)
	}
	if server.initializeCount != 1 {
		t.Fatalf("initializeCount = %d, want 1", server.initializeCount)
	}

	remoteTools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(remoteTools) != 2 {
		t.Fatalf("len(tools) = %d, want 2", len(remoteTools))
	}
	if remoteTools[0].Name != "get_quote" {
		t.Errorf("tools[0].Name = %q, want get_quote", remoteTools[0].Name)
	}
	if remoteTools[0].InputSchema == nil {
		t.Errorf("tools[0].InputSchema is nil, want populated schema")
	}
}

func TestClientListToolsPagination(t *testing.T) {
	server := newTestMCPServer(t)
	server.tools = sampleTools()
	server.paginate = true

	client := NewClient(testServerConfiguration(server.url()))
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	remoteTools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(remoteTools) != 2 {
		t.Fatalf("len(tools) = %d, want 2 across pages", len(remoteTools))
	}
	if server.listCount != 2 {
		t.Errorf("listCount = %d, want 2 (one per page)", server.listCount)
	}
}

func TestClientCallTool(t *testing.T) {
	server := newTestMCPServer(t)
	server.tools = sampleTools()

	client := NewClient(testServerConfiguration(server.url()))
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	result, err := client.CallTool(context.Background(), "get_quote", json.RawMessage(`{"symbol":"AAPL"}`))
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected isError")
	}
	want := `ok:get_quote:{"symbol":"AAPL"}`
	if got := result.Text(); got != want {
		t.Errorf("Text() = %q, want %q", got, want)
	}
}

func TestClientCallToolEmptyArgumentsDefaultsToObject(t *testing.T) {
	server := newTestMCPServer(t)
	client := NewClient(testServerConfiguration(server.url()))
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	result, err := client.CallTool(context.Background(), "ping", nil)
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if got := result.Text(); got != "ok:ping:{}" {
		t.Errorf("Text() = %q, want ok:ping:{}", got)
	}
}

func TestClientHandlesEventStreamResponses(t *testing.T) {
	server := newTestMCPServer(t)
	server.tools = sampleTools()
	server.useSSE = true

	client := NewClient(testServerConfiguration(server.url()))
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect over SSE: %v", err)
	}
	remoteTools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools over SSE: %v", err)
	}
	if len(remoteTools) != 2 {
		t.Fatalf("len(tools) = %d, want 2", len(remoteTools))
	}
}

func TestClientSendsAuthorizationHeader(t *testing.T) {
	server := newTestMCPServer(t)
	server.tools = sampleTools()
	server.requireAuth = "Bearer secret"

	configuration := testServerConfiguration(server.url())
	configuration.Authorization = "Bearer secret"
	client := NewClient(configuration)
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect with auth: %v", err)
	}

	// A client without the credential must be rejected.
	unauthenticated := NewClient(testServerConfiguration(server.url()))
	if err := unauthenticated.Connect(context.Background()); err == nil {
		t.Fatalf("expected unauthenticated Connect to fail")
	}
}

func TestClientReportsServerError(t *testing.T) {
	server := newTestMCPServer(t)
	server.callHandler = func(name string, arguments json.RawMessage) ([]map[string]interface{}, bool) {
		return []map[string]interface{}{{"type": "text", "text": "boom"}}, true
	}
	client := NewClient(testServerConfiguration(server.url()))
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	result, err := client.CallTool(context.Background(), "explode", nil)
	if err != nil {
		t.Fatalf("CallTool transport error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected isError result")
	}
	if result.Text() != "boom" {
		t.Errorf("Text() = %q, want boom", result.Text())
	}
}
