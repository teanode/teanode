package models

import (
	"encoding/json"
	"time"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type StopReason string

const (
	StopReasonUnknown        StopReason = "unknown"
	StopReasonEndTurn        StopReason = "end_turn"
	StopReasonMaxTokens      StopReason = "max_tokens"
	StopReasonToolUse        StopReason = "tool_use"
	StopReasonContextSummary StopReason = "context_summary"
	StopReasonCancelled      StopReason = "cancelled"
	StopReasonError          StopReason = "error"
)

type ConversationMessage struct {
	ID             string          `json:"id,omitempty" yaml:"id,omitempty"`
	ConversationID *string         `json:"conversationId,omitempty" yaml:"conversationId,omitempty"`
	Role           *Role           `json:"role,omitempty" yaml:"role,omitempty"`
	Content        json.RawMessage `json:"content,omitempty" yaml:"content,omitempty"`
	ToolCalls      json.RawMessage `json:"toolCalls,omitempty" yaml:"toolCalls,omitempty"`
	Usage          json.RawMessage `json:"usage,omitempty" yaml:"usage,omitempty"`
	Metadata       json.RawMessage `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	StopReason     *StopReason     `json:"stopReason,omitempty" yaml:"stopReason,omitempty"`
	Model          *string         `json:"model,omitempty" yaml:"model,omitempty"`
	Provider       *string         `json:"provider,omitempty" yaml:"provider,omitempty"`
	ToolCallID     *string         `json:"toolCallId,omitempty" yaml:"toolCallId,omitempty"`
	ToolName       *string         `json:"toolName,omitempty" yaml:"toolName,omitempty"`
	CreatedAt      *time.Time      `json:"createdAt,omitempty" yaml:"createdAt,omitempty"`
	ModifiedAt     *time.Time      `json:"modifiedAt,omitempty" yaml:"modifiedAt,omitempty"`
}
