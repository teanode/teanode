// Package mcp implements a minimal Model Context Protocol (MCP) client for
// remote servers and adapts the tools they expose into TeaNode tools.
//
// Scope and limitations (v1):
//
//   - Transport: only the streamable HTTP transport is implemented. Each
//     logical operation opens a session (initialize + notifications/initialized),
//     performs the request, and relies on the server's response. The optional
//     standalone GET event stream for server-initiated messages is not used.
//   - Capabilities: only the tools capability is consumed. Prompts, resources,
//     sampling, roots, and completion are intentionally out of scope.
//   - Auth: a single static Authorization header value is supported. OAuth and
//     other interactive auth flows are not implemented.
//
// These boundaries are deliberate so the client stays small and reviewable
// while leaving clean seams for richer transport/auth support later.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

// defaultTimeout bounds a single HTTP request when a server does not configure
// its own TimeoutSeconds.
const defaultTimeout = 30 * time.Second

// ServerConfiguration is the resolved, non-pointer configuration for a single
// remote MCP server. It is derived from models.MCPServerConfiguration.
type ServerConfiguration struct {
	Name          string
	URL           string
	Authorization string
	Timeout       time.Duration
}

// RemoteTool is a tool advertised by a remote MCP server via tools/list.
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

// Client is a session-scoped connection to one remote MCP server. A Client is
// not safe for concurrent use; create one per logical operation.
type Client struct {
	server            ServerConfiguration
	httpClient        *http.Client
	sessionId         string
	negotiatedVersion string
	nextId            atomic.Int64
	initialized       bool
}

// NewClient creates a client for the given server. It does not perform any
// network I/O until Connect is called.
func NewClient(server ServerConfiguration) *Client {
	timeout := server.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Client{
		server:     server,
		httpClient: &http.Client{Timeout: timeout},
	}
}

// jsonrpcRequest is an outbound JSON-RPC 2.0 request.
type jsonrpcRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int64       `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"parameters,omitempty"`
}

// jsonrpcNotification is an outbound JSON-RPC 2.0 notification (no id, no reply).
type jsonrpcNotification struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"parameters,omitempty"`
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
// request, captures any session id, and sends notifications/initialized. It is
// idempotent.
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
	if unmarshalError := json.Unmarshal(result, &initializeResult); unmarshalError == nil {
		self.negotiatedVersion = initializeResult.ProtocolVersion
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

// CallTool invokes a remote tool by name with the given JSON arguments.
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

// call sends a JSON-RPC request and returns the raw result payload.
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
	response, err := self.post(ctx, requestBody)
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, response.Error
	}
	return response.Result, nil
}

// notify sends a JSON-RPC notification. The server is expected to acknowledge
// with 202 Accepted and no body.
func (self *Client) notify(ctx context.Context, method string, parameters interface{}) error {
	requestBody, marshalError := json.Marshal(jsonrpcNotification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  parameters,
	})
	if marshalError != nil {
		return fmt.Errorf("mcp: marshaling notification: %w", marshalError)
	}
	httpResponse, err := self.send(ctx, requestBody)
	if err != nil {
		return err
	}
	defer func() { _ = httpResponse.Body.Close() }()
	if httpResponse.StatusCode >= 400 {
		return fmt.Errorf("mcp: notification %q: unexpected status %d", method, httpResponse.StatusCode)
	}
	_, _ = io.Copy(io.Discard, httpResponse.Body)
	return nil
}

// post sends a request body and decodes the single matching JSON-RPC response,
// transparently handling both application/json and text/event-stream replies.
func (self *Client) post(ctx context.Context, body []byte) (*jsonrpcResponse, error) {
	httpResponse, err := self.send(ctx, body)
	if err != nil {
		return nil, err
	}
	defer func() { _ = httpResponse.Body.Close() }()

	// Capture a session id assigned by the server (typically on initialize).
	if sessionId := httpResponse.Header.Get("Mcp-Session-Id"); sessionId != "" {
		self.sessionId = sessionId
	}

	if httpResponse.StatusCode >= 400 {
		snippet, _ := io.ReadAll(io.LimitReader(httpResponse.Body, 2048))
		return nil, fmt.Errorf("mcp: unexpected status %d: %s", httpResponse.StatusCode, strings.TrimSpace(string(snippet)))
	}

	contentType := httpResponse.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "text/event-stream") {
		return self.readEventStream(httpResponse.Body)
	}
	return decodeJsonResponse(httpResponse.Body)
}

// send issues the HTTP POST with the headers required by the streamable HTTP
// transport. The caller is responsible for closing the response body.
func (self *Client) send(ctx context.Context, body []byte) (*http.Response, error) {
	httpRequest, requestError := http.NewRequestWithContext(ctx, http.MethodPost, self.server.URL, bytes.NewReader(body))
	if requestError != nil {
		return nil, fmt.Errorf("mcp: building request: %w", requestError)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json, text/event-stream")
	if self.server.Authorization != "" {
		httpRequest.Header.Set("Authorization", self.server.Authorization)
	}
	if self.sessionId != "" {
		httpRequest.Header.Set("Mcp-Session-Id", self.sessionId)
	}
	negotiated := self.negotiatedVersion
	if negotiated == "" {
		negotiated = protocolVersion
	}
	httpRequest.Header.Set("MCP-Protocol-Version", negotiated)
	return self.httpClient.Do(httpRequest)
}

// decodeJsonResponse decodes a single JSON-RPC response document.
func decodeJsonResponse(reader io.Reader) (*jsonrpcResponse, error) {
	response := &jsonrpcResponse{}
	if decodeError := json.NewDecoder(reader).Decode(response); decodeError != nil {
		return nil, fmt.Errorf("mcp: decoding response: %w", decodeError)
	}
	return response, nil
}

// readEventStream parses a text/event-stream body and returns the first
// JSON-RPC response message it contains. Server-initiated requests and
// notifications interleaved on the stream are ignored: TeaNode does not expose
// sampling or roots, so it has nothing to answer them with. This is a
// deliberate v1 simplification (TODO: handle server-initiated messages).
func (self *Client) readEventStream(reader io.Reader) (*jsonrpcResponse, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var dataLines []string
	flush := func() (*jsonrpcResponse, bool) {
		if len(dataLines) == 0 {
			return nil, false
		}
		payload := strings.Join(dataLines, "\n")
		dataLines = dataLines[:0]
		response := &jsonrpcResponse{}
		if json.Unmarshal([]byte(payload), response) != nil {
			return nil, false
		}
		// Only messages carrying a result or error are responses to our request.
		if response.Result == nil && response.Error == nil {
			return nil, false
		}
		return response, true
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if response, ok := flush(); ok {
				return response, nil
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue // SSE comment
		}
		if value, found := strings.CutPrefix(line, "data:"); found {
			dataLines = append(dataLines, strings.TrimPrefix(value, " "))
		}
		// Other SSE fields (event:, id:, retry:) are not needed here.
	}
	if scanError := scanner.Err(); scanError != nil {
		return nil, fmt.Errorf("mcp: reading event stream: %w", scanError)
	}
	if response, ok := flush(); ok {
		return response, nil
	}
	return nil, fmt.Errorf("mcp: event stream closed without a response")
}
