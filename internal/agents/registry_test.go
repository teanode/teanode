package agents

import (
	"testing"
	"time"
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
		registry.SetDefault("main")
		close(setDefaultDone)
	}()

	select {
	case <-setDefaultDone:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("SetDefault blocked while ForEach callback was running")
	}

	close(releaseCallback)

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("ForEach did not complete after callback was released")
	}
}

func TestAgentRegistryDefaultIDForUser(t *testing.T) {
	registry := NewAgentRegistry()
	registry.Register("main", &Runner{})
	registry.Register("research", &Runner{})
	registry.SetDefault("main")
	if got := registry.DefaultIDForUser("user-1"); got != "" {
		t.Fatalf("DefaultIDForUser(user-1) = %q, want empty", got)
	}

	registry.mutex.Lock()
	state := registry.ensureUserStateLocked("user-1")
	if state == nil {
		registry.mutex.Unlock()
		t.Fatal("expected user state")
	}
	state.DefaultAgentId = "research"
	registry.mutex.Unlock()

	if got := registry.DefaultIDForUser("user-1"); got != "research" {
		t.Fatalf("DefaultIDForUser(user-1) = %q, want %q", got, "research")
	}
	if got := registry.DefaultIDForUser("user-2"); got != "" {
		t.Fatalf("DefaultIDForUser(user-2) = %q, want empty", got)
	}
}
