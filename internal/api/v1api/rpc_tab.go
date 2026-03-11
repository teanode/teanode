package v1api

import (
	"encoding/json"

	"github.com/teanode/teanode/internal/integrations/tabs"
	"github.com/teanode/teanode/internal/pubsub"
)

// --- tab.attach ---

func (self *webSocketConnection) handleTabAttach(frame requestFrame) (interface{}, error) {
	var params struct {
		AgentID        string `json:"agentId"`
		ConversationID string `json:"conversationId"`
		TabURL         string `json:"tabUrl"`
		TabTitle       string `json:"tabTitle"`
		TabID          int    `json:"tabId"`
	}
	if frame.Params != nil {
		_ = json.Unmarshal(frame.Params, &params)
	}
	if params.AgentID == "" || params.ConversationID == "" {
		return nil, rpcError(400, "agentId and conversationId are required")
	}
	if params.TabURL == "" {
		return nil, rpcError(400, "tabUrl is required")
	}

	userId := self.userId()
	if userId == "" {
		return nil, rpcError(401, "authentication required")
	}

	broker := self.api.coordinator.TabBroker()
	connectionId := self.connectionId()
	displaced := broker.Attach(tabs.Attachment{
		UserID:         userId,
		AgentID:        params.AgentID,
		ConversationID: params.ConversationID,
		TabURL:         params.TabURL,
		TabTitle:       params.TabTitle,
		TabID:          params.TabID,
	}, connectionId)

	// If a different tab/connection was displaced, cancel its pending calls
	// and notify it before broadcasting the new attachment.
	if displaced != nil {
		broker.CancelPendingForAttachment(userId, params.AgentID, params.ConversationID)
		self.api.pubsub.Broadcast(pubsub.EventTypeTabAttachment, map[string]interface{}{
			"action":         "detached",
			"userId":         userId,
			"agentId":        params.AgentID,
			"conversationId": params.ConversationID,
			"displaced":      true,
			"tabId":          displaced.TabID,
		})
	}

	self.api.pubsub.Broadcast(pubsub.EventTypeTabAttachment, map[string]interface{}{
		"action":         "attached",
		"userId":         userId,
		"agentId":        params.AgentID,
		"conversationId": params.ConversationID,
		"tabUrl":         params.TabURL,
		"tabTitle":       params.TabTitle,
		"tabId":          params.TabID,
	})

	return nil, nil
}

// --- tab.detach ---

func (self *webSocketConnection) handleTabDetach(frame requestFrame) (interface{}, error) {
	var params struct {
		AgentID        string `json:"agentId"`
		ConversationID string `json:"conversationId"`
	}
	if frame.Params != nil {
		_ = json.Unmarshal(frame.Params, &params)
	}
	if params.AgentID == "" || params.ConversationID == "" {
		return nil, rpcError(400, "agentId and conversationId are required")
	}

	userId := self.userId()
	broker := self.api.coordinator.TabBroker()
	connectionId := self.connectionId()

	// Only cancel pending calls and broadcast if this connection actually
	// owned the attachment. This prevents a stale connection's cleanup from
	// disrupting a newer attachment owned by a different connection.
	if broker.Detach(userId, params.AgentID, params.ConversationID, connectionId) {
		broker.CancelPendingForAttachment(userId, params.AgentID, params.ConversationID)

		self.api.pubsub.Broadcast(pubsub.EventTypeTabAttachment, map[string]interface{}{
			"action":         "detached",
			"userId":         userId,
			"agentId":        params.AgentID,
			"conversationId": params.ConversationID,
		})
	}

	return nil, nil
}

// --- tab.commandResult ---

func (self *webSocketConnection) handleTabCommandResult(frame requestFrame) (interface{}, error) {
	var params struct {
		RequestID string `json:"requestId"`
		Result    string `json:"result"`
		Error     string `json:"error"`
	}
	if frame.Params != nil {
		_ = json.Unmarshal(frame.Params, &params)
	}
	if params.RequestID == "" {
		return nil, rpcError(400, "requestId is required")
	}

	broker := self.api.coordinator.TabBroker()
	if err := broker.Resolve(params.RequestID, tabs.ToolCallResult{
		Result: params.Result,
		Error:  params.Error,
	}); err != nil {
		return nil, rpcError(404, err.Error())
	}

	return nil, nil
}
