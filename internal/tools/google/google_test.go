package google

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/teanode/teanode/internal/providers"
)

// mockRunner returns a commandRunner that records calls and returns canned output.
func mockRunner(output string, err error) (commandRunner, *[][]string) {
	var calls [][]string
	runner := func(ctx context.Context, name string, arguments ...string) ([]byte, error) {
		call := append([]string{name}, arguments...)
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

	arguments := (*calls)[0]
	// Should be: /usr/bin/gog --json --no-input --results-only --account user@test.com gmail search --query test
	expected := []string{"/usr/bin/gog", "--json", "--no-input", "--results-only", "--account", "user@test.com", "gmail", "search", "--query", "test"}
	if len(arguments) != len(expected) {
		t.Fatalf("expected %d arguments, got %d: %v", len(expected), len(arguments), arguments)
	}
	for i, want := range expected {
		if arguments[i] != want {
			t.Errorf("argument[%d] = %q, want %q", i, arguments[i], want)
		}
	}
}

func TestExecGog_NoAccount(t *testing.T) {
	runner, calls := mockRunner(`[]`, nil)

	_, err := execGog(context.Background(), runner, "gog", "", "tasks", "list")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	arguments := (*calls)[0]
	// Should NOT contain --account
	for i, argument := range arguments {
		if argument == "--account" {
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

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "search",
		"query":  "from:test@example.com",
		"limit":  5,
	})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result")
	}

	// Verify the command arguments include gmail search.
	commandArguments := (*calls)[0]
	found := false
	for i, argument := range commandArguments {
		if argument == "gmail" && i+1 < len(commandArguments) && commandArguments[i+1] == "search" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'gmail search' in arguments: %v", commandArguments)
	}
}

func TestGmailTool_ReadAction(t *testing.T) {
	runner, calls := mockRunner(`{"id":"msg1","body":"Hello world"}`, nil)
	tool := &gmailTool{binary: "gog", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":     "read",
		"message_id": "msg123",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	commandArguments := (*calls)[0]
	foundGet := false
	foundMessageId := false
	for index, argument := range commandArguments {
		if argument == "gmail" && index+1 < len(commandArguments) && commandArguments[index+1] == "get" {
			foundGet = true
		}
		if argument == "msg123" {
			foundMessageId = true
		}
	}
	if !foundGet {
		t.Errorf("expected 'gmail get' in arguments: %v", commandArguments)
	}
	if !foundMessageId {
		t.Errorf("expected message ID in arguments: %v", commandArguments)
	}
}

func TestGmailTool_ReplyAction(t *testing.T) {
	runner, calls := mockRunner(`{"status":"sent"}`, nil)
	tool := &gmailTool{binary: "gog", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":     "reply",
		"message_id": "msg456",
		"body":       "Thanks!",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	commandArguments := (*calls)[0]
	foundSend := false
	foundReplyTo := false
	foundBody := false
	for index, argument := range commandArguments {
		if argument == "gmail" && index+1 < len(commandArguments) && commandArguments[index+1] == "send" {
			foundSend = true
		}
		if argument == "--reply-to-message-id" && index+1 < len(commandArguments) && commandArguments[index+1] == "msg456" {
			foundReplyTo = true
		}
		if argument == "--body" && index+1 < len(commandArguments) && commandArguments[index+1] == "Thanks!" {
			foundBody = true
		}
	}
	if !foundSend {
		t.Errorf("expected 'gmail send' in arguments: %v", commandArguments)
	}
	if !foundReplyTo {
		t.Errorf("expected '--reply-to-message-id msg456' in arguments: %v", commandArguments)
	}
	if !foundBody {
		t.Errorf("expected '--body' in arguments: %v", commandArguments)
	}
}

func TestGmailTool_TrashAction(t *testing.T) {
	runner, calls := mockRunner(`{"status":"ok"}`, nil)
	tool := &gmailTool{binary: "gog", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":     "trash",
		"message_id": "msg789",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	commandArguments := (*calls)[0]
	foundThreadModify := false
	foundAddTrash := false
	foundId := false
	for index, argument := range commandArguments {
		if argument == "thread" && index+1 < len(commandArguments) && commandArguments[index+1] == "modify" {
			foundThreadModify = true
		}
		if argument == "--add" && index+1 < len(commandArguments) && commandArguments[index+1] == "TRASH" {
			foundAddTrash = true
		}
		if argument == "msg789" {
			foundId = true
		}
	}
	if !foundThreadModify {
		t.Errorf("expected 'thread modify' in arguments: %v", commandArguments)
	}
	if !foundAddTrash {
		t.Errorf("expected '--add TRASH' in arguments: %v", commandArguments)
	}
	if !foundId {
		t.Errorf("expected message ID in arguments: %v", commandArguments)
	}
}

func TestGmailTool_SendAction(t *testing.T) {
	runner, _ := mockRunner(`{"status":"sent"}`, nil)
	tool := &gmailTool{binary: "gog", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":  "send",
		"to":      "recipient@test.com",
		"subject": "Test Subject",
		"body":    "Test body",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGmailTool_SendMissingFields(t *testing.T) {
	runner, _ := mockRunner("", nil)
	tool := &gmailTool{binary: "gog", runner: runner}

	// Missing 'to'.
	arguments, _ := json.Marshal(map[string]interface{}{
		"action":  "send",
		"subject": "Test",
		"body":    "Test",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "to is required") {
		t.Errorf("expected 'to is required' error, got: %v", err)
	}
}

func TestGmailTool_UnknownAction(t *testing.T) {
	runner, _ := mockRunner("", nil)
	tool := &gmailTool{binary: "gog", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "unknown",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "unknown gmail action") {
		t.Errorf("expected unknown action error, got: %v", err)
	}
}

func TestCalendarTool_ListAction(t *testing.T) {
	runner, calls := mockRunner(`[{"summary":"Meeting"}]`, nil)
	tool := &calendarTool{binary: "gog", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "list",
		"days":   3,
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	commandArguments := (*calls)[0]
	foundDays := false
	for i, argument := range commandArguments {
		if argument == "--days" && i+1 < len(commandArguments) && commandArguments[i+1] == "3" {
			foundDays = true
			break
		}
	}
	if !foundDays {
		t.Errorf("expected --days 3 in arguments: %v", commandArguments)
	}
}

func TestCalendarTool_CreateAction(t *testing.T) {
	runner, calls := mockRunner(`{"status":"created"}`, nil)
	tool := &calendarTool{binary: "gog", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":      "create",
		"summary":     "Team Standup",
		"from":        "2025-01-15T09:00:00",
		"to":          "2025-01-15T09:30:00",
		"description": "Daily standup",
		"attendees":   "alice@test.com,bob@test.com",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	commandArguments := (*calls)[0]
	hasDescription := false
	hasAttendees := false
	for i, argument := range commandArguments {
		if argument == "--description" && i+1 < len(commandArguments) {
			hasDescription = true
		}
		if argument == "--attendees" && i+1 < len(commandArguments) {
			hasAttendees = true
		}
	}
	if !hasDescription {
		t.Error("expected --description in arguments")
	}
	if !hasAttendees {
		t.Error("expected --attendees in arguments")
	}
}

func TestCalendarTool_SearchAction(t *testing.T) {
	runner, calls := mockRunner(`[{"summary":"Team Meeting"}]`, nil)
	tool := &calendarTool{binary: "gog", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "search",
		"query":  "team meeting",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	commandArguments := (*calls)[0]
	foundSearch := false
	foundQuery := false
	for index, argument := range commandArguments {
		if argument == "calendar" && index+1 < len(commandArguments) && commandArguments[index+1] == "search" {
			foundSearch = true
		}
		if argument == "team meeting" {
			foundQuery = true
		}
		if argument == "--query" {
			t.Errorf("should not use --query flag (query is positional): %v", commandArguments)
		}
		if argument == "primary" {
			t.Errorf("should not pass 'primary' as positional (calendar search takes query, not calendarId): %v", commandArguments)
		}
	}
	if !foundSearch {
		t.Errorf("expected 'calendar search' in arguments: %v", commandArguments)
	}
	if !foundQuery {
		t.Errorf("expected query as positional argument: %v", commandArguments)
	}
}

func TestTasksTool_ListAction(t *testing.T) {
	runner, calls := mockRunner(`[{"id":"task1","title":"Buy milk"}]`, nil)
	tool := &tasksTool{binary: "gog", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":    "list",
		"task_list": "mylist123",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	commandArguments := (*calls)[0]
	foundTasksList := false
	foundListId := false
	for index, argument := range commandArguments {
		if argument == "tasks" && index+1 < len(commandArguments) && commandArguments[index+1] == "list" {
			foundTasksList = true
		}
		if argument == "mylist123" {
			foundListId = true
		}
	}
	if !foundTasksList {
		t.Errorf("expected 'tasks list' in arguments: %v", commandArguments)
	}
	if !foundListId {
		t.Errorf("expected task list ID as positional argument: %v", commandArguments)
	}
}

func TestTasksTool_CreateAction(t *testing.T) {
	runner, calls := mockRunner(`{"status":"created"}`, nil)
	tool := &tasksTool{binary: "gog", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":    "create",
		"task_list": "mylist123",
		"title":     "Buy groceries",
		"notes":     "Milk, bread, eggs",
		"due":       "2025-01-20",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	commandArguments := (*calls)[0]
	hasTitle := false
	hasNotes := false
	hasDue := false
	hasListId := false
	for index, argument := range commandArguments {
		if argument == "--title" && index+1 < len(commandArguments) && commandArguments[index+1] == "Buy groceries" {
			hasTitle = true
		}
		if argument == "--notes" && index+1 < len(commandArguments) {
			hasNotes = true
		}
		if argument == "--due" && index+1 < len(commandArguments) {
			hasDue = true
		}
		if argument == "mylist123" {
			hasListId = true
		}
	}
	if !hasTitle {
		t.Error("expected --title in arguments")
	}
	if !hasNotes {
		t.Error("expected --notes in arguments")
	}
	if !hasDue {
		t.Error("expected --due in arguments")
	}
	if !hasListId {
		t.Errorf("expected task list ID as positional argument: %v", commandArguments)
	}
}

func TestTasksTool_CompleteAction(t *testing.T) {
	runner, calls := mockRunner(`{"status":"completed"}`, nil)
	tool := &tasksTool{binary: "gog", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":    "complete",
		"task_list": "mylist123",
		"task_id":   "task123",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	commandArguments := (*calls)[0]
	foundComplete := false
	foundListId := false
	foundTaskId := false
	for index, argument := range commandArguments {
		if argument == "tasks" && index+1 < len(commandArguments) && commandArguments[index+1] == "complete" {
			foundComplete = true
		}
		if argument == "mylist123" {
			foundListId = true
		}
		if argument == "task123" {
			foundTaskId = true
		}
	}
	if !foundComplete {
		t.Errorf("expected 'tasks complete' in arguments: %v", commandArguments)
	}
	if !foundListId {
		t.Errorf("expected task list ID in arguments: %v", commandArguments)
	}
	if !foundTaskId {
		t.Errorf("expected task ID in arguments: %v", commandArguments)
	}
}

func TestTasksTool_DeleteAction(t *testing.T) {
	runner, calls := mockRunner(`{"status":"deleted"}`, nil)
	tool := &tasksTool{binary: "gog", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":    "delete",
		"task_list": "mylist123",
		"task_id":   "task456",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	commandArguments := (*calls)[0]
	foundDelete := false
	foundListId := false
	foundTaskId := false
	for index, argument := range commandArguments {
		if argument == "tasks" && index+1 < len(commandArguments) && commandArguments[index+1] == "delete" {
			foundDelete = true
		}
		if argument == "mylist123" {
			foundListId = true
		}
		if argument == "task456" {
			foundTaskId = true
		}
	}
	if !foundDelete {
		t.Errorf("expected 'tasks delete' in arguments: %v", commandArguments)
	}
	if !foundListId {
		t.Errorf("expected task list ID in arguments: %v", commandArguments)
	}
	if !foundTaskId {
		t.Errorf("expected task ID in arguments: %v", commandArguments)
	}
}

func TestTasksTool_MissingTaskList(t *testing.T) {
	runner, _ := mockRunner("", nil)
	tool := &tasksTool{binary: "gog", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "list",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "task_list is required") {
		t.Errorf("expected 'task_list is required' error, got: %v", err)
	}
}

func TestDriveTool_ListAction(t *testing.T) {
	runner, calls := mockRunner(`[{"name":"doc.pdf"}]`, nil)
	tool := &driveTool{binary: "gog", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "list",
		"limit":  5,
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	commandArguments := (*calls)[0]
	foundLs := false
	foundMax := false
	for index, argument := range commandArguments {
		if argument == "drive" && index+1 < len(commandArguments) && commandArguments[index+1] == "ls" {
			foundLs = true
		}
		if argument == "--max" && index+1 < len(commandArguments) && commandArguments[index+1] == "5" {
			foundMax = true
		}
	}
	if !foundLs {
		t.Errorf("expected 'drive ls' in arguments: %v", commandArguments)
	}
	if !foundMax {
		t.Errorf("expected '--max 5' in arguments: %v", commandArguments)
	}
}

func TestDriveTool_SearchAction(t *testing.T) {
	runner, calls := mockRunner(`[{"name":"doc.pdf"}]`, nil)
	tool := &driveTool{binary: "gog", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "search",
		"query":  "budget 2025",
		"limit":  10,
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	commandArguments := (*calls)[0]
	foundSearch := false
	foundQuery := false
	foundMax := false
	for index, argument := range commandArguments {
		if argument == "drive" && index+1 < len(commandArguments) && commandArguments[index+1] == "search" {
			foundSearch = true
		}
		if argument == "budget 2025" {
			foundQuery = true
		}
		if argument == "--max" && index+1 < len(commandArguments) && commandArguments[index+1] == "10" {
			foundMax = true
		}
		if argument == "--query" {
			t.Errorf("should not use --query flag (query is positional): %v", commandArguments)
		}
	}
	if !foundSearch {
		t.Errorf("expected 'drive search' in arguments: %v", commandArguments)
	}
	if !foundQuery {
		t.Errorf("expected query as positional argument: %v", commandArguments)
	}
	if !foundMax {
		t.Errorf("expected '--max 10' in arguments: %v", commandArguments)
	}
}

func TestDriveTool_InfoAction(t *testing.T) {
	runner, calls := mockRunner(`{"name":"doc.pdf","mimeType":"application/pdf"}`, nil)
	tool := &driveTool{binary: "gog", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":  "info",
		"file_id": "file123",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	commandArguments := (*calls)[0]
	foundGet := false
	foundFileId := false
	for index, argument := range commandArguments {
		if argument == "drive" && index+1 < len(commandArguments) && commandArguments[index+1] == "get" {
			foundGet = true
		}
		if argument == "file123" {
			foundFileId = true
		}
	}
	if !foundGet {
		t.Errorf("expected 'drive get' in arguments: %v", commandArguments)
	}
	if !foundFileId {
		t.Errorf("expected file ID in arguments: %v", commandArguments)
	}
}

func TestContactsTool_SearchAction(t *testing.T) {
	runner, calls := mockRunner(`[{"name":"Alice"}]`, nil)
	tool := &contactsTool{binary: "gog", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "search",
		"query":  "Alice",
		"limit":  5,
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	commandArguments := (*calls)[0]
	foundSearch := false
	foundQuery := false
	foundMax := false
	for index, argument := range commandArguments {
		if argument == "contacts" && index+1 < len(commandArguments) && commandArguments[index+1] == "search" {
			foundSearch = true
		}
		if argument == "Alice" {
			foundQuery = true
		}
		if argument == "--max" && index+1 < len(commandArguments) && commandArguments[index+1] == "5" {
			foundMax = true
		}
		if argument == "--query" {
			t.Errorf("should not use --query flag (query is positional): %v", commandArguments)
		}
	}
	if !foundSearch {
		t.Errorf("expected 'contacts search' in arguments: %v", commandArguments)
	}
	if !foundQuery {
		t.Errorf("expected query as positional argument: %v", commandArguments)
	}
	if !foundMax {
		t.Errorf("expected '--max 5' in arguments: %v", commandArguments)
	}
}

func TestContactsTool_ListAction(t *testing.T) {
	runner, calls := mockRunner(`[{"name":"Alice"},{"name":"Bob"}]`, nil)
	tool := &contactsTool{binary: "gog", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "list",
		"limit":  20,
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	commandArguments := (*calls)[0]
	foundList := false
	foundMax := false
	for index, argument := range commandArguments {
		if argument == "contacts" && index+1 < len(commandArguments) && commandArguments[index+1] == "list" {
			foundList = true
		}
		if argument == "--max" && index+1 < len(commandArguments) && commandArguments[index+1] == "20" {
			foundMax = true
		}
		if argument == "--limit" {
			t.Errorf("should not use --limit flag (use --max): %v", commandArguments)
		}
	}
	if !foundList {
		t.Errorf("expected 'contacts list' in arguments: %v", commandArguments)
	}
	if !foundMax {
		t.Errorf("expected '--max 20' in arguments: %v", commandArguments)
	}
}

func TestToolDefinitions(t *testing.T) {
	tools := []struct {
		name string
		tool interface {
			Definition() providers.ToolDefinition
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
			parameters, ok := def.Function.Parameters.(map[string]interface{})
			if !ok {
				t.Fatal("parameters should be a map")
			}
			props, ok := parameters["properties"].(map[string]interface{})
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
		message string
		want    bool
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
		t.Run(tt.message, func(t *testing.T) {
			got := isAuthError(tt.message)
			if got != tt.want {
				t.Errorf("isAuthError(%q) = %v, want %v", tt.message, got, tt.want)
			}
		})
	}
}
