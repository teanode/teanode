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
	_ = s.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		tx.CreateUser(ctx, &models.User{ID: userId, Username: ptrto.Value(userId), Admin: ptrto.Value(true)}, nil, nil)
		tx.CreateAgent(ctx, &models.Agent{ID: agentId, Name: ptrto.Value("Agent")}, nil, nil)
		return nil
	})
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
	// Provide runner context so the tool can infer conversationId.
	ctx = runners.ContextWithRunner(ctx, &runners.Runner{
		ConversationID: conversationId,
		AgentID:        agentId,
	})
	return ctx
}

func TestConvTodoAddAndList(t *testing.T) {
	s := setupConvTodoStore(t)
	ctx := store.ContextWithStore(context.Background(), s)
	convId := createConv(t, ctx, s, "user1", "agent1")

	todoCtx := buildConvTodoCtx(s, "user1", true, convId, "agent1")
	registry := tools.NewEmptyToolRegistry()
	registry.Register(conversations.NewConversationTodoTool())
	todoTool := registry.Get("conversation_todo")

	// Add.
	addResult, err := todoTool.Execute(todoCtx, `{"action":"add","title":"Conv Todo","priority":"high"}`)
	if err != nil {
		t.Fatalf("add failed: %v", err)
	}
	var added struct {
		Todo models.Todo `json:"todo"`
	}
	json.Unmarshal([]byte(addResult), &added)
	if added.Todo.GetTitle() != "Conv Todo" {
		t.Fatalf("title = %q, want Conv Todo", added.Todo.GetTitle())
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
	json.Unmarshal([]byte(listResult), &listed)
	if len(listed.Todos) != 1 {
		t.Fatalf("expected 1 todo, got %d", len(listed.Todos))
	}
	if listed.OpenCount != 1 {
		t.Fatalf("openCount = %d, want 1", listed.OpenCount)
	}
}

func TestConvTodoComplete(t *testing.T) {
	s := setupConvTodoStore(t)
	ctx := store.ContextWithStore(context.Background(), s)
	convId := createConv(t, ctx, s, "user1", "agent1")
	todoCtx := buildConvTodoCtx(s, "user1", true, convId, "agent1")

	registry := tools.NewEmptyToolRegistry()
	registry.Register(conversations.NewConversationTodoTool())
	todoTool := registry.Get("conversation_todo")

	addResult, _ := todoTool.Execute(todoCtx, `{"action":"add","title":"To Complete"}`)
	var added struct {
		Todo struct {
			ID string `json:"id"`
		} `json:"todo"`
	}
	json.Unmarshal([]byte(addResult), &added)

	completeResult, err := todoTool.Execute(todoCtx, `{"action":"complete","todoId":"`+added.Todo.ID+`"}`)
	if err != nil {
		t.Fatalf("complete failed: %v", err)
	}
	var completed struct {
		Todo models.Todo `json:"todo"`
	}
	json.Unmarshal([]byte(completeResult), &completed)
	if completed.Todo.GetStatus() != models.TodoStatusDone {
		t.Fatalf("status = %q, want done", completed.Todo.GetStatus())
	}
}

func TestConvTodoReopen(t *testing.T) {
	s := setupConvTodoStore(t)
	ctx := store.ContextWithStore(context.Background(), s)
	convId := createConv(t, ctx, s, "user1", "agent1")
	todoCtx := buildConvTodoCtx(s, "user1", true, convId, "agent1")

	registry := tools.NewEmptyToolRegistry()
	registry.Register(conversations.NewConversationTodoTool())
	todoTool := registry.Get("conversation_todo")

	addResult, _ := todoTool.Execute(todoCtx, `{"action":"add","title":"To Reopen"}`)
	var added struct {
		Todo struct {
			ID string `json:"id"`
		} `json:"todo"`
	}
	json.Unmarshal([]byte(addResult), &added)
	todoTool.Execute(todoCtx, `{"action":"complete","todoId":"`+added.Todo.ID+`"}`)

	reopenResult, err := todoTool.Execute(todoCtx, `{"action":"reopen","todoId":"`+added.Todo.ID+`"}`)
	if err != nil {
		t.Fatalf("reopen failed: %v", err)
	}
	var reopened struct {
		Todo models.Todo `json:"todo"`
	}
	json.Unmarshal([]byte(reopenResult), &reopened)
	if reopened.Todo.GetStatus() != models.TodoStatusOpen {
		t.Fatalf("status = %q, want open", reopened.Todo.GetStatus())
	}
}

func TestConvTodoDelete(t *testing.T) {
	s := setupConvTodoStore(t)
	ctx := store.ContextWithStore(context.Background(), s)
	convId := createConv(t, ctx, s, "user1", "agent1")
	todoCtx := buildConvTodoCtx(s, "user1", true, convId, "agent1")

	registry := tools.NewEmptyToolRegistry()
	registry.Register(conversations.NewConversationTodoTool())
	todoTool := registry.Get("conversation_todo")

	addResult, _ := todoTool.Execute(todoCtx, `{"action":"add","title":"To Delete"}`)
	var added struct {
		Todo struct {
			ID string `json:"id"`
		} `json:"todo"`
	}
	json.Unmarshal([]byte(addResult), &added)

	deleteResult, err := todoTool.Execute(todoCtx, `{"action":"delete","todoId":"`+added.Todo.ID+`"}`)
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
}

func TestConvTodoOwnershipDenied(t *testing.T) {
	s := setupConvTodoStore(t)
	ctx := store.ContextWithStore(context.Background(), s)
	// Create conv owned by user1.
	convId := createConv(t, ctx, s, "user1", "agent1")
	// Create user2 (non-admin).
	_ = s.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		tx.CreateUser(ctx, &models.User{ID: "user2", Username: ptrto.Value("user2"), Admin: ptrto.Value(false)}, nil, nil)
		return nil
	})

	// user2 tries to access user1's conversation todos.
	user2Ctx := buildConvTodoCtx(s, "user2", false, convId, "agent1")
	registry := tools.NewEmptyToolRegistry()
	registry.Register(conversations.NewConversationTodoTool())
	todoTool := registry.Get("conversation_todo")

	_, err := todoTool.Execute(user2Ctx, `{"action":"list","conversationId":"`+convId+`"}`)
	if err == nil {
		t.Fatal("user2 should not be able to list user1's conversation todos")
	}
}

func TestConvTodoClearDone(t *testing.T) {
	s := setupConvTodoStore(t)
	ctx := store.ContextWithStore(context.Background(), s)
	convId := createConv(t, ctx, s, "user1", "agent1")
	todoCtx := buildConvTodoCtx(s, "user1", true, convId, "agent1")

	registry := tools.NewEmptyToolRegistry()
	registry.Register(conversations.NewConversationTodoTool())
	todoTool := registry.Get("conversation_todo")

	// Add two todos, complete one.
	todoTool.Execute(todoCtx, `{"action":"add","title":"Open Item"}`)
	addResult, _ := todoTool.Execute(todoCtx, `{"action":"add","title":"Done Item"}`)
	var added struct {
		Todo struct {
			ID string `json:"id"`
		} `json:"todo"`
	}
	json.Unmarshal([]byte(addResult), &added)
	todoTool.Execute(todoCtx, `{"action":"complete","todoId":"`+added.Todo.ID+`"}`)

	// clear_done should remove only the done item.
	clearResult, err := todoTool.Execute(todoCtx, `{"action":"clear_done"}`)
	if err != nil {
		t.Fatalf("clear_done failed: %v", err)
	}
	var cleared struct {
		Action    string `json:"action"`
		Success   bool   `json:"success"`
		DoneCount int    `json:"doneCount"`
	}
	json.Unmarshal([]byte(clearResult), &cleared)
	if !cleared.Success {
		t.Fatal("expected success=true")
	}
	if cleared.DoneCount != 1 {
		t.Fatalf("doneCount = %d, want 1", cleared.DoneCount)
	}

	// Verify only the open item remains.
	listResult, _ := todoTool.Execute(todoCtx, `{"action":"list"}`)
	var listed struct {
		Todos      []interface{} `json:"todos"`
		TotalCount int           `json:"totalCount"`
		OpenCount  int           `json:"openCount"`
	}
	json.Unmarshal([]byte(listResult), &listed)
	if len(listed.Todos) != 1 {
		t.Fatalf("expected 1 todo after clear_done, got %d", len(listed.Todos))
	}
	if listed.OpenCount != 1 {
		t.Fatalf("openCount = %d, want 1", listed.OpenCount)
	}
}

func TestConvTodoReset(t *testing.T) {
	s := setupConvTodoStore(t)
	ctx := store.ContextWithStore(context.Background(), s)
	convId := createConv(t, ctx, s, "user1", "agent1")
	todoCtx := buildConvTodoCtx(s, "user1", true, convId, "agent1")

	registry := tools.NewEmptyToolRegistry()
	registry.Register(conversations.NewConversationTodoTool())
	todoTool := registry.Get("conversation_todo")

	// Add two todos, complete one.
	todoTool.Execute(todoCtx, `{"action":"add","title":"Open Item"}`)
	addResult, _ := todoTool.Execute(todoCtx, `{"action":"add","title":"Done Item"}`)
	var added struct {
		Todo struct {
			ID string `json:"id"`
		} `json:"todo"`
	}
	json.Unmarshal([]byte(addResult), &added)
	todoTool.Execute(todoCtx, `{"action":"complete","todoId":"`+added.Todo.ID+`"}`)

	// reset should remove all items.
	resetResult, err := todoTool.Execute(todoCtx, `{"action":"reset"}`)
	if err != nil {
		t.Fatalf("reset failed: %v", err)
	}
	var resetResp struct {
		Action     string `json:"action"`
		Success    bool   `json:"success"`
		TotalCount int    `json:"totalCount"`
	}
	json.Unmarshal([]byte(resetResult), &resetResp)
	if !resetResp.Success {
		t.Fatal("expected success=true")
	}
	if resetResp.TotalCount != 2 {
		t.Fatalf("totalCount = %d, want 2", resetResp.TotalCount)
	}

	// Verify list is empty.
	listResult, _ := todoTool.Execute(todoCtx, `{"action":"list"}`)
	var listed struct {
		Todos      []interface{} `json:"todos"`
		TotalCount int           `json:"totalCount"`
	}
	json.Unmarshal([]byte(listResult), &listed)
	if len(listed.Todos) != 0 {
		t.Fatalf("expected 0 todos after reset, got %d", len(listed.Todos))
	}
}

func TestConvTodoAdminCrossAccess(t *testing.T) {
	s := setupConvTodoStore(t)
	ctx := store.ContextWithStore(context.Background(), s)
	convId := createConv(t, ctx, s, "user1", "agent1")
	// Create admin user.
	_ = s.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		tx.CreateUser(ctx, &models.User{ID: "admin2", Username: ptrto.Value("admin2"), Admin: ptrto.Value(true)}, nil, nil)
		return nil
	})

	// Admin should access any user's conversation todos.
	adminCtx := buildConvTodoCtx(s, "admin2", true, convId, "agent1")
	registry := tools.NewEmptyToolRegistry()
	registry.Register(conversations.NewConversationTodoTool())
	todoTool := registry.Get("conversation_todo")

	_, err := todoTool.Execute(adminCtx, `{"action":"add","conversationId":"`+convId+`","title":"Admin Todo"}`)
	if err != nil {
		t.Fatalf("admin cross-access add failed: %v", err)
	}
}
