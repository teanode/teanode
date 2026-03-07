package github

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

func TestExecGitHub_ArgsAssembly(testing *testing.T) {
	runner, calls := mockRunner(`{"ok":true}`, nil)

	result, err := execGitHub(context.Background(), runner, "/usr/bin/gh", "issue", "list", "--json", "number,title")
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}
	if result != `{"ok":true}` {
		testing.Errorf("unexpected result: %s", result)
	}
	if len(*calls) != 1 {
		testing.Fatalf("expected 1 call, got %d", len(*calls))
	}

	arguments := (*calls)[0]
	expected := []string{"/usr/bin/gh", "issue", "list", "--json", "number,title"}
	if len(arguments) != len(expected) {
		testing.Fatalf("expected %d args, got %d: %v", len(expected), len(arguments), arguments)
	}
	for index, want := range expected {
		if arguments[index] != want {
			testing.Errorf("arg[%d] = %q, want %q", index, arguments[index], want)
		}
	}
}

func TestExecGitHub_OutputTruncation(testing *testing.T) {
	bigOutput := strings.Repeat("x", maxOutputBytes+1000)
	runner, _ := mockRunner(bigOutput, nil)

	result, err := execGitHub(context.Background(), runner, "gh", "issue", "list")
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasSuffix(result, "\n... (output truncated)") {
		testing.Error("expected truncation suffix")
	}
	if !strings.HasPrefix(result, strings.Repeat("x", 100)) {
		testing.Error("expected content before truncation")
	}
}

func TestExecGitHub_AuthError(testing *testing.T) {
	runner, _ := mockRunner("", fmt.Errorf("error: not logged into any github hosts. Run gh auth login"))

	_, err := execGitHub(context.Background(), runner, "gh", "issue", "list")
	if err == nil {
		testing.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "authentication required") {
		testing.Errorf("expected auth error message, got: %v", err)
	}
}

func TestExecGitHub_GenericError(testing *testing.T) {
	runner, _ := mockRunner("", fmt.Errorf("some random error"))

	_, err := execGitHub(context.Background(), runner, "gh", "issue", "list")
	if err == nil {
		testing.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "gh command failed") {
		testing.Errorf("expected generic error message, got: %v", err)
	}
}

// --- isAuthError tests ---

func TestIsAuthError(t *testing.T) {
	tests := []struct {
		message string
		want    bool
	}{
		{"not logged into any github hosts", true},
		{"authentication required", true},
		{"run gh auth login first", true},
		{"please try authenticating again", true},
		{"token expired", true},
		{"invalid token", true},
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

func TestIssuesTool_ListAction(testing *testing.T) {
	runner, calls := mockRunner(`[{"number":1,"title":"Bug"}]`, nil)
	tool := &issuesTool{binary: "gh", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "list",
		"limit":  10,
		"state":  "open",
	})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		testing.Error("expected non-empty result")
	}

	commandArgs := (*calls)[0]
	foundIssueList := false
	foundJSON := false
	foundState := false
	for index, argument := range commandArgs {
		if argument == "issue" && index+1 < len(commandArgs) && commandArgs[index+1] == "list" {
			foundIssueList = true
		}
		if argument == "--json" {
			foundJSON = true
		}
		if argument == "--state" && index+1 < len(commandArgs) && commandArgs[index+1] == "open" {
			foundState = true
		}
	}
	if !foundIssueList {
		testing.Errorf("expected 'issue list' in args: %v", commandArgs)
	}
	if !foundJSON {
		testing.Errorf("expected '--json' in args: %v", commandArgs)
	}
	if !foundState {
		testing.Errorf("expected '--state open' in args: %v", commandArgs)
	}
}

func TestIssuesTool_ViewAction(testing *testing.T) {
	runner, calls := mockRunner(`{"number":42,"title":"Fix bug","body":"Details"}`, nil)
	tool := &issuesTool{binary: "gh", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "view",
		"number": 42,
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}

	commandArgs := (*calls)[0]
	found := false
	for _, argument := range commandArgs {
		if argument == "42" {
			found = true
			break
		}
	}
	if !found {
		testing.Errorf("expected issue number in args: %v", commandArgs)
	}
}

func TestIssuesTool_CreateAction(testing *testing.T) {
	runner, calls := mockRunner("https://github.com/owner/repo/issues/1", nil)
	tool := &issuesTool{binary: "gh", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "create",
		"title":  "New Bug",
		"body":   "Bug description",
		"labels": "bug",
	})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, `"status":"created"`) {
		testing.Errorf("expected created status in result: %s", result)
	}

	commandArgs := (*calls)[0]
	hasTitle := false
	hasLabel := false
	for index, argument := range commandArgs {
		if argument == "--title" && index+1 < len(commandArgs) && commandArgs[index+1] == "New Bug" {
			hasTitle = true
		}
		if argument == "--label" && index+1 < len(commandArgs) && commandArgs[index+1] == "bug" {
			hasLabel = true
		}
	}
	if !hasTitle {
		testing.Error("expected --title in args")
	}
	if !hasLabel {
		testing.Error("expected --label in args")
	}
}

func TestIssuesTool_CreateMissingFields(testing *testing.T) {
	runner, _ := mockRunner("", nil)
	tool := &issuesTool{binary: "gh", runner: runner}

	// Missing body.
	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "create",
		"title":  "Test",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "body is required") {
		testing.Errorf("expected 'body is required' error, got: %v", err)
	}
}

func TestIssuesTool_CloseAction(testing *testing.T) {
	runner, calls := mockRunner("Closed issue #5", nil)
	tool := &issuesTool{binary: "gh", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "close",
		"number": 5,
	})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, `"status":"closed"`) {
		testing.Errorf("expected closed status in result: %s", result)
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
		testing.Errorf("expected 'issue close' in args: %v", commandArgs)
	}
}

func TestIssuesTool_ListAssigneeMe(testing *testing.T) {
	runner, calls := mockRunner(`[{"number":5}]`, nil)
	tool := &issuesTool{binary: "gh", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":   "list",
		"assignee": "@me",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}

	commandArgs := (*calls)[0]
	foundAssignee := false
	for index, argument := range commandArgs {
		if argument == "--assignee" && index+1 < len(commandArgs) && commandArgs[index+1] == "@me" {
			foundAssignee = true
		}
	}
	if !foundAssignee {
		testing.Errorf("expected '--assignee @me' in args: %v", commandArgs)
	}
}

func TestIssuesTool_ListAuthor(testing *testing.T) {
	runner, calls := mockRunner(`[{"number":6}]`, nil)
	tool := &issuesTool{binary: "gh", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "list",
		"author": "@me",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}

	commandArgs := (*calls)[0]
	foundAuthor := false
	for index, argument := range commandArgs {
		if argument == "--author" && index+1 < len(commandArgs) && commandArgs[index+1] == "@me" {
			foundAuthor = true
		}
	}
	if !foundAuthor {
		testing.Errorf("expected '--author @me' in args: %v", commandArgs)
	}
}

func TestIssuesTool_RepositoryOverride(testing *testing.T) {
	runner, calls := mockRunner(`[{"number":1}]`, nil)
	tool := &issuesTool{binary: "gh", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":     "list",
		"repository": "owner/repo",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}

	commandArgs := (*calls)[0]
	foundRepo := false
	for index, argument := range commandArgs {
		if argument == "-R" && index+1 < len(commandArgs) && commandArgs[index+1] == "owner/repo" {
			foundRepo = true
			break
		}
	}
	if !foundRepo {
		testing.Errorf("expected '-R owner/repo' in args: %v", commandArgs)
	}
}

func TestIssuesTool_UnknownAction(testing *testing.T) {
	runner, _ := mockRunner("", nil)
	tool := &issuesTool{binary: "gh", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "unknown",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "unknown issues action") {
		testing.Errorf("expected unknown action error, got: %v", err)
	}
}

// --- pulls tool tests ---

func TestPullsTool_ListAction(testing *testing.T) {
	runner, calls := mockRunner(`[{"number":10,"title":"Feature"}]`, nil)
	tool := &pullsTool{binary: "gh", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "list",
		"limit":  5,
	})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		testing.Error("expected non-empty result")
	}

	commandArgs := (*calls)[0]
	foundPRList := false
	for index, argument := range commandArgs {
		if argument == "pr" && index+1 < len(commandArgs) && commandArgs[index+1] == "list" {
			foundPRList = true
			break
		}
	}
	if !foundPRList {
		testing.Errorf("expected 'pr list' in args: %v", commandArgs)
	}
}

func TestPullsTool_ListAssigneeMe(testing *testing.T) {
	runner, calls := mockRunner(`[{"number":11}]`, nil)
	tool := &pullsTool{binary: "gh", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":   "list",
		"assignee": "@me",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}

	commandArgs := (*calls)[0]
	foundAssignee := false
	for index, argument := range commandArgs {
		if argument == "--assignee" && index+1 < len(commandArgs) && commandArgs[index+1] == "@me" {
			foundAssignee = true
		}
	}
	if !foundAssignee {
		testing.Errorf("expected '--assignee @me' in args: %v", commandArgs)
	}
}

func TestPullsTool_ListAuthor(testing *testing.T) {
	runner, calls := mockRunner(`[{"number":12}]`, nil)
	tool := &pullsTool{binary: "gh", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "list",
		"author": "@me",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}

	commandArgs := (*calls)[0]
	foundAuthor := false
	for index, argument := range commandArgs {
		if argument == "--author" && index+1 < len(commandArgs) && commandArgs[index+1] == "@me" {
			foundAuthor = true
		}
	}
	if !foundAuthor {
		testing.Errorf("expected '--author @me' in args: %v", commandArgs)
	}
}

func TestPullsTool_CreateAction(testing *testing.T) {
	runner, _ := mockRunner("https://github.com/owner/repo/pull/1", nil)
	tool := &pullsTool{binary: "gh", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "create",
		"title":  "New Feature",
		"body":   "Description",
		"head":   "feature-branch",
		"base":   "main",
	})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, `"status":"created"`) {
		testing.Errorf("expected created status in result: %s", result)
	}
}

func TestPullsTool_CreateMissingHead(testing *testing.T) {
	runner, _ := mockRunner("", nil)
	tool := &pullsTool{binary: "gh", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "create",
		"title":  "Test",
		"body":   "Test body",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "head is required") {
		testing.Errorf("expected 'head is required' error, got: %v", err)
	}
}

func TestPullsTool_EditAction(testing *testing.T) {
	runner, calls := mockRunner("Updated PR #42", nil)
	tool := &pullsTool{binary: "gh", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "edit",
		"number": 42,
		"title":  "Updated Title",
		"body":   "Updated body",
		"base":   "develop",
	})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, `"status":"edited"`) {
		testing.Errorf("expected edited status in result: %s", result)
	}

	commandArgs := (*calls)[0]
	foundTitle := false
	foundBody := false
	foundBase := false
	for index, argument := range commandArgs {
		if argument == "--title" && index+1 < len(commandArgs) && commandArgs[index+1] == "Updated Title" {
			foundTitle = true
		}
		if argument == "--body" && index+1 < len(commandArgs) && commandArgs[index+1] == "Updated body" {
			foundBody = true
		}
		if argument == "--base" && index+1 < len(commandArgs) && commandArgs[index+1] == "develop" {
			foundBase = true
		}
	}
	if !foundTitle {
		testing.Errorf("expected '--title Updated Title' in args: %v", commandArgs)
	}
	if !foundBody {
		testing.Errorf("expected '--body Updated body' in args: %v", commandArgs)
	}
	if !foundBase {
		testing.Errorf("expected '--base develop' in args: %v", commandArgs)
	}
}

func TestPullsTool_EditMissingNumber(testing *testing.T) {
	runner, _ := mockRunner("", nil)
	tool := &pullsTool{binary: "gh", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "edit",
		"title":  "New Title",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "number is required") {
		testing.Errorf("expected 'number is required' error, got: %v", err)
	}
}

func TestPullsTool_MergeAction(testing *testing.T) {
	runner, calls := mockRunner("Merged PR #10", nil)
	tool := &pullsTool{binary: "gh", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":       "merge",
		"number":       10,
		"merge_method": "squash",
	})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, `"status":"merged"`) {
		testing.Errorf("expected merged status in result: %s", result)
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
		testing.Errorf("expected '--squash' in args: %v", commandArgs)
	}
}

func TestPullsTool_DiffAction(testing *testing.T) {
	runner, _ := mockRunner("diff --git a/file.go b/file.go\n+added line", nil)
	tool := &pullsTool{binary: "gh", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "diff",
		"number": 5,
	})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, `"diff"`) {
		testing.Errorf("expected diff key in JSON result: %s", result)
	}
}

func TestPullsTool_ChecksAction(testing *testing.T) {
	runner, calls := mockRunner(`[{"name":"CI","state":"SUCCESS"}]`, nil)
	tool := &pullsTool{binary: "gh", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "checks",
		"number": 7,
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}

	commandArgs := (*calls)[0]
	foundChecks := false
	for index, argument := range commandArgs {
		if argument == "pr" && index+1 < len(commandArgs) && commandArgs[index+1] == "checks" {
			foundChecks = true
			break
		}
	}
	if !foundChecks {
		testing.Errorf("expected 'pr checks' in args: %v", commandArgs)
	}
}

func TestPullsTool_UnknownAction(testing *testing.T) {
	runner, _ := mockRunner("", nil)
	tool := &pullsTool{binary: "gh", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "unknown",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "unknown pulls action") {
		testing.Errorf("expected unknown action error, got: %v", err)
	}
}

// --- repos tool tests ---

func TestReposTool_ViewAction(testing *testing.T) {
	runner, calls := mockRunner(`{"name":"repo","description":"A repo"}`, nil)
	tool := &reposTool{binary: "gh", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":     "view",
		"repository": "owner/repo",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}

	commandArgs := (*calls)[0]
	foundRepoView := false
	foundPositionalRepo := false
	for index, argument := range commandArgs {
		if argument == "repo" && index+1 < len(commandArgs) && commandArgs[index+1] == "view" {
			foundRepoView = true
		}
		if argument == "view" && index+1 < len(commandArgs) && commandArgs[index+1] == "owner/repo" {
			foundPositionalRepo = true
		}
		if argument == "-R" {
			testing.Errorf("repo view should not use -R flag: %v", commandArgs)
		}
	}
	if !foundRepoView {
		testing.Errorf("expected 'repo view' in args: %v", commandArgs)
	}
	if !foundPositionalRepo {
		testing.Errorf("expected 'owner/repo' as positional argument after 'view': %v", commandArgs)
	}
}

func TestReposTool_ListAction(testing *testing.T) {
	runner, calls := mockRunner(`[{"name":"repo1"}]`, nil)
	tool := &reposTool{binary: "gh", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "list",
		"owner":  "testorg",
		"limit":  5,
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}

	commandArgs := (*calls)[0]
	foundOwner := false
	for _, argument := range commandArgs {
		if argument == "testorg" {
			foundOwner = true
			break
		}
	}
	if !foundOwner {
		testing.Errorf("expected owner in args: %v", commandArgs)
	}
}

func TestReposTool_ListMissingOwner(testing *testing.T) {
	runner, _ := mockRunner("", nil)
	tool := &reposTool{binary: "gh", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "list",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "owner is required") {
		testing.Errorf("expected 'owner is required' error, got: %v", err)
	}
}

func TestReposTool_UnknownAction(testing *testing.T) {
	runner, _ := mockRunner("", nil)
	tool := &reposTool{binary: "gh", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "unknown",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "unknown repos action") {
		testing.Errorf("expected unknown action error, got: %v", err)
	}
}

// --- search tool tests ---

func TestSearchTool_IssuesAction(testing *testing.T) {
	runner, calls := mockRunner(`[{"number":1,"title":"Bug"}]`, nil)
	tool := &searchTool{binary: "gh", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "issues",
		"query":  "repo:owner/repo is:open bug",
		"limit":  10,
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}

	commandArgs := (*calls)[0]
	foundSearchIssues := false
	for index, argument := range commandArgs {
		if argument == "search" && index+1 < len(commandArgs) && commandArgs[index+1] == "issues" {
			foundSearchIssues = true
			break
		}
	}
	if !foundSearchIssues {
		testing.Errorf("expected 'search issues' in args: %v", commandArgs)
	}
}

func TestSearchTool_CodeAction(testing *testing.T) {
	runner, calls := mockRunner(`[{"path":"main.go"}]`, nil)
	tool := &searchTool{binary: "gh", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "code",
		"query":  "repo:owner/repo function main",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}

	commandArgs := (*calls)[0]
	foundSearchCode := false
	for index, argument := range commandArgs {
		if argument == "search" && index+1 < len(commandArgs) && commandArgs[index+1] == "code" {
			foundSearchCode = true
			break
		}
	}
	if !foundSearchCode {
		testing.Errorf("expected 'search code' in args: %v", commandArgs)
	}
}

func TestSearchTool_MissingQuery(testing *testing.T) {
	runner, _ := mockRunner("", nil)
	tool := &searchTool{binary: "gh", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "issues",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "query is required") {
		testing.Errorf("expected 'query is required' error, got: %v", err)
	}
}

func TestSearchTool_UnknownAction(testing *testing.T) {
	runner, _ := mockRunner("", nil)
	tool := &searchTool{binary: "gh", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "unknown",
		"query":  "test",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "unknown search action") {
		testing.Errorf("expected unknown action error, got: %v", err)
	}
}

// --- actions tool tests ---

func TestActionsTool_ListWorkflows(testing *testing.T) {
	runner, calls := mockRunner(`[{"id":1,"name":"CI","state":"active"}]`, nil)
	tool := &actionsTool{binary: "gh", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "list_workflows",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}

	commandArgs := (*calls)[0]
	foundWorkflowList := false
	for index, argument := range commandArgs {
		if argument == "workflow" && index+1 < len(commandArgs) && commandArgs[index+1] == "list" {
			foundWorkflowList = true
			break
		}
	}
	if !foundWorkflowList {
		testing.Errorf("expected 'workflow list' in args: %v", commandArgs)
	}
}

func TestActionsTool_ViewRun(testing *testing.T) {
	runner, calls := mockRunner(`{"databaseId":123,"status":"completed"}`, nil)
	tool := &actionsTool{binary: "gh", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "view_run",
		"run_id": "123",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}

	commandArgs := (*calls)[0]
	foundRunId := false
	for _, argument := range commandArgs {
		if argument == "123" {
			foundRunId = true
			break
		}
	}
	if !foundRunId {
		testing.Errorf("expected run ID in args: %v", commandArgs)
	}
}

func TestActionsTool_ViewRunMissingID(testing *testing.T) {
	runner, _ := mockRunner("", nil)
	tool := &actionsTool{binary: "gh", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "view_run",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "run_id is required") {
		testing.Errorf("expected 'run_id is required' error, got: %v", err)
	}
}

func TestActionsTool_UnknownAction(testing *testing.T) {
	runner, _ := mockRunner("", nil)
	tool := &actionsTool{binary: "gh", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "unknown",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "unknown actions action") {
		testing.Errorf("expected unknown action error, got: %v", err)
	}
}

// --- releases tool tests ---

func TestReleasesTool_ListAction(testing *testing.T) {
	runner, calls := mockRunner(`[{"tagName":"v1.0.0","name":"Release 1"}]`, nil)
	tool := &releasesTool{binary: "gh", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "list",
		"limit":  5,
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
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
		testing.Errorf("expected 'release list' in args: %v", commandArgs)
	}
}

func TestReleasesTool_CreateAction(testing *testing.T) {
	runner, calls := mockRunner("https://github.com/owner/repo/releases/tag/v1.0.0", nil)
	tool := &releasesTool{binary: "gh", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":     "create",
		"tag":        "v1.0.0",
		"title":      "Release 1.0.0",
		"notes":      "First release",
		"draft":      true,
		"prerelease": true,
	})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, `"status":"created"`) {
		testing.Errorf("expected created status in result: %s", result)
	}

	commandArgs := (*calls)[0]
	hasDraft := false
	hasPrerelease := false
	hasNotes := false
	for index, argument := range commandArgs {
		if argument == "--draft" {
			hasDraft = true
		}
		if argument == "--prerelease" {
			hasPrerelease = true
		}
		if argument == "--notes" && index+1 < len(commandArgs) && commandArgs[index+1] == "First release" {
			hasNotes = true
		}
	}
	if !hasDraft {
		testing.Error("expected --draft in args")
	}
	if !hasPrerelease {
		testing.Error("expected --prerelease in args")
	}
	if !hasNotes {
		testing.Error("expected --notes in args")
	}
}

func TestReleasesTool_CreateMissingTag(testing *testing.T) {
	runner, _ := mockRunner("", nil)
	tool := &releasesTool{binary: "gh", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "create",
		"title":  "Test",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "tag is required") {
		testing.Errorf("expected 'tag is required' error, got: %v", err)
	}
}

func TestReleasesTool_UnknownAction(testing *testing.T) {
	runner, _ := mockRunner("", nil)
	tool := &releasesTool{binary: "gh", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "unknown",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "unknown releases action") {
		testing.Errorf("expected unknown action error, got: %v", err)
	}
}

// --- tool definition tests ---

func TestToolDefinitions(t *testing.T) {
	tools := []struct {
		name string
		tool interface {
			Definition() providers.ToolDefinition
		}
	}{
		{"issues", &issuesTool{}},
		{"pulls", &pullsTool{}},
		{"repos", &reposTool{}},
		{"search", &searchTool{}},
		{"actions", &actionsTool{}},
		{"releases", &releasesTool{}},
	}

	for _, testCase := range tools {
		t.Run(testCase.name, func(t *testing.T) {
			definition := testCase.tool.Definition()
			if definition.Type != "function" {
				t.Errorf("expected type 'function', got %q", definition.Type)
			}
			if !strings.HasPrefix(definition.Function.Name, "github_") {
				t.Errorf("expected name to start with 'github_', got %q", definition.Function.Name)
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
