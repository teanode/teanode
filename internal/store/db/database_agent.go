package db

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
)

type databaseAgentRecord struct {
	ID            string    `gorm:"column:id;type:varchar(32);primaryKey"`
	Name          *string   `gorm:"column:name;type:varchar(256)"`
	Model         *string   `gorm:"column:model;type:varchar(128)"`
	Skills        []byte    `gorm:"column:skills;type:jsonb"`
	Tools         []byte    `gorm:"column:tools;type:jsonb"`
	Description   *string   `gorm:"column:description"`
	AvatarMediaID *string   `gorm:"column:avatar_media_id;type:varchar(32)"`
	CreatedAt     time.Time `gorm:"column:created_at;not null"`
	ModifiedAt    time.Time `gorm:"column:modified_at;not null"`
}

func (databaseAgentRecord) TableName() string {
	return "agents"
}

func (self *databaseTransaction) ListAgents(options *store.Option) ([]models.Agent, error) {
	records := make([]databaseAgentRecord, 0)
	query := applyOption(self.database.Model(&databaseAgentRecord{}).Order("id ASC"), options)
	listError := query.Find(&records).Error
	if listError != nil {
		return nil, databaseError(listError)
	}
	agents := make([]models.Agent, 0, len(records))
	for _, record := range records {
		agent, convertError := agentRecordToModel(&record)
		if convertError != nil {
			return nil, convertError
		}
		agents = append(agents, *agent)
	}
	return agents, nil
}

func (self *databaseTransaction) CreateAgent(agent *models.Agent, seedWorkspaceFiles []models.WorkspaceFile, options *store.Option) (*models.Agent, error) {
	if agent == nil {
		return nil, store.ErrInvalidOptions
	}
	record, recordError := modelToAgentRecord(agent)
	if recordError != nil {
		return nil, recordError
	}
	if strings.TrimSpace(record.ID) == "" {
		record.ID = security.NewULID()
	}
	record.CreatedAt = valueOrTime(agent.CreatedAt)
	record.ModifiedAt = valueOrTime(agent.ModifiedAt)
	createError := self.database.Create(record).Error
	if createError != nil {
		return nil, databaseError(createError)
	}
	if seedError := self.createSeedWorkspaceFiles(models.ScopeAgent, record.ID, seedWorkspaceFiles, options); seedError != nil {
		return nil, seedError
	}
	return self.GetAgent(record.ID, options)
}

func (self *databaseTransaction) GetAgent(agentId string, options *store.Option) (*models.Agent, error) {
	record := &databaseAgentRecord{}
	getError := self.database.Where("id = ?", strings.TrimSpace(agentId)).Take(record).Error
	if getError != nil {
		return nil, databaseError(getError)
	}
	return agentRecordToModel(record)
}

func (self *databaseTransaction) ModifyAgent(agentId string, modifier func(*models.Agent) error, options *store.Option) (*models.Agent, error) {
	agent, getError := self.GetAgent(agentId, options)
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
	record.ID = strings.TrimSpace(agentId)
	record.ModifiedAt = time.Now().UTC()
	if agent.CreatedAt != nil {
		record.CreatedAt = agent.CreatedAt.UTC()
	}
	saveError := self.database.Model(&databaseAgentRecord{}).Where("id = ?", record.ID).Updates(map[string]interface{}{
		"name":            record.Name,
		"model":           record.Model,
		"skills":          record.Skills,
		"tools":           record.Tools,
		"description":     record.Description,
		"avatar_media_id": record.AvatarMediaID,
		"modified_at":     record.ModifiedAt,
	}).Error
	if saveError != nil {
		return nil, databaseError(saveError)
	}
	return self.GetAgent(record.ID, options)
}

func (self *databaseTransaction) DeleteAgent(agentId string, options *store.Option) error {
	trimmedAgentId := strings.TrimSpace(agentId)
	deleteError := self.database.Where("scope = ? AND scope_id = ?", string(models.ScopeAgent), trimmedAgentId).Delete(&databaseWorkspaceFileRecord{}).Error
	if deleteError != nil {
		return databaseError(deleteError)
	}
	result := self.database.Where("id = ?", trimmedAgentId).Delete(&databaseAgentRecord{})
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
		ID:            strings.TrimSpace(agent.ID),
		Name:          ptrto.TrimmedString(valueOrEmptyString(agent.Name)),
		Model:         ptrto.TrimmedString(valueOrEmptyString(agent.Model)),
		Description:   ptrto.TrimmedString(valueOrEmptyString(agent.Description)),
		AvatarMediaID: ptrto.TrimmedString(valueOrEmptyString(agent.AvatarMediaID)),
	}
	if agent.Skills != nil {
		skillsJSON, marshalError := json.Marshal(*agent.Skills)
		if marshalError != nil {
			return nil, marshalError
		}
		record.Skills = skillsJSON
	}
	if agent.Tools != nil {
		toolsJSON, marshalError := json.Marshal(*agent.Tools)
		if marshalError != nil {
			return nil, marshalError
		}
		record.Tools = toolsJSON
	}
	return record, nil
}

func agentRecordToModel(record *databaseAgentRecord) (*models.Agent, error) {
	agent := &models.Agent{
		ID:            record.ID,
		Name:          ptrto.TrimmedString(valueOrEmptyString(record.Name)),
		Model:         ptrto.TrimmedString(valueOrEmptyString(record.Model)),
		Description:   ptrto.TrimmedString(valueOrEmptyString(record.Description)),
		AvatarMediaID: ptrto.TrimmedString(valueOrEmptyString(record.AvatarMediaID)),
		CreatedAt:     &record.CreatedAt,
		ModifiedAt:    &record.ModifiedAt,
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

func valueOrEmptyString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
