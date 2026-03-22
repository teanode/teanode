package cloud

import (
	"bytes"
	"net"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// wsConn adapts a gorilla/websocket connection to net.Conn for use with yamux.
// It reassembles websocket messages into a contiguous byte stream.
type webSocketConnection struct {
	connection *websocket.Conn
	readBuffer bytes.Buffer
	readMutex  sync.Mutex
	writeMutex sync.Mutex
}

func newWebSocketConnection(connection *websocket.Conn) net.Conn {
	return &webSocketConnection{connection: connection}
}

func (self *webSocketConnection) Read(buffer []byte) (int, error) {
	self.readMutex.Lock()
	defer self.readMutex.Unlock()

	if self.readBuffer.Len() > 0 {
		return self.readBuffer.Read(buffer)
	}

	_, data, err := self.connection.ReadMessage()
	if err != nil {
		return 0, err
	}

	count := copy(buffer, data)
	if count < len(data) {
		self.readBuffer.Write(data[count:])
	}
	return count, nil
}

func (self *webSocketConnection) Write(data []byte) (int, error) {
	self.writeMutex.Lock()
	defer self.writeMutex.Unlock()
	if err := self.connection.WriteMessage(websocket.BinaryMessage, data); err != nil {
		return 0, err
	}
	return len(data), nil
}

func (self *webSocketConnection) Close() error {
	return self.connection.Close()
}

func (self *webSocketConnection) LocalAddr() net.Addr {
	return self.connection.LocalAddr()
}

func (self *webSocketConnection) RemoteAddr() net.Addr {
	return self.connection.RemoteAddr()
}

func (self *webSocketConnection) SetDeadline(deadline time.Time) error {
	if err := self.connection.SetReadDeadline(deadline); err != nil {
		return err
	}
	return self.connection.SetWriteDeadline(deadline)
}

func (self *webSocketConnection) SetReadDeadline(deadline time.Time) error {
	return self.connection.SetReadDeadline(deadline)
}

func (self *webSocketConnection) SetWriteDeadline(deadline time.Time) error {
	return self.connection.SetWriteDeadline(deadline)
}
