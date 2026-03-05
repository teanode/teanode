package voice

import (
	"strings"
	"time"
)

const (
	minCommittedTurnBytes     = 6400 // ~200ms at 16kHz mono s16le
	minCommittedTextRunes     = 1
	bargeInTriggerMinScore    = 0.06
	maxResponseStartDelay     = 2 * time.Second
	vadPreRollFrames          = 8 // keep ~160ms leading context so first words aren't clipped
	streamingFinalGracePeriod = 75 * time.Millisecond
	minStreamingFinalRunes    = 5 // streaming finals shorter than this fall back to batch
	// voiceMaxContextTokens is the estimated-token budget for voice LLM requests.
	// Uses len(text)/4 approximation. Keeps recent turns within a 16k window so
	// long sessions do not balloon the prompt beyond model context limits.
	voiceMaxContextTokens = 16000
)

const voiceCallPromptSuffix = "The user is in a live voice call with you. Their messages are transcribed speech and your responses will be spoken aloud in real time. Keep responses brief and conversational - 1-3 sentences unless the user asks for more detail. Avoid markdown formatting, code blocks, and bullet lists."

func voiceProviderModelHint(kind, provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "openai":
		if kind == "synthesizer" {
			return "tts-1"
		}
		return "whisper-1"
	case "deepgram":
		return "nova-2"
	case "elevenlabs":
		return "eleven_flash_v2_5"
	default:
		return "unknown"
	}
}

func (self *Session) effectivePromptSuffix() string {
	if strings.TrimSpace(self.PromptSuffix) != "" {
		return self.PromptSuffix
	}
	return voiceCallPromptSuffix
}

func (self *Session) transcriptionPrompt() string {
	// Keep STT unprompted to avoid model-side semantic "helpfulness" that can
	// drift from literal user speech.
	return ""
}
