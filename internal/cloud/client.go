package cloud

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hashicorp/yamux"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/util/deferutil"
	"github.com/teanode/teanode/internal/version"
)

// handshake is the initial frame sent to the server after connecting.
type handshake struct {
	NodeID       string `json:"nodeId"`
	Secret       string `json:"secret"`
	Hostname     string `json:"hostname"`
	Platform     string `json:"platform"`
	Architecture string `json:"architecture"`
	Version      string `json:"version"`
}

// Client manages a persistent WebSocket connection to the cloud server,
// multiplexed with yamux.
type Client struct {
	config        *models.CloudConfiguration
	streamHandler StreamHandler

	mutex       sync.Mutex
	session     *yamux.Session
	closed      bool
	doneChannel chan struct{}
}

// New creates a new cloud client with the given configuration.
func New(config *models.CloudConfiguration, handler StreamHandler) *Client {
	return &Client{
		config:        config,
		streamHandler: handler,
		doneChannel:   make(chan struct{}),
	}
}

// Start begins the connection loop in the background. It connects to the
// cloud server and automatically reconnects on disconnection.
func (self *Client) Start(ctx context.Context) {
	go self.connectLoop(ctx)
}

// Close shuts down the client and any active session.
func (self *Client) Close() error {
	self.mutex.Lock()
	if self.closed {
		self.mutex.Unlock()
		return nil
	}
	self.closed = true
	close(self.doneChannel)
	session := self.session
	self.session = nil
	self.mutex.Unlock()

	if session != nil {
		return session.Close()
	}
	return nil
}

// Session returns the current yamux session, or nil if not connected.
func (self *Client) Session() *yamux.Session {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.session
}

func (self *Client) connectLoop(ctx context.Context) {
	defer deferutil.Recover()
	backoff := time.Second

	for {
		// check if closed
		select {
		case <-self.doneChannel:
			return
		case <-ctx.Done():
			return
		default:
		}

		err := self.connect(ctx)
		if err != nil {
			log.Warningf("cloud connection failed: %v", err)
		}

		// check if closed before sleeping
		select {
		case <-self.doneChannel:
			return
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}

		// exponential backoff capped at 30 seconds
		backoff = backoff * 2
		if backoff > 30*time.Second {
			backoff = 30 * time.Second
		}
	}
}

func (self *Client) connect(ctx context.Context) error {
	websocketUrl, err := self.buildUrl()
	if err != nil {
		return err
	}

	log.Infof("connecting to cloud server url=%s", websocketUrl)

	connection, _, err := websocket.DefaultDialer.DialContext(ctx, websocketUrl, nil)
	if err != nil {
		return fmt.Errorf("cloud: websocket dial: %w", err)
	}

	// Perform handshake over raw websocket before starting yamux.
	if err := self.sendHandshake(connection); err != nil {
		_ = connection.Close()
		return fmt.Errorf("cloud: handshake send: %w", err)
	}

	replyPayload, err := receiveHandshake(connection)
	if err != nil {
		_ = connection.Close()
		return fmt.Errorf("cloud: handshake reply: %w", err)
	}

	// Wrap websocket as net.Conn for yamux.
	wrappedConnection := newWebSocketConnection(connection)

	yamuxConfig := yamux.DefaultConfig()
	yamuxConfig.LogOutput = io.Discard
	session, err := yamux.Client(wrappedConnection, yamuxConfig)
	if err != nil {
		_ = wrappedConnection.Close()
		return fmt.Errorf("cloud: yamux client: %w", err)
	}

	self.mutex.Lock()
	if self.closed {
		self.mutex.Unlock()
		_ = session.Close()
		return nil
	}
	self.session = session
	self.mutex.Unlock()

	log.Infof("connected to cloud server reply=%s", string(replyPayload))

	// Accept loop for server-initiated streams. Blocks until session closes.
	for {
		stream, err := session.AcceptStream()
		if err != nil {
			break
		}
		go self.handleStream(stream)
	}

	self.mutex.Lock()
	if self.session == session {
		self.session = nil
	}
	self.mutex.Unlock()

	log.Infof("disconnected from cloud server")
	return nil
}

func (self *Client) sendHandshake(connection *websocket.Conn) error {
	hostname, _ := os.Hostname()

	payload := handshake{
		NodeID:       self.config.GetNodeID(),
		Secret:       self.config.GetNodeSecret(),
		Hostname:     hostname,
		Platform:     runtime.GOOS,
		Architecture: runtime.GOARCH,
		Version:      version.ServerName(),
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("cloud: marshal handshake: %w", err)
	}
	return connection.WriteMessage(websocket.TextMessage, data)
}

// receiveHandshake reads a websocket text message as the handshake reply.
func receiveHandshake(connection *websocket.Conn) ([]byte, error) {
	messageType, data, err := connection.ReadMessage()
	if err != nil {
		return nil, fmt.Errorf("cloud: receive handshake: %w", err)
	}
	if messageType != websocket.TextMessage {
		return nil, fmt.Errorf("cloud: expected text handshake reply, got type %d", messageType)
	}
	return data, nil
}

func (self *Client) handleStream(stream io.ReadWriteCloser) {
	defer deferutil.Recover()
	// read the metadata prefix: [4-byte big-endian length][metadata bytes]
	header := make([]byte, 4)
	if _, err := io.ReadFull(stream, header); err != nil {
		log.Warningf("cloud stream metadata read failed: %v", err)
		_ = stream.Close()
		return
	}
	length := binary.BigEndian.Uint32(header)
	var metadataBytes []byte
	if length > 0 {
		metadataBytes = make([]byte, length)
		if _, err := io.ReadFull(stream, metadataBytes); err != nil {
			log.Warningf("cloud stream metadata read failed: %v", err)
			_ = stream.Close()
			return
		}
	}

	metadata, err := unmarshalStreamMetadata(metadataBytes)
	if err != nil {
		log.Warningf("cloud stream metadata decode failed: %v", err)
		_ = stream.Close()
		return
	}

	log.Infof("cloud proxy stream opened type=%s path=%q", metadata.Type, metadata.Path)
	if self.streamHandler != nil {
		self.streamHandler(metadata, stream)
	} else {
		_ = stream.Close()
	}
}

func (self *Client) buildUrl() (string, error) {
	cloudUrl := self.config.GetURL()

	if cloudUrl == "" {
		return "", fmt.Errorf("cloud: cloud URL is not configured")
	}
	if self.config.GetNodeID() == "" {
		return "", fmt.Errorf("cloud: cloud node ID is not configured")
	}
	if self.config.GetNodeSecret() == "" {
		return "", fmt.Errorf("cloud: cloud node secret is not configured")
	}

	parsed, err := url.Parse(cloudUrl)
	if err != nil {
		return "", fmt.Errorf("cloud: invalid cloud URL: %w", err)
	}

	// convert http(s) to ws(s)
	switch parsed.Scheme {
	case "https":
		parsed.Scheme = "wss"
	case "http":
		parsed.Scheme = "ws"
	case "ws", "wss":
		// already correct
	default:
		return "", fmt.Errorf("cloud: unsupported cloud URL scheme: %s", parsed.Scheme)
	}

	parsed.Path = "/api/node/websocket"

	return parsed.String(), nil
}
