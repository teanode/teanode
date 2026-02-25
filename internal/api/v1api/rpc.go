package v1api

import (
	"encoding/json"
	"errors"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/teanode/teanode/internal/agents"
	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/conversations"
	"github.com/teanode/teanode/internal/gw"
	"github.com/teanode/teanode/internal/jobs"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/onboarding"
	"github.com/teanode/teanode/internal/skills"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
	"github.com/teanode/teanode/internal/version"
	"github.com/teanode/teanode/internal/web"
)

// handleConnect: handshake, return capabilities.
func (self *webSocketConnection) handleConnect(frame requestFrame) {
	agentConfigs := self.api.gateway.Config().Agents()
	defaultAgentId, err := self.api.gateway.EnsureDefaultAgent(self.userId())
	if err != nil {
		self.sendError(frame.ID, 500, "resolving default agent: "+err.Error())
		return
	}
	agentInfos := make([]map[string]interface{}, 0, len(agentConfigs))
	for _, agentConfig := range agentConfigs {
		info := map[string]interface{}{
			"id":                    agentConfig.ID,
			"defaultConversationId": self.api.gateway.EnsureDefaultConversation(self.userId(), agentConfig.ID),
		}
		if agentConfig.Name != "" {
			info["name"] = agentConfig.Name
		}
		if avatarMediaID := self.agentAvatarMediaID(agentConfig.ID); avatarMediaID != "" {
			info["avatarMediaId"] = avatarMediaID
		}
		agentInfos = append(agentInfos, info)
	}

	config := self.api.gateway.Config()

	capabilities := []string{"conversations"}
	if registry := self.api.gateway.ProviderRegistry(); registry != nil {
		if _, _, ok := registry.FindTranscriber(); ok {
			capabilities = append(capabilities, "audio")
		}
	}

	self.sendResponse(frame.ID, map[string]interface{}{
		"version":               version.Version(),
		"capabilities":          capabilities,
		"defaultModel":          config.Models.Default,
		"agents":                agentInfos,
		"defaultAgentId":        defaultAgentId,
		"defaultConversationId": self.api.gateway.EnsureDefaultConversation(self.userId(), defaultAgentId),
		"isAdmin":               self.api.gateway.SecurityConfig().IsAdmin(self.userId()),
		"userId":                self.userId(),
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
	agentConfigs := self.api.gateway.Config().Agents()
	defaultAgentId, err := self.api.gateway.EnsureDefaultAgent(self.userId())
	if err != nil {
		self.sendError(frame.ID, 500, "resolving default agent: "+err.Error())
		return
	}
	agentInfos := make([]map[string]interface{}, 0, len(agentConfigs))
	for _, agentConfig := range agentConfigs {
		info := map[string]interface{}{
			"id":                    agentConfig.ID,
			"defaultConversationId": self.api.gateway.EnsureDefaultConversation(self.userId(), agentConfig.ID),
		}
		if agentConfig.Name != "" {
			info["name"] = agentConfig.Name
		}
		if avatarMediaID := self.agentAvatarMediaID(agentConfig.ID); avatarMediaID != "" {
			info["avatarMediaId"] = avatarMediaID
		}
		agentInfos = append(agentInfos, info)
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"agents":         agentInfos,
		"defaultAgentId": defaultAgentId,
	})
}

func (self *webSocketConnection) agentAvatarMediaID(agentID string) string {
	avatarMediaID := ""
	_ = store.StoreFromContext(self.context).Transaction(func(transaction store.Transaction) error {
		agent, err := transaction.GetAgent(agentID, nil)
		if err != nil {
			return nil
		}
		avatarMediaID = strings.TrimSpace(valueOrEmpty(agent.AvatarMediaID))
		return nil
	})
	return avatarMediaID
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
	if err := self.api.gateway.SetDefaultAgent(self.userId(), parameters.AgentID); err != nil {
		self.sendError(frame.ID, 404, err.Error())
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"defaultAgentId":        parameters.AgentID,
		"defaultConversationId": self.api.gateway.EnsureDefaultConversation(self.userId(), parameters.AgentID),
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
		resolvedDefaultAgentId, err := self.api.gateway.EnsureDefaultAgent(self.userId())
		if err != nil {
			self.sendError(frame.ID, 500, "resolving default agent: "+err.Error())
			return
		}
		agentId = resolvedDefaultAgentId
	}
	self.api.gateway.SetDefaultConversation(self.userId(), agentId, parameters.ConversationID)
	self.sendResponse(frame.ID, map[string]interface{}{
		"defaultAgentId":        agentId,
		"defaultConversationId": parameters.ConversationID,
	})
}

// conversationSendParameters are the parameters for conversations.send.
type conversationSendParameters struct {
	ConversationID     string                     `json:"conversationId"`
	Message            string                     `json:"message"`
	Model              string                     `json:"model,omitempty"`
	AgentID            string                     `json:"agentId,omitempty"`
	OriginID           string                     `json:"originId,omitempty"`
	Attachments        []conversations.Attachment `json:"attachments,omitempty"`
	SystemPromptSuffix string                     `json:"systemPromptSuffix,omitempty"`
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
		resolvedDefaultAgentId, err := self.api.gateway.EnsureDefaultAgent(self.userId())
		if err != nil {
			self.sendError(frame.ID, 500, "resolving default agent: "+err.Error())
			return
		}
		parameters.AgentID = resolvedDefaultAgentId
	}

	handle := self.api.gateway.SendMessage(self.context, gw.SendMessageParameters{
		AgentID:            parameters.AgentID,
		ConversationID:     parameters.ConversationID,
		Message:            parameters.Message,
		Model:              parameters.Model,
		OriginID:           parameters.OriginID,
		Origin:             "webui",
		OriginSessionID:    self.sessionId(),
		Attachments:        parameters.Attachments,
		SystemPromptSuffix: parameters.SystemPromptSuffix,
	}, nil)

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
		resolvedDefaultAgentId, err := self.api.gateway.EnsureDefaultAgent(self.userId())
		if err != nil {
			self.sendError(frame.ID, 500, "resolving default agent: "+err.Error())
			return
		}
		parameters.AgentID = resolvedDefaultAgentId
	}

	limit := parameters.Limit
	if limit <= 0 {
		limit = 50
	}

	store := self.api.gateway.ConversationStore(self.userId(), parameters.AgentID)
	page, err := store.LoadPage(parameters.ConversationID, limit, parameters.BeforeIndex)
	if err != nil {
		self.sendError(frame.ID, 500, "loading conversation: "+err.Error())
		return
	}

	response := map[string]interface{}{
		"conversationId":    parameters.ConversationID,
		"messages":          page.Messages,
		"totalCount":        page.TotalCount,
		"oldestLoadedIndex": page.OldestLoadedIndex,
		"hasMore":           page.HasMore,
	}
	if activeRunId := self.api.gateway.GetActiveRun(parameters.ConversationID); activeRunId != "" {
		response["activeRunId"] = activeRunId
	}
	// Include conversation's locked provider/model from the header.
	if header, headerError := store.LoadHeader(parameters.ConversationID); headerError == nil {
		if header.Provider != "" {
			response["provider"] = header.Provider
		}
		if header.Model != "" {
			response["model"] = header.Model
		}
	}
	self.sendResponse(frame.ID, response)
}

// conversationAbortParameters are the parameters for conversations.abort.
type conversationAbortParameters struct {
	RunID string `json:"runId"`
}

// handleConversationsAbort: cancel a running agent. Works cross-tab and cross-channel.
func (self *webSocketConnection) handleConversationsAbort(frame requestFrame) {
	var parameters conversationAbortParameters
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}

	if self.api.gateway.AbortRun(parameters.RunID) {
		self.sendResponse(frame.ID, map[string]interface{}{
			"aborted": true,
		})
	} else {
		self.sendError(frame.ID, 404, "run not found: "+parameters.RunID)
	}
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
		defaultAgentId, err := self.api.gateway.EnsureDefaultAgent(self.userId())
		if err != nil {
			self.sendError(frame.ID, 500, "resolving default agent: "+err.Error())
			return
		}
		resolvedAgentId = defaultAgentId
	}
	defaultConversationId := self.api.gateway.EnsureDefaultConversation(self.userId(), resolvedAgentId)
	if parameters.ConversationID == defaultConversationId {
		self.sendError(frame.ID, 409, "cannot delete the default conversation")
		return
	}

	if err := self.api.gateway.DeleteConversation(self.userId(), resolvedAgentId, parameters.ConversationID); err != nil {
		self.sendError(frame.ID, 500, "deleting conversation: "+err.Error())
		return
	}

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
		store := self.api.gateway.ConversationStore(self.userId(), parameters.AgentID)
		conversations, err := store.List()
		if err != nil {
			self.sendError(frame.ID, 500, "listing conversations: "+err.Error())
			return
		}
		if parameters.Limit > 0 && len(conversations) > parameters.Limit {
			conversations = conversations[:parameters.Limit]
		}
		self.sendResponse(frame.ID, map[string]interface{}{
			"conversations": conversations,
		})
		return
	}

	// Aggregate conversations from all agents.
	type conversationWithAgent struct {
		ID         string `json:"id"`
		LastActive int64  `json:"lastActive"`
		Title      string `json:"title,omitempty"`
		Summary    string `json:"summary,omitempty"`
		AgentID    string `json:"agentId,omitempty"`
		Provider   string `json:"provider,omitempty"`
		Model      string `json:"model,omitempty"`
	}

	var allConversations []conversationWithAgent
	self.api.gateway.AgentRegistry().ForEach(func(agentId string, _ *agents.Runner) {
		store := self.api.gateway.ConversationStore(self.userId(), agentId)
		conversations, err := store.List()
		if err != nil {
			return
		}
		for _, conversationInfo := range conversations {
			allConversations = append(allConversations, conversationWithAgent{
				ID:         conversationInfo.ID,
				LastActive: conversationInfo.LastActive,
				Title:      conversationInfo.Title,
				Summary:    conversationInfo.Summary,
				AgentID:    agentId,
				Provider:   conversationInfo.Provider,
				Model:      conversationInfo.Model,
			})
		}
	})

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
	Provider      string `json:"provider"`
	ID            string `json:"id"`
	ContextLength int    `json:"context_length,omitempty"`
}

// handleModelsList: return available models from all providers.
func (self *webSocketConnection) handleModelsList(frame requestFrame) {
	models, err := self.api.gateway.LoadModels(self.context)
	if err != nil {
		self.sendError(frame.ID, 500, "loading models: "+err.Error())
		return
	}

	configuration := self.api.gateway.Config()
	defaultContextWindow := configuration.Models.ContextWindow

	var entries []modelsListEntry
	for providerName, modelList := range models {
		for _, model := range modelList {
			contextLength := model.ContextLength
			if contextLength <= 0 {
				contextLength = defaultContextWindow
			}
			entries = append(entries, modelsListEntry{
				Provider:      providerName,
				ID:            model.ID,
				ContextLength: contextLength,
			})
		}
	}

	self.sendResponse(frame.ID, map[string]interface{}{
		"models":       entries,
		"defaultModel": configuration.Models.Default,
	})
}

// --- Config RPC handlers ---

// handleConfigSchema: return the config schema for UI form generation.
func (self *webSocketConnection) handleConfigSchema(frame requestFrame) {
	if !self.requireAdmin(frame) {
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"schema": configs.ConfigSchema(),
	})
}

// handleConfigGet: return the raw on-disk config.
// Only returns user-specified values, not defaults or environment overrides.
func (self *webSocketConnection) handleConfigGet(frame requestFrame) {
	if !self.requireAdmin(frame) {
		return
	}
	var configuration *models.Configuration
	if err := store.StoreFromContext(self.context).Transaction(func(transaction store.Transaction) error {
		result, err := transaction.GetConfiguration(nil)
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

	// Load raw configuration from store.
	var currentData []byte
	if err := store.StoreFromContext(self.context).Transaction(func(transaction store.Transaction) error {
		configuration, err := transaction.GetConfiguration(nil)
		if err != nil {
			return err
		}
		encoded, encodeError := json.Marshal(configuration)
		if encodeError != nil {
			return encodeError
		}
		currentData = encoded
		return nil
	}); err != nil {
		self.sendError(frame.ID, 500, "loading config: "+err.Error())
		return
	}

	// Round-trip current config to a map for merging.
	var err error
	var mergedModelConfig models.Configuration
	currentMap := map[string]interface{}{}
	if err := json.Unmarshal(currentData, &currentMap); err != nil {
		self.sendError(frame.ID, 500, "parsing config: "+err.Error())
		return
	}

	// Parse the incoming partial configs.
	var partialMap map[string]interface{}
	if err := json.Unmarshal(parameters.Config, &partialMap); err != nil {
		self.sendError(frame.ID, 400, "invalid config object: "+err.Error())
		return
	}

	// Deep merge: recursively merge maps so nested secrets are preserved.
	deepMerge(currentMap, partialMap)

	mergedData, err := json.Marshal(currentMap)
	if err != nil {
		self.sendError(frame.ID, 500, "marshalling merged config: "+err.Error())
		return
	}

	if err := json.Unmarshal(mergedData, &mergedModelConfig); err != nil {
		self.sendError(frame.ID, 500, "parsing merged config: "+err.Error())
		return
	}
	if err := store.StoreFromContext(self.context).Transaction(func(transaction store.Transaction) error {
		_, modifyError := transaction.ModifyConfiguration(func(configuration *models.Configuration) error {
			*configuration = mergedModelConfig
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

// deepMerge recursively merges source into destination. For keys where both
// sides are maps, it recurses. Otherwise the source value replaces the
// destination value. This preserves existing nested keys (like API keys that
// were stripped by stripMasked) rather than replacing entire sub-objects.
func deepMerge(destination map[string]interface{}, source map[string]interface{}) {
	for key, sourceValue := range source {
		if sourceMap, ok := sourceValue.(map[string]interface{}); ok {
			if destinationMap, ok := destination[key].(map[string]interface{}); ok {
				deepMerge(destinationMap, sourceMap)
				continue
			}
		}
		destination[key] = sourceValue
	}
}

// --- Agent Config RPC handlers ---

// handleAgentsConfigSchema: return the agent config schema for UI form generation.
func (self *webSocketConnection) handleAgentsConfigSchema(frame requestFrame) {
	if !self.requireAdmin(frame) {
		return
	}
	suggestions := map[string][]string{}

	// Collect tool names from this user's default runner.
	agentId, err := self.api.gateway.EnsureDefaultAgent(self.userId())
	if err == nil {
		runner := self.api.gateway.GetRunner(agentId)
		if runner != nil {
			_, _, tools, _, _ := runner.Snapshot()
			if tools != nil {
				suggestions["tool"] = tools.Names()
			}
		}
	}

	// Collect skill names from the skills directory.
	skillsDirectory := configs.SkillsDirectory()
	suggestions["skill"] = skills.Names(self.context, skillsDirectory)

	self.sendResponse(frame.ID, map[string]interface{}{
		"schema":      configs.AgentConfigSchema(),
		"suggestions": suggestions,
	})
}

// handleAgentsConfigList: return all agent configs from per-agent files.
func (self *webSocketConnection) handleAgentsConfigList(frame requestFrame) {
	if !self.requireAdmin(frame) {
		return
	}
	entries := make([]map[string]interface{}, 0)
	if err := store.StoreFromContext(self.context).Transaction(func(transaction store.Transaction) error {
		agents, err := transaction.ListAgents(nil)
		if err != nil {
			return err
		}
		for _, agent := range agents {
			entry := map[string]interface{}{
				"id": agent.ID,
			}
			if name := strings.TrimSpace(valueOrEmpty(agent.Name)); name != "" {
				entry["name"] = name
			}
			if modelName := strings.TrimSpace(valueOrEmpty(agent.Model)); modelName != "" {
				entry["model"] = modelName
			}
			if agent.Tools != nil && len(*agent.Tools) > 0 {
				entry["tools"] = *agent.Tools
			}
			if agent.Skills != nil && len(*agent.Skills) > 0 {
				entry["skills"] = *agent.Skills
			}
			if avatarMediaID := strings.TrimSpace(valueOrEmpty(agent.AvatarMediaID)); avatarMediaID != "" {
				entry["avatarMediaId"] = avatarMediaID
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
	Agent configs.AgentConfig `json:"agent"`
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
	if err := store.StoreFromContext(self.context).Transaction(func(transaction store.Transaction) error {
		agentID := strings.TrimSpace(agentConfig.ID)
		existingAgent, err := transaction.GetAgent(agentID, nil)
		agentNotFound := false
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				agentNotFound = true
			} else {
				return err
			}
		}

		if !agentNotFound && existingAgent != nil {
			if strings.TrimSpace(agentConfig.Description) == "" {
				agentConfig.Description = strings.TrimSpace(valueOrEmpty(existingAgent.Description))
			}
			if strings.TrimSpace(agentConfig.AvatarMediaID) == "" {
				agentConfig.AvatarMediaID = strings.TrimSpace(valueOrEmpty(existingAgent.AvatarMediaID))
			}
			_, err = transaction.ModifyAgent(agentID, func(agent *models.Agent) error {
				agent.Name = ptrto.TrimmedString(strings.TrimSpace(agentConfig.Name))
				agent.Model = ptrto.TrimmedString(strings.TrimSpace(agentConfig.Model))
				agent.Tools = ptrto.Value(agentConfig.Tools)
				agent.Skills = ptrto.Value(agentConfig.Skills)
				agent.Description = ptrto.TrimmedString(strings.TrimSpace(agentConfig.Description))
				agent.AvatarMediaID = ptrto.TrimmedString(strings.TrimSpace(agentConfig.AvatarMediaID))
				return nil
			}, nil)
			return err
		}

		_, err = transaction.CreateAgent(&models.Agent{
			ID:            agentID,
			Name:          ptrto.TrimmedString(strings.TrimSpace(agentConfig.Name)),
			Model:         ptrto.TrimmedString(strings.TrimSpace(agentConfig.Model)),
			Tools:         ptrto.Value(agentConfig.Tools),
			Skills:        ptrto.Value(agentConfig.Skills),
			Description:   ptrto.TrimmedString(strings.TrimSpace(agentConfig.Description)),
			AvatarMediaID: ptrto.TrimmedString(strings.TrimSpace(agentConfig.AvatarMediaID)),
		}, nil, nil)
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
	defaultAgentId, err := self.api.gateway.EnsureDefaultAgent(self.userId())
	if err != nil {
		self.sendError(frame.ID, 500, "resolving default agent: "+err.Error())
		return
	}
	if parameters.ID == defaultAgentId {
		self.sendError(frame.ID, 409, "cannot delete the default agent")
		return
	}
	if err := store.StoreFromContext(self.context).Transaction(func(transaction store.Transaction) error {
		return transaction.DeleteAgent(parameters.ID, nil)
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
	agentId := strings.TrimSpace(parameters.ID)
	avatarMediaId := strings.TrimSpace(parameters.AvatarMediaID)
	if agentId == "" {
		self.sendError(frame.ID, 400, "id is required")
		return
	}
	if avatarMediaId == "" {
		self.sendError(frame.ID, 400, "avatarMediaId is required")
		return
	}

	if self.api.gateway.Config().AgentByID(agentId) == nil {
		self.sendError(frame.ID, 404, "agent not found")
		return
	}
	if err := store.StoreFromContext(self.context).Transaction(func(transaction store.Transaction) error {
		_, err := transaction.ModifyAgent(agentId, func(agent *models.Agent) error {
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
	agentId := strings.TrimSpace(parameters.ID)
	if agentId == "" {
		self.sendError(frame.ID, 400, "id is required")
		return
	}
	if self.api.gateway.Config().AgentByID(agentId) == nil {
		self.sendError(frame.ID, 404, "agent not found")
		return
	}
	if err := store.StoreFromContext(self.context).Transaction(func(transaction store.Transaction) error {
		_, err := transaction.ModifyAgent(agentId, func(agent *models.Agent) error {
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
	jobsList := make([]models.Job, 0)
	if err := store.StoreFromContext(self.context).Transaction(func(transaction store.Transaction) error {
		listedJobs, listError := transaction.ListJobs(self.userId(), nil)
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
	if strings.TrimSpace(valueOrEmpty(parameters.Job.Name)) == "" {
		self.sendError(frame.ID, 400, "job.name is required")
		return
	}
	if strings.TrimSpace(valueOrEmpty(parameters.Job.Schedule)) == "" && parameters.Job.RunAt == nil {
		self.sendError(frame.ID, 400, "job.schedule or job.runAt is required")
		return
	}
	if strings.TrimSpace(valueOrEmpty(parameters.Job.ConversationID)) == "" {
		defaultConversationId := self.api.gateway.EnsureDefaultConversation(self.userId(), valueOrEmpty(parameters.Job.AgentID))
		parameters.Job.ConversationID = ptrto.Value(defaultConversationId)
	}
	parameters.Job.UserID = ptrto.Value(self.userId())
	if err := store.StoreFromContext(self.context).Transaction(func(transaction store.Transaction) error {
		_, createError := transaction.CreateJob(&parameters.Job, nil)
		return createError
	}); err != nil {
		self.sendError(frame.ID, 500, "creating job: "+err.Error())
		return
	}
	self.api.gateway.Broadcast(gw.EventTypeJobs, nil)
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
	if strings.TrimSpace(valueOrEmpty(parameters.Job.Name)) == "" {
		self.sendError(frame.ID, 400, "job.name is required")
		return
	}
	if strings.TrimSpace(valueOrEmpty(parameters.Job.Schedule)) == "" && parameters.Job.RunAt == nil {
		self.sendError(frame.ID, 400, "job.schedule or job.runAt is required")
		return
	}
	if strings.TrimSpace(valueOrEmpty(parameters.Job.ConversationID)) == "" {
		defaultConversationId := self.api.gateway.EnsureDefaultConversation(self.userId(), valueOrEmpty(parameters.Job.AgentID))
		parameters.Job.ConversationID = ptrto.Value(defaultConversationId)
	}
	parameters.Job.UserID = ptrto.Value(self.userId())
	if err := store.StoreFromContext(self.context).Transaction(func(transaction store.Transaction) error {
		existingJob, getError := transaction.GetJob(parameters.Job.ID, nil)
		if getError != nil {
			return getError
		}
		if valueOrEmpty(existingJob.UserID) != self.userId() {
			return store.ErrNotFound
		}
		_, modifyError := transaction.ModifyJob(parameters.Job.ID, func(job *models.Job) error {
			*job = parameters.Job
			return nil
		}, nil)
		return modifyError
	}); err != nil {
		self.sendError(frame.ID, 500, "updating job: "+err.Error())
		return
	}
	self.api.gateway.Broadcast(gw.EventTypeJobs, nil)
	self.sendResponse(frame.ID, map[string]interface{}{
		"job": parameters.Job,
	})
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

	if err := store.StoreFromContext(self.context).Transaction(func(transaction store.Transaction) error {
		job, getError := transaction.GetJob(parameters.ID, nil)
		if getError != nil {
			return getError
		}
		if valueOrEmpty(job.UserID) != self.userId() {
			return store.ErrNotFound
		}
		return transaction.DeleteJob(parameters.ID, nil)
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

	if err := jobs.SchedulerFromContext(self.context).TriggerJob(self.context, parameters.ID); err != nil {
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
	filteredSessions := make([]models.Session, 0)
	if err := store.StoreFromContext(self.context).Transaction(func(transaction store.Transaction) error {
		sessionList, err := transaction.ListSessions(nil)
		if err != nil {
			return err
		}
		for _, session := range sessionList {
			if strings.TrimSpace(valueOrEmpty(session.UserID)) != self.userId() {
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

	if err := store.StoreFromContext(self.context).Transaction(func(transaction store.Transaction) error {
		session, err := transaction.GetSession(parameters.SessionID, nil)
		if err != nil {
			return err
		}
		if strings.TrimSpace(valueOrEmpty(session.UserID)) != self.userId() {
			return store.ErrNotFound
		}
		return transaction.DeleteSession(parameters.SessionID, nil)
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
	ID         string     `json:"id"`
	Token      string     `json:"token"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
}

func toModelAuthTokenListItems(tokens []models.Token) []authTokenListItem {
	items := make([]authTokenListItem, 0, len(tokens))
	for _, token := range tokens {
		tokenID := strings.TrimSpace(token.ID)
		tokenValue := strings.TrimSpace(valueOrEmpty(token.Token))
		if tokenID == "" || tokenValue == "" {
			continue
		}
		item := authTokenListItem{
			ID:    tokenID,
			Token: tokenValue,
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
	if err := store.StoreFromContext(self.context).Transaction(func(transaction store.Transaction) error {
		tokens, err := transaction.ListTokens(self.userId(), nil)
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
	if err := store.StoreFromContext(self.context).Transaction(func(transaction store.Transaction) error {
		createdToken, err := transaction.CreateToken(&models.Token{
			ID:     security.NewULID(),
			UserID: ptrto.TrimmedString(self.userId()),
			Token:  ptrto.TrimmedString(tokenValue),
		}, nil)
		if err != nil {
			return err
		}
		createdItem = authTokenListItem{
			ID:    createdToken.ID,
			Token: valueOrEmpty(createdToken.Token),
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
	tokenId := strings.TrimSpace(parameters.TokenID)
	if tokenId == "" {
		self.sendError(frame.ID, 400, "tokenId is required")
		return
	}
	if err := store.StoreFromContext(self.context).Transaction(func(transaction store.Transaction) error {
		token, err := transaction.GetToken(tokenId, nil)
		if err != nil {
			return err
		}
		if strings.TrimSpace(valueOrEmpty(token.UserID)) != self.userId() {
			return store.ErrNotFound
		}
		return transaction.DeleteToken(tokenId, nil)
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
	if err := store.StoreFromContext(self.context).Transaction(func(transaction store.Transaction) error {
		user, err := transaction.GetUser(self.userId(), nil)
		if err != nil {
			return err
		}
		existingPassword := strings.TrimSpace(valueOrEmpty(user.Password))
		if existingPassword != "" {
			if parameters.CurrentPassword == "" {
				return web.Error(400, "current password is required")
			}
			match, verifyError := security.VerifyPassword([]byte(existingPassword), parameters.CurrentPassword)
			if verifyError != nil || !match {
				return web.Error(401, "current password is incorrect")
			}
		}
		_, err = transaction.ModifyUser(self.userId(), func(user *models.User) error {
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
	isAdmin := false
	_ = store.StoreFromContext(self.context).Transaction(func(transaction store.Transaction) error {
		user, err := transaction.GetUser(self.userId(), nil)
		if err != nil {
			return nil
		}
		isAdmin = user.Admin != nil && *user.Admin
		return nil
	})
	if !isAdmin {
		self.sendError(frame.ID, 403, "admin access required")
		return false
	}
	return true
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
	if err := store.StoreFromContext(self.context).Transaction(func(transaction store.Transaction) error {
		users, err := transaction.ListUsers(nil)
		if err != nil {
			return err
		}
		sort.Slice(users, func(leftIndex, rightIndex int) bool {
			return users[leftIndex].ID < users[rightIndex].ID
		})
		for _, user := range users {
			userID := user.ID
			item := usersListItem{
				ID:          userID,
				Username:    valueOrEmpty(user.Username),
				Admin:       user.Admin != nil && *user.Admin,
				HasPassword: strings.TrimSpace(valueOrEmpty(user.Password)) != "",
				Name:        valueOrEmpty(user.Username),
				Description: valueOrEmpty(user.Description),
			}
			item.AvatarMediaID = valueOrEmpty(user.AvatarMediaID)
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
	username := strings.TrimSpace(parameters.Username)
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
	admin := false
	var createdUserID string
	if err := store.StoreFromContext(self.context).Transaction(func(transaction store.Transaction) error {
		if _, _, found := transaction.GetUserByUsername(username, nil); found {
			return store.ErrAlreadyExists
		}
		createdUser, err := transaction.CreateUser(&models.User{
			ID:          security.NewULID(),
			Username:    &username,
			Password:    ptrto.TrimmedString(string(hash)),
			Admin:       &admin,
			Description: ptrto.TrimmedString(strings.TrimSpace(parameters.Description)),
		}, nil, nil)
		if err != nil {
			return err
		}
		createdUserID = createdUser.ID
		return nil
	}); err != nil {
		if errors.Is(err, store.ErrAlreadyExists) {
			self.sendError(frame.ID, 409, "username already exists")
			return
		}
		self.sendError(frame.ID, 500, "failed to create user")
		return
	}
	if err := onboarding.InitializeUser(self.context, self.api.gateway, createdUserID); err != nil {
		self.sendError(frame.ID, 500, "failed to initialize user onboarding")
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"user": usersListItem{
			ID:          createdUserID,
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
	userId := strings.TrimSpace(parameters.UserID)
	if userId == "" {
		self.sendError(frame.ID, 400, "userId is required")
		return
	}
	if userId == self.userId() {
		self.sendError(frame.ID, 400, "cannot delete the current user")
		return
	}
	if err := store.StoreFromContext(self.context).Transaction(func(transaction store.Transaction) error {
		sessionList, listError := transaction.ListSessions(nil)
		if listError == nil {
			for _, session := range sessionList {
				if strings.TrimSpace(valueOrEmpty(session.UserID)) == userId {
					_ = transaction.DeleteSession(session.ID, nil)
				}
			}
		}
		return transaction.DeleteUser(userId, nil)
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
	userId := strings.TrimSpace(parameters.UserID)
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
	if err := store.StoreFromContext(self.context).Transaction(func(transaction store.Transaction) error {
		_, err := transaction.ModifyUser(userId, func(user *models.User) error {
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
	userId := strings.TrimSpace(parameters.UserID)
	if userId == "" {
		self.sendError(frame.ID, 400, "userId is required")
		return
	}
	if userId == self.userId() {
		self.sendError(frame.ID, 400, "cannot update the current user from users list")
		return
	}
	if err := store.StoreFromContext(self.context).Transaction(func(transaction store.Transaction) error {
		_, err := transaction.ModifyUser(userId, func(user *models.User) error {
			if parameters.Username != nil {
				nextUsername := strings.TrimSpace(*parameters.Username)
				if nextUsername == "" {
					return web.Error(400, "username is required")
				}
				user.Username = &nextUsername
			}
			if parameters.NewPassword != nil && strings.TrimSpace(*parameters.NewPassword) != "" {
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
				nextName := strings.TrimSpace(*parameters.Name)
				if nextName != "" {
					user.Username = &nextName
				}
			}
			if parameters.Description != nil {
				nextDescription := strings.TrimSpace(*parameters.Description)
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
	userId := strings.TrimSpace(parameters.UserID)
	if userId == "" {
		self.sendError(frame.ID, 400, "userId is required")
		return
	}
	if userId == self.userId() {
		self.sendError(frame.ID, 400, "cannot change the current user's role")
		return
	}
	if err := store.StoreFromContext(self.context).Transaction(func(transaction store.Transaction) error {
		_, err := transaction.ModifyUser(userId, func(user *models.User) error {
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

func profileToRpcPayload(profile *models.User) map[string]interface{} {
	payload := map[string]interface{}{
		"name": valueOrEmpty(profile.Username),
	}
	if strings.TrimSpace(valueOrEmpty(profile.Username)) == "" {
		payload["name"] = configs.OSUsername()
	}
	if description := strings.TrimSpace(valueOrEmpty(profile.Description)); description != "" {
		payload["description"] = description
	}
	if avatarMediaId := strings.TrimSpace(valueOrEmpty(profile.AvatarMediaID)); avatarMediaId != "" {
		payload["avatarMediaId"] = avatarMediaId
	}
	return payload
}

func (self *webSocketConnection) handleProfileGet(frame requestFrame) {
	var profile *models.User
	err := store.StoreFromContext(self.context).Transaction(func(transaction store.Transaction) error {
		user, getError := transaction.GetUser(self.userId(), nil)
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
	self.sendResponse(frame.ID, profileToRpcPayload(profile))
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

	if err := store.StoreFromContext(self.context).Transaction(func(transaction store.Transaction) error {
		_, err := transaction.ModifyUser(self.userId(), func(user *models.User) error {
			if parameters.Name != nil {
				user.Username = ptrto.TrimmedString(strings.TrimSpace(*parameters.Name))
			}
			if parameters.Description != nil {
				user.Description = ptrto.TrimmedString(strings.TrimSpace(*parameters.Description))
			}
			if parameters.AvatarMediaID != nil {
				avatarMediaId := strings.TrimSpace(*parameters.AvatarMediaID)
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
	err := store.StoreFromContext(self.context).Transaction(func(transaction store.Transaction) error {
		user, getError := transaction.GetUser(self.userId(), nil)
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

	self.sendResponse(frame.ID, profileToRpcPayload(persisted))
}

func (self *webSocketConnection) handleProfileAvatarRemove(frame requestFrame) {
	if err := store.StoreFromContext(self.context).Transaction(func(transaction store.Transaction) error {
		_, err := transaction.ModifyUser(self.userId(), func(user *models.User) error {
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
	err := store.StoreFromContext(self.context).Transaction(func(transaction store.Transaction) error {
		user, getError := transaction.GetUser(self.userId(), nil)
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
	self.sendResponse(frame.ID, profileToRpcPayload(persisted))
}

// --- Projects RPC handlers ---

func (self *webSocketConnection) handleProjectsList(frame requestFrame) {
	if !self.requireAdmin(frame) {
		return
	}
	projectList := make([]map[string]interface{}, 0)
	if err := store.StoreFromContext(self.context).Transaction(func(transaction store.Transaction) error {
		projects, err := transaction.ListProjects(nil)
		if err != nil {
			return err
		}
		for _, project := range projects {
			projectList = append(projectList, map[string]interface{}{
				"id":          project.ID,
				"name":        valueOrEmpty(project.Name),
				"description": valueOrEmpty(project.Description),
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
	message := strings.TrimSpace(err.Error())
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
	if err := store.StoreFromContext(self.context).Transaction(func(transaction store.Transaction) error {
		projectID := security.NewULID()
		workspaceFiles := make([]models.WorkspaceFile, 0)
		purpose := strings.TrimSpace(parameters.Purpose)
		if purpose != "" {
			relativePath := "PURPOSE.md"
			content := []byte(purpose)
			workspaceFiles = append(workspaceFiles, models.WorkspaceFile{
				Path:    &relativePath,
				Content: &content,
			})
		}
		project, err := transaction.CreateProject(&models.Project{
			ID:          projectID,
			Name:        ptrto.TrimmedString(strings.TrimSpace(parameters.Name)),
			Description: ptrto.TrimmedString(strings.TrimSpace(parameters.Description)),
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
			"name":        valueOrEmpty(createdProject.Name),
			"description": valueOrEmpty(createdProject.Description),
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
	if err := store.StoreFromContext(self.context).Transaction(func(transaction store.Transaction) error {
		project, err := transaction.ModifyProject(parameters.ProjectID, func(project *models.Project) error {
			project.Name = ptrto.TrimmedString(strings.TrimSpace(parameters.Name))
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
			"name":        valueOrEmpty(updatedProject.Name),
			"description": valueOrEmpty(updatedProject.Description),
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
	if err := store.StoreFromContext(self.context).Transaction(func(transaction store.Transaction) error {
		return transaction.DeleteProject(parameters.ProjectID, nil)
	}); err != nil {
		code, message := projectRpcError(err, "deleting project")
		self.sendError(frame.ID, code, message)
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"deleted": true,
	})
}

// --- Skills Registry RPC handlers ---

func (self *webSocketConnection) handleSkillsRegistryList(frame requestFrame) {
	if !self.requireAdmin(frame) {
		return
	}
	configuration := self.api.gateway.Config()
	self.sendResponse(frame.ID, map[string]interface{}{
		"registries": configuration.SkillsRegistries,
	})
}

type skillsRegistrySearchParameters struct {
	Query string `json:"query,omitempty"`
}

func (self *webSocketConnection) handleSkillsLocalList(frame requestFrame) {
	if !self.requireAdmin(frame) {
		return
	}
	self.sendError(frame.ID, 400, "local skills are no longer supported")
}

func (self *webSocketConnection) handleSkillsRegistrySearch(frame requestFrame) {
	if !self.requireAdmin(frame) {
		return
	}
	var parameters skillsRegistrySearchParameters
	if frame.Params != nil {
		if err := json.Unmarshal(frame.Params, &parameters); err != nil {
			self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
			return
		}
	}
	results, err := skills.Search(self.context, self.api.gateway.Config().SkillsRegistries, parameters.Query)
	if err != nil {
		self.sendError(frame.ID, 500, "searching registry: "+err.Error())
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"results": results,
	})
}

type skillsInstallParameters struct {
	SourceID string `json:"sourceId,omitempty"`
	Name     string `json:"name"`
	Version  string `json:"version,omitempty"`
}

func (self *webSocketConnection) handleSkillsInstall(frame requestFrame) {
	if !self.requireAdmin(frame) {
		return
	}
	var parameters skillsInstallParameters
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}
	if parameters.Name == "" {
		self.sendError(frame.ID, 400, "name is required")
		return
	}
	installed, err := skills.Install(self.context, self.api.gateway.Config().SkillsRegistries, parameters.SourceID, parameters.Name, parameters.Version)
	if err != nil {
		self.sendError(frame.ID, 500, "install failed: "+err.Error())
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"installed": installed,
	})
	if self.api.onSkillsChanged != nil {
		self.api.onSkillsChanged()
	}
}

func (self *webSocketConnection) handleSkillsInstalledList(frame requestFrame) {
	if !self.requireAdmin(frame) {
		return
	}
	installed, err := skills.ListInstalled(self.context)
	if err != nil {
		self.sendError(frame.ID, 500, "listing installed skills: "+err.Error())
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"skills": installed,
	})
}

type skillsUninstallParameters struct {
	Name string `json:"name"`
}

func (self *webSocketConnection) handleSkillsUninstall(frame requestFrame) {
	if !self.requireAdmin(frame) {
		return
	}
	var parameters skillsUninstallParameters
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}
	if parameters.Name == "" {
		self.sendError(frame.ID, 400, "name is required")
		return
	}
	if err := skills.Uninstall(self.context, parameters.Name); err != nil {
		self.sendError(frame.ID, 500, "uninstall failed: "+err.Error())
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"uninstalled": true,
	})
	if self.api.onSkillsChanged != nil {
		self.api.onSkillsChanged()
	}
}

type skillsSetEnabledParameters struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

func (self *webSocketConnection) handleSkillsSetEnabled(frame requestFrame) {
	if !self.requireAdmin(frame) {
		return
	}
	var parameters skillsSetEnabledParameters
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}
	if parameters.Name == "" {
		self.sendError(frame.ID, 400, "name is required")
		return
	}
	if err := skills.SetInstalledSkillEnabled(self.context, parameters.Name, parameters.Enabled); err != nil {
		self.sendError(frame.ID, 500, "set enabled failed: "+err.Error())
		return
	}
	installed, err := skills.ListInstalled(self.context)
	if err != nil {
		self.sendError(frame.ID, 500, "listing installed skills: "+err.Error())
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"ok":      true,
		"name":    parameters.Name,
		"enabled": parameters.Enabled,
		"skills":  installed,
	})
	if self.api.onSkillsChanged != nil {
		self.api.onSkillsChanged()
	}
}

type skillsUpdateParameters struct {
	Name string `json:"name,omitempty"`
}

func (self *webSocketConnection) handleSkillsUpdate(frame requestFrame) {
	if !self.requireAdmin(frame) {
		return
	}
	var parameters skillsUpdateParameters
	if frame.Params != nil {
		if err := json.Unmarshal(frame.Params, &parameters); err != nil {
			self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
			return
		}
	}
	updated, err := skills.Update(self.context, self.api.gateway.Config().SkillsRegistries, parameters.Name)
	if err != nil {
		self.sendError(frame.ID, 500, "update failed: "+err.Error())
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"updated": updated,
	})
	if len(updated) > 0 && self.api.onSkillsChanged != nil {
		self.api.onSkillsChanged()
	}
}
