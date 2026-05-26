package tabs

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestAttachAndGet(t *testing.T) {
	broker := NewTabBroker()
	broker.Attach(Attachment{
		UserID:         "u1",
		AgentID:        "a1",
		ConversationID: "c1",
		TabURL:         "https://example.com",
		TabTitle:       "Example",
		TabID:          42,
	}, "conn1")

	if !broker.HasAttachment("u1", "a1", "c1") {
		t.Fatal("expected attachment to exist")
	}

	attachment := broker.GetAttachment("u1", "a1", "c1")
	if attachment == nil {
		t.Fatal("expected attachment to be returned")
	}
	if attachment.TabURL != "https://example.com" {
		t.Errorf("expected TabURL https://example.com, got %s", attachment.TabURL)
	}
}

func TestAttachIdempotentSameConn(t *testing.T) {
	broker := NewTabBroker()
	broker.Attach(Attachment{
		UserID: "u1", AgentID: "a1", ConversationID: "c1",
		TabURL: "https://old.com", TabTitle: "Old", TabID: 1,
	}, "conn1")

	// Re-attach from same connection+tab updates in place — no displacement.
	displaced := broker.Attach(Attachment{
		UserID: "u1", AgentID: "a1", ConversationID: "c1",
		TabURL: "https://new.com", TabTitle: "New", TabID: 1,
	}, "conn1")

	if displaced != nil {
		t.Errorf("expected no displacement for same connection+tab, got %+v", displaced)
	}

	attachment := broker.GetAttachment("u1", "a1", "c1")
	if attachment.TabURL != "https://new.com" {
		t.Errorf("expected updated URL, got %s", attachment.TabURL)
	}
}

func TestAttachReplacesStaleConnection(t *testing.T) {
	broker := NewTabBroker()
	broker.Attach(Attachment{
		UserID: "u1", AgentID: "a1", ConversationID: "c1",
		TabURL: "https://old.com", TabID: 1,
	}, "conn-old")

	// Different connection replaces — returns displaced attachment.
	displaced := broker.Attach(Attachment{
		UserID: "u1", AgentID: "a1", ConversationID: "c1",
		TabURL: "https://new.com", TabID: 2,
	}, "conn-new")

	if displaced == nil {
		t.Fatal("expected displaced attachment to be returned")
	}
	if displaced.TabURL != "https://old.com" {
		t.Errorf("expected displaced URL https://old.com, got %s", displaced.TabURL)
	}
	if displaced.TabID != 1 {
		t.Errorf("expected displaced TabID 1, got %d", displaced.TabID)
	}

	attachment := broker.GetAttachment("u1", "a1", "c1")
	if attachment.TabURL != "https://new.com" {
		t.Errorf("expected replacement URL, got %s", attachment.TabURL)
	}
}

func TestAttachDisplacesSameConnDifferentTab(t *testing.T) {
	broker := NewTabBroker()
	broker.Attach(Attachment{
		UserID: "u1", AgentID: "a1", ConversationID: "c1",
		TabURL: "https://old.com", TabID: 1,
	}, "conn1")

	// Same connection, different tab → displacement.
	displaced := broker.Attach(Attachment{
		UserID: "u1", AgentID: "a1", ConversationID: "c1",
		TabURL: "https://new.com", TabID: 2,
	}, "conn1")

	if displaced == nil {
		t.Fatal("expected displaced attachment for same conn, different tab")
	}
	if displaced.TabID != 1 {
		t.Errorf("expected displaced TabID 1, got %d", displaced.TabID)
	}
}

func TestAttachFreshNoDisplacement(t *testing.T) {
	broker := NewTabBroker()
	displaced := broker.Attach(Attachment{
		UserID: "u1", AgentID: "a1", ConversationID: "c1",
		TabURL: "https://example.com", TabID: 1,
	}, "conn1")

	if displaced != nil {
		t.Errorf("expected no displacement on fresh attach, got %+v", displaced)
	}
}

func TestDetachOnlyOwner(t *testing.T) {
	broker := NewTabBroker()
	broker.Attach(Attachment{
		UserID: "u1", AgentID: "a1", ConversationID: "c1",
	}, "conn1")

	// Wrong connection can't detach.
	if broker.Detach("u1", "a1", "c1", "conn-other") {
		t.Fatal("expected Detach to return false for wrong connection")
	}
	if !broker.HasAttachment("u1", "a1", "c1") {
		t.Fatal("expected attachment to survive wrong-connection detach")
	}

	// Correct connection can detach.
	if !broker.Detach("u1", "a1", "c1", "conn1") {
		t.Fatal("expected Detach to return true for correct connection")
	}
	if broker.HasAttachment("u1", "a1", "c1") {
		t.Fatal("expected attachment to be removed")
	}
}

func TestGetAttachmentNonexistent(t *testing.T) {
	broker := NewTabBroker()
	if broker.GetAttachment("u1", "a1", "c999") != nil {
		t.Fatal("expected nil for nonexistent attachment")
	}
}

func TestListForUser(t *testing.T) {
	broker := NewTabBroker()
	broker.Attach(Attachment{
		UserID: "u1", AgentID: "a1", ConversationID: "c1",
	}, "conn1")
	broker.Attach(Attachment{
		UserID: "u1", AgentID: "a1", ConversationID: "c2",
	}, "conn1")
	broker.Attach(Attachment{
		UserID: "u2", AgentID: "a1", ConversationID: "c3",
	}, "conn2")

	list := broker.ListForUser("u1")
	if len(list) != 2 {
		t.Fatalf("expected 2 attachments for u1, got %d", len(list))
	}
}

func TestPendingResolve(t *testing.T) {
	broker := NewTabBroker()
	pending := &PendingToolCall{
		ID:         "req1",
		UserID:     "u1",
		resultChan: MakeResultChan(),
	}
	broker.RegisterPending(pending)

	go func() {
		time.Sleep(10 * time.Millisecond)
		if err := broker.Resolve("req1", ToolCallResult{Result: `{"ok":true}`}); err != nil {
			t.Errorf("resolve pending request: %v", err)
		}
	}()

	result := <-pending.resultChan
	if result.Result != `{"ok":true}` {
		t.Errorf("unexpected result: %s", result.Result)
	}
}

func TestPendingResolveNotFound(t *testing.T) {
	broker := NewTabBroker()
	err := broker.Resolve("nonexistent", ToolCallResult{})
	if err == nil {
		t.Fatal("expected error for nonexistent pending call")
	}
}

func TestPendingCancel(t *testing.T) {
	broker := NewTabBroker()
	pending := &PendingToolCall{
		ID:         "req1",
		resultChan: MakeResultChan(),
	}
	broker.RegisterPending(pending)
	broker.CancelPending("req1")

	// Channel should be closed.
	_, ok := <-pending.resultChan
	if ok {
		t.Fatal("expected channel to be closed after cancel")
	}
}

func TestDetachAllForConnection(t *testing.T) {
	broker := NewTabBroker()

	broker.Attach(Attachment{
		UserID: "u1", AgentID: "a1", ConversationID: "c1",
	}, "conn1")
	broker.Attach(Attachment{
		UserID: "u1", AgentID: "a1", ConversationID: "c2",
	}, "conn1")
	broker.Attach(Attachment{
		UserID: "u2", AgentID: "a1", ConversationID: "c3",
	}, "conn2")

	// Register a pending call for u1:c1.
	pending := &PendingToolCall{
		ID:             "req1",
		UserID:         "u1",
		ConversationID: "c1",
		resultChan:     MakeResultChan(),
	}
	broker.RegisterPending(pending)

	broker.DetachAllForConnection("conn1")

	if broker.HasAttachment("u1", "a1", "c1") {
		t.Fatal("expected c1 to be detached")
	}
	if broker.HasAttachment("u1", "a1", "c2") {
		t.Fatal("expected c2 to be detached")
	}
	if !broker.HasAttachment("u2", "a1", "c3") {
		t.Fatal("expected c3 to survive (different connection)")
	}

	// Pending call should have been rejected.
	result := <-pending.resultChan
	if result.Error != "extension disconnected" {
		t.Errorf("expected 'extension disconnected' error, got %q", result.Error)
	}
}

func TestCancelPendingForAttachment(t *testing.T) {
	broker := NewTabBroker()
	pending1 := &PendingToolCall{
		ID:             "req1",
		UserID:         "u1",
		AgentID:        "a1",
		ConversationID: "c1",
		resultChan:     MakeResultChan(),
	}
	pending2 := &PendingToolCall{
		ID:             "req2",
		UserID:         "u1",
		AgentID:        "a1",
		ConversationID: "c2",
		resultChan:     MakeResultChan(),
	}
	broker.RegisterPending(pending1)
	broker.RegisterPending(pending2)

	broker.CancelPendingForAttachment("u1", "a1", "c1")

	// pending1 should be rejected.
	result := <-pending1.resultChan
	if result.Error != "tab detached" {
		t.Errorf("expected 'tab detached' error, got %q", result.Error)
	}

	// pending2 should still be pending (different conversation).
	select {
	case <-pending2.resultChan:
		t.Fatal("pending2 should not have been resolved")
	default:
		// good
	}
}

func TestConcurrentAccess(t *testing.T) {
	broker := NewTabBroker()
	var waitGroup sync.WaitGroup

	// Concurrent attach/detach.
	for index := 0; index < 50; index++ {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			connectionId := "conn"
			broker.Attach(Attachment{
				UserID: "u1", AgentID: "a1", ConversationID: "c1",
				TabURL: "https://example.com",
			}, connectionId)
			broker.HasAttachment("u1", "a1", "c1")
			broker.GetAttachment("u1", "a1", "c1")
			broker.ListForUser("u1")
		}(index)
	}

	// Concurrent pending register/resolve.
	for index := 0; index < 50; index++ {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			id := fmt.Sprintf("pending-%d", index)
			pending := &PendingToolCall{
				ID:         id,
				UserID:     "u1",
				resultChan: MakeResultChan(),
			}
			broker.RegisterPending(pending)
			if err := broker.Resolve(id, ToolCallResult{Result: "ok"}); err != nil {
				t.Errorf("resolve %s: %v", id, err)
			}
		}(index)
	}

	waitGroup.Wait()
}
