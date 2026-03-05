package v1api

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/teanode/teanode/internal/coordinators"
	"github.com/teanode/teanode/internal/jobs"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/onboarding"
	"github.com/teanode/teanode/internal/prompts"
	"github.com/teanode/teanode/internal/pubsub"
	"github.com/teanode/teanode/internal/schemas"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
	"github.com/teanode/teanode/internal/version"
	"github.com/teanode/teanode/internal/web"
)

// handleConnect: handshake, return capabilities.
func (self *webSocketConnection) handleConnect(frame requestFrame) {
	agentsList, listError := self.listAgents()
	if listError != nil {
		self.sendError(frame.ID, 500, "listing agents: "+listError.Error())
		return
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

	defaultProviderModelName := ""
	if configuration, err := self.loadConfiguration(); err == nil {
		if configuration.Models != nil && configuration.Models.Default != nil {
			defaultProviderModelName = *configuration.Models.Default
		}
	}

	capabilities := []string{"conversations"}
	if providerRegistry := self.api.coordinator.ProviderRegistry(); providerRegistry != nil {
		if _, _, ok := providerRegistry.FindTranscriber(); ok {
			capabilities = append(capabilities, "audio")
		}
	}

	self.sendResponse(frame.ID, map[string]interface{}{
		"version":                  version.Version(),
		"capabilities":             capabilities,
		"defaultProviderModelName": defaultProviderModelName,
		"agents":                   agentInfos,
		"defaultAgentId":           defaultAgentId,
		"defaultConversationId":    self.api.coordinator.EnsureDefaultConversation(self.userId(), defaultAgentId),
		"isAdmin":                  self.isAdmin(),
		"userId":                   self.userId(),
	})
}

// handleHealth: health check.
func (self *webSocketConnection) handleHealth(frame requestFrame) {
	self.sendResponse(frame.ID, map[string]interface{}{
		"status": "ok",
	})
}

// handleAgentsList: return list of configured agents.
func (self *webSocketConnection) handleAgentsList(frame requestFrame) {
	agentsList, listError := self.listAgents()
	if listError != nil {
		self.sendError(frame.ID, 500, "listing agents: "+listError.Error())
		return
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
	self.sendResponse(frame.ID, map[string]interface{}{
		"agents":         agentInfos,
		"defaultAgentId": defaultAgentId,
	})
}

func (self *webSocketConnection) listAgents() ([]*models.Agent, error) {
	agentsList := make([]*models.Agent, 0)
	if transactionError := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		listedAgents, listError := transaction.ListAgents(ctx, nil)
		if listError != nil {
			return listError
		}
		agentsList = listedAgents
		return nil
	}); transactionError != nil {
		return nil, transactionError
	}
	return agentsList, nil
}

func (self *webSocketConnection) loadConfiguration() (*models.Configuration, error) {
	var configuration *models.Configuration
	if transactionError := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		loadedConfiguration, loadError := transaction.GetConfiguration(ctx, nil)
		if loadError != nil {
			return loadError
		}
		configuration = loadedConfiguration
		return nil
	}); transactionError != nil {
		return nil, transactionError
	}
	return configuration, nil
}

func (self *webSocketConnection) agentAvatarMediaId(agentID string) string {
	avatarMediaId := ""
	_ = store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		agent, err := transaction.GetAgent(ctx, agentID, nil)
		if err != nil {
			return nil
		}
		avatarMediaId = agent.GetAvatarMediaID()
		return nil
	})
	return avatarMediaId
}

// agentsSetDefaultParameters are the parameters for agents.setDefault.
type agentsSetDefaultParameters struct {
	AgentID string `json:"agentId"`
}

// handleAgentsSetDefault: set the default agent.
func (self *webSocketConnection) handleAgentsSetDefault(frame requestFrame) {
	var parameters agentsSetDefaultParameters
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}
	if parameters.AgentID == "" {
		self.sendError(frame.ID, 400, "agentId is required")
		return
	}
	agentExists := false
	_ = store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		if _, getError := transaction.GetAgent(ctx, parameters.AgentID, nil); getError == nil {
			agentExists = true
		}
		return nil
	})
	if !agentExists {
		self.sendError(frame.ID, 404, "agent not found: "+parameters.AgentID)
		return
	}
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		_, err := transaction.ModifyUser(ctx, self.userId(), func(user *models.User) error {
			user.DefaultAgentID = ptrto.Value(parameters.AgentID)
			return nil
		}, nil)
		return err
	}); err != nil {
		self.sendError(frame.ID, 500, "updating default agent: "+err.Error())
		return
	}
	self.api.pubsub.Broadcast(pubsub.EventTypeDefaultAgent, map[string]interface{}{
		"defaultAgentId": parameters.AgentID,
		"userId":         self.userId(),
	})
	self.sendResponse(frame.ID, map[string]interface{}{
		"defaultAgentId":        parameters.AgentID,
		"defaultConversationId": self.api.coordinator.EnsureDefaultConversation(self.userId(), parameters.AgentID),
	})
}

// conversationsSetDefaultParameters are the parameters for conversations.setDefault.
type conversationsSetDefaultParameters struct {
	AgentID        string `json:"agentId"`
	ConversationID string `json:"conversationId"`
}

// handleConversationsSetDefault: set the default conversation for an agent.
func (self *webSocketConnection) handleConversationsSetDefault(frame requestFrame) {
	var parameters conversationsSetDefaultParameters
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}
	if parameters.ConversationID == "" {
		self.sendError(frame.ID, 400, "conversationId is required")
		return
	}
	agentId := parameters.AgentID
	if agentId == "" {
		agentId = self.defaultAgentId()
	}
	self.api.coordinator.SetDefaultConversation(self.userId(), agentId, parameters.ConversationID)
	self.sendResponse(frame.ID, map[string]interface{}{
		"defaultAgentId":        agentId,
		"defaultConversationId": parameters.ConversationID,
	})
}

// conversationSendParameters are the parameters for conversations.send.
type conversationSendParameters struct {
	ConversationID     string              `json:"conversationId"`
	Message            string              `json:"message"`
	ProviderModelName  string              `json:"providerModelName,omitempty"`
	AgentID            string              `json:"agentId,omitempty"`
	OriginID           string              `json:"originId,omitempty"`
	Attachments        []map[string]string `json:"attachments,omitempty"`
	SystemPromptSuffix string              `json:"systemPromptSuffix,omitempty"`
}

// handleConversationsSend: send user message, trigger agent run via gateway.
func (self *webSocketConnection) handleConversationsSend(frame requestFrame) {
	var parameters conversationSendParameters
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}

	if parameters.Message == "" {
		self.sendError(frame.ID, 400, "message is required")
		return
	}

	if parameters.AgentID == "" {
		parameters.AgentID = self.defaultAgentId()
	}

	handle, sendError := self.api.coordinator.Run(self.ctx, coordinators.RunParameters{
		AgentID:            parameters.AgentID,
		ConversationID:     parameters.ConversationID,
		Message:            parameters.Message,
		ProviderModelName:  parameters.ProviderModelName,
		OriginID:           parameters.OriginID,
		Origin:             "webui",
		OriginSessionID:    self.sessionId(),
		Attachments:        parameters.Attachments,
		SystemPromptSuffix: parameters.SystemPromptSuffix,
	}, nil)
	if sendError != nil {
		self.sendError(frame.ID, 500, sendError.Error())
		return
	}

	self.sendResponse(frame.ID, map[string]interface{}{
		"runId":          handle.RunID,
		"conversationId": handle.ConversationID,
	})
}

// conversationHistoryParameters are the parameters for conversations.history.
type conversationHistoryParameters struct {
	ConversationID string `json:"conversationId"`
	AgentID        string `json:"agentId,omitempty"`
	Limit          int    `json:"limit,omitempty"`
	BeforeIndex    int    `json:"beforeIndex,omitempty"`
}

// handleConversationsHistory: return conversation transcript.
func (self *webSocketConnection) handleConversationsHistory(frame requestFrame) {
	var parameters conversationHistoryParameters
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}

	if parameters.ConversationID == "" {
		self.sendError(frame.ID, 400, "conversationId is required")
		return
	}

	if parameters.AgentID == "" {
		parameters.AgentID = self.defaultAgentId()
	}

	// Verify the requesting user owns this conversation.
	if err := self.verifyConversationOwnership(parameters.ConversationID); err != nil {
		self.sendError(frame.ID, 404, "conversation not found")
		return
	}

	limit := parameters.Limit
	if limit <= 0 {
		limit = 50
	}

	messages, err := listConversationMessages(self.ctx, parameters.ConversationID)
	if err != nil {
		self.sendError(frame.ID, 500, "loading conversation: "+err.Error())
		return
	}
	pageMessages, totalCount, oldestLoadedIndex, hasMore := pageConversationMessages(messages, limit, parameters.BeforeIndex)
	providerName, providerModelName := resolveConversationProviderAndModel(messages)

	response := map[string]interface{}{
		"conversationId":    parameters.ConversationID,
		"messages":          marshalConversationMessages(pageMessages),
		"totalCount":        totalCount,
		"oldestLoadedIndex": oldestLoadedIndex,
		"hasMore":           hasMore,
	}
	if self.api.coordinator.GetActiveConversationRunner(parameters.ConversationID) != nil {
		if activeRunId := self.api.coordinator.GetActiveConversationRunID(parameters.ConversationID); activeRunId != "" {
			response["activeRunId"] = activeRunId
		}
	}
	if providerName != "" {
		response["providerName"] = providerName
	}
	if providerModelName != "" {
		response["providerModelName"] = providerModelName
	}
	self.sendResponse(frame.ID, response)
}

// conversationAbortParameters are the parameters for conversations.abort.
type conversationAbortParameters struct {
	RunID          string `json:"runId"`
	ConversationID string `json:"conversationId,omitempty"`
}

// handleConversationsAbort: cancel a running agent. Works cross-tab and cross-channel.
func (self *webSocketConnection) handleConversationsAbort(frame requestFrame) {
	var parameters conversationAbortParameters
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}

	if parameters.RunID == "" && parameters.ConversationID == "" {
		self.sendError(frame.ID, 400, "runId or conversationId is required")
		return
	}

	if parameters.RunID != "" && self.api.coordinator.AbortRun(parameters.RunID) {
		self.sendResponse(frame.ID, map[string]interface{}{
			"aborted": true,
		})
		return
	}

	if parameters.ConversationID != "" {
		if err := self.verifyConversationOwnership(parameters.ConversationID); err != nil {
			self.sendError(frame.ID, 404, "conversation not found")
			return
		}
		if self.api.coordinator.AbortConversationRun(parameters.ConversationID) {
			self.sendResponse(frame.ID, map[string]interface{}{
				"aborted": true,
			})
			return
		}
		self.sendError(frame.ID, 404, "conversation has no active run: "+parameters.ConversationID)
		return
	}

	self.sendError(frame.ID, 404, "run not found: "+parameters.RunID)
}

// conversationsDeleteParameters are the parameters for conversations.delete.
type conversationsDeleteParameters struct {
	ConversationID string `json:"conversationId"`
	AgentID        string `json:"agentId,omitempty"`
}

// handleConversationsDelete: delete a conversation.
func (self *webSocketConnection) handleConversationsDelete(frame requestFrame) {
	var parameters conversationsDeleteParameters
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}

	if parameters.ConversationID == "" {
		self.sendError(frame.ID, 400, "conversationId is required")
		return
	}

	// Resolve the agent ID for default-conversation check.
	resolvedAgentId := parameters.AgentID
	if resolvedAgentId == "" {
		resolvedAgentId = self.defaultAgentId()
	}
	defaultConversationId := self.api.coordinator.EnsureDefaultConversation(self.userId(), resolvedAgentId)
	if parameters.ConversationID == defaultConversationId {
		self.sendError(frame.ID, 409, "cannot delete the default conversation")
		return
	}

	// Verify the requesting user owns this conversation.
	if err := self.verifyConversationOwnership(parameters.ConversationID); err != nil {
		self.sendError(frame.ID, 404, "conversation not found")
		return
	}

	if self.api.coordinator.GetActiveConversationRunner(parameters.ConversationID) != nil {
		self.sendError(frame.ID, 500, "conversation has active run")
		return
	}

	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		return transaction.DeleteConversation(ctx, parameters.ConversationID, nil)
	}); err != nil && err != store.ErrNotFound {
		self.sendError(frame.ID, 500, "deleting conversation: "+err.Error())
		return
	}
	self.api.pubsub.Broadcast(pubsub.EventTypeConversations, nil)

	self.sendResponse(frame.ID, map[string]interface{}{
		"deleted": true,
	})
}

// conversationsListParameters are the parameters for conversations.list.
type conversationsListParameters struct {
	AgentID string `json:"agentId,omitempty"`
	Limit   int    `json:"limit,omitempty"`
}

// handleConversationsList: list available conversations.
func (self *webSocketConnection) handleConversationsList(frame requestFrame) {
	var parameters conversationsListParameters
	if frame.Params != nil {
		json.Unmarshal(frame.Params, &parameters)
	}

	if parameters.AgentID != "" {
		// List conversations for a specific agent.
		conversationList, err := listConversations(self.ctx, self.userId(), parameters.AgentID)
		if err != nil {
			self.sendError(frame.ID, 500, "listing conversations: "+err.Error())
			return
		}
		conversationPayload := marshalConversationList(conversationList)
		sort.Slice(conversationPayload, func(leftIndex, rightIndex int) bool {
			return conversationPayload[leftIndex]["lastActive"].(int64) > conversationPayload[rightIndex]["lastActive"].(int64)
		})
		if parameters.Limit > 0 && len(conversationPayload) > parameters.Limit {
			conversationPayload = conversationPayload[:parameters.Limit]
		}
		self.sendResponse(frame.ID, map[string]interface{}{
			"conversations": conversationPayload,
		})
		return
	}

	// Aggregate conversations from all agents.
	type conversationWithAgent struct {
		ID                string `json:"id"`
		LastActive        int64  `json:"lastActive"`
		Title             string `json:"title,omitempty"`
		Summary           string `json:"summary,omitempty"`
		AgentID           string `json:"agentId,omitempty"`
		ProviderName      string `json:"providerName,omitempty"`
		ProviderModelName string `json:"providerModelName,omitempty"`
	}

	var allConversations []conversationWithAgent
	agentsList, agentsListError := self.listAgents()
	if agentsListError != nil {
		self.sendError(frame.ID, 500, "listing agents: "+agentsListError.Error())
		return
	}
	for _, agent := range agentsList {
		conversationList, err := listConversations(self.ctx, self.userId(), agent.ID)
		if err != nil {
			continue
		}
		for _, conversationInfo := range conversationList {
			lastActive := int64(0)
			if conversationInfo.ModifiedAt != nil {
				lastActive = conversationInfo.ModifiedAt.UnixMilli()
			} else if conversationInfo.CreatedAt != nil {
				lastActive = conversationInfo.CreatedAt.UnixMilli()
			}
			allConversations = append(allConversations, conversationWithAgent{
				ID:         conversationInfo.ID,
				LastActive: lastActive,
				Title:      conversationInfo.GetTitle(),
				Summary:    conversationInfo.GetSummary(),
				AgentID:    agent.ID,
			})
		}
	}

	if parameters.Limit > 0 && len(allConversations) > parameters.Limit {
		allConversations = allConversations[:parameters.Limit]
	}

	self.sendResponse(frame.ID, map[string]interface{}{
		"conversations": allConversations,
	})
}

// --- Models RPC handlers ---

// modelsListEntry is a single model in the models.list response.
type modelsListEntry struct {
	ProviderName  string `json:"providerName"`
	ID            string `json:"id"`
	ContextLength int    `json:"context_length,omitempty"`
}

// handleModelsList: return available models from all providers.
func (self *webSocketConnection) handleModelsList(frame requestFrame) {
	defaultProviderModelName := ""
	if configuration, err := self.loadConfiguration(); err == nil && configuration != nil {
		if configuration.Models != nil {
			if configuration.Models.Default != nil {
				defaultProviderModelName = *configuration.Models.Default
			}
		}
	}

	var entries []modelsListEntry
	if providerRegistry := self.api.coordinator.ProviderRegistry(); providerRegistry != nil {
		providerModels := providerRegistry.ListAllModels(self.ctx)
		entries = make([]modelsListEntry, len(providerModels))
		for index, entry := range providerModels {
			entries[index] = modelsListEntry{
				ProviderName:  entry.ProviderName,
				ID:            entry.ModelName,
				ContextLength: entry.ContextLength,
			}
		}
	}
	if entries == nil {
		entries = []modelsListEntry{}
	}

	self.sendResponse(frame.ID, map[string]interface{}{
		"models":                   entries,
		"defaultProviderModelName": defaultProviderModelName,
	})
}

// --- Config RPC handlers ---

// handleConfigSchema: return the config schema for UI form generation.
func (self *webSocketConnection) handleConfigSchema(frame requestFrame) {
	if !self.requireAdmin(frame) {
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"schema": schemas.ConfigSchema(),
	})
}

// handleConfigGet: return the raw on-disk config.
// Only returns user-specified values, not defaults or environment overrides.
func (self *webSocketConnection) handleConfigGet(frame requestFrame) {
	if !self.requireAdmin(frame) {
		return
	}
	var configuration *models.Configuration
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		result, err := transaction.GetConfiguration(ctx, nil)
		if err != nil {
			return err
		}
		configuration = result
		return nil
	}); err != nil {
		self.sendError(frame.ID, 500, "loading config: "+err.Error())
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"config": configuration,
	})
}

// configUpdateParameters are the parameters for configs.update.
type configUpdateParameters struct {
	Config json.RawMessage `json:"config"`
}

// handleConfigUpdate: merge a partial config into the raw on-disk config and save.
// Only user-specified values are persisted; defaults and env overrides are not saved.
func (self *webSocketConnection) handleConfigUpdate(frame requestFrame) {
	if !self.requireAdmin(frame) {
		return
	}
	var parameters configUpdateParameters
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}

	// Parse the incoming partial config into a typed struct.
	var partialConfiguration models.Configuration
	if err := json.Unmarshal(parameters.Config, &partialConfiguration); err != nil {
		self.sendError(frame.ID, 400, "invalid config object: "+err.Error())
		return
	}

	// Deep merge via generated Update(): only non-nil fields are applied,
	// nested structs are recursively merged.
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		_, modifyError := transaction.ModifyConfiguration(ctx, func(configuration *models.Configuration) error {
			configuration.Update(&partialConfiguration)
			return nil
		}, nil)
		return modifyError
	}); err != nil {
		self.sendError(frame.ID, 500, "saving config: "+err.Error())
		return
	}

	self.sendResponse(frame.ID, map[string]interface{}{
		"ok": true,
	})
}

// --- Agent Config RPC handlers ---

// handleAgentsConfigSchema: return the agent config schema for UI form generation.
func (self *webSocketConnection) handleAgentsConfigSchema(frame requestFrame) {
	if !self.requireAdmin(frame) {
		return
	}
	suggestions := map[string][]string{}

	// TODO: Collect tool names from this user's default runner.
	// agentId := self.defaultAgentId()
	// if agentId != "" {
	// 	runner := self.api.coordinator.GetRunner(agentId)
	// 	if runner != nil && runner.Tools != nil {
	// 		suggestions["tool"] = runner.Tools.Names()
	// 	}
	// }

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

	self.sendResponse(frame.ID, map[string]interface{}{
		"schema":      schemas.AgentSchema(),
		"suggestions": suggestions,
	})
}

// handleAgentsConfigList: return all agent configs from per-agent files.
func (self *webSocketConnection) handleAgentsConfigList(frame requestFrame) {
	if !self.requireAdmin(frame) {
		return
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
		self.sendError(frame.ID, 500, "loading agents: "+err.Error())
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"agents": entries,
	})
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
func (self *webSocketConnection) handleAgentsConfigSave(frame requestFrame) {
	if !self.requireAdmin(frame) {
		return
	}
	var parameters agentsConfigSaveParameters
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}
	if parameters.Agent.ID == "" {
		self.sendError(frame.ID, 400, "agent id is required")
		return
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
		self.sendError(frame.ID, 500, "saving agent: "+err.Error())
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"ok": true,
	})
}

// agentsConfigDeleteParameters are the parameters for agents.config.delete.
type agentsConfigDeleteParameters struct {
	ID string `json:"id"`
}

// handleAgentsConfigDelete: delete an agent's config directory.
func (self *webSocketConnection) handleAgentsConfigDelete(frame requestFrame) {
	if !self.requireAdmin(frame) {
		return
	}
	var parameters agentsConfigDeleteParameters
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}
	if parameters.ID == "" {
		self.sendError(frame.ID, 400, "id is required")
		return
	}
	defaultAgentId := self.defaultAgentId()
	if parameters.ID == defaultAgentId {
		self.sendError(frame.ID, 409, "cannot delete the default agent")
		return
	}
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		return transaction.DeleteAgent(ctx, parameters.ID, nil)
	}); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			self.sendError(frame.ID, 404, "agent not found")
			return
		}
		self.sendError(frame.ID, 500, "deleting agent: "+err.Error())
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"deleted": true,
	})
}

type agentsAvatarSetParameters struct {
	ID            string `json:"id"`
	AvatarMediaID string `json:"avatarMediaId"`
}

func (self *webSocketConnection) handleAgentsAvatarSet(frame requestFrame) {
	if !self.requireAdmin(frame) {
		return
	}
	var parameters agentsAvatarSetParameters
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}
	agentId := parameters.ID
	avatarMediaId := parameters.AvatarMediaID
	if agentId == "" {
		self.sendError(frame.ID, 400, "id is required")
		return
	}
	if avatarMediaId == "" {
		self.sendError(frame.ID, 400, "avatarMediaId is required")
		return
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
			self.sendError(frame.ID, 404, "agent not found")
			return
		}
		self.sendError(frame.ID, 500, "saving agent state: "+err.Error())
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"ok":            true,
		"avatarMediaId": avatarMediaId,
	})
}

type agentsAvatarRemoveParameters struct {
	ID string `json:"id"`
}

func (self *webSocketConnection) handleAgentsAvatarRemove(frame requestFrame) {
	if !self.requireAdmin(frame) {
		return
	}
	var parameters agentsAvatarRemoveParameters
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}
	agentId := parameters.ID
	if agentId == "" {
		self.sendError(frame.ID, 400, "id is required")
		return
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
			self.sendError(frame.ID, 404, "agent not found")
			return
		}
		self.sendError(frame.ID, 500, "saving agent state: "+err.Error())
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"ok":            true,
		"avatarMediaId": "",
	})
}

// --- Jobs RPC handlers ---

// handleJobsList: list all jobs.
func (self *webSocketConnection) handleJobsList(frame requestFrame) {
	jobsList := make([]*models.Job, 0)
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		listedJobs, listError := transaction.ListJobs(ctx, self.userId(), nil)
		if listError != nil {
			return listError
		}
		jobsList = listedJobs
		return nil
	}); err != nil {
		self.sendError(frame.ID, 500, "listing jobs: "+err.Error())
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"jobs": jobsList,
	})
}

// jobCreateParameters are the parameters for job.create.
type jobCreateParameters struct {
	Job models.Job `json:"job"`
}

// handleJobsCreate: create a new job.
func (self *webSocketConnection) handleJobsCreate(frame requestFrame) {
	var parameters jobCreateParameters
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}
	if parameters.Job.GetName() == "" {
		self.sendError(frame.ID, 400, "job.name is required")
		return
	}
	if parameters.Job.GetSchedule() == "" && parameters.Job.RunAt == nil {
		self.sendError(frame.ID, 400, "job.schedule or job.runAt is required")
		return
	}
	if parameters.Job.GetConversationID() == "" {
		defaultConversationId := self.api.coordinator.EnsureDefaultConversation(self.userId(), parameters.Job.GetAgentID())
		parameters.Job.ConversationID = ptrto.Value(defaultConversationId)
	}
	parameters.Job.UserID = ptrto.Value(self.userId())
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		_, createError := transaction.CreateJob(ctx, &parameters.Job, nil)
		return createError
	}); err != nil {
		self.sendError(frame.ID, 500, "creating job: "+err.Error())
		return
	}
	self.api.pubsub.Broadcast(pubsub.EventTypeJobs, nil)
	self.sendResponse(frame.ID, map[string]interface{}{
		"job": parameters.Job,
	})
}

// jobUpdateParameters are the parameters for job.update.
type jobUpdateParameters struct {
	Job models.Job `json:"job"`
}

// handleJobsUpdate: update an existing job.
func (self *webSocketConnection) handleJobsUpdate(frame requestFrame) {
	var parameters jobUpdateParameters
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}
	if parameters.Job.ID == "" {
		self.sendError(frame.ID, 400, "job.id is required")
		return
	}
	var updatedJob *models.Job
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		existingJob, getError := transaction.GetJob(ctx, parameters.Job.ID, nil)
		if getError != nil {
			return getError
		}
		if existingJob.GetUserID() != self.userId() {
			return store.ErrNotFound
		}
		var modifyError error
		updatedJob, modifyError = transaction.ModifyJob(ctx, parameters.Job.ID, func(job *models.Job) error {
			mergeJobUpdate(job, &parameters.Job)
			return nil
		}, nil)
		return modifyError
	}); err != nil {
		self.sendError(frame.ID, 500, "updating job: "+err.Error())
		return
	}
	self.api.pubsub.Broadcast(pubsub.EventTypeJobs, nil)
	self.sendResponse(frame.ID, map[string]interface{}{
		"job": updatedJob,
	})
}

// mergeJobUpdate applies non-nil fields from patch onto an existing job.
func mergeJobUpdate(job *models.Job, patch *models.Job) {
	if patch.Name != nil {
		job.Name = patch.Name
	}
	if patch.Schedule != nil {
		job.Schedule = patch.Schedule
	}
	if patch.RunAt != nil {
		job.RunAt = patch.RunAt
	}
	if patch.Prompt != nil {
		job.Prompt = patch.Prompt
	}
	if patch.ProviderModelName != nil {
		job.ProviderModelName = patch.ProviderModelName
	}
	if patch.AgentID != nil {
		job.AgentID = patch.AgentID
	}
	if patch.ConversationID != nil {
		job.ConversationID = patch.ConversationID
	}
	if patch.Enabled != nil {
		job.Enabled = patch.Enabled
	}
	if patch.OneShot != nil {
		job.OneShot = patch.OneShot
	}
}

// jobDeleteParameters are the parameters for job.delete.
type jobDeleteParameters struct {
	ID string `json:"id"`
}

// handleJobsDelete: delete a job.
func (self *webSocketConnection) handleJobsDelete(frame requestFrame) {
	var parameters jobDeleteParameters
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}
	if parameters.ID == "" {
		self.sendError(frame.ID, 400, "id is required")
		return
	}

	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		job, getError := transaction.GetJob(ctx, parameters.ID, nil)
		if getError != nil {
			return getError
		}
		if job.GetUserID() != self.userId() {
			return store.ErrNotFound
		}
		return transaction.DeleteJob(ctx, parameters.ID, nil)
	}); err != nil {
		self.sendError(frame.ID, 500, "deleting job: "+err.Error())
		return
	}

	self.sendResponse(frame.ID, map[string]interface{}{
		"deleted": true,
	})
}

// jobTriggerParameters are the parameters for job.trigger.
type jobTriggerParameters struct {
	ID string `json:"id"`
}

// handleJobsTrigger: manually trigger a job.
func (self *webSocketConnection) handleJobsTrigger(frame requestFrame) {
	var parameters jobTriggerParameters
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}
	if parameters.ID == "" {
		self.sendError(frame.ID, 400, "id is required")
		return
	}

	// Verify the requesting user owns this job.
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		job, getError := transaction.GetJob(ctx, parameters.ID, nil)
		if getError != nil {
			return getError
		}
		if job.GetUserID() != self.userId() {
			return store.ErrNotFound
		}
		return nil
	}); err != nil {
		self.sendError(frame.ID, 404, "job not found")
		return
	}

	if err := jobs.SchedulerFromContext(self.ctx).TriggerJob(self.ctx, parameters.ID); err != nil {
		self.sendError(frame.ID, 500, "triggering job: "+err.Error())
		return
	}

	self.sendResponse(frame.ID, map[string]interface{}{
		"triggered": true,
	})
}

// --- Sessions RPC handlers ---

// handleSessionsList: list all active sessions.
func (self *webSocketConnection) handleSessionsList(frame requestFrame) {
	filteredSessions := make([]*models.Session, 0)
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		sessionList, err := transaction.ListSessions(ctx, nil)
		if err != nil {
			return err
		}
		for _, session := range sessionList {
			if session.GetUserID() != self.userId() {
				continue
			}
			filteredSessions = append(filteredSessions, session)
		}
		return nil
	}); err != nil {
		self.sendError(frame.ID, 500, "listing sessions: "+err.Error())
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"sessions":         filteredSessions,
		"currentSessionId": self.sessionId(),
	})
}

// sessionsRevokeParameters are the parameters for sessions.revoke.
type sessionsRevokeParameters struct {
	SessionID string `json:"sessionId"`
}

// handleSessionsRevoke: revoke (delete) a session.
func (self *webSocketConnection) handleSessionsRevoke(frame requestFrame) {
	var parameters sessionsRevokeParameters
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}
	if parameters.SessionID == "" {
		self.sendError(frame.ID, 400, "sessionId is required")
		return
	}
	if parameters.SessionID == self.sessionId() {
		self.sendError(frame.ID, 400, "cannot revoke the current session")
		return
	}

	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		session, err := transaction.GetSession(ctx, parameters.SessionID, nil)
		if err != nil {
			return err
		}
		if session.GetUserID() != self.userId() {
			return store.ErrNotFound
		}
		return transaction.DeleteSession(ctx, parameters.SessionID, nil)
	}); err != nil {
		self.sendError(frame.ID, 404, "session not found: "+parameters.SessionID)
		return
	}

	self.sendResponse(frame.ID, map[string]interface{}{
		"revoked": true,
	})
}

// --- Auth RPC handlers ---

type authTokenListItem struct {
	ID            string     `json:"id"`
	Token         string     `json:"token"`
	CreatedAt     time.Time  `json:"createdAt"`
	LastUsedAt    *time.Time `json:"lastUsedAt,omitempty"`
	RemoteAddress string     `json:"remoteAddress,omitempty"`
	UserAgent     string     `json:"userAgent,omitempty"`
}

func toModelAuthTokenListItems(tokens []*models.Token) []authTokenListItem {
	items := make([]authTokenListItem, 0, len(tokens))
	for _, token := range tokens {
		tokenId := token.ID
		tokenValue := token.GetToken()
		if tokenId == "" || tokenValue == "" {
			continue
		}
		item := authTokenListItem{
			ID:            tokenId,
			Token:         tokenValue,
			RemoteAddress: token.GetRemoteAddress(),
			UserAgent:     token.GetUserAgent(),
		}
		if token.CreatedAt != nil {
			item.CreatedAt = *token.CreatedAt
		}
		if token.LastUsedAt != nil {
			lastUsedAt := *token.LastUsedAt
			item.LastUsedAt = &lastUsedAt
		}
		items = append(items, item)
	}
	sort.Slice(items, func(left, right int) bool {
		return items[left].CreatedAt.After(items[right].CreatedAt)
	})
	return items
}

func (self *webSocketConnection) handleAuthTokensList(frame requestFrame) {
	items := make([]authTokenListItem, 0)
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		tokens, err := transaction.ListTokens(ctx, self.userId(), nil)
		if err != nil {
			return err
		}
		items = toModelAuthTokenListItems(tokens)
		return nil
	}); err != nil {
		self.sendError(frame.ID, 500, "failed to list tokens")
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"tokens": items,
	})
}

func (self *webSocketConnection) handleAuthTokensCreate(frame requestFrame) {
	tokenValue := security.GenerateRandomString(48, security.LowerAlphaNumeric)
	var createdItem authTokenListItem
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		createdToken, err := transaction.CreateToken(ctx, &models.Token{
			ID:     security.NewULID(),
			UserID: ptrto.Value(self.userId()),
			Token:  ptrto.TrimmedString(tokenValue),
		}, nil)
		if err != nil {
			return err
		}
		createdItem = authTokenListItem{
			ID:    createdToken.ID,
			Token: createdToken.GetToken(),
		}
		if createdToken.CreatedAt != nil {
			createdItem.CreatedAt = *createdToken.CreatedAt
		}
		if createdToken.LastUsedAt != nil {
			lastUsedAt := *createdToken.LastUsedAt
			createdItem.LastUsedAt = &lastUsedAt
		}
		return nil
	}); err != nil {
		self.sendError(frame.ID, 500, "failed to create token")
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"token": createdItem,
	})
}

type authTokensDeleteParameters struct {
	TokenID string `json:"tokenId"`
}

func (self *webSocketConnection) handleAuthTokensDelete(frame requestFrame) {
	var parameters authTokensDeleteParameters
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}
	tokenId := parameters.TokenID
	if tokenId == "" {
		self.sendError(frame.ID, 400, "tokenId is required")
		return
	}
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		token, err := transaction.GetToken(ctx, tokenId, nil)
		if err != nil {
			return err
		}
		if token.GetUserID() != self.userId() {
			return store.ErrNotFound
		}
		return transaction.DeleteToken(ctx, tokenId, nil)
	}); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			self.sendError(frame.ID, 404, "token not found")
			return
		}
		self.sendError(frame.ID, 500, "failed to delete token")
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"deleted": true,
		"tokenId": tokenId,
	})
}

// authChangePasswordParameters are the parameters for auth.changePassword.
type authChangePasswordParameters struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

// handleAuthChangePassword changes the login password given the current password.
func (self *webSocketConnection) handleAuthChangePassword(frame requestFrame) {
	var parameters authChangePasswordParameters
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}
	if len(parameters.NewPassword) < 8 {
		self.sendError(frame.ID, 400, "new password must be at least 8 characters")
		return
	}
	hash, err := security.HashPassword(parameters.NewPassword)
	if err != nil {
		self.sendError(frame.ID, 500, "failed to hash password")
		return
	}
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		user, err := transaction.GetUser(ctx, self.userId(), nil)
		if err != nil {
			return err
		}
		existingPassword := user.GetPassword()
		if existingPassword != "" {
			if parameters.CurrentPassword == "" {
				return web.Error(400, "current password is required")
			}
			match, verifyError := security.VerifyPassword([]byte(existingPassword), parameters.CurrentPassword)
			if verifyError != nil || !match {
				return web.Error(401, "current password is incorrect")
			}
		}
		_, err = transaction.ModifyUser(ctx, self.userId(), func(user *models.User) error {
			user.Password = ptrto.TrimmedString(string(hash))
			return nil
		}, nil)
		return err
	}); err != nil {
		if typedError, ok := err.(*web.HTTPError); ok {
			self.sendError(frame.ID, typedError.StatusCode, typedError.Error())
			return
		}
		if errors.Is(err, store.ErrNotFound) {
			self.sendError(frame.ID, 404, "user not found")
			return
		}
		self.sendError(frame.ID, 500, "failed to save password")
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"ok": true,
	})
}

func (self *webSocketConnection) requireAdmin(frame requestFrame) bool {
	if !self.isAdmin() {
		self.sendError(frame.ID, 403, "admin access required")
		return false
	}
	return true
}

// verifyConversationOwnership checks that the conversation belongs to the current user.
func (self *webSocketConnection) verifyConversationOwnership(conversationId string) error {
	return store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		conversation, err := transaction.GetConversation(ctx, conversationId, nil)
		if err != nil {
			return err
		}
		if conversation.GetUserID() != self.userId() {
			return store.ErrNotFound
		}
		return nil
	})
}

type usersListItem struct {
	ID            string `json:"id"`
	Username      string `json:"username"`
	Admin         bool   `json:"admin"`
	HasPassword   bool   `json:"hasPassword"`
	Name          string `json:"name,omitempty"`
	Description   string `json:"description,omitempty"`
	AvatarMediaID string `json:"avatarMediaId,omitempty"`
}

func (self *webSocketConnection) handleUsersList(frame requestFrame) {
	if !self.requireAdmin(frame) {
		return
	}
	items := make([]usersListItem, 0)
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		users, err := transaction.ListUsers(ctx, nil)
		if err != nil {
			return err
		}
		sort.Slice(users, func(leftIndex, rightIndex int) bool {
			return users[leftIndex].ID < users[rightIndex].ID
		})
		for _, user := range users {
			userId := user.ID
			item := usersListItem{
				ID:          userId,
				Username:    user.GetUsername(),
				Admin:       user.Admin != nil && *user.Admin,
				HasPassword: user.GetPassword() != "",
				Name:        user.GetUsername(),
				Description: user.GetDescription(),
			}
			item.AvatarMediaID = user.GetAvatarMediaID()
			items = append(items, item)
		}
		return nil
	}); err != nil {
		self.sendError(frame.ID, 500, "listing users: "+err.Error())
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"users": items,
	})
}

type usersCreateParameters struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
}

func (self *webSocketConnection) handleUsersCreate(frame requestFrame) {
	if !self.requireAdmin(frame) {
		return
	}
	var parameters usersCreateParameters
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}
	username := parameters.Username
	if username == "" {
		self.sendError(frame.ID, 400, "username is required")
		return
	}
	if len(parameters.Password) < 8 {
		self.sendError(frame.ID, 400, "password must be at least 8 characters")
		return
	}
	hash, err := security.HashPassword(parameters.Password)
	if err != nil {
		self.sendError(frame.ID, 500, "failed to hash password")
		return
	}
	var createdUserId string
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		if existingUser, _ := transaction.GetUserByUsername(ctx, username, nil); existingUser != nil {
			return store.ErrAlreadyExists
		}
		createdUser, err := onboarding.CreateUser(ctx, transaction, &models.User{
			ID:          security.NewULID(),
			Username:    &username,
			Password:    ptrto.TrimmedString(string(hash)),
			Admin:       ptrto.Value(false),
			Description: ptrto.TrimmedString(parameters.Description),
		})
		if err != nil {
			return err
		}
		createdUserId = createdUser.ID
		return nil
	}); err != nil {
		if errors.Is(err, store.ErrAlreadyExists) {
			self.sendError(frame.ID, 409, "username already exists")
			return
		}
		self.sendError(frame.ID, 500, "failed to create user")
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"user": usersListItem{
			ID:          createdUserId,
			Username:    username,
			Admin:       false,
			HasPassword: true,
			Name:        username,
		},
	})
}

type usersDeleteParameters struct {
	UserID string `json:"userId"`
}

func (self *webSocketConnection) handleUsersDelete(frame requestFrame) {
	if !self.requireAdmin(frame) {
		return
	}
	var parameters usersDeleteParameters
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}
	userId := parameters.UserID
	if userId == "" {
		self.sendError(frame.ID, 400, "userId is required")
		return
	}
	if userId == self.userId() {
		self.sendError(frame.ID, 400, "cannot delete the current user")
		return
	}
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		sessionList, listError := transaction.ListSessions(ctx, nil)
		if listError == nil {
			for _, session := range sessionList {
				if session.GetUserID() == userId {
					_ = transaction.DeleteSession(ctx, session.ID, nil)
				}
			}
		}
		return transaction.DeleteUser(ctx, userId, nil)
	}); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			self.sendError(frame.ID, 404, "user not found")
			return
		}
		self.sendError(frame.ID, 500, "deleting user: "+err.Error())
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"deleted": true,
	})
}

type usersChangePasswordParameters struct {
	UserID      string `json:"userId"`
	NewPassword string `json:"newPassword"`
}

func (self *webSocketConnection) handleUsersChangePassword(frame requestFrame) {
	if !self.requireAdmin(frame) {
		return
	}
	var parameters usersChangePasswordParameters
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}
	userId := parameters.UserID
	if userId == "" {
		self.sendError(frame.ID, 400, "userId is required")
		return
	}
	if userId == self.userId() {
		self.sendError(frame.ID, 400, "use auth.changePassword for current user")
		return
	}
	if len(parameters.NewPassword) < 8 {
		self.sendError(frame.ID, 400, "new password must be at least 8 characters")
		return
	}
	hash, err := security.HashPassword(parameters.NewPassword)
	if err != nil {
		self.sendError(frame.ID, 500, "failed to hash password")
		return
	}
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		_, err := transaction.ModifyUser(ctx, userId, func(user *models.User) error {
			user.Password = ptrto.TrimmedString(string(hash))
			return nil
		}, nil)
		return err
	}); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			self.sendError(frame.ID, 404, "user not found")
			return
		}
		self.sendError(frame.ID, 500, "failed to update password")
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"ok": true,
	})
}

type usersUpdateParameters struct {
	UserID      string  `json:"userId"`
	Username    *string `json:"username,omitempty"`
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	NewPassword *string `json:"newPassword,omitempty"`
}

func (self *webSocketConnection) handleUsersUpdate(frame requestFrame) {
	if !self.requireAdmin(frame) {
		return
	}
	var parameters usersUpdateParameters
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}
	userId := parameters.UserID
	if userId == "" {
		self.sendError(frame.ID, 400, "userId is required")
		return
	}
	if userId == self.userId() {
		self.sendError(frame.ID, 400, "cannot update the current user from users list")
		return
	}
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		_, err := transaction.ModifyUser(ctx, userId, func(user *models.User) error {
			if parameters.Username != nil {
				nextUsername := *parameters.Username
				if nextUsername == "" {
					return web.Error(400, "username is required")
				}
				user.Username = &nextUsername
			}
			if parameters.NewPassword != nil && *parameters.NewPassword != "" {
				if len(*parameters.NewPassword) < 8 {
					return web.Error(400, "new password must be at least 8 characters")
				}
				hash, err := security.HashPassword(*parameters.NewPassword)
				if err != nil {
					return err
				}
				user.Password = ptrto.TrimmedString(string(hash))
			}
			if parameters.Name != nil {
				nextName := *parameters.Name
				if nextName != "" {
					user.Name = &nextName
				}
			}
			if parameters.Description != nil {
				nextDescription := *parameters.Description
				user.Description = &nextDescription
			}
			return nil
		}, nil)
		return err
	}); err != nil {
		if typedError, ok := err.(*web.HTTPError); ok {
			self.sendError(frame.ID, typedError.StatusCode, typedError.Error())
			return
		}
		if errors.Is(err, store.ErrNotFound) {
			self.sendError(frame.ID, 404, "user not found")
			return
		}
		self.sendError(frame.ID, 500, "updating user failed")
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"ok": true,
	})
}

type usersSetRoleParameters struct {
	UserID string `json:"userId"`
	Admin  bool   `json:"admin"`
}

func (self *webSocketConnection) handleUsersSetRole(frame requestFrame) {
	if !self.requireAdmin(frame) {
		return
	}
	var parameters usersSetRoleParameters
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}
	userId := parameters.UserID
	if userId == "" {
		self.sendError(frame.ID, 400, "userId is required")
		return
	}
	if userId == self.userId() {
		self.sendError(frame.ID, 400, "cannot change the current user's role")
		return
	}
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		_, err := transaction.ModifyUser(ctx, userId, func(user *models.User) error {
			user.Admin = &parameters.Admin
			return nil
		}, nil)
		return err
	}); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			self.sendError(frame.ID, 404, "user not found")
			return
		}
		self.sendError(frame.ID, 500, "failed to update role")
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"ok":     true,
		"userId": userId,
		"admin":  parameters.Admin,
	})
}

func (self *webSocketConnection) profileToRpcPayload(profile *models.User) map[string]interface{} {
	payload := map[string]interface{}{
		"name": profile.GetUsername(),
	}
	if description := profile.GetDescription(); description != "" {
		payload["description"] = description
	}
	if avatarMediaId := profile.GetAvatarMediaID(); avatarMediaId != "" {
		payload["avatarMediaId"] = avatarMediaId
	}
	return payload
}

func (self *webSocketConnection) handleProfileGet(frame requestFrame) {
	var profile *models.User
	err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		user, getError := transaction.GetUser(ctx, self.userId(), nil)
		if getError != nil {
			return getError
		}
		profile = user
		return nil
	})
	if err != nil {
		self.sendError(frame.ID, 500, "failed to load profile")
		return
	}
	self.sendResponse(frame.ID, self.profileToRpcPayload(profile))
}

type profileUpdateParameters struct {
	Name          *string `json:"name,omitempty"`
	Description   *string `json:"description,omitempty"`
	AvatarMediaID *string `json:"avatarMediaId,omitempty"`
}

func (self *webSocketConnection) handleProfileUpdate(frame requestFrame) {
	var parameters profileUpdateParameters
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}

	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		_, err := transaction.ModifyUser(ctx, self.userId(), func(user *models.User) error {
			if parameters.Name != nil {
				user.Name = ptrto.TrimmedString(*parameters.Name)
			}
			if parameters.Description != nil {
				user.Description = ptrto.TrimmedString(*parameters.Description)
			}
			if parameters.AvatarMediaID != nil {
				avatarMediaId := *parameters.AvatarMediaID
				user.AvatarMediaID = &avatarMediaId
			}
			return nil
		}, nil)
		return err
	}); err != nil {
		self.sendError(frame.ID, 500, "failed to save profile")
		return
	}
	var persisted *models.User
	err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		user, getError := transaction.GetUser(ctx, self.userId(), nil)
		if getError != nil {
			return getError
		}
		persisted = user
		return nil
	})
	if err != nil {
		self.sendError(frame.ID, 500, "failed to load profile")
		return
	}

	self.sendResponse(frame.ID, self.profileToRpcPayload(persisted))
}

func (self *webSocketConnection) handleProfileAvatarRemove(frame requestFrame) {
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		_, err := transaction.ModifyUser(ctx, self.userId(), func(user *models.User) error {
			emptyValue := ""
			user.AvatarMediaID = &emptyValue
			return nil
		}, nil)
		return err
	}); err != nil {
		self.sendError(frame.ID, 500, "failed to save profile")
		return
	}
	var persisted *models.User
	err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		user, getError := transaction.GetUser(ctx, self.userId(), nil)
		if getError != nil {
			return getError
		}
		persisted = user
		return nil
	})
	if err != nil {
		self.sendError(frame.ID, 500, "failed to load profile")
		return
	}
	self.sendResponse(frame.ID, self.profileToRpcPayload(persisted))
}

// --- Projects RPC handlers ---

func (self *webSocketConnection) handleProjectsList(frame requestFrame) {
	if !self.requireAdmin(frame) {
		return
	}
	projectList := make([]map[string]interface{}, 0)
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		projects, err := transaction.ListProjects(ctx, nil)
		if err != nil {
			return err
		}
		for _, project := range projects {
			projectList = append(projectList, map[string]interface{}{
				"id":          project.ID,
				"name":        project.GetName(),
				"description": project.GetDescription(),
			})
		}
		return nil
	}); err != nil {
		self.sendError(frame.ID, 500, "listing projects: "+err.Error())
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"projects": projectList,
	})
}

type projectsCreateParameters struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Purpose     string `json:"purpose,omitempty"`
}

func projectRpcError(err error, operation string) (int, string) {
	message := err.Error()
	lower := strings.ToLower(message)
	if errors.Is(err, os.ErrNotExist) {
		return 404, operation + ": not found"
	}
	if strings.Contains(lower, "not found") {
		return 404, operation + ": " + message
	}
	if strings.Contains(lower, "invalid projectid") || strings.Contains(lower, "name is required") {
		return 400, operation + ": " + message
	}
	return 500, operation + ": " + message
}
func (self *webSocketConnection) handleProjectsCreate(frame requestFrame) {
	if !self.requireAdmin(frame) {
		return
	}
	var parameters projectsCreateParameters
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}
	if parameters.Name == "" {
		self.sendError(frame.ID, 400, "name is required")
		return
	}
	var createdProject *models.Project
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		projectId := security.NewULID()
		projectMarkdown, buildError := prompts.BuildProjectMarkdown(parameters.Name, projectId, parameters.Description, parameters.Purpose)
		if buildError != nil {
			return buildError
		}
		relativePath := "PROJECT.md"
		contentBytes := []byte(projectMarkdown)
		workspaceFiles := []models.WorkspaceFile{
			{Path: &relativePath, Content: &contentBytes},
		}
		project, err := transaction.CreateProject(ctx, &models.Project{
			ID:          projectId,
			Name:        ptrto.TrimmedString(parameters.Name),
			Description: ptrto.TrimmedString(parameters.Description),
		}, workspaceFiles, nil)
		if err != nil {
			return err
		}
		createdProject = project
		return nil
	}); err != nil {
		code, message := projectRpcError(err, "creating project")
		self.sendError(frame.ID, code, message)
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"project": map[string]interface{}{
			"id":          createdProject.ID,
			"name":        createdProject.GetName(),
			"description": createdProject.GetDescription(),
		},
	})
}

type projectsRenameParameters struct {
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
}

func (self *webSocketConnection) handleProjectsRename(frame requestFrame) {
	if !self.requireAdmin(frame) {
		return
	}
	var parameters projectsRenameParameters
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}
	if parameters.ProjectID == "" {
		self.sendError(frame.ID, 400, "projectId is required")
		return
	}
	if parameters.Name == "" {
		self.sendError(frame.ID, 400, "name is required")
		return
	}
	var updatedProject *models.Project
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		project, err := transaction.ModifyProject(ctx, parameters.ProjectID, func(project *models.Project) error {
			project.Name = ptrto.TrimmedString(parameters.Name)
			return nil
		}, nil)
		if err != nil {
			return err
		}
		updatedProject = project
		return nil
	}); err != nil {
		code, message := projectRpcError(err, "renaming project")
		self.sendError(frame.ID, code, message)
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"project": map[string]interface{}{
			"id":          updatedProject.ID,
			"name":        updatedProject.GetName(),
			"description": updatedProject.GetDescription(),
		},
	})
}

type projectsDeleteParameters struct {
	ProjectID string `json:"projectId"`
}

func (self *webSocketConnection) handleProjectsDelete(frame requestFrame) {
	if !self.requireAdmin(frame) {
		return
	}
	var parameters projectsDeleteParameters
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}
	if parameters.ProjectID == "" {
		self.sendError(frame.ID, 400, "projectId is required")
		return
	}
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		return transaction.DeleteProject(ctx, parameters.ProjectID, nil)
	}); err != nil {
		code, message := projectRpcError(err, "deleting project")
		self.sendError(frame.ID, code, message)
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"deleted": true,
	})
}

func listConversations(ctx context.Context, userId, agentId string) ([]*models.Conversation, error) {
	result := make([]*models.Conversation, 0)
	err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		items, err := transaction.ListConversations(ctx, store.ConversationListOptions{
			UserID:  ptrto.Value(userId),
			AgentID: ptrto.Value(agentId),
		}, nil)
		if err != nil {
			return err
		}
		result = append(result, items...)
		return nil
	})
	return result, err
}

func listConversationMessages(ctx context.Context, conversationId string) ([]*models.ConversationMessage, error) {
	result := make([]*models.ConversationMessage, 0)
	err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		items, err := transaction.ListConversationMessages(ctx, conversationId, nil)
		if err != nil {
			return err
		}
		result = append(result, items...)
		return nil
	})
	if err == store.ErrNotFound {
		return nil, nil
	}
	return result, err
}

func resolveConversationProviderAndModel(messages []*models.ConversationMessage) (string, string) {
	providerName := ""
	providerModelName := ""
	for index := len(messages) - 1; index >= 0; index-- {
		if providerName == "" && messages[index].ProviderName != nil {
			providerName = *messages[index].ProviderName
		}
		if providerModelName == "" && messages[index].ProviderModelName != nil {
			providerModelName = *messages[index].ProviderModelName
		}
		if providerName != "" && providerModelName != "" {
			break
		}
	}
	return providerName, providerModelName
}

func pageConversationMessages(messages []*models.ConversationMessage, limit int, beforeIndex int) ([]*models.ConversationMessage, int, int, bool) {
	totalCount := len(messages)
	end := totalCount
	if beforeIndex > 0 && beforeIndex < totalCount {
		end = beforeIndex
	}
	start := end - limit
	if start < 0 {
		start = 0
	}
	return messages[start:end], totalCount, start, start > 0
}

func marshalConversationList(conversationList []*models.Conversation) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(conversationList))
	for _, conversation := range conversationList {
		lastActive := int64(0)
		if conversation.ModifiedAt != nil {
			lastActive = conversation.ModifiedAt.UnixMilli()
		} else if conversation.CreatedAt != nil {
			lastActive = conversation.CreatedAt.UnixMilli()
		}
		result = append(result, map[string]interface{}{
			"id":         conversation.ID,
			"lastActive": lastActive,
			"title":      conversation.GetTitle(),
			"summary":    conversation.GetSummary(),
		})
	}
	return result
}

func marshalConversationMessages(messages []*models.ConversationMessage) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(messages))
	for _, message := range messages {
		entry := map[string]interface{}{
			"role":      valueOrRole(message.Role),
			"content":   json.RawMessage(message.Content),
			"timestamp": valueOrTimeUnixMillis(message.CreatedAt),
		}
		if message.StopReason != nil {
			entry["stopReason"] = string(*message.StopReason)
		}
		if message.ProviderModelName != nil {
			entry["providerModelName"] = *message.ProviderModelName
		}
		if message.ProviderName != nil {
			entry["providerName"] = *message.ProviderName
		}
		if message.ToolCallID != nil {
			entry["toolCallId"] = *message.ToolCallID
		}
		if message.ToolName != nil {
			entry["toolName"] = *message.ToolName
		}
		if len(message.Metadata) > 0 {
			entry["metadata"] = json.RawMessage(message.Metadata)
		}
		if len(message.ToolCalls) > 0 {
			entry["toolCalls"] = json.RawMessage(message.ToolCalls)
		}
		if len(message.Usage) > 0 {
			entry["usage"] = json.RawMessage(message.Usage)
		}
		result = append(result, entry)
	}
	return result
}

func valueOrRole(value *models.Role) string {
	if value == nil {
		return ""
	}
	return string(*value)
}

func valueOrTimeUnixMillis(value *time.Time) int64 {
	if value == nil {
		return 0
	}
	return value.UnixMilli()
}
