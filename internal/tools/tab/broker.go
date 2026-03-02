package tab

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

// TabAttachment records a browser tab attached to a conversation.
type TabAttachment struct {
	UserID         string    `json:"userId,omitempty"`
	AgentID        string    `json:"agentId"`
	ConversationID string    `json:"conversationId"`
	TabURL         string    `json:"tabUrl"`
	TabTitle       string    `json:"tabTitle"`
	TabID          int       `json:"tabId"`
	AttachedAt     time.Time `json:"attachedAt"`
	connID         string    // opaque identifier for the owning WS connection
}

// TabToolBroker manages tab attachments and pending tool calls.
// Modelled after askuser.QuestionBroker.
type TabToolBroker struct {
	mu          sync.Mutex
	attachments map[string]*TabAttachment  // "userId:agentId:conversationId" → attachment
	pending     map[string]*PendingToolCall // requestId → pending call
}

// NewTabToolBroker creates a new broker.
func NewTabToolBroker() *TabToolBroker {
	return &TabToolBroker{
		attachments: make(map[string]*TabAttachment),
		pending:     make(map[string]*PendingToolCall),
	}
}

func attachmentKey(userId, agentId, conversationId string) string {
	return userId + ":" + agentId + ":" + conversationId
}

// Attach registers a tab attachment. If an attachment for the same key exists
// from the same connection, it is updated in place. If from a different
// connection, the old attachment is replaced (stale connection assumed).
func (b *TabToolBroker) Attach(a TabAttachment, connID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	a.connID = connID
	a.AttachedAt = time.Now()
	key := attachmentKey(a.UserID, a.AgentID, a.ConversationID)
	b.attachments[key] = &a
}

// Detach removes an attachment. Only the owning connection may detach.
func (b *TabToolBroker) Detach(userId, agentId, conversationId, connID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := attachmentKey(userId, agentId, conversationId)
	existing := b.attachments[key]
	if existing == nil || existing.connID != connID {
		return
	}
	delete(b.attachments, key)
}

// HasAttachment checks existence.
func (b *TabToolBroker) HasAttachment(userId, agentId, conversationId string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, ok := b.attachments[attachmentKey(userId, agentId, conversationId)]
	return ok
}

// GetAttachment returns the attachment or nil.
func (b *TabToolBroker) GetAttachment(userId, agentId, conversationId string) *TabAttachment {
	b.mu.Lock()
	defer b.mu.Unlock()
	a := b.attachments[attachmentKey(userId, agentId, conversationId)]
	if a == nil {
		return nil
	}
	// Return a copy to avoid races.
	copy := *a
	return &copy
}

// ListForUser returns all attachments belonging to a user.
func (b *TabToolBroker) ListForUser(userId string) []TabAttachment {
	b.mu.Lock()
	defer b.mu.Unlock()
	var result []TabAttachment
	for _, a := range b.attachments {
		if a.UserID == userId {
			result = append(result, *a)
		}
	}
	return result
}

// RegisterPending adds a pending tool call.
func (b *TabToolBroker) RegisterPending(p *PendingToolCall) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pending[p.ID] = p
}

// Resolve delivers a result to a pending tool call and removes it.
func (b *TabToolBroker) Resolve(requestId string, result ToolCallResult) error {
	b.mu.Lock()
	p, ok := b.pending[requestId]
	if ok {
		delete(b.pending, requestId)
	}
	b.mu.Unlock()
	if !ok {
		return fmt.Errorf("pending tool call not found: %s", requestId)
	}
	p.resultChan <- result
	return nil
}

// CancelPending closes the channel and removes the pending call.
func (b *TabToolBroker) CancelPending(requestId string) {
	b.mu.Lock()
	p, ok := b.pending[requestId]
	if ok {
		delete(b.pending, requestId)
	}
	b.mu.Unlock()
	if ok {
		close(p.resultChan)
	}
}

// DetachAllForConnection removes all attachments owned by connID and
// rejects any pending tool calls that belong to those attachments.
func (b *TabToolBroker) DetachAllForConnection(connID string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Collect conversation IDs being detached.
	detachedKeys := make(map[string]bool)
	for key, a := range b.attachments {
		if a.connID == connID {
			detachedKeys[a.UserID+":"+a.ConversationID] = true
			delete(b.attachments, key)
		}
	}

	// Reject pending calls for detached conversations.
	for id, p := range b.pending {
		ownerKey := p.UserID + ":" + p.ConversationID
		if detachedKeys[ownerKey] {
			delete(b.pending, id)
			p.resultChan <- ToolCallResult{Error: "extension disconnected"}
		}
	}
}

// CancelPendingForAttachment cancels all pending tool calls for a specific attachment.
func (b *TabToolBroker) CancelPendingForAttachment(userId, agentId, conversationId string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for id, p := range b.pending {
		if p.UserID == userId && p.AgentID == agentId && p.ConversationID == conversationId {
			delete(b.pending, id)
			p.resultChan <- ToolCallResult{Error: "tab detached"}
		}
	}
}
