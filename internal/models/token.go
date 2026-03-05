package models

import "time"

type Token struct {
	ID         string     `json:"id,omitempty" yaml:"id,omitempty"`
	CreatedAt  *time.Time `json:"createdAt,omitempty" yaml:"createdAt,omitempty"`
	ModifiedAt *time.Time `json:"modifiedAt,omitempty" yaml:"modifiedAt,omitempty"`

	UserID        *string    `json:"userId,omitempty" yaml:"userId,omitempty"`
	Token         *string    `json:"token,omitempty" yaml:"token,omitempty"`
	LastUsedAt    *time.Time `json:"lastUsedAt,omitempty" yaml:"lastUsedAt,omitempty"`
	RemoteAddress *string    `json:"remoteAddress,omitempty" yaml:"remoteAddress,omitempty"`
	UserAgent     *string    `json:"userAgent,omitempty" yaml:"userAgent,omitempty"`
}
