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

// ContentBlock represents a single block within a multi-part message content array.
type ContentBlock struct {
	Type     string `json:"type"`               // "text" or "attachment"
	Text     string `json:"text,omitempty"`      // for type="text"
	MediaID  string `json:"mediaId,omitempty"`   // for type="attachment"
	Format   string `json:"format,omitempty"`    // for type="attachment"
	Filename string `json:"filename,omitempty"`  // for type="attachment"
}

// Attachment represents a file attached to a user message.
type Attachment struct {
	MediaID  string `json:"mediaId"`
	Format   string `json:"format"`
	Filename string `json:"filename"`
}

// ContentBlocks parses the message content as a []ContentBlock.
// If content is a plain JSON string, it returns a single text block.
// If content is a JSON array, it parses into []ContentBlock.
func (self *Message) ContentBlocks() []ContentBlock {
	// Try array first.
	var blocks []ContentBlock
	if err := json.Unmarshal(self.Content, &blocks); err == nil && len(blocks) > 0 {
		// Validate that we got actual content blocks (not just random data).
		if blocks[0].Type != "" {
			return blocks
		}
	}
	// Fall back to plain string.
	return []ContentBlock{{Type: "text", Text: self.ContentText()}}
}

// NewMessageWithAttachments creates a user message with text and file attachments.
// The content is stored as a JSON array of ContentBlock entries.
func NewMessageWithAttachments(role, text string, attachments []Attachment, timestamp int64) Message {
	blocks := []ContentBlock{{Type: "text", Text: text}}
	for _, attachment := range attachments {
		blocks = append(blocks, ContentBlock{
			Type:     "attachment",
			MediaID:  attachment.MediaID,
			Format:   attachment.Format,
			Filename: attachment.Filename,
		})
	}
	content, _ := json.Marshal(blocks)
	return Message{
		Role:      role,
		Content:   content,
		Timestamp: timestamp,
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
