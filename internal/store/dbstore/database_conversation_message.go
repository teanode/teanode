package dbstore

import (
	"context"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
	"github.com/teanode/teanode/internal/util/valueor"
)

type databaseConversationMessageRecord struct {
	ID                string    `gorm:"column:id;type:varchar(32);primaryKey"`
	ConversationID    *string   `gorm:"column:conversation_id;type:varchar(32)"`
	Role              *string   `gorm:"column:role;type:varchar(32)"`
	Content           []byte    `gorm:"column:content;type:bytea"`
	ToolCalls         []byte    `gorm:"column:tool_calls;type:bytea"`
	Usage             []byte    `gorm:"column:usage;type:bytea"`
	Metadata          []byte    `gorm:"column:metadata;type:bytea"`
	StopReason        *string   `gorm:"column:stop_reason;type:varchar(32)"`
	ProviderModelName *string   `gorm:"column:model;type:varchar(128)"`
	ProviderName      *string   `gorm:"column:provider;type:varchar(128)"`
	ToolCallID        *string   `gorm:"column:tool_call_id;type:varchar(128)"`
	ToolName          *string   `gorm:"column:tool_name;type:varchar(128)"`
	CreatedAt         time.Time `gorm:"column:created_at;not null"`
	ModifiedAt        time.Time `gorm:"column:modified_at;not null"`
}

func (databaseConversationMessageRecord) TableName() string {
	return "conversation_messages"
}

func (self *databaseTransaction) ListConversationMessages(ctx context.Context, conversationId string, options *store.Option) ([]*models.ConversationMessage, error) {
	records := make([]databaseConversationMessageRecord, 0)
	query := self.database.Model(&databaseConversationMessageRecord{}).Where("conversation_id = ?", conversationId).Order("id ASC")
	query = applyOption(query, options)
	listError := query.Find(&records).Error
	if listError != nil {
		return nil, databaseError(listError)
	}
	messages := make([]*models.ConversationMessage, 0, len(records))
	for _, record := range records {
		messages = append(messages, conversationMessageRecordToModel(&record))
	}
	return messages, nil
}

func (self *databaseTransaction) CreateConversationMessage(ctx context.Context, message *models.ConversationMessage, options *store.Option) (*models.ConversationMessage, error) {
	if message == nil || message.ConversationID == nil || message.Role == nil || len(message.Content) == 0 {
		return nil, store.ErrInvalidOptions
	}
	record := modelToConversationMessageRecord(message)
	if record.ID == "" {
		record.ID = security.NewULID()
	}
	now := ptrto.TimeNowInLocal()
	record.CreatedAt = *now
	record.ModifiedAt = *now
	createError := self.database.Create(record).Error
	if createError != nil {
		return nil, databaseError(createError)
	}
	return self.getConversationMessage(record.ID)
}

func (self *databaseTransaction) getConversationMessage(messageId string) (*models.ConversationMessage, error) {
	record := &databaseConversationMessageRecord{}
	getError := self.database.Where("id = ?", messageId).Take(record).Error
	if getError != nil {
		return nil, databaseError(getError)
	}
	return conversationMessageRecordToModel(record), nil
}

func modelToConversationMessageRecord(message *models.ConversationMessage) *databaseConversationMessageRecord {
	role := ptrto.TrimmedString(stringValue(message.Role))
	stopReason := ptrto.TrimmedString(stringValue(message.StopReason))
	return &databaseConversationMessageRecord{
		ID:                message.ID,
		ConversationID:    ptrto.TrimmedString(message.GetConversationID()),
		Role:              role,
		Content:           []byte(message.Content),
		ToolCalls:         []byte(message.ToolCalls),
		Usage:             []byte(message.Usage),
		Metadata:          []byte(message.Metadata),
		StopReason:        stopReason,
		ProviderModelName: ptrto.TrimmedString(message.GetProviderModelName()),
		ProviderName:      ptrto.TrimmedString(message.GetProviderName()),
		ToolCallID:        ptrto.TrimmedString(message.GetToolCallID()),
		ToolName:          ptrto.TrimmedString(message.GetToolName()),
	}
}

func conversationMessageRecordToModel(record *databaseConversationMessageRecord) *models.ConversationMessage {
	role := models.Role(valueor.Zero(record.Role))
	stopReason := models.StopReason(valueor.Zero(record.StopReason))
	message := &models.ConversationMessage{
		ID:                record.ID,
		ConversationID:    ptrto.TrimmedString(valueor.Zero(record.ConversationID)),
		Role:              nil,
		Content:           copyBytes(record.Content),
		ToolCalls:         copyBytes(record.ToolCalls),
		Usage:             copyBytes(record.Usage),
		Metadata:          copyBytes(record.Metadata),
		StopReason:        nil,
		ProviderModelName: ptrto.TrimmedString(valueor.Zero(record.ProviderModelName)),
		ProviderName:      ptrto.TrimmedString(valueor.Zero(record.ProviderName)),
		ToolCallID:        ptrto.TrimmedString(valueor.Zero(record.ToolCallID)),
		ToolName:          ptrto.TrimmedString(valueor.Zero(record.ToolName)),
		CreatedAt:         &record.CreatedAt,
		ModifiedAt:        &record.ModifiedAt,
	}
	if string(role) != "" {
		message.Role = &role
	}
	if string(stopReason) != "" {
		message.StopReason = &stopReason
	}
	return message
}

func copyBytes(source []byte) []byte {
	if len(source) == 0 {
		return nil
	}
	destination := make([]byte, len(source))
	copy(destination, source)
	return destination
}

func stringValue[Type ~string](value *Type) string {
	if value == nil {
		return ""
	}
	return string(*value)
}
