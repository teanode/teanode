package google

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/teanode/teanode/internal/provider"
)

// mockRunner returns a commandRunner that records calls and returns canned output.
func mockRunner(output string, err error) (commandRunner, *[][]string) {
	var calls [][]string
	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		call := append([]string{name}, args...)
		calls = append(calls, call)
		if err != nil {
			return nil, err
		}
		return []byte(output), nil
	}
	return runner, &calls
}

func TestExecGog_ArgsAssembly(t *testing.T) {
	runner, calls := mockRunner(`{"ok":true}`, nil)

	result, err := execGog(context.Background(), runner, "/usr/bin/gog", "user@test.com", "gmail", "search", "--query", "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != `{"ok":true}` {
		t.Errorf("unexpected result: %s", result)
	}
	if len(*calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(*calls))
	}

	args := (*calls)[0]
	// Should be: /usr/bin/gog --json --no-input --results-only --account user@test.com gmail search --query test
	expected := []string{"/usr/bin/gog", "--json", "--no-input", "--results-only", "--account", "user@test.com", "gmail", "search", "--query", "test"}
	if len(args) != len(expected) {
		t.Fatalf("expected %d args, got %d: %v", len(expected), len(args), args)
	}
	for i, want := range expected {
		if args[i] != want {
			t.Errorf("arg[%d] = %q, want %q", i, args[i], want)
		}
	}
}

func TestExecGog_NoAccount(t *testing.T) {
	runner, calls := mockRunner(`[]`, nil)

	_, err := execGog(context.Background(), runner, "gog", "", "tasks", "list")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := (*calls)[0]
	// Should NOT contain --account
	for i, arg := range args {
		if arg == "--account" {
			t.Errorf("unexpected --account flag at position %d", i)
		}
	}
}

func TestExecGog_OutputTruncation(t *testing.T) {
	// Generate output larger than maxOutputBytes.
	bigOutput := strings.Repeat("x", maxOutputBytes+1000)
	runner, _ := mockRunner(bigOutput, nil)

	result, err := execGog(context.Background(), runner, "gog", "", "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasSuffix(result, "\n... (output truncated)") {
		t.Error("expected truncation suffix")
	}
	// The truncated content should be maxOutputBytes + the suffix.
	if !strings.HasPrefix(result, strings.Repeat("x", 100)) {
		t.Error("expected content before truncation")
	}
}

func TestExecGog_AuthError(t *testing.T) {
	runner, _ := mockRunner("", fmt.Errorf("error: not authenticated, run gog auth login"))

	_, err := execGog(context.Background(), runner, "gog", "", "gmail", "search")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "authentication required") {
		t.Errorf("expected auth error message, got: %v", err)
	}
}

func TestExecGog_GenericError(t *testing.T) {
	runner, _ := mockRunner("", fmt.Errorf("some random error"))

	_, err := execGog(context.Background(), runner, "gog", "", "gmail", "search")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "gog command failed") {
		t.Errorf("expected generic error message, got: %v", err)
	}
}

func TestGmailTool_SearchAction(t *testing.T) {
	runner, calls := mockRunner(`[{"id":"msg1","subject":"Hello"}]`, nil)
	tool := &gmailTool{binary: "gog", runner: runner}

	args, _ := json.Marshal(map[string]interface{}{
		"action": "search",
		"query":  "from:test@example.com",
		"limit":  5,
	})
	result, err := tool.Execute(context.Background(), string(args))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result")
	}

	// Verify the command args include gmail search.
	cmdArgs := (*calls)[0]
	found := false
	for i, arg := range cmdArgs {
		if arg == "gmail" && i+1 < len(cmdArgs) && cmdArgs[i+1] == "search" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'gmail search' in args: %v", cmdArgs)
	}
}

func TestGmailTool_ReadAction(t *testing.T) {
	runner, calls := mockRunner(`{"id":"msg1","body":"Hello world"}`, nil)
	tool := &gmailTool{binary: "gog", runner: runner}

	args, _ := json.Marshal(map[string]interface{}{
		"action":     "read",
		"message_id": "msg123",
	})
	_, err := tool.Execute(context.Background(), string(args))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmdArgs := (*calls)[0]
	foundGet := false
	foundMessageID := false
	for index, arg := range cmdArgs {
		if arg == "gmail" && index+1 < len(cmdArgs) && cmdArgs[index+1] == "get" {
			foundGet = true
		}
		if arg == "msg123" {
			foundMessageID = true
		}
	}
	if !foundGet {
		t.Errorf("expected 'gmail get' in args: %v", cmdArgs)
	}
	if !foundMessageID {
		t.Errorf("expected message ID in args: %v", cmdArgs)
	}
}

func TestGmailTool_ReplyAction(t *testing.T) {
	runner, calls := mockRunner(`{"status":"sent"}`, nil)
	tool := &gmailTool{binary: "gog", runner: runner}

	args, _ := json.Marshal(map[string]interface{}{
		"action":     "reply",
		"message_id": "msg456",
		"body":       "Thanks!",
	})
	_, err := tool.Execute(context.Background(), string(args))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmdArgs := (*calls)[0]
	foundSend := false
	foundReplyTo := false
	foundBody := false
	for index, arg := range cmdArgs {
		if arg == "gmail" && index+1 < len(cmdArgs) && cmdArgs[index+1] == "send" {
			foundSend = true
		}
		if arg == "--reply-to-message-id" && index+1 < len(cmdArgs) && cmdArgs[index+1] == "msg456" {
			foundReplyTo = true
		}
		if arg == "--body" && index+1 < len(cmdArgs) && cmdArgs[index+1] == "Thanks!" {
			foundBody = true
		}
	}
	if !foundSend {
		t.Errorf("expected 'gmail send' in args: %v", cmdArgs)
	}
	if !foundReplyTo {
		t.Errorf("expected '--reply-to-message-id msg456' in args: %v", cmdArgs)
	}
	if !foundBody {
		t.Errorf("expected '--body' in args: %v", cmdArgs)
	}
}

func TestGmailTool_TrashAction(t *testing.T) {
	runner, calls := mockRunner(`{"status":"ok"}`, nil)
	tool := &gmailTool{binary: "gog", runner: runner}

	args, _ := json.Marshal(map[string]interface{}{
		"action":     "trash",
		"message_id": "msg789",
	})
	_, err := tool.Execute(context.Background(), string(args))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmdArgs := (*calls)[0]
	foundThreadModify := false
	foundAddTrash := false
	foundID := false
	for index, arg := range cmdArgs {
		if arg == "thread" && index+1 < len(cmdArgs) && cmdArgs[index+1] == "modify" {
			foundThreadModify = true
		}
		if arg == "--add" && index+1 < len(cmdArgs) && cmdArgs[index+1] == "TRASH" {
			foundAddTrash = true
		}
		if arg == "msg789" {
			foundID = true
		}
	}
	if !foundThreadModify {
		t.Errorf("expected 'thread modify' in args: %v", cmdArgs)
	}
	if !foundAddTrash {
		t.Errorf("expected '--add TRASH' in args: %v", cmdArgs)
	}
	if !foundID {
		t.Errorf("expected message ID in args: %v", cmdArgs)
	}
}

func TestGmailTool_SendAction(t *testing.T) {
	runner, _ := mockRunner(`{"status":"sent"}`, nil)
	tool := &gmailTool{binary: "gog", runner: runner}

	args, _ := json.Marshal(map[string]interface{}{
		"action":  "send",
		"to":      "recipient@test.com",
		"subject": "Test Subject",
		"body":    "Test body",
	})
	_, err := tool.Execute(context.Background(), string(args))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGmailTool_SendMissingFields(t *testing.T) {
	runner, _ := mockRunner("", nil)
	tool := &gmailTool{binary: "gog", runner: runner}

	// Missing 'to'.
	args, _ := json.Marshal(map[string]interface{}{
		"action":  "send",
		"subject": "Test",
		"body":    "Test",
	})
	_, err := tool.Execute(context.Background(), string(args))
	if err == nil || !strings.Contains(err.Error(), "to is required") {
		t.Errorf("expected 'to is required' error, got: %v", err)
	}
}

func TestGmailTool_UnknownAction(t *testing.T) {
	runner, _ := mockRunner("", nil)
	tool := &gmailTool{binary: "gog", runner: runner}

	args, _ := json.Marshal(map[string]interface{}{
		"action": "unknown",
	})
	_, err := tool.Execute(context.Background(), string(args))
	if err == nil || !strings.Contains(err.Error(), "unknown gmail action") {
		t.Errorf("expected unknown action error, got: %v", err)
	}
}

func TestCalendarTool_ListAction(t *testing.T) {
	runner, calls := mockRunner(`[{"summary":"Meeting"}]`, nil)
	tool := &calendarTool{binary: "gog", runner: runner}

	args, _ := json.Marshal(map[string]interface{}{
		"action": "list",
		"days":   3,
	})
	_, err := tool.Execute(context.Background(), string(args))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmdArgs := (*calls)[0]
	foundDays := false
	for i, arg := range cmdArgs {
		if arg == "--days" && i+1 < len(cmdArgs) && cmdArgs[i+1] == "3" {
			foundDays = true
			break
		}
	}
	if !foundDays {
		t.Errorf("expected --days 3 in args: %v", cmdArgs)
	}
}

func TestCalendarTool_CreateAction(t *testing.T) {
	runner, calls := mockRunner(`{"status":"created"}`, nil)
	tool := &calendarTool{binary: "gog", runner: runner}

	args, _ := json.Marshal(map[string]interface{}{
		"action":      "create",
		"summary":     "Team Standup",
		"from":        "2025-01-15T09:00:00",
		"to":          "2025-01-15T09:30:00",
		"description": "Daily standup",
		"attendees":   "alice@test.com,bob@test.com",
	})
	_, err := tool.Execute(context.Background(), string(args))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmdArgs := (*calls)[0]
	hasDescription := false
	hasAttendees := false
	for i, arg := range cmdArgs {
		if arg == "--description" && i+1 < len(cmdArgs) {
			hasDescription = true
		}
		if arg == "--attendees" && i+1 < len(cmdArgs) {
			hasAttendees = true
		}
	}
	if !hasDescription {
		t.Error("expected --description in args")
	}
	if !hasAttendees {
		t.Error("expected --attendees in args")
	}
}

func TestCalendarTool_SearchAction(t *testing.T) {
	runner, calls := mockRunner(`[{"summary":"Team Meeting"}]`, nil)
	tool := &calendarTool{binary: "gog", runner: runner}

	args, _ := json.Marshal(map[string]interface{}{
		"action": "search",
		"query":  "team meeting",
	})
	_, err := tool.Execute(context.Background(), string(args))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmdArgs := (*calls)[0]
	foundSearch := false
	foundQuery := false
	for index, arg := range cmdArgs {
		if arg == "calendar" && index+1 < len(cmdArgs) && cmdArgs[index+1] == "search" {
			foundSearch = true
		}
		if arg == "team meeting" {
			foundQuery = true
		}
		if arg == "--query" {
			t.Errorf("should not use --query flag (query is positional): %v", cmdArgs)
		}
		if arg == "primary" {
			t.Errorf("should not pass 'primary' as positional (calendar search takes query, not calendarId): %v", cmdArgs)
		}
	}
	if !foundSearch {
		t.Errorf("expected 'calendar search' in args: %v", cmdArgs)
	}
	if !foundQuery {
		t.Errorf("expected query as positional arg: %v", cmdArgs)
	}
}

func TestTasksTool_ListAction(t *testing.T) {
	runner, calls := mockRunner(`[{"id":"task1","title":"Buy milk"}]`, nil)
	tool := &tasksTool{binary: "gog", runner: runner}

	args, _ := json.Marshal(map[string]interface{}{
		"action":    "list",
		"task_list": "mylist123",
	})
	_, err := tool.Execute(context.Background(), string(args))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmdArgs := (*calls)[0]
	foundTasksList := false
	foundListID := false
	for index, arg := range cmdArgs {
		if arg == "tasks" && index+1 < len(cmdArgs) && cmdArgs[index+1] == "list" {
			foundTasksList = true
		}
		if arg == "mylist123" {
			foundListID = true
		}
	}
	if !foundTasksList {
		t.Errorf("expected 'tasks list' in args: %v", cmdArgs)
	}
	if !foundListID {
		t.Errorf("expected task list ID as positional arg: %v", cmdArgs)
	}
}

func TestTasksTool_CreateAction(t *testing.T) {
	runner, calls := mockRunner(`{"status":"created"}`, nil)
	tool := &tasksTool{binary: "gog", runner: runner}

	args, _ := json.Marshal(map[string]interface{}{
		"action":    "create",
		"task_list": "mylist123",
		"title":     "Buy groceries",
		"notes":     "Milk, bread, eggs",
		"due":       "2025-01-20",
	})
	_, err := tool.Execute(context.Background(), string(args))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmdArgs := (*calls)[0]
	hasTitle := false
	hasNotes := false
	hasDue := false
	hasListID := false
	for index, arg := range cmdArgs {
		if arg == "--title" && index+1 < len(cmdArgs) && cmdArgs[index+1] == "Buy groceries" {
			hasTitle = true
		}
		if arg == "--notes" && index+1 < len(cmdArgs) {
			hasNotes = true
		}
		if arg == "--due" && index+1 < len(cmdArgs) {
			hasDue = true
		}
		if arg == "mylist123" {
			hasListID = true
		}
	}
	if !hasTitle {
		t.Error("expected --title in args")
	}
	if !hasNotes {
		t.Error("expected --notes in args")
	}
	if !hasDue {
		t.Error("expected --due in args")
	}
	if !hasListID {
		t.Errorf("expected task list ID as positional arg: %v", cmdArgs)
	}
}

func TestTasksTool_CompleteAction(t *testing.T) {
	runner, calls := mockRunner(`{"status":"completed"}`, nil)
	tool := &tasksTool{binary: "gog", runner: runner}

	args, _ := json.Marshal(map[string]interface{}{
		"action":    "complete",
		"task_list": "mylist123",
		"task_id":   "task123",
	})
	_, err := tool.Execute(context.Background(), string(args))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmdArgs := (*calls)[0]
	foundComplete := false
	foundListID := false
	foundTaskID := false
	for index, arg := range cmdArgs {
		if arg == "tasks" && index+1 < len(cmdArgs) && cmdArgs[index+1] == "complete" {
			foundComplete = true
		}
		if arg == "mylist123" {
			foundListID = true
		}
		if arg == "task123" {
			foundTaskID = true
		}
	}
	if !foundComplete {
		t.Errorf("expected 'tasks complete' in args: %v", cmdArgs)
	}
	if !foundListID {
		t.Errorf("expected task list ID in args: %v", cmdArgs)
	}
	if !foundTaskID {
		t.Errorf("expected task ID in args: %v", cmdArgs)
	}
}

func TestTasksTool_DeleteAction(t *testing.T) {
	runner, calls := mockRunner(`{"status":"deleted"}`, nil)
	tool := &tasksTool{binary: "gog", runner: runner}

	args, _ := json.Marshal(map[string]interface{}{
		"action":    "delete",
		"task_list": "mylist123",
		"task_id":   "task456",
	})
	_, err := tool.Execute(context.Background(), string(args))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmdArgs := (*calls)[0]
	foundDelete := false
	foundListID := false
	foundTaskID := false
	for index, arg := range cmdArgs {
		if arg == "tasks" && index+1 < len(cmdArgs) && cmdArgs[index+1] == "delete" {
			foundDelete = true
		}
		if arg == "mylist123" {
			foundListID = true
		}
		if arg == "task456" {
			foundTaskID = true
		}
	}
	if !foundDelete {
		t.Errorf("expected 'tasks delete' in args: %v", cmdArgs)
	}
	if !foundListID {
		t.Errorf("expected task list ID in args: %v", cmdArgs)
	}
	if !foundTaskID {
		t.Errorf("expected task ID in args: %v", cmdArgs)
	}
}

func TestTasksTool_MissingTaskList(t *testing.T) {
	runner, _ := mockRunner("", nil)
	tool := &tasksTool{binary: "gog", runner: runner}

	args, _ := json.Marshal(map[string]interface{}{
		"action": "list",
	})
	_, err := tool.Execute(context.Background(), string(args))
	if err == nil || !strings.Contains(err.Error(), "task_list is required") {
		t.Errorf("expected 'task_list is required' error, got: %v", err)
	}
}

func TestDriveTool_ListAction(t *testing.T) {
	runner, calls := mockRunner(`[{"name":"doc.pdf"}]`, nil)
	tool := &driveTool{binary: "gog", runner: runner}

	args, _ := json.Marshal(map[string]interface{}{
		"action": "list",
		"limit":  5,
	})
	_, err := tool.Execute(context.Background(), string(args))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmdArgs := (*calls)[0]
	foundLs := false
	foundMax := false
	for index, arg := range cmdArgs {
		if arg == "drive" && index+1 < len(cmdArgs) && cmdArgs[index+1] == "ls" {
			foundLs = true
		}
		if arg == "--max" && index+1 < len(cmdArgs) && cmdArgs[index+1] == "5" {
			foundMax = true
		}
	}
	if !foundLs {
		t.Errorf("expected 'drive ls' in args: %v", cmdArgs)
	}
	if !foundMax {
		t.Errorf("expected '--max 5' in args: %v", cmdArgs)
	}
}

func TestDriveTool_SearchAction(t *testing.T) {
	runner, calls := mockRunner(`[{"name":"doc.pdf"}]`, nil)
	tool := &driveTool{binary: "gog", runner: runner}

	args, _ := json.Marshal(map[string]interface{}{
		"action": "search",
		"query":  "budget 2025",
		"limit":  10,
	})
	_, err := tool.Execute(context.Background(), string(args))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmdArgs := (*calls)[0]
	foundSearch := false
	foundQuery := false
	foundMax := false
	for index, arg := range cmdArgs {
		if arg == "drive" && index+1 < len(cmdArgs) && cmdArgs[index+1] == "search" {
			foundSearch = true
		}
		if arg == "budget 2025" {
			foundQuery = true
		}
		if arg == "--max" && index+1 < len(cmdArgs) && cmdArgs[index+1] == "10" {
			foundMax = true
		}
		if arg == "--query" {
			t.Errorf("should not use --query flag (query is positional): %v", cmdArgs)
		}
	}
	if !foundSearch {
		t.Errorf("expected 'drive search' in args: %v", cmdArgs)
	}
	if !foundQuery {
		t.Errorf("expected query as positional arg: %v", cmdArgs)
	}
	if !foundMax {
		t.Errorf("expected '--max 10' in args: %v", cmdArgs)
	}
}

func TestDriveTool_InfoAction(t *testing.T) {
	runner, calls := mockRunner(`{"name":"doc.pdf","mimeType":"application/pdf"}`, nil)
	tool := &driveTool{binary: "gog", runner: runner}

	args, _ := json.Marshal(map[string]interface{}{
		"action":  "info",
		"file_id": "file123",
	})
	_, err := tool.Execute(context.Background(), string(args))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmdArgs := (*calls)[0]
	foundGet := false
	foundFileID := false
	for index, arg := range cmdArgs {
		if arg == "drive" && index+1 < len(cmdArgs) && cmdArgs[index+1] == "get" {
			foundGet = true
		}
		if arg == "file123" {
			foundFileID = true
		}
	}
	if !foundGet {
		t.Errorf("expected 'drive get' in args: %v", cmdArgs)
	}
	if !foundFileID {
		t.Errorf("expected file ID in args: %v", cmdArgs)
	}
}

func TestContactsTool_SearchAction(t *testing.T) {
	runner, calls := mockRunner(`[{"name":"Alice"}]`, nil)
	tool := &contactsTool{binary: "gog", runner: runner}

	args, _ := json.Marshal(map[string]interface{}{
		"action": "search",
		"query":  "Alice",
		"limit":  5,
	})
	_, err := tool.Execute(context.Background(), string(args))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmdArgs := (*calls)[0]
	foundSearch := false
	foundQuery := false
	foundMax := false
	for index, arg := range cmdArgs {
		if arg == "contacts" && index+1 < len(cmdArgs) && cmdArgs[index+1] == "search" {
			foundSearch = true
		}
		if arg == "Alice" {
			foundQuery = true
		}
		if arg == "--max" && index+1 < len(cmdArgs) && cmdArgs[index+1] == "5" {
			foundMax = true
		}
		if arg == "--query" {
			t.Errorf("should not use --query flag (query is positional): %v", cmdArgs)
		}
	}
	if !foundSearch {
		t.Errorf("expected 'contacts search' in args: %v", cmdArgs)
	}
	if !foundQuery {
		t.Errorf("expected query as positional arg: %v", cmdArgs)
	}
	if !foundMax {
		t.Errorf("expected '--max 5' in args: %v", cmdArgs)
	}
}

func TestContactsTool_ListAction(t *testing.T) {
	runner, calls := mockRunner(`[{"name":"Alice"},{"name":"Bob"}]`, nil)
	tool := &contactsTool{binary: "gog", runner: runner}

	args, _ := json.Marshal(map[string]interface{}{
		"action": "list",
		"limit":  20,
	})
	_, err := tool.Execute(context.Background(), string(args))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmdArgs := (*calls)[0]
	foundList := false
	foundMax := false
	for index, arg := range cmdArgs {
		if arg == "contacts" && index+1 < len(cmdArgs) && cmdArgs[index+1] == "list" {
			foundList = true
		}
		if arg == "--max" && index+1 < len(cmdArgs) && cmdArgs[index+1] == "20" {
			foundMax = true
		}
		if arg == "--limit" {
			t.Errorf("should not use --limit flag (use --max): %v", cmdArgs)
		}
	}
	if !foundList {
		t.Errorf("expected 'contacts list' in args: %v", cmdArgs)
	}
	if !foundMax {
		t.Errorf("expected '--max 20' in args: %v", cmdArgs)
	}
}

func TestToolDefinitions(t *testing.T) {
	tools := []struct {
		name string
		tool interface {
			Definition() provider.ToolDefinition
		}
	}{
		{"gmail", &gmailTool{}},
		{"calendar", &calendarTool{}},
		{"tasks", &tasksTool{}},
		{"drive", &driveTool{}},
		{"contacts", &contactsTool{}},
	}

	for _, tt := range tools {
		t.Run(tt.name, func(t *testing.T) {
			def := tt.tool.Definition()
			if def.Type != "function" {
				t.Errorf("expected type 'function', got %q", def.Type)
			}
			if !strings.HasPrefix(def.Function.Name, "google_") {
				t.Errorf("expected name to start with 'google_', got %q", def.Function.Name)
			}
			if def.Function.Description == "" {
				t.Error("expected non-empty description")
			}
			if def.Function.Parameters == nil {
				t.Error("expected non-nil parameters")
			}

			// Verify action enum exists in parameters.
			params, ok := def.Function.Parameters.(map[string]interface{})
			if !ok {
				t.Fatal("parameters should be a map")
			}
			props, ok := params["properties"].(map[string]interface{})
			if !ok {
				t.Fatal("properties should be a map")
			}
			action, ok := props["action"].(map[string]interface{})
			if !ok {
				t.Fatal("action property should exist")
			}
			if action["type"] != "string" {
				t.Error("action should be type string")
			}
			if action["enum"] == nil {
				t.Error("action should have enum")
			}
		})
	}
}

func TestIsAuthError(t *testing.T) {
	tests := []struct {
		msg  string
		want bool
	}{
		{"not authenticated", true},
		{"error: not logged in", true},
		{"token expired please login again", true},
		{"invalid credentials", true},
		{"login required", true},
		{"file not found", false},
		{"network timeout", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.msg, func(t *testing.T) {
			got := isAuthError(tt.msg)
			if got != tt.want {
				t.Errorf("isAuthError(%q) = %v, want %v", tt.msg, got, tt.want)
			}
		})
	}
}
