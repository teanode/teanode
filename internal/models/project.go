package models

import "time"

type Project struct {
	ID         string     `json:"id,omitempty" yaml:"id,omitempty"`
	CreatedAt  *time.Time `json:"createdAt,omitempty" yaml:"createdAt,omitempty"`
	ModifiedAt *time.Time `json:"modifiedAt,omitempty" yaml:"modifiedAt,omitempty"`

	Name        *string `json:"name,omitempty" yaml:"name,omitempty"`
	Description *string `json:"description,omitempty" yaml:"description,omitempty"`
}
