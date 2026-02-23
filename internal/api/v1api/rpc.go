package v1api

import (
	"context"
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
	"github.com/teanode/teanode/internal/onboarding"
	projectstore "github.com/teanode/teanode/internal/projects"
	"github.com/teanode/teanode/internal/sessions"
	"github.com/teanode/teanode/internal/skills"
	"github.com/teanode/teanode/internal/util/cronexpr"
	"github.com/teanode/teanode/internal/util/security"
	"github.com/teanode/teanode/internal/util/trash"
	"github.com/teanode/teanode/internal/version"
)

// handleConnect: handshake, return capabilities.
func (self *webSocketConnection) handleConnect(frame requestFrame) {
	agentConfigs := self.api.gateway.Config().ResolveAgents()
	defaultAgentId := self.api.gateway.DefaultAgentID()
	agentInfos := make([]map[string]interface{}, 0, len(agentConfigs))
	for _, agentConfig := range agentConfigs {
		info := map[string]interface{}{
			"id":                    agentConfig.ID,
			"defaultConversationId": self.api.gateway.DefaultConversationID(self.userId, agentConfig.ID),
		}
		if agentConfig.Name != "" {
			info["name"] = agentConfig.Name
		}
		if state, err := configs.LoadAgentState(agentConfig.ID); err == nil && state.AvatarMediaID != "" {
			info["avatarMediaId"] = state.AvatarMediaID
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
		"defaultConversationId": self.api.gateway.DefaultConversationID(self.userId, defaultAgentId),
		"isAdmin":               self.api.gateway.SecurityConfig().IsAdmin(self.userId),
		"userId":                self.userId,
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
	agentConfigs := self.api.gateway.Config().ResolveAgents()
	agentInfos := make([]map[string]interface{}, 0, len(agentConfigs))
	for _, agentConfig := range agentConfigs {
		info := map[string]interface{}{
			"id":                    agentConfig.ID,
			"defaultConversationId": self.api.gateway.DefaultConversationID(self.userId, agentConfig.ID),
		}
		if agentConfig.Name != "" {
			info["name"] = agentConfig.Name
		}
		if state, err := configs.LoadAgentState(agentConfig.ID); err == nil && state.AvatarMediaID != "" {
			info["avatarMediaId"] = state.AvatarMediaID
		}
		agentInfos = append(agentInfos, info)
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"agents":         agentInfos,
		"defaultAgentId": self.api.gateway.DefaultAgentID(),
	})
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
	if err := self.api.gateway.SetDefaultAgent(parameters.AgentID); err != nil {
		self.sendError(frame.ID, 404, err.Error())
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"defaultAgentId":        parameters.AgentID,
		"defaultConversationId": self.api.gateway.DefaultConversationID(self.userId, parameters.AgentID),
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
		agentId = self.api.gateway.DefaultAgentID()
	}
	self.api.gateway.SetDefaultConversation(self.userId, agentId, parameters.ConversationID)
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

	runner := self.api.gateway.ResolveRunner(parameters.AgentID)
	if runner == nil {
		self.sendError(frame.ID, 404, "agent not found: "+parameters.AgentID)
		return
	}

	handle := self.api.gateway.SendMessage(context.Background(), gw.SendMessageParameters{
		UserContext:        &gw.UserContext{UserID: self.userId, SessionID: self.sessionId, AuthMethod: gw.AuthMethodSession},
		AgentID:            parameters.AgentID,
		ConversationID:     parameters.ConversationID,
		Message:            parameters.Message,
		Model:              parameters.Model,
		OriginID:           parameters.OriginID,
		Origin:             "webui",
		OriginSessionID:    self.sessionId,
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

	runner := self.api.gateway.ResolveRunner(parameters.AgentID)
	if runner == nil {
		self.sendError(frame.ID, 404, "agent not found: "+parameters.AgentID)
		return
	}

	limit := parameters.Limit
	if limit <= 0 {
		limit = 50
	}

	store := runner.ConversationsForUser(self.userId)
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
		resolvedAgentId = self.api.gateway.DefaultAgentID()
	}
	defaultConversationId := self.api.gateway.DefaultConversationID(self.userId, resolvedAgentId)
	if parameters.ConversationID == defaultConversationId {
		self.sendError(frame.ID, 409, "cannot delete the default conversation")
		return
	}

	if err := self.api.gateway.DeleteConversation(self.userId, parameters.AgentID, parameters.ConversationID); err != nil {
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
		runner := self.api.gateway.ResolveRunner(parameters.AgentID)
		if runner == nil {
			self.sendError(frame.ID, 404, "agent not found: "+parameters.AgentID)
			return
		}
		conversations, err := runner.ConversationsForUser(self.userId).List()
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
	self.api.gateway.AgentRegistry().ForEach(func(agentId string, runner *agents.Runner) {
		conversations, err := runner.ConversationsForUser(self.userId).List()
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
	models, err := self.api.gateway.LoadModels(context.Background())
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
	// Load raw config from disk (no defaults, no env overrides).
	config, err := configs.LoadRaw()
	if err != nil {
		self.sendError(frame.ID, 500, "loading config: "+err.Error())
		return
	}

	self.sendResponse(frame.ID, map[string]interface{}{
		"config": config,
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

	// Load raw config from disk (no defaults, no env overrides).
	rawConfig, err := configs.LoadRaw()
	if err != nil {
		self.sendError(frame.ID, 500, "loading config: "+err.Error())
		return
	}

	// Round-trip raw config to a map for merging.
	currentData, err := json.Marshal(rawConfig)
	if err != nil {
		self.sendError(frame.ID, 500, "marshalling config: "+err.Error())
		return
	}
	var currentMap map[string]interface{}
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

	// Unmarshal merged map back to Config struct.
	mergedData, err := json.Marshal(currentMap)
	if err != nil {
		self.sendError(frame.ID, 500, "marshalling merged config: "+err.Error())
		return
	}
	var mergedConfig configs.Config
	if err := json.Unmarshal(mergedData, &mergedConfig); err != nil {
		self.sendError(frame.ID, 500, "parsing merged config: "+err.Error())
		return
	}

	// Save to disk. The file watcher will trigger hot-reload.
	if err := configs.Save(&mergedConfig); err != nil {
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

	// Collect tool names from the default runner.
	runner := self.api.gateway.AgentRegistry().Default()
	if runner != nil {
		_, _, tools, _, _ := runner.Snapshot()
		if tools != nil {
			suggestions["tool"] = tools.Names()
		}
	}

	// Collect skill names from the skills directory.
	skillsDirectory, err := configs.SkillsDirectory()
	if err == nil {
		suggestions["skill"] = skills.Names(skillsDirectory)
	}

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
	agents, err := configs.LoadAgents()
	if err != nil {
		self.sendError(frame.ID, 500, "loading agents: "+err.Error())
		return
	}

	entries := make([]map[string]interface{}, 0, len(agents))
	for _, agentConfig := range agents {
		entry := map[string]interface{}{
			"id": agentConfig.ID,
		}
		if agentConfig.Name != "" {
			entry["name"] = agentConfig.Name
		}
		if agentConfig.Model != "" {
			entry["model"] = agentConfig.Model
		}
		if len(agentConfig.Tools) > 0 {
			entry["tools"] = agentConfig.Tools
		}
		if len(agentConfig.Skills) > 0 {
			entry["skills"] = agentConfig.Skills
		}
		if state, stateErr := configs.LoadAgentState(agentConfig.ID); stateErr == nil && state.AvatarMediaID != "" {
			entry["avatarMediaId"] = state.AvatarMediaID
		}
		entries = append(entries, entry)
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
	if err := configs.SaveAgent(parameters.Agent); err != nil {
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
	if parameters.ID == self.api.gateway.DefaultAgentID() {
		self.sendError(frame.ID, 409, "cannot delete the default agent")
		return
	}
	if state, stateErr := configs.LoadAgentState(parameters.ID); stateErr == nil && state.AvatarMediaID != "" && self.api.gateway.MediaStore() != nil {
		_ = self.api.gateway.MediaStore().Delete(state.AvatarMediaID)
	}
	if err := configs.DeleteAgent(parameters.ID); err != nil {
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

	state, err := configs.LoadAgentState(agentId)
	if err != nil {
		self.sendError(frame.ID, 500, "loading agent state: "+err.Error())
		return
	}
	oldAvatarMediaId := strings.TrimSpace(state.AvatarMediaID)
	state.AvatarMediaID = avatarMediaId
	if err := configs.SaveAgentState(agentId, state); err != nil {
		self.sendError(frame.ID, 500, "saving agent state: "+err.Error())
		return
	}
	if oldAvatarMediaId != "" && oldAvatarMediaId != avatarMediaId && self.api.gateway.MediaStore() != nil {
		_ = self.api.gateway.MediaStore().Delete(oldAvatarMediaId)
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

	state, err := configs.LoadAgentState(agentId)
	if err != nil {
		self.sendError(frame.ID, 500, "loading agent state: "+err.Error())
		return
	}
	oldAvatarMediaId := strings.TrimSpace(state.AvatarMediaID)
	state.AvatarMediaID = ""
	if err := configs.SaveAgentState(agentId, state); err != nil {
		self.sendError(frame.ID, 500, "saving agent state: "+err.Error())
		return
	}
	if oldAvatarMediaId != "" && self.api.gateway.MediaStore() != nil {
		_ = self.api.gateway.MediaStore().Delete(oldAvatarMediaId)
	}

	self.sendResponse(frame.ID, map[string]interface{}{
		"ok":            true,
		"avatarMediaId": "",
	})
}

// --- Jobs RPC handlers ---

// handleJobsList: list all jobs.
func (self *webSocketConnection) handleJobsList(frame requestFrame) {
	if self.api.gateway.Scheduler() == nil {
		self.sendError(frame.ID, 500, "scheduler not available")
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"jobs": self.api.gateway.Scheduler().List(self.userId),
	})
}

// jobCreateParameters are the parameters for job.create.
type jobCreateParameters struct {
	Name     string `json:"name"`
	Schedule string `json:"schedule"`
	Message  string `json:"message"`
	Model    string `json:"model,omitempty"`
	AgentID  string `json:"agentId,omitempty"`
}

// handleJobsCreate: create a new job.
func (self *webSocketConnection) handleJobsCreate(frame requestFrame) {
	if self.api.gateway.Scheduler() == nil {
		self.sendError(frame.ID, 500, "scheduler not available")
		return
	}

	var parameters jobCreateParameters
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}
	if parameters.Name == "" {
		self.sendError(frame.ID, 400, "name is required")
		return
	}
	if parameters.Schedule == "" {
		self.sendError(frame.ID, 400, "schedule is required")
		return
	}
	if parameters.Message == "" {
		self.sendError(frame.ID, 400, "message is required")
		return
	}
	if _, err := cronexpr.Parse(parameters.Schedule); err != nil {
		self.sendError(frame.ID, 400, "invalid schedule expression: "+err.Error())
		return
	}

	job := jobs.Job{
		ID:             security.NewULID(),
		Name:           parameters.Name,
		Schedule:       parameters.Schedule,
		Message:        parameters.Message,
		Model:          parameters.Model,
		AgentID:        parameters.AgentID,
		Enabled:        true,
		ConversationID: "", // resolved at execution time from default conversation
		CreatedAt:      time.Now().UnixMilli(),
	}

	if err := self.api.gateway.Scheduler().CreateAndReload(self.userId, job); err != nil {
		self.sendError(frame.ID, 500, "creating job: "+err.Error())
		return
	}

	self.sendResponse(frame.ID, map[string]interface{}{
		"job": job,
	})
}

// jobUpdateParameters are the parameters for job.update.
type jobUpdateParameters struct {
	ID       string `json:"id"`
	Name     string `json:"name,omitempty"`
	Schedule string `json:"schedule,omitempty"`
	Message  string `json:"message,omitempty"`
	Model    string `json:"model,omitempty"`
	Enabled  *bool  `json:"enabled,omitempty"`
	AgentID  string `json:"agentId,omitempty"`
}

// handleJobsUpdate: update a job.
func (self *webSocketConnection) handleJobsUpdate(frame requestFrame) {
	if self.api.gateway.Scheduler() == nil {
		self.sendError(frame.ID, 500, "scheduler not available")
		return
	}

	var parameters jobUpdateParameters
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}
	if parameters.ID == "" {
		self.sendError(frame.ID, 400, "id is required")
		return
	}

	// Find existing job.
	allJobs := self.api.gateway.Scheduler().List(self.userId)
	var job *jobs.Job
	for index := range allJobs {
		if allJobs[index].ID == parameters.ID {
			job = &allJobs[index]
			break
		}
	}
	if job == nil {
		self.sendError(frame.ID, 404, "job not found: "+parameters.ID)
		return
	}

	if parameters.Name != "" {
		job.Name = parameters.Name
	}
	if parameters.Schedule != "" {
		if _, err := cronexpr.Parse(parameters.Schedule); err != nil {
			self.sendError(frame.ID, 400, "invalid schedule expression: "+err.Error())
			return
		}
		job.Schedule = parameters.Schedule
	}
	if parameters.Message != "" {
		job.Message = parameters.Message
	}
	if parameters.Model != "" {
		job.Model = parameters.Model
	}
	if parameters.Enabled != nil {
		job.Enabled = *parameters.Enabled
	}
	if parameters.AgentID != "" && parameters.AgentID != job.AgentID {
		job.AgentID = parameters.AgentID
		job.ConversationID = "" // resolved at execution time from default conversation
	}

	if err := self.api.gateway.Scheduler().UpdateAndReload(self.userId, *job); err != nil {
		self.sendError(frame.ID, 500, "updating job: "+err.Error())
		return
	}

	self.sendResponse(frame.ID, map[string]interface{}{
		"job": job,
	})
}

// jobDeleteParameters are the parameters for job.delete.
type jobDeleteParameters struct {
	ID string `json:"id"`
}

// handleJobsDelete: delete a job.
func (self *webSocketConnection) handleJobsDelete(frame requestFrame) {
	if self.api.gateway.Scheduler() == nil {
		self.sendError(frame.ID, 500, "scheduler not available")
		return
	}

	var parameters jobDeleteParameters
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}
	if parameters.ID == "" {
		self.sendError(frame.ID, 400, "id is required")
		return
	}

	if err := self.api.gateway.Scheduler().DeleteAndReload(self.userId, parameters.ID); err != nil {
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
	if self.api.gateway.Scheduler() == nil {
		self.sendError(frame.ID, 500, "scheduler not available")
		return
	}

	var parameters jobTriggerParameters
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}
	if parameters.ID == "" {
		self.sendError(frame.ID, 400, "id is required")
		return
	}

	if err := self.api.gateway.Scheduler().Trigger(self.userId, parameters.ID); err != nil {
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
	store := self.api.gateway.SessionStore()
	if store == nil {
		self.sendResponse(frame.ID, map[string]interface{}{
			"sessions": []interface{}{},
		})
		return
	}
	sessionList, err := store.List()
	if err != nil {
		self.sendError(frame.ID, 500, "listing sessions: "+err.Error())
		return
	}
	filteredSessions := make([]*sessions.Session, 0, len(sessionList))
	for _, session := range sessionList {
		if session == nil {
			continue
		}
		if session.UserID != self.userId {
			continue
		}
		filteredSessions = append(filteredSessions, session)
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"sessions":         filteredSessions,
		"currentSessionId": self.sessionId,
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
	if parameters.SessionID == self.sessionId {
		self.sendError(frame.ID, 400, "cannot revoke the current session")
		return
	}

	store := self.api.gateway.SessionStore()
	if store == nil {
		self.sendError(frame.ID, 500, "session store not available")
		return
	}

	sessionList, err := store.List()
	if err != nil {
		self.sendError(frame.ID, 500, "listing sessions: "+err.Error())
		return
	}
	ownsSession := false
	for _, session := range sessionList {
		if session != nil && session.ID == parameters.SessionID && session.UserID == self.userId {
			ownsSession = true
			break
		}
	}
	if !ownsSession {
		self.sendError(frame.ID, 404, "session not found: "+parameters.SessionID)
		return
	}
	if err := store.Delete(parameters.SessionID); err != nil {
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

func toAuthTokenListItems(tokens []configs.SecurityToken) []authTokenListItem {
	items := make([]authTokenListItem, 0, len(tokens))
	for _, token := range tokens {
		if strings.TrimSpace(token.ID) == "" || strings.TrimSpace(token.Token) == "" {
			continue
		}
		items = append(items, authTokenListItem{
			ID:         token.ID,
			Token:      token.Token,
			CreatedAt:  token.CreatedAt,
			LastUsedAt: token.LastUsedAt,
		})
	}
	sort.Slice(items, func(left, right int) bool {
		return items[left].CreatedAt.After(items[right].CreatedAt)
	})
	return items
}

func (self *webSocketConnection) handleAuthTokensList(frame requestFrame) {
	securityConfig := self.api.gateway.SecurityConfig()
	user, ok := securityConfig.Users[self.userId]
	if !ok {
		self.sendError(frame.ID, 404, "user not found")
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"tokens": toAuthTokenListItems(user.Tokens),
	})
}

func (self *webSocketConnection) handleAuthTokensCreate(frame requestFrame) {
	tokenValue := security.GenerateRandomString(48, security.LowerAlphaNumeric)

	securityConfig := self.api.gateway.SecurityConfig()
	user, ok := securityConfig.Users[self.userId]
	if !ok {
		self.sendError(frame.ID, 404, "user not found")
		return
	}
	created := configs.SecurityToken{
		ID:        security.NewULID(),
		Token:     tokenValue,
		CreatedAt: time.Now(),
	}
	user.Tokens = append(user.Tokens, created)
	securityConfig.Users[self.userId] = user

	if err := configs.SaveSecurity(securityConfig); err != nil {
		self.sendError(frame.ID, 500, "failed to save security config")
		return
	}

	self.sendResponse(frame.ID, map[string]interface{}{
		"token": authTokenListItem{
			ID:        created.ID,
			Token:     created.Token,
			CreatedAt: created.CreatedAt,
		},
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

	securityConfig := self.api.gateway.SecurityConfig()
	user, ok := securityConfig.Users[self.userId]
	if !ok {
		self.sendError(frame.ID, 404, "user not found")
		return
	}

	filtered := make([]configs.SecurityToken, 0, len(user.Tokens))
	deleted := false
	for _, token := range user.Tokens {
		if token.ID == tokenId {
			deleted = true
			continue
		}
		filtered = append(filtered, token)
	}
	if !deleted {
		self.sendError(frame.ID, 404, "token not found")
		return
	}

	user.Tokens = filtered
	securityConfig.Users[self.userId] = user
	if err := configs.SaveSecurity(securityConfig); err != nil {
		self.sendError(frame.ID, 500, "failed to save security config")
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

	securityConfig := self.api.gateway.SecurityConfig()
	user, ok := securityConfig.Users[self.userId]
	if !ok {
		self.sendError(frame.ID, 404, "user not found")
		return
	}

	// If a password is already set, verify the current password.
	if user.PasswordHash != "" {
		if parameters.CurrentPassword == "" {
			self.sendError(frame.ID, 400, "current password is required")
			return
		}
		match, err := security.VerifyPassword([]byte(user.PasswordHash), parameters.CurrentPassword)
		if err != nil || !match {
			self.sendError(frame.ID, 401, "current password is incorrect")
			return
		}
	}

	hash, err := security.HashPassword(parameters.NewPassword)
	if err != nil {
		self.sendError(frame.ID, 500, "failed to hash password")
		return
	}

	user.PasswordHash = string(hash)
	securityConfig.Users[self.userId] = user
	if err := configs.SaveSecurity(securityConfig); err != nil {
		self.sendError(frame.ID, 500, "failed to save security config")
		return
	}

	self.sendResponse(frame.ID, map[string]interface{}{
		"ok": true,
	})
}

func (self *webSocketConnection) requireAdmin(frame requestFrame) bool {
	if !self.api.gateway.SecurityConfig().IsAdmin(self.userId) {
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
	securityConfig := self.api.gateway.SecurityConfig()
	items := make([]usersListItem, 0, len(securityConfig.Users))
	userIds := make([]string, 0, len(securityConfig.Users))
	for userId := range securityConfig.Users {
		userIds = append(userIds, userId)
	}
	sort.Strings(userIds)
	for _, userId := range userIds {
		user := securityConfig.Users[userId]
		item := usersListItem{
			ID:          userId,
			Username:    user.Username,
			Admin:       user.Admin,
			HasPassword: strings.TrimSpace(user.PasswordHash) != "",
		}
		if profile, err := self.api.loadProfile(userId); err == nil {
			item.Name = strings.TrimSpace(profile.Name)
			item.Description = strings.TrimSpace(profile.Description)
			item.AvatarMediaID = strings.TrimSpace(profile.AvatarMediaID)
		}
		items = append(items, item)
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
	securityConfig := self.api.gateway.SecurityConfig()
	if _, _, exists := securityConfig.FindUserByUsername(username); exists {
		self.sendError(frame.ID, 409, "username already exists")
		return
	}
	hash, err := security.HashPassword(parameters.Password)
	if err != nil {
		self.sendError(frame.ID, 500, "failed to hash password")
		return
	}
	userId := security.NewULID()
	securityConfig.Users[userId] = configs.SecurityUser{
		Username:     username,
		Admin:        false,
		PasswordHash: string(hash),
	}
	if err := configs.SaveSecurity(securityConfig); err != nil {
		self.sendError(frame.ID, 500, "failed to save security config")
		return
	}
	name := strings.TrimSpace(parameters.Name)
	if name == "" {
		name = username
	}
	if err := configs.SaveUserProfile(userId, &configs.UserProfile{
		Name:        name,
		Description: strings.TrimSpace(parameters.Description),
	}); err != nil {
		self.sendError(frame.ID, 500, "failed to save profile")
		return
	}
	if err := onboarding.InitializeUser(self.api.gateway, userId); err != nil {
		self.sendError(frame.ID, 500, "failed to initialize user onboarding")
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"user": usersListItem{
			ID:          userId,
			Username:    username,
			Admin:       false,
			HasPassword: true,
			Name:        name,
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
	if userId == self.userId {
		self.sendError(frame.ID, 400, "cannot delete the current user")
		return
	}
	securityConfig := self.api.gateway.SecurityConfig()
	if _, exists := securityConfig.Users[userId]; !exists {
		self.sendError(frame.ID, 404, "user not found")
		return
	}
	delete(securityConfig.Users, userId)
	if securityConfig.ChannelLinks.Telegram != nil {
		for chatId, linkedUserId := range securityConfig.ChannelLinks.Telegram {
			if linkedUserId == userId {
				delete(securityConfig.ChannelLinks.Telegram, chatId)
			}
		}
	}
	if securityConfig.ChannelLinks.Discord != nil {
		for discordUserId, linkedUserId := range securityConfig.ChannelLinks.Discord {
			if linkedUserId == userId {
				delete(securityConfig.ChannelLinks.Discord, discordUserId)
			}
		}
	}
	if err := configs.SaveSecurity(securityConfig); err != nil {
		self.sendError(frame.ID, 500, "failed to save security config")
		return
	}

	if store := self.api.gateway.SessionStore(); store != nil {
		if sessionList, err := store.List(); err == nil {
			for _, session := range sessionList {
				if session.UserID == userId {
					_ = store.Delete(session.ID)
				}
			}
		}
	}

	if userDirectory, err := configs.UserDirectory(userId); err == nil {
		if _, statErr := os.Stat(userDirectory); statErr == nil {
			if trashDirectory, trashErr := configs.TrashDirectory(); trashErr == nil {
				_ = trash.Move(userDirectory, trashDirectory)
			}
		}
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
	if userId == self.userId {
		self.sendError(frame.ID, 400, "use auth.changePassword for current user")
		return
	}
	if len(parameters.NewPassword) < 8 {
		self.sendError(frame.ID, 400, "new password must be at least 8 characters")
		return
	}

	securityConfig := self.api.gateway.SecurityConfig()
	user, exists := securityConfig.Users[userId]
	if !exists {
		self.sendError(frame.ID, 404, "user not found")
		return
	}
	hash, err := security.HashPassword(parameters.NewPassword)
	if err != nil {
		self.sendError(frame.ID, 500, "failed to hash password")
		return
	}
	user.PasswordHash = string(hash)
	securityConfig.Users[userId] = user
	if err := configs.SaveSecurity(securityConfig); err != nil {
		self.sendError(frame.ID, 500, "failed to save security config")
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
	if userId == self.userId {
		self.sendError(frame.ID, 400, "cannot update the current user from users list")
		return
	}

	securityConfig := self.api.gateway.SecurityConfig()
	user, exists := securityConfig.Users[userId]
	if !exists {
		self.sendError(frame.ID, 404, "user not found")
		return
	}

	if parameters.Username != nil {
		nextUsername := strings.TrimSpace(*parameters.Username)
		if nextUsername == "" {
			self.sendError(frame.ID, 400, "username is required")
			return
		}
		if existingUserId, _, found := securityConfig.FindUserByUsername(nextUsername); found && existingUserId != userId {
			self.sendError(frame.ID, 409, "username already exists")
			return
		}
		user.Username = nextUsername
	}

	if parameters.NewPassword != nil {
		nextPassword := *parameters.NewPassword
		if strings.TrimSpace(nextPassword) != "" {
			if len(nextPassword) < 8 {
				self.sendError(frame.ID, 400, "new password must be at least 8 characters")
				return
			}
			hash, err := security.HashPassword(nextPassword)
			if err != nil {
				self.sendError(frame.ID, 500, "failed to hash password")
				return
			}
			user.PasswordHash = string(hash)
		}
	}

	securityConfig.Users[userId] = user
	if err := configs.SaveSecurity(securityConfig); err != nil {
		self.sendError(frame.ID, 500, "failed to save security config")
		return
	}

	if parameters.Name != nil || parameters.Description != nil {
		profile, err := self.api.loadProfile(userId)
		if err != nil {
			self.sendError(frame.ID, 500, "failed to load profile")
			return
		}
		profileName := strings.TrimSpace(profile.Name)
		if parameters.Name != nil {
			profileName = strings.TrimSpace(*parameters.Name)
			if profileName == "" {
				profileName = user.Username
			}
		}
		profile.Name = profileName
		if parameters.Description != nil {
			profile.Description = strings.TrimSpace(*parameters.Description)
		}
		if err := configs.SaveUserProfile(userId, profile); err != nil {
			self.sendError(frame.ID, 500, "failed to save profile")
			return
		}
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
	if userId == self.userId {
		self.sendError(frame.ID, 400, "cannot change the current user's role")
		return
	}

	securityConfig := self.api.gateway.SecurityConfig()
	user, exists := securityConfig.Users[userId]
	if !exists {
		self.sendError(frame.ID, 404, "user not found")
		return
	}
	user.Admin = parameters.Admin
	securityConfig.Users[userId] = user
	if err := configs.SaveSecurity(securityConfig); err != nil {
		self.sendError(frame.ID, 500, "failed to save security config")
		return
	}

	self.sendResponse(frame.ID, map[string]interface{}{
		"ok":     true,
		"userId": userId,
		"admin":  parameters.Admin,
	})
}

func profileToRpcPayload(profile *configs.UserProfile) map[string]interface{} {
	payload := map[string]interface{}{
		"name": profile.Name,
	}
	if strings.TrimSpace(profile.Description) != "" {
		payload["description"] = profile.Description
	}
	if strings.TrimSpace(profile.AvatarMediaID) != "" {
		payload["avatarMediaId"] = profile.AvatarMediaID
	}
	return payload
}

func (self *webSocketConnection) handleProfileGet(frame requestFrame) {
	profile, err := self.api.loadProfile(self.userId)
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

	existing, err := self.api.loadProfile(self.userId)
	if err != nil {
		self.sendError(frame.ID, 500, "failed to load profile")
		return
	}

	profile := &configs.UserProfile{
		Name:          strings.TrimSpace(existing.Name),
		Description:   strings.TrimSpace(existing.Description),
		AvatarMediaID: strings.TrimSpace(existing.AvatarMediaID),
	}
	if parameters.Name != nil {
		profile.Name = strings.TrimSpace(*parameters.Name)
	}
	if parameters.Description != nil {
		profile.Description = strings.TrimSpace(*parameters.Description)
	}
	if parameters.AvatarMediaID != nil {
		profile.AvatarMediaID = strings.TrimSpace(*parameters.AvatarMediaID)
	}
	if err := configs.SaveUserProfile(self.userId, profile); err != nil {
		self.sendError(frame.ID, 500, "failed to save profile")
		return
	}
	persisted, err := self.api.loadProfile(self.userId)
	if err != nil {
		self.sendError(frame.ID, 500, "failed to load profile")
		return
	}

	self.sendResponse(frame.ID, profileToRpcPayload(persisted))
}

func (self *webSocketConnection) handleProfileAvatarRemove(frame requestFrame) {
	mediaStore := self.api.gateway.MediaStore()
	if mediaStore == nil {
		self.sendError(frame.ID, 500, "media store not available")
		return
	}
	profile, err := self.api.loadProfile(self.userId)
	if err != nil {
		self.sendError(frame.ID, 500, "failed to load profile")
		return
	}

	oldAvatarMediaId := profile.AvatarMediaID
	profile.AvatarMediaID = ""
	if err := configs.SaveUserProfile(self.userId, profile); err != nil {
		self.sendError(frame.ID, 500, "failed to save profile")
		return
	}
	persisted, err := self.api.loadProfile(self.userId)
	if err != nil {
		self.sendError(frame.ID, 500, "failed to load profile")
		return
	}
	if oldAvatarMediaId != "" {
		_ = mediaStore.Delete(oldAvatarMediaId)
	}

	self.sendResponse(frame.ID, profileToRpcPayload(persisted))
}

// --- Projects RPC handlers ---

func (self *webSocketConnection) handleProjectsList(frame requestFrame) {
	if !self.requireAdmin(frame) {
		return
	}
	items, err := projectstore.List()
	if err != nil {
		self.sendError(frame.ID, 500, "listing projects: "+err.Error())
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"projects": items,
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
	item, err := projectstore.Create(parameters.Name, parameters.Description, parameters.Purpose)
	if err != nil {
		code, message := projectRpcError(err, "creating project")
		self.sendError(frame.ID, code, message)
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"project": item,
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
	item, err := projectstore.Rename(parameters.ProjectID, parameters.Name)
	if err != nil {
		code, message := projectRpcError(err, "renaming project")
		self.sendError(frame.ID, code, message)
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"project": item,
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
	if err := projectstore.Delete(parameters.ProjectID); err != nil {
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
	skillsDirectory, err := configs.SkillsDirectory()
	if err != nil {
		self.sendError(frame.ID, 500, "resolving skills directory: "+err.Error())
		return
	}
	definitions, err := skills.ListLocal(skillsDirectory)
	if err != nil {
		self.sendError(frame.ID, 500, "listing local skills: "+err.Error())
		return
	}

	type localSkillSummary struct {
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
		ToolCount   int    `json:"toolCount"`
	}
	result := make([]localSkillSummary, 0, len(definitions))
	for _, definition := range definitions {
		result = append(result, localSkillSummary{
			Name:        definition.Name,
			Description: definition.Description,
			ToolCount:   len(definition.Tools),
		})
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"skills": result,
	})
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
	results, err := skills.Search(context.Background(), self.api.gateway.Config().SkillsRegistries, parameters.Query)
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
	installed, err := skills.Install(context.Background(), self.api.gateway.Config().SkillsRegistries, parameters.SourceID, parameters.Name, parameters.Version)
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
	installed, err := skills.ListInstalled()
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
	if err := skills.Uninstall(parameters.Name); err != nil {
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
	if err := skills.SetInstalledSkillEnabled(parameters.Name, parameters.Enabled); err != nil {
		self.sendError(frame.ID, 500, "set enabled failed: "+err.Error())
		return
	}
	installed, err := skills.ListInstalled()
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
	updated, err := skills.Update(context.Background(), self.api.gateway.Config().SkillsRegistries, parameters.Name)
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
