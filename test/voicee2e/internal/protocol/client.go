package protocol

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/teanode/teanode/internal/voice"
	"github.com/teanode/teanode/test/voicee2e/internal/model"
)

type rpcRequest struct {
	Type       string      `json:"type"`
	ID         string      `json:"id"`
	Method     string      `json:"method"`
	Parameters interface{} `json:"params,omitempty"`
}

type rpcResponse struct {
	Type    string          `json:"type"`
	ID      string          `json:"id"`
	OK      bool            `json:"ok"`
	Payload json.RawMessage `json:"payload"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type eventFrame struct {
	Type    string          `json:"type"`
	Event   string          `json:"event"`
	Payload json.RawMessage `json:"payload"`
}

type voiceStartParams struct {
	ConversationID string `json:"conversationId"`
	AgentID        string `json:"agentId"`
	PromptSuffix   string `json:"promptSuffix,omitempty"`
	AudioIn        struct {
		Codec        string `json:"codec"`
		SampleRateHz int    `json:"sampleRateHz"`
		Channels     int    `json:"channels"`
		FrameMS      int    `json:"frameMs"`
	} `json:"audioIn"`
	AudioOut struct {
		Codec        string `json:"codec"`
		SampleRateHz int    `json:"sampleRateHz"`
		Channels     int    `json:"channels"`
	} `json:"audioOut"`
	Features struct {
		ServerVAD    bool   `json:"serverVad"`
		ServerTurn   bool   `json:"serverTurn"`
		BargeIn      bool   `json:"bargeIn"`
		TurnStrategy string `json:"turnStrategy,omitempty"`
	} `json:"features"`
}

type voiceEnvelope struct {
	V         int             `json:"v"`
	Type      string          `json:"type"`
	SessionID string          `json:"sessionId"`
	Sequence  uint64          `json:"seq"`
	TSMS      int64           `json:"tsMs"`
	Payload   json.RawMessage `json:"payload"`
}

type Client struct {
	gatewayUrl   string
	promptSuffix string
	configJSON   string
}

func NewClient(gatewayUrl string) *Client {
	return &Client{gatewayUrl: gatewayUrl}
}

func (self *Client) SetPromptSuffix(value string) {
	self.promptSuffix = value
}

func (self *Client) SetConfigJSON(value string) {
	self.configJSON = strings.TrimSpace(value)
}

func (self *Client) RunScenario(ctx context.Context, scenario model.ScenarioSpecification) ([]model.TimelineEvent, error) {
	debugEnabled := voiceE2eDebugEnabled()

	websocketUrl, err := toWebSocketUrl(self.gatewayUrl)
	if err != nil {
		return nil, err
	}
	headers := http.Header{}
	if token := resolveGatewayToken(); token != "" {
		headers.Set("Authorization", "Bearer "+token)
	}
	debugf(debugEnabled, "dial websocket url=%s auth_header=%t", websocketUrl, headers.Get("Authorization") != "")
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, websocketUrl, headers)
	if err != nil {
		return nil, fmt.Errorf("dial websocket: %w", err)
	}
	defer conn.Close()

	type responseWaiter struct {
		channel chan rpcResponse
	}
	waiters := map[string]responseWaiter{}
	var waitersMutex sync.Mutex
	timeline := make([]model.TimelineEvent, 0, 256)
	var timelineMutex sync.Mutex
	var seq atomic.Uint64
	var responseStartedCount atomic.Int32
	responseStartedByRun := map[string]bool{}

	record := func(event model.TimelineEvent) {
		if event.Type == model.EventResponseStarted {
			responseStartedCount.Add(1)
		}
		timelineMutex.Lock()
		timeline = append(timeline, event)
		timelineMutex.Unlock()
	}

	readerDone := make(chan error, 1)
	go func() {
		for {
			messageType, data, readErr := conn.ReadMessage()
			if readErr != nil {
				debugf(debugEnabled, "read message error: %v", readErr)
				readerDone <- readErr
				return
			}
			now := time.Now()
			if messageType == websocket.BinaryMessage {
				frame, parseErr := voice.ParseBinaryAudioFrame(data)
				if parseErr != nil {
					continue
				}
				if frame.FrameType == voice.FrameTypeFlush {
					record(model.TimelineEvent{At: now, Type: model.EventResponseCompleted, Value: 0})
				}
				continue
			}

			var raw map[string]any
			if err := json.Unmarshal(data, &raw); err != nil {
				continue
			}

			if frameType, _ := raw["type"].(string); frameType == "res" {
				var response rpcResponse
				if err := json.Unmarshal(data, &response); err != nil {
					continue
				}
				debugf(debugEnabled, "rpc response id=%s ok=%t", response.ID, response.OK)
				waitersMutex.Lock()
				waiter, ok := waiters[response.ID]
				waitersMutex.Unlock()
				if ok {
					waiter.channel <- response
				}
				continue
			}

			if _, ok := raw["v"]; ok {
				var envelope voiceEnvelope
				if err := json.Unmarshal(data, &envelope); err != nil {
					continue
				}
				debugf(debugEnabled, "voice envelope type=%s session=%s", envelope.Type, envelope.SessionID)
				convertVoiceEnvelope(record, now, envelope)
				continue
			}

			if frameType, _ := raw["type"].(string); frameType == "event" {
				var frame eventFrame
				if err := json.Unmarshal(data, &frame); err != nil {
					continue
				}
				if frame.Event == "conversation" {
					var payload map[string]any
					if err := json.Unmarshal(frame.Payload, &payload); err == nil {
						state, _ := payload["state"].(string)
						text, _ := payload["text"].(string)
						runId, _ := payload["runId"].(string)
						if state == "delta" && strings.TrimSpace(text) != "" && runId != "" && !responseStartedByRun[runId] {
							responseStartedByRun[runId] = true
							record(model.TimelineEvent{At: now, Type: model.EventResponseStarted, RunID: runId, Raw: payload})
						}
					}
					debugf(debugEnabled, "conversation event")
					convertConversationEvent(record, now, frame.Payload)
				}
			}
		}
	}()

	sendRpc := func(method string, parameters any) error {
		id := fmt.Sprintf("r-%d", seq.Add(1))
		wait := responseWaiter{channel: make(chan rpcResponse, 1)}
		waitersMutex.Lock()
		waiters[id] = wait
		waitersMutex.Unlock()
		defer func() {
			waitersMutex.Lock()
			delete(waiters, id)
			waitersMutex.Unlock()
		}()

		request := rpcRequest{Type: "req", ID: id, Method: method, Parameters: parameters}
		debugf(debugEnabled, "send rpc method=%s id=%s", method, id)
		if err := conn.WriteJSON(request); err != nil {
			return err
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-readerDone:
			return err
		case response := <-wait.channel:
			if !response.OK {
				if response.Error != nil {
					return fmt.Errorf("%s failed: %s", method, response.Error.Message)
				}
				return fmt.Errorf("%s failed", method)
			}
			return nil
		}
	}

	start := voiceStartParams{
		AgentID:        "main",
		ConversationID: "",
		PromptSuffix:   self.promptSuffix,
	}
	start.AudioIn.Codec = "pcm_s16le"
	start.AudioIn.SampleRateHz = 16000
	start.AudioIn.Channels = 1
	start.AudioIn.FrameMS = 20
	start.AudioOut.Codec = "pcm_s16le"
	start.AudioOut.SampleRateHz = 24000
	start.AudioOut.Channels = 1
	start.Features.ServerVAD = true
	start.Features.ServerTurn = true
	start.Features.BargeIn = true
	if self.configJSON != "" {
		self.applyConfig(&start)
	}
	if err := sendRpc("voice.start", start); err != nil {
		return nil, err
	}
	debugf(debugEnabled, "voice.start acknowledged")

	base := filepath.Join("test", "voicee2e", "fixtures")
	frameSeq := uint64(1)
	maxResponseWait := time.Duration(scenario.Expect.MaxResponseLatencyMS) * time.Millisecond
	if maxResponseWait <= 0 {
		maxResponseWait = 5 * time.Second
	}
	waitForResponseStarted := func(beforeCount int32) error {
		deadline := time.Now().Add(maxResponseWait)
		for time.Now().Before(deadline) {
			if responseStartedCount.Load() > beforeCount {
				return nil
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case err := <-readerDone:
				return err
			case <-time.After(50 * time.Millisecond):
			}
		}
		return nil
	}
	for index, step := range scenario.Audio {
		if step.DelayBeforeMS > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(step.DelayBeforeMS) * time.Millisecond):
			}
		}
		pcm, err := LoadWAVAsPCM16Mono(filepath.Join(base, step.Fixture), 16000)
		if err != nil {
			return nil, fmt.Errorf("load fixture %s: %w", step.Fixture, err)
		}
		for len(pcm) > 0 {
			readLen := len(pcm)
			if readLen > 640 {
				readLen = 640
			}
			chunk := pcm[:readLen]
			if readLen < 640 {
				padding := make([]byte, 640)
				copy(padding, chunk)
				chunk = padding
			}
			pcm = pcm[readLen:]
			frame := voice.EncodeBinaryAudioFrame(voice.BinaryAudioFrame{
				FrameType:   voice.FrameTypeAudioIn,
				Seq:         frameSeq,
				CaptureTSMs: time.Now().UnixMilli(),
				DurationMS:  20,
				Data:        chunk,
			})
			frameSeq++
			if err := conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
				return nil, fmt.Errorf("write binary frame: %w", err)
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(20 * time.Millisecond):
			}
		}
		if step.DelayAfterMS > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(step.DelayAfterMS) * time.Millisecond):
			}
		}

		hasNext := index+1 < len(scenario.Audio)
		nextStepExpectsBargeIn := hasNext && scenario.Audio[index+1].ExpectBargeIn
		shouldWaitForResponse := !hasNext || (!step.ExpectBargeIn && !nextStepExpectsBargeIn)
		if shouldWaitForResponse {
			beforeCount := responseStartedCount.Load()
			if err := waitForResponseStarted(beforeCount); err != nil {
				return nil, err
			}
		}
	}

	_ = sendRpc("voice.end", map[string]any{})
	debugf(debugEnabled, "voice.end sent")
	_ = conn.Close()

	timelineMutex.Lock()
	defer timelineMutex.Unlock()
	return append([]model.TimelineEvent(nil), timeline...), nil
}

func (self *Client) applyConfig(start *voiceStartParams) {
	var cfg struct {
		Features struct {
			ServerVAD  *bool `json:"serverVad"`
			ServerTurn *bool `json:"serverTurn"`
			BargeIn    *bool `json:"bargeIn"`
		} `json:"features"`
		Voice struct {
			TurnStrategy string `json:"turnStrategy"`
		} `json:"voice"`
	}
	if err := json.Unmarshal([]byte(self.configJSON), &cfg); err != nil {
		return
	}
	if cfg.Features.ServerVAD != nil {
		start.Features.ServerVAD = *cfg.Features.ServerVAD
	}
	if cfg.Features.ServerTurn != nil {
		start.Features.ServerTurn = *cfg.Features.ServerTurn
	}
	if cfg.Features.BargeIn != nil {
		start.Features.BargeIn = *cfg.Features.BargeIn
	}
	if text := strings.TrimSpace(cfg.Voice.TurnStrategy); text != "" {
		start.Features.TurnStrategy = text
	}
}

func toWebSocketUrl(gateway string) (string, error) {
	raw := gateway
	if raw == "" {
		raw = "http://127.0.0.1:8833"
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse gateway url: %w", err)
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("unsupported scheme: %s", u.Scheme)
	}
	if !strings.HasSuffix(u.Path, "/api/v1/websocket") {
		u.Path = strings.TrimSuffix(u.Path, "/") + "/api/v1/websocket"
	}

	return u.String(), nil
}

func resolveGatewayToken() string {
	if token, exists := os.LookupEnv("TEANODE_GATEWAY_TOKEN"); exists {
		return token
	}
	return ""
}

func voiceE2eDebugEnabled() bool {
	value := os.Getenv("VOICE_E2E_DEBUG")
	return value == "1" || strings.EqualFold(value, "true") || strings.EqualFold(value, "yes")
}

func debugf(enabled bool, format string, arguments ...interface{}) {
	if !enabled {
		return
	}
	fmt.Fprintf(os.Stderr, "[voicee2e] "+format+"\n", arguments...)
}

func convertConversationEvent(record func(model.TimelineEvent), now time.Time, payload json.RawMessage) {
	var event map[string]any
	if err := json.Unmarshal(payload, &event); err != nil {
		return
	}
	state, _ := event["state"].(string)
	text, _ := event["text"].(string)
	runId, _ := event["runId"].(string)
	switch state {
	case "user_message":
		record(model.TimelineEvent{At: now, Type: model.EventTranscriptFinal, Text: text, RunID: runId, Raw: event})
	case "delta":
		record(model.TimelineEvent{At: now, Type: model.EventTTSInput, Text: text, RunID: runId, Raw: event})
	}
}

func convertVoiceEnvelope(record func(model.TimelineEvent), now time.Time, envelope voiceEnvelope) {
	var payload map[string]any
	_ = json.Unmarshal(envelope.Payload, &payload)
	switch envelope.Type {
	case "turn.event":
		event, _ := payload["event"].(string)
		switch event {
		case "speech_started":
			record(model.TimelineEvent{At: now, Type: model.EventSpeechStarted, SessionID: envelope.SessionID, Raw: payload})
		case "speech_ended":
			record(model.TimelineEvent{At: now, Type: model.EventSpeechEnded, SessionID: envelope.SessionID, Raw: payload})
		case "turn_committed":
			record(model.TimelineEvent{At: now, Type: model.EventTurnCommitted, SessionID: envelope.SessionID, Raw: payload})
		case "turn_queued":
			record(model.TimelineEvent{At: now, Type: model.EventTurnQueued, SessionID: envelope.SessionID, Raw: payload})
		case "turn_dropped":
			record(model.TimelineEvent{At: now, Type: model.EventTurnDropped, SessionID: envelope.SessionID, Raw: payload})
		case "bargeInTriggered":
			record(model.TimelineEvent{At: now, Type: model.EventBargeInTriggered, SessionID: envelope.SessionID, Raw: payload})
		}
	case "transcript.final":
		text, _ := payload["text"].(string)
		turnId, _ := payload["turnId"].(string)
		record(model.TimelineEvent{At: now, Type: model.EventTranscriptFinal, SessionID: envelope.SessionID, TurnID: turnId, Text: text, Raw: payload})
	case "response.started":
		responseId, _ := payload["responseId"].(string)
		turnId, _ := payload["turnId"].(string)
		record(model.TimelineEvent{At: now, Type: model.EventResponseStarted, SessionID: envelope.SessionID, TurnID: turnId, ResponseID: responseId, Raw: payload})
	case "response.completed":
		responseId, _ := payload["responseId"].(string)
		turnId, _ := payload["turnId"].(string)
		record(model.TimelineEvent{At: now, Type: model.EventResponseCompleted, SessionID: envelope.SessionID, TurnID: turnId, ResponseID: responseId, Raw: payload})
	case "turn.metrics":
		record(model.TimelineEvent{At: now, Type: model.EventTurnMetrics, SessionID: envelope.SessionID, Raw: payload})
	}
}
