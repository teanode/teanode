package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
)

const defaultRealtimeModel = "gpt-4o-realtime-preview"

// DialRealtime opens a WebSocket connection to the OpenAI Realtime API.
func (self *Client) DialRealtime(ctx context.Context, config RealtimeSessionConfig) (RealtimeConn, error) {
	model := config.Model
	if model == "" {
		model = defaultRealtimeModel
	}

	// Build the Realtime API WebSocket URL.
	baseWsUrl := strings.Replace(self.baseUrl, "https://", "wss://", 1)
	baseWsUrl = strings.Replace(baseWsUrl, "http://", "ws://", 1)
	// Strip /v1 suffix if present — the realtime endpoint is at /v1/realtime.
	baseWsUrl = strings.TrimSuffix(baseWsUrl, "/v1")
	wsUrl := fmt.Sprintf("%s/v1/realtime?model=%s", baseWsUrl, url.QueryEscape(model))

	headers := http.Header{
		"Authorization": []string{"Bearer " + self.apiKey},
		"OpenAI-Beta":   []string{"realtime=v1"},
	}

	dialer := websocket.DefaultDialer
	conn, _, err := dialer.DialContext(ctx, wsUrl, headers)
	if err != nil {
		return nil, fmt.Errorf("dialing realtime API: %w", err)
	}

	return &realtimeConn{conn: conn}, nil
}

// realtimeConn wraps a gorilla/websocket.Conn for the Realtime API.
type realtimeConn struct {
	conn       *websocket.Conn
	writeMutex sync.Mutex
}

func (self *realtimeConn) SendJSON(event map[string]any) error {
	self.writeMutex.Lock()
	defer self.writeMutex.Unlock()
	return self.conn.WriteJSON(event)
}

func (self *realtimeConn) ReadJSON(v any) error {
	return self.conn.ReadJSON(v)
}

func (self *realtimeConn) Close() error {
	return self.conn.Close()
}
