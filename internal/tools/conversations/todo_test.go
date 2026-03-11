package conversations_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/runners"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/store/fsstore"
	"github.com/teanode/teanode/internal/tools"
	"github.com/teanode/teanode/internal/tools/conversations"
	"github.com/teanode/teanode/internal/util/ptrto"
)

func mustUnmarshalConversationJSON(t testing.TB, result string, target any) {
	t.Helper()
	if err := json.Unmarshal([]byte(result), target); err != nil {
		t.Fatalf("unmarshal JSON: %v", err)
	}
}

func setupConvTodoStore(t *testing.T) store.Store {
	t.Helper()
	s, err := fsstore.Open(fsstore.Options{DataDirectory: t.TempDir()})
	if err != nil {
		t.Fatalf("opening store: %v", err)
	}
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("migrating: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func createConv(t *testing.T, ctx context.Context, s store.Store, userId, agentId string) string {
	t.Helper()
	if err := s.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		if _, err := tx.CreateUser(ctx, &models.User{ID: userId, Username: ptrto.Value(userId), Admin: ptrto.Value(true)}, nil, nil); err != nil {
			return err
		}
		if _, err := tx.CreateAgent(ctx, &models.Agent{ID: agentId, Name: ptrto.Value("Agent")}, nil, nil); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatalf("creating conversation dependencies: %v", err)
	}
	var convId string
	_ = s.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		conv, err := tx.CreateConversation(ctx, &models.Conversation{
			UserID:  ptrto.Value(userId),
			AgentID: ptrto.Value(agentId),
		}, nil)
		if err != nil {
			t.Fatalf("creating conversation: %v", err)
		}
		convId = conv.ID
		return nil
	})
	return convId
}

func buildConvTodoCtx(s store.Store, userId string, isAdmin bool, conversationId, agentId string) context.Context {
	ctx := store.ContextWithStore(context.Background(), s)
	ctx = models.ContextWithUserSessionToken(ctx, &models.User{ID: userId, Admin: ptrto.Value(isAdmin)}, nil, nil)
	ctx = runners.ContextWithRunner(ctx, &runners.Runner{
		ConversationID: conversationId,
		AgentID:        agentId,
	})
	return ctx
}

type batchResponse struct {
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

func newConvTodoTool(t *testing.T) tools.Tool {
	t.Helper()
	registry := tools.NewEmptyToolRegistry()
	registry.Register(conversations.NewConversationTodoTool())
	return registry.Get("conversation_todo")
}

func TestConvTodoBatchAdd(t *testing.T) {
	s := setupConvTodoStore(t)
	ctx := store.ContextWithStore(context.Background(), s)
	convId := createConv(t, ctx, s, "user1", "agent1")
	todoCtx := buildConvTodoCtx(s, "user1", true, convId, "agent1")
	todoTool := newConvTodoTool(t)

	result, err := todoTool.Execute(todoCtx, `{"action":"batch","items":[{"op":"add","title":"Task A","priority":"high"},{"op":"add","title":"Task B","tags":["backend"]}]}`)
	if err != nil {
		t.Fatalf("batch add failed: %v", err)
	}

	var resp batchResponse
	mustUnmarshalConversationJSON(t, result, &resp)
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
	if resp.Results[0].Todo.GetTitle() != "Task A" {
		t.Fatalf("result[0] title = %q, want Task A", resp.Results[0].Todo.GetTitle())
	}
	if resp.Results[0].Todo.GetPriority() != models.TodoPriorityHigh {
		t.Fatalf("result[0] priority = %q, want high", resp.Results[0].Todo.GetPriority())
	}
	if resp.Results[1].Todo.GetTitle() != "Task B" {
		t.Fatalf("result[1] title = %q, want Task B", resp.Results[1].Todo.GetTitle())
	}
	if resp.Summary.Total != 2 || resp.Summary.Succeeded != 2 || resp.Summary.Failed != 0 {
		t.Fatalf("summary = %+v, want 2/2/0", resp.Summary)
	}
	if resp.OpenCount != 2 {
		t.Fatalf("openCount = %d, want 2", resp.OpenCount)
	}
}

func TestConvTodoBatchMixedOps(t *testing.T) {
	s := setupConvTodoStore(t)
	ctx := store.ContextWithStore(context.Background(), s)
	convId := createConv(t, ctx, s, "user1", "agent1")
	todoCtx := buildConvTodoCtx(s, "user1", true, convId, "agent1")
	todoTool := newConvTodoTool(t)

	// First, add two items.
	addResult, err := todoTool.Execute(todoCtx, `{"action":"batch","items":[{"op":"add","title":"To Complete"},{"op":"add","title":"To Delete"},{"op":"add","title":"To Update"}]}`)
	if err != nil {
		t.Fatalf("setup batch failed: %v", err)
	}
	var setup batchResponse
	mustUnmarshalConversationJSON(t, addResult, &setup)

	completeId := setup.Results[0].Todo.ID
	deleteId := setup.Results[1].Todo.ID
	updateId := setup.Results[2].Todo.ID

	// Now batch: add + complete + delete + update.
	mixedArgs, _ := json.Marshal(map[string]interface{}{
		"action": "batch",
		"items": []map[string]interface{}{
			{"op": "add", "title": "New Item"},
			{"op": "complete", "todoId": completeId},
			{"op": "delete", "todoId": deleteId},
			{"op": "update", "todoId": updateId, "title": "Updated Title", "priority": "high"},
		},
	})

	result, err := todoTool.Execute(todoCtx, string(mixedArgs))
	if err != nil {
		t.Fatalf("mixed batch failed: %v", err)
	}

	var resp batchResponse
	mustUnmarshalConversationJSON(t, result, &resp)
	if len(resp.Results) != 4 {
		t.Fatalf("results count = %d, want 4", len(resp.Results))
	}
	for i, r := range resp.Results {
		if !r.Success {
			t.Fatalf("result[%d] failed: %s", i, r.Error)
		}
	}
	// Verify ops
	if resp.Results[0].Op != "add" || resp.Results[0].Todo.GetTitle() != "New Item" {
		t.Fatalf("result[0] unexpected: op=%s", resp.Results[0].Op)
	}
	if resp.Results[1].Op != "complete" || resp.Results[1].Todo.GetStatus() != models.TodoStatusDone {
		t.Fatalf("result[1] unexpected: status=%s", resp.Results[1].Todo.GetStatus())
	}
	if resp.Results[2].Op != "delete" || resp.Results[2].TodoID != deleteId {
		t.Fatalf("result[2] unexpected: todoId=%s", resp.Results[2].TodoID)
	}
	if resp.Results[3].Op != "update" || resp.Results[3].Todo.GetTitle() != "Updated Title" {
		t.Fatalf("result[3] unexpected: title=%s", resp.Results[3].Todo.GetTitle())
	}
	if resp.Summary.Total != 4 || resp.Summary.Succeeded != 4 {
		t.Fatalf("summary = %+v", resp.Summary)
	}
}

func TestConvTodoBatchPartialFailure(t *testing.T) {
	s := setupConvTodoStore(t)
	ctx := store.ContextWithStore(context.Background(), s)
	convId := createConv(t, ctx, s, "user1", "agent1")
	todoCtx := buildConvTodoCtx(s, "user1", true, convId, "agent1")
	todoTool := newConvTodoTool(t)

	// Batch with one good add and one bad update (nonexistent todoId).
	result, err := todoTool.Execute(todoCtx, `{"action":"batch","items":[{"op":"add","title":"Good"},{"op":"update","todoId":"nonexistent","title":"Bad"}]}`)
	if err != nil {
		t.Fatalf("batch failed: %v", err)
	}

	var resp batchResponse
	mustUnmarshalConversationJSON(t, result, &resp)
	if len(resp.Results) != 2 {
		t.Fatalf("results count = %d, want 2", len(resp.Results))
	}
	if !resp.Results[0].Success {
		t.Fatalf("result[0] should succeed")
	}
	if resp.Results[1].Success {
		t.Fatalf("result[1] should fail")
	}
	if resp.Results[1].Error == "" {
		t.Fatalf("result[1] error should be non-empty")
	}
	if resp.Summary.Succeeded != 1 || resp.Summary.Failed != 1 {
		t.Fatalf("summary = %+v, want 1/1", resp.Summary)
	}
}

func TestConvTodoBatchValidation(t *testing.T) {
	s := setupConvTodoStore(t)
	ctx := store.ContextWithStore(context.Background(), s)
	convId := createConv(t, ctx, s, "user1", "agent1")
	todoCtx := buildConvTodoCtx(s, "user1", true, convId, "agent1")
	todoTool := newConvTodoTool(t)

	// Empty items.
	_, err := todoTool.Execute(todoCtx, `{"action":"batch","items":[]}`)
	if err == nil {
		t.Fatal("empty items should fail")
	}

	// Missing title on add → per-item error.
	result, err := todoTool.Execute(todoCtx, `{"action":"batch","items":[{"op":"add"}]}`)
	if err != nil {
		t.Fatalf("batch failed: %v", err)
	}
	var resp batchResponse
	mustUnmarshalConversationJSON(t, result, &resp)
	if resp.Results[0].Success {
		t.Fatal("add without title should fail")
	}

	// Missing todoId on update → per-item error.
	result2, err := todoTool.Execute(todoCtx, `{"action":"batch","items":[{"op":"update","title":"no id"}]}`)
	if err != nil {
		t.Fatalf("batch failed: %v", err)
	}
	var resp2 batchResponse
	mustUnmarshalConversationJSON(t, result2, &resp2)
	if resp2.Results[0].Success {
		t.Fatal("update without todoId should fail")
	}
}

func TestConvTodoBatchSingleItem(t *testing.T) {
	s := setupConvTodoStore(t)
	ctx := store.ContextWithStore(context.Background(), s)
	convId := createConv(t, ctx, s, "user1", "agent1")
	todoCtx := buildConvTodoCtx(s, "user1", true, convId, "agent1")
	todoTool := newConvTodoTool(t)

	result, err := todoTool.Execute(todoCtx, `{"action":"batch","items":[{"op":"add","title":"Single"}]}`)
	if err != nil {
		t.Fatalf("batch failed: %v", err)
	}

	var resp batchResponse
	mustUnmarshalConversationJSON(t, result, &resp)
	if len(resp.Results) != 1 {
		t.Fatalf("results count = %d, want 1", len(resp.Results))
	}
	if !resp.Results[0].Success || resp.Results[0].Todo.GetTitle() != "Single" {
		t.Fatalf("unexpected result: %+v", resp.Results[0])
	}
}

func TestConvTodoPrune(t *testing.T) {
	s := setupConvTodoStore(t)
	ctx := store.ContextWithStore(context.Background(), s)
	convId := createConv(t, ctx, s, "user1", "agent1")
	todoCtx := buildConvTodoCtx(s, "user1", true, convId, "agent1")
	todoTool := newConvTodoTool(t)

	// Add 2, complete 1.
	addResult, err := todoTool.Execute(todoCtx, `{"action":"batch","items":[{"op":"add","title":"Open Item"},{"op":"add","title":"Done Item"}]}`)
	if err != nil {
		t.Fatalf("setup batch failed: %v", err)
	}
	var setup batchResponse
	mustUnmarshalConversationJSON(t, addResult, &setup)
	doneId := setup.Results[1].Todo.ID
	if _, err := todoTool.Execute(todoCtx, `{"action":"batch","items":[{"op":"complete","todoId":"`+doneId+`"}]}`); err != nil {
		t.Fatalf("complete todo: %v", err)
	}

	// Prune.
	pruneResult, err := todoTool.Execute(todoCtx, `{"action":"prune"}`)
	if err != nil {
		t.Fatalf("prune failed: %v", err)
	}
	var pruneResp struct {
		Action    string `json:"action"`
		Success   bool   `json:"success"`
		DoneCount int    `json:"doneCount"`
	}
	mustUnmarshalConversationJSON(t, pruneResult, &pruneResp)
	if !pruneResp.Success {
		t.Fatal("expected success=true")
	}
	if pruneResp.DoneCount != 1 {
		t.Fatalf("doneCount = %d, want 1", pruneResp.DoneCount)
	}

	// Verify only the open item remains.
	listResult, err := todoTool.Execute(todoCtx, `{"action":"list"}`)
	if err != nil {
		t.Fatalf("list after prune failed: %v", err)
	}
	var listed struct {
		Todos      []interface{} `json:"todos"`
		TotalCount int           `json:"totalCount"`
		OpenCount  int           `json:"openCount"`
	}
	mustUnmarshalConversationJSON(t, listResult, &listed)
	if len(listed.Todos) != 1 {
		t.Fatalf("expected 1 todo after prune, got %d", len(listed.Todos))
	}
	if listed.OpenCount != 1 {
		t.Fatalf("openCount = %d, want 1", listed.OpenCount)
	}
}

func TestConvTodoList(t *testing.T) {
	s := setupConvTodoStore(t)
	ctx := store.ContextWithStore(context.Background(), s)
	convId := createConv(t, ctx, s, "user1", "agent1")
	todoCtx := buildConvTodoCtx(s, "user1", true, convId, "agent1")
	todoTool := newConvTodoTool(t)

	// Add.
	if _, err := todoTool.Execute(todoCtx, `{"action":"batch","items":[{"op":"add","title":"Conv Todo","priority":"high"}]}`); err != nil {
		t.Fatalf("seed batch failed: %v", err)
	}

	// List.
	listResult, err := todoTool.Execute(todoCtx, `{"action":"list"}`)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	var listed struct {
		Todos      []interface{} `json:"todos"`
		TotalCount int           `json:"totalCount"`
		OpenCount  int           `json:"openCount"`
	}
	mustUnmarshalConversationJSON(t, listResult, &listed)
	if len(listed.Todos) != 1 {
		t.Fatalf("expected 1 todo, got %d", len(listed.Todos))
	}
	if listed.OpenCount != 1 {
		t.Fatalf("openCount = %d, want 1", listed.OpenCount)
	}
}

func TestConvTodoOwnershipDenied(t *testing.T) {
	s := setupConvTodoStore(t)
	ctx := store.ContextWithStore(context.Background(), s)
	convId := createConv(t, ctx, s, "user1", "agent1")
	if err := s.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		if _, err := tx.CreateUser(ctx, &models.User{ID: "user2", Username: ptrto.Value("user2"), Admin: ptrto.Value(false)}, nil, nil); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatalf("creating user2: %v", err)
	}

	user2Ctx := buildConvTodoCtx(s, "user2", false, convId, "agent1")
	todoTool := newConvTodoTool(t)

	// user2 tries batch on user1's conversation.
	_, err := todoTool.Execute(user2Ctx, `{"action":"batch","conversationId":"`+convId+`","items":[{"op":"add","title":"Nope"}]}`)
	if err == nil {
		t.Fatal("user2 should not be able to batch on user1's conversation")
	}
}

func TestConvTodoAdminCrossAccess(t *testing.T) {
	s := setupConvTodoStore(t)
	ctx := store.ContextWithStore(context.Background(), s)
	convId := createConv(t, ctx, s, "user1", "agent1")
	if err := s.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		if _, err := tx.CreateUser(ctx, &models.User{ID: "admin2", Username: ptrto.Value("admin2"), Admin: ptrto.Value(true)}, nil, nil); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatalf("creating admin2: %v", err)
	}

	adminCtx := buildConvTodoCtx(s, "admin2", true, convId, "agent1")
	todoTool := newConvTodoTool(t)

	_, err := todoTool.Execute(adminCtx, `{"action":"batch","conversationId":"`+convId+`","items":[{"op":"add","title":"Admin Todo"}]}`)
	if err != nil {
		t.Fatalf("admin cross-access batch failed: %v", err)
	}
}
