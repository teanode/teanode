package v1api

import (
	"encoding/json"

	"github.com/teanode/teanode/internal/pubsub"
	"github.com/teanode/teanode/internal/tools/tab"
)

// --- tab.attach ---

func (self *webSocketConnection) handleTabAttach(frame requestFrame) {
	var params struct {
		AgentID        string `json:"agentId"`
		ConversationID string `json:"conversationId"`
		TabURL         string `json:"tabUrl"`
		TabTitle       string `json:"tabTitle"`
		TabID          int    `json:"tabId"`
	}
	if frame.Params != nil {
		json.Unmarshal(frame.Params, &params)
	}
	if params.AgentID == "" || params.ConversationID == "" {
		self.sendError(frame.ID, 400, "agentId and conversationId are required")
		return
	}
	if params.TabURL == "" {
		self.sendError(frame.ID, 400, "tabUrl is required")
		return
	}

	userId := self.userId()
	if userId == "" {
		self.sendError(frame.ID, 401, "authentication required")
		return
	}

	broker := self.api.coordinator.TabToolBroker()
	connID := self.connID()
	broker.Attach(tab.TabAttachment{
		UserID:         userId,
		AgentID:        params.AgentID,
		ConversationID: params.ConversationID,
		TabURL:         params.TabURL,
		TabTitle:       params.TabTitle,
		TabID:          params.TabID,
	}, connID)

	self.api.pubsub.Broadcast(pubsub.EventTypeTabAttachment, map[string]interface{}{
		"action":         "attached",
		"userId":         userId,
		"agentId":        params.AgentID,
		"conversationId": params.ConversationID,
		"tabUrl":         params.TabURL,
		"tabTitle":       params.TabTitle,
	})

	self.sendResponse(frame.ID, nil)
}

// --- tab.detach ---

func (self *webSocketConnection) handleTabDetach(frame requestFrame) {
	var params struct {
		AgentID        string `json:"agentId"`
		ConversationID string `json:"conversationId"`
	}
	if frame.Params != nil {
		json.Unmarshal(frame.Params, &params)
	}
	if params.AgentID == "" || params.ConversationID == "" {
		self.sendError(frame.ID, 400, "agentId and conversationId are required")
		return
	}

	userId := self.userId()
	broker := self.api.coordinator.TabToolBroker()
	connID := self.connID()

	broker.CancelPendingForAttachment(userId, params.AgentID, params.ConversationID)
	broker.Detach(userId, params.AgentID, params.ConversationID, connID)

	self.api.pubsub.Broadcast(pubsub.EventTypeTabAttachment, map[string]interface{}{
		"action":         "detached",
		"userId":         userId,
		"agentId":        params.AgentID,
		"conversationId": params.ConversationID,
	})

	self.sendResponse(frame.ID, nil)
}

// --- tab.tool_result ---

func (self *webSocketConnection) handleTabToolResult(frame requestFrame) {
	var params struct {
		RequestID string `json:"requestId"`
		Result    string `json:"result"`
		Error     string `json:"error"`
	}
	if frame.Params != nil {
		json.Unmarshal(frame.Params, &params)
	}
	if params.RequestID == "" {
		self.sendError(frame.ID, 400, "requestId is required")
		return
	}

	broker := self.api.coordinator.TabToolBroker()
	if err := broker.Resolve(params.RequestID, tab.ToolCallResult{
		Result: params.Result,
		Error:  params.Error,
	}); err != nil {
		self.sendError(frame.ID, 404, err.Error())
		return
	}

	self.sendResponse(frame.ID, nil)
}

// --- tab.attachments.list ---

func (self *webSocketConnection) handleTabAttachmentsList(frame requestFrame) {
	userId := self.userId()
	broker := self.api.coordinator.TabToolBroker()
	attachments := broker.ListForUser(userId)
	if attachments == nil {
		attachments = []tab.TabAttachment{}
	}
	self.sendResponse(frame.ID, map[string]interface{}{
		"attachments": attachments,
	})
}
