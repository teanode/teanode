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
)

// httpTransport speaks the MCP streamable HTTP transport. Each request is a POST
// carrying a single JSON-RPC message; replies arrive either as application/json
// or as a text/event-stream the transport flattens to the matching response.
type httpTransport struct {
	server            ServerConfiguration
	httpClient        *http.Client
	sessionId         string
	negotiatedVersion string
}

// newHttpTransport creates an HTTP transport for the given server.
func newHttpTransport(server ServerConfiguration) *httpTransport {
	timeout := server.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &httpTransport{
		server:     server,
		httpClient: &http.Client{Timeout: timeout},
	}
}

func (self *httpTransport) observeProtocolVersion(version string) {
	self.negotiatedVersion = version
}

// close is a no-op for HTTP; there is no persistent resource to release.
func (self *httpTransport) close() error { return nil }

// httpStatusError is returned when a server responds with a non-2xx HTTP status.
// It carries the status code so callers (notably discovery retry) can decide
// whether the failure is worth retrying.
type httpStatusError struct {
	StatusCode int
	Body       string
}

func (self *httpStatusError) Error() string {
	if self.Body == "" {
		return fmt.Sprintf("mcp: unexpected status %d", self.StatusCode)
	}
	return fmt.Sprintf("mcp: unexpected status %d: %s", self.StatusCode, self.Body)
}

// roundTrip sends a request body and decodes the single matching JSON-RPC
// response, transparently handling both application/json and text/event-stream
// replies.
func (self *httpTransport) roundTrip(ctx context.Context, payload []byte) (*jsonrpcResponse, error) {
	httpResponse, err := self.send(ctx, payload)
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
		return nil, &httpStatusError{StatusCode: httpResponse.StatusCode, Body: strings.TrimSpace(string(snippet))}
	}

	contentType := httpResponse.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "text/event-stream") {
		return self.readEventStream(httpResponse.Body)
	}
	return decodeJsonResponse(httpResponse.Body)
}

// notify sends a notification payload. The server is expected to acknowledge
// with 202 Accepted and no body.
func (self *httpTransport) notify(ctx context.Context, payload []byte) error {
	httpResponse, err := self.send(ctx, payload)
	if err != nil {
		return err
	}
	defer func() { _ = httpResponse.Body.Close() }()
	if httpResponse.StatusCode >= 400 {
		return fmt.Errorf("mcp: notification: unexpected status %d", httpResponse.StatusCode)
	}
	_, _ = io.Copy(io.Discard, httpResponse.Body)
	return nil
}

// send issues the HTTP POST with the headers required by the streamable HTTP
// transport. The caller is responsible for closing the response body.
func (self *httpTransport) send(ctx context.Context, body []byte) (*http.Response, error) {
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
func (self *httpTransport) readEventStream(reader io.Reader) (*jsonrpcResponse, error) {
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
