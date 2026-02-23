package agents

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/conversations"
)

func TestSummarizerSummarizeAllIteratesAllUsersAndAgents(t *testing.T) {
	t.Setenv("TEANODE_DIR", t.TempDir())

	for _, userId := range []string{"user-b", "user-a"} {
		userDirectory, err := configs.UserDirectory(userId)
		if err != nil {
			t.Fatalf("UserDirectory(%q): %v", userId, err)
		}
		if err := os.MkdirAll(userDirectory, 0755); err != nil {
			t.Fatalf("MkdirAll(%q): %v", userDirectory, err)
		}
	}

	var mutex sync.Mutex
	calls := map[string]int{}
	newRunner := func(agentId string) *Runner {
		return &Runner{
			AgentID: agentId,
			ResolveConversations: func(userId, resolvedAgentId string) *conversations.Store {
				mutex.Lock()
				calls[userId+":"+resolvedAgentId]++
				mutex.Unlock()
				return conversations.NewStore(filepath.Join(t.TempDir(), userId, resolvedAgentId))
			},
			Config: &configs.Config{},
		}
	}

	registry := NewAgentRegistry()
	registry.Register("agent-a", newRunner("agent-a"))
	registry.Register("agent-b", newRunner("agent-b"))

	summarizer := NewSummarizer(registry, &configs.Config{})
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

func TestSummarizerSkipsNilConversationStore(t *testing.T) {
	t.Setenv("TEANODE_DIR", t.TempDir())

	userDirectory, err := configs.UserDirectory("user-1")
	if err != nil {
		t.Fatalf("UserDirectory: %v", err)
	}
	if err := os.MkdirAll(userDirectory, 0755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", userDirectory, err)
	}

	registry := NewAgentRegistry()
	registry.Register("agent-a", &Runner{
		AgentID: "agent-a",
		ResolveConversations: func(userId, agentId string) *conversations.Store {
			return nil
		},
		Config: &configs.Config{},
	})

	summarizer := NewSummarizer(registry, &configs.Config{})
	summarizer.summarizeAll(context.Background())
}

