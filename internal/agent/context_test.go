package agent

import (
	"strings"
	"testing"

	"github.com/teanode/teanode/internal/provider"
)

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"", 0},
		{"a", 1},
		{"ab", 1},
		{"abc", 1},
		{"abcd", 1},
		{"abcde", 2},
		{"hello world", 3},
		{strings.Repeat("x", 100), 25},
		{strings.Repeat("x", 400), 100},
	}
	for _, tt := range tests {
		got := estimateTokens(tt.input)
		if got != tt.want {
			t.Errorf("estimateTokens(%d chars) = %d, want %d", len(tt.input), got, tt.want)
		}
	}
}

func TestEstimateMessageTokens(t *testing.T) {
	msg := provider.ChatMessage{
		Role:    "assistant",
		Content: strings.Repeat("x", 100),
		ToolCalls: []provider.ToolCall{
			{
				ID:   "call-1",
				Type: "function",
				Function: provider.FunctionCall{
					Name:      "web_search",
					Arguments: `{"query":"test"}`,
				},
			},
		},
	}
	tokens := estimateMessageTokens(msg)
	// Content: 25 + 4 overhead + tool call tokens
	if tokens < 25 {
		t.Errorf("estimateMessageTokens = %d, expected >= 25", tokens)
	}
}

func TestTruncateOldToolResults(t *testing.T) {
	// Build messages: system + 12 messages (> minKeepMessages)
	messages := make([]provider.ChatMessage, 0, 15)
	messages = append(messages, provider.ChatMessage{Role: "system", Content: "system prompt"})

	// Old tool result with large content (should be truncated)
	bigContent := strings.Repeat("x", 20000)
	messages = append(messages, provider.ChatMessage{Role: "user", Content: "search for something"})
	messages = append(messages, provider.ChatMessage{
		Role:    "assistant",
		Content: "",
		ToolCalls: []provider.ToolCall{
			{ID: "call-1", Type: "function", Function: provider.FunctionCall{Name: "web_search", Arguments: `{}`}},
		},
	})
	messages = append(messages, provider.ChatMessage{Role: "tool", Content: bigContent, ToolCallID: "call-1", Name: "web_search"})
	messages = append(messages, provider.ChatMessage{Role: "assistant", Content: "here's what I found"})

	// Add enough recent messages to fill minKeepMessages
	for index := 0; index < minKeepMessages; index++ {
		messages = append(messages, provider.ChatMessage{Role: "user", Content: "recent message"})
	}

	result := truncateOldToolResults(messages)

	// The old tool result (index 3) should be truncated
	if len(result[3].Content) >= 20000 {
		t.Errorf("old tool result was not truncated: len=%d", len(result[3].Content))
	}
	if !strings.HasSuffix(result[3].Content, "... (truncated)") {
		t.Error("truncated content should end with '... (truncated)'")
	}
	if len(result[3].Content) > maxToolResultChars+20 {
		t.Errorf("truncated content too long: %d", len(result[3].Content))
	}

	// Recent messages should be preserved
	lastIdx := len(result) - 1
	if result[lastIdx].Content != "recent message" {
		t.Errorf("recent message was modified: %q", result[lastIdx].Content)
	}
}

func TestTruncateOldToolResultsShortHistory(t *testing.T) {
	messages := []provider.ChatMessage{
		{Role: "system", Content: "prompt"},
		{Role: "user", Content: "hi"},
		{Role: "tool", Content: strings.Repeat("x", 20000), ToolCallID: "c1", Name: "test"},
	}

	result := truncateOldToolResults(messages)

	// With fewer than minKeepMessages, nothing should be truncated
	if result[2].Content != messages[2].Content {
		t.Error("short history should not be truncated")
	}
}

func TestFindKeepBoundary(t *testing.T) {
	t.Run("simple messages", func(t *testing.T) {
		messages := make([]provider.ChatMessage, 20)
		for index := range messages {
			if index%2 == 0 {
				messages[index] = provider.ChatMessage{Role: "user", Content: "msg"}
			} else {
				messages[index] = provider.ChatMessage{Role: "assistant", Content: "reply"}
			}
		}

		boundary := findKeepBoundary(messages, 10)
		if boundary != 10 {
			t.Errorf("findKeepBoundary = %d, want 10", boundary)
		}
	})

	t.Run("tool result at boundary", func(t *testing.T) {
		messages := []provider.ChatMessage{
			{Role: "user", Content: "q1"},
			{Role: "assistant", Content: "a1"},
			{Role: "user", Content: "q2"},
			{Role: "assistant", Content: "", ToolCalls: []provider.ToolCall{{ID: "c1"}}},
			{Role: "tool", Content: "result1", ToolCallID: "c1"}, // target split would be here
			{Role: "tool", Content: "result2", ToolCallID: "c2"},
			{Role: "assistant", Content: "final"},
			{Role: "user", Content: "q3"},
			{Role: "assistant", Content: "a3"},
		}

		// minKeep=4 -> target = 9-4 = 5 (tool result)
		boundary := findKeepBoundary(messages, 4)
		// Should back up to include the assistant with tool calls at index 3
		if boundary > 3 {
			t.Errorf("findKeepBoundary = %d, should be <= 3 to include assistant with tool calls", boundary)
		}
	})

	t.Run("assistant with tool calls at boundary", func(t *testing.T) {
		messages := []provider.ChatMessage{
			{Role: "user", Content: "q1"},
			{Role: "assistant", Content: "a1"},
			{Role: "user", Content: "q2"},
			{Role: "assistant", Content: "", ToolCalls: []provider.ToolCall{{ID: "c1"}}},
			{Role: "tool", Content: "result", ToolCallID: "c1"},
			{Role: "assistant", Content: "done"},
		}

		// minKeep=2 -> target = 6-2 = 4 (tool result)
		boundary := findKeepBoundary(messages, 2)
		// Should include the assistant+tool pair
		if boundary > 3 {
			t.Errorf("findKeepBoundary = %d, should be <= 3", boundary)
		}
	})

	t.Run("all messages within minKeep", func(t *testing.T) {
		messages := []provider.ChatMessage{
			{Role: "user", Content: "q1"},
			{Role: "assistant", Content: "a1"},
		}

		boundary := findKeepBoundary(messages, 10)
		if boundary != 0 {
			t.Errorf("findKeepBoundary = %d, want 0", boundary)
		}
	})
}
