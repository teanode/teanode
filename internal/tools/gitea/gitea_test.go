package gitea

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

// --- exec tests ---

func TestExecGitea_ArgsAssembly(t *testing.T) {
	runner, calls := mockRunner(`[{"index":1}]`, nil)

	result, err := execGitea(context.Background(), runner, "/usr/bin/tea", "issues", "list", "--output", "json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != `[{"index":1}]` {
		t.Errorf("unexpected result: %s", result)
	}
	if len(*calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(*calls))
	}

	arguments := (*calls)[0]
	expected := []string{"/usr/bin/tea", "issues", "list", "--output", "json"}
	if len(arguments) != len(expected) {
		t.Fatalf("expected %d arguments, got %d: %v", len(expected), len(arguments), arguments)
	}
	for index, want := range expected {
		if arguments[index] != want {
			t.Errorf("argument[%d] = %q, want %q", index, arguments[index], want)
		}
	}
}

func TestExecGitea_OutputTruncation(t *testing.T) {
	bigOutput := strings.Repeat("x", maxOutputBytes+1000)
	runner, _ := mockRunner(bigOutput, nil)

	result, err := execGitea(context.Background(), runner, "tea", "issues", "list")
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

func TestExecGitea_AuthError(t *testing.T) {
	runner, _ := mockRunner("", fmt.Errorf("No login configured. Please use tea login add to add a login"))

	_, err := execGitea(context.Background(), runner, "tea", "issues", "list")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "gitea authentication required") {
		t.Errorf("expected auth error message, got: %v", err)
	}
}

func TestExecGitea_GenericError(t *testing.T) {
	runner, _ := mockRunner("", fmt.Errorf("some random error"))

	_, err := execGitea(context.Background(), runner, "tea", "issues", "list")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "tea command failed") {
		t.Errorf("expected generic error message, got: %v", err)
	}
}

// --- isAuthError tests ---

func TestIsAuthError(t *testing.T) {
	tests := []struct {
		message string
		want    bool
	}{
		{"No login configured", true},
		{"please use tea login add", true},
		{"401 Unauthorized", true},
		{"token is required", true},
		{"unauthorized access", true},
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

func TestIssuesTool_Definition(t *testing.T) {
	tool := &issuesTool{binary: "tea"}
	definition := tool.Definition()
	if definition.Function.Name != "gitea_issues" {
		t.Errorf("expected name gitea_issues, got %s", definition.Function.Name)
	}
	if definition.Type != "function" {
		t.Errorf("expected type function, got %s", definition.Type)
	}
}

func TestIssuesTool_ListAction(t *testing.T) {
	runner, calls := mockRunner(`[{"index":1,"title":"Bug"}]`, nil)
	tool := &issuesTool{binary: "tea", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "list",
		"limit":  10,
		"state":  "open",
	})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result")
	}

	commandArguments := (*calls)[0]
	foundIssuesList := false
	foundOutputJSON := false
	foundState := false
	for index, argument := range commandArguments {
		if argument == "issues" && index+1 < len(commandArguments) && commandArguments[index+1] == "list" {
			foundIssuesList = true
		}
		if argument == "--output" && index+1 < len(commandArguments) && commandArguments[index+1] == "json" {
			foundOutputJSON = true
		}
		if argument == "--state" && index+1 < len(commandArguments) && commandArguments[index+1] == "open" {
			foundState = true
		}
	}
	if !foundIssuesList {
		t.Errorf("expected 'issues list' in arguments: %v", commandArguments)
	}
	if !foundOutputJSON {
		t.Errorf("expected '--output json' in arguments: %v", commandArguments)
	}
	if !foundState {
		t.Errorf("expected '--state open' in arguments: %v", commandArguments)
	}
}

func TestIssuesTool_ListDefaultLimit(t *testing.T) {
	runner, calls := mockRunner(`[]`, nil)
	tool := &issuesTool{binary: "tea", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "list",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	commandArguments := (*calls)[0]
	foundLimit := false
	for index, argument := range commandArguments {
		if argument == "--limit" && index+1 < len(commandArguments) && commandArguments[index+1] == "30" {
			foundLimit = true
		}
	}
	if !foundLimit {
		t.Errorf("expected default '--limit 30' in arguments: %v", commandArguments)
	}
}

func TestIssuesTool_ListWithFilters(t *testing.T) {
	runner, calls := mockRunner(`[]`, nil)
	tool := &issuesTool{binary: "tea", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":   "list",
		"labels":   "bug,critical",
		"assignee": "alice",
		"author":   "bob",
		"keyword":  "crash",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	commandArguments := (*calls)[0]
	assertContainsFlag(t, commandArguments, "--labels", "bug,critical")
	assertContainsFlag(t, commandArguments, "--assignee", "alice")
	assertContainsFlag(t, commandArguments, "--author", "bob")
	assertContainsFlag(t, commandArguments, "--keyword", "crash")
}

func TestIssuesTool_ListWithRepository(t *testing.T) {
	runner, calls := mockRunner(`[]`, nil)
	tool := &issuesTool{binary: "tea", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":     "list",
		"repository": "owner/repo",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	commandArguments := (*calls)[0]
	assertContainsFlag(t, commandArguments, "--repo", "owner/repo")
}

func TestIssuesTool_ViewAction(t *testing.T) {
	runner, calls := mockRunner(`{"index":5,"title":"Bug report"}`, nil)
	tool := &issuesTool{binary: "tea", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "view",
		"number": 5,
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	commandArguments := (*calls)[0]
	assertContainsArg(t, commandArguments, "5")
	assertContainsArg(t, commandArguments, "--comments")
}

func TestIssuesTool_ViewRequiresNumber(t *testing.T) {
	runner, _ := mockRunner("", nil)
	tool := &issuesTool{binary: "tea", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "view",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil {
		t.Fatal("expected error for missing number")
	}
	if !strings.Contains(err.Error(), "number is required") {
		t.Errorf("expected 'number is required' error, got: %v", err)
	}
}

func TestIssuesTool_CreateAction(t *testing.T) {
	runner, calls := mockRunner("Created issue #1", nil)
	tool := &issuesTool{binary: "tea", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":      "create",
		"title":       "New Bug",
		"description": "Something broke",
		"labels":      "bug",
		"assignees":   "alice",
		"milestone":   "v1.0",
	})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "created") {
		t.Errorf("expected 'created' in result: %s", result)
	}

	commandArguments := (*calls)[0]
	assertContainsFlag(t, commandArguments, "--title", "New Bug")
	assertContainsFlag(t, commandArguments, "--description", "Something broke")
	assertContainsFlag(t, commandArguments, "--labels", "bug")
	assertContainsFlag(t, commandArguments, "--assignees", "alice")
	assertContainsFlag(t, commandArguments, "--milestone", "v1.0")
}

func TestIssuesTool_CreateRequiresTitle(t *testing.T) {
	runner, _ := mockRunner("", nil)
	tool := &issuesTool{binary: "tea", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":      "create",
		"description": "Something broke",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil {
		t.Fatal("expected error for missing title")
	}
}

func TestIssuesTool_CreateRequiresDescription(t *testing.T) {
	runner, _ := mockRunner("", nil)
	tool := &issuesTool{binary: "tea", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "create",
		"title":  "Bug",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil {
		t.Fatal("expected error for missing description")
	}
}

func TestIssuesTool_CommentAction(t *testing.T) {
	runner, calls := mockRunner("Comment added", nil)
	tool := &issuesTool{binary: "tea", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":      "comment",
		"number":      3,
		"description": "This is a comment",
	})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "commented") {
		t.Errorf("expected 'commented' in result: %s", result)
	}

	commandArguments := (*calls)[0]
	assertContainsArg(t, commandArguments, "comment")
	assertContainsArg(t, commandArguments, "3")
	assertContainsArg(t, commandArguments, "This is a comment")
}

func TestIssuesTool_CloseAction(t *testing.T) {
	runner, calls := mockRunner("Issue closed", nil)
	tool := &issuesTool{binary: "tea", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "close",
		"number": 7,
	})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "closed") {
		t.Errorf("expected 'closed' in result: %s", result)
	}

	commandArguments := (*calls)[0]
	assertContainsArg(t, commandArguments, "close")
	assertContainsArg(t, commandArguments, "7")
}

func TestIssuesTool_ReopenAction(t *testing.T) {
	runner, calls := mockRunner("Issue reopened", nil)
	tool := &issuesTool{binary: "tea", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "reopen",
		"number": 7,
	})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "reopened") {
		t.Errorf("expected 'reopened' in result: %s", result)
	}

	commandArguments := (*calls)[0]
	assertContainsArg(t, commandArguments, "reopen")
	assertContainsArg(t, commandArguments, "7")
}

func TestIssuesTool_EditAction(t *testing.T) {
	runner, calls := mockRunner("Issue updated", nil)
	tool := &issuesTool{binary: "tea", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":      "edit",
		"number":      4,
		"title":       "Updated Title",
		"description": "Updated description",
		"labels":      "enhancement",
	})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "edited") {
		t.Errorf("expected 'edited' in result: %s", result)
	}

	commandArguments := (*calls)[0]
	assertContainsFlag(t, commandArguments, "--title", "Updated Title")
	assertContainsFlag(t, commandArguments, "--description", "Updated description")
	assertContainsFlag(t, commandArguments, "--add-labels", "enhancement")
}

func TestIssuesTool_EditRequiresUpdate(t *testing.T) {
	runner, _ := mockRunner("", nil)
	tool := &issuesTool{binary: "tea", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "edit",
		"number": 4,
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil {
		t.Fatal("expected error for missing update fields")
	}
}

func TestIssuesTool_UnknownAction(t *testing.T) {
	runner, _ := mockRunner("", nil)
	tool := &issuesTool{binary: "tea", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "unknown",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil {
		t.Fatal("expected error for unknown action")
	}
	if !strings.Contains(err.Error(), "unknown issues action") {
		t.Errorf("expected 'unknown issues action' error, got: %v", err)
	}
}

// --- pulls tool tests ---

func TestPullsTool_Definition(t *testing.T) {
	tool := &pullsTool{binary: "tea"}
	definition := tool.Definition()
	if definition.Function.Name != "gitea_pulls" {
		t.Errorf("expected name gitea_pulls, got %s", definition.Function.Name)
	}
}

func TestPullsTool_ListAction(t *testing.T) {
	runner, calls := mockRunner(`[{"index":1,"title":"Feature"}]`, nil)
	tool := &pullsTool{binary: "tea", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "list",
		"limit":  10,
		"state":  "open",
	})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result")
	}

	commandArguments := (*calls)[0]
	foundPullsList := false
	foundOutputJSON := false
	for index, argument := range commandArguments {
		if argument == "pulls" && index+1 < len(commandArguments) && commandArguments[index+1] == "list" {
			foundPullsList = true
		}
		if argument == "--output" && index+1 < len(commandArguments) && commandArguments[index+1] == "json" {
			foundOutputJSON = true
		}
	}
	if !foundPullsList {
		t.Errorf("expected 'pulls list' in arguments: %v", commandArguments)
	}
	if !foundOutputJSON {
		t.Errorf("expected '--output json' in arguments: %v", commandArguments)
	}
}

func TestPullsTool_ViewAction(t *testing.T) {
	runner, calls := mockRunner(`{"index":3,"title":"Feature PR"}`, nil)
	tool := &pullsTool{binary: "tea", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "view",
		"number": 3,
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	commandArguments := (*calls)[0]
	assertContainsArg(t, commandArguments, "3")
	assertContainsArg(t, commandArguments, "--comments")
}

func TestPullsTool_ViewRequiresNumber(t *testing.T) {
	runner, _ := mockRunner("", nil)
	tool := &pullsTool{binary: "tea", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "view",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil {
		t.Fatal("expected error for missing number")
	}
}

func TestPullsTool_CreateAction(t *testing.T) {
	runner, calls := mockRunner("Created PR #1", nil)
	tool := &pullsTool{binary: "tea", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":      "create",
		"title":       "New Feature",
		"description": "Adds new feature",
		"head":        "feature-branch",
		"base":        "main",
	})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "created") {
		t.Errorf("expected 'created' in result: %s", result)
	}

	commandArguments := (*calls)[0]
	assertContainsFlag(t, commandArguments, "--title", "New Feature")
	assertContainsFlag(t, commandArguments, "--description", "Adds new feature")
	assertContainsFlag(t, commandArguments, "--head", "feature-branch")
	assertContainsFlag(t, commandArguments, "--base", "main")
}

func TestPullsTool_CreateRequiresTitle(t *testing.T) {
	runner, _ := mockRunner("", nil)
	tool := &pullsTool{binary: "tea", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":      "create",
		"description": "Some desc",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil {
		t.Fatal("expected error for missing title")
	}
}

func TestPullsTool_CommentAction(t *testing.T) {
	runner, calls := mockRunner("Comment added", nil)
	tool := &pullsTool{binary: "tea", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":      "comment",
		"number":      5,
		"description": "LGTM",
	})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "commented") {
		t.Errorf("expected 'commented' in result: %s", result)
	}

	commandArguments := (*calls)[0]
	assertContainsArg(t, commandArguments, "comment")
	assertContainsArg(t, commandArguments, "5")
	assertContainsArg(t, commandArguments, "LGTM")
}

func TestPullsTool_CloseAction(t *testing.T) {
	runner, calls := mockRunner("PR closed", nil)
	tool := &pullsTool{binary: "tea", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "close",
		"number": 2,
	})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "closed") {
		t.Errorf("expected 'closed' in result: %s", result)
	}

	commandArguments := (*calls)[0]
	assertContainsArg(t, commandArguments, "close")
	assertContainsArg(t, commandArguments, "2")
}

func TestPullsTool_MergeAction(t *testing.T) {
	runner, calls := mockRunner("PR merged", nil)
	tool := &pullsTool{binary: "tea", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":      "merge",
		"number":      10,
		"merge_style": "squash",
	})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "merged") {
		t.Errorf("expected 'merged' in result: %s", result)
	}

	commandArguments := (*calls)[0]
	assertContainsFlag(t, commandArguments, "--style", "squash")
}

func TestPullsTool_MergeWithMessage(t *testing.T) {
	runner, calls := mockRunner("PR merged", nil)
	tool := &pullsTool{binary: "tea", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":        "merge",
		"number":        10,
		"merge_message": "Merge feature",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	commandArguments := (*calls)[0]
	assertContainsFlag(t, commandArguments, "--message", "Merge feature")
}

func TestPullsTool_ApproveAction(t *testing.T) {
	runner, calls := mockRunner("PR approved", nil)
	tool := &pullsTool{binary: "tea", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "approve",
		"number": 8,
	})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "approved") {
		t.Errorf("expected 'approved' in result: %s", result)
	}

	commandArguments := (*calls)[0]
	assertContainsArg(t, commandArguments, "approve")
	assertContainsArg(t, commandArguments, "8")
}

func TestPullsTool_RejectAction(t *testing.T) {
	runner, calls := mockRunner("Changes requested", nil)
	tool := &pullsTool{binary: "tea", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "reject",
		"number": 9,
	})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "rejected") {
		t.Errorf("expected 'rejected' in result: %s", result)
	}

	commandArguments := (*calls)[0]
	assertContainsArg(t, commandArguments, "reject")
	assertContainsArg(t, commandArguments, "9")
}

func TestPullsTool_UnknownAction(t *testing.T) {
	runner, _ := mockRunner("", nil)
	tool := &pullsTool{binary: "tea", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "unknown",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil {
		t.Fatal("expected error for unknown action")
	}
}

// --- repos tool tests ---

func TestReposTool_Definition(t *testing.T) {
	tool := &reposTool{binary: "tea"}
	definition := tool.Definition()
	if definition.Function.Name != "gitea_repos" {
		t.Errorf("expected name gitea_repos, got %s", definition.Function.Name)
	}
}

func TestReposTool_ViewAction(t *testing.T) {
	runner, calls := mockRunner(`{"name":"myrepo","owner":"alice"}`, nil)
	tool := &reposTool{binary: "tea", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "view",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	commandArguments := (*calls)[0]
	assertContainsArg(t, commandArguments, "repos")
	assertContainsFlag(t, commandArguments, "--output", "json")
}

func TestReposTool_ViewWithRepository(t *testing.T) {
	runner, calls := mockRunner(`{"name":"myrepo"}`, nil)
	tool := &reposTool{binary: "tea", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":     "view",
		"repository": "alice/myrepo",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	commandArguments := (*calls)[0]
	assertContainsArg(t, commandArguments, "alice/myrepo")
}

func TestReposTool_ListAction(t *testing.T) {
	runner, calls := mockRunner(`[{"name":"repo1"}]`, nil)
	tool := &reposTool{binary: "tea", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "list",
		"type":   "source",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	commandArguments := (*calls)[0]
	assertContainsArg(t, commandArguments, "list")
	assertContainsFlag(t, commandArguments, "--type", "source")
	assertContainsFlag(t, commandArguments, "--limit", "30")
}

func TestReposTool_SearchAction(t *testing.T) {
	runner, calls := mockRunner(`[{"name":"found-repo"}]`, nil)
	tool := &reposTool{binary: "tea", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "search",
		"query":  "myproject",
		"owner":  "alice",
		"limit":  5,
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	commandArguments := (*calls)[0]
	assertContainsArg(t, commandArguments, "search")
	assertContainsArg(t, commandArguments, "myproject")
	assertContainsFlag(t, commandArguments, "--owner", "alice")
	assertContainsFlag(t, commandArguments, "--limit", "5")
}

func TestReposTool_SearchRequiresQuery(t *testing.T) {
	runner, _ := mockRunner("", nil)
	tool := &reposTool{binary: "tea", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "search",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil {
		t.Fatal("expected error for missing query")
	}
}

func TestReposTool_UnknownAction(t *testing.T) {
	runner, _ := mockRunner("", nil)
	tool := &reposTool{binary: "tea", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "unknown",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil {
		t.Fatal("expected error for unknown action")
	}
}

// --- releases tool tests ---

func TestReleasesTool_Definition(t *testing.T) {
	tool := &releasesTool{binary: "tea"}
	definition := tool.Definition()
	if definition.Function.Name != "gitea_releases" {
		t.Errorf("expected name gitea_releases, got %s", definition.Function.Name)
	}
}

func TestReleasesTool_ListAction(t *testing.T) {
	runner, calls := mockRunner(`[{"tag_name":"v1.0"}]`, nil)
	tool := &releasesTool{binary: "tea", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "list",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	commandArguments := (*calls)[0]
	assertContainsArg(t, commandArguments, "releases")
	assertContainsArg(t, commandArguments, "list")
	assertContainsFlag(t, commandArguments, "--output", "json")
}

func TestReleasesTool_CreateAction(t *testing.T) {
	runner, calls := mockRunner("Release created", nil)
	tool := &releasesTool{binary: "tea", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":     "create",
		"tag":        "v2.0",
		"title":      "Version 2.0",
		"note":       "Major release",
		"target":     "main",
		"draft":      true,
		"prerelease": true,
	})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "created") {
		t.Errorf("expected 'created' in result: %s", result)
	}

	commandArguments := (*calls)[0]
	assertContainsFlag(t, commandArguments, "--tag", "v2.0")
	assertContainsFlag(t, commandArguments, "--title", "Version 2.0")
	assertContainsFlag(t, commandArguments, "--note", "Major release")
	assertContainsFlag(t, commandArguments, "--target", "main")
	assertContainsArg(t, commandArguments, "--draft")
	assertContainsArg(t, commandArguments, "--prerelease")
}

func TestReleasesTool_CreateRequiresTag(t *testing.T) {
	runner, _ := mockRunner("", nil)
	tool := &releasesTool{binary: "tea", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "create",
		"title":  "Release",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil {
		t.Fatal("expected error for missing tag")
	}
}

func TestReleasesTool_CreateRequiresTitle(t *testing.T) {
	runner, _ := mockRunner("", nil)
	tool := &releasesTool{binary: "tea", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "create",
		"tag":    "v1.0",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil {
		t.Fatal("expected error for missing title")
	}
}

func TestReleasesTool_UnknownAction(t *testing.T) {
	runner, _ := mockRunner("", nil)
	tool := &releasesTool{binary: "tea", runner: runner}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "unknown",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil {
		t.Fatal("expected error for unknown action")
	}
}

// --- wrapPlainOutput test ---

func TestWrapPlainOutput(t *testing.T) {
	result := wrapPlainOutput("created", "Issue #1 created")
	var envelope map[string]string
	if err := json.Unmarshal([]byte(result), &envelope); err != nil {
		t.Fatalf("failed to parse wrapped output: %v", err)
	}
	if envelope["status"] != "created" {
		t.Errorf("expected status 'created', got %q", envelope["status"])
	}
	if envelope["message"] != "Issue #1 created" {
		t.Errorf("expected message 'Issue #1 created', got %q", envelope["message"])
	}
}

// --- appendRepository test ---

func TestAppendRepository(t *testing.T) {
	commandArguments := []string{"issues", "list"}
	appendRepository(&commandArguments, "owner/repo")
	assertContainsFlag(t, commandArguments, "--repo", "owner/repo")
}

func TestAppendRepository_Empty(t *testing.T) {
	commandArguments := []string{"issues", "list"}
	appendRepository(&commandArguments, "")
	for _, argument := range commandArguments {
		if argument == "--repo" {
			t.Error("should not add --repo for empty repository")
		}
	}
}

// --- all tools implement interface ---

func TestToolsImplementInterface(t *testing.T) {
	tools := []interface{}{
		&issuesTool{binary: "tea"},
		&pullsTool{binary: "tea"},
		&reposTool{binary: "tea"},
		&releasesTool{binary: "tea"},
	}
	for _, tool := range tools {
		if _, ok := tool.(interface {
			Definition() providers.ToolDefinition
			Execute(ctx context.Context, arguments string) (string, error)
		}); !ok {
			t.Errorf("tool %T does not implement the expected interface", tool)
		}
	}
}

// --- helpers ---

func assertContainsArg(t *testing.T, commandArguments []string, want string) {
	t.Helper()
	for _, argument := range commandArguments {
		if argument == want {
			return
		}
	}
	t.Errorf("expected %q in arguments: %v", want, commandArguments)
}

func assertContainsFlag(t *testing.T, commandArguments []string, flag string, value string) {
	t.Helper()
	for index, argument := range commandArguments {
		if argument == flag && index+1 < len(commandArguments) && commandArguments[index+1] == value {
			return
		}
	}
	t.Errorf("expected '%s %s' in arguments: %v", flag, value, commandArguments)
}
