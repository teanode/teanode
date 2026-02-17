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
	found := false
	for _, arg := range cmdArgs {
		if arg == "msg123" {
			found = true
			break
		}
	}
	if !found {
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

func TestTasksTool_CreateAction(t *testing.T) {
	runner, calls := mockRunner(`{"status":"created"}`, nil)
	tool := &tasksTool{binary: "gog", runner: runner}

	args, _ := json.Marshal(map[string]interface{}{
		"action": "create",
		"title":  "Buy groceries",
		"notes":  "Milk, bread, eggs",
		"due":    "2025-01-20",
	})
	_, err := tool.Execute(context.Background(), string(args))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmdArgs := (*calls)[0]
	hasTitle := false
	hasNotes := false
	hasDue := false
	for i, arg := range cmdArgs {
		if arg == "--title" && i+1 < len(cmdArgs) && cmdArgs[i+1] == "Buy groceries" {
			hasTitle = true
		}
		if arg == "--notes" && i+1 < len(cmdArgs) {
			hasNotes = true
		}
		if arg == "--due" && i+1 < len(cmdArgs) {
			hasDue = true
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
}

func TestTasksTool_CompleteAction(t *testing.T) {
	runner, calls := mockRunner(`{"status":"completed"}`, nil)
	tool := &tasksTool{binary: "gog", runner: runner}

	args, _ := json.Marshal(map[string]interface{}{
		"action":  "complete",
		"task_id": "task123",
	})
	_, err := tool.Execute(context.Background(), string(args))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmdArgs := (*calls)[0]
	found := false
	for _, arg := range cmdArgs {
		if arg == "task123" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected task ID in args: %v", cmdArgs)
	}
}

func TestDriveTool_SearchAction(t *testing.T) {
	runner, calls := mockRunner(`[{"name":"doc.pdf"}]`, nil)
	tool := &driveTool{binary: "gog", runner: runner}

	args, _ := json.Marshal(map[string]interface{}{
		"action": "search",
		"query":  "budget 2025",
	})
	_, err := tool.Execute(context.Background(), string(args))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmdArgs := (*calls)[0]
	found := false
	for i, arg := range cmdArgs {
		if arg == "drive" && i+1 < len(cmdArgs) && cmdArgs[i+1] == "search" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'drive search' in args: %v", cmdArgs)
	}
}

func TestContactsTool_SearchAction(t *testing.T) {
	runner, calls := mockRunner(`[{"name":"Alice"}]`, nil)
	tool := &contactsTool{binary: "gog", runner: runner}

	args, _ := json.Marshal(map[string]interface{}{
		"action": "search",
		"query":  "Alice",
	})
	_, err := tool.Execute(context.Background(), string(args))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmdArgs := (*calls)[0]
	found := false
	for i, arg := range cmdArgs {
		if arg == "contacts" && i+1 < len(cmdArgs) && cmdArgs[i+1] == "search" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'contacts search' in args: %v", cmdArgs)
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
