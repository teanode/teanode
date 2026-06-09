package mcp

import "context"

// TransportType selects how a Client reaches its MCP server.
type TransportType string

const (
	// TransportHTTP is the streamable HTTP transport (the default).
	TransportHTTP TransportType = "http"
	// TransportStdio is the local stdio transport: a subprocess speaking
	// newline-delimited JSON-RPC over its stdin/stdout.
	TransportStdio TransportType = "stdio"
)

// transport carries framed JSON-RPC messages to and from a single MCP server.
// The streamable HTTP and local stdio transports each implement it; the
// session-level Client (see client.go) drives the MCP handshake over whichever
// transport a server is configured for.
type transport interface {
	// roundTrip sends a JSON-RPC request payload and returns the matching
	// response. Server-initiated requests and notifications received in the
	// meantime are ignored (TeaNode exposes neither sampling nor roots).
	roundTrip(ctx context.Context, payload []byte) (*jsonrpcResponse, error)
	// notify sends a one-way JSON-RPC notification payload.
	notify(ctx context.Context, payload []byte) error
	// observeProtocolVersion records the protocol version negotiated during
	// initialize. The HTTP transport echoes it back in the MCP-Protocol-Version
	// header; the stdio transport ignores it.
	observeProtocolVersion(version string)
	// close releases the transport's resources. The stdio transport terminates
	// the subprocess; the HTTP transport is a no-op.
	close() error
}

// newTransport builds the transport for a server based on its configured
// transport type, defaulting to streamable HTTP.
func newTransport(server ServerConfiguration) transport {
	if server.Transport == TransportStdio {
		return newStdioTransport(server)
	}
	return newHttpTransport(server)
}
