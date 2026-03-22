package cloud

import (
	"io"
	"sync"

	"github.com/gorilla/websocket"
)

// StreamTransport adapts a yamux stream with framed messages as a
// api.MessageTransport, bridging the cloud proxy to the local RPC handler.
type StreamTransport struct {
	stream     io.ReadWriteCloser
	writeMutex sync.Mutex
}

// NewStreamTransport creates a new StreamTransport wrapping the given stream.
func NewStreamTransport(stream io.ReadWriteCloser) *StreamTransport {
	return &StreamTransport{stream: stream}
}

// ReadMessage reads a framed message from the stream and returns it
// as a WebSocket-style message type + payload.
func (self *StreamTransport) ReadMessage() (int, []byte, error) {
	message, err := readMessage(self.stream)
	if err != nil {
		if err == io.EOF {
			return 0, nil, io.EOF
		}
		return 0, nil, err
	}
	switch message.messageType {
	case messageTypeText:
		return websocket.TextMessage, message.payload, nil
	case messageTypeBinary:
		return websocket.BinaryMessage, message.payload, nil
	default:
		return websocket.TextMessage, message.payload, nil
	}
}

// WriteTextMessage sends a text message as a framed message on the stream.
func (self *StreamTransport) WriteTextMessage(data []byte) error {
	self.writeMutex.Lock()
	defer self.writeMutex.Unlock()
	return writeMessage(self.stream, &message{messageType: messageTypeText, payload: data})
}

// WriteBinaryMessage sends a binary message as a framed message on the stream.
func (self *StreamTransport) WriteBinaryMessage(data []byte) error {
	self.writeMutex.Lock()
	defer self.writeMutex.Unlock()
	return writeMessage(self.stream, &message{messageType: messageTypeBinary, payload: data})
}

// Close closes the underlying stream.
func (self *StreamTransport) Close() error {
	return self.stream.Close()
}
