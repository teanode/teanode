package channels

import (
	"regexp"
	"strings"
)

// suggestionsPattern matches the hidden suggested replies marker along with
// any adjacent newlines introduced by its presence.
var suggestionsPattern = regexp.MustCompile(`\n?<!--suggestions:\[.*?\]-->\n?`)

// StripSuggestedReplies removes any hidden suggested-replies marker from text
// before it is sent to an external channel (Discord, Telegram, etc.).
func StripSuggestedReplies(text string) string {
	result := suggestionsPattern.ReplaceAllStringFunc(text, func(match string) string {
		// If the marker is surrounded by newlines on both sides (middle of text),
		// preserve one newline to keep surrounding lines properly separated.
		if strings.HasPrefix(match, "\n") && strings.HasSuffix(match, "\n") {
			return "\n"
		}
		return ""
	})
	return strings.TrimRight(result, "\n")
}
