package gateway

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/teanode/teanode/internal/agent"
	"github.com/teanode/teanode/internal/config"
	"github.com/teanode/teanode/internal/cron"
	"github.com/teanode/teanode/internal/types"
	"github.com/teanode/teanode/internal/util/deferutil"
)

// handleConnect: handshake, return capabilities.
func (self *webSocketConnection) handleConnect(frame types.RequestFrame) {
	agents := self.server.Config.ResolveAgents()
	agentInfos := make([]map[string]interface{}, 0, len(agents))
	for _, agentConfig := range agents {
		agentInfos = append(agentInfos, map[string]interface{}{
			"id": agentConfig.ID,
		})
	}

	self.sendResponse(frame.ID, map[string]interface{}{
		"version":        "1.0.0",
		"capabilities":   []string{"chat"},
		"defaultModel":   self.server.Config.Models.Default,
		"agents":         agentInfos,
		"defaultAgentId": config.DefaultAgentID,
	})
}

// handleHealth: health check.
func (self *webSocketConnection) handleHealth(frame types.RequestFrame) {
	self.sendResponse(frame.ID, map[string]interface{}{
		"status": "ok",
	})
}

// handleAgentsList: return list of configured agents.
func (self *webSocketConnection) handleAgentsList(frame types.RequestFrame) {
	agents := self.server.Config.ResolveAgents()
	agentInfos := make([]map[string]interface{}, 0, len(agents))
	for _, agentConfig := range agents {
		agentInfos = append(agentInfos, map[string]interface{}{
			"id": agentConfig.ID,
		})
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"agents":         agentInfos,
		"defaultAgentId": config.DefaultAgentID,
	})
}

// chatSendParameters are the parameters for chat.send.
type chatSendParameters struct {
	SessionKey string `json:"sessionKey"`
	Message    string `json:"message"`
	Model      string `json:"model,omitempty"`
	AgentID    string `json:"agentId,omitempty"`
}

// handleChatSend: send user message, trigger agent run with streaming.
func (self *webSocketConnection) handleChatSend(frame types.RequestFrame) {
	var parameters chatSendParameters
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}

	if parameters.Message == "" {
		self.sendError(frame.ID, 400, "message is required")
		return
	}

	if parameters.SessionKey == "" {
		parameters.SessionKey = uuid.New().String()
	}

	runner := self.server.resolveRunner(parameters.AgentID)
	if runner == nil {
		self.sendError(frame.ID, 404, "agent not found: "+parameters.AgentID)
		return
	}

	runId := uuid.New().String()
	ctx, cancel := context.WithCancel(context.Background())
	self.runs.Store(runId, cancel)

	// Acknowledge the request immediately with the run ID.
	self.sendResponse(frame.ID, map[string]interface{}{
		"runId":      runId,
		"sessionKey": parameters.SessionKey,
	})

	// Track active run before starting the goroutine.
	self.server.SetActiveRun(parameters.SessionKey, runId)

	// Run agent in background goroutine.
	go func() {
		defer deferutil.Recover()
		defer func() {
			self.server.ClearActiveRun(parameters.SessionKey, runId)
			self.runs.Delete(runId)
			cancel()
		}()

		result, err := runner.Run(ctx, agent.RunParams{
			SessionKey: parameters.SessionKey,
			Message:    parameters.Message,
			Model:      parameters.Model,
		}, &agent.RunCallbacks{
			OnTextDelta: func(text string) {
				self.server.Broadcast("chat", map[string]interface{}{
					"state":      "delta",
					"runId":      runId,
					"sessionKey": parameters.SessionKey,
					"text":       text,
				})
			},
			OnToolCall: func(toolName string, arguments string) {
				self.server.Broadcast("chat", map[string]interface{}{
					"state":      "tool_call",
					"runId":      runId,
					"sessionKey": parameters.SessionKey,
					"toolName":   toolName,
					"arguments":  arguments,
				})
			},
			OnToolResult: func(toolName string, result string) {
				self.server.Broadcast("chat", map[string]interface{}{
					"state":      "tool_result",
					"runId":      runId,
					"sessionKey": parameters.SessionKey,
					"toolName":   toolName,
					"result":     result,
				})
			},
			OnTitleUpdate: func(title string) {
				self.server.Broadcast("chat", map[string]interface{}{
					"state":      "title",
					"sessionKey": parameters.SessionKey,
					"title":      title,
				})
				self.server.Broadcast("sessions", nil)
			},
		})

		if err != nil {
			if ctx.Err() != nil {
				self.server.Broadcast("chat", map[string]interface{}{
					"state":      "aborted",
					"runId":      runId,
					"sessionKey": parameters.SessionKey,
				})
				return
			}
			log.Errorf("agent run error: %v", err)
			self.server.Broadcast("chat", map[string]interface{}{
				"state":      "error",
				"runId":      runId,
				"sessionKey": parameters.SessionKey,
				"error":      err.Error(),
			})
			return
		}

		payload := map[string]interface{}{
			"state":      "final",
			"runId":      runId,
			"sessionKey": parameters.SessionKey,
			"text":       result.Response,
			"model":      result.Model,
			"stopReason": result.StopReason,
		}
		if result.Usage != nil {
			payload["usage"] = result.Usage
		}
		self.server.Broadcast("chat", payload)
	}()
}

// chatHistoryParameters are the parameters for chat.history.
type chatHistoryParameters struct {
	SessionKey string `json:"sessionKey"`
	AgentID    string `json:"agentId,omitempty"`
}

// handleChatHistory: return session transcript.
func (self *webSocketConnection) handleChatHistory(frame types.RequestFrame) {
	var parameters chatHistoryParameters
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}

	if parameters.SessionKey == "" {
		self.sendError(frame.ID, 400, "sessionKey is required")
		return
	}

	runner := self.server.resolveRunner(parameters.AgentID)
	if runner == nil {
		self.sendError(frame.ID, 404, "agent not found: "+parameters.AgentID)
		return
	}

	messages, err := runner.Sessions.Load(parameters.SessionKey)
	if err != nil {
		self.sendError(frame.ID, 500, "loading session: "+err.Error())
		return
	}

	response := map[string]interface{}{
		"sessionKey": parameters.SessionKey,
		"messages":   messages,
	}
	if activeRunId := self.server.GetActiveRun(parameters.SessionKey); activeRunId != "" {
		response["activeRunId"] = activeRunId
	}
	self.sendResponse(frame.ID, response)
}

// chatAbortParameters are the parameters for chat.abort.
type chatAbortParameters struct {
	RunID string `json:"runId"`
}

// handleChatAbort: cancel a running agent.
func (self *webSocketConnection) handleChatAbort(frame types.RequestFrame) {
	var parameters chatAbortParameters
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}

	if cancelFn, ok := self.runs.Load(parameters.RunID); ok {
		cancelFn.(context.CancelFunc)()
		self.sendResponse(frame.ID, map[string]interface{}{
			"aborted": true,
		})
	} else {
		self.sendError(frame.ID, 404, "run not found: "+parameters.RunID)
	}
}

// sessionsDeleteParameters are the parameters for sessions.delete.
type sessionsDeleteParameters struct {
	SessionKey string `json:"sessionKey"`
	AgentID    string `json:"agentId,omitempty"`
}

// handleSessionsDelete: delete a session.
func (self *webSocketConnection) handleSessionsDelete(frame types.RequestFrame) {
	var parameters sessionsDeleteParameters
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}

	if parameters.SessionKey == "" {
		self.sendError(frame.ID, 400, "sessionKey is required")
		return
	}

	runner := self.server.resolveRunner(parameters.AgentID)
	if runner == nil {
		self.sendError(frame.ID, 404, "agent not found: "+parameters.AgentID)
		return
	}

	if err := runner.Sessions.Delete(parameters.SessionKey); err != nil {
		self.sendError(frame.ID, 500, "deleting session: "+err.Error())
		return
	}

	self.sendResponse(frame.ID, map[string]interface{}{
		"deleted": true,
	})
	self.server.Broadcast("sessions", nil)
}

// sessionsRenameParameters are the parameters for sessions.rename.
type sessionsRenameParameters struct {
	SessionKey string `json:"sessionKey"`
	Title      string `json:"title"`
	AgentID    string `json:"agentId,omitempty"`
}

// handleSessionsRename: rename a session title.
func (self *webSocketConnection) handleSessionsRename(frame types.RequestFrame) {
	var parameters sessionsRenameParameters
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}

	if parameters.SessionKey == "" {
		self.sendError(frame.ID, 400, "sessionKey is required")
		return
	}

	runner := self.server.resolveRunner(parameters.AgentID)
	if runner == nil {
		self.sendError(frame.ID, 404, "agent not found: "+parameters.AgentID)
		return
	}

	if err := runner.Sessions.SetTitle(parameters.SessionKey, parameters.Title); err != nil {
		self.sendError(frame.ID, 500, "renaming session: "+err.Error())
		return
	}

	self.sendResponse(frame.ID, map[string]interface{}{
		"ok": true,
	})
	self.server.Broadcast("sessions", nil)
}

// sessionsListParameters are the parameters for sessions.list.
type sessionsListParameters struct {
	AgentID string `json:"agentId,omitempty"`
}

// handleSessionsList: list available sessions.
func (self *webSocketConnection) handleSessionsList(frame types.RequestFrame) {
	var parameters sessionsListParameters
	if frame.Params != nil {
		json.Unmarshal(frame.Params, &parameters)
	}

	if parameters.AgentID != "" {
		// List sessions for a specific agent.
		runner := self.server.resolveRunner(parameters.AgentID)
		if runner == nil {
			self.sendError(frame.ID, 404, "agent not found: "+parameters.AgentID)
			return
		}
		sessions, err := runner.Sessions.List()
		if err != nil {
			self.sendError(frame.ID, 500, "listing sessions: "+err.Error())
			return
		}
		self.sendResponse(frame.ID, map[string]interface{}{
			"sessions": sessions,
		})
		return
	}

	// Aggregate sessions from all agents.
	type sessionWithAgent struct {
		Key        string `json:"key"`
		LastActive int64  `json:"lastActive"`
		Title      string `json:"title,omitempty"`
		AgentID    string `json:"agentId,omitempty"`
	}

	var allSessions []sessionWithAgent
	self.server.AgentRegistry.ForEach(func(agentId string, runner *agent.Runner) {
		sessions, err := runner.Sessions.List()
		if err != nil {
			return
		}
		for _, sessionInfo := range sessions {
			allSessions = append(allSessions, sessionWithAgent{
				Key:        sessionInfo.Key,
				LastActive: sessionInfo.LastActive,
				Title:      sessionInfo.Title,
				AgentID:    agentId,
			})
		}
	})

	self.sendResponse(frame.ID, map[string]interface{}{
		"sessions": allSessions,
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
func (self *webSocketConnection) handleModelsList(frame types.RequestFrame) {
	models, err := self.server.loadModels(context.Background())
	if err != nil {
		self.sendError(frame.ID, 500, "loading models: "+err.Error())
		return
	}

	var entries []modelsListEntry
	for providerName, modelList := range models {
		for _, m := range modelList {
			entries = append(entries, modelsListEntry{
				Provider:      providerName,
				ID:            m.ID,
				ContextLength: m.ContextLength,
			})
		}
	}

	self.sendResponse(frame.ID, map[string]interface{}{
		"models":       entries,
		"defaultModel": self.server.Config.Models.Default,
	})
}

// --- Config RPC handlers ---

// handleConfigSchema: return the config schema for UI form generation.
func (self *webSocketConnection) handleConfigSchema(frame types.RequestFrame) {
	self.sendResponse(frame.ID, map[string]interface{}{
		"schema": config.ConfigSchema(),
	})
}

// handleConfigGet: return the current config with sensitive fields masked.
func (self *webSocketConnection) handleConfigGet(frame types.RequestFrame) {
	// Deep-copy via JSON round-trip so we can mask fields.
	data, err := json.Marshal(self.server.Config)
	if err != nil {
		self.sendError(frame.ID, 500, "marshalling config: "+err.Error())
		return
	}
	var configCopy map[string]interface{}
	if err := json.Unmarshal(data, &configCopy); err != nil {
		self.sendError(frame.ID, 500, "copying config: "+err.Error())
		return
	}

	// Mask sensitive fields: replace non-empty strings with "********".
	maskField(configCopy, "models", "apiKey")
	maskField(configCopy, "tools", "braveApiKey")
	maskNested(configCopy, "gateway", "auth", "token")
	maskNested(configCopy, "gateway", "auth", "password")
	maskField(configCopy, "discord", "token")
	maskField(configCopy, "telegram", "token")

	// Mask provider API keys.
	if models, ok := configCopy["models"].(map[string]interface{}); ok {
		if providers, ok := models["providers"].(map[string]interface{}); ok {
			for _, providerValue := range providers {
				if provider, ok := providerValue.(map[string]interface{}); ok {
					if apiKey, ok := provider["apiKey"].(string); ok && apiKey != "" {
						provider["apiKey"] = "********"
					}
				}
			}
		}
	}

	self.sendResponse(frame.ID, map[string]interface{}{
		"config": configCopy,
	})
}

// maskField replaces a non-empty string field inside a top-level object with "********".
func maskField(root map[string]interface{}, section string, field string) {
	if sectionMap, ok := root[section].(map[string]interface{}); ok {
		if value, ok := sectionMap[field].(string); ok && value != "" {
			sectionMap[field] = "********"
		}
	}
}

// maskNested replaces a non-empty string field inside a doubly-nested object with "********".
func maskNested(root map[string]interface{}, section string, subsection string, field string) {
	if sectionMap, ok := root[section].(map[string]interface{}); ok {
		if subMap, ok := sectionMap[subsection].(map[string]interface{}); ok {
			if value, ok := subMap[field].(string); ok && value != "" {
				subMap[field] = "********"
			}
		}
	}
}

// configUpdateParameters are the parameters for config.update.
type configUpdateParameters struct {
	Config json.RawMessage `json:"config"`
}

// handleConfigUpdate: merge a partial config into the current config and save.
func (self *webSocketConnection) handleConfigUpdate(frame types.RequestFrame) {
	var parameters configUpdateParameters
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}

	// Load fresh config from disk to merge into.
	currentConfig, err := config.Load()
	if err != nil {
		self.sendError(frame.ID, 500, "loading config: "+err.Error())
		return
	}

	// Round-trip current config to a map for merging.
	currentData, err := json.Marshal(currentConfig)
	if err != nil {
		self.sendError(frame.ID, 500, "marshalling config: "+err.Error())
		return
	}
	var currentMap map[string]interface{}
	if err := json.Unmarshal(currentData, &currentMap); err != nil {
		self.sendError(frame.ID, 500, "parsing config: "+err.Error())
		return
	}

	// Parse the incoming partial config.
	var partialMap map[string]interface{}
	if err := json.Unmarshal(parameters.Config, &partialMap); err != nil {
		self.sendError(frame.ID, 400, "invalid config object: "+err.Error())
		return
	}

	// Strip masked values so we don't overwrite real secrets with "********".
	stripMasked(partialMap)

	// Deep merge: recursively merge maps so nested secrets are preserved.
	deepMerge(currentMap, partialMap)

	// Unmarshal merged map back to Config struct.
	mergedData, err := json.Marshal(currentMap)
	if err != nil {
		self.sendError(frame.ID, 500, "marshalling merged config: "+err.Error())
		return
	}
	var mergedConfig config.Config
	if err := json.Unmarshal(mergedData, &mergedConfig); err != nil {
		self.sendError(frame.ID, 500, "parsing merged config: "+err.Error())
		return
	}

	// Save to disk. The file watcher will trigger hot-reload.
	if err := config.Save(&mergedConfig); err != nil {
		self.sendError(frame.ID, 500, "saving config: "+err.Error())
		return
	}

	self.sendResponse(frame.ID, map[string]interface{}{
		"ok": true,
	})
}

// stripMasked recursively removes "********" values from a map so they
// don't overwrite real secrets.
func stripMasked(object map[string]interface{}) {
	for key, value := range object {
		switch typed := value.(type) {
		case string:
			if typed == "********" {
				delete(object, key)
			}
		case map[string]interface{}:
			stripMasked(typed)
		}
	}
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
func (self *webSocketConnection) handleAgentsConfigSchema(frame types.RequestFrame) {
	self.sendResponse(frame.ID, map[string]interface{}{
		"schema": config.AgentConfigSchema(),
	})
}

// handleAgentsConfigList: return all agent configs from per-agent files.
func (self *webSocketConnection) handleAgentsConfigList(frame types.RequestFrame) {
	agents, err := config.LoadAgents()
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
	Agent config.AgentConfig `json:"agent"`
}

// handleAgentsConfigSave: save a single agent config to its per-agent file.
func (self *webSocketConnection) handleAgentsConfigSave(frame types.RequestFrame) {
	var parameters agentsConfigSaveParameters
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}
	if parameters.Agent.ID == "" {
		self.sendError(frame.ID, 400, "agent id is required")
		return
	}
	if err := config.SaveAgent(parameters.Agent); err != nil {
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
func (self *webSocketConnection) handleAgentsConfigDelete(frame types.RequestFrame) {
	var parameters agentsConfigDeleteParameters
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}
	if parameters.ID == "" {
		self.sendError(frame.ID, 400, "id is required")
		return
	}
	if err := config.DeleteAgent(parameters.ID); err != nil {
		self.sendError(frame.ID, 500, "deleting agent: "+err.Error())
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"deleted": true,
	})
}

// --- Cron RPC handlers ---

// handleCronsList: list all cron jobs.
func (self *webSocketConnection) handleCronsList(frame types.RequestFrame) {
	if self.server.Scheduler == nil {
		self.sendError(frame.ID, 500, "scheduler not available")
		return
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"jobs": self.server.Scheduler.List(),
	})
}

// cronsCreateParameters are the parameters for crons.create.
type cronsCreateParameters struct {
	Name     string `json:"name"`
	Schedule string `json:"schedule"`
	Message  string `json:"message"`
	Model    string `json:"model,omitempty"`
	AgentID  string `json:"agentId,omitempty"`
}

// handleCronsCreate: create a new cron job.
func (self *webSocketConnection) handleCronsCreate(frame types.RequestFrame) {
	if self.server.Scheduler == nil {
		self.sendError(frame.ID, 500, "scheduler not available")
		return
	}

	var parameters cronsCreateParameters
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
	if _, err := cron.Parse(parameters.Schedule); err != nil {
		self.sendError(frame.ID, 400, "invalid cron expression: "+err.Error())
		return
	}

	job := cron.CronJob{
		ID:         uuid.New().String()[:8],
		Name:       parameters.Name,
		Schedule:   parameters.Schedule,
		Message:    parameters.Message,
		Model:      parameters.Model,
		AgentID:    parameters.AgentID,
		Enabled:    true,
		SessionKey: cron.GenerateSessionKey(parameters.Name),
		CreatedAt:  time.Now().UnixMilli(),
	}

	if err := self.server.Scheduler.CreateAndReload(job); err != nil {
		self.sendError(frame.ID, 500, "creating job: "+err.Error())
		return
	}

	self.sendResponse(frame.ID, map[string]interface{}{
		"job": job,
	})
}

// cronsUpdateParameters are the parameters for crons.update.
type cronsUpdateParameters struct {
	ID       string `json:"id"`
	Name     string `json:"name,omitempty"`
	Schedule string `json:"schedule,omitempty"`
	Message  string `json:"message,omitempty"`
	Model    string `json:"model,omitempty"`
	Enabled  *bool  `json:"enabled,omitempty"`
	AgentID  string `json:"agentId,omitempty"`
}

// handleCronsUpdate: update a cron job.
func (self *webSocketConnection) handleCronsUpdate(frame types.RequestFrame) {
	if self.server.Scheduler == nil {
		self.sendError(frame.ID, 500, "scheduler not available")
		return
	}

	var parameters cronsUpdateParameters
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}
	if parameters.ID == "" {
		self.sendError(frame.ID, 400, "id is required")
		return
	}

	// Find existing job.
	jobs := self.server.Scheduler.List()
	var job *cron.CronJob
	for i := range jobs {
		if jobs[i].ID == parameters.ID {
			job = &jobs[i]
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
		if _, err := cron.Parse(parameters.Schedule); err != nil {
			self.sendError(frame.ID, 400, "invalid cron expression: "+err.Error())
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
	if parameters.AgentID != "" {
		job.AgentID = parameters.AgentID
	}

	if err := self.server.Scheduler.UpdateAndReload(*job); err != nil {
		self.sendError(frame.ID, 500, "updating job: "+err.Error())
		return
	}

	self.sendResponse(frame.ID, map[string]interface{}{
		"job": job,
	})
}

// cronsDeleteParameters are the parameters for crons.delete.
type cronsDeleteParameters struct {
	ID string `json:"id"`
}

// handleCronsDelete: delete a cron job.
func (self *webSocketConnection) handleCronsDelete(frame types.RequestFrame) {
	if self.server.Scheduler == nil {
		self.sendError(frame.ID, 500, "scheduler not available")
		return
	}

	var parameters cronsDeleteParameters
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}
	if parameters.ID == "" {
		self.sendError(frame.ID, 400, "id is required")
		return
	}

	if err := self.server.Scheduler.DeleteAndReload(parameters.ID); err != nil {
		self.sendError(frame.ID, 500, "deleting job: "+err.Error())
		return
	}

	self.sendResponse(frame.ID, map[string]interface{}{
		"deleted": true,
	})
}

// cronsTriggerParameters are the parameters for crons.trigger.
type cronsTriggerParameters struct {
	ID string `json:"id"`
}

// handleCronsTrigger: manually trigger a cron job.
func (self *webSocketConnection) handleCronsTrigger(frame types.RequestFrame) {
	if self.server.Scheduler == nil {
		self.sendError(frame.ID, 500, "scheduler not available")
		return
	}

	var parameters cronsTriggerParameters
	if err := json.Unmarshal(frame.Params, &parameters); err != nil {
		self.sendError(frame.ID, 400, "invalid parameters: "+err.Error())
		return
	}
	if parameters.ID == "" {
		self.sendError(frame.ID, 400, "id is required")
		return
	}

	if err := self.server.Scheduler.Trigger(parameters.ID); err != nil {
		self.sendError(frame.ID, 500, "triggering job: "+err.Error())
		return
	}

	self.sendResponse(frame.ID, map[string]interface{}{
		"triggered": true,
	})
}
