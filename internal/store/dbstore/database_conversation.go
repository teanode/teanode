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

type databaseConversationRecord struct {
	ID         string    `gorm:"column:id;type:varchar(32);primaryKey"`
	UserID     *string   `gorm:"column:user_id;type:varchar(32)"`
	AgentID    *string   `gorm:"column:agent_id;type:varchar(32)"`
	Default    *bool     `gorm:"column:default"`
	Title      *string   `gorm:"column:title;type:varchar(256)"`
	Summary      *string    `gorm:"column:summary"`
	SummarizedAt *time.Time `gorm:"column:summarized_at"`
	CreatedAt    time.Time  `gorm:"column:created_at;not null"`
	ModifiedAt   time.Time  `gorm:"column:modified_at;not null"`
}

func (databaseConversationRecord) TableName() string {
	return "conversations"
}

func (self *databaseTransaction) ListConversations(ctx context.Context, listOptions store.ConversationListOptions, options *store.Option) ([]*models.Conversation, error) {
	query := self.database.Model(&databaseConversationRecord{})
	if listOptions.UserID != nil {
		query = query.Where("user_id = ?", *listOptions.UserID)
	}
	if listOptions.AgentID != nil {
		query = query.Where("agent_id = ?", *listOptions.AgentID)
	}
	if listOptions.Default != nil {
		query = query.Where("\"default\" = ?", *listOptions.Default)
	}
	query = applyOption(query.Order("modified_at DESC, created_at DESC"), options)
	records := make([]databaseConversationRecord, 0)
	listError := query.Find(&records).Error
	if listError != nil {
		return nil, databaseError(listError)
	}
	conversations := make([]*models.Conversation, 0, len(records))
	for _, record := range records {
		conversations = append(conversations, conversationRecordToModel(&record))
	}
	return conversations, nil
}

func (self *databaseTransaction) CreateConversation(ctx context.Context, conversation *models.Conversation, options *store.Option) (*models.Conversation, error) {
	if conversation == nil {
		return nil, store.ErrInvalidOptions
	}
	record := modelToConversationRecord(conversation)
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
	return self.GetConversation(ctx, record.ID, options)
}

func (self *databaseTransaction) GetConversation(ctx context.Context, conversationId string, options *store.Option) (*models.Conversation, error) {
	record := &databaseConversationRecord{}
	getError := self.database.Where("id = ?", conversationId).Take(record).Error
	if getError != nil {
		return nil, databaseError(getError)
	}
	return conversationRecordToModel(record), nil
}

func (self *databaseTransaction) FindDefaultConversation(ctx context.Context, userId string, agentId string, options *store.Option) (*models.Conversation, error) {
	record := &databaseConversationRecord{}
	getError := self.database.Where("user_id = ? AND agent_id = ? AND \"default\" = ?", userId, agentId, true).Order("modified_at DESC").Take(record).Error
	if getError != nil {
		return nil, databaseError(getError)
	}
	return conversationRecordToModel(record), nil
}

func (self *databaseTransaction) ModifyConversation(ctx context.Context, conversationId string, modifier func(*models.Conversation) error, options *store.Option) (*models.Conversation, error) {
	conversation, getError := self.GetConversation(ctx, conversationId, options)
	if getError != nil {
		return nil, getError
	}
	if modifierError := modifier(conversation); modifierError != nil {
		return nil, modifierError
	}
	record := modelToConversationRecord(conversation)
	record.ID = conversationId
	record.ModifiedAt = *ptrto.TimeNowInLocal()
	updateError := self.database.Model(&databaseConversationRecord{}).Where("id = ?", record.ID).Updates(map[string]interface{}{
		"user_id":     record.UserID,
		"agent_id":    record.AgentID,
		"default":     record.Default,
		"title":       record.Title,
		"summary":        record.Summary,
		"summarized_at":  record.SummarizedAt,
		"modified_at":    record.ModifiedAt,
	}).Error
	if updateError != nil {
		return nil, databaseError(updateError)
	}
	return self.GetConversation(ctx, record.ID, options)
}

func (self *databaseTransaction) DeleteConversation(ctx context.Context, conversationId string, options *store.Option) error {
	result := self.database.Where("id = ?", conversationId).Delete(&databaseConversationRecord{})
	if result.Error != nil {
		return databaseError(result.Error)
	}
	if result.RowsAffected == 0 {
		return store.ErrNotFound
	}
	return nil
}

func modelToConversationRecord(conversation *models.Conversation) *databaseConversationRecord {
	return &databaseConversationRecord{
		ID:           conversation.ID,
		UserID:       ptrto.TrimmedString(conversation.GetUserID()),
		AgentID:      ptrto.TrimmedString(conversation.GetAgentID()),
		Default:      conversation.Default,
		Title:        ptrto.TrimmedString(conversation.GetTitle()),
		Summary:      ptrto.TrimmedString(conversation.GetSummary()),
		SummarizedAt: conversation.SummarizedAt,
	}
}

func conversationRecordToModel(record *databaseConversationRecord) *models.Conversation {
	return &models.Conversation{
		ID:           record.ID,
		UserID:       ptrto.TrimmedString(valueor.Zero(record.UserID)),
		AgentID:      ptrto.TrimmedString(valueor.Zero(record.AgentID)),
		Default:      record.Default,
		Title:        ptrto.TrimmedString(valueor.Zero(record.Title)),
		Summary:      ptrto.TrimmedString(valueor.Zero(record.Summary)),
		SummarizedAt: record.SummarizedAt,
		CreatedAt:    &record.CreatedAt,
		ModifiedAt:   &record.ModifiedAt,
	}
}
