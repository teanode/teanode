package gitlab

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

// --- exec tests ---

func TestExecGitLab_ArgsAssembly(t *testing.T) {
	runner, calls := mockRunner(`[{"iid":1}]`, nil)

	result, err := execGitLab(context.Background(), runner, "/usr/bin/glab", "issue", "list", "--output", "json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != `[{"iid":1}]` {
		t.Errorf("unexpected result: %s", result)
	}
	if len(*calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(*calls))
	}

	arguments := (*calls)[0]
	expected := []string{"/usr/bin/glab", "issue", "list", "--output", "json"}
	if len(arguments) != len(expected) {
		t.Fatalf("expected %d args, got %d: %v", len(expected), len(arguments), arguments)
	}
	for index, want := range expected {
		if arguments[index] != want {
			t.Errorf("arg[%d] = %q, want %q", index, arguments[index], want)
		}
	}
}

func TestExecGitLab_OutputTruncation(t *testing.T) {
	bigOutput := strings.Repeat("x", maxOutputBytes+1000)
	runner, _ := mockRunner(bigOutput, nil)

	result, err := execGitLab(context.Background(), runner, "glab", "issue", "list")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasSuffix(result, "\n... (output truncated)") {
		t.Error("expected truncation suffix")
	}
	if !strings.HasPrefix(result, strings.Repeat("x", 100)) {
		t.Error("expected content before truncation")
	}
}

func TestExecGitLab_AuthError(t *testing.T) {
	runner, _ := mockRunner("", fmt.Errorf("none of the git remotes configured for this repository point to a known GitLab host. To tell glab about a new GitLab host, please use glab auth login"))

	_, err := execGitLab(context.Background(), runner, "glab", "issue", "list")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "authentication required") {
		t.Errorf("expected auth error message, got: %v", err)
	}
}

func TestExecGitLab_GenericError(t *testing.T) {
	runner, _ := mockRunner("", fmt.Errorf("some random error"))

	_, err := execGitLab(context.Background(), runner, "glab", "issue", "list")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "glab command failed") {
		t.Errorf("expected generic error message, got: %v", err)
	}
}

// --- isAuthError tests ---

func TestIsAuthError(t *testing.T) {
	tests := []struct {
		message string
		want    bool
	}{
		{"not logged into any gitlab hosts", true},
		{"please use glab auth login", true},
		{"401 Unauthorized", true},
		{"Invalid token provided", true},
		{"token was revoked", true},
		{"none of the git remotes configured for this repo", true},
		{"file not found", false},
		{"network timeout", false},
		{"", false},
	}
	for _, testCase := range tests {
		t.Run(testCase.message, func(t *testing.T) {
			got := isAuthError(testCase.message)
			if got != testCase.want {
				t.Errorf("isAuthError(%q) = %v, want %v", testCase.message, got, testCase.want)
			}
		})
	}
}

// --- issues tool tests ---

func TestIssuesTool_ListAction(t *testing.T) {
	runner, calls := mockRunner(`[{"iid":1,"title":"Bug"}]`, nil)
	tool := &issuesTool{binary: "glab", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":   "list",
		"per_page": 10,
		"state":    "opened",
	})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result")
	}

	commandArgs := (*calls)[0]
	foundIssueList := false
	foundOutputJSON := false
	for index, argument := range commandArgs {
		if argument == "issue" && index+1 < len(commandArgs) && commandArgs[index+1] == "list" {
			foundIssueList = true
		}
		if argument == "--output" && index+1 < len(commandArgs) && commandArgs[index+1] == "json" {
			foundOutputJSON = true
		}
		if argument == "--state" {
			t.Errorf("unexpected --state flag in args (glab uses boolean flags): %v", commandArgs)
		}
	}
	if !foundIssueList {
		t.Errorf("expected 'issue list' in args: %v", commandArgs)
	}
	if !foundOutputJSON {
		t.Errorf("expected '--output json' in args: %v", commandArgs)
	}
}

func TestIssuesTool_ListClosedState(t *testing.T) {
	runner, calls := mockRunner(`[{"iid":2,"title":"Closed Bug"}]`, nil)
	tool := &issuesTool{binary: "glab", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "list",
		"state":  "closed",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	commandArgs := (*calls)[0]
	foundClosed := false
	for _, argument := range commandArgs {
		if argument == "--closed" {
			foundClosed = true
		}
	}
	if !foundClosed {
		t.Errorf("expected '--closed' in args: %v", commandArgs)
	}
}

func TestIssuesTool_ListAllState(t *testing.T) {
	runner, calls := mockRunner(`[{"iid":3}]`, nil)
	tool := &issuesTool{binary: "glab", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "list",
		"state":  "all",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	commandArgs := (*calls)[0]
	foundAll := false
	for _, argument := range commandArgs {
		if argument == "--all" {
			foundAll = true
		}
	}
	if !foundAll {
		t.Errorf("expected '--all' in args: %v", commandArgs)
	}
}

func TestIssuesTool_ViewAction(t *testing.T) {
	runner, calls := mockRunner(`{"iid":42,"title":"Fix bug","description":"Details"}`, nil)
	tool := &issuesTool{binary: "glab", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "view",
		"number": 42,
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	commandArgs := (*calls)[0]
	foundNumber := false
	foundComments := false
	for _, argument := range commandArgs {
		if argument == "42" {
			foundNumber = true
		}
		if argument == "--comments" {
			foundComments = true
		}
	}
	if !foundNumber {
		t.Errorf("expected issue number in args: %v", commandArgs)
	}
	if !foundComments {
		t.Errorf("expected --comments in args: %v", commandArgs)
	}
}

func TestIssuesTool_CreateAction(t *testing.T) {
	runner, calls := mockRunner("https://gitlab.com/owner/repo/-/issues/1", nil)
	tool := &issuesTool{binary: "glab", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":      "create",
		"title":       "New Bug",
		"description": "Bug description",
		"labels":      "bug",
	})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, `"status":"created"`) {
		t.Errorf("expected created status in result: %s", result)
	}

	commandArgs := (*calls)[0]
	hasTitle := false
	hasDescription := false
	hasLabel := false
	for index, argument := range commandArgs {
		if argument == "--title" && index+1 < len(commandArgs) && commandArgs[index+1] == "New Bug" {
			hasTitle = true
		}
		if argument == "--description" && index+1 < len(commandArgs) && commandArgs[index+1] == "Bug description" {
			hasDescription = true
		}
		if argument == "--label" && index+1 < len(commandArgs) && commandArgs[index+1] == "bug" {
			hasLabel = true
		}
	}
	if !hasTitle {
		t.Error("expected --title in args")
	}
	if !hasDescription {
		t.Error("expected --description in args")
	}
	if !hasLabel {
		t.Error("expected --label in args")
	}
}

func TestIssuesTool_CreateMissingFields(t *testing.T) {
	runner, _ := mockRunner("", nil)
	tool := &issuesTool{binary: "glab", runner: runner}

	// Missing description.
	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "create",
		"title":  "Test",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "description is required") {
		t.Errorf("expected 'description is required' error, got: %v", err)
	}
}

func TestIssuesTool_CommentAction(t *testing.T) {
	runner, calls := mockRunner("Comment added", nil)
	tool := &issuesTool{binary: "glab", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":      "comment",
		"number":      5,
		"description": "This is a comment",
	})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, `"status":"commented"`) {
		t.Errorf("expected commented status in result: %s", result)
	}

	commandArgs := (*calls)[0]
	foundNote := false
	foundMessage := false
	for index, argument := range commandArgs {
		if argument == "note" {
			foundNote = true
		}
		if argument == "--message" && index+1 < len(commandArgs) && commandArgs[index+1] == "This is a comment" {
			foundMessage = true
		}
	}
	if !foundNote {
		t.Errorf("expected 'note' in args: %v", commandArgs)
	}
	if !foundMessage {
		t.Errorf("expected '--message' in args: %v", commandArgs)
	}
}

func TestIssuesTool_CloseAction(t *testing.T) {
	runner, calls := mockRunner("Closed issue #5", nil)
	tool := &issuesTool{binary: "glab", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "close",
		"number": 5,
	})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, `"status":"closed"`) {
		t.Errorf("expected closed status in result: %s", result)
	}

	commandArgs := (*calls)[0]
	foundClose := false
	for index, argument := range commandArgs {
		if argument == "issue" && index+1 < len(commandArgs) && commandArgs[index+1] == "close" {
			foundClose = true
			break
		}
	}
	if !foundClose {
		t.Errorf("expected 'issue close' in args: %v", commandArgs)
	}
}

func TestIssuesTool_EditAction(t *testing.T) {
	runner, calls := mockRunner("Issue updated", nil)
	tool := &issuesTool{binary: "glab", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "edit",
		"number": 3,
		"title":  "Updated title",
		"labels": "enhancement",
	})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, `"status":"edited"`) {
		t.Errorf("expected edited status in result: %s", result)
	}

	commandArgs := (*calls)[0]
	foundUpdate := false
	foundLabel := false
	for index, argument := range commandArgs {
		if argument == "issue" && index+1 < len(commandArgs) && commandArgs[index+1] == "update" {
			foundUpdate = true
		}
		if argument == "--label" && index+1 < len(commandArgs) && commandArgs[index+1] == "enhancement" {
			foundLabel = true
		}
	}
	if !foundUpdate {
		t.Errorf("expected 'issue update' in args: %v", commandArgs)
	}
	if !foundLabel {
		t.Errorf("expected '--label' in args: %v", commandArgs)
	}
}

func TestIssuesTool_EditAssigneesAndUnlabel(t *testing.T) {
	runner, calls := mockRunner("Issue updated", nil)
	tool := &issuesTool{binary: "glab", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":    "edit",
		"number":    7,
		"assignees": "alice,bob",
		"unlabel":   "wontfix",
		"milestone": "v2.0",
	})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, `"status":"edited"`) {
		t.Errorf("expected edited status in result: %s", result)
	}

	commandArgs := (*calls)[0]
	foundAssignee := false
	foundUnlabel := false
	foundMilestone := false
	for index, argument := range commandArgs {
		if argument == "--assignee" && index+1 < len(commandArgs) && commandArgs[index+1] == "alice,bob" {
			foundAssignee = true
		}
		if argument == "--unlabel" && index+1 < len(commandArgs) && commandArgs[index+1] == "wontfix" {
			foundUnlabel = true
		}
		if argument == "--milestone" && index+1 < len(commandArgs) && commandArgs[index+1] == "v2.0" {
			foundMilestone = true
		}
	}
	if !foundAssignee {
		t.Errorf("expected '--assignee' in args: %v", commandArgs)
	}
	if !foundUnlabel {
		t.Errorf("expected '--unlabel' in args: %v", commandArgs)
	}
	if !foundMilestone {
		t.Errorf("expected '--milestone' in args: %v", commandArgs)
	}
}

func TestIssuesTool_EditUnassignAll(t *testing.T) {
	runner, calls := mockRunner("Issue updated", nil)
	tool := &issuesTool{binary: "glab", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":   "edit",
		"number":   9,
		"unassign": true,
	})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, `"status":"edited"`) {
		t.Errorf("expected edited status in result: %s", result)
	}

	commandArgs := (*calls)[0]
	foundUnassign := false
	for _, argument := range commandArgs {
		if argument == "--unassign" {
			foundUnassign = true
		}
	}
	if !foundUnassign {
		t.Errorf("expected '--unassign' in args: %v", commandArgs)
	}
}

func TestIssuesTool_EditReassign(t *testing.T) {
	runner, calls := mockRunner("Issue updated", nil)
	tool := &issuesTool{binary: "glab", runner: runner}

	// To reassign: use assignee prefix notation to replace assignees.
	arguments, _ := json.Marshal(map[string]interface{}{
		"action":    "edit",
		"number":    9,
		"assignees": "bob",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	commandArgs := (*calls)[0]
	foundAssignee := false
	for index, argument := range commandArgs {
		if argument == "--assignee" && index+1 < len(commandArgs) && commandArgs[index+1] == "bob" {
			foundAssignee = true
		}
	}
	if !foundAssignee {
		t.Errorf("expected '--assignee bob' in args: %v", commandArgs)
	}
}

func TestIssuesTool_EditDueDateAndConfidential(t *testing.T) {
	runner, calls := mockRunner("Issue updated", nil)
	tool := &issuesTool{binary: "glab", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":       "edit",
		"number":       4,
		"due_date":     "2026-03-01",
		"confidential": true,
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	commandArgs := (*calls)[0]
	foundDueDate := false
	foundConfidential := false
	for index, argument := range commandArgs {
		if argument == "--due-date" && index+1 < len(commandArgs) && commandArgs[index+1] == "2026-03-01" {
			foundDueDate = true
		}
		if argument == "--confidential" {
			foundConfidential = true
		}
	}
	if !foundDueDate {
		t.Errorf("expected '--due-date' in args: %v", commandArgs)
	}
	if !foundConfidential {
		t.Errorf("expected '--confidential' in args: %v", commandArgs)
	}
}

func TestIssuesTool_EditMakePublic(t *testing.T) {
	runner, calls := mockRunner("Issue updated", nil)
	tool := &issuesTool{binary: "glab", runner: runner}

	confidential := false
	arguments, _ := json.Marshal(map[string]interface{}{
		"action":       "edit",
		"number":       4,
		"confidential": confidential,
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	commandArgs := (*calls)[0]
	foundPublic := false
	for _, argument := range commandArgs {
		if argument == "--public" {
			foundPublic = true
		}
	}
	if !foundPublic {
		t.Errorf("expected '--public' in args: %v", commandArgs)
	}
}

func TestIssuesTool_EditNoFields(t *testing.T) {
	runner, _ := mockRunner("", nil)
	tool := &issuesTool{binary: "glab", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "edit",
		"number": 5,
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "at least one field to update is required") {
		t.Errorf("expected validation error, got: %v", err)
	}
}

func TestIssuesTool_ListAssigneeMe(t *testing.T) {
	runner, calls := mockRunner(`[{"iid":5}]`, nil)
	tool := &issuesTool{binary: "glab", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":   "list",
		"assignee": "@me",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	commandArgs := (*calls)[0]
	foundAssignee := false
	for index, argument := range commandArgs {
		if argument == "--assignee" && index+1 < len(commandArgs) && commandArgs[index+1] == "@me" {
			foundAssignee = true
		}
	}
	if !foundAssignee {
		t.Errorf("expected '--assignee @me' in args: %v", commandArgs)
	}
}

func TestIssuesTool_ListAuthor(t *testing.T) {
	runner, calls := mockRunner(`[{"iid":6}]`, nil)
	tool := &issuesTool{binary: "glab", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "list",
		"author": "@me",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	commandArgs := (*calls)[0]
	foundAuthor := false
	for index, argument := range commandArgs {
		if argument == "--author" && index+1 < len(commandArgs) && commandArgs[index+1] == "@me" {
			foundAuthor = true
		}
	}
	if !foundAuthor {
		t.Errorf("expected '--author @me' in args: %v", commandArgs)
	}
}

func TestIssuesTool_RepositoryOverride(t *testing.T) {
	runner, calls := mockRunner(`[{"iid":1}]`, nil)
	tool := &issuesTool{binary: "glab", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":     "list",
		"repository": "mygroup/myproject",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	commandArgs := (*calls)[0]
	foundRepo := false
	for index, argument := range commandArgs {
		if argument == "-R" && index+1 < len(commandArgs) && commandArgs[index+1] == "mygroup/myproject" {
			foundRepo = true
			break
		}
	}
	if !foundRepo {
		t.Errorf("expected '-R mygroup/myproject' in args: %v", commandArgs)
	}
}

func TestIssuesTool_UnknownAction(t *testing.T) {
	runner, _ := mockRunner("", nil)
	tool := &issuesTool{binary: "glab", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "unknown",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "unknown issues action") {
		t.Errorf("expected unknown action error, got: %v", err)
	}
}

// --- merge requests tool tests ---

func TestMergeRequestsTool_ListAction(t *testing.T) {
	runner, calls := mockRunner(`[{"iid":10,"title":"Feature"}]`, nil)
	tool := &mergeRequestsTool{binary: "glab", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":   "list",
		"per_page": 5,
	})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result")
	}

	commandArgs := (*calls)[0]
	foundMRList := false
	for index, argument := range commandArgs {
		if argument == "mr" && index+1 < len(commandArgs) && commandArgs[index+1] == "list" {
			foundMRList = true
			break
		}
	}
	if !foundMRList {
		t.Errorf("expected 'mr list' in args: %v", commandArgs)
	}
}

func TestMergeRequestsTool_ListClosedState(t *testing.T) {
	runner, calls := mockRunner(`[{"iid":11}]`, nil)
	tool := &mergeRequestsTool{binary: "glab", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "list",
		"state":  "closed",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	commandArgs := (*calls)[0]
	foundClosed := false
	for _, argument := range commandArgs {
		if argument == "--closed" {
			foundClosed = true
		}
	}
	if !foundClosed {
		t.Errorf("expected '--closed' in args: %v", commandArgs)
	}
}

func TestMergeRequestsTool_ListMergedState(t *testing.T) {
	runner, calls := mockRunner(`[{"iid":12}]`, nil)
	tool := &mergeRequestsTool{binary: "glab", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "list",
		"state":  "merged",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	commandArgs := (*calls)[0]
	foundMerged := false
	for _, argument := range commandArgs {
		if argument == "--merged" {
			foundMerged = true
		}
	}
	if !foundMerged {
		t.Errorf("expected '--merged' in args: %v", commandArgs)
	}
}

func TestMergeRequestsTool_ListAllState(t *testing.T) {
	runner, calls := mockRunner(`[{"iid":13}]`, nil)
	tool := &mergeRequestsTool{binary: "glab", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "list",
		"state":  "all",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	commandArgs := (*calls)[0]
	foundAll := false
	for _, argument := range commandArgs {
		if argument == "--all" {
			foundAll = true
		}
	}
	if !foundAll {
		t.Errorf("expected '--all' in args: %v", commandArgs)
	}
}

func TestMergeRequestsTool_ListAssigneeMe(t *testing.T) {
	runner, calls := mockRunner(`[{"iid":14}]`, nil)
	tool := &mergeRequestsTool{binary: "glab", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":   "list",
		"assignee": "@me",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	commandArgs := (*calls)[0]
	foundAssignee := false
	for index, argument := range commandArgs {
		if argument == "--assignee" && index+1 < len(commandArgs) && commandArgs[index+1] == "@me" {
			foundAssignee = true
		}
	}
	if !foundAssignee {
		t.Errorf("expected '--assignee @me' in args: %v", commandArgs)
	}
}

func TestMergeRequestsTool_ListReviewerMe(t *testing.T) {
	runner, calls := mockRunner(`[{"iid":15}]`, nil)
	tool := &mergeRequestsTool{binary: "glab", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":   "list",
		"reviewer": "@me",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	commandArgs := (*calls)[0]
	foundReviewer := false
	for index, argument := range commandArgs {
		if argument == "--reviewer" && index+1 < len(commandArgs) && commandArgs[index+1] == "@me" {
			foundReviewer = true
		}
	}
	if !foundReviewer {
		t.Errorf("expected '--reviewer @me' in args: %v", commandArgs)
	}
}

func TestMergeRequestsTool_ListAuthor(t *testing.T) {
	runner, calls := mockRunner(`[{"iid":16}]`, nil)
	tool := &mergeRequestsTool{binary: "glab", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "list",
		"author": "@me",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	commandArgs := (*calls)[0]
	foundAuthor := false
	for index, argument := range commandArgs {
		if argument == "--author" && index+1 < len(commandArgs) && commandArgs[index+1] == "@me" {
			foundAuthor = true
		}
	}
	if !foundAuthor {
		t.Errorf("expected '--author @me' in args: %v", commandArgs)
	}
}

func TestMergeRequestsTool_ViewAction(t *testing.T) {
	runner, calls := mockRunner(`{"iid":42,"title":"Fix"}`, nil)
	tool := &mergeRequestsTool{binary: "glab", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "view",
		"number": 42,
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	commandArgs := (*calls)[0]
	foundComments := false
	for _, argument := range commandArgs {
		if argument == "--comments" {
			foundComments = true
		}
	}
	if !foundComments {
		t.Errorf("expected --comments in args: %v", commandArgs)
	}
}

func TestMergeRequestsTool_CreateAction(t *testing.T) {
	runner, calls := mockRunner("https://gitlab.com/owner/repo/-/merge_requests/1", nil)
	tool := &mergeRequestsTool{binary: "glab", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":        "create",
		"title":         "New Feature",
		"description":   "Description",
		"source_branch": "feature-branch",
		"target_branch": "main",
	})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, `"status":"created"`) {
		t.Errorf("expected created status in result: %s", result)
	}

	commandArgs := (*calls)[0]
	hasSourceBranch := false
	hasTargetBranch := false
	for index, argument := range commandArgs {
		if argument == "--source-branch" && index+1 < len(commandArgs) && commandArgs[index+1] == "feature-branch" {
			hasSourceBranch = true
		}
		if argument == "--target-branch" && index+1 < len(commandArgs) && commandArgs[index+1] == "main" {
			hasTargetBranch = true
		}
	}
	if !hasSourceBranch {
		t.Error("expected --source-branch in args")
	}
	if !hasTargetBranch {
		t.Error("expected --target-branch in args")
	}
}

func TestMergeRequestsTool_CreateMissingSourceBranch(t *testing.T) {
	runner, _ := mockRunner("", nil)
	tool := &mergeRequestsTool{binary: "glab", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":      "create",
		"title":       "Test",
		"description": "Test description",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "source_branch is required") {
		t.Errorf("expected 'source_branch is required' error, got: %v", err)
	}
}

func TestMergeRequestsTool_MergeAction(t *testing.T) {
	runner, calls := mockRunner("Merged MR !10", nil)
	tool := &mergeRequestsTool{binary: "glab", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "merge",
		"number": 10,
		"squash": true,
	})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, `"status":"merged"`) {
		t.Errorf("expected merged status in result: %s", result)
	}

	commandArgs := (*calls)[0]
	foundSquash := false
	for _, argument := range commandArgs {
		if argument == "--squash" {
			foundSquash = true
			break
		}
	}
	if !foundSquash {
		t.Errorf("expected '--squash' in args: %v", commandArgs)
	}
}

func TestMergeRequestsTool_DiffAction(t *testing.T) {
	runner, _ := mockRunner("diff --git a/file.go b/file.go\n+added line", nil)
	tool := &mergeRequestsTool{binary: "glab", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "diff",
		"number": 5,
	})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, `"diff"`) {
		t.Errorf("expected diff key in JSON result: %s", result)
	}
}

func TestMergeRequestsTool_ApproveAction(t *testing.T) {
	runner, calls := mockRunner("Approved MR !7", nil)
	tool := &mergeRequestsTool{binary: "glab", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "approve",
		"number": 7,
	})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, `"status":"approved"`) {
		t.Errorf("expected approved status in result: %s", result)
	}

	commandArgs := (*calls)[0]
	foundApprove := false
	for index, argument := range commandArgs {
		if argument == "mr" && index+1 < len(commandArgs) && commandArgs[index+1] == "approve" {
			foundApprove = true
			break
		}
	}
	if !foundApprove {
		t.Errorf("expected 'mr approve' in args: %v", commandArgs)
	}
}

func TestMergeRequestsTool_CommentAction(t *testing.T) {
	runner, calls := mockRunner("Note added", nil)
	tool := &mergeRequestsTool{binary: "glab", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":      "comment",
		"number":      3,
		"description": "LGTM",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	commandArgs := (*calls)[0]
	foundNote := false
	foundMessage := false
	for index, argument := range commandArgs {
		if argument == "note" {
			foundNote = true
		}
		if argument == "--message" && index+1 < len(commandArgs) && commandArgs[index+1] == "LGTM" {
			foundMessage = true
		}
	}
	if !foundNote {
		t.Errorf("expected 'note' in args: %v", commandArgs)
	}
	if !foundMessage {
		t.Errorf("expected '--message' in args: %v", commandArgs)
	}
}

func TestMergeRequestsTool_UnknownAction(t *testing.T) {
	runner, _ := mockRunner("", nil)
	tool := &mergeRequestsTool{binary: "glab", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "unknown",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "unknown merge_requests action") {
		t.Errorf("expected unknown action error, got: %v", err)
	}
}

// --- projects tool tests ---

func TestProjectsTool_ViewAction(t *testing.T) {
	runner, calls := mockRunner(`{"name":"myproject","description":"A project"}`, nil)
	tool := &projectsTool{binary: "glab", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":     "view",
		"repository": "mygroup/myproject",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	commandArgs := (*calls)[0]
	foundRepoView := false
	for index, argument := range commandArgs {
		if argument == "repo" && index+1 < len(commandArgs) && commandArgs[index+1] == "view" {
			foundRepoView = true
			break
		}
	}
	if !foundRepoView {
		t.Errorf("expected 'repo view' in args: %v", commandArgs)
	}
}

func TestProjectsTool_ListAction(t *testing.T) {
	runner, calls := mockRunner(`[{"name":"project1"}]`, nil)
	tool := &projectsTool{binary: "glab", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":   "list",
		"per_page": 5,
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	commandArgs := (*calls)[0]
	foundRepoList := false
	for index, argument := range commandArgs {
		if argument == "repo" && index+1 < len(commandArgs) && commandArgs[index+1] == "list" {
			foundRepoList = true
			break
		}
	}
	if !foundRepoList {
		t.Errorf("expected 'repo list' in args: %v", commandArgs)
	}
}

func TestProjectsTool_SearchAction(t *testing.T) {
	runner, calls := mockRunner(`[{"name":"found-project"}]`, nil)
	tool := &projectsTool{binary: "glab", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "search",
		"query":  "kubernetes",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	commandArgs := (*calls)[0]
	foundRepoSearch := false
	foundSearchFlag := false
	for index, argument := range commandArgs {
		if argument == "repo" && index+1 < len(commandArgs) && commandArgs[index+1] == "search" {
			foundRepoSearch = true
		}
		if argument == "--search" && index+1 < len(commandArgs) && commandArgs[index+1] == "kubernetes" {
			foundSearchFlag = true
		}
	}
	if !foundRepoSearch {
		t.Errorf("expected 'repo search' in args: %v", commandArgs)
	}
	if !foundSearchFlag {
		t.Errorf("expected '--search kubernetes' in args: %v", commandArgs)
	}
}

func TestProjectsTool_SearchMissingQuery(t *testing.T) {
	runner, _ := mockRunner("", nil)
	tool := &projectsTool{binary: "glab", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "search",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "query is required") {
		t.Errorf("expected 'query is required' error, got: %v", err)
	}
}

func TestProjectsTool_UnknownAction(t *testing.T) {
	runner, _ := mockRunner("", nil)
	tool := &projectsTool{binary: "glab", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "unknown",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "unknown projects action") {
		t.Errorf("expected unknown action error, got: %v", err)
	}
}

// --- pipelines tool tests ---

func TestPipelinesTool_ListAction(t *testing.T) {
	runner, calls := mockRunner(`[{"id":1,"status":"success"}]`, nil)
	tool := &pipelinesTool{binary: "glab", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":   "list",
		"per_page": 10,
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	commandArgs := (*calls)[0]
	foundCIList := false
	for index, argument := range commandArgs {
		if argument == "ci" && index+1 < len(commandArgs) && commandArgs[index+1] == "list" {
			foundCIList = true
			break
		}
	}
	if !foundCIList {
		t.Errorf("expected 'ci list' in args: %v", commandArgs)
	}
}

func TestPipelinesTool_ViewAction(t *testing.T) {
	runner, calls := mockRunner(`{"id":123,"status":"success"}`, nil)
	tool := &pipelinesTool{binary: "glab", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":      "view",
		"pipeline_id": "123",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	commandArgs := (*calls)[0]
	foundCIGet := false
	foundPipelineID := false
	for index, argument := range commandArgs {
		if argument == "ci" && index+1 < len(commandArgs) && commandArgs[index+1] == "get" {
			foundCIGet = true
		}
		if argument == "--pipeline-id" && index+1 < len(commandArgs) && commandArgs[index+1] == "123" {
			foundPipelineID = true
		}
	}
	if !foundCIGet {
		t.Errorf("expected 'ci get' in args: %v", commandArgs)
	}
	if !foundPipelineID {
		t.Errorf("expected '--pipeline-id 123' in args: %v", commandArgs)
	}
}

func TestPipelinesTool_RunAction(t *testing.T) {
	runner, calls := mockRunner("Pipeline triggered", nil)
	tool := &pipelinesTool{binary: "glab", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "run",
		"branch": "main",
	})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, `"status":"triggered"`) {
		t.Errorf("expected triggered status in result: %s", result)
	}

	commandArgs := (*calls)[0]
	foundBranch := false
	for index, argument := range commandArgs {
		if argument == "--branch" && index+1 < len(commandArgs) && commandArgs[index+1] == "main" {
			foundBranch = true
			break
		}
	}
	if !foundBranch {
		t.Errorf("expected '--branch main' in args: %v", commandArgs)
	}
}

func TestPipelinesTool_ViewMissingID(t *testing.T) {
	runner, _ := mockRunner("", nil)
	tool := &pipelinesTool{binary: "glab", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "view",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "pipeline_id is required") {
		t.Errorf("expected 'pipeline_id is required' error, got: %v", err)
	}
}

func TestPipelinesTool_UnknownAction(t *testing.T) {
	runner, _ := mockRunner("", nil)
	tool := &pipelinesTool{binary: "glab", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "unknown",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "unknown pipelines action") {
		t.Errorf("expected unknown action error, got: %v", err)
	}
}

// --- releases tool tests ---

func TestReleasesTool_ListAction(t *testing.T) {
	runner, calls := mockRunner(`[{"tag_name":"v1.0.0","name":"Release 1"}]`, nil)
	tool := &releasesTool{binary: "glab", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "list",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	commandArgs := (*calls)[0]
	foundReleaseList := false
	for index, argument := range commandArgs {
		if argument == "release" && index+1 < len(commandArgs) && commandArgs[index+1] == "list" {
			foundReleaseList = true
			break
		}
	}
	if !foundReleaseList {
		t.Errorf("expected 'release list' in args: %v", commandArgs)
	}
}

func TestReleasesTool_CreateAction(t *testing.T) {
	runner, calls := mockRunner("https://gitlab.com/owner/repo/-/releases/v1.0.0", nil)
	tool := &releasesTool{binary: "glab", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "create",
		"tag":    "v1.0.0",
		"name":   "Release 1.0.0",
		"notes":  "First release",
	})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, `"status":"created"`) {
		t.Errorf("expected created status in result: %s", result)
	}

	commandArgs := (*calls)[0]
	hasName := false
	hasNotes := false
	for index, argument := range commandArgs {
		if argument == "--name" && index+1 < len(commandArgs) && commandArgs[index+1] == "Release 1.0.0" {
			hasName = true
		}
		if argument == "--notes" && index+1 < len(commandArgs) && commandArgs[index+1] == "First release" {
			hasNotes = true
		}
	}
	if !hasName {
		t.Error("expected --name in args")
	}
	if !hasNotes {
		t.Error("expected --notes in args")
	}
}

func TestReleasesTool_CreateMissingTag(t *testing.T) {
	runner, _ := mockRunner("", nil)
	tool := &releasesTool{binary: "glab", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "create",
		"name":   "Test",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "tag is required") {
		t.Errorf("expected 'tag is required' error, got: %v", err)
	}
}

func TestReleasesTool_CreateMissingName(t *testing.T) {
	runner, _ := mockRunner("", nil)
	tool := &releasesTool{binary: "glab", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "create",
		"tag":    "v1.0.0",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "name is required") {
		t.Errorf("expected 'name is required' error, got: %v", err)
	}
}

func TestReleasesTool_UnknownAction(t *testing.T) {
	runner, _ := mockRunner("", nil)
	tool := &releasesTool{binary: "glab", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "unknown",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "unknown releases action") {
		t.Errorf("expected unknown action error, got: %v", err)
	}
}

// --- tool definition tests ---

func TestToolDefinitions(t *testing.T) {
	tools := []struct {
		name string
		tool interface {
			Definition() provider.ToolDefinition
		}
	}{
		{"issues", &issuesTool{}},
		{"merge_requests", &mergeRequestsTool{}},
		{"projects", &projectsTool{}},
		{"pipelines", &pipelinesTool{}},
		{"releases", &releasesTool{}},
	}

	for _, testCase := range tools {
		t.Run(testCase.name, func(t *testing.T) {
			definition := testCase.tool.Definition()
			if definition.Type != "function" {
				t.Errorf("expected type 'function', got %q", definition.Type)
			}
			if !strings.HasPrefix(definition.Function.Name, "gitlab_") {
				t.Errorf("expected name to start with 'gitlab_', got %q", definition.Function.Name)
			}
			if definition.Function.Description == "" {
				t.Error("expected non-empty description")
			}
			if definition.Function.Parameters == nil {
				t.Error("expected non-nil parameters")
			}
			if definition.Function.Returns == nil {
				t.Error("expected non-nil returns")
			}

			// Verify action enum exists in parameters.
			params, ok := definition.Function.Parameters.(map[string]interface{})
			if !ok {
				t.Fatal("parameters should be a map")
			}
			properties, ok := params["properties"].(map[string]interface{})
			if !ok {
				t.Fatal("properties should be a map")
			}
			action, ok := properties["action"].(map[string]interface{})
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
