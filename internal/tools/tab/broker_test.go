package tab

import (
	"sync"
	"testing"
	"time"
)

func TestAttachAndGet(t *testing.T) {
	b := NewTabToolBroker()
	b.Attach(TabAttachment{
		UserID:         "u1",
		AgentID:        "a1",
		ConversationID: "c1",
		TabURL:         "https://example.com",
		TabTitle:       "Example",
		TabID:          42,
	}, "conn1")

	if !b.HasAttachment("u1", "a1", "c1") {
		t.Fatal("expected attachment to exist")
	}

	a := b.GetAttachment("u1", "a1", "c1")
	if a == nil {
		t.Fatal("expected attachment to be returned")
	}
	if a.TabURL != "https://example.com" {
		t.Errorf("expected TabURL https://example.com, got %s", a.TabURL)
	}
}

func TestAttachIdempotentSameConn(t *testing.T) {
	b := NewTabToolBroker()
	b.Attach(TabAttachment{
		UserID: "u1", AgentID: "a1", ConversationID: "c1",
		TabURL: "https://old.com", TabTitle: "Old",
	}, "conn1")

	// Re-attach from same connection updates in place.
	b.Attach(TabAttachment{
		UserID: "u1", AgentID: "a1", ConversationID: "c1",
		TabURL: "https://new.com", TabTitle: "New",
	}, "conn1")

	a := b.GetAttachment("u1", "a1", "c1")
	if a.TabURL != "https://new.com" {
		t.Errorf("expected updated URL, got %s", a.TabURL)
	}
}

func TestAttachReplacesStaleConnection(t *testing.T) {
	b := NewTabToolBroker()
	b.Attach(TabAttachment{
		UserID: "u1", AgentID: "a1", ConversationID: "c1",
		TabURL: "https://old.com",
	}, "conn-old")

	// Different connection replaces.
	b.Attach(TabAttachment{
		UserID: "u1", AgentID: "a1", ConversationID: "c1",
		TabURL: "https://new.com",
	}, "conn-new")

	a := b.GetAttachment("u1", "a1", "c1")
	if a.TabURL != "https://new.com" {
		t.Errorf("expected replacement URL, got %s", a.TabURL)
	}
}

func TestDetachOnlyOwner(t *testing.T) {
	b := NewTabToolBroker()
	b.Attach(TabAttachment{
		UserID: "u1", AgentID: "a1", ConversationID: "c1",
	}, "conn1")

	// Wrong connection can't detach.
	b.Detach("u1", "a1", "c1", "conn-other")
	if !b.HasAttachment("u1", "a1", "c1") {
		t.Fatal("expected attachment to survive wrong-connection detach")
	}

	// Correct connection can detach.
	b.Detach("u1", "a1", "c1", "conn1")
	if b.HasAttachment("u1", "a1", "c1") {
		t.Fatal("expected attachment to be removed")
	}
}

func TestGetAttachmentNonexistent(t *testing.T) {
	b := NewTabToolBroker()
	if b.GetAttachment("u1", "a1", "c999") != nil {
		t.Fatal("expected nil for nonexistent attachment")
	}
}

func TestListForUser(t *testing.T) {
	b := NewTabToolBroker()
	b.Attach(TabAttachment{
		UserID: "u1", AgentID: "a1", ConversationID: "c1",
	}, "conn1")
	b.Attach(TabAttachment{
		UserID: "u1", AgentID: "a1", ConversationID: "c2",
	}, "conn1")
	b.Attach(TabAttachment{
		UserID: "u2", AgentID: "a1", ConversationID: "c3",
	}, "conn2")

	list := b.ListForUser("u1")
	if len(list) != 2 {
		t.Fatalf("expected 2 attachments for u1, got %d", len(list))
	}
}

func TestPendingResolve(t *testing.T) {
	b := NewTabToolBroker()
	p := &PendingToolCall{
		ID:         "req1",
		UserID:     "u1",
		resultChan: MakeResultChan(),
	}
	b.RegisterPending(p)

	go func() {
		time.Sleep(10 * time.Millisecond)
		b.Resolve("req1", ToolCallResult{Result: `{"ok":true}`})
	}()

	result := <-p.resultChan
	if result.Result != `{"ok":true}` {
		t.Errorf("unexpected result: %s", result.Result)
	}
}

func TestPendingResolveNotFound(t *testing.T) {
	b := NewTabToolBroker()
	err := b.Resolve("nonexistent", ToolCallResult{})
	if err == nil {
		t.Fatal("expected error for nonexistent pending call")
	}
}

func TestPendingCancel(t *testing.T) {
	b := NewTabToolBroker()
	p := &PendingToolCall{
		ID:         "req1",
		resultChan: MakeResultChan(),
	}
	b.RegisterPending(p)
	b.CancelPending("req1")

	// Channel should be closed.
	_, ok := <-p.resultChan
	if ok {
		t.Fatal("expected channel to be closed after cancel")
	}
}

func TestDetachAllForConnection(t *testing.T) {
	b := NewTabToolBroker()

	b.Attach(TabAttachment{
		UserID: "u1", AgentID: "a1", ConversationID: "c1",
	}, "conn1")
	b.Attach(TabAttachment{
		UserID: "u1", AgentID: "a1", ConversationID: "c2",
	}, "conn1")
	b.Attach(TabAttachment{
		UserID: "u2", AgentID: "a1", ConversationID: "c3",
	}, "conn2")

	// Register a pending call for u1:c1.
	p := &PendingToolCall{
		ID:             "req1",
		UserID:         "u1",
		ConversationID: "c1",
		resultChan:     MakeResultChan(),
	}
	b.RegisterPending(p)

	b.DetachAllForConnection("conn1")

	if b.HasAttachment("u1", "a1", "c1") {
		t.Fatal("expected c1 to be detached")
	}
	if b.HasAttachment("u1", "a1", "c2") {
		t.Fatal("expected c2 to be detached")
	}
	if !b.HasAttachment("u2", "a1", "c3") {
		t.Fatal("expected c3 to survive (different connection)")
	}

	// Pending call should have been rejected.
	result := <-p.resultChan
	if result.Error != "extension disconnected" {
		t.Errorf("expected 'extension disconnected' error, got %q", result.Error)
	}
}

func TestCancelPendingForAttachment(t *testing.T) {
	b := NewTabToolBroker()
	p1 := &PendingToolCall{
		ID:             "req1",
		UserID:         "u1",
		AgentID:        "a1",
		ConversationID: "c1",
		resultChan:     MakeResultChan(),
	}
	p2 := &PendingToolCall{
		ID:             "req2",
		UserID:         "u1",
		AgentID:        "a1",
		ConversationID: "c2",
		resultChan:     MakeResultChan(),
	}
	b.RegisterPending(p1)
	b.RegisterPending(p2)

	b.CancelPendingForAttachment("u1", "a1", "c1")

	// p1 should be rejected.
	result := <-p1.resultChan
	if result.Error != "tab detached" {
		t.Errorf("expected 'tab detached' error, got %q", result.Error)
	}

	// p2 should still be pending (different conversation).
	select {
	case <-p2.resultChan:
		t.Fatal("p2 should not have been resolved")
	default:
		// good
	}
}

func TestConcurrentAccess(t *testing.T) {
	b := NewTabToolBroker()
	var wg sync.WaitGroup

	// Concurrent attach/detach.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			connID := "conn"
			b.Attach(TabAttachment{
				UserID: "u1", AgentID: "a1", ConversationID: "c1",
				TabURL: "https://example.com",
			}, connID)
			b.HasAttachment("u1", "a1", "c1")
			b.GetAttachment("u1", "a1", "c1")
			b.ListForUser("u1")
		}(i)
	}

	// Concurrent pending register/resolve.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := "pending-" + string(rune('A'+i%26))
			p := &PendingToolCall{
				ID:         id,
				UserID:     "u1",
				resultChan: MakeResultChan(),
			}
			b.RegisterPending(p)
			b.Resolve(id, ToolCallResult{Result: "ok"})
		}(i)
	}

	wg.Wait()
}
