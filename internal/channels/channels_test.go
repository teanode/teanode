package channels

import "testing"

func TestStripSuggestedReplies(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no marker",
			input:    "Hello, how can I help?",
			expected: "Hello, how can I help?",
		},
		{
			name:     "marker at end with newline",
			input:    "Hello, how can I help?\n<!--suggestions:[\"Option A\",\"Option B\"]-->",
			expected: "Hello, how can I help?",
		},
		{
			name:     "marker at end without preceding newline",
			input:    "Hello<!--suggestions:[\"Yes\",\"No\"]-->",
			expected: "Hello",
		},
		{
			name:     "marker at end with trailing newline",
			input:    "Hello\n<!--suggestions:[\"Yes\",\"No\"]-->\n",
			expected: "Hello",
		},
		{
			name:     "marker in middle",
			input:    "Hello\n<!--suggestions:[\"A\",\"B\"]-->\nWorld",
			expected: "Hello\nWorld",
		},
		{
			name:     "empty suggestions array",
			input:    "Hello\n<!--suggestions:[]-->",
			expected: "Hello",
		},
		{
			name:     "complex suggestions",
			input:    "Check this out\n<!--suggestions:[\"Tell me more\",\"What's next?\",\"Thanks!\"]-->",
			expected: "Check this out",
		},
		{
			name:     "multiline message with marker at end",
			input:    "Line 1\nLine 2\nLine 3\n<!--suggestions:[\"OK\"]-->",
			expected: "Line 1\nLine 2\nLine 3",
		},
		{
			name:     "empty input",
			input:    "",
			expected: "",
		},
		{
			name:     "only marker",
			input:    "<!--suggestions:[\"A\"]-->",
			expected: "",
		},
		{
			name:     "preserves leading whitespace",
			input:    "  indented\n<!--suggestions:[\"A\"]-->",
			expected: "  indented",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := StripSuggestedReplies(tt.input)
			if result != tt.expected {
				t.Errorf("StripSuggestedReplies(%q)\n  got:  %q\n  want: %q", tt.input, result, tt.expected)
			}
		})
	}
}
