package api

import (
	"encoding/json"

	"github.com/teanode/teanode/internal/integrations/approvals"
	"github.com/teanode/teanode/internal/pubsub"
)

// --- approvals.list ---

func (self *webSocketConnection) handleApprovalsList(frame requestFrame) (interface{}, error) {
	var parameters struct {
		ConversationID string `json:"conversationId"`
	}
	if frame.Parameters != nil {
		_ = json.Unmarshal(frame.Parameters, &parameters)
	}
	if parameters.ConversationID == "" {
		return nil, rpcError(400, "conversationId is required")
	}

	if err := self.verifyConversationAccess(parameters.ConversationID); err != nil {
		return nil, rpcError(403, err.Error())
	}

	broker := self.api.coordinator.ApprovalBroker()
	pending := broker.PendingForConversation(parameters.ConversationID)

	// Filter to only approvals belonging to this user.
	var result []*approvals.PendingApproval
	for _, a := range pending {
		if a.UserID == self.userId() {
			result = append(result, a)
		}
	}
	if result == nil {
		result = make([]*approvals.PendingApproval, 0)
	}

	return map[string]interface{}{"approvals": result}, nil
}

// --- approvals.resolve ---
//
// Accepts: { verdicts: [{ approvalId, verdict, reason? }, ...] }

type verdictEntry struct {
	ApprovalID string `json:"approvalId"`
	Verdict    string `json:"verdict"`
	Reason     string `json:"reason,omitempty"`
}

func (self *webSocketConnection) handleApprovalsResolve(frame requestFrame) (interface{}, error) {
	var parameters struct {
		Verdicts []verdictEntry `json:"verdicts"`
	}
	if frame.Parameters != nil {
		_ = json.Unmarshal(frame.Parameters, &parameters)
	}

	verdicts := parameters.Verdicts
	if len(verdicts) == 0 {
		return nil, rpcError(400, "verdicts array is required and must not be empty")
	}

	// Validate each entry before touching the broker.
	payloads := make(map[string]approvals.ApprovalPayload, len(verdicts))
	for _, entry := range verdicts {
		if entry.ApprovalID == "" || entry.Verdict == "" {
			return nil, rpcError(400, "each verdict must have approvalId and verdict")
		}
		if entry.Verdict != string(approvals.ApprovalVerdictApproved) && entry.Verdict != string(approvals.ApprovalVerdictRejected) {
			return nil, rpcError(400, "verdict must be 'approved' or 'rejected'")
		}
		if _, dup := payloads[entry.ApprovalID]; dup {
			return nil, rpcError(400, "duplicate approvalId: "+entry.ApprovalID)
		}
		payloads[entry.ApprovalID] = approvals.ApprovalPayload{
			Verdict: approvals.ApprovalVerdict(entry.Verdict),
			Reason:  entry.Reason,
		}
	}

	broker := self.api.coordinator.ApprovalBroker()

	// Atomic batch: validates all, then delivers all — no partial state.
	if err := broker.ResolveBatch(payloads, self.userId()); err != nil {
		return nil, rpcError(400, err.Error())
	}

	// Broadcast "resolved" events for each approval so other tabs dismiss them.
	for _, entry := range verdicts {
		event := map[string]interface{}{
			"action":     "resolved",
			"userId":     self.userId(),
			"approvalId": entry.ApprovalID,
			"verdict":    entry.Verdict,
		}
		if entry.Reason != "" {
			event["reason"] = entry.Reason
		}
		self.api.pubsub.Broadcast(pubsub.EventTypeConversationApprovals, event)
	}

	return map[string]interface{}{"ok": true}, nil
}
