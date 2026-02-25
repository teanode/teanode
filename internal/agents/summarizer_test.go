package agents

import (
	"context"
	"sync"
	"testing"

	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/conversations"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	storefs "github.com/teanode/teanode/internal/store/fs"
)

func TestSummarizerSummarizeAllIteratesAllUsersAndAgents(t *testing.T) {
	baseDirectory := t.TempDir()
	configs.SetDirectory(baseDirectory)
	defer configs.SetDirectory("")

	openedStore, openError := storefs.Open(storefs.Options{DataDirectory: baseDirectory})
	if openError != nil {
		t.Fatalf("open store: %v", openError)
	}
	defer openedStore.Close()
	if createError := openedStore.Transaction(func(transaction store.Transaction) error {
		for _, userID := range []string{"user-b", "user-a"} {
			username := userID
			admin := false
			if _, err := transaction.CreateUser(&models.User{
				ID:       userID,
				Username: &username,
				Admin:    &admin,
			}, nil, nil); err != nil {
				return err
			}
		}
		return nil
	}); createError != nil {
		t.Fatalf("create users: %v", createError)
	}
	contextWithStore := store.ContextWithStore(context.Background(), openedStore)

	var mutex sync.Mutex
	calls := map[string]int{}
	newRunner := func(agentId string) *Runner {
		return &Runner{
			AgentID: agentId,
			ResolveConversations: func(userId, resolvedAgentId string) *conversations.Store {
				mutex.Lock()
				calls[userId+":"+resolvedAgentId]++
				mutex.Unlock()
				return conversations.NewStore(contextWithStore, userId, resolvedAgentId)
			},
			Config: &configs.Config{},
		}
	}

	registry := NewAgentRegistry(contextWithStore)
	registry.Register("agent-a", newRunner("agent-a"))
	registry.Register("agent-b", newRunner("agent-b"))

	summarizer := NewSummarizer(contextWithStore, registry, &configs.Config{})
	summarizer.summarizeAll(context.Background())

	expected := []string{
		"user-a:agent-a",
		"user-a:agent-b",
		"user-b:agent-a",
		"user-b:agent-b",
	}
	for _, key := range expected {
		if calls[key] != 1 {
			t.Fatalf("resolver calls[%q] = %d, want 1", key, calls[key])
		}
	}
	if len(calls) != len(expected) {
		t.Fatalf("resolver call cardinality = %d, want %d", len(calls), len(expected))
	}
}

func TestSummarizerSummarizeAllWithEmptyConversationStore(t *testing.T) {
	baseDirectory := t.TempDir()
	configs.SetDirectory(baseDirectory)
	defer configs.SetDirectory("")

	openedStore, openError := storefs.Open(storefs.Options{DataDirectory: baseDirectory})
	if openError != nil {
		t.Fatalf("open store: %v", openError)
	}
	defer openedStore.Close()
	if createError := openedStore.Transaction(func(transaction store.Transaction) error {
		userID := "user-1"
		username := userID
		admin := false
		_, err := transaction.CreateUser(&models.User{
			ID:       userID,
			Username: &username,
			Admin:    &admin,
		}, nil, nil)
		return err
	}); createError != nil {
		t.Fatalf("create user: %v", createError)
	}
	contextWithStore := store.ContextWithStore(context.Background(), openedStore)

	registry := NewAgentRegistry(contextWithStore)
	registry.Register("agent-a", &Runner{
		AgentID: "agent-a",
		ResolveConversations: func(userId, agentId string) *conversations.Store {
			return conversations.NewStore(contextWithStore, userId, agentId)
		},
		Config: &configs.Config{},
	})

	summarizer := NewSummarizer(contextWithStore, registry, &configs.Config{})
	summarizer.summarizeAll(context.Background())
}
