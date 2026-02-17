package conversations

import (
	"fmt"
	"os"
	"testing"
)

func TestStoreAppendAndLoad(t *testing.T) {
	directory := t.TempDir()
	store := NewStore(directory)

	key := "test-conversation"

	// Load from non-existent conversation returns nil.
	messages, err := store.Load(key)
	if err != nil {
		t.Fatalf("Load empty: %v", err)
	}
	if messages != nil {
		t.Fatalf("expected nil, got %v", messages)
	}

	// Append a user message.
	message1 := NewTextMessage("user", "hello", 1000)
	if err := store.Append(key, message1); err != nil {
		t.Fatalf("Append user: %v", err)
	}

	// Append an assistant message.
	message2 := NewTextMessage("assistant", "hi there", 2000)
	message2.Model = "gpt-4o"
	message2.StopReason = "stop"
	if err := store.Append(key, message2); err != nil {
		t.Fatalf("Append assistant: %v", err)
	}

	// Load and verify.
	messages, err = store.Load(key)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
	if messages[0].Role != "user" || messages[0].ContentText() != "hello" {
		t.Errorf("msg[0] = %v", messages[0])
	}
	if messages[1].Role != "assistant" || messages[1].ContentText() != "hi there" {
		t.Errorf("msg[1] = %v", messages[1])
	}
	if messages[1].Model != "gpt-4o" {
		t.Errorf("msg[1].Model = %q", messages[1].Model)
	}

	// Verify JSONL file was created.
	conversationPath, _ := store.path(key)
	info, err := os.Stat(conversationPath)
	if err != nil {
		t.Fatalf("conversation file: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("conversation file is empty")
	}
}

func TestStoreList(t *testing.T) {
	directory := t.TempDir()
	store := NewStore(directory)

	// Empty list.
	conversations, err := store.List()
	if err != nil {
		t.Fatalf("List empty: %v", err)
	}
	if len(conversations) != 0 {
		t.Fatalf("expected 0, got %d", len(conversations))
	}

	// Create two conversations.
	store.Append("conversation-a", NewTextMessage("user", "a", 1000))
	store.Append("conversation-b", NewTextMessage("user", "b", 2000))

	conversations, err = store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(conversations) != 2 {
		t.Fatalf("expected 2, got %d", len(conversations))
	}
	// Should have ids and positive timestamps.
	for _, entry := range conversations {
		if entry.ID == "" {
			t.Error("empty conversation id")
		}
		if entry.LastActive <= 0 {
			t.Errorf("conversation %q has invalid lastActive: %d", entry.ID, entry.LastActive)
		}
	}
}

func TestStoreDelete(t *testing.T) {
	directory := t.TempDir()
	store := NewStore(directory)

	store.Append("to-delete", NewTextMessage("user", "bye", 1000))

	if err := store.Delete("to-delete"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	messages, err := store.Load("to-delete")
	if err != nil {
		t.Fatalf("Load after delete: %v", err)
	}
	if messages != nil {
		t.Fatalf("expected nil after delete, got %v", messages)
	}

	// Deleting non-existent is a no-op.
	if err := store.Delete("nonexistent"); err != nil {
		t.Fatalf("Delete nonexistent: %v", err)
	}
}

func TestStoreLoadPage(t *testing.T) {
	directory := t.TempDir()
	store := NewStore(directory)
	key := "paged-conversation"

	// LoadPage on a non-existent conversation returns empty result.
	page, err := store.LoadPage(key, 10, 0)
	if err != nil {
		t.Fatalf("LoadPage empty: %v", err)
	}
	if len(page.Messages) != 0 {
		t.Fatalf("expected 0 messages, got %d", len(page.Messages))
	}
	if page.HasMore {
		t.Fatal("expected HasMore=false for empty conversation")
	}

	// Create 10 messages.
	for index := 0; index < 10; index++ {
		store.Append(key, NewTextMessage("user", fmt.Sprintf("message-%d", index), int64(index*1000)))
	}

	// Load last 3 messages (beforeIndex=0 means from the end).
	page, err = store.LoadPage(key, 3, 0)
	if err != nil {
		t.Fatalf("LoadPage last 3: %v", err)
	}
	if len(page.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(page.Messages))
	}
	if page.TotalCount != 10 {
		t.Errorf("expected TotalCount=10, got %d", page.TotalCount)
	}
	if page.OldestLoadedIndex != 7 {
		t.Errorf("expected OldestLoadedIndex=7, got %d", page.OldestLoadedIndex)
	}
	if !page.HasMore {
		t.Error("expected HasMore=true")
	}
	if page.Messages[0].ContentText() != "message-7" {
		t.Errorf("expected message-7, got %q", page.Messages[0].ContentText())
	}
	if page.Messages[2].ContentText() != "message-9" {
		t.Errorf("expected message-9, got %q", page.Messages[2].ContentText())
	}

	// Load older page using beforeIndex from previous result.
	page, err = store.LoadPage(key, 3, page.OldestLoadedIndex)
	if err != nil {
		t.Fatalf("LoadPage older: %v", err)
	}
	if len(page.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(page.Messages))
	}
	if page.OldestLoadedIndex != 4 {
		t.Errorf("expected OldestLoadedIndex=4, got %d", page.OldestLoadedIndex)
	}
	if !page.HasMore {
		t.Error("expected HasMore=true")
	}
	if page.Messages[0].ContentText() != "message-4" {
		t.Errorf("expected message-4, got %q", page.Messages[0].ContentText())
	}

	// Load remaining messages — should exhaust all messages.
	page, err = store.LoadPage(key, 10, page.OldestLoadedIndex)
	if err != nil {
		t.Fatalf("LoadPage remaining: %v", err)
	}
	if len(page.Messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(page.Messages))
	}
	if page.OldestLoadedIndex != 0 {
		t.Errorf("expected OldestLoadedIndex=0, got %d", page.OldestLoadedIndex)
	}
	if page.HasMore {
		t.Error("expected HasMore=false")
	}
	if page.Messages[0].ContentText() != "message-0" {
		t.Errorf("expected message-0, got %q", page.Messages[0].ContentText())
	}

	// Load all at once (limit bigger than total).
	page, err = store.LoadPage(key, 100, 0)
	if err != nil {
		t.Fatalf("LoadPage all: %v", err)
	}
	if len(page.Messages) != 10 {
		t.Fatalf("expected 10 messages, got %d", len(page.Messages))
	}
	if page.HasMore {
		t.Error("expected HasMore=false when loading all")
	}
}
