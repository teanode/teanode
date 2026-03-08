package memory

import (
	"encoding/json"
	"strings"
)

// contentBlock represents one block in a multimodal content array.
type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// extractTextContent returns the plain text from a conversation message's
// Content field. Content may be a JSON string literal or an array of content
// blocks (only "text" blocks are extracted).
func extractTextContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	// Try plain string first.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}

	// Try array of content blocks.
	var blocks []contentBlock
	if err := json.Unmarshal(raw, &blocks); err == nil {
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}

	return ""
}
