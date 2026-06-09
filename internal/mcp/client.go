// Package mcp implements a minimal Model Context Protocol (MCP) client and
// adapts the tools that MCP servers expose into TeaNode tools.
//
// Scope and limitations (v1):
//
//   - Transport: the streamable HTTP transport (remote servers) and the stdio
//     transport (a local subprocess speaking newline-delimited JSON-RPC over its
//     stdin/stdout) are implemented. Each logical operation opens a session
//     (initialize + notifications/initialized), performs the request, and relies
//     on the server's response. The optional standalone GET event stream for
//     server-initiated messages is not used.
//   - Capabilities: only the tools capability is consumed. Prompts, resources,
//     sampling, roots, and completion are intentionally out of scope.
//   - Auth: HTTP servers support a single static Authorization header value (and,
//     for user-scoped servers, a per-user credential or OAuth token); stdio
//     servers run locally and use no HTTP auth.
//
// These boundaries are deliberate so the client stays small and reviewable
// while leaving clean seams for richer transport/auth support later.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/op/go-logging"
	"github.com/teanode/teanode/internal/version"
)

var log = logging.MustGetLogger("mcp")

// protocolVersion is the MCP protocol revision this client advertises during
// initialization. Servers negotiate down to a version they support; the
// negotiated value is echoed back on subsequent requests.
const protocolVersion = "2025-06-18"

// defaultTimeout bounds a single operation when a server does not configure its
// own TimeoutSeconds.
const defaultTimeout = 30 * time.Second

// ServerConfiguration is the resolved, non-pointer configuration for a single
// MCP server. It is derived from models.MCPServerConfiguration.
type ServerConfiguration struct {
	Name string
	// Transport selects the wire transport. The empty value is treated as
	// TransportHTTP so existing HTTP callers need not set it.
	Transport TransportType
	// URL is the streamable HTTP endpoint (TransportHTTP).
	URL string
	// Authorization is the verbatim HTTP Authorization header value (TransportHTTP).
	Authorization string
	// Command, Arguments, Environment, and WorkingDir configure the subprocess
	// (TransportStdio). Environment entries are "KEY=VALUE" pairs merged over
	// TeaNode's own environment.
	Command     string
	Arguments   []string
	Environment []string
	WorkingDir  string
	Timeout     time.Duration
	// ConnectionID, when set, is the per-user models.MCPConnection backing this
	// server. It lets discovery outcomes be reflected back onto the user's
	// connection status. It is empty for shared servers and is deliberately
	// excluded from the discovery cache signature.
	ConnectionID string
}

// RemoteTool is a tool advertised by an MCP server via tools/list.
type RemoteTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

// CallResult is the outcome of a tools/call request.
type CallResult struct {
	Content []contentBlock `json:"content"`
	IsError bool           `json:"isError"`
}

// contentBlock is a single block of tool result content. Only text blocks are
// rendered verbatim; other block types are summarized so the model still sees
// that content was returned.
type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Text flattens the result content blocks into a single string suitable for a
// tool message. Non-text blocks are rendered as a short placeholder.
func (self *CallResult) Text() string {
	parts := make([]string, 0, len(self.Content))
	for _, block := range self.Content {
		if block.Type == "text" {
			parts = append(parts, block.Text)
			continue
		}
		parts = append(parts, fmt.Sprintf("[%s content omitted]", block.Type))
	}
	return strings.Join(parts, "\n")
}

// Client is a session-scoped connection to one MCP server over a transport. A
// Client is not safe for concurrent use; create one per logical operation and
// Close it when done.
type Client struct {
	server      ServerConfiguration
	transport   transport
	nextId      atomic.Int64
	initialized bool
}

// NewClient creates a client for the given server. It does not perform any I/O
// (no network connection, no subprocess launch) until Connect is called.
func NewClient(server ServerConfiguration) *Client {
	return &Client{
		server:    server,
		transport: newTransport(server),
	}
}

// Close releases the client's transport resources (terminating the subprocess
// for a stdio server). It is safe to call more than once and on a client that
// was never connected.
func (self *Client) Close() error {
	if self.transport == nil {
		return nil
	}
	return self.transport.close()
}

// jsonrpcRequest is an outbound JSON-RPC 2.0 request.
type jsonrpcRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int64       `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// jsonrpcNotification is an outbound JSON-RPC 2.0 notification (no id, no reply).
type jsonrpcNotification struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// jsonrpcResponse is an inbound JSON-RPC 2.0 response.
type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
}

type jsonrpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (self *jsonrpcError) Error() string {
	return fmt.Sprintf("mcp: server error %d: %s", self.Code, self.Message)
}

// Connect performs the MCP initialization handshake: it sends the initialize
// request, records the negotiated protocol version, and sends
// notifications/initialized. It is idempotent.
func (self *Client) Connect(ctx context.Context) error {
	if self.initialized {
		return nil
	}
	parameters := map[string]interface{}{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]interface{}{},
		"clientInfo": map[string]interface{}{
			"name":    "teanode",
			"version": version.Version(),
		},
	}
	result, err := self.call(ctx, "initialize", parameters)
	if err != nil {
		return fmt.Errorf("mcp: initialize %q: %w", self.server.Name, err)
	}
	var initializeResult struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if unmarshalError := json.Unmarshal(result, &initializeResult); unmarshalError == nil && initializeResult.ProtocolVersion != "" {
		self.transport.observeProtocolVersion(initializeResult.ProtocolVersion)
	}
	// Per the spec the client confirms readiness before issuing other requests.
	if notifyError := self.notify(ctx, "notifications/initialized", nil); notifyError != nil {
		return fmt.Errorf("mcp: initialized %q: %w", self.server.Name, notifyError)
	}
	self.initialized = true
	return nil
}

// ListTools returns every tool advertised by the server, following pagination
// cursors until the list is exhausted.
func (self *Client) ListTools(ctx context.Context) ([]RemoteTool, error) {
	var allTools []RemoteTool
	var cursor string
	for {
		parameters := map[string]interface{}{}
		if cursor != "" {
			parameters["cursor"] = cursor
		}
		result, err := self.call(ctx, "tools/list", parameters)
		if err != nil {
			return nil, fmt.Errorf("mcp: tools/list %q: %w", self.server.Name, err)
		}
		var page struct {
			Tools      []RemoteTool `json:"tools"`
			NextCursor string       `json:"nextCursor"`
		}
		if unmarshalError := json.Unmarshal(result, &page); unmarshalError != nil {
			return nil, fmt.Errorf("mcp: tools/list %q: decoding result: %w", self.server.Name, unmarshalError)
		}
		allTools = append(allTools, page.Tools...)
		if page.NextCursor == "" {
			return allTools, nil
		}
		cursor = page.NextCursor
	}
}

// CallTool invokes a tool by name with the given JSON arguments.
func (self *Client) CallTool(ctx context.Context, name string, arguments json.RawMessage) (*CallResult, error) {
	if len(arguments) == 0 {
		arguments = json.RawMessage("{}")
	}
	parameters := map[string]interface{}{
		"name":      name,
		"arguments": arguments,
	}
	result, err := self.call(ctx, "tools/call", parameters)
	if err != nil {
		return nil, fmt.Errorf("mcp: tools/call %q/%q: %w", self.server.Name, name, err)
	}
	callResult := &CallResult{}
	if unmarshalError := json.Unmarshal(result, callResult); unmarshalError != nil {
		return nil, fmt.Errorf("mcp: tools/call %q/%q: decoding result: %w", self.server.Name, name, unmarshalError)
	}
	return callResult, nil
}

// call sends a JSON-RPC request over the transport and returns the raw result
// payload.
func (self *Client) call(ctx context.Context, method string, parameters interface{}) (json.RawMessage, error) {
	id := self.nextId.Add(1)
	requestBody, marshalError := json.Marshal(jsonrpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  parameters,
	})
	if marshalError != nil {
		return nil, fmt.Errorf("mcp: marshaling request: %w", marshalError)
	}
	response, err := self.transport.roundTrip(ctx, requestBody)
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, response.Error
	}
	return response.Result, nil
}

// notify sends a JSON-RPC notification over the transport.
func (self *Client) notify(ctx context.Context, method string, parameters interface{}) error {
	requestBody, marshalError := json.Marshal(jsonrpcNotification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  parameters,
	})
	if marshalError != nil {
		return fmt.Errorf("mcp: marshaling notification: %w", marshalError)
	}
	return self.transport.notify(ctx, requestBody)
}
