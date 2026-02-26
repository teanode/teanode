package models

import "time"

type MediaSource string

const (
	MediaSourceTool     MediaSource = "tool"
	MediaSourceUpload   MediaSource = "upload"
	MediaSourceDiscord  MediaSource = "discord"
	MediaSourceTelegram MediaSource = "telegram"
)

type Media struct {
	ID             string       `json:"id,omitempty" yaml:"id,omitempty"`
	UserID         *string      `json:"userId,omitempty" yaml:"userId,omitempty"`
	Format         *string      `json:"format,omitempty" yaml:"format,omitempty"`
	ContentType    *string      `json:"contentType,omitempty" yaml:"contentType,omitempty"`
	Source         *MediaSource `json:"source,omitempty" yaml:"source,omitempty"`
	SourceAgentID  *string      `json:"sourceAgentId,omitempty" yaml:"sourceAgentId,omitempty"`
	ConversationID *string      `json:"conversationId,omitempty" yaml:"conversationId,omitempty"`
	ToolName       *string      `json:"toolName,omitempty" yaml:"toolName,omitempty"`
	ToolCallID     *string      `json:"toolCallId,omitempty" yaml:"toolCallId,omitempty"`
	OriginalName   *string      `json:"originalName,omitempty" yaml:"originalName,omitempty"`
	Size           *int64       `json:"size,omitempty" yaml:"size,omitempty"`
	CreatedAt      *time.Time   `json:"createdAt,omitempty" yaml:"createdAt,omitempty"`
	ModifiedAt     *time.Time   `json:"modifiedAt,omitempty" yaml:"modifiedAt,omitempty"`
}
