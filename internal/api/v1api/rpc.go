package v1api

import (
	"context"
	"encoding/json"
	"time"

	"github.com/teanode/teanode/internal/agents"
	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/gw"
	"github.com/teanode/teanode/internal/jobs"
	"github.com/teanode/teanode/internal/skills"
	"github.com/teanode/teanode/internal/util/cronexpr"
	"github.com/teanode/teanode/internal/util/ulid"
	"github.com/teanode/teanode/internal/version"
)

// handleConnect: handshake, return capabilities.
func (self *webSocketConnection) handleConnect(frame requestFrame) {
	agentConfigs := self.api.gateway.Config().ResolveAgents()
	activeAgentId := self.api.gateway.ActiveAgentID()
	agentInfos := make([]map[string]interface{}, 0, len(agentConfigs))
	for _, agentConfig := range agentConfigs {
		info := map[string]interface{}{
			"id":                   agentConfig.ID,
			"activeConversationId": self.api.gateway.ActiveConversationID(agentConfig.ID),
		}
		if agentConfig.Name != "" {
			info["name"] = agentConfig.Name
		}
		agentInfos = append(agentInfos, info)
	}

	self.sendResponse(frame.ID, map[string]interface{}{
		"version":              version.Version(),
		"capabilities":         []string{"conversations"},
		"defaultModel":         self.api.gateway.Config().Models.Default,
		"agents":               agentInfos,
		"defaultAgentId":       self.api.gateway.AgentRegistry().DefaultID(),
		"activeAgentId":        activeAgentId,
		"activeConversationId": self.api.gateway.ActiveConversationID(activeAgentId),
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
	activeAgentId := self.api.gateway.ActiveAgentID()
	agentInfos := make([]map[string]interface{}, 0, len(agentConfigs))
	for _, agentConfig := range agentConfigs {
		info := map[string]interface{}{
			"id":                   agentConfig.ID,
			"activeConversationId": self.api.gateway.ActiveConversationID(agentConfig.ID),
		}
		if agentConfig.Name != "" {
			info["name"] = agentConfig.Name
		}
		agentInfos = append(agentInfos, info)
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"agents":         agentInfos,
		"defaultAgentId": self.api.gateway.AgentRegistry().DefaultID(),
		"activeAgentId":  activeAgentId,
	})
}

// agentsSetActiveParameters are the parameters for agents.setActive.
type agentsSetActiveParameters struct {
	AgentID string `json:"agentId"`
}

// handleAgentsSetActive: set the active agent.
func (self *webSocketConnection) handleAgentsSetActive(frame requestFrame) {
	var parameters agentsSetActiveParameters
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}
	if parameters.AgentID == "" {
		self.sendError(frame.ID, 400, "agentId is required")
		return
	}
	if err := self.api.gateway.SetActiveAgent(parameters.AgentID); err != nil {
		self.sendError(frame.ID, 404, err.Error())
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"activeAgentId":        parameters.AgentID,
		"activeConversationId": self.api.gateway.ActiveConversationID(parameters.AgentID),
	})
}

// conversationsSetActiveParameters are the parameters for conversations.setActive.
type conversationsSetActiveParameters struct {
	AgentID        string `json:"agentId"`
	ConversationID string `json:"conversationId"`
}

// handleConversationsSetActive: set the active conversation for an agent.
func (self *webSocketConnection) handleConversationsSetActive(frame requestFrame) {
	var parameters conversationsSetActiveParameters
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
		agentId = self.api.gateway.ActiveAgentID()
	}
	self.api.gateway.SetActiveConversation(agentId, parameters.ConversationID)
	self.sendResponse(frame.ID, map[string]interface{}{
		"activeAgentId":        agentId,
		"activeConversationId": parameters.ConversationID,
	})
}

// conversationSendParameters are the parameters for conversations.send.
type conversationSendParameters struct {
	ConversationID string `json:"conversationId"`
	Message        string `json:"message"`
	Model          string `json:"model,omitempty"`
	AgentID        string `json:"agentId,omitempty"`
	OriginID       string `json:"originId,omitempty"`
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
		AgentID:        parameters.AgentID,
		ConversationID: parameters.ConversationID,
		Message:        parameters.Message,
		Model:          parameters.Model,
		OriginID:       parameters.OriginID,
		Origin:         "webui",
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

	messages, err := runner.Conversations.Load(parameters.ConversationID)
	if err != nil {
		self.sendError(frame.ID, 500, "loading conversation: "+err.Error())
		return
	}

	response := map[string]interface{}{
		"conversationId": parameters.ConversationID,
		"messages":       messages,
	}
	if activeRunId := self.api.gateway.GetActiveRun(parameters.ConversationID); activeRunId != "" {
		response["activeRunId"] = activeRunId
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

	// Resolve the agent ID for active-conversation check.
	resolvedAgentId := parameters.AgentID
	if resolvedAgentId == "" {
		resolvedAgentId = self.api.gateway.ActiveAgentID()
	}
	activeConversationId := self.api.gateway.ActiveConversationID(resolvedAgentId)
	if parameters.ConversationID == activeConversationId {
		self.sendError(frame.ID, 409, "cannot delete the active conversation")
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

	var entries []modelsListEntry
	for providerName, modelList := range models {
		for _, model := range modelList {
			entries = append(entries, modelsListEntry{
				Provider:      providerName,
				ID:            model.ID,
				ContextLength: model.ContextLength,
			})
		}
	}

	self.sendResponse(frame.ID, map[string]interface{}{
		"models":       entries,
		"defaultModel": self.api.gateway.Config().Models.Default,
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
	agents, err := configs.LoadAgents()
	if err != nil {
		self.sendError(frame.ID, 500, "loading agents: "+err.Error())
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"agents": agents,
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
	if parameters.ID == self.api.gateway.AgentRegistry().DefaultID() {
		self.sendError(frame.ID, 409, "cannot delete the default agent")
		return
	}
	if parameters.ID == self.api.gateway.ActiveAgentID() {
		self.sendError(frame.ID, 409, "cannot delete the active agent")
		return
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
		ID:             ulid.GenerateString(),
		Name:           parameters.Name,
		Schedule:       parameters.Schedule,
		Message:        parameters.Message,
		Model:          parameters.Model,
		AgentID:        parameters.AgentID,
		Enabled:        true,
		ConversationID: "", // resolved at execution time from active conversation
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
		job.ConversationID = "" // resolved at execution time from active conversation
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
