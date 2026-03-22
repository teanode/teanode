package api

import (
	"testing"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/util/ptrto"
)

func TestMergeJobUpdatePartialFields(t *testing.T) {
	original := &models.Job{
		ID:                "job-1",
		UserID:            ptrto.Value("user-1"),
		Name:              ptrto.Value("Daily Report"),
		Schedule:          ptrto.Value("0 9 * * *"),
		Prompt:            ptrto.Value("Generate a daily report"),
		ProviderModelName: ptrto.Value("openai/gpt-4"),
		AgentID:           ptrto.Value("agent-1"),
		ConversationID:    ptrto.Value("conv-1"),
		Enabled:           ptrto.Value(true),
		CreatedAt:         ptrto.Value(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)),
		LastRunAt:         ptrto.Value(time.Date(2025, 6, 1, 9, 0, 0, 0, time.UTC)),
	}

	// Patch changes only the name and prompt.
	patch := &models.Job{
		Name:   ptrto.Value("Weekly Report"),
		Prompt: ptrto.Value("Generate a weekly report"),
	}

	mergeJobUpdate(original, patch)

	if original.GetName() != "Weekly Report" {
		t.Errorf("Name: got %q, want %q", original.GetName(), "Weekly Report")
	}
	if original.GetPrompt() != "Generate a weekly report" {
		t.Errorf("Prompt: got %q, want %q", original.GetPrompt(), "Generate a weekly report")
	}
	// Unchanged fields must be preserved.
	if original.GetSchedule() != "0 9 * * *" {
		t.Errorf("Schedule should be preserved, got %q", original.GetSchedule())
	}
	if original.GetProviderModelName() != "openai/gpt-4" {
		t.Errorf("ProviderModelName should be preserved, got %q", original.GetProviderModelName())
	}
	if original.GetAgentID() != "agent-1" {
		t.Errorf("AgentID should be preserved, got %q", original.GetAgentID())
	}
	if original.GetConversationID() != "conv-1" {
		t.Errorf("ConversationID should be preserved, got %q", original.GetConversationID())
	}
	if original.GetEnabled() != true {
		t.Error("Enabled should be preserved as true")
	}
	// Metadata fields must not be wiped.
	if original.CreatedAt == nil {
		t.Error("CreatedAt should be preserved")
	}
	if original.LastRunAt == nil {
		t.Error("LastRunAt should be preserved")
	}
	if original.GetUserID() != "user-1" {
		t.Errorf("UserID should be preserved, got %q", original.GetUserID())
	}
}

func TestMergeJobUpdateAllFields(t *testing.T) {
	original := &models.Job{
		ID:                "job-1",
		Name:              ptrto.Value("Old Name"),
		Schedule:          ptrto.Value("0 9 * * *"),
		Prompt:            ptrto.Value("Old prompt"),
		ProviderModelName: ptrto.Value("openai/gpt-4"),
		AgentID:           ptrto.Value("agent-1"),
		Enabled:           ptrto.Value(true),
	}

	patch := &models.Job{
		Name:              ptrto.Value("New Name"),
		Schedule:          ptrto.Value("0 18 * * *"),
		Prompt:            ptrto.Value("New prompt"),
		ProviderModelName: ptrto.Value("anthropic/claude-3"),
		AgentID:           ptrto.Value("agent-2"),
		Enabled:           ptrto.Value(false),
	}

	mergeJobUpdate(original, patch)

	if original.GetName() != "New Name" {
		t.Errorf("Name: got %q, want %q", original.GetName(), "New Name")
	}
	if original.GetSchedule() != "0 18 * * *" {
		t.Errorf("Schedule: got %q, want %q", original.GetSchedule(), "0 18 * * *")
	}
	if original.GetPrompt() != "New prompt" {
		t.Errorf("Prompt: got %q, want %q", original.GetPrompt(), "New prompt")
	}
	if original.GetProviderModelName() != "anthropic/claude-3" {
		t.Errorf("ProviderModelName: got %q, want %q", original.GetProviderModelName(), "anthropic/claude-3")
	}
	if original.GetAgentID() != "agent-2" {
		t.Errorf("AgentID: got %q, want %q", original.GetAgentID(), "agent-2")
	}
	if original.GetEnabled() != false {
		t.Error("Enabled: got true, want false")
	}
}

func TestMergeJobUpdateEmptyPatch(t *testing.T) {
	original := &models.Job{
		ID:       "job-1",
		Name:     ptrto.Value("Keep Me"),
		Schedule: ptrto.Value("0 9 * * *"),
		Prompt:   ptrto.Value("Keep this too"),
	}

	patch := &models.Job{}

	mergeJobUpdate(original, patch)

	if original.GetName() != "Keep Me" {
		t.Errorf("Name should be unchanged, got %q", original.GetName())
	}
	if original.GetSchedule() != "0 9 * * *" {
		t.Errorf("Schedule should be unchanged, got %q", original.GetSchedule())
	}
	if original.GetPrompt() != "Keep this too" {
		t.Errorf("Prompt should be unchanged, got %q", original.GetPrompt())
	}
}
