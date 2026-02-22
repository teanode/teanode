package protocol

import "testing"

func TestToWebSocketURL(t *testing.T) {
	t.Parallel()
	url, err := toWebSocketUrl("http://127.0.0.1:8833")
	if err != nil {
		t.Fatalf("toWebSocketUrl failed: %v", err)
	}
	expected := "ws://127.0.0.1:8833/api/v1/websocket"
	if url != expected {
		t.Fatalf("unexpected websocket URL: got=%s want=%s", url, expected)
	}
}
