package models

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type Scope string

const (
	ScopeAgent   Scope = "agent"
	ScopeUser    Scope = "user"
	ScopeProject Scope = "project"
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
