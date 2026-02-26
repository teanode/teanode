package models

import "time"

type Scope string

const (
	ScopeAgent   Scope = "agent"
	ScopeUser    Scope = "user"
	ScopeProject Scope = "project"
)

type WorkspaceFile struct {
	ID         string     `json:"id,omitempty" yaml:"id,omitempty"`
	CreatedAt  *time.Time `json:"createdAt,omitempty" yaml:"createdAt,omitempty"`
	ModifiedAt *time.Time `json:"modifiedAt,omitempty" yaml:"modifiedAt,omitempty"`

	Scope       *Scope  `json:"scope,omitempty" yaml:"scope,omitempty"`
	ScopeID     *string `json:"scopeId,omitempty" yaml:"scopeId,omitempty"`
	Path        *string `json:"path,omitempty" yaml:"path,omitempty"`
	Content     *[]byte `json:"content,omitempty" yaml:"content,omitempty"`
	ContentType *string `json:"contentType,omitempty" yaml:"contentType,omitempty"`
}
