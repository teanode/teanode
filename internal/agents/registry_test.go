package agents

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/teanode/teanode/internal/configs"
)

func TestAgentRegistryForEachDoesNotHoldLockDuringCallback(t *testing.T) {
	registry := NewAgentRegistry()
	registry.Register("main", &Runner{})

	callbackEntered := make(chan struct{})
	releaseCallback := make(chan struct{})
	done := make(chan struct{})

	go func() {
		registry.ForEach(func(agentId string, runner *Runner) {
			close(callbackEntered)
			<-releaseCallback
		})
		close(done)
	}()

	select {
	case <-callbackEntered:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for callback to start")
	}

	setDefaultDone := make(chan struct{})
	go func() {
		_, _, _ = registry.EnsureDefaultAgent("user-1", "main")
		close(setDefaultDone)
	}()

	select {
	case <-setDefaultDone:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("EnsureDefaultAgent blocked while ForEach callback was running")
	}

	close(releaseCallback)

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("ForEach did not complete after callback was released")
	}
}

func TestAgentRegistryEnsureDefaultAgent(t *testing.T) {
	registry := NewAgentRegistry()
	registry.Register("main", &Runner{})
	registry.Register("research", &Runner{})
	agentId, assigned, err := registry.EnsureDefaultAgent("user-1", "main")
	if err != nil {
		t.Fatalf("EnsureDefaultAgent(user-1) error = %v", err)
	}
	if !assigned {
		t.Fatal("EnsureDefaultAgent(user-1) assigned = false, want true")
	}
	if agentId != "main" {
		t.Fatalf("EnsureDefaultAgent(user-1) agentId = %q, want %q", agentId, "main")
	}

	registry.mutex.Lock()
	state := registry.ensureUserStateLocked("user-1")
	if state == nil {
		registry.mutex.Unlock()
		t.Fatal("expected user state")
	}
	state.DefaultAgentID = "research"
	registry.mutex.Unlock()

	agentId, assigned, err = registry.EnsureDefaultAgent("user-1", "main")
	if err != nil {
		t.Fatalf("EnsureDefaultAgent(user-1) error = %v", err)
	}
	if assigned {
		t.Fatal("EnsureDefaultAgent(user-1) assigned = true, want false")
	}
	if agentId != "research" {
		t.Fatalf("EnsureDefaultAgent(user-1) agentId = %q, want %q", agentId, "research")
	}

	agentId, assigned, err = registry.EnsureDefaultAgent("user-2", "missing")
	if err == nil {
		t.Fatal("EnsureDefaultAgent(user-2) error = nil, want non-nil")
	}
	if assigned {
		t.Fatal("EnsureDefaultAgent(user-2) assigned = true, want false")
	}
	if agentId != "" {
		t.Fatalf("EnsureDefaultAgent(user-2) agentId = %q, want empty", agentId)
	}
}

func TestAgentRegistryDefaultConversationPersistsAcrossReload(t *testing.T) {
	temporaryDirectory := t.TempDir()
	configs.SetDirectory(temporaryDirectory)
	t.Cleanup(func() {
		configs.SetDirectory("")
	})

	registry := NewAgentRegistry()
	registry.Register("main", &Runner{})
	registry.SetDefaultConversation("user-1", "main", "conv-123")

	stateFilename := configs.StateFilename()
	if _, err := os.Stat(stateFilename); err != nil {
		t.Fatalf("state file not written at %s: %v", stateFilename, err)
	}
	if filepath.Dir(stateFilename) != temporaryDirectory {
		t.Fatalf("state file directory = %q, want %q", filepath.Dir(stateFilename), temporaryDirectory)
	}

	reloaded := NewAgentRegistry()
	reloaded.Register("main", &Runner{})
	reloaded.LoadState()

	defaultConversationID := reloaded.EnsureDefaultConversation("user-1", "main")
	if defaultConversationID != "conv-123" {
		t.Fatalf("default conversation after reload = %q, want %q", defaultConversationID, "conv-123")
	}
}
