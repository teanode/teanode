package conversations

import (
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
