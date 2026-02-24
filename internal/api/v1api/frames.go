package v1api

import (
	"encoding/json"
	"fmt"
	"strings"
)

// requestFrame is a client-to-server RPC request.
type requestFrame struct {
	Type   string          `json:"type"` // "req"
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
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

// voiceEnvelope is the canonical wrapper for server/client voice JSON control messages.
type voiceEnvelope struct {
	V         int         `json:"v"`
	Type      string      `json:"type"`
	SessionID string      `json:"session_id"`
	Seq       uint64      `json:"seq"`
	TSMS      int64       `json:"ts_ms"`
	Payload   interface{} `json:"payload,omitempty"`
}

type voiceAudioFormat struct {
	Codec        string `json:"codec"`
	SampleRateHz int    `json:"sample_rate_hz"`
	Channels     int    `json:"channels"`
	FrameMS      int    `json:"frame_ms,omitempty"`
}

type voiceFeatures struct {
	ServerVAD    bool   `json:"server_vad"`
	ServerTurn   bool   `json:"server_turn"`
	ServerDenoise bool  `json:"server_denoise"`
	BargeIn      bool   `json:"barge_in"`
	TurnStrategy string `json:"turn_strategy,omitempty"`
}

type voiceClientInfo struct {
	Platform   string `json:"platform,omitempty"`
	AppVersion string `json:"app_version,omitempty"`
}

type voiceStartParams struct {
	ConversationID string           `json:"conversation_id"`
	AgentID        string           `json:"agent_id"`
	PromptSuffix   string           `json:"prompt_suffix,omitempty"`
	AudioIn        voiceAudioFormat `json:"audio_in"`
	AudioOut       voiceAudioFormat `json:"audio_out"`
	Features       voiceFeatures    `json:"features"`
	Client         voiceClientInfo  `json:"client,omitempty"`
}

type voiceSessionReadyPayload struct {
	SessionID      string           `json:"session_id"`
	ConversationID string           `json:"conversation_id"`
	AudioOut       voiceAudioFormat `json:"audio_out"`
	Features       voiceFeatures    `json:"features"`
}

type voiceEndParams struct {
	SessionID string `json:"session_id"`
}

type voiceResponseCancelParams struct {
	ResponseID string `json:"response_id"`
	Reason     string `json:"reason,omitempty"`
}

type voiceInputCommitParams struct {
	Reason string `json:"reason,omitempty"`
}

type turnEventPayload struct {
	TurnID      string  `json:"turn_id,omitempty"`
	Event       string  `json:"event"`
	VADScore    float64 `json:"vad_score,omitempty"`
	AudioSeqRef uint64  `json:"audio_seq_ref,omitempty"`
}

type transcriptFinalPayload struct {
	TurnID string `json:"turn_id,omitempty"`
	Text   string `json:"text"`
}

type responseStartedPayload struct {
	ResponseID string `json:"response_id"`
	TurnID     string `json:"turn_id,omitempty"`
}

type responseCompletedPayload struct {
	ResponseID string `json:"response_id"`
	TurnID     string `json:"turn_id,omitempty"`
}

type voiceErrorPayload struct {
	Code         string `json:"code"`
	Message      string `json:"message"`
	Recoverable  bool   `json:"recoverable"`
	RetryAfterMS int    `json:"retry_after_ms,omitempty"`
}

type sessionEndedPayload struct {
	Reason         string `json:"reason,omitempty"`
	ConversationID string `json:"conversation_id,omitempty"`
}

func validateVoiceAudioFormats(audioIn, audioOut voiceAudioFormat) error {
	inCodec := strings.ToLower(strings.TrimSpace(audioIn.Codec))
	outCodec := strings.ToLower(strings.TrimSpace(audioOut.Codec))

	if inCodec == "" {
		inCodec = "pcm_s16le"
	}
	if outCodec == "" {
		outCodec = "pcm_s16le"
	}

	if inCodec != "pcm_s16le" {
		return fmt.Errorf("audio_in.codec must be pcm_s16le")
	}
	if outCodec != "pcm_s16le" {
		return fmt.Errorf("audio_out.codec must be pcm_s16le")
	}
	if audioIn.Channels != 0 && audioIn.Channels != 1 {
		return fmt.Errorf("audio_in.channels must be 1")
	}
	if audioIn.SampleRateHz != 0 && audioIn.SampleRateHz != 8000 && audioIn.SampleRateHz != 16000 {
		return fmt.Errorf("audio_in.sample_rate_hz must be 8000 or 16000")
	}
	return nil
}
