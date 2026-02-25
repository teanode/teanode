package models

import "time"

type User struct {
	ID         string     `json:"id,omitempty" yaml:"id,omitempty"`
	CreatedAt  *time.Time `json:"createdAt,omitempty" yaml:"createdAt,omitempty"`
	ModifiedAt *time.Time `json:"modifiedAt,omitempty" yaml:"modifiedAt,omitempty"`

	Username       *string `json:"username,omitempty" yaml:"username,omitempty"`
	Password       *string `json:"password,omitempty" yaml:"password,omitempty"`
	Admin          *bool   `json:"admin,omitempty" yaml:"admin,omitempty"`
	DefaultAgentID *string `json:"defaultAgentId,omitempty" yaml:"defaultAgentId,omitempty"`
	TelegramChatID *int64  `json:"telegramChatId,omitempty" yaml:"telegramChatId,omitempty"`
	DiscordUserID  *string `json:"discordUserId,omitempty" yaml:"discordUserId,omitempty"`
	AvatarMediaID  *string    `json:"avatarMediaId,omitempty" yaml:"avatarMediaId,omitempty"`
	Description    *string    `json:"description,omitempty" yaml:"description,omitempty"`
	SummarizedAt   *time.Time `json:"summarizedAt,omitempty" yaml:"summarizedAt,omitempty"`
}
