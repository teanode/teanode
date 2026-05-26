package providers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestElevenLabsClient_StreamSynthesize(t *testing.T) {
	upgrader := websocket.Upgrader{}
	receivedTexts := make(chan string, 3)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		conn, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer func() { _ = conn.Close() }()

		for i := 0; i < 3; i++ {
			_, payload, err := conn.ReadMessage()
			if err != nil {
				t.Errorf("read message: %v", err)
				return
			}
			var envelope map[string]any
			if err := json.Unmarshal(payload, &envelope); err != nil {
				t.Errorf("unmarshal sender envelope: %v", err)
				return
			}
			if text, ok := envelope["text"].(string); ok {
				receivedTexts <- text
			}
		}

		_ = conn.WriteJSON(map[string]any{"audio": base64.StdEncoding.EncodeToString([]byte{1, 2, 3})})
		_ = conn.WriteMessage(websocket.BinaryMessage, []byte{4, 5})
		_ = conn.WriteJSON(map[string]any{"isFinal": true})
	}))
	defer server.Close()

	client := NewElevenLabsClient(server.URL, "key")
	chunks, err := client.SynthesizeStream(context.Background(), SynthesizeStreamRequest{
		Text:  "hello",
		Voice: "voice-id",
	})
	if err != nil {
		t.Fatalf("SynthesizeStream: %v", err)
	}

	var got [][]byte
	for chunk := range chunks {
		if chunk.Err != nil {
			t.Fatalf("unexpected chunk error: %v", chunk.Err)
		}
		got = append(got, chunk.Audio)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(got))
	}
	if len(got[0]) != 3 || len(got[1]) != 2 {
		t.Fatalf("unexpected chunk lengths: %d %d", len(got[0]), len(got[1]))
	}

	first := <-receivedTexts
	second := <-receivedTexts
	third := <-receivedTexts
	if first != " " || second != "hello" || third != "" {
		t.Fatalf("unexpected sender payloads: %q, %q, %q", first, second, third)
	}
}

func TestElevenLabsClient_ContextCancel(t *testing.T) {
	upgrader := websocket.Upgrader{}
	serverExited := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		conn, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer close(serverExited)
		defer func() { _ = conn.Close() }()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer server.Close()

	client := NewElevenLabsClient(server.URL, "key")
	ctx, cancel := context.WithCancel(context.Background())
	chunks, err := client.SynthesizeStream(ctx, SynthesizeStreamRequest{
		Text: "cancel me",
	})
	if err != nil {
		t.Fatalf("SynthesizeStream: %v", err)
	}

	var waitGroup sync.WaitGroup
	waitGroup.Add(1)
	go func() {
		defer waitGroup.Done()
		for range chunks {
		}
	}()
	cancel()

	done := make(chan struct{})
	go func() {
		waitGroup.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream reader did not exit after cancel")
	}
	select {
	case <-serverExited:
	case <-time.After(2 * time.Second):
		t.Fatal("server connection not closed after cancel")
	}
}
