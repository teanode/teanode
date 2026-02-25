package models

import "time"

type Skill struct {
	ID                string                    `json:"id,omitempty" yaml:"id,omitempty"`
	Name              *string                   `json:"name,omitempty" yaml:"name,omitempty"`
	Description       *string                   `json:"description,omitempty" yaml:"description,omitempty"`
	Version           *string                   `json:"version,omitempty" yaml:"version,omitempty"`
	RuntimeMinVersion *string                   `json:"runtimeMinVersion,omitempty" yaml:"runtimeMinVersion,omitempty"`
	HTTPAuth          *map[string]interface{}   `json:"httpAuth,omitempty" yaml:"httpAuth,omitempty"`
	Tools             *[]map[string]interface{} `json:"tools,omitempty" yaml:"tools,omitempty"`
	Enabled           *bool                     `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	Source            *string                   `json:"source,omitempty" yaml:"source,omitempty"`
	Publisher         *string                   `json:"publisher,omitempty" yaml:"publisher,omitempty"`
	Metadata          *map[string]interface{}   `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	Prompt            *string                   `json:"prompt,omitempty" yaml:"prompt,omitempty"`
	CreatedAt         *time.Time                `json:"createdAt,omitempty" yaml:"createdAt,omitempty"`
	ModifiedAt        *time.Time                `json:"modifiedAt,omitempty" yaml:"modifiedAt,omitempty"`
}
