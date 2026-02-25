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

type databaseSkillRecord struct {
	ID         string    `gorm:"column:id;type:varchar(32);primaryKey"`
	Name       *string   `gorm:"column:name;type:varchar(256)"`
	Version    *string   `gorm:"column:version;type:varchar(128)"`
	Source     *string   `gorm:"column:source;type:varchar(256)"`
	Metadata   []byte    `gorm:"column:metadata;type:jsonb"`
	Prompt     *string   `gorm:"column:prompt"`
	CreatedAt  time.Time `gorm:"column:created_at;not null"`
	ModifiedAt time.Time `gorm:"column:modified_at;not null"`
}

func (databaseSkillRecord) TableName() string {
	return "skills"
}

func (self *databaseTransaction) ListSkills(ctx context.Context, options *store.Option) ([]*models.Skill, error) {
	records := make([]databaseSkillRecord, 0)
	query := applyOption(self.database.Model(&databaseSkillRecord{}).Order("id ASC"), options)
	listError := query.Find(&records).Error
	if listError != nil {
		return nil, databaseError(listError)
	}
	skills := make([]*models.Skill, 0, len(records))
	for _, record := range records {
		skill, convertError := skillRecordToModel(&record)
		if convertError != nil {
			return nil, convertError
		}
		skills = append(skills, skill)
	}
	return skills, nil
}

func (self *databaseTransaction) CreateSkill(ctx context.Context, skill *models.Skill, options *store.Option) (*models.Skill, error) {
	if skill == nil {
		return nil, store.ErrInvalidOptions
	}
	record, recordError := modelToSkillRecord(skill)
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
	return self.GetSkill(ctx, record.ID, options)
}

func (self *databaseTransaction) GetSkill(ctx context.Context, skillId string, options *store.Option) (*models.Skill, error) {
	record := &databaseSkillRecord{}
	getError := self.database.Where("id = ?", skillId).Take(record).Error
	if getError != nil {
		return nil, databaseError(getError)
	}
	return skillRecordToModel(record)
}

func (self *databaseTransaction) ModifySkill(ctx context.Context, skillId string, modifier func(*models.Skill) error, options *store.Option) (*models.Skill, error) {
	skill, getError := self.GetSkill(ctx, skillId, options)
	if getError != nil {
		return nil, getError
	}
	if modifierError := modifier(skill); modifierError != nil {
		return nil, modifierError
	}
	record, recordError := modelToSkillRecord(skill)
	if recordError != nil {
		return nil, recordError
	}
	record.ID = skillId
	record.ModifiedAt = *ptrto.TimeNowInLocal()
	updateError := self.database.Model(&databaseSkillRecord{}).Where("id = ?", record.ID).Updates(map[string]interface{}{
		"name":        record.Name,
		"version":     record.Version,
		"source":      record.Source,
		"metadata":    record.Metadata,
		"prompt":      record.Prompt,
		"modified_at": record.ModifiedAt,
	}).Error
	if updateError != nil {
		return nil, databaseError(updateError)
	}
	return self.GetSkill(ctx, record.ID, options)
}

func (self *databaseTransaction) DeleteSkill(ctx context.Context, skillId string, options *store.Option) error {
	result := self.database.Where("id = ?", skillId).Delete(&databaseSkillRecord{})
	if result.Error != nil {
		return databaseError(result.Error)
	}
	if result.RowsAffected == 0 {
		return store.ErrNotFound
	}
	return nil
}

func modelToSkillRecord(skill *models.Skill) (*databaseSkillRecord, error) {
	record := &databaseSkillRecord{
		ID:      skill.ID,
		Name:    ptrto.TrimmedString(skill.GetName()),
		Version: ptrto.TrimmedString(skill.GetVersion()),
		Source:  ptrto.TrimmedString(skill.GetSource()),
		Prompt:  ptrto.TrimmedString(skill.GetPrompt()),
	}
	metadata := map[string]interface{}{}
	if value := skill.GetDescription(); value != "" {
		metadata["description"] = value
	}
	if value := skill.GetRuntimeMinVersion(); value != "" {
		metadata["runtimeMinVersion"] = value
	}
	if skill.AuthenticationProfiles != nil && len(*skill.AuthenticationProfiles) > 0 {
		metadata["httpAuth"] = *skill.AuthenticationProfiles
	}
	if skill.Tools != nil && len(*skill.Tools) > 0 {
		metadata["tools"] = *skill.Tools
	}
	if skill.Enabled != nil {
		metadata["enabled"] = *skill.Enabled
	}
	if value := skill.GetPublisher(); value != "" {
		metadata["publisher"] = value
	}
	if len(metadata) > 0 {
		metadataJSON, marshalError := json.Marshal(metadata)
		if marshalError != nil {
			return nil, marshalError
		}
		record.Metadata = metadataJSON
	}
	return record, nil
}

func skillRecordToModel(record *databaseSkillRecord) (*models.Skill, error) {
	skill := &models.Skill{
		ID:         record.ID,
		Name:       ptrto.TrimmedString(valueor.Zero(record.Name)),
		Version:    ptrto.TrimmedString(valueor.Zero(record.Version)),
		Source:     ptrto.TrimmedString(valueor.Zero(record.Source)),
		Prompt:     ptrto.TrimmedString(valueor.Zero(record.Prompt)),
		CreatedAt:  &record.CreatedAt,
		ModifiedAt: &record.ModifiedAt,
	}
	if len(record.Metadata) > 0 {
		metadata := map[string]interface{}{}
		if unmarshalError := json.Unmarshal(record.Metadata, &metadata); unmarshalError != nil {
			return nil, unmarshalError
		}
		if value, ok := metadata["description"].(string); ok {
			skill.Description = ptrto.TrimmedString(value)
		}
		if value, ok := metadata["runtimeMinVersion"].(string); ok {
			skill.RuntimeMinVersion = ptrto.TrimmedString(value)
		}
		if value, exists := metadata["httpAuth"]; exists {
			httpAuthData, marshalError := json.Marshal(value)
			if marshalError != nil {
				return nil, marshalError
			}
			httpAuth := map[string]models.SkillAuthenticationProfiles{}
			if unmarshalError := json.Unmarshal(httpAuthData, &httpAuth); unmarshalError != nil {
				return nil, unmarshalError
			}
			skill.AuthenticationProfiles = &httpAuth
		}
		if value, exists := metadata["tools"]; exists {
			toolsData, marshalError := json.Marshal(value)
			if marshalError != nil {
				return nil, marshalError
			}
			tools := []*models.SkillTool{}
			if unmarshalError := json.Unmarshal(toolsData, &tools); unmarshalError != nil {
				return nil, unmarshalError
			}
			skill.Tools = &tools
		}
		if value, ok := metadata["enabled"].(bool); ok {
			skill.Enabled = &value
		}
		if value, ok := metadata["publisher"].(string); ok {
			skill.Publisher = ptrto.TrimmedString(value)
		}
	}
	return skill, nil
}
