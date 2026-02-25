package fs

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
)

func (self *transaction) ListConversationMessages(conversationId string, options *store.Option) ([]models.ConversationMessage, error) {
	return self.listConversationMessages(conversationId, options)
}

func (self *transaction) CreateConversationMessage(message *models.ConversationMessage, options *store.Option) (*models.ConversationMessage, error) {
	return self.createConversationMessage(message, options)
}

func (self *transaction) GetConversationMessage(messageId string, options *store.Option) (*models.ConversationMessage, error) {
	return self.getConversationMessage(messageId, options)
}

func (self *transaction) ModifyConversationMessage(messageId string, modifier func(*models.ConversationMessage) error, options *store.Option) (*models.ConversationMessage, error) {
	return self.modifyConversationMessage(messageId, modifier, options)
}

func (self *transaction) DeleteConversationMessage(messageId string, options *store.Option) error {
	return self.deleteConversationMessage(messageId, options)
}

func (self *transaction) listConversationMessages(conversationId string, options *store.Option) ([]models.ConversationMessage, error) {
	conversation, err := self.GetConversation(conversationId, options)
	if err != nil {
		return nil, err
	}
	_, messages, loadError := self.loadConversationData(*conversation.UserID, *conversation.AgentID, conversationId)
	if loadError != nil {
		return nil, loadError
	}
	results := make([]models.ConversationMessage, 0, len(messages))
	for index, message := range messages {
		sequence := int64(index + 1)
		results = append(results, conversationRawMessageToModel(conversationId, sequence, message))
	}
	return applyOffsetLimitConversationMessages(results, options), nil
}

func (self *transaction) createConversationMessage(message *models.ConversationMessage, options *store.Option) (*models.ConversationMessage, error) {
	if message == nil || message.ConversationID == nil || message.Role == nil || message.Content == nil {
		return nil, store.ErrInvalidOptions
	}
	conversation, err := self.GetConversation(*message.ConversationID, options)
	if err != nil {
		return nil, err
	}
	conversationPath := self.conversationFilePath(*conversation.UserID, *conversation.AgentID, *message.ConversationID)
	if _, statError := os.Stat(conversationPath); statError != nil {
		if os.IsNotExist(statError) {
			return nil, store.ErrNotFound
		}
		return nil, statError
	}
	timestamp := time.Now().UnixMilli()
	if message.CreatedAt != nil {
		timestamp = message.CreatedAt.UnixMilli()
	}
	appendMessage := conversationFileMessage{
		Role:       string(*message.Role),
		Content:    json.RawMessage(*message.Content),
		Timestamp:  timestamp,
		Metadata:   json.RawMessage(valueOrEmptyBytes(message.Metadata)),
		StopReason: valueOrEmptyStopReason(message.StopReason),
		Model:      valueOrEmpty(message.Model),
		Provider:   valueOrEmpty(message.Provider),
		ToolCallID: valueOrEmpty(message.ToolCallID),
		ToolName:   valueOrEmpty(message.ToolName),
	}
	encodedMessage, marshalError := json.Marshal(appendMessage)
	if marshalError != nil {
		return nil, marshalError
	}
	file, openError := os.OpenFile(conversationPath, os.O_APPEND|os.O_WRONLY, 0644)
	if openError != nil {
		return nil, openError
	}
	defer file.Close()
	if _, writeError := file.Write(append(encodedMessage, '\n')); writeError != nil {
		return nil, writeError
	}

	messages, listError := self.ListConversationMessages(*message.ConversationID, nil)
	if listError != nil || len(messages) == 0 {
		return message, listError
	}
	result := messages[len(messages)-1]
	return &result, nil
}

func (self *transaction) getConversationMessage(messageId string, options *store.Option) (*models.ConversationMessage, error) {
	conversationId, sequence, parseError := parseConversationMessageId(messageId)
	if parseError != nil {
		return nil, store.ErrNotFound
	}
	messages, err := self.ListConversationMessages(conversationId, nil)
	if err != nil {
		return nil, err
	}
	for _, message := range messages {
		if message.Sequence != nil && *message.Sequence == sequence {
			return &message, nil
		}
	}
	return nil, store.ErrNotFound
}

func (self *transaction) modifyConversationMessage(messageId string, modifier func(*models.ConversationMessage) error, options *store.Option) (*models.ConversationMessage, error) {
	conversationId, sequence, parseError := parseConversationMessageId(messageId)
	if parseError != nil {
		return nil, store.ErrNotFound
	}
	conversation, err := self.GetConversation(conversationId, options)
	if err != nil {
		return nil, err
	}
	header, rawMessages, loadError := self.loadConversationData(*conversation.UserID, *conversation.AgentID, conversationId)
	if loadError != nil {
		return nil, loadError
	}
	messageIndex := int(sequence - 1)
	if messageIndex < 0 || messageIndex >= len(rawMessages) {
		return nil, store.ErrNotFound
	}
	message := conversationRawMessageToModel(conversationId, sequence, rawMessages[messageIndex])
	if modifierError := modifier(&message); modifierError != nil {
		return nil, modifierError
	}

	updatedRawMessage := rawMessages[messageIndex]
	if message.Role != nil {
		updatedRawMessage.Role = strings.TrimSpace(string(*message.Role))
	}
	if message.Content != nil {
		updatedRawMessage.Content = json.RawMessage(*message.Content)
	}
	if message.Metadata != nil {
		updatedRawMessage.Metadata = json.RawMessage(*message.Metadata)
	}
	updatedRawMessage.StopReason = valueOrEmptyStopReason(message.StopReason)
	updatedRawMessage.Model = valueOrEmpty(message.Model)
	updatedRawMessage.Provider = valueOrEmpty(message.Provider)
	updatedRawMessage.ToolCallID = valueOrEmpty(message.ToolCallID)
	updatedRawMessage.ToolName = valueOrEmpty(message.ToolName)
	rawMessages[messageIndex] = updatedRawMessage

	if rewriteError := self.rewriteConversationFile(*conversation.UserID, *conversation.AgentID, conversationId, header, rawMessages); rewriteError != nil {
		return nil, rewriteError
	}
	return self.GetConversationMessage(messageId, options)
}

func (self *transaction) deleteConversationMessage(messageId string, options *store.Option) error {
	conversationId, sequence, parseError := parseConversationMessageId(messageId)
	if parseError != nil {
		return store.ErrNotFound
	}
	conversation, err := self.GetConversation(conversationId, options)
	if err != nil {
		return err
	}
	header, rawMessages, loadError := self.loadConversationData(*conversation.UserID, *conversation.AgentID, conversationId)
	if loadError != nil {
		return loadError
	}
	messageIndex := int(sequence - 1)
	if messageIndex < 0 || messageIndex >= len(rawMessages) {
		return store.ErrNotFound
	}
	remainingMessages := append(rawMessages[:messageIndex], rawMessages[messageIndex+1:]...)
	return self.rewriteConversationFile(*conversation.UserID, *conversation.AgentID, conversationId, header, remainingMessages)
}

func conversationRawMessageToModel(conversationId string, sequence int64, message conversationFileMessage) models.ConversationMessage {
	messageId := fmt.Sprintf("%s:%d", conversationId, sequence)
	role := models.Role(message.Role)
	content := []byte(message.Content)
	metadata := []byte(message.Metadata)
	createdAt := time.UnixMilli(message.Timestamp)
	return models.ConversationMessage{
		ID:             messageId,
		ConversationID: &conversationId,
		Role:           &role,
		Content:        &content,
		Metadata:       bytesPointer(metadata),
		ToolCallID:     ptrto.TrimmedString(message.ToolCallID),
		ToolName:       ptrto.TrimmedString(message.ToolName),
		Model:          ptrto.TrimmedString(message.Model),
		Provider:       ptrto.TrimmedString(message.Provider),
		StopReason:     stopReasonPointer(message.StopReason),
		Sequence:       &sequence,
		CreatedAt:      &createdAt,
		ModifiedAt:     &createdAt,
	}
}

func valueOrEmptyBytes(value *[]byte) []byte {
	if value == nil {
		return nil
	}
	valueCopy := make([]byte, len(*value))
	copy(valueCopy, *value)
	return valueCopy
}

func bytesPointer(value []byte) *[]byte {
	if len(value) == 0 {
		return nil
	}
	valueCopy := make([]byte, len(value))
	copy(valueCopy, value)
	return &valueCopy
}

func applyOffsetLimitConversationMessages(values []models.ConversationMessage, options *store.Option) []models.ConversationMessage {
	if options == nil {
		return values
	}
	offset := int(uint64Value(options.Offset))
	if offset >= len(values) {
		return []models.ConversationMessage{}
	}
	values = values[offset:]
	limit := int(uint64Value(options.Limit))
	if limit > 0 && limit < len(values) {
		values = values[:limit]
	}
	return values
}

func stopReasonPointer(value string) *models.StopReason {
	trimmedValue := strings.TrimSpace(value)
	if trimmedValue == "" {
		return nil
	}
	stopReason := models.StopReason(trimmedValue)
	return &stopReason
}

func parseConversationMessageId(messageId string) (string, int64, error) {
	parts := strings.Split(strings.TrimSpace(messageId), ":")
	if len(parts) != 2 {
		return "", 0, fmt.Errorf("invalid message id")
	}
	sequence, parseError := strconv.ParseInt(parts[1], 10, 64)
	if parseError != nil {
		return "", 0, parseError
	}
	return parts[0], sequence, nil
}
