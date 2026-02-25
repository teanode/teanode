package models

import "time"

type ConversationMessage struct {
	ID             string      `json:"id,omitempty" yaml:"id,omitempty"`
	ConversationID *string     `json:"conversationId,omitempty" yaml:"conversationId,omitempty"`
	Role           *Role       `json:"role,omitempty" yaml:"role,omitempty"`
	Content        *[]byte     `json:"content,omitempty" yaml:"content,omitempty"`
	Metadata       *[]byte     `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	StopReason     *StopReason `json:"stopReason,omitempty" yaml:"stopReason,omitempty"`
	Model          *string     `json:"model,omitempty" yaml:"model,omitempty"`
	Provider       *string     `json:"provider,omitempty" yaml:"provider,omitempty"`
	ToolCallID     *string     `json:"toolCallId,omitempty" yaml:"toolCallId,omitempty"`
	ToolName       *string     `json:"toolName,omitempty" yaml:"toolName,omitempty"`
	Sequence       *int64      `json:"sequence,omitempty" yaml:"sequence,omitempty"`
	CreatedAt      *time.Time  `json:"createdAt,omitempty" yaml:"createdAt,omitempty"`
	ModifiedAt     *time.Time  `json:"modifiedAt,omitempty" yaml:"modifiedAt,omitempty"`
}
