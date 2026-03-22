package api

import (
	"testing"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/util/ptrto"
)

func countTodosByStatus(todos []*models.Todo) (int, int) {
	openCount, doneCount := 0, 0
	for _, todo := range todos {
		switch todo.GetStatus() {
		case models.TodoStatusOpen:
			openCount++
		case models.TodoStatusDone:
			doneCount++
		}
	}
	return openCount, doneCount
}

func TestCountTodosByStatusEmpty(t *testing.T) {
	openCount, doneCount := countTodosByStatus(nil)
	if openCount != 0 || doneCount != 0 {
		t.Errorf("expected (0, 0), got (%d, %d)", openCount, doneCount)
	}
}

func TestCountTodosByStatusMixed(t *testing.T) {
	todos := []*models.Todo{
		{ID: "1", Status: ptrto.Value(models.TodoStatusOpen)},
		{ID: "2", Status: ptrto.Value(models.TodoStatusOpen)},
		{ID: "3", Status: ptrto.Value(models.TodoStatusDone)},
		{ID: "4", Status: ptrto.Value(models.TodoStatusOpen)},
		{ID: "5", Status: ptrto.Value(models.TodoStatusDone)},
	}
	openCount, doneCount := countTodosByStatus(todos)
	if openCount != 3 {
		t.Errorf("openCount: got %d, want 3", openCount)
	}
	if doneCount != 2 {
		t.Errorf("doneCount: got %d, want 2", doneCount)
	}
}

func TestCountTodosByStatusAllOpen(t *testing.T) {
	todos := []*models.Todo{
		{ID: "1", Status: ptrto.Value(models.TodoStatusOpen)},
		{ID: "2", Status: ptrto.Value(models.TodoStatusOpen)},
	}
	openCount, doneCount := countTodosByStatus(todos)
	if openCount != 2 {
		t.Errorf("openCount: got %d, want 2", openCount)
	}
	if doneCount != 0 {
		t.Errorf("doneCount: got %d, want 0", doneCount)
	}
}

func TestCountTodosByStatusAllDone(t *testing.T) {
	todos := []*models.Todo{
		{ID: "1", Status: ptrto.Value(models.TodoStatusDone)},
		{ID: "2", Status: ptrto.Value(models.TodoStatusDone)},
		{ID: "3", Status: ptrto.Value(models.TodoStatusDone)},
	}
	openCount, doneCount := countTodosByStatus(todos)
	if openCount != 0 {
		t.Errorf("openCount: got %d, want 0", openCount)
	}
	if doneCount != 3 {
		t.Errorf("doneCount: got %d, want 3", doneCount)
	}
}

func TestCountTodosByStatusNilStatus(t *testing.T) {
	// A todo with nil status should not be counted as open or done.
	todos := []*models.Todo{
		{ID: "1"},
		{ID: "2", Status: ptrto.Value(models.TodoStatusOpen)},
	}
	openCount, doneCount := countTodosByStatus(todos)
	if openCount != 1 {
		t.Errorf("openCount: got %d, want 1", openCount)
	}
	if doneCount != 0 {
		t.Errorf("doneCount: got %d, want 0", doneCount)
	}
}
