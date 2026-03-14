package approvals

import (
	"testing"
	"time"
)

func TestRegisterAndResolve(t *testing.T) {
	b := NewApprovalBroker()
	a := &PendingApproval{
		ID:             "a1",
		ConversationID: "c1",
		UserID:         "u1",
		ToolName:       "shell",
		Arguments:      `{"command":"rm -rf /"}`,
		PolicyReason:   "dangerous command",
		Risk:           "high",
		approvalChan:   MakeApprovalChan(),
	}
	b.Register(a)

	go func() {
		payloads := map[string]ApprovalPayload{
			"a1": {Verdict: ApprovalVerdictApproved},
		}
		if err := b.ResolveBatch(payloads, "u1"); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	}()

	select {
	case payload := <-a.approvalChan:
		if payload.Verdict != ApprovalVerdictApproved {
			t.Errorf("expected approved, got %s", payload.Verdict)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for approval")
	}
}

func TestResolveUnknownID(t *testing.T) {
	b := NewApprovalBroker()
	err := b.ResolveBatch(map[string]ApprovalPayload{
		"nonexistent": {Verdict: ApprovalVerdictApproved},
	}, "u1")
	if err == nil {
		t.Fatal("expected error for unknown approval ID")
	}
}

func TestResolveWrongUser(t *testing.T) {
	b := NewApprovalBroker()
	a := &PendingApproval{
		ID:           "a1",
		UserID:       "u1",
		approvalChan: MakeApprovalChan(),
	}
	b.Register(a)

	err := b.ResolveBatch(map[string]ApprovalPayload{
		"a1": {Verdict: ApprovalVerdictApproved},
	}, "u2")
	if err == nil {
		t.Fatal("expected error for wrong user")
	}

	// Channel should NOT have received anything (atomic).
	select {
	case <-a.approvalChan:
		t.Fatal("approval should not have been delivered")
	default:
	}
}

func TestCancel(t *testing.T) {
	b := NewApprovalBroker()
	a := &PendingApproval{
		ID:           "a1",
		UserID:       "u1",
		approvalChan: MakeApprovalChan(),
	}
	b.Register(a)
	b.Cancel("a1")

	// Channel should be closed.
	select {
	case _, ok := <-a.approvalChan:
		if ok {
			t.Fatal("expected channel to be closed")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}

	// After cancel, resolve should fail.
	err := b.ResolveBatch(map[string]ApprovalPayload{
		"a1": {Verdict: ApprovalVerdictApproved},
	}, "u1")
	if err == nil {
		t.Fatal("expected error after cancel")
	}
}

func TestCancelNonexistent(t *testing.T) {
	b := NewApprovalBroker()
	// Should not panic.
	b.Cancel("nonexistent")
}

func TestPendingForConversation(t *testing.T) {
	b := NewApprovalBroker()
	b.Register(&PendingApproval{ID: "a1", ConversationID: "c1", UserID: "u1", approvalChan: MakeApprovalChan()})
	b.Register(&PendingApproval{ID: "a2", ConversationID: "c2", UserID: "u1", approvalChan: MakeApprovalChan()})
	b.Register(&PendingApproval{ID: "a3", ConversationID: "c1", UserID: "u2", approvalChan: MakeApprovalChan()})

	result := b.PendingForConversation("c1")
	if len(result) != 2 {
		t.Fatalf("expected 2 approvals for c1, got %d", len(result))
	}

	ids := map[string]bool{}
	for _, a := range result {
		ids[a.ID] = true
	}
	if !ids["a1"] || !ids["a3"] {
		t.Errorf("expected a1 and a3, got %v", ids)
	}

	result = b.PendingForConversation("c2")
	if len(result) != 1 || result[0].ID != "a2" {
		t.Errorf("expected [a2] for c2, got %v", result)
	}

	result = b.PendingForConversation("nonexistent")
	if len(result) != 0 {
		t.Errorf("expected empty for nonexistent, got %d", len(result))
	}
}

func TestResolveBatchHappyPath(t *testing.T) {
	b := NewApprovalBroker()
	a1 := &PendingApproval{ID: "a1", UserID: "u1", approvalChan: MakeApprovalChan()}
	a2 := &PendingApproval{ID: "a2", UserID: "u1", approvalChan: MakeApprovalChan()}
	b.Register(a1)
	b.Register(a2)

	payloads := map[string]ApprovalPayload{
		"a1": {Verdict: ApprovalVerdictApproved},
		"a2": {Verdict: ApprovalVerdictRejected, Reason: "too risky"},
	}
	if err := b.ResolveBatch(payloads, "u1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case p := <-a1.approvalChan:
		if p.Verdict != ApprovalVerdictApproved {
			t.Errorf("a1: expected approved, got %s", p.Verdict)
		}
	case <-time.After(time.Second):
		t.Fatal("a1: timed out")
	}
	select {
	case p := <-a2.approvalChan:
		if p.Verdict != ApprovalVerdictRejected || p.Reason != "too risky" {
			t.Errorf("a2: expected rejected/too risky, got %s/%s", p.Verdict, p.Reason)
		}
	case <-time.After(time.Second):
		t.Fatal("a2: timed out")
	}
}

func TestResolveBatchRejectsUnknownApproval(t *testing.T) {
	b := NewApprovalBroker()
	a1 := &PendingApproval{ID: "a1", ConversationID: "c1", UserID: "u1", approvalChan: MakeApprovalChan()}
	b.Register(a1)

	payloads := map[string]ApprovalPayload{
		"a1":          {Verdict: ApprovalVerdictApproved},
		"nonexistent": {Verdict: ApprovalVerdictApproved},
	}
	err := b.ResolveBatch(payloads, "u1")
	if err == nil {
		t.Fatal("expected error for unknown approval")
	}

	// a1 should NOT have been resolved (atomic rollback).
	select {
	case <-a1.approvalChan:
		t.Fatal("a1 should not have received a payload")
	default:
	}

	// a1 should still be pending.
	pending := b.PendingForConversation("c1")
	if len(pending) != 1 || pending[0].ID != "a1" {
		t.Error("a1 should still be pending after failed batch")
	}
}

func TestResolveBatchRejectsWrongUser(t *testing.T) {
	b := NewApprovalBroker()
	a1 := &PendingApproval{ID: "a1", ConversationID: "c1", UserID: "u1", approvalChan: MakeApprovalChan()}
	a2 := &PendingApproval{ID: "a2", ConversationID: "c1", UserID: "u2", approvalChan: MakeApprovalChan()}
	b.Register(a1)
	b.Register(a2)

	// u1 tries to resolve a2 which belongs to u2.
	payloads := map[string]ApprovalPayload{
		"a1": {Verdict: ApprovalVerdictApproved},
		"a2": {Verdict: ApprovalVerdictApproved},
	}
	err := b.ResolveBatch(payloads, "u1")
	if err == nil {
		t.Fatal("expected error for wrong user")
	}

	// Neither should have been resolved.
	select {
	case <-a1.approvalChan:
		t.Fatal("a1 should not have received a payload")
	default:
	}
	select {
	case <-a2.approvalChan:
		t.Fatal("a2 should not have received a payload")
	default:
	}
}

func TestResolveBatchEmptyMap(t *testing.T) {
	b := NewApprovalBroker()
	// Empty batch should error (payloads must not be empty).
	if err := b.ResolveBatch(map[string]ApprovalPayload{}, "u1"); err == nil {
		t.Fatal("expected error for empty payloads")
	}
}

func TestResolveRejectedWithReason(t *testing.T) {
	b := NewApprovalBroker()
	a := &PendingApproval{
		ID:           "a1",
		UserID:       "u1",
		ToolName:     "filesystem",
		PolicyReason: "write access requires approval",
		approvalChan: MakeApprovalChan(),
	}
	b.Register(a)

	go func() {
		payloads := map[string]ApprovalPayload{
			"a1": {Verdict: ApprovalVerdictRejected, Reason: "not needed"},
		}
		if err := b.ResolveBatch(payloads, "u1"); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	}()

	select {
	case payload := <-a.approvalChan:
		if payload.Verdict != ApprovalVerdictRejected {
			t.Errorf("expected rejected, got %s", payload.Verdict)
		}
		if payload.Reason != "not needed" {
			t.Errorf("expected reason 'not needed', got %s", payload.Reason)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for rejection")
	}
}
