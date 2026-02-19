//go:build !linux

package cmd

import (
	"context"
	"strings"
	"testing"
)

func TestNewTerminalCommandStub(t *testing.T) {
	command := NewTerminalCommand()
	if command == nil {
		t.Fatal("expected terminal command")
	}
	if command.Name != "terminal" {
		t.Fatalf("unexpected command name: %q", command.Name)
	}
	if command.Action == nil {
		t.Fatal("expected command action")
	}

	err := command.Action(context.Background(), command)
	if err == nil {
		t.Fatal("expected unsupported-platform error")
	}
	if !strings.Contains(err.Error(), "only supported on Linux") {
		t.Fatalf("unexpected error: %v", err)
	}
}
