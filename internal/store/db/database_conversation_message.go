package db

import (
	"strings"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
)

type databaseConversationMessageRecord struct {
	ID             string    `gorm:"column:id;type:varchar(32);primaryKey"`
	ConversationID *string   `gorm:"column:conversation_id;type:varchar(32)"`
	Role           *string   `gorm:"column:role;type:varchar(32)"`
	Content        []byte    `gorm:"column:content;type:bytea"`
	Metadata       []byte    `gorm:"column:metadata;type:bytea"`
	StopReason     *string   `gorm:"column:stop_reason;type:varchar(32)"`
	Model          *string   `gorm:"column:model;type:varchar(128)"`
	Provider       *string   `gorm:"column:provider;type:varchar(128)"`
	ToolCallID     *string   `gorm:"column:tool_call_id;type:varchar(128)"`
	ToolName       *string   `gorm:"column:tool_name;type:varchar(128)"`
	Sequence       *int64    `gorm:"column:sequence"`
	CreatedAt      time.Time `gorm:"column:created_at;not null"`
	ModifiedAt     time.Time `gorm:"column:modified_at;not null"`
}

func (databaseConversationMessageRecord) TableName() string {
	return "conversation_messages"
}

func (self *databaseTransaction) ListConversationMessages(conversationId string, options *store.Option) ([]models.ConversationMessage, error) {
	records := make([]databaseConversationMessageRecord, 0)
	query := self.database.Model(&databaseConversationMessageRecord{}).Where("conversation_id = ?", strings.TrimSpace(conversationId)).Order("sequence ASC")
	query = applyOption(query, options)
	listError := query.Find(&records).Error
	if listError != nil {
		return nil, databaseError(listError)
	}
	messages := make([]models.ConversationMessage, 0, len(records))
	for _, record := range records {
		messages = append(messages, *conversationMessageRecordToModel(&record))
	}
	return messages, nil
}

func (self *databaseTransaction) CreateConversationMessage(message *models.ConversationMessage, options *store.Option) (*models.ConversationMessage, error) {
	if message == nil || message.ConversationID == nil || message.Role == nil || message.Content == nil {
		return nil, store.ErrInvalidOptions
	}
	record := modelToConversationMessageRecord(message)
	if strings.TrimSpace(record.ID) == "" {
		record.ID = security.NewULID()
	}
	sequence := int64(0)
	if message.Sequence != nil {
		sequence = *message.Sequence
	} else {
		rawQuery := self.database.Raw("SELECT COALESCE(MAX(sequence), 0) + 1 FROM conversation_messages WHERE conversation_id = ?", strings.TrimSpace(*message.ConversationID))
		if sequenceError := rawQuery.Scan(&sequence).Error; sequenceError != nil {
			return nil, databaseError(sequenceError)
		}
	}
	record.Sequence = &sequence
	record.CreatedAt = valueOrTime(message.CreatedAt)
	record.ModifiedAt = valueOrTime(message.ModifiedAt)
	createError := self.database.Create(record).Error
	if createError != nil {
		return nil, databaseError(createError)
	}
	return self.GetConversationMessage(record.ID, options)
}

func (self *databaseTransaction) GetConversationMessage(messageId string, options *store.Option) (*models.ConversationMessage, error) {
	record := &databaseConversationMessageRecord{}
	getError := self.database.Where("id = ?", strings.TrimSpace(messageId)).Take(record).Error
	if getError != nil {
		return nil, databaseError(getError)
	}
	return conversationMessageRecordToModel(record), nil
}

func (self *databaseTransaction) ModifyConversationMessage(messageId string, modifier func(*models.ConversationMessage) error, options *store.Option) (*models.ConversationMessage, error) {
	message, getError := self.GetConversationMessage(messageId, options)
	if getError != nil {
		return nil, getError
	}
	if modifierError := modifier(message); modifierError != nil {
		return nil, modifierError
	}
	record := modelToConversationMessageRecord(message)
	record.ID = strings.TrimSpace(messageId)
	record.ModifiedAt = time.Now().UTC()
	if message.CreatedAt != nil {
		record.CreatedAt = message.CreatedAt.UTC()
	}
	updateError := self.database.Model(&databaseConversationMessageRecord{}).Where("id = ?", record.ID).Updates(map[string]interface{}{
		"conversation_id": record.ConversationID,
		"role":            record.Role,
		"content":         record.Content,
		"metadata":        record.Metadata,
		"stop_reason":     record.StopReason,
		"model":           record.Model,
		"provider":        record.Provider,
		"tool_call_id":    record.ToolCallID,
		"tool_name":       record.ToolName,
		"sequence":        record.Sequence,
		"modified_at":     record.ModifiedAt,
	}).Error
	if updateError != nil {
		return nil, databaseError(updateError)
	}
	return self.GetConversationMessage(record.ID, options)
}

func (self *databaseTransaction) DeleteConversationMessage(messageId string, options *store.Option) error {
	result := self.database.Where("id = ?", strings.TrimSpace(messageId)).Delete(&databaseConversationMessageRecord{})
	if result.Error != nil {
		return databaseError(result.Error)
	}
	if result.RowsAffected == 0 {
		return store.ErrNotFound
	}
	return nil
}

func modelToConversationMessageRecord(message *models.ConversationMessage) *databaseConversationMessageRecord {
	content := []byte{}
	if message.Content != nil {
		content = *message.Content
	}
	metadata := []byte{}
	if message.Metadata != nil {
		metadata = *message.Metadata
	}
	role := ptrto.TrimmedString(stringValue(message.Role))
	stopReason := ptrto.TrimmedString(stringValue(message.StopReason))
	return &databaseConversationMessageRecord{
		ID:             strings.TrimSpace(message.ID),
		ConversationID: ptrto.TrimmedString(valueOrEmptyString(message.ConversationID)),
		Role:           role,
		Content:        content,
		Metadata:       metadata,
		StopReason:     stopReason,
		Model:          ptrto.TrimmedString(valueOrEmptyString(message.Model)),
		Provider:       ptrto.TrimmedString(valueOrEmptyString(message.Provider)),
		ToolCallID:     ptrto.TrimmedString(valueOrEmptyString(message.ToolCallID)),
		ToolName:       ptrto.TrimmedString(valueOrEmptyString(message.ToolName)),
		Sequence:       message.Sequence,
	}
}

func conversationMessageRecordToModel(record *databaseConversationMessageRecord) *models.ConversationMessage {
	content := make([]byte, len(record.Content))
	copy(content, record.Content)
	metadata := make([]byte, len(record.Metadata))
	copy(metadata, record.Metadata)
	role := models.Role(valueOrEmptyString(record.Role))
	stopReason := models.StopReason(valueOrEmptyString(record.StopReason))
	model := &models.ConversationMessage{
		ID:             record.ID,
		ConversationID: ptrto.TrimmedString(valueOrEmptyString(record.ConversationID)),
		Role:           nil,
		Content:        &content,
		Metadata:       &metadata,
		StopReason:     nil,
		Model:          ptrto.TrimmedString(valueOrEmptyString(record.Model)),
		Provider:       ptrto.TrimmedString(valueOrEmptyString(record.Provider)),
		ToolCallID:     ptrto.TrimmedString(valueOrEmptyString(record.ToolCallID)),
		ToolName:       ptrto.TrimmedString(valueOrEmptyString(record.ToolName)),
		Sequence:       record.Sequence,
		CreatedAt:      &record.CreatedAt,
		ModifiedAt:     &record.ModifiedAt,
	}
	if strings.TrimSpace(string(role)) != "" {
		model.Role = &role
	}
	if strings.TrimSpace(string(stopReason)) != "" {
		model.StopReason = &stopReason
	}
	return model
}

func stringValue[Type ~string](value *Type) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(string(*value))
}
