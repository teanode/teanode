package api

import (
	"encoding/json"
	"fmt"
	"strings"
)

// requestFrame is a client-to-server RPC request.
type requestFrame struct {
	Type       string          `json:"type"` // "request"
	ID         string          `json:"id"`
	Method     string          `json:"method"`
	Parameters json.RawMessage `json:"parameters,omitempty"`
}

// responseFrame is a server-to-client RPC response.
type responseFrame struct {
	Type    string      `json:"type"` // "res"
	ID      string      `json:"id"`
	OK      bool        `json:"ok"`
	Payload interface{} `json:"payload,omitempty"`
	Error   *apiError   `json:"error,omitempty"`
}

// eventFrame is a server-to-client push event.
type eventFrame struct {
	Type    string      `json:"type"` // "event"
	Event   string      `json:"event"`
	Payload interface{} `json:"payload,omitempty"`
}

// apiError describes an error in an RPC response.
type apiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type voiceAudioFormat struct {
	Codec             string `json:"codec"`
	SampleRateHz      int    `json:"sampleRateHz"`
	Channels          int    `json:"channels"`
	FrameMilliseconds int    `json:"frameMs,omitempty"`
}

type voiceFeatures struct {
	ServerVAD    bool   `json:"serverVad"`
	ServerTurn   bool   `json:"serverTurn"`
	BargeIn      bool   `json:"bargeIn"`
	TurnStrategy string `json:"turnStrategy,omitempty"`
}

type voiceClientInformation struct {
	Platform   string `json:"platform,omitempty"`
	AppVersion string `json:"appVersion,omitempty"`
}

type voiceStartParameters struct {
	ConversationID string                 `json:"conversationId"`
	AgentID        string                 `json:"agentId"`
	AudioIn        voiceAudioFormat       `json:"audioIn"`
	AudioOut       voiceAudioFormat       `json:"audioOut"`
	Features       voiceFeatures          `json:"features"`
	Client         voiceClientInformation `json:"client,omitempty"`
}

type voiceSessionReadyPayload struct {
	SessionID      string           `json:"sessionId"`
	ConversationID string           `json:"conversationId"`
	AudioOut       voiceAudioFormat `json:"audioOut"`
	Features       voiceFeatures    `json:"features"`
}

type voiceEndParameters struct {
	SessionID string `json:"sessionId"`
}

type voiceResponseCancelParameters struct {
	ResponseID string `json:"responseId"`
	Reason     string `json:"reason,omitempty"`
}

type voiceInputCommitParameters struct {
	Reason string `json:"reason,omitempty"`
}

func validateVoiceAudioFormats(audioIn, audioOut voiceAudioFormat) error {
	inCodec := strings.ToLower(audioIn.Codec)
	outCodec := strings.ToLower(audioOut.Codec)

	if inCodec == "" {
		inCodec = "pcm_s16le"
	}
	if outCodec == "" {
		outCodec = "pcm_s16le"
	}

	if inCodec != "pcm_s16le" {
		return fmt.Errorf("api: audio_in.codec must be pcm_s16le")
	}
	if outCodec != "pcm_s16le" {
		return fmt.Errorf("api: audio_out.codec must be pcm_s16le")
	}
	if audioIn.Channels != 0 && audioIn.Channels != 1 {
		return fmt.Errorf("api: audio_in.channels must be 1")
	}
	if audioIn.SampleRateHz != 0 && audioIn.SampleRateHz != 8000 && audioIn.SampleRateHz != 16000 {
		return fmt.Errorf("api: audio_in.sample_rate_hz must be 8000 or 16000")
	}
	return nil
}
