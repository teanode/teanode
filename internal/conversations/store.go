package conversations

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
)

// Store provides JSONL-based conversation persistence.
type Store struct {
	ctx     context.Context
	userId  string
	agentId string
}

// NewStore creates a Store backed by the provided persistence store.
func NewStore(ctx context.Context, userId string, agentId string) *Store {
	return &Store{
		ctx:     ctx,
		userId:  userId,
		agentId: agentId,
	}
}

// Load reads all messages from a conversation file.
// Returns empty slice (not error) if the conversation doesn't exist.
func (self *Store) Load(conversationId string) ([]Message, error) {
	return self.loadFromStore(conversationId)
}

func (self *Store) loadFromStore(conversationId string) ([]Message, error) {
	messages := make([]Message, 0)
	err := store.StoreFromContext(self.ctx).Transaction(func(transaction store.Transaction) error {
		conversationMessages, err := transaction.ListConversationMessages(conversationId, nil)
		if err != nil {
			return err
		}
		for _, message := range conversationMessages {
			messages = append(messages, conversationMessageModelToMessage(message))
		}
		return nil
	})
	if err != nil {
		if err == store.ErrNotFound {
			return nil, nil
		}
		return nil, err
	}
	return messages, nil
}

// Create creates a new conversation file with just the header line.
// The header includes the provider and model so the conversation is bound from the start.
func (self *Store) Create(conversationId, provider, model string) error {
	return store.StoreFromContext(self.ctx).Transaction(func(transaction store.Transaction) error {
		_, err := transaction.CreateConversation(&models.Conversation{
			ID:      conversationId,
			UserID:  optionalStringPointer(self.userId),
			AgentID: optionalStringPointer(self.agentId),
		}, nil)
		return err
	})
}

// AppendOption configures optional behavior for Append.
type AppendOption func(*appendOptions)

type appendOptions struct {
	provider string
	model    string
}

// WithProviderAndModel sets the provider and model on the conversation header
// when the conversation is first created.
func WithProviderAndModel(provider, model string) AppendOption {
	return func(options *appendOptions) {
		options.provider = provider
		options.model = model
	}
}

// Append writes a message to the conversation file, creating it with a header if needed.
func (self *Store) Append(conversationId string, message Message, options ...AppendOption) error {
	return self.append(conversationId, message)
}

func (self *Store) append(conversationId string, message Message) error {
	return store.StoreFromContext(self.ctx).Transaction(func(transaction store.Transaction) error {
		if _, err := transaction.GetConversation(conversationId, nil); err != nil {
			if err != store.ErrNotFound {
				return err
			}
			if _, createError := transaction.CreateConversation(&models.Conversation{
				ID:      conversationId,
				UserID:  optionalStringPointer(self.userId),
				AgentID: optionalStringPointer(self.agentId),
			}, nil); createError != nil {
				return createError
			}
		}
		_, err := transaction.CreateConversationMessage(messageToConversationMessageModel(conversationId, message), nil)
		return err
	})
}

// Info contains a conversation id and its last activity time.
type Info struct {
	ID         string `json:"id"`
	LastActive int64  `json:"lastActive"` // ms since epoch
	Title      string `json:"title,omitempty"`
	Summary    string `json:"summary,omitempty"`
	Provider   string `json:"provider,omitempty"`
	Model      string `json:"model,omitempty"`
}

// List returns all conversations, sorted by last modification time (newest first).
func (self *Store) List() ([]Info, error) {
	return self.listFromStore()
}

func (self *Store) listFromStore() ([]Info, error) {
	conversations := make([]Info, 0)
	err := store.StoreFromContext(self.ctx).Transaction(func(transaction store.Transaction) error {
		listOptions := store.ConversationListOptions{
			UserID:  ptrto.Value(self.userId),
			AgentID: ptrto.Value(self.agentId),
		}
		items, err := transaction.ListConversations(listOptions, nil)
		if err != nil {
			return err
		}
		conversations = make([]Info, 0, len(items))
		for _, item := range items {
			lastActive := int64(0)
			if item.ModifiedAt != nil {
				lastActive = item.ModifiedAt.UnixMilli()
			} else if item.CreatedAt != nil {
				lastActive = item.CreatedAt.UnixMilli()
			}
			conversations = append(conversations, Info{
				ID:         item.ID,
				LastActive: lastActive,
				Title:      valueOrEmptyString(item.Title),
				Summary:    valueOrEmptyString(item.Summary),
			})
		}
		sort.Slice(conversations, func(leftIndex, rightIndex int) bool {
			return conversations[leftIndex].LastActive > conversations[rightIndex].LastActive
		})
		return nil
	})
	return conversations, err
}

// LoadHeader reads and parses just the first line (header) of a conversation JSONL file.
func (self *Store) LoadHeader(conversationId string) (*Header, error) {
	return self.loadHeaderFromStore(conversationId)
}

func (self *Store) loadHeaderFromStore(conversationId string) (*Header, error) {
	header := &Header{
		Type:    "conversation",
		Version: 1,
		ID:      conversationId,
	}
	err := store.StoreFromContext(self.ctx).Transaction(func(transaction store.Transaction) error {
		conversation, err := transaction.GetConversation(conversationId, nil)
		if err != nil {
			return err
		}
		header.Title = valueOrEmptyString(conversation.Title)
		header.Summary = valueOrEmptyString(conversation.Summary)
		if conversation.ModifiedAt != nil {
			header.SummarizedAt = conversation.ModifiedAt.UnixMilli()
			header.Timestamp = conversation.ModifiedAt.UTC().Format(time.RFC3339)
		}
		messages, listError := transaction.ListConversationMessages(conversationId, nil)
		if listError == nil {
			for index := len(messages) - 1; index >= 0; index-- {
				modelName := strings.TrimSpace(valueOrEmptyString(messages[index].Model))
				providerName := strings.TrimSpace(valueOrEmptyString(messages[index].Provider))
				if header.Model == "" && modelName != "" {
					header.Model = modelName
				}
				if header.Provider == "" && providerName != "" {
					header.Provider = providerName
				}
				if header.Model != "" && header.Provider != "" {
					break
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return header, nil
}

// SetTitleAndSummary updates both the title and summary in a conversation's header
// line in a single write, preserving the file's original modification time.
func (self *Store) SetTitleAndSummary(conversationId, title, summary string) error {
	return store.StoreFromContext(self.ctx).Transaction(func(transaction store.Transaction) error {
		_, err := transaction.ModifyConversation(conversationId, func(conversation *models.Conversation) error {
			conversation.Title = ptrto.Value(title)
			conversation.Summary = ptrto.Value(summary)
			return nil
		}, nil)
		return err
	})
}

// Delete removes a conversation file.
func (self *Store) Delete(conversationId string) error {
	return store.StoreFromContext(self.ctx).Transaction(func(transaction store.Transaction) error {
		err := transaction.DeleteConversation(conversationId, nil)
		if err == store.ErrNotFound {
			return nil
		}
		return err
	})
}

// PageResult holds a page of messages plus pagination metadata.
type PageResult struct {
	Messages          []Message `json:"messages"`
	TotalCount        int       `json:"totalCount"`
	OldestLoadedIndex int       `json:"oldestLoadedIndex"`
	HasMore           bool      `json:"hasMore"`
}

// LoadPage returns a page of messages from a conversation.
// It loads the full conversation and slices in memory — the savings come from
// sending fewer messages over the wire to the frontend.
//
// If beforeIndex <= 0, the last `limit` messages are returned.
// Otherwise, `limit` messages ending just before `beforeIndex` are returned.
func (self *Store) LoadPage(conversationId string, limit, beforeIndex int) (*PageResult, error) {
	messages, err := self.Load(conversationId)
	if err != nil {
		return nil, err
	}
	if messages == nil {
		return &PageResult{Messages: []Message{}}, nil
	}

	totalCount := len(messages)

	// Determine the slice end (exclusive upper bound).
	end := totalCount
	if beforeIndex > 0 && beforeIndex < totalCount {
		end = beforeIndex
	}

	// Determine the slice start.
	start := end - limit
	if start < 0 {
		start = 0
	}

	return &PageResult{
		Messages:          messages[start:end],
		TotalCount:        totalCount,
		OldestLoadedIndex: start,
		HasMore:           start > 0,
	}, nil
}

func messageToConversationMessageModel(conversationId string, message Message) *models.ConversationMessage {
	createdAt := time.UnixMilli(message.Timestamp)
	role := models.Role(message.Role)
	content := []byte(message.Content)
	metadata := encodeMetadataForStorage(message)
	return &models.ConversationMessage{
		ID:             security.NewULID(),
		ConversationID: ptrto.Value(conversationId),
		Role:           &role,
		Content:        &content,
		Metadata:       metadata,
		StopReason:     stopReasonPointer(message.StopReason),
		Model:          ptrto.Value(message.Model),
		Provider:       ptrto.Value(message.Provider),
		ToolCallID:     ptrto.Value(message.ToolCallID),
		ToolName:       ptrto.Value(message.ToolName),
		CreatedAt:      &createdAt,
		ModifiedAt:     &createdAt,
	}
}

func conversationMessageModelToMessage(message models.ConversationMessage) Message {
	result := Message{
		Role:       stringValueOrEmptyRole(message.Role),
		Content:    []byte{},
		Timestamp:  0,
		Metadata:   []byte{},
		StopReason: stopReasonValueOrEmpty(message.StopReason),
		Model:      valueOrEmptyString(message.Model),
		Provider:   valueOrEmptyString(message.Provider),
		ToolCallID: valueOrEmptyString(message.ToolCallID),
		ToolName:   valueOrEmptyString(message.ToolName),
	}
	if message.Content != nil {
		result.Content = *message.Content
	}
	if message.Metadata != nil {
		result.Metadata = *message.Metadata
		decodeMetadataFromStorage(&result)
	}
	if message.CreatedAt != nil {
		result.Timestamp = message.CreatedAt.UnixMilli()
	}
	return result
}

func stopReasonPointer(value string) *models.StopReason {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	stopReason := models.StopReason(trimmed)
	return &stopReason
}

func valueOrEmptyString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func stopReasonValueOrEmpty(value *models.StopReason) string {
	if value == nil {
		return ""
	}
	return string(*value)
}

func stringValueOrEmptyRole(value *models.Role) string {
	if value == nil {
		return ""
	}
	return string(*value)
}

func optionalStringPointer(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return ptrto.Value(trimmed)
}

func encodeMetadataForStorage(message Message) *[]byte {
	metadata := message.Metadata
	if len(message.ToolCalls) == 0 && message.Usage == nil {
		if len(metadata) == 0 {
			return nil
		}
		metadataCopy := append([]byte(nil), metadata...)
		return &metadataCopy
	}

	envelope := map[string]json.RawMessage{}
	if len(metadata) > 0 {
		envelope["metadata"] = append([]byte(nil), metadata...)
	}
	if len(message.ToolCalls) > 0 {
		envelope["toolCalls"] = append([]byte(nil), message.ToolCalls...)
	}
	if message.Usage != nil {
		usageBytes, usageError := json.Marshal(message.Usage)
		if usageError == nil {
			envelope["usage"] = usageBytes
		}
	}
	if len(envelope) == 0 {
		return nil
	}
	encoded, encodeError := json.Marshal(envelope)
	if encodeError != nil {
		if len(metadata) == 0 {
			return nil
		}
		metadataCopy := append([]byte(nil), metadata...)
		return &metadataCopy
	}
	return &encoded
}

func decodeMetadataFromStorage(message *Message) {
	if len(message.Metadata) == 0 {
		return
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(message.Metadata, &envelope); err != nil {
		return
	}

	if toolCalls, ok := envelope["toolCalls"]; ok && len(toolCalls) > 0 {
		message.ToolCalls = append([]byte(nil), toolCalls...)
	}
	if usage, ok := envelope["usage"]; ok && len(usage) > 0 {
		var parsedUsage Usage
		if usageError := json.Unmarshal(usage, &parsedUsage); usageError == nil {
			message.Usage = &parsedUsage
		}
	}
	if rawMetadata, ok := envelope["metadata"]; ok {
		message.Metadata = append([]byte(nil), rawMetadata...)
	}
}
