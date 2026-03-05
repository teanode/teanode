package providers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
)

// ElevenLabsClient provides streaming TTS.
type ElevenLabsClient struct {
	BaseProvider
	baseURL string
	apiKey  string
	dialer  *websocket.Dialer
}

// NewElevenLabsClient creates an ElevenLabs provider.
func NewElevenLabsClient(baseURL, apiKey string) *ElevenLabsClient {
	return &ElevenLabsClient{
		baseURL: strings.TrimSpace(baseURL),
		apiKey:  strings.TrimSpace(apiKey),
		dialer:  websocket.DefaultDialer,
	}
}

// Synthesize is intentionally unsupported; voice path uses streaming synthesis.
func (self *ElevenLabsClient) Synthesize(_ context.Context, _ SynthesizeRequest) (*SynthesizeResponse, error) {
	return nil, fmt.Errorf("elevenlabs batch synthesis is unsupported in this integration")
}

// SynthesizeStream opens an ElevenLabs websocket stream and emits PCM chunks.
func (self *ElevenLabsClient) SynthesizeStream(ctx context.Context, request SynthesizeStreamRequest) (<-chan SynthesizeChunk, error) {
	if strings.TrimSpace(self.apiKey) == "" {
		return nil, fmt.Errorf("elevenlabs api key is required")
	}
	streamURL, err := elevenLabsStreamURL(self.baseURL, request.Voice, request.SampleRateHz)
	if err != nil {
		return nil, err
	}

	headers := http.Header{}
	headers.Set("xi-api-key", self.apiKey)
	conn, _, err := self.dialer.DialContext(ctx, streamURL, headers)
	if err != nil {
		return nil, fmt.Errorf("open elevenlabs stream: %w", err)
	}

	out := make(chan SynthesizeChunk, 32)
	closeOnce := sync.Once{}
	closeConn := func() {
		closeOnce.Do(func() {
			_ = conn.Close()
		})
	}

	go func() {
		<-ctx.Done()
		closeConn()
	}()

	go func() {
		defer close(out)
		defer closeConn()

		// ElevenLabs streaming protocol requires an initial whitespace-only
		// text message to prime the connection before sending actual content.
		if err := conn.WriteJSON(map[string]any{
			"text": " ",
		}); err != nil {
			out <- SynthesizeChunk{Err: fmt.Errorf("elevenlabs write init: %w", err)}
			return
		}
		if err := conn.WriteJSON(map[string]any{
			"text":                   request.Text,
			"try_trigger_generation": true,
		}); err != nil {
			out <- SynthesizeChunk{Err: fmt.Errorf("elevenlabs write text: %w", err)}
			return
		}
		if err := conn.WriteJSON(map[string]any{"text": ""}); err != nil {
			out <- SynthesizeChunk{Err: fmt.Errorf("elevenlabs write end: %w", err)}
			return
		}

		for {
			msgType, payload, err := conn.ReadMessage()
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				out <- SynthesizeChunk{Err: fmt.Errorf("elevenlabs read: %w", err)}
				return
			}
			if msgType == websocket.BinaryMessage {
				if len(payload) == 0 {
					continue
				}
				select {
				case <-ctx.Done():
					return
				case out <- SynthesizeChunk{Audio: payload}:
				}
				continue
			}
			if msgType == websocket.TextMessage {
				var envelope struct {
					Audio   string `json:"audio"`
					IsFinal bool   `json:"isFinal"`
				}
				if err := json.Unmarshal(payload, &envelope); err == nil {
					if strings.TrimSpace(envelope.Audio) != "" {
						audio, decodeErr := base64.StdEncoding.DecodeString(envelope.Audio)
						if decodeErr != nil {
							out <- SynthesizeChunk{Err: fmt.Errorf("elevenlabs decode audio: %w", decodeErr)}
							return
						}
						if len(audio) > 0 {
							select {
							case <-ctx.Done():
								return
							case out <- SynthesizeChunk{Audio: audio}:
							}
						}
					}
					if envelope.IsFinal {
						return
					}
				}
			}
		}
	}()

	return out, nil
}

func elevenLabsStreamURL(baseURL, voice string, sampleRateHz int) (string, error) {
	base := strings.TrimSpace(baseURL)
	if base == "" {
		return "", fmt.Errorf("elevenlabs base url is required")
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("parse elevenlabs base url: %w", err)
	}
	switch parsed.Scheme {
	case "https":
		parsed.Scheme = "wss"
	case "http":
		parsed.Scheme = "ws"
	case "wss", "ws":
	default:
		return "", fmt.Errorf("unsupported elevenlabs scheme: %s", parsed.Scheme)
	}

	voiceID := resolveElevenLabsVoiceID(voice)
	parsed.Path = fmt.Sprintf("/v1/text-to-speech/%s/stream-input", voiceID)
	query := parsed.Query()
	query.Set("model_id", "eleven_flash_v2_5")
	if sampleRateHz == 24000 || sampleRateHz == 0 {
		query.Set("output_format", "pcm_24000")
	} else {
		query.Set("output_format", "pcm_16000")
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func resolveElevenLabsVoiceID(voice string) string {
	switch strings.ToLower(strings.TrimSpace(voice)) {
	case "", "alloy", "default":
		return "EXAVITQu4vr4xnSDxMaL"
	default:
		return strings.TrimSpace(voice)
	}
}
