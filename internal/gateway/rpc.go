package gateway

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/cron"
	"github.com/ziyan/teanode/internal/types"
	"github.com/ziyan/teanode/internal/util/deferutil"
)

// handleConnect: handshake, return capabilities.
func (self *webSocketConnection) handleConnect(frame types.RequestFrame) {
	self.sendResponse(frame.ID, map[string]interface{}{
		"version":      "1.0.0",
		"capabilities": []string{"chat"},
		"defaultModel": self.server.Config.Models.Default,
	})
}

// handleHealth: health check.
func (self *webSocketConnection) handleHealth(frame types.RequestFrame) {
	self.sendResponse(frame.ID, map[string]interface{}{
		"status": "ok",
	})
}

// chatSendParameters are the parameters for chat.send.
type chatSendParameters struct {
	SessionKey string `json:"sessionKey"`
	Message    string `json:"message"`
	Model      string `json:"model,omitempty"`
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

		result, err := self.server.Agent.Run(ctx, agent.RunParams{
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

	messages, err := self.server.Sessions.Load(parameters.SessionKey)
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

	if err := self.server.Sessions.Delete(parameters.SessionKey); err != nil {
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

	if err := self.server.Sessions.SetTitle(parameters.SessionKey, parameters.Title); err != nil {
		self.sendError(frame.ID, 500, "renaming session: "+err.Error())
		return
	}

	self.sendResponse(frame.ID, map[string]interface{}{
		"ok": true,
	})
	self.server.Broadcast("sessions", nil)
}

// handleSessionsList: list available sessions.
func (self *webSocketConnection) handleSessionsList(frame types.RequestFrame) {
	sessions, err := self.server.Sessions.List()
	if err != nil {
		self.sendError(frame.ID, 500, "listing sessions: "+err.Error())
		return
	}

	self.sendResponse(frame.ID, map[string]interface{}{
		"sessions": sessions,
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
