package approvals

import (
	"fmt"
	"sort"
	"sync"
)

// ApprovalBroker is an in-memory registry of pending approvals.
// It mirrors the behavior of internal/integrations/questions.QuestionBroker.
type ApprovalBroker struct {
	mutex   sync.Mutex
	pending map[string]*PendingApproval
}

func NewApprovalBroker() *ApprovalBroker {
	return &ApprovalBroker{pending: make(map[string]*PendingApproval)}
}

func (self *ApprovalBroker) Register(pending *PendingApproval) {
	if pending == nil || pending.ID == "" {
		return
	}
	self.mutex.Lock()
	self.pending[pending.ID] = pending
	self.mutex.Unlock()
}

func (self *ApprovalBroker) PendingForConversation(conversationId string) []*PendingApproval {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if conversationId == "" {
		return nil
	}
	var result []*PendingApproval
	for _, approval := range self.pending {
		if approval.ConversationID == conversationId {
			result = append(result, approval)
		}
	}
	sort.Slice(result, func(leftIndex, rightIndex int) bool { return result[leftIndex].ID < result[rightIndex].ID })
	return result
}

func (self *ApprovalBroker) ResolveBatch(payloads map[string]ApprovalPayload, callerUserId string) error {
	if len(payloads) == 0 {
		return fmt.Errorf("approvals: payloads must not be empty")
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	// Validate all first.
	for approvalId := range payloads {
		pending := self.pending[approvalId]
		if pending == nil {
			return fmt.Errorf("approvals: approval not found: %s", approvalId)
		}
		if pending.UserID != callerUserId {
			return fmt.Errorf("approvals: approval does not belong to user")
		}
	}

	// Then resolve all.
	for approvalId, payload := range payloads {
		pending := self.pending[approvalId]
		delete(self.pending, approvalId)
		if pending.approvalChan != nil {
			pending.approvalChan <- payload
			close(pending.approvalChan)
		}
	}
	return nil
}

func (self *ApprovalBroker) Cancel(approvalId string) {
	if approvalId == "" {
		return
	}
	self.mutex.Lock()
	pending := self.pending[approvalId]
	delete(self.pending, approvalId)
	self.mutex.Unlock()
	if pending != nil && pending.approvalChan != nil {
		close(pending.approvalChan)
	}
}
