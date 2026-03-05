package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestDeepgramClient_StreamTranscribe(t *testing.T) {
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		conn, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		if _, audio, err := conn.ReadMessage(); err != nil || len(audio) == 0 {
			t.Errorf("read audio: %v", err)
			return
		}
		_ = conn.WriteJSON(map[string]any{
			"type":     "Results",
			"is_final": true,
			"channel": map[string]any{
				"alternatives": []map[string]any{
					{"transcript": "hello world"},
				},
			},
		})
	}))
	defer server.Close()

	client := NewDeepgramClient(server.URL, "token")
	client.keepAliveInterval = time.Hour
	stream, err := client.TranscribeStream(context.Background(), StreamTranscribeRequest{
		SampleRate: 16000,
		Channels:   1,
	})
	if err != nil {
		t.Fatalf("TranscribeStream: %v", err)
	}
	defer stream.Close()

	if err := stream.SendAudio([]byte{1, 2, 3, 4}); err != nil {
		t.Fatalf("SendAudio: %v", err)
	}

	select {
	case event := <-stream.Events():
		if event.Err != nil {
			t.Fatalf("unexpected stream error: %v", event.Err)
		}
		if event.Type != "final" || event.Text != "hello world" {
			t.Fatalf("unexpected event: %#v", event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for transcript event")
	}
}

func TestDeepgramClient_KeepAlive(t *testing.T) {
	upgrader := websocket.Upgrader{}
	keepAliveSeen := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		conn, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		for {
			_, payload, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var envelope map[string]any
			if err := json.Unmarshal(payload, &envelope); err != nil {
				continue
			}
			if envelope["type"] == "KeepAlive" {
				select {
				case keepAliveSeen <- struct{}{}:
				default:
				}
				return
			}
		}
	}))
	defer server.Close()

	client := NewDeepgramClient(server.URL, "token")
	client.keepAliveInterval = 20 * time.Millisecond
	stream, err := client.TranscribeStream(context.Background(), StreamTranscribeRequest{
		SampleRate: 16000,
		Channels:   1,
	})
	if err != nil {
		t.Fatalf("TranscribeStream: %v", err)
	}
	defer stream.Close()

	select {
	case <-keepAliveSeen:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for keepalive")
	}
}

func TestDeepgramClient_ErrorMidStream(t *testing.T) {
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		conn, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		_ = conn.Close()
	}))
	defer server.Close()

	client := NewDeepgramClient(server.URL, "token")
	stream, err := client.TranscribeStream(context.Background(), StreamTranscribeRequest{
		SampleRate: 16000,
		Channels:   1,
	})
	if err != nil {
		t.Fatalf("TranscribeStream: %v", err)
	}
	defer stream.Close()

	select {
	case event, ok := <-stream.Events():
		if !ok {
			t.Fatal("expected error event before stream close")
		}
		if event.Err == nil {
			t.Fatalf("expected stream error event, got %#v", event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for stream error")
	}
}
