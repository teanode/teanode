package tabs

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// ToolCallResult carries the result from the extension back to the blocked tool.
type ToolCallResult struct {
	Result string
	Error  string
}

// PendingToolCall represents a tool call waiting for the extension to respond.
type PendingToolCall struct {
	ID             string
	UserID         string
	AgentID        string
	ConversationID string
	ToolName       string
	Arguments      json.RawMessage
	resultChan     chan ToolCallResult
}

// MakeResultChan creates a buffered channel for a PendingToolCall.
func MakeResultChan() chan ToolCallResult {
	return make(chan ToolCallResult, 1)
}

// ResultChan returns the result channel.
func (self *PendingToolCall) ResultChan() chan ToolCallResult {
	return self.resultChan
}

// SetResultChan sets the result channel.
func (self *PendingToolCall) SetResultChan(ch chan ToolCallResult) {
	self.resultChan = ch
}

// Attachment records a browser tab attached to a conversation.
type Attachment struct {
	UserID         string    `json:"userId,omitempty"`
	AgentID        string    `json:"agentId"`
	ConversationID string    `json:"conversationId"`
	TabURL         string    `json:"tabUrl"`
	TabTitle       string    `json:"tabTitle"`
	TabID          int       `json:"tabId"`
	AttachedAt     time.Time `json:"attachedAt"`
	connectionId   string    // opaque identifier for the owning WebSocket connection
}

// TabBroker manages tab attachments and pending tool calls.
// Modelled after askuser.QuestionBroker.
type TabBroker struct {
	mutex       sync.Mutex
	attachments map[string]*Attachment      // "userId:agentId:conversationId" → attachment
	pending     map[string]*PendingToolCall // requestId → pending call
}

// NewTabBroker creates a new broker.
func NewTabBroker() *TabBroker {
	return &TabBroker{
		attachments: make(map[string]*Attachment),
		pending:     make(map[string]*PendingToolCall),
	}
}

func attachmentKey(userId, agentId, conversationId string) string {
	return userId + ":" + agentId + ":" + conversationId
}

// Attach registers a tab attachment. If an attachment for the same key exists
// from a different connection or tab, the old attachment is returned so the
// caller can notify the displaced overlay. Returns nil when no displacement
// occurred (same connection+tab update, or fresh attach).
func (self *TabBroker) Attach(attachment Attachment, connectionId string) *Attachment {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	attachment.connectionId = connectionId
	attachment.AttachedAt = time.Now()
	key := attachmentKey(attachment.UserID, attachment.AgentID, attachment.ConversationID)
	old := self.attachments[key]
	self.attachments[key] = &attachment
	// Return old only if it's genuinely different (different connection or tab).
	if old != nil && (old.connectionId != connectionId || old.TabID != attachment.TabID) {
		cp := *old
		return &cp
	}
	return nil
}

// Detach removes an attachment. Only the owning connection may detach.
func (self *TabBroker) Detach(userId, agentId, conversationId, connectionId string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	key := attachmentKey(userId, agentId, conversationId)
	existing := self.attachments[key]
	if existing == nil || existing.connectionId != connectionId {
		return
	}
	delete(self.attachments, key)
}

// HasAttachment checks existence.
func (self *TabBroker) HasAttachment(userId, agentId, conversationId string) bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	_, ok := self.attachments[attachmentKey(userId, agentId, conversationId)]
	return ok
}

// GetAttachment returns the attachment or nil.
func (self *TabBroker) GetAttachment(userId, agentId, conversationId string) *Attachment {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	attachment := self.attachments[attachmentKey(userId, agentId, conversationId)]
	if attachment == nil {
		return nil
	}
	// Return a copy to avoid races.
	copy := *attachment
	return &copy
}

// ListForUser returns all attachments belonging to a user.
func (self *TabBroker) ListForUser(userId string) []Attachment {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	var result []Attachment
	for _, attachment := range self.attachments {
		if attachment.UserID == userId {
			result = append(result, *attachment)
		}
	}
	return result
}

// FirstPendingID returns the ID of any one pending tool call, or "" if none.
// Intended for testing.
func (self *TabBroker) FirstPendingID() string {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	for id := range self.pending {
		return id
	}
	return ""
}

// RegisterPending adds a pending tool call.
func (self *TabBroker) RegisterPending(pending *PendingToolCall) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.pending[pending.ID] = pending
}

// Resolve delivers a result to a pending tool call and removes it.
func (self *TabBroker) Resolve(requestId string, result ToolCallResult) error {
	self.mutex.Lock()
	pending, ok := self.pending[requestId]
	if ok {
		delete(self.pending, requestId)
	}
	self.mutex.Unlock()
	if !ok {
		return fmt.Errorf("pending tool call not found: %s", requestId)
	}
	pending.resultChan <- result
	return nil
}

// CancelPending closes the channel and removes the pending call.
func (self *TabBroker) CancelPending(requestId string) {
	self.mutex.Lock()
	pending, ok := self.pending[requestId]
	if ok {
		delete(self.pending, requestId)
	}
	self.mutex.Unlock()
	if ok {
		close(pending.resultChan)
	}
}

// DetachAllForConnection removes all attachments owned by connectionId and
// rejects any pending tool calls that belong to those attachments.
func (self *TabBroker) DetachAllForConnection(connectionId string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	// Collect conversation IDs being detached.
	detachedKeys := make(map[string]bool)
	for key, attachment := range self.attachments {
		if attachment.connectionId == connectionId {
			detachedKeys[attachment.UserID+":"+attachment.ConversationID] = true
			delete(self.attachments, key)
		}
	}

	// Reject pending calls for detached conversations.
	for id, pending := range self.pending {
		ownerKey := pending.UserID + ":" + pending.ConversationID
		if detachedKeys[ownerKey] {
			delete(self.pending, id)
			pending.resultChan <- ToolCallResult{Error: "extension disconnected"}
		}
	}
}

// CancelPendingForAttachment cancels all pending tool calls for a specific attachment.
func (self *TabBroker) CancelPendingForAttachment(userId, agentId, conversationId string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	for id, pending := range self.pending {
		if pending.UserID == userId && pending.AgentID == agentId && pending.ConversationID == conversationId {
			delete(self.pending, id)
			pending.resultChan <- ToolCallResult{Error: "tab detached"}
		}
	}
}
