package conversations

import (
	"context"
	"strings"
	"testing"

	"github.com/teanode/teanode/internal/store"
)

func newConversationStoreForTest(t *testing.T) *Store {
	t.Helper()
	testScopeID := strings.NewReplacer("/", "-", " ", "-").Replace(strings.ToLower(t.Name()))
	return NewStore(store.ContextWithStore(context.Background(), openedStoreForTests), "user-"+testScopeID, "agent-"+testScopeID)
}

func TestStoreAppendAndLoad(t *testing.T) {
	conversationStore := newConversationStoreForTest(t)
	conversationID := "test-conversation"

	messages, loadError := conversationStore.Load(conversationID)
	if loadError != nil {
		t.Fatalf("Load empty: %v", loadError)
	}
	if messages != nil {
		t.Fatalf("expected nil, got %v", messages)
	}

	messageOne := NewTextMessage("user", "hello", 1000)
	if appendError := conversationStore.Append(conversationID, messageOne); appendError != nil {
		t.Fatalf("Append user: %v", appendError)
	}

	messageTwo := NewTextMessage("assistant", "hi there", 2000)
	messageTwo.Model = "gpt-4o"
	messageTwo.StopReason = "stop"
	if appendError := conversationStore.Append(conversationID, messageTwo); appendError != nil {
		t.Fatalf("Append assistant: %v", appendError)
	}

	messages, loadError = conversationStore.Load(conversationID)
	if loadError != nil {
		t.Fatalf("Load: %v", loadError)
	}
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
}

func TestStoreListAndDelete(t *testing.T) {
	conversationStore := newConversationStoreForTest(t)
	if appendError := conversationStore.Append("conversation-a", NewTextMessage("user", "a", 1000)); appendError != nil {
		t.Fatalf("append conversation-a: %v", appendError)
	}
	if appendError := conversationStore.Append("conversation-b", NewTextMessage("user", "b", 2000)); appendError != nil {
		t.Fatalf("append conversation-b: %v", appendError)
	}

	conversationList, listError := conversationStore.List()
	if listError != nil {
		t.Fatalf("List: %v", listError)
	}
	if len(conversationList) != 2 {
		t.Fatalf("expected 2 conversations, got %d", len(conversationList))
	}

	if deleteError := conversationStore.Delete("conversation-a"); deleteError != nil {
		t.Fatalf("Delete: %v", deleteError)
	}
	conversationList, listError = conversationStore.List()
	if listError != nil {
		t.Fatalf("List after delete: %v", listError)
	}
	if len(conversationList) != 1 {
		t.Fatalf("expected 1 conversation after delete, got %d", len(conversationList))
	}
}

func TestStoreLoadPage(t *testing.T) {
	conversationStore := newConversationStoreForTest(t)
	conversationID := "paged-conversation"

	page, pageError := conversationStore.LoadPage(conversationID, 10, 0)
	if pageError != nil {
		t.Fatalf("LoadPage empty: %v", pageError)
	}
	if len(page.Messages) != 0 {
		t.Fatalf("expected 0 messages, got %d", len(page.Messages))
	}

	for index := 0; index < 10; index++ {
		if appendError := conversationStore.Append(conversationID, NewTextMessage("user", "m", int64(index))); appendError != nil {
			t.Fatalf("append %d: %v", index, appendError)
		}
	}

	page, pageError = conversationStore.LoadPage(conversationID, 3, 0)
	if pageError != nil {
		t.Fatalf("LoadPage: %v", pageError)
	}
	if len(page.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(page.Messages))
	}
	if !page.HasMore {
		t.Fatal("expected HasMore=true")
	}
}
