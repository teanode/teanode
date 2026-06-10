package textsplit

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateUTF8(t *testing.T) {
	testCases := []struct {
		name         string
		text         string
		maximumBytes int
		expected     string
	}{
		{name: "fits", text: "hello", maximumBytes: 10, expected: "hello"},
		{name: "exact", text: "hello", maximumBytes: 5, expected: "hello"},
		{name: "ascii cut", text: "hello", maximumBytes: 3, expected: "hel"},
		{name: "zero limit", text: "hello", maximumBytes: 0, expected: ""},
		{name: "multibyte boundary", text: "héllo", maximumBytes: 2, expected: "h"},
		{name: "emoji not split", text: "a😀b", maximumBytes: 4, expected: "a"},
		{name: "emoji fits", text: "a😀b", maximumBytes: 5, expected: "a😀"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result := TruncateUTF8(testCase.text, testCase.maximumBytes)
			if result != testCase.expected {
				t.Fatalf("TruncateUTF8(%q, %d) = %q, want %q", testCase.text, testCase.maximumBytes, result, testCase.expected)
			}
			if !utf8.ValidString(result) {
				t.Fatalf("TruncateUTF8(%q, %d) produced invalid UTF-8", testCase.text, testCase.maximumBytes)
			}
		})
	}
}

func TestChunkPoint(t *testing.T) {
	t.Run("fits returns full length", func(t *testing.T) {
		if point := ChunkPoint("short", 100); point != 5 {
			t.Fatalf("expected 5, got %d", point)
		}
	})

	t.Run("prefers newline in second half", func(t *testing.T) {
		text := strings.Repeat("a", 60) + "\n" + strings.Repeat("b", 60)
		if point := ChunkPoint(text, 100); point != 60 {
			t.Fatalf("expected 60, got %d", point)
		}
	})

	t.Run("ignores newline in first half", func(t *testing.T) {
		text := strings.Repeat("a", 10) + "\n" + strings.Repeat("b", 200)
		if point := ChunkPoint(text, 100); point != 100 {
			t.Fatalf("expected 100, got %d", point)
		}
	})

	t.Run("never splits a rune", func(t *testing.T) {
		text := strings.Repeat("😀", 50)
		point := ChunkPoint(text, 99)
		if !utf8.ValidString(text[:point]) || !utf8.ValidString(text[point:]) {
			t.Fatalf("chunk point %d splits a rune", point)
		}
	})
}
