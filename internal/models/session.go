package models

import "time"

type Session struct {
	ID            string     `json:"id,omitempty" yaml:"id,omitempty"`
	UserID        *string    `json:"userId,omitempty" yaml:"userId,omitempty"`
	UserAgent     *string    `json:"userAgent,omitempty" yaml:"userAgent,omitempty"`
	RemoteAddress *string    `json:"remoteAddress,omitempty" yaml:"remoteAddress,omitempty"`
	ExpiresAt     *time.Time `json:"expiresAt,omitempty" yaml:"expiresAt,omitempty"`
	CreatedAt     *time.Time `json:"createdAt,omitempty" yaml:"createdAt,omitempty"`
	ModifiedAt    *time.Time `json:"modifiedAt,omitempty" yaml:"modifiedAt,omitempty"`
}
