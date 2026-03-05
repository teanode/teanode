package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
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
	if !strings.Contains(definition.Function.Description, "plan or progress log") {
		testing.Error("expected description to mention file-based continuity")
	}
	if definition.Function.Parameters == nil {
		testing.Error("expected non-nil parameters")
	}
	if definition.Function.Returns == nil {
		testing.Error("expected non-nil returns")
	}

	// Verify prompt is required (not action).
	parameters, ok := definition.Function.Parameters.(map[string]interface{})
	if !ok {
		testing.Fatal("parameters should be a map")
	}
	required, ok := parameters["required"].([]string)
	if !ok {
		testing.Fatal("required should be a string slice")
	}
	if len(required) != 1 || required[0] != "prompt" {
		testing.Errorf("expected required=[prompt], got %v", required)
	}

	// Verify no action parameter exists.
	properties, ok := parameters["properties"].(map[string]interface{})
	if !ok {
		testing.Fatal("properties should be a map")
	}
	if _, hasAction := properties["action"]; hasAction {
		testing.Error("action parameter should not exist")
	}
	if _, hasSessionId := properties["sessionId"]; hasSessionId {
		testing.Error("sessionId parameter should not exist")
	}
	if _, hasForceNew := properties["forceNewSession"]; hasForceNew {
		testing.Error("forceNewSession parameter should not exist")
	}

	// Verify no session-related fields in returns.
	returns, ok := definition.Function.Returns.(map[string]interface{})
	if !ok {
		testing.Fatal("returns should be a map")
	}
	returnProps, ok := returns["properties"].(map[string]interface{})
	if !ok {
		testing.Fatal("return properties should be a map")
	}
	if _, hasSessionId := returnProps["sessionId"]; hasSessionId {
		testing.Error("sessionId return field should not exist")
	}
	if _, hasResume := returnProps["resume"]; hasResume {
		testing.Error("resume return field should not exist")
	}
	if _, hasSessions := returnProps["sessions"]; hasSessions {
		testing.Error("sessions return field should not exist")
	}
}

// --- run tests ---

func TestRunWithValidJSON(testing *testing.T) {
	codexOutput := `{"result":"Files listed successfully","session_id":"abc-123","is_error":false,"cost_usd":0.05,"num_input_tokens":100,"num_output_tokens":50}`
	runner, calls := mockRunner(codexOutput, "", 0, nil)

	tool := &codexTool{
		binaryPath: "/usr/bin/codex",
		runner:     runner,
	}

	arguments, _ := json.Marshal(map[string]interface{}{
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
	// Verify no session-related fields in output.
	if _, hasSessionId := parsed["sessionId"]; hasSessionId {
		testing.Error("sessionId should not be in output")
	}
	if _, hasResume := parsed["resume"]; hasResume {
		testing.Error("resume should not be in output")
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
		binaryPath: "/usr/bin/codex",
		runner:     runner,
	}

	arguments, _ := json.Marshal(map[string]interface{}{
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
		binaryPath: "/usr/bin/codex",
		runner:     runner,
	}

	arguments, _ := json.Marshal(map[string]interface{}{
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
		binaryPath: "/usr/bin/codex",
		runner:     runner,
	}

	arguments, _ := json.Marshal(map[string]interface{}{})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "prompt is required") {
		testing.Errorf("expected 'prompt is required' error, got: %v", err)
	}
}

// --- argument building tests ---

func TestBuildArgumentsBasic(testing *testing.T) {
	tool := &codexTool{}

	arguments := tool.buildArguments(context.Background(), "Do something", "")

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

func TestBuildArgumentsWithoutModelConfig(testing *testing.T) {
	tool := &codexTool{}

	arguments := tool.buildArguments(context.Background(), "Do something", "")

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

	arguments := tool.buildArguments(context.Background(), "Do something", "You are a helpful assistant")

	combinedPrompt := arguments[len(arguments)-1]
	if !strings.Contains(combinedPrompt, "Additional system instructions:\nYou are a helpful assistant") {
		testing.Errorf("expected systemPrompt to be embedded in final prompt arg, got: %q", combinedPrompt)
	}
}

func TestBuildArgumentsDoesNotEmitUnsupportedLegacyFlags(testing *testing.T) {
	tool := &codexTool{}

	arguments := tool.buildArguments(context.Background(), "Do something", "")

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
		binaryPath: "/usr/bin/codex",
		runner:     runner,
	}

	// Request a timeout that exceeds the max — it should be capped.
	arguments, _ := json.Marshal(map[string]interface{}{
		"prompt":         "Do something",
		"timeoutSeconds": 9999,
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}
}

// --- command execution error test ---

func TestRunCommandExecutionError(testing *testing.T) {
	runner, _ := mockRunner("", "command not found", -1, fmt.Errorf("exec: command not found"))

	tool := &codexTool{
		binaryPath: "/usr/bin/codex",
		runner:     runner,
	}

	arguments, _ := json.Marshal(map[string]interface{}{
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
		binaryPath: "/usr/bin/codex",
		runner:     runner,
	}

	arguments, _ := json.Marshal(map[string]interface{}{
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
		binaryPath: "/usr/bin/codex",
		runner:     runner,
	}

	arguments, _ := json.Marshal(map[string]interface{}{
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
