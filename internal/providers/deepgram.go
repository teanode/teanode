package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// DeepgramClient provides streaming STT.
type DeepgramClient struct {
	BaseProvider
	baseURL           string
	apiKey            string
	dialer            *websocket.Dialer
	keepAliveInterval time.Duration
}

// NewDeepgramClient creates a Deepgram provider.
func NewDeepgramClient(baseURL, apiKey string) *DeepgramClient {
	return &DeepgramClient{
		baseURL:           strings.TrimSpace(baseURL),
		apiKey:            strings.TrimSpace(apiKey),
		dialer:            websocket.DefaultDialer,
		keepAliveInterval: 8 * time.Second,
	}
}

// TranscribeStream creates a realtime transcription websocket session.
func (self *DeepgramClient) TranscribeStream(ctx context.Context, request StreamTranscribeRequest) (TranscribeStream, error) {
	if strings.TrimSpace(self.apiKey) == "" {
		return nil, fmt.Errorf("deepgram api key is required")
	}
	listenURL, err := deepgramListenURL(self.baseURL, request)
	if err != nil {
		return nil, err
	}
	headers := http.Header{}
	headers.Set("Authorization", "Token "+self.apiKey)
	connection, _, err := self.dialer.DialContext(ctx, listenURL, headers)
	if err != nil {
		return nil, fmt.Errorf("open deepgram stream: %w", err)
	}
	stream := &deepgramStream{
		connection:        connection,
		events:            make(chan TranscribeStreamEvent, 32),
		done:              make(chan struct{}),
		keepAliveInterval: self.keepAliveInterval,
	}
	go stream.readLoop()
	go stream.keepAliveLoop()
	return stream, nil
}

func deepgramListenURL(baseURL string, request StreamTranscribeRequest) (string, error) {
	base := strings.TrimSpace(baseURL)
	if base == "" {
		return "", fmt.Errorf("deepgram base url is required")
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("parse deepgram base url: %w", err)
	}
	switch parsed.Scheme {
	case "https":
		parsed.Scheme = "wss"
	case "http":
		parsed.Scheme = "ws"
	case "wss", "ws":
	default:
		return "", fmt.Errorf("unsupported deepgram scheme: %s", parsed.Scheme)
	}
	parsed.Path = "/v1/listen"
	query := parsed.Query()
	query.Set("model", "nova-2")
	query.Set("encoding", "linear16")
	sampleRate := request.SampleRate
	if sampleRate <= 0 {
		sampleRate = 16000
	}
	channels := request.Channels
	if channels <= 0 {
		channels = 1
	}
	query.Set("sample_rate", fmt.Sprintf("%d", sampleRate))
	query.Set("channels", fmt.Sprintf("%d", channels))
	query.Set("interim_results", "true")
	query.Set("endpointing", "false")
	query.Set("punctuate", "true")
	query.Set("smart_format", "true")
	if text := strings.TrimSpace(request.Language); text != "" {
		query.Set("language", text)
	}
	if text := strings.TrimSpace(request.Prompt); text != "" {
		query.Set("keywords", text)
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

type deepgramStream struct {
	connection        *websocket.Conn
	writeMu           sync.Mutex
	events            chan TranscribeStreamEvent
	done              chan struct{}
	closeOnce         sync.Once
	keepAliveInterval time.Duration
}

func (self *deepgramStream) SendAudio(pcm []byte) error {
	if len(pcm) == 0 {
		return nil
	}
	self.writeMu.Lock()
	defer self.writeMu.Unlock()
	return self.connection.WriteMessage(websocket.BinaryMessage, pcm)
}

func (self *deepgramStream) Events() <-chan TranscribeStreamEvent {
	return self.events
}

func (self *deepgramStream) Close() error {
	var closeErr error
	self.closeOnce.Do(func() {
		close(self.done)
		self.writeMu.Lock()
		_ = self.connection.WriteJSON(map[string]string{"type": "CloseStream"})
		self.writeMu.Unlock()
		closeErr = self.connection.Close()
	})
	return closeErr
}

func (self *deepgramStream) readLoop() {
	defer close(self.events)
	for {
		_, payload, err := self.connection.ReadMessage()
		if err != nil {
			select {
			case self.events <- TranscribeStreamEvent{Err: fmt.Errorf("deepgram read: %w", err)}:
			case <-self.done:
			}
			_ = self.Close()
			return
		}
		var envelope struct {
			Type    string `json:"type"`
			IsFinal bool   `json:"is_final"`
			Channel struct {
				Alternatives []struct {
					Transcript string  `json:"transcript"`
					Confidence float64 `json:"confidence"`
				} `json:"alternatives"`
			} `json:"channel"`
		}
		if err := json.Unmarshal(payload, &envelope); err != nil {
			continue
		}
		if envelope.Type != "Results" {
			continue
		}
		if len(envelope.Channel.Alternatives) == 0 {
			continue
		}
		text := strings.TrimSpace(envelope.Channel.Alternatives[0].Transcript)
		if text == "" {
			continue
		}
		eventType := "interim"
		if envelope.IsFinal {
			eventType = "final"
		}
		select {
		case <-self.done:
			return
		case self.events <- TranscribeStreamEvent{
			Type:       eventType,
			Text:       text,
			Confidence: envelope.Channel.Alternatives[0].Confidence,
		}:
		}
	}
}

func (self *deepgramStream) keepAliveLoop() {
	interval := self.keepAliveInterval
	if interval <= 0 {
		interval = 8 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-self.done:
			return
		case <-ticker.C:
			self.writeMu.Lock()
			err := self.connection.WriteJSON(map[string]string{"type": "KeepAlive"})
			self.writeMu.Unlock()
			if err != nil {
				return
			}
		}
	}
}
