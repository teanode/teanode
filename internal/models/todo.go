package models

import "time"

// Todo represents a task item scoped to either a project or a conversation.
type Todo struct {
	ID             string     `json:"id,omitempty"             yaml:"id,omitempty"`
	ProjectID      *string    `json:"projectId,omitempty"      yaml:"projectId,omitempty"`
	ConversationID *string    `json:"conversationId,omitempty" yaml:"conversationId,omitempty"`
	Title          *string    `json:"title,omitempty"          yaml:"title,omitempty"`
	Description    *string    `json:"description,omitempty"    yaml:"description,omitempty"`
	Status         *string    `json:"status,omitempty"         yaml:"status,omitempty"`
	Priority       *string    `json:"priority,omitempty"       yaml:"priority,omitempty"`
	Tags           *[]string  `json:"tags,omitempty"           yaml:"tags,omitempty"`
	CompletedAt    *time.Time `json:"completedAt,omitempty"    yaml:"completedAt,omitempty"`
	CreatedAt      *time.Time `json:"createdAt,omitempty"      yaml:"createdAt,omitempty"`
	ModifiedAt     *time.Time `json:"modifiedAt,omitempty"     yaml:"modifiedAt,omitempty"`
}

func (self *Todo) GetTitle() string {
	if self.Title != nil {
		return *self.Title
	}
	return ""
}

func (self *Todo) GetStatus() string {
	if self.Status != nil {
		return *self.Status
	}
	return ""
}

func (self *Todo) GetPriority() string {
	if self.Priority != nil {
		return *self.Priority
	}
	return ""
}

func (self *Todo) GetProjectID() string {
	if self.ProjectID != nil {
		return *self.ProjectID
	}
	return ""
}

func (self *Todo) GetConversationID() string {
	if self.ConversationID != nil {
		return *self.ConversationID
	}
	return ""
}

func (self *Todo) GetTags() []string {
	if self.Tags != nil {
		return *self.Tags
	}
	return nil
}
