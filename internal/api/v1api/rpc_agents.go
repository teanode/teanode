package v1api

import (
	"context"
	"errors"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/prompts"
	"github.com/teanode/teanode/internal/pubsub"
	"github.com/teanode/teanode/internal/schemas"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
)

// handleAgentsList: return list of configured agents.
func (self *webSocketConnection) handleAgentsList(frame requestFrame) (interface{}, error) {
	agentsList, listError := self.listAgents()
	if listError != nil {
		return nil, rpcError(500, "listing agents: "+listError.Error())
	}
	defaultAgentId := self.defaultAgentId()
	agentInfos := make([]map[string]interface{}, 0, len(agentsList))
	for _, agent := range agentsList {
		info := map[string]interface{}{
			"id":                    agent.ID,
			"defaultConversationId": self.api.coordinator.EnsureDefaultConversation(self.userId(), agent.ID),
		}
		if name := agent.GetName(); name != "" {
			info["name"] = name
		}
		if avatarMediaId := self.agentAvatarMediaId(agent.ID); avatarMediaId != "" {
			info["avatarMediaId"] = avatarMediaId
		}
		agentInfos = append(agentInfos, info)
	}
	return map[string]interface{}{
		"agents":         agentInfos,
		"defaultAgentId": defaultAgentId,
	}, nil
}

// agentsSetDefaultParameters are the parameters for agents.setDefault.
type agentsSetDefaultParameters struct {
	AgentID string `json:"agentId"`
}

// handleAgentsSetDefault: set the default agent.
func (self *webSocketConnection) handleAgentsSetDefault(frame requestFrame) (interface{}, error) {
	parameters, err := unmarshalParams[agentsSetDefaultParameters](frame)
	if err != nil {
		return nil, err
	}
	if parameters.AgentID == "" {
		return nil, rpcError(400, "agentId is required")
	}
	agentExists := false
	_ = store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		if _, getError := transaction.GetAgent(ctx, parameters.AgentID, nil); getError == nil {
			agentExists = true
		}
		return nil
	})
	if !agentExists {
		return nil, rpcError(404, "agent not found: "+parameters.AgentID)
	}
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		_, err := transaction.ModifyUser(ctx, self.userId(), func(user *models.User) error {
			user.DefaultAgentID = ptrto.Value(parameters.AgentID)
			return nil
		}, nil)
		return err
	}); err != nil {
		return nil, rpcError(500, "updating default agent: "+err.Error())
	}
	self.api.pubsub.Broadcast(pubsub.EventTypeDefaultAgent, map[string]interface{}{
		"defaultAgentId": parameters.AgentID,
		"userId":         self.userId(),
	})
	return map[string]interface{}{
		"defaultAgentId":        parameters.AgentID,
		"defaultConversationId": self.api.coordinator.EnsureDefaultConversation(self.userId(), parameters.AgentID),
	}, nil
}

// handleAgentsConfigSchema: return the agent config schema for UI form generation.
func (self *webSocketConnection) handleAgentsConfigSchema(frame requestFrame) (interface{}, error) {
	if err := self.requireAdmin(); err != nil {
		return nil, err
	}
	suggestions := map[string][]string{}

	// Collect skill names from store-backed skills.
	skillNames := make([]string, 0)
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		skills, err := transaction.ListSkills(ctx, nil)
		if err != nil {
			return err
		}
		for _, skill := range skills {
			skillNames = append(skillNames, skill.GetName())
		}
		return nil
	}); err != nil {
		log.Warningf("failed to collect a list of skill names: %v", err)
	}

	suggestions["skill"] = skillNames

	return map[string]interface{}{
		"schema":      schemas.AgentSchema(),
		"suggestions": suggestions,
	}, nil
}

// handleAgentsConfigList: return all agent configs from per-agent files.
func (self *webSocketConnection) handleAgentsConfigList(frame requestFrame) (interface{}, error) {
	if err := self.requireAdmin(); err != nil {
		return nil, err
	}
	entries := make([]map[string]interface{}, 0)
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		agents, err := transaction.ListAgents(ctx, nil)
		if err != nil {
			return err
		}
		for _, agent := range agents {
			entry := map[string]interface{}{
				"id": agent.ID,
			}
			if name := agent.GetName(); name != "" {
				entry["name"] = name
			}
			if modelName := agent.GetProviderModelName(); modelName != "" {
				entry["providerModelName"] = modelName
			}
			if agent.Tools != nil && len(*agent.Tools) > 0 {
				entry["tools"] = *agent.Tools
			}
			if agent.Skills != nil && len(*agent.Skills) > 0 {
				entry["skills"] = *agent.Skills
			}
			if avatarMediaId := agent.GetAvatarMediaID(); avatarMediaId != "" {
				entry["avatarMediaId"] = avatarMediaId
			}
			entries = append(entries, entry)
		}
		return nil
	}); err != nil {
		return nil, rpcError(500, "loading agents: "+err.Error())
	}
	return map[string]interface{}{
		"agents": entries,
	}, nil
}

// agentsConfigSaveParameters are the parameters for agents.config.save.
type agentsConfigSaveParameters struct {
	Agent struct {
		ID                string   `json:"id"`
		Name              string   `json:"name,omitempty"`
		ProviderModelName string   `json:"providerModelName,omitempty"`
		Tools             []string `json:"tools,omitempty"`
		Skills            []string `json:"skills,omitempty"`
		Description       string   `json:"description,omitempty"`
		AvatarMediaID     string   `json:"avatarMediaId,omitempty"`
	} `json:"agent"`
}

// handleAgentsConfigSave: save a single agent config to its per-agent file.
func (self *webSocketConnection) handleAgentsConfigSave(frame requestFrame) (interface{}, error) {
	if err := self.requireAdmin(); err != nil {
		return nil, err
	}
	parameters, err := unmarshalParams[agentsConfigSaveParameters](frame)
	if err != nil {
		return nil, err
	}
	if parameters.Agent.ID == "" {
		return nil, rpcError(400, "agent id is required")
	}
	agentConfig := parameters.Agent
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		agentID := agentConfig.ID
		existingAgent, err := transaction.GetAgent(ctx, agentID, nil)
		agentNotFound := false
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				agentNotFound = true
			} else {
				return err
			}
		}

		if !agentNotFound && existingAgent != nil {
			if agentConfig.Description == "" {
				agentConfig.Description = existingAgent.GetDescription()
			}
			if agentConfig.AvatarMediaID == "" {
				agentConfig.AvatarMediaID = existingAgent.GetAvatarMediaID()
			}
			_, err = transaction.ModifyAgent(ctx, agentID, func(agent *models.Agent) error {
				agent.Name = ptrto.TrimmedString(agentConfig.Name)
				agent.ProviderModelName = ptrto.TrimmedString(agentConfig.ProviderModelName)
				agent.Tools = ptrto.Value(agentConfig.Tools)
				agent.Skills = ptrto.Value(agentConfig.Skills)
				agent.Description = ptrto.TrimmedString(agentConfig.Description)
				agent.AvatarMediaID = ptrto.TrimmedString(agentConfig.AvatarMediaID)
				return nil
			}, nil)
			return err
		}

		seedWorkspaceFiles := []models.WorkspaceFile{
			{Path: ptrto.Value("AGENT.md"), Content: ptrto.Value([]byte(prompts.DefaultAgentMarkdown()))},
			{Path: ptrto.Value("MEMORY.md"), Content: ptrto.Value([]byte(prompts.DefaultMemoryMarkdown()))},
			{Path: ptrto.Value("SKILLS.md"), Content: ptrto.Value([]byte(prompts.DefaultSkillsMarkdown()))},
		}
		_, err = transaction.CreateAgent(ctx, &models.Agent{
			ID:                agentID,
			Name:              ptrto.TrimmedString(agentConfig.Name),
			ProviderModelName: ptrto.TrimmedString(agentConfig.ProviderModelName),
			Tools:             ptrto.Value(agentConfig.Tools),
			Skills:            ptrto.Value(agentConfig.Skills),
			Description:       ptrto.TrimmedString(agentConfig.Description),
			AvatarMediaID:     ptrto.TrimmedString(agentConfig.AvatarMediaID),
		}, seedWorkspaceFiles, nil)
		return err
	}); err != nil {
		return nil, rpcError(500, "saving agent: "+err.Error())
	}
	return map[string]interface{}{
		"ok": true,
	}, nil
}

// agentsConfigDeleteParameters are the parameters for agents.config.delete.
type agentsConfigDeleteParameters struct {
	ID string `json:"id"`
}

// handleAgentsConfigDelete: delete an agent's config directory.
func (self *webSocketConnection) handleAgentsConfigDelete(frame requestFrame) (interface{}, error) {
	if err := self.requireAdmin(); err != nil {
		return nil, err
	}
	parameters, err := unmarshalParams[agentsConfigDeleteParameters](frame)
	if err != nil {
		return nil, err
	}
	if parameters.ID == "" {
		return nil, rpcError(400, "id is required")
	}
	defaultAgentId := self.defaultAgentId()
	if parameters.ID == defaultAgentId {
		return nil, rpcError(409, "cannot delete the default agent")
	}
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		return transaction.DeleteAgent(ctx, parameters.ID, nil)
	}); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, rpcError(404, "agent not found")
		}
		return nil, rpcError(500, "deleting agent: "+err.Error())
	}
	return map[string]interface{}{
		"deleted": true,
	}, nil
}

type agentsAvatarSetParameters struct {
	ID            string `json:"id"`
	AvatarMediaID string `json:"avatarMediaId"`
}

func (self *webSocketConnection) handleAgentsAvatarSet(frame requestFrame) (interface{}, error) {
	if err := self.requireAdmin(); err != nil {
		return nil, err
	}
	parameters, err := unmarshalParams[agentsAvatarSetParameters](frame)
	if err != nil {
		return nil, err
	}
	agentId := parameters.ID
	avatarMediaId := parameters.AvatarMediaID
	if agentId == "" {
		return nil, rpcError(400, "id is required")
	}
	if avatarMediaId == "" {
		return nil, rpcError(400, "avatarMediaId is required")
	}

	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		if _, getError := transaction.GetAgent(ctx, agentId, nil); getError != nil {
			return getError
		}
		_, err := transaction.ModifyAgent(ctx, agentId, func(agent *models.Agent) error {
			agent.AvatarMediaID = &avatarMediaId
			return nil
		}, nil)
		return err
	}); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, rpcError(404, "agent not found")
		}
		return nil, rpcError(500, "saving agent state: "+err.Error())
	}
	return map[string]interface{}{
		"ok":            true,
		"avatarMediaId": avatarMediaId,
	}, nil
}

type agentsAvatarRemoveParameters struct {
	ID string `json:"id"`
}

func (self *webSocketConnection) handleAgentsAvatarRemove(frame requestFrame) (interface{}, error) {
	if err := self.requireAdmin(); err != nil {
		return nil, err
	}
	parameters, err := unmarshalParams[agentsAvatarRemoveParameters](frame)
	if err != nil {
		return nil, err
	}
	agentId := parameters.ID
	if agentId == "" {
		return nil, rpcError(400, "id is required")
	}
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		if _, getError := transaction.GetAgent(ctx, agentId, nil); getError != nil {
			return getError
		}
		_, err := transaction.ModifyAgent(ctx, agentId, func(agent *models.Agent) error {
			agent.AvatarMediaID = ptrto.TrimmedString("")
			return nil
		}, nil)
		return err
	}); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, rpcError(404, "agent not found")
		}
		return nil, rpcError(500, "saving agent state: "+err.Error())
	}
	return map[string]interface{}{
		"ok":            true,
		"avatarMediaId": "",
	}, nil
}
