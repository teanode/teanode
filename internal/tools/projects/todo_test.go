package projects_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/store/fsstore"
	"github.com/teanode/teanode/internal/tools"
	"github.com/teanode/teanode/internal/tools/projects"
	"github.com/teanode/teanode/internal/util/ptrto"
)

func setupTodoToolStore(t *testing.T) store.Store {
	t.Helper()
	s, err := fsstore.Open(fsstore.Options{DataDirectory: t.TempDir()})
	if err != nil {
		t.Fatalf("opening store: %v", err)
	}
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("migrating store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func createProject(t *testing.T, ctx context.Context, s store.Store) string {
	t.Helper()
	registry := tools.NewEmptyToolRegistry()
	registry.Register(projects.NewProjectsTool())
	tool := registry.Get("projects")
	result, err := tool.Execute(ctx, `{"action":"create","name":"TestProject","description":"test"}`)
	if err != nil {
		t.Fatalf("creating project: %v", err)
	}
	var created struct {
		Project struct {
			ID string `json:"id"`
		} `json:"project"`
	}
	json.Unmarshal([]byte(result), &created)
	return created.Project.ID
}

func TestProjectTodoAddAndList(t *testing.T) {
	s := setupTodoToolStore(t)
	ctx := store.ContextWithStore(context.Background(), s)
	ctx = models.ContextWithUserSessionToken(ctx, &models.User{ID: "admin", Admin: ptrto.Value(true)}, nil, nil)
	projectId := createProject(t, ctx, s)

	registry := tools.NewEmptyToolRegistry()
	registry.Register(projects.NewProjectTodoTool())
	todoTool := registry.Get("project_todo")

	// Add a todo.
	addResult, err := todoTool.Execute(ctx, `{"action":"add","projectId":"`+projectId+`","title":"Implement auth","priority":"high","tags":["backend"]}`)
	if err != nil {
		t.Fatalf("add failed: %v", err)
	}
	var added struct {
		Action string      `json:"action"`
		Todo   models.Todo `json:"todo"`
	}
	json.Unmarshal([]byte(addResult), &added)
	if added.Action != "add" {
		t.Fatalf("action = %q, want add", added.Action)
	}
	if added.Todo.GetTitle() != "Implement auth" {
		t.Fatalf("title = %q, want 'Implement auth'", added.Todo.GetTitle())
	}
	if added.Todo.GetPriority() != "high" {
		t.Fatalf("priority = %q, want high", added.Todo.GetPriority())
	}
	if added.Todo.GetStatus() != "open" {
		t.Fatalf("status = %q, want open", added.Todo.GetStatus())
	}

	// List todos.
	listResult, err := todoTool.Execute(ctx, `{"action":"list","projectId":"`+projectId+`"}`)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	var listed struct {
		Todos      []models.Todo `json:"todos"`
		TotalCount int           `json:"totalCount"`
		OpenCount  int           `json:"openCount"`
	}
	json.Unmarshal([]byte(listResult), &listed)
	if len(listed.Todos) != 1 {
		t.Fatalf("expected 1 todo, got %d", len(listed.Todos))
	}
	if listed.TotalCount != 1 || listed.OpenCount != 1 {
		t.Fatalf("counts: total=%d open=%d, want 1/1", listed.TotalCount, listed.OpenCount)
	}
}

func TestProjectTodoComplete(t *testing.T) {
	s := setupTodoToolStore(t)
	ctx := store.ContextWithStore(context.Background(), s)
	ctx = models.ContextWithUserSessionToken(ctx, &models.User{ID: "admin", Admin: ptrto.Value(true)}, nil, nil)
	projectId := createProject(t, ctx, s)

	registry := tools.NewEmptyToolRegistry()
	registry.Register(projects.NewProjectTodoTool())
	todoTool := registry.Get("project_todo")

	// Add.
	addResult, _ := todoTool.Execute(ctx, `{"action":"add","projectId":"`+projectId+`","title":"To Complete"}`)
	var added struct {
		Todo struct {
			ID string `json:"id"`
		} `json:"todo"`
	}
	json.Unmarshal([]byte(addResult), &added)

	// Complete.
	completeResult, err := todoTool.Execute(ctx, `{"action":"complete","projectId":"`+projectId+`","todoId":"`+added.Todo.ID+`"}`)
	if err != nil {
		t.Fatalf("complete failed: %v", err)
	}
	var completed struct {
		Todo models.Todo `json:"todo"`
	}
	json.Unmarshal([]byte(completeResult), &completed)
	if completed.Todo.GetStatus() != "done" {
		t.Fatalf("status = %q, want done", completed.Todo.GetStatus())
	}
	if completed.Todo.CompletedAt == nil {
		t.Fatal("completedAt should be set")
	}
}

func TestProjectTodoReopen(t *testing.T) {
	s := setupTodoToolStore(t)
	ctx := store.ContextWithStore(context.Background(), s)
	ctx = models.ContextWithUserSessionToken(ctx, &models.User{ID: "admin", Admin: ptrto.Value(true)}, nil, nil)
	projectId := createProject(t, ctx, s)

	registry := tools.NewEmptyToolRegistry()
	registry.Register(projects.NewProjectTodoTool())
	todoTool := registry.Get("project_todo")

	// Add, complete, reopen.
	addResult, _ := todoTool.Execute(ctx, `{"action":"add","projectId":"`+projectId+`","title":"To Reopen"}`)
	var added struct {
		Todo struct{ ID string `json:"id"` } `json:"todo"`
	}
	json.Unmarshal([]byte(addResult), &added)
	todoTool.Execute(ctx, `{"action":"complete","projectId":"`+projectId+`","todoId":"`+added.Todo.ID+`"}`)

	reopenResult, err := todoTool.Execute(ctx, `{"action":"reopen","projectId":"`+projectId+`","todoId":"`+added.Todo.ID+`"}`)
	if err != nil {
		t.Fatalf("reopen failed: %v", err)
	}
	var reopened struct {
		Todo models.Todo `json:"todo"`
	}
	json.Unmarshal([]byte(reopenResult), &reopened)
	if reopened.Todo.GetStatus() != "open" {
		t.Fatalf("status = %q, want open", reopened.Todo.GetStatus())
	}
	if reopened.Todo.CompletedAt != nil {
		t.Fatal("completedAt should be nil after reopen")
	}
}

func TestProjectTodoDelete(t *testing.T) {
	s := setupTodoToolStore(t)
	ctx := store.ContextWithStore(context.Background(), s)
	ctx = models.ContextWithUserSessionToken(ctx, &models.User{ID: "admin", Admin: ptrto.Value(true)}, nil, nil)
	projectId := createProject(t, ctx, s)

	registry := tools.NewEmptyToolRegistry()
	registry.Register(projects.NewProjectTodoTool())
	todoTool := registry.Get("project_todo")

	// Add, delete, list.
	addResult, _ := todoTool.Execute(ctx, `{"action":"add","projectId":"`+projectId+`","title":"To Delete"}`)
	var added struct {
		Todo struct{ ID string `json:"id"` } `json:"todo"`
	}
	json.Unmarshal([]byte(addResult), &added)

	deleteResult, err := todoTool.Execute(ctx, `{"action":"delete","projectId":"`+projectId+`","todoId":"`+added.Todo.ID+`"}`)
	if err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	var deleted struct {
		Success bool `json:"success"`
	}
	json.Unmarshal([]byte(deleteResult), &deleted)
	if !deleted.Success {
		t.Fatal("expected success=true")
	}

	// Verify list is empty.
	listResult, _ := todoTool.Execute(ctx, `{"action":"list","projectId":"`+projectId+`"}`)
	var listed struct {
		Todos []interface{} `json:"todos"`
	}
	json.Unmarshal([]byte(listResult), &listed)
	if len(listed.Todos) != 0 {
		t.Fatalf("expected 0 todos after delete, got %d", len(listed.Todos))
	}
}

func TestProjectTodoClearDone(t *testing.T) {
	s := setupTodoToolStore(t)
	ctx := store.ContextWithStore(context.Background(), s)
	ctx = models.ContextWithUserSessionToken(ctx, &models.User{ID: "admin", Admin: ptrto.Value(true)}, nil, nil)
	projectId := createProject(t, ctx, s)

	registry := tools.NewEmptyToolRegistry()
	registry.Register(projects.NewProjectTodoTool())
	todoTool := registry.Get("project_todo")

	// Add two, complete one.
	todoTool.Execute(ctx, `{"action":"add","projectId":"`+projectId+`","title":"Open Item"}`)
	addResult, _ := todoTool.Execute(ctx, `{"action":"add","projectId":"`+projectId+`","title":"Done Item"}`)
	var added struct{ Todo struct{ ID string `json:"id"` } `json:"todo"` }
	json.Unmarshal([]byte(addResult), &added)
	todoTool.Execute(ctx, `{"action":"complete","projectId":"`+projectId+`","todoId":"`+added.Todo.ID+`"}`)

	// clear_done should remove only the done item.
	clearResult, err := todoTool.Execute(ctx, `{"action":"clear_done","projectId":"`+projectId+`"}`)
	if err != nil {
		t.Fatalf("clear_done failed: %v", err)
	}
	var cleared struct {
		Success   bool `json:"success"`
		DoneCount int  `json:"doneCount"`
	}
	json.Unmarshal([]byte(clearResult), &cleared)
	if !cleared.Success {
		t.Fatal("expected success=true")
	}
	if cleared.DoneCount != 1 {
		t.Fatalf("doneCount = %d, want 1", cleared.DoneCount)
	}

	// Verify only the open item remains.
	listResult, _ := todoTool.Execute(ctx, `{"action":"list","projectId":"`+projectId+`"}`)
	var listed struct {
		Todos      []interface{} `json:"todos"`
		TotalCount int           `json:"totalCount"`
	}
	json.Unmarshal([]byte(listResult), &listed)
	if len(listed.Todos) != 1 {
		t.Fatalf("expected 1 todo after clear_done, got %d", len(listed.Todos))
	}
}

func TestProjectTodoReset(t *testing.T) {
	s := setupTodoToolStore(t)
	ctx := store.ContextWithStore(context.Background(), s)
	ctx = models.ContextWithUserSessionToken(ctx, &models.User{ID: "admin", Admin: ptrto.Value(true)}, nil, nil)
	projectId := createProject(t, ctx, s)

	registry := tools.NewEmptyToolRegistry()
	registry.Register(projects.NewProjectTodoTool())
	todoTool := registry.Get("project_todo")

	// Add two, complete one.
	todoTool.Execute(ctx, `{"action":"add","projectId":"`+projectId+`","title":"Open Item"}`)
	addResult, _ := todoTool.Execute(ctx, `{"action":"add","projectId":"`+projectId+`","title":"Done Item"}`)
	var added struct{ Todo struct{ ID string `json:"id"` } `json:"todo"` }
	json.Unmarshal([]byte(addResult), &added)
	todoTool.Execute(ctx, `{"action":"complete","projectId":"`+projectId+`","todoId":"`+added.Todo.ID+`"}`)

	// reset should remove all.
	resetResult, err := todoTool.Execute(ctx, `{"action":"reset","projectId":"`+projectId+`"}`)
	if err != nil {
		t.Fatalf("reset failed: %v", err)
	}
	var resetResp struct {
		Success    bool `json:"success"`
		TotalCount int  `json:"totalCount"`
	}
	json.Unmarshal([]byte(resetResult), &resetResp)
	if !resetResp.Success {
		t.Fatal("expected success=true")
	}
	if resetResp.TotalCount != 2 {
		t.Fatalf("totalCount = %d, want 2", resetResp.TotalCount)
	}

	// Verify empty.
	listResult, _ := todoTool.Execute(ctx, `{"action":"list","projectId":"`+projectId+`"}`)
	var listed struct {
		Todos []interface{} `json:"todos"`
	}
	json.Unmarshal([]byte(listResult), &listed)
	if len(listed.Todos) != 0 {
		t.Fatalf("expected 0 todos after reset, got %d", len(listed.Todos))
	}
}

func TestProjectTodoNonAdminReadOnly(t *testing.T) {
	s := setupTodoToolStore(t)
	adminCtx := store.ContextWithStore(context.Background(), s)
	adminCtx = models.ContextWithUserSessionToken(adminCtx, &models.User{ID: "admin", Admin: ptrto.Value(true)}, nil, nil)
	projectId := createProject(t, adminCtx, s)

	// Add a todo as admin.
	registry := tools.NewEmptyToolRegistry()
	registry.Register(projects.NewProjectTodoTool())
	todoTool := registry.Get("project_todo")
	todoTool.Execute(adminCtx, `{"action":"add","projectId":"`+projectId+`","title":"Admin Todo"}`)

	// Non-admin should be able to list.
	userCtx := store.ContextWithStore(context.Background(), s)
	userCtx = models.ContextWithUserSessionToken(userCtx, &models.User{ID: "user", Admin: ptrto.Value(false)}, nil, nil)

	listResult, err := todoTool.Execute(userCtx, `{"action":"list","projectId":"`+projectId+`"}`)
	if err != nil {
		t.Fatalf("non-admin list failed: %v", err)
	}
	var listed struct {
		Todos []interface{} `json:"todos"`
	}
	json.Unmarshal([]byte(listResult), &listed)
	if len(listed.Todos) != 1 {
		t.Fatalf("non-admin should see 1 todo, got %d", len(listed.Todos))
	}

	// Non-admin should not be able to add.
	_, err = todoTool.Execute(userCtx, `{"action":"add","projectId":"`+projectId+`","title":"User Todo"}`)
	if err == nil {
		t.Fatal("non-admin add should fail")
	}

	// Non-admin should not be able to delete.
	_, err = todoTool.Execute(userCtx, `{"action":"delete","projectId":"`+projectId+`","todoId":"whatever"}`)
	if err == nil {
		t.Fatal("non-admin delete should fail")
	}
}

func TestProjectTodoAddMissingTitle(t *testing.T) {
	s := setupTodoToolStore(t)
	ctx := store.ContextWithStore(context.Background(), s)
	ctx = models.ContextWithUserSessionToken(ctx, &models.User{ID: "admin", Admin: ptrto.Value(true)}, nil, nil)
	projectId := createProject(t, ctx, s)

	registry := tools.NewEmptyToolRegistry()
	registry.Register(projects.NewProjectTodoTool())
	todoTool := registry.Get("project_todo")

	_, err := todoTool.Execute(ctx, `{"action":"add","projectId":"`+projectId+`"}`)
	if err == nil {
		t.Fatal("add without title should fail")
	}
}

func TestProjectTodoProjectNameLookup(t *testing.T) {
	s := setupTodoToolStore(t)
	ctx := store.ContextWithStore(context.Background(), s)
	ctx = models.ContextWithUserSessionToken(ctx, &models.User{ID: "admin", Admin: ptrto.Value(true)}, nil, nil)
	createProject(t, ctx, s) // Creates "TestProject"

	registry := tools.NewEmptyToolRegistry()
	registry.Register(projects.NewProjectTodoTool())
	todoTool := registry.Get("project_todo")

	// Use projectName instead of projectId.
	addResult, err := todoTool.Execute(ctx, `{"action":"add","projectName":"testproject","title":"By Name"}`)
	if err != nil {
		t.Fatalf("add by projectName failed: %v", err)
	}
	var added struct {
		Todo models.Todo `json:"todo"`
	}
	json.Unmarshal([]byte(addResult), &added)
	if added.Todo.GetTitle() != "By Name" {
		t.Fatalf("title = %q, want 'By Name'", added.Todo.GetTitle())
	}
}

func TestProjectTodoListFilters(t *testing.T) {
	s := setupTodoToolStore(t)
	ctx := store.ContextWithStore(context.Background(), s)
	ctx = models.ContextWithUserSessionToken(ctx, &models.User{ID: "admin", Admin: ptrto.Value(true)}, nil, nil)
	projectId := createProject(t, ctx, s)

	registry := tools.NewEmptyToolRegistry()
	registry.Register(projects.NewProjectTodoTool())
	todoTool := registry.Get("project_todo")

	// Add multiple todos.
	addResult, _ := todoTool.Execute(ctx, `{"action":"add","projectId":"`+projectId+`","title":"High Open","priority":"high","tags":["backend"]}`)
	var added struct{ Todo struct{ ID string `json:"id"` } `json:"todo"` }
	json.Unmarshal([]byte(addResult), &added)
	todoTool.Execute(ctx, `{"action":"add","projectId":"`+projectId+`","title":"Low Open","priority":"low","tags":["frontend"]}`)
	todoTool.Execute(ctx, `{"action":"complete","projectId":"`+projectId+`","todoId":"`+added.Todo.ID+`"}`)

	// Filter by status.
	openResult, _ := todoTool.Execute(ctx, `{"action":"list","projectId":"`+projectId+`","status":"open"}`)
	var openList struct{ Todos []interface{} `json:"todos"` }
	json.Unmarshal([]byte(openResult), &openList)
	if len(openList.Todos) != 1 {
		t.Fatalf("open filter: expected 1, got %d", len(openList.Todos))
	}

	// Filter by priority.
	highResult, _ := todoTool.Execute(ctx, `{"action":"list","projectId":"`+projectId+`","priority":"high"}`)
	var highList struct{ Todos []interface{} `json:"todos"` }
	json.Unmarshal([]byte(highResult), &highList)
	if len(highList.Todos) != 1 {
		t.Fatalf("high priority filter: expected 1, got %d", len(highList.Todos))
	}

	// Filter by tag.
	tagResult, _ := todoTool.Execute(ctx, `{"action":"list","projectId":"`+projectId+`","tag":"frontend"}`)
	var tagList struct{ Todos []interface{} `json:"todos"` }
	json.Unmarshal([]byte(tagResult), &tagList)
	if len(tagList.Todos) != 1 {
		t.Fatalf("tag filter: expected 1, got %d", len(tagList.Todos))
	}
}
