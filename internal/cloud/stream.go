package cloud

import "encoding/json"

// StreamType identifies the protocol used on a multiplexed stream.
type StreamType string

const (
	// StreamTypeWebSocket indicates a message-framed WebSocket relay stream.
	// Data is exchanged using the cloud message protocol (type + length + payload).
	StreamTypeWebSocket StreamType = "websocket"

	// StreamTypeHTTP indicates an HTTP/1.1 request/response stream.
	// A single HTTP request is written to the stream, and a single HTTP
	// response is read back.
	StreamTypeHTTP StreamType = "http"
)

// StreamMetadata is the structured metadata received when accepting a stream
// from the cloud server. It is JSON-encoded in the yamux stream metadata bytes.
type StreamMetadata struct {
	// Type specifies the stream protocol.
	Type StreamType `json:"type"`

	// Path is the target API endpoint (e.g., "/api/websocket").
	Path string `json:"path"`
}

// unmarshalStreamMetadata decodes metadata from the stream header bytes.
func unmarshalStreamMetadata(data []byte) (*StreamMetadata, error) {
	var metadata StreamMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, err
	}
	return &metadata, nil
}
