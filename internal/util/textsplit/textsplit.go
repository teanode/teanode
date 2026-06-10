// Package textsplit provides UTF-8-safe helpers for truncating and chunking
// text to byte-length limits, e.g. for chat platforms with maximum message
// sizes. Plain byte slicing can split a multi-byte rune and produce invalid
// UTF-8; these helpers always cut at rune boundaries.
package textsplit

import (
	"unicode/utf8"

	"github.com/op/go-logging"
)

// Per-package logger declaration (mulint_log).
var log = logging.MustGetLogger("textsplit") //nolint:unused

// TruncateUTF8 returns text truncated to at most maximumBytes bytes without
// splitting a multi-byte UTF-8 rune. If text already fits, it is returned
// unchanged.
func TruncateUTF8(text string, maximumBytes int) string {
	if len(text) <= maximumBytes {
		return text
	}
	if maximumBytes <= 0 {
		return ""
	}
	cut := maximumBytes
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut]
}

// ChunkPoint returns the byte offset at which text should be cut so the first
// chunk is at most maximumBytes bytes. It prefers the last newline within the
// limit, as long as that keeps the chunk at least half full; otherwise it cuts
// at the largest rune boundary that fits. Returns len(text) when no cut is
// needed.
func ChunkPoint(text string, maximumBytes int) int {
	if len(text) <= maximumBytes {
		return len(text)
	}
	window := TruncateUTF8(text, maximumBytes)
	cut := len(window)
	for index := len(window) - 1; index >= maximumBytes/2; index-- {
		if window[index] == '\n' {
			cut = index
			break
		}
	}
	return cut
}
