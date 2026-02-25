package models

import "time"

type Conversation struct {
	ID         string     `json:"id,omitempty" yaml:"id,omitempty"`
	UserID     *string    `json:"userId,omitempty" yaml:"userId,omitempty"`
	AgentID    *string    `json:"agentId,omitempty" yaml:"agentId,omitempty"`
	Default    *bool      `json:"default,omitempty" yaml:"default,omitempty"`
	Title      *string    `json:"title,omitempty" yaml:"title,omitempty"`
	Summary    *string    `json:"summary,omitempty" yaml:"summary,omitempty"`
	CreatedAt    *time.Time `json:"createdAt,omitempty" yaml:"createdAt,omitempty"`
	ModifiedAt   *time.Time `json:"modifiedAt,omitempty" yaml:"modifiedAt,omitempty"`
	SummarizedAt *time.Time `json:"summarizedAt,omitempty" yaml:"summarizedAt,omitempty"`
}
