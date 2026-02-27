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
	for index := 0; index < len(runes); index++ {
		if !isSentenceEndRune(runes[index]) {
			continue
		}
		if runes[index] == '.' && looksLikeAbbreviation(runes[start:index+1]) {
			continue
		}
		if isASCIIPunct(runes[index]) {
			if index+1 < len(runes) && !unicode.IsSpace(runes[index+1]) {
				continue
			}
			if index+1 < len(runes) {
				innerIndex := index + 1
				for innerIndex < len(runes) && unicode.IsSpace(runes[innerIndex]) {
					innerIndex++
				}
				if innerIndex < len(runes) && !unicode.IsUpper(runes[innerIndex]) {
					continue
				}
			}
		}
		sentence := string(runes[start : index+1])
		if sentence != "" {
			out = append(out, sentence)
		}
		start = index + 1
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
	index := 0
	for pos := range text {
		if index == runeIdx {
			return pos
		}
		index++
	}
	return len(text)
}

func looksLikeAbbreviation(sentenceRunes []rune) bool {
	sentenceString := string(sentenceRunes)
	if !strings.HasSuffix(sentenceString, ".") {
		return false
	}
	sentenceString = strings.TrimSuffix(sentenceString, ".")
	parts := strings.Fields(sentenceString)
	if len(parts) == 0 {
		return false
	}
	last := strings.ToLower(parts[len(parts)-1])
	_, ok := abbreviations[last]
	return ok
}

func isASCIIPunct(runeValue rune) bool {
	return runeValue == '.' || runeValue == '!' || runeValue == '?'
}

func isSentenceEndRune(runeValue rune) bool {
	return isASCIIPunct(runeValue) || runeValue == '。' || runeValue == '！' || runeValue == '？'
}
