package fsstore_test

import (
	"context"
	"testing"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/store/fsstore"
	"github.com/teanode/teanode/internal/util/ptrto"
)

func setupTodoTestStore(t *testing.T) store.Store {
	t.Helper()
	openedStore, err := fsstore.Open(fsstore.Options{DataDirectory: t.TempDir()})
	if err != nil {
		t.Fatalf("opening store: %v", err)
	}
	if err := openedStore.Migrate(context.Background()); err != nil {
		t.Fatalf("migrating store: %v", err)
	}
	t.Cleanup(func() { _ = openedStore.Close() })
	return openedStore
}

func createTestProject(t *testing.T, ctx context.Context, s store.Store) *models.Project {
	t.Helper()
	var project *models.Project
	if err := s.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		p, err := tx.CreateProject(ctx, &models.Project{
			Name:        ptrto.Value("Test Project"),
			Description: ptrto.Value("A test project"),
		}, nil, nil)
		if err != nil {
			return err
		}
		project = p
		return nil
	}); err != nil {
		t.Fatalf("creating project: %v", err)
	}
	return project
}

func createTestConversation(t *testing.T, ctx context.Context, s store.Store, userId, agentId string) *models.Conversation {
	t.Helper()
	// Ensure user and agent exist.
	_ = s.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		tx.CreateUser(ctx, &models.User{ID: userId, Username: ptrto.Value(userId), Admin: ptrto.Value(true)}, nil, nil)
		tx.CreateAgent(ctx, &models.Agent{ID: agentId, Name: ptrto.Value("Test Agent")}, nil, nil)
		return nil
	})

	var conversation *models.Conversation
	if err := s.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		c, err := tx.CreateConversation(ctx, &models.Conversation{
			UserID:  ptrto.Value(userId),
			AgentID: ptrto.Value(agentId),
		}, nil)
		if err != nil {
			return err
		}
		conversation = c
		return nil
	}); err != nil {
		t.Fatalf("creating conversation: %v", err)
	}
	return conversation
}

func TestTodoCreateAndGet(t *testing.T) {
	s := setupTodoTestStore(t)
	ctx := context.Background()
	project := createTestProject(t, ctx, s)

	var created *models.Todo
	if err := s.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		todo, err := tx.CreateTodo(ctx, &models.Todo{
			ProjectID:   ptrto.Value(project.ID),
			Title:       ptrto.Value("Test Todo"),
			Description: ptrto.Value("A test description"),
			Priority:    ptrto.Value(models.TodoPriorityHigh),
			Tags:        &[]string{"backend", "test"},
		}, nil)
		if err != nil {
			return err
		}
		created = todo
		return nil
	}); err != nil {
		t.Fatalf("creating todo: %v", err)
	}

	if created.ID == "" {
		t.Fatal("todo ID should not be empty")
	}
	if created.GetTitle() != "Test Todo" {
		t.Fatalf("title = %q, want %q", created.GetTitle(), "Test Todo")
	}
	if created.GetStatus() != models.TodoStatusOpen {
		t.Fatalf("status = %q, want %q", created.GetStatus(), models.TodoStatusOpen)
	}
	if created.GetPriority() != models.TodoPriorityHigh {
		t.Fatalf("priority = %q, want %q", created.GetPriority(), models.TodoPriorityHigh)
	}
	if created.CreatedAt == nil {
		t.Fatal("createdAt should be set")
	}

	// Get by ID.
	var fetched *models.Todo
	if err := s.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		todo, err := tx.GetTodo(ctx, created.ID, nil)
		if err != nil {
			return err
		}
		fetched = todo
		return nil
	}); err != nil {
		t.Fatalf("getting todo: %v", err)
	}

	if fetched.GetTitle() != "Test Todo" {
		t.Fatalf("fetched title = %q, want %q", fetched.GetTitle(), "Test Todo")
	}
	if fetched.GetPriority() != models.TodoPriorityHigh {
		t.Fatalf("fetched priority = %q, want %q", fetched.GetPriority(), models.TodoPriorityHigh)
	}
}

func TestTodoListByProjectId(t *testing.T) {
	s := setupTodoTestStore(t)
	ctx := context.Background()
	project1 := createTestProject(t, ctx, s)
	project2 := createTestProject(t, ctx, s)

	// Create todos in both projects.
	_ = s.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		tx.CreateTodo(ctx, &models.Todo{ProjectID: ptrto.Value(project1.ID), Title: ptrto.Value("P1 Todo 1")}, nil)
		tx.CreateTodo(ctx, &models.Todo{ProjectID: ptrto.Value(project1.ID), Title: ptrto.Value("P1 Todo 2")}, nil)
		tx.CreateTodo(ctx, &models.Todo{ProjectID: ptrto.Value(project2.ID), Title: ptrto.Value("P2 Todo 1")}, nil)
		return nil
	})

	// List project 1 todos.
	var todos []*models.Todo
	if err := s.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		listed, err := tx.ListTodos(ctx, store.TodoListOptions{ProjectID: &project1.ID}, nil)
		if err != nil {
			return err
		}
		todos = listed
		return nil
	}); err != nil {
		t.Fatalf("listing todos: %v", err)
	}

	if len(todos) != 2 {
		t.Fatalf("expected 2 todos for project1, got %d", len(todos))
	}

	// List project 2 todos.
	if err := s.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		listed, err := tx.ListTodos(ctx, store.TodoListOptions{ProjectID: &project2.ID}, nil)
		if err != nil {
			return err
		}
		todos = listed
		return nil
	}); err != nil {
		t.Fatalf("listing todos: %v", err)
	}

	if len(todos) != 1 {
		t.Fatalf("expected 1 todo for project2, got %d", len(todos))
	}
}

func TestTodoListByConversationId(t *testing.T) {
	s := setupTodoTestStore(t)
	ctx := context.Background()
	conv1 := createTestConversation(t, ctx, s, "user1", "agent1")
	conv2 := createTestConversation(t, ctx, s, "user1", "agent1")

	_ = s.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		tx.CreateTodo(ctx, &models.Todo{ConversationID: ptrto.Value(conv1.ID), Title: ptrto.Value("C1 Todo")}, nil)
		tx.CreateTodo(ctx, &models.Todo{ConversationID: ptrto.Value(conv2.ID), Title: ptrto.Value("C2 Todo 1")}, nil)
		tx.CreateTodo(ctx, &models.Todo{ConversationID: ptrto.Value(conv2.ID), Title: ptrto.Value("C2 Todo 2")}, nil)
		return nil
	})

	var todos []*models.Todo
	if err := s.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		listed, err := tx.ListTodos(ctx, store.TodoListOptions{ConversationID: &conv1.ID}, nil)
		if err != nil {
			return err
		}
		todos = listed
		return nil
	}); err != nil {
		t.Fatalf("listing todos: %v", err)
	}

	if len(todos) != 1 {
		t.Fatalf("expected 1 todo for conv1, got %d", len(todos))
	}
}

func TestTodoModify(t *testing.T) {
	s := setupTodoTestStore(t)
	ctx := context.Background()
	project := createTestProject(t, ctx, s)

	var created *models.Todo
	_ = s.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		todo, _ := tx.CreateTodo(ctx, &models.Todo{
			ProjectID: ptrto.Value(project.ID),
			Title:     ptrto.Value("Original Title"),
			Priority:  ptrto.Value(models.TodoPriorityLow),
		}, nil)
		created = todo
		return nil
	})

	var modified *models.Todo
	if err := s.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		todo, err := tx.ModifyTodo(ctx, created.ID, func(t *models.Todo) error {
			t.Title = ptrto.Value("Updated Title")
			t.Priority = ptrto.Value(models.TodoPriorityHigh)
			t.Status = ptrto.Value(models.TodoStatusDone)
			return nil
		}, nil)
		if err != nil {
			return err
		}
		modified = todo
		return nil
	}); err != nil {
		t.Fatalf("modifying todo: %v", err)
	}

	if modified.GetTitle() != "Updated Title" {
		t.Fatalf("title = %q, want %q", modified.GetTitle(), "Updated Title")
	}
	if modified.GetPriority() != models.TodoPriorityHigh {
		t.Fatalf("priority = %q, want %q", modified.GetPriority(), models.TodoPriorityHigh)
	}
	if modified.GetStatus() != models.TodoStatusDone {
		t.Fatalf("status = %q, want %q", modified.GetStatus(), models.TodoStatusDone)
	}
}

func TestTodoDelete(t *testing.T) {
	s := setupTodoTestStore(t)
	ctx := context.Background()
	project := createTestProject(t, ctx, s)

	var created *models.Todo
	_ = s.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		todo, _ := tx.CreateTodo(ctx, &models.Todo{
			ProjectID: ptrto.Value(project.ID),
			Title:     ptrto.Value("To Delete"),
		}, nil)
		created = todo
		return nil
	})

	if err := s.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		return tx.DeleteTodo(ctx, created.ID, nil)
	}); err != nil {
		t.Fatalf("deleting todo: %v", err)
	}

	// Verify it's gone.
	if err := s.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		_, err := tx.GetTodo(ctx, created.ID, nil)
		return err
	}); err == nil {
		t.Fatal("expected error getting deleted todo")
	}
}

func TestTodoDeleteNotFound(t *testing.T) {
	s := setupTodoTestStore(t)
	ctx := context.Background()

	err := s.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		return tx.DeleteTodo(ctx, "nonexistent", nil)
	})
	if err == nil {
		t.Fatal("expected error deleting non-existent todo")
	}
}

func TestTodoListEmpty(t *testing.T) {
	s := setupTodoTestStore(t)
	ctx := context.Background()
	project := createTestProject(t, ctx, s)

	var todos []*models.Todo
	if err := s.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		listed, err := tx.ListTodos(ctx, store.TodoListOptions{ProjectID: &project.ID}, nil)
		if err != nil {
			return err
		}
		todos = listed
		return nil
	}); err != nil {
		t.Fatalf("listing todos: %v", err)
	}

	if todos == nil {
		t.Fatal("expected empty slice, got nil")
	}
	if len(todos) != 0 {
		t.Fatalf("expected 0 todos, got %d", len(todos))
	}
}

func TestTodoDefaultValues(t *testing.T) {
	s := setupTodoTestStore(t)
	ctx := context.Background()
	project := createTestProject(t, ctx, s)

	var created *models.Todo
	_ = s.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		todo, _ := tx.CreateTodo(ctx, &models.Todo{
			ProjectID: ptrto.Value(project.ID),
			Title:     ptrto.Value("Minimal Todo"),
		}, nil)
		created = todo
		return nil
	})

	if created.GetStatus() != models.TodoStatusOpen {
		t.Fatalf("default status = %q, want %q", created.GetStatus(), models.TodoStatusOpen)
	}
	if created.GetPriority() != models.TodoPriorityMedium {
		t.Fatalf("default priority = %q, want %q", created.GetPriority(), models.TodoPriorityMedium)
	}
	tags := created.GetTags()
	if tags == nil || len(tags) != 0 {
		t.Fatalf("default tags should be empty slice, got %v", tags)
	}
}

func TestTodoCascadeOnProjectDelete(t *testing.T) {
	s := setupTodoTestStore(t)
	ctx := context.Background()
	project := createTestProject(t, ctx, s)

	// Create a todo.
	var created *models.Todo
	_ = s.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		todo, _ := tx.CreateTodo(ctx, &models.Todo{
			ProjectID: ptrto.Value(project.ID),
			Title:     ptrto.Value("Cascade Test"),
		}, nil)
		created = todo
		return nil
	})

	// Delete the project.
	_ = s.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		return tx.DeleteProject(ctx, project.ID, nil)
	})

	// The todo should be gone (directory moved to trash).
	err := s.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		_, err := tx.GetTodo(ctx, created.ID, nil)
		return err
	})
	if err == nil {
		t.Fatal("expected error: todo should be gone after project delete")
	}
}
