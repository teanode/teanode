package mcp

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// testMCPServer is a minimal in-memory MCP server speaking the streamable HTTP
// transport, used to exercise the client without a real network dependency.
type testMCPServer struct {
	server *httptest.Server

	mutex           sync.Mutex
	initializeCount int
	listCount       int
	callCount       int

	// configuration knobs
	tools       []map[string]interface{}
	useSSE      bool
	sessionID   string
	requireAuth string // when set, requests must carry this Authorization value
	paginate    bool   // when true, tools/list returns one tool per page
	callHandler func(name string, arguments json.RawMessage) (content []map[string]interface{}, isError bool)
}

func newTestMCPServer(t *testing.T) *testMCPServer {
	t.Helper()
	mcpServer := &testMCPServer{
		sessionID: "session-123",
		callHandler: func(name string, arguments json.RawMessage) ([]map[string]interface{}, bool) {
			return []map[string]interface{}{{"type": "text", "text": "ok:" + name + ":" + string(arguments)}}, false
		},
	}
	mcpServer.server = httptest.NewServer(http.HandlerFunc(mcpServer.handle))
	t.Cleanup(mcpServer.server.Close)
	return mcpServer
}

func (self *testMCPServer) url() string { return self.server.URL }

func (self *testMCPServer) handle(writer http.ResponseWriter, request *http.Request) {
	if self.requireAuth != "" && request.Header.Get("Authorization") != self.requireAuth {
		http.Error(writer, "unauthorized", http.StatusUnauthorized)
		return
	}

	body, _ := io.ReadAll(request.Body)
	var message struct {
		ID         *int64          `json:"id"`
		Method     string          `json:"method"`
		Parameters json.RawMessage `json:"parameters"`
	}
	if unmarshalError := json.Unmarshal(body, &message); unmarshalError != nil {
		http.Error(writer, "bad request", http.StatusBadRequest)
		return
	}

	// Notifications carry no id and expect a bare 202.
	if message.ID == nil {
		writer.WriteHeader(http.StatusAccepted)
		return
	}

	// Every non-initialize request must echo the session id assigned earlier.
	if message.Method != "initialize" && request.Header.Get("Mcp-Session-Id") != self.sessionID {
		http.Error(writer, "missing session id", http.StatusBadRequest)
		return
	}

	var result interface{}
	switch message.Method {
	case "initialize":
		self.mutex.Lock()
		self.initializeCount++
		self.mutex.Unlock()
		writer.Header().Set("Mcp-Session-Id", self.sessionID)
		result = map[string]interface{}{
			"protocolVersion": "2025-06-18",
			"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
			"serverInfo":      map[string]interface{}{"name": "test", "version": "1.0"},
		}
	case "tools/list":
		self.mutex.Lock()
		self.listCount++
		self.mutex.Unlock()
		result = self.toolsListResult(message.Parameters)
	case "tools/call":
		self.mutex.Lock()
		self.callCount++
		self.mutex.Unlock()
		var parameters struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		_ = json.Unmarshal(message.Parameters, &parameters)
		content, isError := self.callHandler(parameters.Name, parameters.Arguments)
		result = map[string]interface{}{"content": content, "isError": isError}
	default:
		http.Error(writer, "unknown method", http.StatusBadRequest)
		return
	}

	self.writeResult(writer, *message.ID, result)
}

func (self *testMCPServer) toolsListResult(parameters json.RawMessage) map[string]interface{} {
	if !self.paginate {
		return map[string]interface{}{"tools": self.tools}
	}
	var parsed struct {
		Cursor string `json:"cursor"`
	}
	_ = json.Unmarshal(parameters, &parsed)
	if parsed.Cursor == "" {
		// First page: one tool plus a cursor for the rest.
		return map[string]interface{}{"tools": self.tools[:1], "nextCursor": "page-2"}
	}
	return map[string]interface{}{"tools": self.tools[1:]}
}

func (self *testMCPServer) writeResult(writer http.ResponseWriter, id int64, result interface{}) {
	response := map[string]interface{}{"jsonrpc": "2.0", "id": id, "result": result}
	payload, _ := json.Marshal(response)
	if self.useSSE {
		writer.Header().Set("Content-Type", "text/event-stream")
		writer.WriteHeader(http.StatusOK)
		// Include a comment and an unrelated notification to prove the parser
		// skips non-response frames before the actual response.
		_, _ = writer.Write([]byte(": keep-alive\n\n"))
		_, _ = writer.Write([]byte("event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\"}\n\n"))
		_, _ = writer.Write([]byte("event: message\ndata: "))
		_, _ = writer.Write(payload)
		_, _ = writer.Write([]byte("\n\n"))
		if flusher, ok := writer.(http.Flusher); ok {
			flusher.Flush()
		}
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(payload)
}

// sampleTools returns two tools for discovery tests.
func sampleTools() []map[string]interface{} {
	return []map[string]interface{}{
		{
			"name":        "get_quote",
			"description": "Get a stock quote",
			"inputSchema": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{"symbol": map[string]interface{}{"type": "string"}},
				"required":   []interface{}{"symbol"},
			},
		},
		{
			"name":        "list_positions",
			"description": "List positions",
		},
	}
}
