package runners

import "context"

// buildVoiceOverlay returns a formatted reminder when the current message
// originates from a voice interaction. Best-effort: returns "" when not in a
// voice context.
func buildVoiceOverlay(ctx context.Context) string {
	mode := VoiceModeFromContext(ctx)
	switch mode {
	case VoiceModeCall:
		return "<voice-call>\n" +
			"The user is in a live voice call with you. Their messages are transcribed speech " +
			"and your responses will be spoken aloud in real time. Keep responses brief and " +
			"conversational — 1-3 sentences unless the user asks for more detail. Avoid markdown " +
			"formatting, code blocks, and bullet lists.\n" +
			"</voice-call>"
	case VoiceModeInput:
		return "<voice-input>\n" +
			"The user dictated this message using voice input and the response may be read aloud. " +
			"Keep the response concise and avoid heavy markdown formatting.\n" +
			"</voice-input>"
	default:
		return ""
	}
}
