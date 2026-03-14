// Package approvals provides tool-call approval brokering between runners and external clients.
package approvals

// ApprovalVerdict is the decision returned by the user in response to a pending approval.
type ApprovalVerdict string

const (
	ApprovalVerdictApproved ApprovalVerdict = "approved"
	ApprovalVerdictRejected ApprovalVerdict = "rejected"
)

// ApprovalPayload is delivered to a PendingApproval when the user resolves it.
// Reason is optional and primarily used for rejections.
type ApprovalPayload struct {
	Verdict ApprovalVerdict `json:"verdict"`
	Reason  string          `json:"reason,omitempty"`
}

// PendingApproval represents a tool invocation that requires explicit user approval.
// It is kept in-memory only.
type PendingApproval struct {
	ID             string `json:"id"`
	ConversationID string `json:"conversationId"`
	AgentID        string `json:"agentId"`
	UserID         string `json:"userId"`
	RunID          string `json:"runId"`

	ToolCallID string `json:"toolCallId"`
	ToolName   string `json:"toolName"`
	Arguments  string `json:"arguments"`

	PolicyReason string `json:"policyReason"`
	Risk         string `json:"risk,omitempty"`

	approvalChan chan ApprovalPayload
}

func (self *PendingApproval) SetApprovalChan(channel chan ApprovalPayload) {
	self.approvalChan = channel
}

func (self *PendingApproval) ApprovalChan() <-chan ApprovalPayload {
	return self.approvalChan
}

func MakeApprovalChan() chan ApprovalPayload {
	return make(chan ApprovalPayload, 1)
}
