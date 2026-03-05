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
	for _, agentConfiguration := range applyOffsetLimit(agentConfigurations, options) {
		agent := agentConfigurationToModel(agentConfiguration)
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
	if _, err := os.Stat(self.agentConfigurationFilename(agentId)); err == nil {
		return nil, store.ErrAlreadyExists
	}
	configuration := modelToAgentConfiguration(*agent)
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
	createdAgent := agentConfigurationToModel(configuration)
	createdAgent.ID = agentId
	return &createdAgent, nil
}

func (self *fileSystemTransaction) getAgent(agentId string, options *store.Option) (*models.Agent, error) {
	if _, statError := os.Stat(self.agentConfigurationFilename(agentId)); statError != nil {
		if os.IsNotExist(statError) {
			return nil, store.ErrNotFound
		}
		return nil, statError
	}
	configuration, err := self.loadAgentRecord(agentId)
	if err != nil {
		return nil, err
	}
	agent := agentConfigurationToModel(*configuration)
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
	configuration := modelToAgentConfiguration(*agent)
	if err := self.saveAgentRecord(agentId, &configuration); err != nil {
		return nil, err
	}
	result := agentConfigurationToModel(configuration)
	result.ID = agentId
	return &result, nil
}

func (self *fileSystemTransaction) deleteAgent(agentId string, options *store.Option) error {
	return self.deleteAgentDirectories(agentId)
}

func agentConfigurationToModel(configuration storeAgentRecord) models.Agent {
	agent := models.Agent{
		ID:                configuration.ID,
		Name:              ptrto.TrimmedString(configuration.Name),
		ProviderModelName: ptrto.TrimmedString(configuration.ProviderModelName),
		Skills:            ptrto.TrimmedStrings(configuration.Skills),
		Tools:             ptrto.TrimmedStrings(configuration.Tools),
		Description:       ptrto.TrimmedString(configuration.Description),
		AvatarMediaID:     ptrto.TrimmedString(configuration.AvatarMediaID),
	}
	if !configuration.SummarizedAt.Time.IsZero() {
		agent.SummarizedAt = &configuration.SummarizedAt.Time
	}
	return agent
}

func modelToAgentConfiguration(agent models.Agent) storeAgentRecord {
	record := storeAgentRecord{
		ID:                agent.ID,
		Name:              agent.GetName(),
		ProviderModelName: agent.GetProviderModelName(),
		Skills:            sliceValue(agent.Skills),
		Tools:             sliceValue(agent.Tools),
		Description:       agent.GetDescription(),
		AvatarMediaID:     agent.GetAvatarMediaID(),
	}
	if agent.SummarizedAt != nil {
		record.SummarizedAt = timeutil.Timestamp{Time: *agent.SummarizedAt}
	}
	return record
}
