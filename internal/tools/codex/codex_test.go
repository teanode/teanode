package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	storefs "github.com/teanode/teanode/internal/store/fsstore"
)

// mockRunner returns a commandRunner that records calls and returns canned output.
func mockRunner(stdout string, stderr string, exitCode int, err error) (commandRunner, *[]mockCall) {
	var calls []mockCall
	runner := func(ctx context.Context, name string, args []string, directory string) ([]byte, []byte, int, error) {
		calls = append(calls, mockCall{
			Name:      name,
			Arguments: args,
			Directory: directory,
		})
		if err != nil {
			return []byte(stdout), []byte(stderr), exitCode, err
		}
		return []byte(stdout), []byte(stderr), exitCode, nil
	}
	return runner, &calls
}

type mockCall struct {
	Name      string
	Arguments []string
	Directory string
}

// --- tool definition tests ---

func TestDefinition(testing *testing.T) {
	tool := &codexTool{}
	definition := tool.Definition()

	if definition.Type != "function" {
		testing.Errorf("expected type 'function', got %q", definition.Type)
	}
	if definition.Function.Name != "codex" {
		testing.Errorf("expected name 'codex', got %q", definition.Function.Name)
	}
	if definition.Function.Description == "" {
		testing.Error("expected non-empty description")
	}
	if definition.Function.Parameters == nil {
		testing.Error("expected non-nil parameters")
	}
	if definition.Function.Returns == nil {
		testing.Error("expected non-nil returns")
	}

	// Verify action enum exists.
	parameters, ok := definition.Function.Parameters.(map[string]interface{})
	if !ok {
		testing.Fatal("parameters should be a map")
	}
	properties, ok := parameters["properties"].(map[string]interface{})
	if !ok {
		testing.Fatal("properties should be a map")
	}
	action, ok := properties["action"].(map[string]interface{})
	if !ok {
		testing.Fatal("action property should exist")
	}
	if action["type"] != "string" {
		testing.Error("action should be type string")
	}
	if action["enum"] == nil {
		testing.Error("action should have enum")
	}
}

// --- run action tests ---

func TestRunWithValidJSON(testing *testing.T) {
	codexOutput := `{"result":"Files listed successfully","session_id":"abc-123","is_error":false,"cost_usd":0.05,"num_input_tokens":100,"num_output_tokens":50}`
	runner, calls := mockRunner(codexOutput, "", 0, nil)

	tool := &codexTool{
		binaryPath:   "/usr/bin/codex",
		runner:       runner,
		sessions:     make(map[string]*sessionInfo),
	}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "run",
		"prompt": "List files in the current directory",
	})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}

	// Verify the result contains expected fields.
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		testing.Fatalf("result is not valid JSON: %v", err)
	}
	if parsed["sessionId"] != "abc-123" {
		testing.Errorf("expected sessionId 'abc-123', got %v", parsed["sessionId"])
	}
	if parsed["result"] != "Files listed successfully" {
		testing.Errorf("unexpected result: %v", parsed["result"])
	}
	if parsed["isError"] != false {
		testing.Errorf("expected isError false, got %v", parsed["isError"])
	}
	if parsed["costUsd"] != 0.05 {
		testing.Errorf("expected costUsd 0.05, got %v", parsed["costUsd"])
	}
	if parsed["timedOut"] != false {
		testing.Errorf("expected timedOut false, got %v", parsed["timedOut"])
	}
	resume, ok := parsed["resume"].(map[string]interface{})
	if !ok {
		testing.Fatalf("expected resume payload, got: %v", parsed["resume"])
	}
	if resume["action"] != "resume" {
		testing.Errorf("expected resume.action 'resume', got %v", resume["action"])
	}
	if resume["sessionId"] != "abc-123" {
		testing.Errorf("expected resume.sessionId 'abc-123', got %v", resume["sessionId"])
	}

	// Verify command was called correctly.
	if len(*calls) != 1 {
		testing.Fatalf("expected 1 call, got %d", len(*calls))
	}
	call := (*calls)[0]
	if call.Name != "/usr/bin/codex" {
		testing.Errorf("expected binary '/usr/bin/codex', got %q", call.Name)
	}
}

func TestRunWithNonJSONFallback(testing *testing.T) {
	runner, _ := mockRunner("Some plain text output\nfrom codex", "", 0, nil)

	tool := &codexTool{
		binaryPath:   "/usr/bin/codex",
		runner:       runner,
		sessions:     make(map[string]*sessionInfo),
	}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "run",
		"prompt": "Do something",
	})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		testing.Fatalf("result is not valid JSON: %v", err)
	}
	if parsed["result"] != "Some plain text output\nfrom codex" {
		testing.Errorf("unexpected result: %v", parsed["result"])
	}
	if parsed["isError"] != false {
		testing.Errorf("expected isError false for exit code 0, got %v", parsed["isError"])
	}
}

func TestRunWithNonJSONFallbackError(testing *testing.T) {
	runner, _ := mockRunner("Error output", "some stderr", 1, nil)

	tool := &codexTool{
		binaryPath:   "/usr/bin/codex",
		runner:       runner,
		sessions:     make(map[string]*sessionInfo),
	}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "run",
		"prompt": "Do something",
	})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		testing.Fatalf("result is not valid JSON: %v", err)
	}
	if parsed["isError"] != true {
		testing.Errorf("expected isError true for exit code 1, got %v", parsed["isError"])
	}
	if parsed["exitCode"] != float64(1) {
		testing.Errorf("expected exitCode 1, got %v", parsed["exitCode"])
	}
}

func TestRunMissingPrompt(testing *testing.T) {
	runner, _ := mockRunner("", "", 0, nil)
	tool := &codexTool{
		binaryPath:   "/usr/bin/codex",
		runner:       runner,
		sessions:     make(map[string]*sessionInfo),
	}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "run",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "prompt is required") {
		testing.Errorf("expected 'prompt is required' error, got: %v", err)
	}
}

func TestRunRequiresResumeWhenSessionExists(testing *testing.T) {
	runner, _ := mockRunner("", "", 0, nil)
	tool := &codexTool{
		binaryPath:   "/usr/bin/codex",
		runner:       runner,
		sessions: map[string]*sessionInfo{
			"existing-session": {SessionID: "existing-session"},
		},
	}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "run",
		"prompt": "Do something",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "existing session(s) detected") {
		testing.Errorf("expected existing session guidance error, got: %v", err)
	}
}

func TestRunAllowsForceNewSessionWhenSessionExists(testing *testing.T) {
	codexOutput := `{"result":"Started fresh","session_id":"new-session","is_error":false,"cost_usd":0.01,"num_input_tokens":10,"num_output_tokens":5}`
	runner, calls := mockRunner(codexOutput, "", 0, nil)
	tool := &codexTool{
		binaryPath:   "/usr/bin/codex",
		runner:       runner,
		sessions: map[string]*sessionInfo{
			"existing-session": {SessionID: "existing-session"},
		},
	}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":          "run",
		"prompt":          "Start a new task",
		"forceNewSession": true,
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}
	if len(*calls) != 1 {
		testing.Fatalf("expected 1 call, got %d", len(*calls))
	}
}

// --- resume action tests ---

func TestResumeKnownSession(testing *testing.T) {
	codexOutput := `{"result":"Continued work","session_id":"abc-123","is_error":false,"cost_usd":0.03,"num_input_tokens":50,"num_output_tokens":30}`
	runner, calls := mockRunner(codexOutput, "", 0, nil)

	tool := &codexTool{
		binaryPath:   "/usr/bin/codex",
		runner:       runner,
		sessions: map[string]*sessionInfo{
			"abc-123": {
				SessionID:  "abc-123",
				CreatedAt:  time.Now().Add(-time.Hour),
				LastUsedAt: time.Now().Add(-time.Minute),
				TurnCount:  1,
			},
		},
	}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":    "resume",
		"sessionId": "abc-123",
		"prompt":    "Continue working on the task",
	})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		testing.Fatalf("result is not valid JSON: %v", err)
	}
	if parsed["sessionId"] != "abc-123" {
		testing.Errorf("expected sessionId 'abc-123', got %v", parsed["sessionId"])
	}

	// Verify resume subcommand and session id were passed.
	if len(*calls) != 1 {
		testing.Fatalf("expected 1 call, got %d", len(*calls))
	}
	commandArguments := (*calls)[0].Arguments
	foundResume := false
	foundSessionId := false
	for _, argument := range commandArguments {
		if argument == "resume" {
			foundResume = true
		}
		if argument == "abc-123" {
			foundSessionId = true
		}
	}
	if !foundResume || !foundSessionId {
		testing.Errorf("expected 'exec resume ... abc-123 ...' in args: %v", commandArguments)
	}

	// Verify turn count was incremented.
	tool.mutex.Lock()
	session := tool.sessions["abc-123"]
	tool.mutex.Unlock()
	if session.TurnCount != 2 {
		testing.Errorf("expected turnCount 2, got %d", session.TurnCount)
	}
}

func TestResumeWithoutTrackedSession(testing *testing.T) {
	codexOutput := `{"result":"Continued anyway","session_id":"nonexistent","is_error":false,"cost_usd":0.01,"num_input_tokens":10,"num_output_tokens":5}`
	runner, calls := mockRunner(codexOutput, "", 0, nil)
	tool := &codexTool{
		binaryPath:   "/usr/bin/codex",
		runner:       runner,
		sessions:     make(map[string]*sessionInfo),
	}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":    "resume",
		"sessionId": "nonexistent",
		"prompt":    "Continue",
	})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}
	if len(*calls) != 1 {
		testing.Fatalf("expected 1 call, got %d", len(*calls))
	}
	commandArguments := (*calls)[0].Arguments
	foundResume := false
	foundSessionId := false
	for _, argument := range commandArguments {
		if argument == "resume" {
			foundResume = true
		}
		if argument == "nonexistent" {
			foundSessionId = true
		}
	}
	if !foundResume || !foundSessionId {
		testing.Errorf("expected resume args for explicit session ID, got: %v", commandArguments)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		testing.Fatalf("result is not valid JSON: %v", err)
	}
	if parsed["sessionId"] != "nonexistent" {
		testing.Errorf("expected sessionId 'nonexistent', got %v", parsed["sessionId"])
	}
}

func TestResumeMissingSessionID(testing *testing.T) {
	runner, _ := mockRunner("", "", 0, nil)
	tool := &codexTool{
		binaryPath:   "/usr/bin/codex",
		runner:       runner,
		sessions:     make(map[string]*sessionInfo),
	}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "resume",
		"prompt": "Continue",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "sessionId is required") {
		testing.Errorf("expected 'sessionId is required' error, got: %v", err)
	}
}

func TestResumeMissingPrompt(testing *testing.T) {
	runner, _ := mockRunner("", "", 0, nil)
	tool := &codexTool{
		binaryPath:   "/usr/bin/codex",
		runner:       runner,
		sessions: map[string]*sessionInfo{
			"abc-123": {SessionID: "abc-123"},
		},
	}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":    "resume",
		"sessionId": "abc-123",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "prompt is required") {
		testing.Errorf("expected 'prompt is required' error, got: %v", err)
	}
}

// --- list_sessions tests ---

func TestListSessionsEmpty(testing *testing.T) {
	tool := &codexTool{
		sessions: make(map[string]*sessionInfo),
	}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "list_sessions",
	})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		testing.Fatalf("result is not valid JSON: %v", err)
	}
	sessions, ok := parsed["sessions"].([]interface{})
	if !ok {
		testing.Fatal("expected sessions to be an array")
	}
	if len(sessions) != 0 {
		testing.Errorf("expected 0 sessions, got %d", len(sessions))
	}
}

func TestListSessionsAfterRuns(testing *testing.T) {
	codexOutput1 := `{"result":"Done 1","session_id":"session-1","is_error":false,"cost_usd":0.01,"num_input_tokens":10,"num_output_tokens":5}`
	codexOutput2 := `{"result":"Done 2","session_id":"session-2","is_error":false,"cost_usd":0.02,"num_input_tokens":20,"num_output_tokens":10}`

	callCount := 0
	outputs := []string{codexOutput1, codexOutput2}
	runner := func(ctx context.Context, name string, args []string, directory string) ([]byte, []byte, int, error) {
		output := outputs[callCount]
		callCount++
		return []byte(output), nil, 0, nil
	}

	tool := &codexTool{
		binaryPath:   "/usr/bin/codex",
		runner:       runner,
		sessions:     make(map[string]*sessionInfo),
	}

	// Run two tasks.
	for index, prompt := range []string{"Task 1", "Task 2"} {
		arguments, _ := json.Marshal(map[string]interface{}{
			"action": "run",
			"prompt": prompt,
		})
		if index == 1 {
			arguments, _ = json.Marshal(map[string]interface{}{
				"action":          "run",
				"prompt":          prompt,
				"forceNewSession": true,
			})
		}
		_, err := tool.Execute(context.Background(), string(arguments))
		if err != nil {
			testing.Fatalf("unexpected error: %v", err)
		}
	}

	// List sessions.
	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "list_sessions",
	})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		testing.Fatalf("result is not valid JSON: %v", err)
	}
	sessions, ok := parsed["sessions"].([]interface{})
	if !ok {
		testing.Fatal("expected sessions to be an array")
	}
	if len(sessions) != 2 {
		testing.Errorf("expected 2 sessions, got %d", len(sessions))
	}
}

func TestLoadSessionsFromConversationStore(testing *testing.T) {
	dataDirectory := testing.TempDir()
	openedStore, openError := storefs.Open(storefs.Options{DataDirectory: dataDirectory})
	if openError != nil {
		testing.Fatalf("opening store: %v", openError)
	}
	if migrateError := openedStore.Migrate(context.Background()); migrateError != nil {
		testing.Fatalf("migrating store: %v", migrateError)
	}
	testing.Cleanup(func() {
		_ = openedStore.Close()
	})

	ctx := store.ContextWithStore(context.Background(), openedStore)
	userId := "user-1"
	agentId := "agent-1"

	timestamp1 := time.Now().Add(-2 * time.Hour)
	timestamp2 := time.Now().Add(-time.Hour)
	timestamp3 := time.Now()

	toolRole := models.RoleTool
	contentS1 := marshalContent(`{"sessionId":"s1","result":"one"}`)
	contentS1Resume := marshalContent(`{"resume":{"sessionId":"s1"}}`)
	contentS2 := marshalContent(`{"sessionId":"s2","result":"two"}`)
	contentOther := marshalContent(`{"sessionId":"ignored"}`)
	codexToolName := "codex"
	otherToolName := "other_tool"

	if err := openedStore.Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		conversation1, createError := transaction.CreateConversation(ctx, &models.Conversation{
			UserID:  &userId,
			AgentID: &agentId,
		}, nil)
		if createError != nil {
			return createError
		}
		conversation2, createError := transaction.CreateConversation(ctx, &models.Conversation{
			UserID:  &userId,
			AgentID: &agentId,
		}, nil)
		if createError != nil {
			return createError
		}

		// Conversation 1: two tool messages for session s1
		if _, err := transaction.CreateConversationMessage(ctx, &models.ConversationMessage{
			ConversationID: &conversation1.ID,
			Role:           &toolRole,
			Content:        contentS1,
			ToolName:       &codexToolName,
			CreatedAt:      &timestamp1,
		}, nil); err != nil {
			return err
		}
		if _, err := transaction.CreateConversationMessage(ctx, &models.ConversationMessage{
			ConversationID: &conversation1.ID,
			Role:           &toolRole,
			Content:        contentS1Resume,
			ToolName:       &codexToolName,
			CreatedAt:      &timestamp2,
		}, nil); err != nil {
			return err
		}

		// Conversation 2: one tool message for session s2, one for another tool
		if _, err := transaction.CreateConversationMessage(ctx, &models.ConversationMessage{
			ConversationID: &conversation2.ID,
			Role:           &toolRole,
			Content:        contentS2,
			ToolName:       &codexToolName,
			CreatedAt:      &timestamp3,
		}, nil); err != nil {
			return err
		}
		if _, err := transaction.CreateConversationMessage(ctx, &models.ConversationMessage{
			ConversationID: &conversation2.ID,
			Role:           &toolRole,
			Content:        contentOther,
			ToolName:       &otherToolName,
			CreatedAt:      &timestamp3,
		}, nil); err != nil {
			return err
		}

		return nil
	}); err != nil {
		testing.Fatalf("seeding store: %v", err)
	}

	tool := &codexTool{}
	sessions, err := tool.loadSessionsFromConversationStore(ctx, userId, agentId)
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}
	if len(sessions) != 2 {
		testing.Fatalf("expected 2 sessions, got %d", len(sessions))
	}

	sessionsByID := map[string]*sessionInfo{}
	for index := range sessions {
		sessionsByID[sessions[index].SessionID] = &sessions[index]
	}

	s1 := sessionsByID["s1"]
	if s1 == nil {
		testing.Fatalf("expected session s1 in results: %+v", sessions)
	}
	if s1.TurnCount != 2 {
		testing.Errorf("expected s1 turnCount 2, got %d", s1.TurnCount)
	}
	s2 := sessionsByID["s2"]
	if s2 == nil {
		testing.Fatalf("expected session s2 in results: %+v", sessions)
	}
	if s2.TurnCount != 1 {
		testing.Errorf("expected s2 turnCount 1, got %d", s2.TurnCount)
	}
}

func marshalContent(value string) []byte {
	data, _ := json.Marshal(value)
	return data
}

// --- argument building tests ---

func TestBuildArgumentsBasic(testing *testing.T) {
	tool := &codexTool{}

	arguments := tool.buildArguments(context.Background(), "Do something", "", "")

	// Verify exec mode and prompt placement.
	if len(arguments) < 2 || arguments[0] != "exec" {
		testing.Errorf("expected 'exec' at start, got: %v", arguments)
	}
	if arguments[len(arguments)-1] != "Do something" {
		testing.Errorf("expected prompt as last arg, got: %v", arguments)
	}

	// Verify --output-format is NOT present (newer codex CLI does not support it).
	for _, argument := range arguments {
		if argument == "--output-format" {
			testing.Errorf("did not expect '--output-format' in args: %v", arguments)
			break
		}
	}

	// Verify --json is present.
	foundJSON := false
	for _, argument := range arguments {
		if argument == "--json" {
			foundJSON = true
			break
		}
	}
	if !foundJSON {
		testing.Errorf("expected '--json' in args: %v", arguments)
	}

	// Verify resume subcommand is NOT present.
	for _, argument := range arguments {
		if argument == "resume" {
			testing.Errorf("did not expect 'resume' in args: %v", arguments)
			break
		}
	}
}

func TestBuildArgumentsWithResume(testing *testing.T) {
	tool := &codexTool{}

	arguments := tool.buildArguments(context.Background(), "Continue", "session-xyz", "")

	foundResumeSubcommand := false
	foundSessionId := false
	for index, argument := range arguments {
		if argument == "resume" {
			foundResumeSubcommand = true
		}
		if argument == "session-xyz" {
			foundSessionId = true
		}
		if foundResumeSubcommand && foundSessionId && index > 0 {
			break
		}
	}
	if !foundResumeSubcommand || !foundSessionId {
		testing.Errorf("expected 'exec resume ... session-xyz ...' in args: %v", arguments)
	}
}

func TestBuildArgumentsWithoutModelConfig(testing *testing.T) {
	tool := &codexTool{}

	arguments := tool.buildArguments(context.Background(), "Do something", "", "")

	// Without a store in context, no --model flag should be emitted.
	for _, argument := range arguments {
		if argument == "--model" {
			testing.Errorf("did not expect '--model' without config, got: %v", arguments)
			break
		}
	}
}

func TestBuildArgumentsWithSystemPrompt(testing *testing.T) {
	tool := &codexTool{}

	arguments := tool.buildArguments(context.Background(), "Do something", "", "You are a helpful assistant")

	combinedPrompt := arguments[len(arguments)-1]
	if !strings.Contains(combinedPrompt, "Additional system instructions:\nYou are a helpful assistant") {
		testing.Errorf("expected systemPrompt to be embedded in final prompt arg, got: %q", combinedPrompt)
	}
}

func TestBuildArgumentsDoesNotEmitUnsupportedLegacyFlags(testing *testing.T) {
	tool := &codexTool{}

	arguments := tool.buildArguments(context.Background(), "Do something", "", "")

	for _, argument := range arguments {
		if argument == "--allowedTools" || argument == "--append-system-prompt" || argument == "--resume" {
			testing.Errorf("did not expect legacy unsupported flag %q in args: %v", argument, arguments)
		}
	}
}

// --- timeout tests ---

func TestTimeoutCapping(testing *testing.T) {
	codexOutput := `{"result":"Done","session_id":"s1","is_error":false,"cost_usd":0.01,"num_input_tokens":10,"num_output_tokens":5}`
	runner, _ := mockRunner(codexOutput, "", 0, nil)

	tool := &codexTool{
		binaryPath:   "/usr/bin/codex",
		runner:       runner,
		sessions:     make(map[string]*sessionInfo),
	}

	// Request a timeout that exceeds the max — it should be capped.
	arguments, _ := json.Marshal(map[string]interface{}{
		"action":         "run",
		"prompt":         "Do something",
		"timeoutSeconds": 9999,
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}
}

// --- unknown action test ---

func TestUnknownAction(testing *testing.T) {
	tool := &codexTool{
		sessions: make(map[string]*sessionInfo),
	}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "unknown",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "unknown codex action") {
		testing.Errorf("expected 'unknown codex action' error, got: %v", err)
	}
}

// --- session tracking tests ---

func TestSessionTracking(testing *testing.T) {
	tool := &codexTool{
		sessions: make(map[string]*sessionInfo),
	}

	// Track a new session.
	tool.trackSession("session-1")

	tool.mutex.Lock()
	session, exists := tool.sessions["session-1"]
	tool.mutex.Unlock()
	if !exists {
		testing.Fatal("expected session to be tracked")
	}
	if session.TurnCount != 1 {
		testing.Errorf("expected turnCount 1, got %d", session.TurnCount)
	}

	// Track the same session again.
	tool.trackSession("session-1")

	tool.mutex.Lock()
	session = tool.sessions["session-1"]
	tool.mutex.Unlock()
	if session.TurnCount != 2 {
		testing.Errorf("expected turnCount 2, got %d", session.TurnCount)
	}
}

// --- command execution error test ---

func TestRunCommandExecutionError(testing *testing.T) {
	runner, _ := mockRunner("", "command not found", -1, fmt.Errorf("exec: command not found"))

	tool := &codexTool{
		binaryPath:   "/usr/bin/codex",
		runner:       runner,
		sessions:     make(map[string]*sessionInfo),
	}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "run",
		"prompt": "Do something",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "executing codex") {
		testing.Errorf("expected 'executing codex' error, got: %v", err)
	}
}

// --- working directory test ---

func TestRunWithWorkingDirectory(testing *testing.T) {
	codexOutput := `{"result":"Done","session_id":"s1","is_error":false,"cost_usd":0.01,"num_input_tokens":10,"num_output_tokens":5}`
	runner, calls := mockRunner(codexOutput, "", 0, nil)

	tool := &codexTool{
		binaryPath:   "/usr/bin/codex",
		runner:       runner,
		sessions:     make(map[string]*sessionInfo),
	}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":           "run",
		"prompt":           "List files",
		"workingDirectory": "/tmp/test",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}

	if len(*calls) != 1 {
		testing.Fatalf("expected 1 call, got %d", len(*calls))
	}
	if (*calls)[0].Directory != "/tmp/test" {
		testing.Errorf("expected directory '/tmp/test', got %q", (*calls)[0].Directory)
	}
}

// --- fallback to stderr when stdout is empty ---

func TestRunFallbackToStderr(testing *testing.T) {
	runner, _ := mockRunner("", "Error: something went wrong", 1, nil)

	tool := &codexTool{
		binaryPath:   "/usr/bin/codex",
		runner:       runner,
		sessions:     make(map[string]*sessionInfo),
	}

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "run",
		"prompt": "Do something",
	})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		testing.Fatalf("result is not valid JSON: %v", err)
	}
	if parsed["result"] != "Error: something went wrong" {
		testing.Errorf("expected stderr fallback result, got: %v", parsed["result"])
	}
}
