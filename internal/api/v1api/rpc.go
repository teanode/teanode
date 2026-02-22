package v1api

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/teanode/teanode/internal/agents"
	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/conversations"
	"github.com/teanode/teanode/internal/gw"
	"github.com/teanode/teanode/internal/jobs"
	projectstore "github.com/teanode/teanode/internal/projects"
	"github.com/teanode/teanode/internal/sessions"
	"github.com/teanode/teanode/internal/skills"
	"github.com/teanode/teanode/internal/util/cronexpr"
	"github.com/teanode/teanode/internal/util/security"
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
			"defaultConversationId": self.api.gateway.DefaultConversationID(agentConfig.ID),
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
		"defaultConversationId": self.api.gateway.DefaultConversationID(defaultAgentId),
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
			"defaultConversationId": self.api.gateway.DefaultConversationID(agentConfig.ID),
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
		"defaultConversationId": self.api.gateway.DefaultConversationID(parameters.AgentID),
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
	self.api.gateway.SetDefaultConversation(agentId, parameters.ConversationID)
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

	page, err := runner.Conversations.LoadPage(parameters.ConversationID, limit, parameters.BeforeIndex)
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
	if header, headerError := runner.Conversations.LoadHeader(parameters.ConversationID); headerError == nil {
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
	defaultConversationId := self.api.gateway.DefaultConversationID(resolvedAgentId)
	if parameters.ConversationID == defaultConversationId {
		self.sendError(frame.ID, 409, "cannot delete the default conversation")
		return
	}

	if err := self.api.gateway.DeleteConversation(parameters.AgentID, parameters.ConversationID); err != nil {
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
		conversations, err := runner.Conversations.List()
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
		conversations, err := runner.Conversations.List()
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
	self.sendResponse(frame.ID, map[string]interface{}{
		"schema": configs.ConfigSchema(),
	})
}

// handleConfigGet: return the raw on-disk config.
// Only returns user-specified values, not defaults or environment overrides.
func (self *webSocketConnection) handleConfigGet(frame requestFrame) {
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
	suggestions := map[string][]string{}

	// Collect tool names from the default runner.
	runner := self.api.gateway.AgentRegistry().Default()
	if runner != nil {
		_, _, tools, _, _, _ := runner.Snapshot()
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

// --- Jobs RPC handlers ---

// handleJobsList: list all jobs.
func (self *webSocketConnection) handleJobsList(frame requestFrame) {
	if self.api.gateway.Scheduler() == nil {
		self.sendError(frame.ID, 500, "scheduler not available")
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"jobs": self.api.gateway.Scheduler().List(),
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

	if err := self.api.gateway.Scheduler().CreateAndReload(job); err != nil {
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
	allJobs := self.api.gateway.Scheduler().List()
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

	if err := self.api.gateway.Scheduler().UpdateAndReload(*job); err != nil {
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

	if err := self.api.gateway.Scheduler().DeleteAndReload(parameters.ID); err != nil {
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

	if err := self.api.gateway.Scheduler().Trigger(parameters.ID); err != nil {
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
	if sessionList == nil {
		sessionList = []*sessions.Session{}
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"sessions":         sessionList,
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
	if err := store.Delete(parameters.SessionID); err != nil {
		self.sendError(frame.ID, 404, "session not found: "+parameters.SessionID)
		return
	}

	self.sendResponse(frame.ID, map[string]interface{}{
		"revoked": true,
	})
}

// --- Auth RPC handlers ---

// handleAuthRegenerateToken generates a new random auth token, saves it, and returns it.
func (self *webSocketConnection) handleAuthRegenerateToken(frame requestFrame) {
	token := security.GenerateRandomString(48, security.LowerAlphaNumeric)

	securityConfig := self.api.gateway.SecurityConfig()
	securityConfig.Token = token

	if err := configs.SaveSecurity(securityConfig); err != nil {
		self.sendError(frame.ID, 500, "failed to save security config")
		return
	}

	self.sendResponse(frame.ID, map[string]interface{}{
		"token": token,
	})
}

// handleAuthGetToken returns the current auth token.
func (self *webSocketConnection) handleAuthGetToken(frame requestFrame) {
	securityConfig := self.api.gateway.SecurityConfig()
	self.sendResponse(frame.ID, map[string]interface{}{
		"token": securityConfig.Token,
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

	// If a password is already set, verify the current password.
	if securityConfig.Password != "" {
		if parameters.CurrentPassword == "" {
			self.sendError(frame.ID, 400, "current password is required")
			return
		}
		match, err := security.VerifyPassword([]byte(securityConfig.Password), parameters.CurrentPassword)
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

	securityConfig.Password = string(hash)
	if err := configs.SaveSecurity(securityConfig); err != nil {
		self.sendError(frame.ID, 500, "failed to save security config")
		return
	}

	self.sendResponse(frame.ID, map[string]interface{}{
		"ok": true,
	})
}

func profileToRPCPayload(profile *configs.Profile) map[string]interface{} {
	payload := map[string]interface{}{
		"name":      profile.Name,
		"biography": profile.Bio,
	}
	if strings.TrimSpace(profile.AvatarMediaID) != "" {
		payload["avatarMediaId"] = profile.AvatarMediaID
	}
	return payload
}

func (self *webSocketConnection) handleProfileGet(frame requestFrame) {
	profile, err := self.api.loadProfile()
	if err != nil {
		self.sendError(frame.ID, 500, "failed to load profile")
		return
	}
	self.sendResponse(frame.ID, profileToRPCPayload(profile))
}

type profileUpdateParameters struct {
	Name      string `json:"name"`
	Biography string `json:"biography"`
}

func (self *webSocketConnection) handleProfileUpdate(frame requestFrame) {
	var parameters profileUpdateParameters
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}

	existing, err := self.api.loadProfile()
	if err != nil {
		self.sendError(frame.ID, 500, "failed to load profile")
		return
	}

	profile := &configs.Profile{
		Name:          strings.TrimSpace(parameters.Name),
		Bio:           parameters.Biography,
		AvatarMediaID: strings.TrimSpace(existing.AvatarMediaID),
	}
	if err := configs.SaveProfileOverwriteBio(profile); err != nil {
		self.sendError(frame.ID, 500, "failed to save profile")
		return
	}
	persisted, err := self.api.loadProfile()
	if err != nil {
		self.sendError(frame.ID, 500, "failed to load profile")
		return
	}

	self.sendResponse(frame.ID, profileToRPCPayload(persisted))
}

func (self *webSocketConnection) handleProfileAvatarRemove(frame requestFrame) {
	mediaStore := self.api.gateway.MediaStore()
	if mediaStore == nil {
		self.sendError(frame.ID, 500, "media store not available")
		return
	}
	profile, err := self.api.loadProfile()
	if err != nil {
		self.sendError(frame.ID, 500, "failed to load profile")
		return
	}

	oldAvatarMediaID := profile.AvatarMediaID
	profile.AvatarMediaID = ""
	if err := configs.SaveProfile(profile); err != nil {
		self.sendError(frame.ID, 500, "failed to save profile")
		return
	}
	persisted, err := self.api.loadProfile()
	if err != nil {
		self.sendError(frame.ID, 500, "failed to load profile")
		return
	}
	if oldAvatarMediaID != "" {
		_ = mediaStore.Delete(oldAvatarMediaID)
	}

	self.sendResponse(frame.ID, profileToRPCPayload(persisted))
}

// --- Projects RPC handlers ---

func (self *webSocketConnection) handleProjectsList(frame requestFrame) {
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

func projectRPCError(err error, operation string) (int, string) {
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
		code, message := projectRPCError(err, "creating project")
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
		code, message := projectRPCError(err, "renaming project")
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
		code, message := projectRPCError(err, "deleting project")
		self.sendError(frame.ID, code, message)
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"deleted": true,
	})
}

// --- Skills Registry RPC handlers ---

func (self *webSocketConnection) handleSkillsRegistryList(frame requestFrame) {
	configuration := self.api.gateway.Config()
	self.sendResponse(frame.ID, map[string]interface{}{
		"registries": configuration.SkillsRegistries,
	})
}

type skillsRegistrySearchParameters struct {
	Query string `json:"query,omitempty"`
}

func (self *webSocketConnection) handleSkillsLocalList(frame requestFrame) {
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
