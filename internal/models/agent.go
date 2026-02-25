package models

import "time"

type Agent struct {
	ID         string     `json:"id,omitempty" yaml:"id,omitempty"`
	CreatedAt  *time.Time `json:"createdAt,omitempty" yaml:"createdAt,omitempty"`
	ModifiedAt *time.Time `json:"modifiedAt,omitempty" yaml:"modifiedAt,omitempty"`

	Name          *string   `json:"name,omitempty" yaml:"name,omitempty"`
	Model         *string   `json:"model,omitempty" yaml:"model,omitempty"`
	Skills        *[]string `json:"skills,omitempty" yaml:"skills,omitempty"`
	Tools         *[]string `json:"tools,omitempty" yaml:"tools,omitempty"`
	Description   *string     `json:"description,omitempty" yaml:"description,omitempty"`
	AvatarMediaID *string     `json:"avatarMediaId,omitempty" yaml:"avatarMediaId,omitempty"`
	SummarizedAt  *time.Time  `json:"summarizedAt,omitempty" yaml:"summarizedAt,omitempty"`
}
