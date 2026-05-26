package cloud

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	// messageTypeText represents a WebSocket text message.
	messageTypeText byte = 0x01
	// messageTypeBinary represents a WebSocket binary message.
	messageTypeBinary byte = 0x02

	// maxMessageSize limits the size of a single framed message (16MB).
	maxMessageSize = 16 << 20
)

// message is a typed message carried over a yamux stream, preserving
// WebSocket text/binary semantics.
type message struct {
	messageType byte
	payload     []byte
}

// writeMessage writes a length-prefixed message to the writer.
// Wire format: [1 byte type][4 bytes big-endian length][payload]
func writeMessage(writer io.Writer, message *message) error {
	header := make([]byte, 5)
	header[0] = message.messageType
	binary.BigEndian.PutUint32(header[1:5], uint32(len(message.payload)))
	if _, err := writer.Write(header); err != nil {
		return err
	}
	if len(message.payload) > 0 {
		if _, err := writer.Write(message.payload); err != nil {
			return err
		}
	}
	return nil
}

// readMessage reads a length-prefixed message from the reader.
func readMessage(reader io.Reader) (*message, error) {
	header := make([]byte, 5)
	if _, err := io.ReadFull(reader, header); err != nil {
		return nil, err
	}
	messageType := header[0]
	length := binary.BigEndian.Uint32(header[1:5])
	if length > maxMessageSize {
		return nil, fmt.Errorf("cloud: message too large: %d bytes", length)
	}
	payload := make([]byte, length)
	if length > 0 {
		if _, err := io.ReadFull(reader, payload); err != nil {
			return nil, err
		}
	}
	return &message{messageType: messageType, payload: payload}, nil
}
