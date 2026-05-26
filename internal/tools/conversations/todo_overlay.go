package conversations

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/runners"
	"github.com/teanode/teanode/internal/store"
)

// BuildOverlay implements tools.OverlayBuilder. It returns a formatted TODO
// summary for the current conversation.
func (self *conversationTodoTool) BuildOverlay(ctx context.Context) (string, error) {
	runner := runners.RunnerFromContext(ctx)
	if runner == nil || runner.ConversationID == "" {
		return "", nil
	}
	return buildTodoOverlay(ctx, runner.ConversationID)
}

// buildTodoOverlay returns a formatted TODO summary for the given conversation.
// It is best-effort: errors return ("", err) and the caller should silently skip.
func buildTodoOverlay(ctx context.Context, conversationId string) (string, error) {
	dataStore := store.StoreFromContextSafe(ctx)
	if dataStore == nil {
		return "", nil
	}

	var overlay string
	err := dataStore.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		todos, listErr := tx.ListTodos(ctx, store.TodoListOptions{
			ConversationID: &conversationId,
		}, nil)
		if listErr != nil {
			return listErr
		}

		overlay = formatTodoOverlay(todos)
		return nil
	})
	return overlay, err
}

// formatTodoOverlay builds the overlay string from a list of todos.
// Open items are displayed oldest-first (ascending creation time), capped at 10.
func formatTodoOverlay(todos []*models.Todo) string {
	var openCount, doneCount int
	var openTodos []*models.Todo

	for _, todo := range todos {
		status := models.TodoStatusOpen
		if todo.Status != nil {
			status = *todo.Status
		}
		switch status {
		case models.TodoStatusDone:
			doneCount++
		default:
			openCount++
			openTodos = append(openTodos, todo)
		}
	}

	// Sort open items oldest-first (ascending creation time).
	sort.Slice(openTodos, func(i, j int) bool {
		a, b := openTodos[i], openTodos[j]
		if a.CreatedAt != nil && b.CreatedAt != nil {
			return a.CreatedAt.Before(*b.CreatedAt)
		}
		return a.ID < b.ID
	})
	if len(openTodos) > 10 {
		openTodos = openTodos[:10]
	}

	if openCount+doneCount == 0 {
		return ""
	}

	var builder strings.Builder
	builder.WriteString("<todos>\n")
	fmt.Fprintf(&builder, "Open: %d | Done: %d\n", openCount, doneCount)

	if len(openTodos) > 0 {
		builder.WriteString("\n# Open TODOs (top 10 by priority)\n")
		for index, todo := range openTodos {
			priority := priorityLabel(todo)
			title := todo.GetTitle()
			fmt.Fprintf(&builder, "%d. [%s] %s — %s\n", index+1, priority, todo.ID, title)
			if todo.Description != nil && *todo.Description != "" {
				description := truncateDescription(*todo.Description, 120)
				fmt.Fprintf(&builder, "   %s\n", description)
			}
		}
	}

	builder.WriteString("\nReminder: mark completed todos done via the todo tool. Prune old done items when the list gets noisy.\n")
	builder.WriteString("</todos>")

	return builder.String()
}

func priorityLabel(todo *models.Todo) string {
	if todo.Priority == nil {
		return "MEDIUM"
	}
	switch *todo.Priority {
	case models.TodoPriorityHigh:
		return "HIGH"
	case models.TodoPriorityLow:
		return "LOW"
	default:
		return "MEDIUM"
	}
}

func truncateDescription(description string, maxLength int) string {
	if len(description) <= maxLength {
		return description
	}
	return description[:maxLength] + "…"
}
