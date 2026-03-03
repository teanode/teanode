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

type projectBatchResponse struct {
	Action     string `json:"action"`
	TotalCount int    `json:"totalCount"`
	OpenCount  int    `json:"openCount"`
	DoneCount  int    `json:"doneCount"`
	Results    []struct {
		Index   int          `json:"index"`
		Op      string       `json:"op"`
		Success bool         `json:"success"`
		Todo    *models.Todo `json:"todo,omitempty"`
		Error   string       `json:"error,omitempty"`
		TodoID  string       `json:"todoId,omitempty"`
	} `json:"results"`
	Summary *struct {
		Total     int `json:"total"`
		Succeeded int `json:"succeeded"`
		Failed    int `json:"failed"`
	} `json:"summary"`
}

func newProjectTodoTool(t *testing.T) tools.Tool {
	t.Helper()
	registry := tools.NewEmptyToolRegistry()
	registry.Register(projects.NewProjectTodoTool())
	return registry.Get("project_todo")
}

func TestProjectTodoBatchAdd(t *testing.T) {
	s := setupTodoToolStore(t)
	ctx := store.ContextWithStore(context.Background(), s)
	ctx = models.ContextWithUserSessionToken(ctx, &models.User{ID: "admin", Admin: ptrto.Value(true)}, nil, nil)
	projectId := createProject(t, ctx, s)
	todoTool := newProjectTodoTool(t)

	result, err := todoTool.Execute(ctx, `{"action":"batch","projectId":"`+projectId+`","items":[{"op":"add","title":"Implement auth","priority":"high","tags":["backend"]},{"op":"add","title":"Write docs"}]}`)
	if err != nil {
		t.Fatalf("batch add failed: %v", err)
	}

	var resp projectBatchResponse
	json.Unmarshal([]byte(result), &resp)
	if resp.Action != "batch" {
		t.Fatalf("action = %q, want batch", resp.Action)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("results count = %d, want 2", len(resp.Results))
	}
	for i, r := range resp.Results {
		if !r.Success {
			t.Fatalf("result[%d] failed: %s", i, r.Error)
		}
		if r.Todo == nil {
			t.Fatalf("result[%d] todo is nil", i)
		}
	}
	if resp.Results[0].Todo.GetTitle() != "Implement auth" {
		t.Fatalf("result[0] title = %q, want 'Implement auth'", resp.Results[0].Todo.GetTitle())
	}
	if resp.Results[0].Todo.GetPriority() != models.TodoPriorityHigh {
		t.Fatalf("result[0] priority = %q, want high", resp.Results[0].Todo.GetPriority())
	}
	if resp.Summary.Total != 2 || resp.Summary.Succeeded != 2 {
		t.Fatalf("summary = %+v", resp.Summary)
	}
	if resp.OpenCount != 2 {
		t.Fatalf("openCount = %d, want 2", resp.OpenCount)
	}
}

func TestProjectTodoBatchMixedOps(t *testing.T) {
	s := setupTodoToolStore(t)
	ctx := store.ContextWithStore(context.Background(), s)
	ctx = models.ContextWithUserSessionToken(ctx, &models.User{ID: "admin", Admin: ptrto.Value(true)}, nil, nil)
	projectId := createProject(t, ctx, s)
	todoTool := newProjectTodoTool(t)

	// Setup: add 3 items.
	setupResult, _ := todoTool.Execute(ctx, `{"action":"batch","projectId":"`+projectId+`","items":[{"op":"add","title":"To Complete"},{"op":"add","title":"To Delete"},{"op":"add","title":"To Update"}]}`)
	var setup projectBatchResponse
	json.Unmarshal([]byte(setupResult), &setup)

	completeId := setup.Results[0].Todo.ID
	deleteId := setup.Results[1].Todo.ID
	updateId := setup.Results[2].Todo.ID

	// Mixed batch.
	mixedArgs, _ := json.Marshal(map[string]interface{}{
		"action":    "batch",
		"projectId": projectId,
		"items": []map[string]interface{}{
			{"op": "add", "title": "New Item"},
			{"op": "complete", "todoId": completeId},
			{"op": "delete", "todoId": deleteId},
			{"op": "update", "todoId": updateId, "title": "Updated", "priority": "low"},
		},
	})

	result, err := todoTool.Execute(ctx, string(mixedArgs))
	if err != nil {
		t.Fatalf("mixed batch failed: %v", err)
	}

	var resp projectBatchResponse
	json.Unmarshal([]byte(result), &resp)
	if len(resp.Results) != 4 {
		t.Fatalf("results count = %d, want 4", len(resp.Results))
	}
	for i, r := range resp.Results {
		if !r.Success {
			t.Fatalf("result[%d] (%s) failed: %s", i, r.Op, r.Error)
		}
	}
	if resp.Results[1].Todo.GetStatus() != models.TodoStatusDone {
		t.Fatalf("complete result status = %q, want done", resp.Results[1].Todo.GetStatus())
	}
	if resp.Results[3].Todo.GetTitle() != "Updated" {
		t.Fatalf("update result title = %q, want Updated", resp.Results[3].Todo.GetTitle())
	}
	if resp.Summary.Succeeded != 4 {
		t.Fatalf("summary succeeded = %d, want 4", resp.Summary.Succeeded)
	}
}

func TestProjectTodoBatchPartialFailure(t *testing.T) {
	s := setupTodoToolStore(t)
	ctx := store.ContextWithStore(context.Background(), s)
	ctx = models.ContextWithUserSessionToken(ctx, &models.User{ID: "admin", Admin: ptrto.Value(true)}, nil, nil)
	projectId := createProject(t, ctx, s)
	todoTool := newProjectTodoTool(t)

	result, err := todoTool.Execute(ctx, `{"action":"batch","projectId":"`+projectId+`","items":[{"op":"add","title":"Good"},{"op":"complete","todoId":"nonexistent"}]}`)
	if err != nil {
		t.Fatalf("batch failed: %v", err)
	}

	var resp projectBatchResponse
	json.Unmarshal([]byte(result), &resp)
	if !resp.Results[0].Success {
		t.Fatal("result[0] should succeed")
	}
	if resp.Results[1].Success {
		t.Fatal("result[1] should fail")
	}
	if resp.Summary.Succeeded != 1 || resp.Summary.Failed != 1 {
		t.Fatalf("summary = %+v", resp.Summary)
	}
}

func TestProjectTodoPrune(t *testing.T) {
	s := setupTodoToolStore(t)
	ctx := store.ContextWithStore(context.Background(), s)
	ctx = models.ContextWithUserSessionToken(ctx, &models.User{ID: "admin", Admin: ptrto.Value(true)}, nil, nil)
	projectId := createProject(t, ctx, s)
	todoTool := newProjectTodoTool(t)

	// Add 2, complete 1.
	setupResult, _ := todoTool.Execute(ctx, `{"action":"batch","projectId":"`+projectId+`","items":[{"op":"add","title":"Open Item"},{"op":"add","title":"Done Item"}]}`)
	var setup projectBatchResponse
	json.Unmarshal([]byte(setupResult), &setup)
	doneId := setup.Results[1].Todo.ID
	todoTool.Execute(ctx, `{"action":"batch","projectId":"`+projectId+`","items":[{"op":"complete","todoId":"`+doneId+`"}]}`)

	// Prune.
	pruneResult, err := todoTool.Execute(ctx, `{"action":"prune","projectId":"`+projectId+`"}`)
	if err != nil {
		t.Fatalf("prune failed: %v", err)
	}
	var pruneResp struct {
		Action    string `json:"action"`
		Success   bool   `json:"success"`
		DoneCount int    `json:"doneCount"`
	}
	json.Unmarshal([]byte(pruneResult), &pruneResp)
	if !pruneResp.Success {
		t.Fatal("expected success=true")
	}
	if pruneResp.DoneCount != 1 {
		t.Fatalf("doneCount = %d, want 1", pruneResp.DoneCount)
	}

	// Verify only open item remains.
	listResult, _ := todoTool.Execute(ctx, `{"action":"list","projectId":"`+projectId+`"}`)
	var listed struct {
		Todos      []interface{} `json:"todos"`
		TotalCount int           `json:"totalCount"`
	}
	json.Unmarshal([]byte(listResult), &listed)
	if len(listed.Todos) != 1 {
		t.Fatalf("expected 1 todo after prune, got %d", len(listed.Todos))
	}
}

func TestProjectTodoNonAdminReadOnly(t *testing.T) {
	s := setupTodoToolStore(t)
	adminCtx := store.ContextWithStore(context.Background(), s)
	adminCtx = models.ContextWithUserSessionToken(adminCtx, &models.User{ID: "admin", Admin: ptrto.Value(true)}, nil, nil)
	projectId := createProject(t, adminCtx, s)
	todoTool := newProjectTodoTool(t)

	// Add a todo as admin.
	todoTool.Execute(adminCtx, `{"action":"batch","projectId":"`+projectId+`","items":[{"op":"add","title":"Admin Todo"}]}`)

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

	// Non-admin should not be able to batch.
	_, err = todoTool.Execute(userCtx, `{"action":"batch","projectId":"`+projectId+`","items":[{"op":"add","title":"User Todo"}]}`)
	if err == nil {
		t.Fatal("non-admin batch should fail")
	}

	// Non-admin should not be able to prune.
	_, err = todoTool.Execute(userCtx, `{"action":"prune","projectId":"`+projectId+`"}`)
	if err == nil {
		t.Fatal("non-admin prune should fail")
	}
}

func TestProjectTodoProjectNameLookup(t *testing.T) {
	s := setupTodoToolStore(t)
	ctx := store.ContextWithStore(context.Background(), s)
	ctx = models.ContextWithUserSessionToken(ctx, &models.User{ID: "admin", Admin: ptrto.Value(true)}, nil, nil)
	createProject(t, ctx, s) // Creates "TestProject"
	todoTool := newProjectTodoTool(t)

	// Use projectName instead of projectId.
	result, err := todoTool.Execute(ctx, `{"action":"batch","projectName":"testproject","items":[{"op":"add","title":"By Name"}]}`)
	if err != nil {
		t.Fatalf("batch by projectName failed: %v", err)
	}
	var resp projectBatchResponse
	json.Unmarshal([]byte(result), &resp)
	if !resp.Results[0].Success || resp.Results[0].Todo.GetTitle() != "By Name" {
		t.Fatalf("unexpected result: %+v", resp.Results[0])
	}
}
