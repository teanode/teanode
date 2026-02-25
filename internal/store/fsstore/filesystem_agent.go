package fsstore

import (
	"context"
	"fmt"
	"os"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
	"github.com/teanode/teanode/internal/util/timeutil"
)

func (self *fileSystemTransaction) ListAgents(ctx context.Context, options *store.Option) ([]*models.Agent, error) {
	return self.listAgents(options)
}

func (self *fileSystemTransaction) CreateAgent(ctx context.Context, agent *models.Agent, seedWorkspaceFiles []models.WorkspaceFile, options *store.Option) (*models.Agent, error) {
	return self.createAgent(ctx, agent, seedWorkspaceFiles, options)
}

func (self *fileSystemTransaction) GetAgent(ctx context.Context, agentId string, options *store.Option) (*models.Agent, error) {
	return self.getAgent(agentId, options)
}

func (self *fileSystemTransaction) ModifyAgent(ctx context.Context, agentId string, modifier func(*models.Agent) error, options *store.Option) (*models.Agent, error) {
	return self.modifyAgent(ctx, agentId, modifier, options)
}

func (self *fileSystemTransaction) DeleteAgent(ctx context.Context, agentId string, options *store.Option) error {
	return self.deleteAgent(agentId, options)
}
func (self *fileSystemTransaction) listAgents(options *store.Option) ([]*models.Agent, error) {
	agentConfigurations, err := self.listAgentRecords()
	if err != nil {
		return nil, err
	}
	agents := make([]*models.Agent, 0, len(agentConfigurations))
	for _, agentConfiguration := range applyOffsetLimitAgentConfig(agentConfigurations, options) {
		agent := agentConfigToModel(agentConfiguration)
		agents = append(agents, &agent)
	}
	return agents, nil
}

func (self *fileSystemTransaction) createAgent(ctx context.Context, agent *models.Agent, seedWorkspaceFiles []models.WorkspaceFile, options *store.Option) (*models.Agent, error) {
	if agent == nil {
		return nil, fmt.Errorf("agent is required")
	}
	agentId := agent.ID
	if agentId == "" {
		agentId = security.NewULID()
	}
	if _, err := os.Stat(self.agentConfigFilename(agentId)); err == nil {
		return nil, store.ErrAlreadyExists
	}
	configuration := modelToAgentConfig(*agent)
	if err := self.saveAgentRecord(agentId, &configuration); err != nil {
		return nil, err
	}
	for _, file := range seedWorkspaceFiles {
		copyFile := file
		scope := models.ScopeAgent
		copyFile.Scope = &scope
		copyFile.ScopeID = &agentId
		if _, err := self.CreateWorkspaceFile(ctx, &copyFile, options); err != nil {
			return nil, err
		}
	}
	createdAgent := agentConfigToModel(configuration)
	createdAgent.ID = agentId
	return &createdAgent, nil
}

func (self *fileSystemTransaction) getAgent(agentId string, options *store.Option) (*models.Agent, error) {
	configuration, err := self.loadAgentRecord(agentId)
	if err != nil {
		return nil, err
	}
	if configuration == nil || configuration.ID == "" {
		return nil, store.ErrNotFound
	}
	agent := agentConfigToModel(*configuration)
	return &agent, nil
}

func (self *fileSystemTransaction) modifyAgent(ctx context.Context, agentId string, modifier func(*models.Agent) error, options *store.Option) (*models.Agent, error) {
	agent, err := self.GetAgent(ctx, agentId, options)
	if err != nil {
		return nil, err
	}
	if err := modifier(agent); err != nil {
		return nil, err
	}
	configuration := modelToAgentConfig(*agent)
	if err := self.saveAgentRecord(agentId, &configuration); err != nil {
		return nil, err
	}
	result := agentConfigToModel(configuration)
	result.ID = agentId
	return &result, nil
}

func (self *fileSystemTransaction) deleteAgent(agentId string, options *store.Option) error {
	return self.deleteAgentDirectories(agentId)
}

func agentConfigToModel(configuration storeAgentRecord) models.Agent {
	agent := models.Agent{
		ID:            configuration.ID,
		Name:          ptrto.TrimmedString(configuration.Name),
		Model:         ptrto.TrimmedString(configuration.Model),
		Skills:        ptrto.TrimmedStrings(configuration.Skills),
		Tools:         ptrto.TrimmedStrings(configuration.Tools),
		Description:   ptrto.TrimmedString(configuration.Description),
		AvatarMediaID: ptrto.TrimmedString(configuration.AvatarMediaID),
	}
	if !configuration.SummarizedAt.Time.IsZero() {
		agent.SummarizedAt = &configuration.SummarizedAt.Time
	}
	return agent
}

func modelToAgentConfig(agent models.Agent) storeAgentRecord {
	record := storeAgentRecord{
		ID:            agent.ID,
		Name:          agent.GetName(),
		Model:         agent.GetModel(),
		Skills:        sliceValue(agent.Skills),
		Tools:         sliceValue(agent.Tools),
		Description:   agent.GetDescription(),
		AvatarMediaID: agent.GetAvatarMediaID(),
	}
	if agent.SummarizedAt != nil {
		record.SummarizedAt = timeutil.Timestamp{Time: *agent.SummarizedAt}
	}
	return record
}

func applyOffsetLimitAgentConfig(values []storeAgentRecord, options *store.Option) []storeAgentRecord {
	if options == nil {
		return values
	}
	offset := int(uint64Value(options.Offset))
	if offset >= len(values) {
		return []storeAgentRecord{}
	}
	values = values[offset:]
	limit := int(uint64Value(options.Limit))
	if limit > 0 && limit < len(values) {
		values = values[:limit]
	}
	return values
}
