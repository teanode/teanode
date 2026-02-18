package conversations

import "encoding/json"

// Header is the first line of a JSONL conversation file.
type Header struct {
	Type         string `json:"type"`                   // "conversation"
	Version      int    `json:"version"`                // 1
	ID           string `json:"id"`                     // ULID
	Timestamp    string `json:"timestamp"`              // RFC3339
	Title        string `json:"title,omitempty"`        // conversation title
	Summary      string `json:"summary,omitempty"`      // conversation summary
	SummarizedAt int64  `json:"summarizedAt,omitempty"` // unix ms when summary was generated
	Provider     string `json:"provider,omitempty"`     // provider name (e.g. "openai")
	Model        string `json:"model,omitempty"`        // qualified "provider:model" format
}

// Message represents a chat message in a conversation.
type Message struct {
	Role       string          `json:"role"`      // "user" | "assistant" | "system" | "tool"
	Content    json.RawMessage `json:"content"`   // string or []ContentBlock
	Timestamp  int64           `json:"timestamp"` // ms since epoch
	StopReason string          `json:"stopReason,omitempty"`
	Usage      *Usage          `json:"usage,omitempty"`
	Model      string          `json:"model,omitempty"`
	Provider   string          `json:"provider,omitempty"`
	ToolCalls  json.RawMessage `json:"toolCalls,omitempty"`  // []provider.ToolCall, stored as raw JSON
	ToolCallID string          `json:"toolCallId,omitempty"` // for tool result messages
	ToolName   string          `json:"toolName,omitempty"`   // for tool result messages
}

// Usage tracks token usage for a message.
type Usage struct {
	Input  int `json:"input"`
	Output int `json:"output"`
	Total  int `json:"totalTokens"`
}

// ContentText returns the message content as a plain string.
// If content is a JSON string, it returns the unquoted value.
// Otherwise it returns the raw JSON bytes as-is.
func (self *Message) ContentText() string {
	var text string
	if err := json.Unmarshal(self.Content, &text); err == nil {
		return text
	}
	return string(self.Content)
}

// SetContentText sets the message content to a plain string.
func (self *Message) SetContentText(text string) {
	self.Content, _ = json.Marshal(text)
}

// NewTextMessage creates a message with plain text content.
func NewTextMessage(role, text string, timestamp int64) Message {
	content, _ := json.Marshal(text)
	return Message{
		Role:      role,
		Content:   content,
		Timestamp: timestamp,
	}
}

// NewSummaryMessage creates a system-role message that holds a context summary.
func NewSummaryMessage(summary string, timestamp int64) Message {
	content, _ := json.Marshal(summary)
	return Message{
		Role:       "system",
		Content:    content,
		Timestamp:  timestamp,
		StopReason: "context_summary",
	}
}

// NewToolMessage creates a tool result message.
func NewToolMessage(toolCallId, toolName, content string, timestamp int64) Message {
	contentJSON, _ := json.Marshal(content)
	return Message{
		Role:       "tool",
		Content:    contentJSON,
		Timestamp:  timestamp,
		ToolCallID: toolCallId,
		ToolName:   toolName,
	}
}
