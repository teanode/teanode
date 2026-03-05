package models

import "time"

type TodoStatus string

const (
	TodoStatusOpen TodoStatus = "open"
	TodoStatusDone TodoStatus = "done"
)

type TodoPriority string

const (
	TodoPriorityLow    TodoPriority = "low"
	TodoPriorityMedium TodoPriority = "medium"
	TodoPriorityHigh   TodoPriority = "high"
)

// Todo represents a task item scoped to either a project or a conversation.
type Todo struct {
	ID             string        `json:"id,omitempty"             yaml:"id,omitempty"`
	ProjectID      *string       `json:"projectId,omitempty"      yaml:"projectId,omitempty"`
	ConversationID *string       `json:"conversationId,omitempty" yaml:"conversationId,omitempty"`
	Title          *string       `json:"title,omitempty"          yaml:"title,omitempty"`
	Description    *string       `json:"description,omitempty"    yaml:"description,omitempty"`
	Status         *TodoStatus   `json:"status,omitempty"         yaml:"status,omitempty"`
	Priority       *TodoPriority `json:"priority,omitempty"       yaml:"priority,omitempty"`
	Tags           *[]string     `json:"tags,omitempty"           yaml:"tags,omitempty"`
	CompletedAt    *time.Time    `json:"completedAt,omitempty"    yaml:"completedAt,omitempty"`
	CreatedAt      *time.Time    `json:"createdAt,omitempty"      yaml:"createdAt,omitempty"`
	ModifiedAt     *time.Time    `json:"modifiedAt,omitempty"     yaml:"modifiedAt,omitempty"`
}
