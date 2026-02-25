package fs

import (
	"fmt"
	"os"
	"strings"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
)

func (self *transaction) ListAgents(options *store.Option) ([]models.Agent, error) {
	return self.listAgents(options)
}

func (self *transaction) CreateAgent(agent *models.Agent, seedWorkspaceFiles []models.WorkspaceFile, options *store.Option) (*models.Agent, error) {
	return self.createAgent(agent, seedWorkspaceFiles, options)
}

func (self *transaction) GetAgent(agentId string, options *store.Option) (*models.Agent, error) {
	return self.getAgent(agentId, options)
}

func (self *transaction) ModifyAgent(agentId string, modifier func(*models.Agent) error, options *store.Option) (*models.Agent, error) {
	return self.modifyAgent(agentId, modifier, options)
}

func (self *transaction) DeleteAgent(agentId string, options *store.Option) error {
	return self.deleteAgent(agentId, options)
}
func (self *transaction) listAgents(options *store.Option) ([]models.Agent, error) {
	agentConfigurations, err := self.listAgentRecords()
	if err != nil {
		return nil, err
	}
	agents := make([]models.Agent, 0, len(agentConfigurations))
	for _, agentConfiguration := range applyOffsetLimitAgentConfig(agentConfigurations, options) {
		agents = append(agents, agentConfigToModel(agentConfiguration))
	}
	return agents, nil
}

func (self *transaction) createAgent(agent *models.Agent, seedWorkspaceFiles []models.WorkspaceFile, options *store.Option) (*models.Agent, error) {
	if agent == nil {
		return nil, fmt.Errorf("agent is required")
	}
	agentId := strings.TrimSpace(agent.ID)
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
		if _, err := self.CreateWorkspaceFile(&copyFile, options); err != nil {
			return nil, err
		}
	}
	createdAgent := agentConfigToModel(configuration)
	createdAgent.ID = agentId
	return &createdAgent, nil
}

func (self *transaction) getAgent(agentId string, options *store.Option) (*models.Agent, error) {
	configuration, err := self.loadAgentRecord(agentId)
	if err != nil {
		return nil, err
	}
	if configuration == nil || strings.TrimSpace(configuration.ID) == "" {
		return nil, store.ErrNotFound
	}
	agent := agentConfigToModel(*configuration)
	return &agent, nil
}

func (self *transaction) modifyAgent(agentId string, modifier func(*models.Agent) error, options *store.Option) (*models.Agent, error) {
	agent, err := self.GetAgent(agentId, options)
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

func (self *transaction) deleteAgent(agentId string, options *store.Option) error {
	return self.deleteAgentDirectories(agentId)
}

func agentConfigToModel(configuration storeAgentRecord) models.Agent {
	return models.Agent{
		ID:            strings.TrimSpace(configuration.ID),
		Name:          ptrto.TrimmedString(configuration.Name),
		Model:         ptrto.TrimmedString(configuration.Model),
		Skills:        ptrto.TrimmedStrings(configuration.Skills),
		Tools:         ptrto.TrimmedStrings(configuration.Tools),
		Description:   ptrto.TrimmedString(configuration.Description),
		AvatarMediaID: ptrto.TrimmedString(configuration.AvatarMediaID),
	}
}

func modelToAgentConfig(agent models.Agent) storeAgentRecord {
	return storeAgentRecord{
		ID:            strings.TrimSpace(agent.ID),
		Name:          strings.TrimSpace(valueOrEmpty(agent.Name)),
		Model:         strings.TrimSpace(valueOrEmpty(agent.Model)),
		Skills:        sliceValue(agent.Skills),
		Tools:         sliceValue(agent.Tools),
		Description:   strings.TrimSpace(valueOrEmpty(agent.Description)),
		AvatarMediaID: strings.TrimSpace(valueOrEmpty(agent.AvatarMediaID)),
	}
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
