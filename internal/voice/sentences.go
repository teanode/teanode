package voice

import (
	"strings"
	"unicode"
)

var abbreviations = map[string]struct{}{
	"mr": {}, "mrs": {}, "ms": {}, "dr": {}, "prof": {}, "sr": {}, "jr": {}, "st": {}, "vs": {},
	"etc": {}, "approx": {}, "inc": {}, "ltd": {}, "corp": {}, "u.s": {}, "u.k": {}, "e.g": {}, "i.e": {},
}

// ExtractCompleteSentences returns only newly complete sentences after alreadyEnqueued.
func ExtractCompleteSentences(text string, alreadyEnqueued int) ([]string, int) {
	all := splitSentences(text)
	if alreadyEnqueued >= len(all) {
		return nil, len(all)
	}
	return all[alreadyEnqueued:], len(all)
}

// FlushRemaining returns the tail fragment after all complete sentences.
func FlushRemaining(text string, alreadyEnqueued int) string {
	all := splitSentences(text)
	if len(all) == 0 {
		return strings.TrimSpace(text)
	}
	joined := strings.Join(all, " ")
	trimmed := strings.TrimSpace(text)
	rem := strings.TrimSpace(strings.TrimPrefix(trimmed, strings.TrimSpace(joined)))
	if alreadyEnqueued >= len(all) {
		return rem
	}
	joined = strings.Join(all[:alreadyEnqueued], " ")
	return strings.TrimSpace(strings.TrimPrefix(trimmed, strings.TrimSpace(joined)))
}

func splitSentences(text string) []string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) == 0 {
		return nil
	}
	var out []string
	start := 0
	for i := 0; i < len(runes); i++ {
		if !isSentenceEndRune(runes[i]) {
			continue
		}
		if runes[i] == '.' && looksLikeAbbreviation(runes[start:i+1]) {
			continue
		}
		if isASCIIPunct(runes[i]) {
			if i+1 < len(runes) && !unicode.IsSpace(runes[i+1]) {
				continue
			}
			if i+1 < len(runes) {
				j := i + 1
				for j < len(runes) && unicode.IsSpace(runes[j]) {
					j++
				}
				if j < len(runes) && !unicode.IsUpper(runes[j]) {
					continue
				}
			}
		}
		s := strings.TrimSpace(string(runes[start : i+1]))
		if s != "" {
			out = append(out, s)
		}
		start = i + 1
		for start < len(runes) && unicode.IsSpace(runes[start]) {
			start++
		}
	}
	return out
}

func looksLikeAbbreviation(sentence []rune) bool {
	s := strings.TrimSpace(string(sentence))
	if !strings.HasSuffix(s, ".") {
		return false
	}
	s = strings.TrimSuffix(s, ".")
	parts := strings.Fields(s)
	if len(parts) == 0 {
		return false
	}
	last := strings.ToLower(strings.Trim(parts[len(parts)-1], " \t\r\n\"'()[]{}"))
	_, ok := abbreviations[last]
	return ok
}

func isASCIIPunct(r rune) bool {
	return r == '.' || r == '!' || r == '?'
}

func isSentenceEndRune(r rune) bool {
	return isASCIIPunct(r) || r == '。' || r == '！' || r == '？'
}
