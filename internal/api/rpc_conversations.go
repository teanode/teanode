package api

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/teanode/teanode/internal/coordinators"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/pubsub"
	"github.com/teanode/teanode/internal/runners"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
)

// conversationsSetDefaultParameters are the parameters for conversations.setDefault.
type conversationsSetDefaultParameters struct {
	AgentID        string `json:"agentId"`
	ConversationID string `json:"conversationId"`
}

// handleConversationsSetDefault: set the default conversation for an agent.
func (self *webSocketConnection) handleConversationsSetDefault(frame requestFrame) (interface{}, error) {
	parameters, err := unmarshalParams[conversationsSetDefaultParameters](frame)
	if err != nil {
		return nil, err
	}
	if parameters.ConversationID == "" {
		return nil, rpcError(400, "conversationId is required")
	}
	agentId := parameters.AgentID
	if agentId == "" {
		agentId = self.defaultAgentId()
	}
	self.api.coordinator.SetDefaultConversation(self.userId(), agentId, parameters.ConversationID)
	return map[string]interface{}{
		"defaultAgentId":        agentId,
		"defaultConversationId": parameters.ConversationID,
	}, nil
}

// conversationSendParameters are the parameters for conversations.send.
type conversationSendParameters struct {
	ConversationID    string              `json:"conversationId"`
	Message           string              `json:"message"`
	ProviderModelName string              `json:"providerModelName,omitempty"`
	AgentID           string              `json:"agentId,omitempty"`
	OriginID          string              `json:"originId,omitempty"`
	Attachments       []map[string]string `json:"attachments,omitempty"`
	VoiceMode         string              `json:"voiceMode,omitempty"`
}

// handleConversationsSend: send user message, trigger agent run via node.
func (self *webSocketConnection) handleConversationsSend(frame requestFrame) (interface{}, error) {
	parameters, err := unmarshalParams[conversationSendParameters](frame)
	if err != nil {
		return nil, err
	}

	if parameters.Message == "" {
		return nil, rpcError(400, "message is required")
	}

	if parameters.AgentID == "" {
		parameters.AgentID = self.defaultAgentId()
	}

	handle, sendError := self.api.coordinator.Run(self.ctx, coordinators.RunParameters{
		AgentID:           parameters.AgentID,
		ConversationID:    parameters.ConversationID,
		Message:           parameters.Message,
		ProviderModelName: parameters.ProviderModelName,
		OriginID:          parameters.OriginID,
		Origin:            runners.OriginWeb,
		OriginSessionID:   self.sessionId(),
		Attachments:       parameters.Attachments,
		VoiceMode:         runners.VoiceMode(parameters.VoiceMode),
	}, nil)
	if sendError != nil {
		return nil, rpcError(500, sendError.Error())
	}

	return map[string]interface{}{
		"runId":          handle.RunID,
		"conversationId": handle.ConversationID,
	}, nil
}

// conversationHistoryParameters are the parameters for conversations.history.
type conversationHistoryParameters struct {
	ConversationID string `json:"conversationId"`
	AgentID        string `json:"agentId,omitempty"`
	Limit          int    `json:"limit,omitempty"`
	BeforeIndex    int    `json:"beforeIndex,omitempty"`
}

// handleConversationsHistory: return conversation transcript.
func (self *webSocketConnection) handleConversationsHistory(frame requestFrame) (interface{}, error) {
	parameters, err := unmarshalParams[conversationHistoryParameters](frame)
	if err != nil {
		return nil, err
	}

	if parameters.ConversationID == "" {
		return nil, rpcError(400, "conversationId is required")
	}

	if parameters.AgentID == "" {
		parameters.AgentID = self.defaultAgentId()
	}

	// Verify the requesting user owns this conversation.
	if err := self.verifyConversationOwnership(parameters.ConversationID); err != nil {
		return nil, rpcError(404, "conversation not found")
	}

	limit := parameters.Limit
	if limit <= 0 {
		limit = 50
	}

	messages, err := listConversationMessages(self.ctx, parameters.ConversationID)
	if err != nil {
		return nil, rpcError(500, "loading conversation: "+err.Error())
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
	return response, nil
}

// conversationAbortParameters are the parameters for conversations.abort.
type conversationAbortParameters struct {
	RunID          string `json:"runId"`
	ConversationID string `json:"conversationId,omitempty"`
}

// handleConversationsAbort: cancel a running agent. Works cross-tab and cross-channel.
func (self *webSocketConnection) handleConversationsAbort(frame requestFrame) (interface{}, error) {
	parameters, err := unmarshalParams[conversationAbortParameters](frame)
	if err != nil {
		return nil, err
	}

	if parameters.RunID == "" && parameters.ConversationID == "" {
		return nil, rpcError(400, "runId or conversationId is required")
	}

	if parameters.RunID != "" && self.api.coordinator.AbortRun(parameters.RunID) {
		return map[string]interface{}{
			"aborted": true,
		}, nil
	}

	if parameters.ConversationID != "" {
		if err := self.verifyConversationOwnership(parameters.ConversationID); err != nil {
			return nil, rpcError(404, "conversation not found")
		}
		if self.api.coordinator.AbortConversationRun(parameters.ConversationID) {
			return map[string]interface{}{
				"aborted": true,
			}, nil
		}
		return nil, rpcError(404, "conversation has no active run: "+parameters.ConversationID)
	}

	return nil, rpcError(404, "run not found: "+parameters.RunID)
}

// conversationsDeleteParameters are the parameters for conversations.delete.
type conversationsDeleteParameters struct {
	ConversationID string `json:"conversationId"`
	AgentID        string `json:"agentId,omitempty"`
}

// handleConversationsDelete: delete a conversation.
func (self *webSocketConnection) handleConversationsDelete(frame requestFrame) (interface{}, error) {
	parameters, err := unmarshalParams[conversationsDeleteParameters](frame)
	if err != nil {
		return nil, err
	}

	if parameters.ConversationID == "" {
		return nil, rpcError(400, "conversationId is required")
	}

	// Resolve the agent ID for default-conversation check.
	resolvedAgentId := parameters.AgentID
	if resolvedAgentId == "" {
		resolvedAgentId = self.defaultAgentId()
	}
	defaultConversationId := self.api.coordinator.EnsureDefaultConversation(self.userId(), resolvedAgentId)
	if parameters.ConversationID == defaultConversationId {
		return nil, rpcError(409, "cannot delete the default conversation")
	}

	// Verify the requesting user owns this conversation.
	if err := self.verifyConversationOwnership(parameters.ConversationID); err != nil {
		return nil, rpcError(404, "conversation not found")
	}

	if self.api.coordinator.GetActiveConversationRunner(parameters.ConversationID) != nil {
		return nil, rpcError(500, "conversation has active run")
	}

	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		return transaction.DeleteConversation(ctx, parameters.ConversationID, nil)
	}); err != nil && err != store.ErrNotFound {
		return nil, rpcError(500, "deleting conversation: "+err.Error())
	}
	self.api.pubsub.Broadcast(pubsub.EventTypeConversations, nil)

	return map[string]interface{}{
		"deleted": true,
	}, nil
}

// conversationsListParameters are the parameters for conversations.list.
type conversationsListParameters struct {
	AgentID string `json:"agentId,omitempty"`
	Limit   int    `json:"limit,omitempty"`
}

// handleConversationsList: list available conversations.
func (self *webSocketConnection) handleConversationsList(frame requestFrame) (interface{}, error) {
	var parameters conversationsListParameters
	if frame.Params != nil {
		_ = json.Unmarshal(frame.Params, &parameters)
	}

	if parameters.AgentID != "" {
		// List conversations for a specific agent.
		conversationList, err := listConversations(self.ctx, self.userId(), parameters.AgentID)
		if err != nil {
			return nil, rpcError(500, "listing conversations: "+err.Error())
		}
		conversationPayload := marshalConversationList(conversationList)
		sort.Slice(conversationPayload, func(leftIndex, rightIndex int) bool {
			return conversationPayload[leftIndex]["lastActive"].(int64) > conversationPayload[rightIndex]["lastActive"].(int64)
		})
		if parameters.Limit > 0 && len(conversationPayload) > parameters.Limit {
			conversationPayload = conversationPayload[:parameters.Limit]
		}
		return map[string]interface{}{
			"conversations": conversationPayload,
		}, nil
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
		return nil, rpcError(500, "listing agents: "+agentsListError.Error())
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

	return map[string]interface{}{
		"conversations": allConversations,
	}, nil
}

// --- Conversation helpers ---

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
