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
	all, _ := splitSentences(text)
	if alreadyEnqueued >= len(all) {
		return nil, len(all)
	}
	return all[alreadyEnqueued:], len(all)
}

// FlushRemaining returns the tail fragment after all complete sentences.
func FlushRemaining(text string, alreadyEnqueued int) string {
	all, consumed := splitSentences(text)
	trimmed := text
	if len(all) == 0 {
		return trimmed
	}
	rem := trimmed[consumed:]
	if alreadyEnqueued >= len(all) {
		return rem
	}
	if alreadyEnqueued <= 0 {
		return trimmed
	}
	partial := all[:alreadyEnqueued]
	_, partialConsumed := splitSentences(strings.Join(partial, " "))
	if partialConsumed > len(trimmed) {
		return ""
	}
	return trimmed[partialConsumed:]
}

func splitSentences(text string) ([]string, int) {
	runes := []rune(text)
	if len(runes) == 0 {
		return nil, 0
	}
	var out []string
	start := 0
	lastConsumedRune := 0
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
		s := string(runes[start : i+1])
		if s != "" {
			out = append(out, s)
		}
		start = i + 1
		lastConsumedRune = start
		for start < len(runes) && unicode.IsSpace(runes[start]) {
			start++
			lastConsumedRune = start
		}
	}
	return out, byteOffsetForRuneIndex(string(runes), lastConsumedRune)
}

func byteOffsetForRuneIndex(text string, runeIdx int) int {
	if runeIdx <= 0 {
		return 0
	}
	i := 0
	for pos := range text {
		if i == runeIdx {
			return pos
		}
		i++
	}
	return len(text)
}

func looksLikeAbbreviation(sentence []rune) bool {
	s := string(sentence)
	if !strings.HasSuffix(s, ".") {
		return false
	}
	s = strings.TrimSuffix(s, ".")
	parts := strings.Fields(s)
	if len(parts) == 0 {
		return false
	}
	last := strings.ToLower(parts[len(parts)-1])
	_, ok := abbreviations[last]
	return ok
}

func isASCIIPunct(r rune) bool {
	return r == '.' || r == '!' || r == '?'
}

func isSentenceEndRune(r rune) bool {
	return isASCIIPunct(r) || r == '。' || r == '！' || r == '？'
}
