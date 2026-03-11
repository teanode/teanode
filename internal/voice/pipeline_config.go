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
)

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

func (self *Session) transcriptionPrompt() string {
	// Keep STT unprompted to avoid model-side semantic "helpfulness" that can
	// drift from literal user speech.
	return ""
}
