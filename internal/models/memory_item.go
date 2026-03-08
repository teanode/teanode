package models

import "time"

type MemoryItem struct {
	ID         string     `json:"id,omitempty" yaml:"id,omitempty"`
	CreatedAt  *time.Time `json:"createdAt,omitempty" yaml:"createdAt,omitempty"`
	ModifiedAt *time.Time `json:"modifiedAt,omitempty" yaml:"modifiedAt,omitempty"`

	Scope   *Scope  `json:"scope,omitempty" yaml:"scope,omitempty"`
	ScopeID *string `json:"scopeId,omitempty" yaml:"scopeId,omitempty"`

	Title   *string   `json:"title,omitempty" yaml:"title,omitempty"`
	Content *string   `json:"content,omitempty" yaml:"content,omitempty"`
	Tags    *[]string `json:"tags,omitempty" yaml:"tags,omitempty"`

	ArchivedAt *time.Time `json:"archivedAt,omitempty" yaml:"archivedAt,omitempty"`
}
