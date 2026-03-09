package runners

import (
	"context"
	"fmt"
	"strings"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
)

// buildTodoOverlay returns a formatted TODO summary for the given conversation.
// It is best-effort: errors return ("", err) and the caller should silently skip.
func buildTodoOverlay(ctx context.Context, conversationID string) (string, error) {
	dataStore := store.StoreFromContextSafe(ctx)
	if dataStore == nil {
		return "", nil
	}

	var overlay string
	err := dataStore.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		todos, listErr := tx.ListTodos(ctx, store.TodoListOptions{
			ConversationID: &conversationID,
		}, nil)
		if listErr != nil {
			return listErr
		}

		overlay = formatTodoOverlay(todos)
		return nil
	})
	return overlay, err
}

// formatTodoOverlay builds the overlay string from a pre-sorted list of todos.
// The DB returns todos sorted: open first, then by priority (high>medium>low),
// then by created_at DESC.
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
			if len(openTodos) < 10 {
				openTodos = append(openTodos, todo)
			}
		}
	}

	if openCount+doneCount == 0 {
		return ""
	}

	var builder strings.Builder
	builder.WriteString("<todos>\n")
	fmt.Fprintf(&builder, "Open: %d | Done: %d\n", openCount, doneCount)

	if len(openTodos) > 0 {
		builder.WriteString("\n# Open TODOs (top 10 by priority)\n")
		for idx, todo := range openTodos {
			priority := priorityLabel(todo)
			title := todo.GetTitle()
			fmt.Fprintf(&builder, "%d. [%s] %s — %s\n", idx+1, priority, todo.ID, title)
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
