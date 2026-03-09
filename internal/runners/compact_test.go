package runners

import (
	"strings"
	"testing"

	"github.com/teanode/teanode/internal/providers"
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
	for _, testCase := range tests {
		got := estimateTokens(testCase.input)
		if got != testCase.want {
			t.Errorf("estimateTokens(%d chars) = %d, want %d", len(testCase.input), got, testCase.want)
		}
	}
}

func TestEstimateMessageTokens(t *testing.T) {
	message := providers.ChatMessage{
		Role:    "assistant",
		Content: strings.Repeat("x", 100),
		ToolCalls: []providers.ToolCall{
			{
				ID:   "call-1",
				Type: "function",
				Function: providers.FunctionCall{
					Name:      "web_search",
					Arguments: `{"query":"test"}`,
				},
			},
		},
	}
	tokens := estimateMessageTokens(message)
	// Content: 25 + 4 overhead + tool call tokens
	if tokens < 25 {
		t.Errorf("estimateMessageTokens = %d, expected >= 25", tokens)
	}
}

func TestSanitizeToolResultForCompaction(t *testing.T) {
	input := `{"result":"ok","details":{"verbose":"drop-me"},"nested":{"details":"drop-too","keep":1}}`
	sanitized := sanitizeToolResultForCompaction(input)
	if strings.Contains(strings.ToLower(sanitized), "details") {
		t.Fatalf("sanitized tool result still contains details: %s", sanitized)
	}
	if !strings.Contains(sanitized, `"result":"ok"`) {
		t.Fatalf("sanitized tool result missing result field: %s", sanitized)
	}
	if !strings.Contains(sanitized, `"keep":1`) {
		t.Fatalf("sanitized tool result missing nested keep field: %s", sanitized)
	}
}

func TestEstimateMessageTokensIgnoresToolDetails(t *testing.T) {
	base := providers.ChatMessage{
		Role:    "tool",
		Content: `{"result":"ok"}`,
	}
	withDetails := providers.ChatMessage{
		Role: "tool",
		Content: `{"result":"ok","details":{"payload":"` +
			strings.Repeat("x", 10000) + `"}}`,
	}

	baseTokens := estimateMessageTokens(base)
	withDetailsTokens := estimateMessageTokens(withDetails)
	if withDetailsTokens != baseTokens {
		t.Fatalf("expected details to be ignored: base=%d withDetails=%d", baseTokens, withDetailsTokens)
	}
}

func TestChatMessagesTextIgnoresToolDetails(t *testing.T) {
	messages := []providers.ChatMessage{
		{
			Role:    "tool",
			Name:    "test",
			Content: `{"result":"ok","details":{"payload":"` + strings.Repeat("x", 2048) + `"}}`,
		},
	}
	text := chatMessagesText(messages, 0, 10000)
	if strings.Contains(strings.ToLower(text), "details") {
		t.Fatalf("chatMessagesText should strip details, got: %s", text)
	}
	if !strings.Contains(text, "result") {
		t.Fatalf("chatMessagesText missing result field: %s", text)
	}
}

func TestTruncateOldToolResults(t *testing.T) {
	// Build messages: system + 12 messages (> minKeepMessages)
	messages := make([]providers.ChatMessage, 0, 15)
	messages = append(messages, providers.ChatMessage{Role: "system", Content: "system prompt"})

	// Old tool result with large content (should be truncated)
	bigContent := strings.Repeat("x", 20000)
	messages = append(messages, providers.ChatMessage{Role: "user", Content: "search for something"})
	messages = append(messages, providers.ChatMessage{
		Role:    "assistant",
		Content: "",
		ToolCalls: []providers.ToolCall{
			{ID: "call-1", Type: "function", Function: providers.FunctionCall{Name: "web_search", Arguments: `{}`}},
		},
	})
	messages = append(messages, providers.ChatMessage{Role: "tool", Content: bigContent, ToolCallID: "call-1", Name: "web_search"})
	messages = append(messages, providers.ChatMessage{Role: "assistant", Content: "here's what I found"})

	// Add enough recent messages to fill minKeepMessages
	for index := 0; index < defaultModelRuntimeLimits().MinKeepMessages; index++ {
		messages = append(messages, providers.ChatMessage{Role: "user", Content: "recent message"})
	}

	result := truncateOldToolResults(messages, defaultModelRuntimeLimits().MinKeepMessages, defaultModelRuntimeLimits().MaxToolResultCharacters)

	// The old tool result (index 3) should be truncated
	if len(result[3].ContentText()) >= 20000 {
		t.Errorf("old tool result was not truncated: len=%d", len(result[3].ContentText()))
	}
	if !strings.HasSuffix(result[3].ContentText(), "... (truncated)") {
		t.Error("truncated content should end with '... (truncated)'")
	}
	if len(result[3].ContentText()) > defaultModelRuntimeLimits().MaxToolResultCharacters+40 {
		t.Errorf("truncated content too long: %d", len(result[3].ContentText()))
	}

	// Recent messages should be preserved
	lastIndex := len(result) - 1
	if result[lastIndex].ContentText() != "recent message" {
		t.Errorf("recent message was modified: %q", result[lastIndex].ContentText())
	}
}

func TestTruncateOldToolResultsShortHistory(t *testing.T) {
	messages := []providers.ChatMessage{
		{Role: "system", Content: "prompt"},
		{Role: "user", Content: "hi"},
		{Role: "tool", Content: strings.Repeat("x", 20000), ToolCallID: "c1", Name: "test"},
	}

	result := truncateOldToolResults(messages, defaultModelRuntimeLimits().MinKeepMessages, defaultModelRuntimeLimits().MaxToolResultCharacters)

	// With fewer than minKeepMessages, nothing should be truncated
	if result[2].ContentText() != messages[2].ContentText() {
		t.Error("short history should not be truncated")
	}
}

func TestTruncateOldToolResultsHardClear(t *testing.T) {
	maxChars := defaultModelRuntimeLimits().MaxToolResultCharacters
	messages := []providers.ChatMessage{
		{Role: "system", Content: "prompt"},
		{Role: "tool", Content: strings.Repeat("x", maxChars*5), ToolCallID: "c1", Name: "test"},
	}

	result := truncateOldToolResults(messages, 0, maxChars)
	if result[1].ContentText() != defaultHardClearedToolPlaceholder {
		t.Fatalf("expected hard-cleared placeholder, got %q", result[1].ContentText())
	}
}

func TestParseStructuredSummaryResponse(t *testing.T) {
	raw := "```json\n{\"summary\":\"done\",\"criticalFacts\":{\"decisions\":[\"A\"],\"todos\":[\"B\"]}}\n```"
	parsed := parseStructuredSummaryResponse(raw)
	if parsed.Summary != "done" {
		t.Fatalf("summary = %q, want done", parsed.Summary)
	}
	if len(parsed.CriticalFacts.Decisions) != 1 || parsed.CriticalFacts.Decisions[0] != "A" {
		t.Fatalf("unexpected decisions: %v", parsed.CriticalFacts.Decisions)
	}
	if len(parsed.CriticalFacts.Todos) != 1 || parsed.CriticalFacts.Todos[0] != "B" {
		t.Fatalf("unexpected todos: %v", parsed.CriticalFacts.Todos)
	}
}

func TestFormatStructuredSummary(t *testing.T) {
	formatted := formatStructuredSummary(structuredSummary{
		Summary: "Summary line",
		CriticalFacts: criticalFacts{
			Decisions:       []string{"Use A"},
			Todos:           []string{"Ship B"},
			Constraints:     []string{"Keep API stable"},
			UserPreferences: []string{"Short answers"},
			OpenQuestions:   []string{"Need benchmark?"},
		},
	})
	for _, want := range []string{
		"Summary line",
		"Critical facts:",
		"Decisions:",
		"Todos:",
		"Constraints:",
		"User preferences:",
		"Open questions:",
	} {
		if !strings.Contains(formatted, want) {
			t.Fatalf("formatted summary missing %q: %s", want, formatted)
		}
	}
}

func TestFindKeepBoundary(t *testing.T) {
	t.Run("simple messages", func(t *testing.T) {
		messages := make([]providers.ChatMessage, 20)
		for index := range messages {
			if index%2 == 0 {
				messages[index] = providers.ChatMessage{Role: "user", Content: "msg"}
			} else {
				messages[index] = providers.ChatMessage{Role: "assistant", Content: "reply"}
			}
		}

		boundary := findKeepBoundary(messages, 10)
		if boundary != 10 {
			t.Errorf("findKeepBoundary = %d, want 10", boundary)
		}
	})

	t.Run("tool result at boundary", func(t *testing.T) {
		messages := []providers.ChatMessage{
			{Role: "user", Content: "q1"},
			{Role: "assistant", Content: "a1"},
			{Role: "user", Content: "q2"},
			{Role: "assistant", Content: "", ToolCalls: []providers.ToolCall{{ID: "c1"}}},
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
		messages := []providers.ChatMessage{
			{Role: "user", Content: "q1"},
			{Role: "assistant", Content: "a1"},
			{Role: "user", Content: "q2"},
			{Role: "assistant", Content: "", ToolCalls: []providers.ToolCall{{ID: "c1"}}},
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
		messages := []providers.ChatMessage{
			{Role: "user", Content: "q1"},
			{Role: "assistant", Content: "a1"},
		}

		boundary := findKeepBoundary(messages, 10)
		if boundary != 0 {
			t.Errorf("findKeepBoundary = %d, want 0", boundary)
		}
	})
}

func TestExpandKeepBoundaryForRecentTokens(t *testing.T) {
	messages := []providers.ChatMessage{
		{Role: "system", Content: "prompt"},
		{Role: "user", Content: strings.Repeat("a", 200)},
		{Role: "assistant", Content: strings.Repeat("b", 200)},
		{Role: "user", Content: strings.Repeat("c", 50)},
	}

	originalKeepIndex := 3
	expandedKeepIndex := expandKeepBoundaryForRecentTokens(messages, originalKeepIndex, 40)
	if expandedKeepIndex != 2 {
		t.Fatalf("expandedKeepIndex = %d, want 2", expandedKeepIndex)
	}
}

func TestExpandKeepBoundaryForRecentTokensNoChangeWhenAlreadyEnough(t *testing.T) {
	messages := []providers.ChatMessage{
		{Role: "system", Content: "prompt"},
		{Role: "user", Content: strings.Repeat("x", 600)},
		{Role: "assistant", Content: strings.Repeat("y", 600)},
		{Role: "user", Content: strings.Repeat("z", 600)},
	}

	originalKeepIndex := 3
	expandedKeepIndex := expandKeepBoundaryForRecentTokens(messages, originalKeepIndex, 100)
	if expandedKeepIndex != originalKeepIndex {
		t.Fatalf("expandedKeepIndex = %d, want unchanged %d", expandedKeepIndex, originalKeepIndex)
	}
}
