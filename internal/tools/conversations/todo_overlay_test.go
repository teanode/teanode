package conversations

import (
	"context"
	"strings"
	"testing"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/store/fsstore"
	"github.com/teanode/teanode/internal/util/ptrto"
)

func setupOverlayStore(t *testing.T) (context.Context, store.Store) {
	t.Helper()
	opened, err := fsstore.Open(fsstore.Options{DataDirectory: t.TempDir()})
	if err != nil {
		t.Fatalf("opening store: %v", err)
	}
	if err := opened.Migrate(context.Background()); err != nil {
		t.Fatalf("migrating: %v", err)
	}
	t.Cleanup(func() { _ = opened.Close() })
	ctx := store.ContextWithStore(context.Background(), opened)
	return ctx, opened
}

// createTestConversation creates a user, agent, and conversation, returning the conversation ID.
func createTestConversation(t *testing.T, ctx context.Context, dataStore store.Store, userID, agentID string) string {
	t.Helper()
	if err := dataStore.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		if _, err := tx.CreateUser(ctx, &models.User{ID: userID, Username: ptrto.Value(userID), Admin: ptrto.Value(true)}, nil, nil); err != nil {
			return err
		}
		if _, err := tx.CreateAgent(ctx, &models.Agent{ID: agentID, Name: ptrto.Value("Agent")}, nil, nil); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatalf("creating overlay dependencies: %v", err)
	}
	var convID string
	err := dataStore.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		conv, createErr := tx.CreateConversation(ctx, &models.Conversation{
			UserID:  ptrto.Value(userID),
			AgentID: ptrto.Value(agentID),
		}, nil)
		if createErr != nil {
			return createErr
		}
		convID = conv.ID
		return nil
	})
	if err != nil {
		t.Fatalf("creating conversation: %v", err)
	}
	return convID
}

func createTodo(t *testing.T, ctx context.Context, dataStore store.Store, convID string, title string, status models.TodoStatus, priority models.TodoPriority) {
	t.Helper()
	err := dataStore.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		_, createErr := tx.CreateTodo(ctx, &models.Todo{
			ConversationID: &convID,
			Title:          &title,
			Status:         &status,
			Priority:       &priority,
		}, nil)
		return createErr
	})
	if err != nil {
		t.Fatalf("creating todo: %v", err)
	}
}

func createTodoWithDescription(t *testing.T, ctx context.Context, dataStore store.Store, convID string, title string, description string, status models.TodoStatus, priority models.TodoPriority) {
	t.Helper()
	err := dataStore.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		_, createErr := tx.CreateTodo(ctx, &models.Todo{
			ConversationID: &convID,
			Title:          &title,
			Description:    &description,
			Status:         &status,
			Priority:       &priority,
		}, nil)
		return createErr
	})
	if err != nil {
		t.Fatalf("creating todo: %v", err)
	}
}

func TestBuildTodoOverlay_NoTodos(t *testing.T) {
	ctx, _ := setupOverlayStore(t)
	result, err := buildTodoOverlay(ctx, "conv-no-todos")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty overlay, got: %s", result)
	}
}

func TestBuildTodoOverlay_OnlyDoneTodos(t *testing.T) {
	ctx, dataStore := setupOverlayStore(t)
	convID := createTestConversation(t, ctx, dataStore, "user1", "agent1")

	for idx := 0; idx < 3; idx++ {
		createTodo(t, ctx, dataStore, convID, "Done task", models.TodoStatusDone, models.TodoPriorityMedium)
	}

	result, err := buildTodoOverlay(ctx, convID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		t.Fatal("expected non-empty overlay for done todos")
	}
	if !strings.Contains(result, "Open: 0 | Done: 3") {
		t.Errorf("expected counts 'Open: 0 | Done: 3', got: %s", result)
	}
	if strings.Contains(result, "# Open TODOs") {
		t.Error("should not contain open todos section when there are no open todos")
	}
}

func TestBuildTodoOverlay_MixedOpenDone(t *testing.T) {
	ctx, dataStore := setupOverlayStore(t)
	convID := createTestConversation(t, ctx, dataStore, "user2", "agent2")

	for idx := 0; idx < 5; idx++ {
		createTodo(t, ctx, dataStore, convID, "Open task", models.TodoStatusOpen, models.TodoPriorityMedium)
	}
	for idx := 0; idx < 2; idx++ {
		createTodo(t, ctx, dataStore, convID, "Done task", models.TodoStatusDone, models.TodoPriorityMedium)
	}

	result, err := buildTodoOverlay(ctx, convID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Open: 5 | Done: 2") {
		t.Errorf("expected counts 'Open: 5 | Done: 2', got: %s", result)
	}
	if !strings.Contains(result, "<todos>") || !strings.Contains(result, "</todos>") {
		t.Error("expected <todos> wrapper tags")
	}
}

func TestBuildTodoOverlay_MoreThan10Open(t *testing.T) {
	ctx, dataStore := setupOverlayStore(t)
	convID := createTestConversation(t, ctx, dataStore, "user3", "agent3")

	for idx := 0; idx < 15; idx++ {
		createTodo(t, ctx, dataStore, convID, "Open task", models.TodoStatusOpen, models.TodoPriorityMedium)
	}

	result, err := buildTodoOverlay(ctx, convID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Open: 15 | Done: 0") {
		t.Errorf("expected counts 'Open: 15 | Done: 0', got: %s", result)
	}
	// Count numbered list entries
	lineCount := 0
	for _, line := range strings.Split(result, "\n") {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) > 1 && trimmed[0] >= '1' && trimmed[0] <= '9' && strings.Contains(trimmed, ". [") {
			lineCount++
		}
	}
	if lineCount != 10 {
		t.Errorf("expected 10 listed todos, got %d", lineCount)
	}
}

func TestBuildTodoOverlay_OldestFirstOrdering(t *testing.T) {
	ctx, dataStore := setupOverlayStore(t)
	convID := createTestConversation(t, ctx, dataStore, "user4", "agent4")

	// Create in reverse priority order; overlay should show oldest-first regardless of priority
	for idx := 0; idx < 3; idx++ {
		createTodo(t, ctx, dataStore, convID, "Low task", models.TodoStatusOpen, models.TodoPriorityLow)
	}
	for idx := 0; idx < 3; idx++ {
		createTodo(t, ctx, dataStore, convID, "High task", models.TodoStatusOpen, models.TodoPriorityHigh)
	}
	for idx := 0; idx < 3; idx++ {
		createTodo(t, ctx, dataStore, convID, "Medium task", models.TodoStatusOpen, models.TodoPriorityMedium)
	}

	result, err := buildTodoOverlay(ctx, convID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Low tasks were created first, so they should appear before High tasks
	lowIdx := strings.Index(result, "[LOW]")
	highIdx := strings.Index(result, "[HIGH]")
	medIdx := strings.Index(result, "[MEDIUM]")

	if lowIdx < 0 || highIdx < 0 || medIdx < 0 {
		t.Fatalf("missing priority labels in overlay: %s", result)
	}
	if lowIdx >= highIdx {
		t.Errorf("LOW (created first) should appear before HIGH (created later): low=%d high=%d", lowIdx, highIdx)
	}
	if highIdx >= medIdx {
		t.Errorf("HIGH (created second) should appear before MEDIUM (created last): high=%d medium=%d", highIdx, medIdx)
	}
}

func TestBuildTodoOverlay_DescriptionTruncation(t *testing.T) {
	ctx, dataStore := setupOverlayStore(t)
	convID := createTestConversation(t, ctx, dataStore, "user5", "agent5")

	longDesc := strings.Repeat("a", 200)
	createTodoWithDescription(t, ctx, dataStore, convID, "Long desc", longDesc, models.TodoStatusOpen, models.TodoPriorityHigh)

	result, err := buildTodoOverlay(ctx, convID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	truncated := strings.Repeat("a", 120) + "…"
	if !strings.Contains(result, truncated) {
		t.Errorf("expected truncated description in overlay")
	}
	if strings.Contains(result, longDesc) {
		t.Error("description should be truncated, not full length")
	}
}

func TestBuildTodoOverlay_NilDescription(t *testing.T) {
	ctx, dataStore := setupOverlayStore(t)
	convID := createTestConversation(t, ctx, dataStore, "user6", "agent6")

	createTodo(t, ctx, dataStore, convID, "No description", models.TodoStatusOpen, models.TodoPriorityMedium)

	result, err := buildTodoOverlay(ctx, convID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "No description") {
		t.Error("expected title in overlay")
	}
	// Verify no indented description line follows the todo entry
	lines := strings.Split(result, "\n")
	for idx, line := range lines {
		if strings.Contains(line, "No description") && idx+1 < len(lines) {
			nextLine := lines[idx+1]
			if strings.HasPrefix(nextLine, "   ") {
				trimmed := strings.TrimSpace(nextLine)
				// Only fail if it looks like a description line
				if trimmed != "" && !strings.HasPrefix(trimmed, "Reminder") && !strings.HasPrefix(trimmed, "#") {
					if len(trimmed) == 0 || trimmed[0] < '0' || trimmed[0] > '9' {
						t.Errorf("no description line expected, but found: %q", nextLine)
					}
				}
			}
		}
	}
}

func TestBuildTodoOverlay_NoStoreInContext(t *testing.T) {
	ctx := context.Background()
	result, err := buildTodoOverlay(ctx, "conv-no-store")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty overlay without store, got: %s", result)
	}
}

func TestBuildTodoOverlay_ConversationIsolation(t *testing.T) {
	ctx, dataStore := setupOverlayStore(t)

	convA := createTestConversation(t, ctx, dataStore, "userA", "agentA")
	convB := createTestConversation(t, ctx, dataStore, "userB", "agentB")

	createTodo(t, ctx, dataStore, convA, "Task for A", models.TodoStatusOpen, models.TodoPriorityHigh)
	createTodo(t, ctx, dataStore, convB, "Task for B", models.TodoStatusOpen, models.TodoPriorityHigh)

	resultA, err := buildTodoOverlay(ctx, convA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resultB, err := buildTodoOverlay(ctx, convB)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(resultA, "Task for A") {
		t.Error("conv A overlay should contain A's task")
	}
	if strings.Contains(resultA, "Task for B") {
		t.Error("conv A overlay should NOT contain B's task")
	}
	if !strings.Contains(resultB, "Task for B") {
		t.Error("conv B overlay should contain B's task")
	}
	if strings.Contains(resultB, "Task for A") {
		t.Error("conv B overlay should NOT contain A's task")
	}
}

func TestFormatTodoOverlay_Empty(t *testing.T) {
	result := formatTodoOverlay(nil)
	if result != "" {
		t.Errorf("expected empty string for nil todos, got: %s", result)
	}
}

func TestFormatTodoOverlay_ReminderAlwaysPresent(t *testing.T) {
	todos := []*models.Todo{
		{
			ID:       "todo-1",
			Title:    ptrto.Value("Test"),
			Status:   ptrto.Value(models.TodoStatusOpen),
			Priority: ptrto.Value(models.TodoPriorityMedium),
		},
	}
	result := formatTodoOverlay(todos)
	if !strings.Contains(result, "Reminder: mark completed todos done") {
		t.Error("expected reminder text in overlay")
	}
}

func TestTruncateDescription(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{"short", "hello", 120, "hello"},
		{"exact", strings.Repeat("x", 120), 120, strings.Repeat("x", 120)},
		{"long", strings.Repeat("x", 200), 120, strings.Repeat("x", 120) + "…"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := truncateDescription(tc.input, tc.maxLen)
			if got != tc.expected {
				t.Errorf("truncateDescription(%d chars, %d) = %d chars, want %d chars", len(tc.input), tc.maxLen, len(got), len(tc.expected))
			}
		})
	}
}
