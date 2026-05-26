package dbstore

import (
	"context"
	"encoding/json"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
	"github.com/teanode/teanode/internal/util/valueor"
)

type databaseAgentRecord struct {
	ID                string     `gorm:"column:id;type:varchar(32);primaryKey"`
	Name              *string    `gorm:"column:name;type:varchar(256)"`
	ProviderModelName *string    `gorm:"column:model;type:varchar(128)"`
	Skills            []byte     `gorm:"column:skills;type:jsonb"`
	Tools             []byte     `gorm:"column:tools;type:jsonb"`
	Description       *string    `gorm:"column:description"`
	AvatarMediaID     *string    `gorm:"column:avatar_media_id;type:varchar(32)"`
	SummarizedAt      *time.Time `gorm:"column:summarized_at"`
	CreatedAt         time.Time  `gorm:"column:created_at;not null"`
	ModifiedAt        time.Time  `gorm:"column:modified_at;not null"`
}

func (self databaseAgentRecord) TableName() string {
	return "agents"
}

func (self *databaseTransaction) ListAgents(ctx context.Context, options *store.Option) ([]*models.Agent, error) {
	records := make([]databaseAgentRecord, 0)
	query := applyOption(self.database.Model(&databaseAgentRecord{}).Order("id ASC"), options)
	listError := query.Find(&records).Error
	if listError != nil {
		return nil, databaseError(listError)
	}
	agents := make([]*models.Agent, 0, len(records))
	for _, record := range records {
		agent, convertError := agentRecordToModel(&record)
		if convertError != nil {
			return nil, convertError
		}
		agents = append(agents, agent)
	}
	return agents, nil
}

func (self *databaseTransaction) CreateAgent(ctx context.Context, agent *models.Agent, seedWorkspaceFiles []models.WorkspaceFile, options *store.Option) (*models.Agent, error) {
	if agent == nil {
		return nil, store.ErrInvalidOptions
	}
	record, recordError := modelToAgentRecord(agent)
	if recordError != nil {
		return nil, recordError
	}
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
	if seedError := self.createSeedWorkspaceFiles(ctx, models.ScopeAgent, record.ID, seedWorkspaceFiles, options); seedError != nil {
		return nil, seedError
	}
	return self.GetAgent(ctx, record.ID, options)
}

func (self *databaseTransaction) GetAgent(ctx context.Context, agentId string, options *store.Option) (*models.Agent, error) {
	record := &databaseAgentRecord{}
	getError := self.database.Where("id = ?", agentId).Take(record).Error
	if getError != nil {
		return nil, databaseError(getError)
	}
	return agentRecordToModel(record)
}

func (self *databaseTransaction) ModifyAgent(ctx context.Context, agentId string, modifier func(*models.Agent) error, options *store.Option) (*models.Agent, error) {
	agent, getError := self.GetAgent(ctx, agentId, options)
	if getError != nil {
		return nil, getError
	}
	if modifierError := modifier(agent); modifierError != nil {
		return nil, modifierError
	}
	record, recordError := modelToAgentRecord(agent)
	if recordError != nil {
		return nil, recordError
	}
	record.ID = agentId
	record.ModifiedAt = *ptrto.TimeNowInLocal()
	saveError := self.database.Model(&databaseAgentRecord{}).Where("id = ?", record.ID).Updates(map[string]interface{}{
		"name":            record.Name,
		"model":           record.ProviderModelName,
		"skills":          record.Skills,
		"tools":           record.Tools,
		"description":     record.Description,
		"avatar_media_id": record.AvatarMediaID,
		"summarized_at":   record.SummarizedAt,
		"modified_at":     record.ModifiedAt,
	}).Error
	if saveError != nil {
		return nil, databaseError(saveError)
	}
	return self.GetAgent(ctx, record.ID, options)
}

func (self *databaseTransaction) DeleteAgent(ctx context.Context, agentId string, options *store.Option) error {
	deleteWorkspaceFileError := self.database.Where("scope = ? AND scope_id = ?", string(models.ScopeAgent), agentId).Delete(&databaseWorkspaceFileRecord{}).Error
	if deleteWorkspaceFileError != nil {
		return databaseError(deleteWorkspaceFileError)
	}
	result := self.database.Where("id = ?", agentId).Delete(&databaseAgentRecord{})
	if result.Error != nil {
		return databaseError(result.Error)
	}
	if result.RowsAffected == 0 {
		return store.ErrNotFound
	}
	return nil
}

func modelToAgentRecord(agent *models.Agent) (*databaseAgentRecord, error) {
	record := &databaseAgentRecord{
		ID:                agent.ID,
		Name:              ptrto.TrimmedString(agent.GetName()),
		ProviderModelName: ptrto.TrimmedString(agent.GetProviderModelName()),
		Description:       ptrto.TrimmedString(agent.GetDescription()),
		AvatarMediaID:     ptrto.TrimmedString(agent.GetAvatarMediaID()),
		SummarizedAt:      agent.SummarizedAt,
	}
	if agent.Skills != nil {
		skillsJson, marshalError := json.Marshal(*agent.Skills)
		if marshalError != nil {
			return nil, marshalError
		}
		record.Skills = skillsJson
	}
	if agent.Tools != nil {
		toolsJson, marshalError := json.Marshal(*agent.Tools)
		if marshalError != nil {
			return nil, marshalError
		}
		record.Tools = toolsJson
	}
	return record, nil
}

func agentRecordToModel(record *databaseAgentRecord) (*models.Agent, error) {
	agent := &models.Agent{
		ID:                record.ID,
		Name:              ptrto.TrimmedString(valueor.Zero(record.Name)),
		ProviderModelName: ptrto.TrimmedString(valueor.Zero(record.ProviderModelName)),
		Description:       ptrto.TrimmedString(valueor.Zero(record.Description)),
		AvatarMediaID:     ptrto.TrimmedString(valueor.Zero(record.AvatarMediaID)),
		SummarizedAt:      record.SummarizedAt,
		CreatedAt:         &record.CreatedAt,
		ModifiedAt:        &record.ModifiedAt,
	}
	if len(record.Skills) > 0 {
		skills := make([]string, 0)
		if unmarshalError := json.Unmarshal(record.Skills, &skills); unmarshalError != nil {
			return nil, unmarshalError
		}
		agent.Skills = &skills
	}
	if len(record.Tools) > 0 {
		tools := make([]string, 0)
		if unmarshalError := json.Unmarshal(record.Tools, &tools); unmarshalError != nil {
			return nil, unmarshalError
		}
		agent.Tools = &tools
	}
	return agent, nil
}
