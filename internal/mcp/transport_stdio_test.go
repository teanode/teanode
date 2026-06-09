package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// stdioServerEnv, when set, makes the test binary act as a minimal stdio MCP
// server instead of running tests. Tests spawn the binary itself with this set,
// which keeps the stdio transport tests hermetic (no external server needed).
const stdioServerEnv = "TEANODE_MCP_STDIO_TEST_SERVER"

func TestMain(m *testing.M) {
	switch os.Getenv(stdioServerEnv) {
	case "echo":
		runStdioTestServer(false)
		return
	case "crash-before-response":
		// Exit immediately so the client observes the process leaving without a
		// reply, exercising the exitError path.
		os.Exit(1)
	case "no-tools":
		runStdioTestServer(true)
		return
	}
	os.Exit(m.Run())
}

// runStdioTestServer speaks just enough of the MCP stdio protocol to serve the
// transport tests: initialize, tools/list, and an echoing tools/call. When
// emptyTools is true it advertises no tools.
func runStdioTestServer(emptyTools bool) {
	reader := bufio.NewScanner(os.Stdin)
	reader.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	writer := bufio.NewWriter(os.Stdout)
	defer func() { _ = writer.Flush() }()

	// Emit a server-initiated notification first to prove the client skips
	// non-response frames before the matching response.
	_, _ = fmt.Fprintln(writer, `{"jsonrpc":"2.0","method":"notifications/message","params":{"level":"info"}}`)
	_ = writer.Flush()

	for reader.Scan() {
		line := strings.TrimSpace(reader.Text())
		if line == "" {
			continue
		}
		var message struct {
			ID         *int64          `json:"id"`
			Method     string          `json:"method"`
			Parameters json.RawMessage `json:"params"`
		}
		if json.Unmarshal([]byte(line), &message) != nil {
			continue
		}
		// Notifications carry no id and expect no reply.
		if message.ID == nil {
			continue
		}
		var result interface{}
		switch message.Method {
		case "initialize":
			result = map[string]interface{}{
				"protocolVersion": "2025-06-18",
				"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
				"serverInfo":      map[string]interface{}{"name": "stdio-test", "version": "1.0"},
			}
		case "tools/list":
			tools := []map[string]interface{}{}
			if !emptyTools {
				tools = append(tools, map[string]interface{}{
					"name":        "echo",
					"description": "Echo the arguments back",
					"inputSchema": map[string]interface{}{
						"type":       "object",
						"properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}},
					},
				})
			}
			result = map[string]interface{}{"tools": tools}
		case "tools/call":
			var parameters struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			_ = json.Unmarshal(message.Parameters, &parameters)
			result = map[string]interface{}{
				"content": []map[string]interface{}{{"type": "text", "text": "echo:" + parameters.Name + ":" + string(parameters.Arguments)}},
				"isError": false,
			}
		default:
			continue
		}
		response := map[string]interface{}{"jsonrpc": "2.0", "id": *message.ID, "result": result}
		payload, _ := json.Marshal(response)
		_, _ = writer.Write(payload)
		_, _ = writer.Write([]byte("\n"))
		_ = writer.Flush()
	}
}

// stdioServerConfiguration returns a ServerConfiguration that launches the test
// binary itself as a stdio MCP server in the given mode.
func stdioServerConfiguration(mode string) ServerConfiguration {
	return ServerConfiguration{
		Name:        "stdio-test",
		Transport:   TransportStdio,
		Command:     os.Args[0],
		Environment: []string{stdioServerEnv + "=" + mode},
		Timeout:     10 * time.Second,
	}
}

func TestStdioClientListTools(t *testing.T) {
	client := NewClient(stdioServerConfiguration("echo"))
	defer func() { _ = client.Close() }()

	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("expected one echo tool, got %+v", tools)
	}
}

func TestStdioClientCallTool(t *testing.T) {
	client := NewClient(stdioServerConfiguration("echo"))
	defer func() { _ = client.Close() }()

	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	result, err := client.CallTool(context.Background(), "echo", json.RawMessage(`{"value":"hi"}`))
	if err != nil {
		t.Fatalf("call tool: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Text())
	}
	if got := result.Text(); got != `echo:echo:{"value":"hi"}` {
		t.Fatalf("unexpected result %q", got)
	}
}

func TestStdioClientEmptyToolsConnects(t *testing.T) {
	client := NewClient(stdioServerConfiguration("no-tools"))
	defer func() { _ = client.Close() }()

	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(tools) != 0 {
		t.Fatalf("expected no tools, got %+v", tools)
	}
}

func TestStdioClientMissingCommand(t *testing.T) {
	client := NewClient(ServerConfiguration{
		Name:      "broken",
		Transport: TransportStdio,
		Command:   "teanode-nonexistent-binary-xyz",
		Timeout:   5 * time.Second,
	})
	defer func() { _ = client.Close() }()

	if err := client.Connect(context.Background()); err == nil {
		t.Fatalf("expected connect to fail for a missing command")
	}
}

func TestStdioClientProcessExitsBeforeResponse(t *testing.T) {
	client := NewClient(stdioServerConfiguration("crash-before-response"))
	defer func() { _ = client.Close() }()

	if err := client.Connect(context.Background()); err == nil {
		t.Fatalf("expected connect to fail when the server exits without replying")
	}
}

func TestStdioClientContextCancellation(t *testing.T) {
	client := NewClient(stdioServerConfiguration("echo"))
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := client.Connect(ctx); err == nil {
		t.Fatalf("expected a cancelled context to abort connect")
	}
}
