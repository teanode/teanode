// Package codex exposes a tool integration for Codex.
package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/op/go-logging"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/tools"
	"github.com/teanode/teanode/internal/util/cmdexec"
)

var log = logging.MustGetLogger("codex")

const (
	defaultTimeout = 15 * time.Minute
	maxTimeout     = 30 * time.Minute
	maxOutputBytes = 256 * 1024 // 256 KB
)

// commandRunner abstracts command execution for testing.
type commandRunner func(ctx context.Context, name string, arguments []string, directory string) (stdout []byte, stderr []byte, exitCode int, err error)

// defaultCommandRunner executes commands via cmdexec.Run.
func defaultCommandRunner(ctx context.Context, name string, arguments []string, directory string) ([]byte, []byte, int, error) {
	result, err := cmdexec.Run(ctx, name, arguments, cmdexec.Options{
		Directory: directory,
	})
	if err != nil {
		return result.Stdout, result.Stderr, result.ExitCode, err
	}
	return result.Stdout, result.Stderr, result.ExitCode, nil
}

// codexTool delegates complex coding tasks to Codex in headless mode.
type codexTool struct {
	binaryPath string
	runner     commandRunner
}

// configurationFromContext reads the Codex tool configuration from the store.
func configurationFromContext(ctx context.Context) (extraArguments []string, modelName string, timeout time.Duration) {
	timeout = defaultTimeout
	dataStore := store.StoreFromContextSafe(ctx)
	if dataStore == nil {
		return
	}
	_ = dataStore.Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		storedConfiguration, err := transaction.GetConfiguration(ctx, nil)
		if err != nil {
			return err
		}
		if storedConfiguration.Tools != nil && storedConfiguration.Tools.Codex != nil {
			configuration := storedConfiguration.Tools.Codex
			extraArguments = configuration.GetExtraArguments()
			modelName = configuration.GetModelName()
			if seconds := configuration.GetMaxTurnTimeoutSeconds(); seconds > 0 {
				timeout = time.Duration(seconds) * time.Second
				if timeout > maxTimeout {
					timeout = maxTimeout
				}
			}
		}
		return nil
	})
	return
}

func init() {
	tools.RegisterBuiltinTool(createTools)
}

func createTools() []tools.Tool {
	binaryPath := "codex"

	resolvedPath, err := exec.LookPath(binaryPath)
	if err != nil {
		log.Infof("Codex tools skipped: %s binary not found", binaryPath)
		return nil
	}
	log.Infof("Codex tools enabled (binary: %s)", resolvedPath)

	// Store the unresolved name so exec.LookPath is called at execution time.
	// This avoids "no such file or directory" errors when the binary auto-updates
	// and the resolved path changes while the process is running.
	return []tools.Tool{&codexTool{
		binaryPath: binaryPath,
		runner:     defaultCommandRunner,
	}}
}

func (self *codexTool) Policy(ctx context.Context, arguments string) tools.PolicyDecision {
	return tools.AllowPolicy()
}

func (self *codexTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name: "codex",
			Description: "Delegate complex coding tasks to Codex running in headless mode. " +
				"Codex can autonomously read/edit files, run commands, and reason about code. " +
				"Each invocation is a single run — there is no session resumption. " +
				"If you need continuity across multiple invocations, instruct Codex to write " +
				"a plan or progress log to a file in the workspace, then read that file in subsequent calls.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"prompt": map[string]interface{}{
						"type":        "string",
						"description": "The prompt to send to Codex.",
					},
					"systemPrompt": map[string]interface{}{
						"type":        "string",
						"description": "Additional system prompt instructions appended to Codex's system prompt.",
					},
					"workingDirectory": map[string]interface{}{
						"type":        "string",
						"description": "Working directory for the subprocess. Defaults to the user's home directory.",
					},
					"timeoutSeconds": map[string]interface{}{
						"type":        "integer",
						"description": "Per-call timeout override in seconds (default 300, max 1800).",
					},
				},
				"required": []string{"prompt"},
			},
			Returns: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"result": map[string]interface{}{
						"type":        "string",
						"description": "The text result from Codex.",
					},
					"isError": map[string]interface{}{
						"type":        "boolean",
						"description": "Whether Codex reported an error.",
					},
					"duration": map[string]interface{}{
						"type":        "number",
						"description": "Execution duration in seconds.",
					},
					"exitCode": map[string]interface{}{
						"type":        "integer",
						"description": "Subprocess exit code.",
					},
					"costUsd": map[string]interface{}{
						"type":        "number",
						"description": "Cost in USD reported by Codex.",
					},
					"inputTokens": map[string]interface{}{
						"type":        "integer",
						"description": "Number of input tokens used.",
					},
					"outputTokens": map[string]interface{}{
						"type":        "integer",
						"description": "Number of output tokens used.",
					},
					"workingDirectory": map[string]interface{}{
						"type":        "string",
						"description": "Working directory used for this Codex invocation.",
					},
					"timedOut": map[string]interface{}{
						"type":        "boolean",
						"description": "Whether the command timed out.",
					},
				},
			},
		},
	}
}

func (self *codexTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Prompt           string `json:"prompt"`
		SystemPrompt     string `json:"systemPrompt"`
		WorkingDirectory string `json:"workingDirectory"`
		TimeoutSeconds   int    `json:"timeoutSeconds"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}

	if arguments.Prompt == "" {
		return "", fmt.Errorf("prompt is required")
	}

	commandArguments := self.buildArguments(ctx, arguments.Prompt, arguments.SystemPrompt)
	return self.executeCommand(ctx, commandArguments, arguments.WorkingDirectory, arguments.TimeoutSeconds)
}

func (self *codexTool) buildArguments(ctx context.Context, prompt, systemPrompt string) []string {
	extraArguments, modelName, _ := configurationFromContext(ctx)

	if systemPrompt != "" {
		prompt = fmt.Sprintf("Additional system instructions:\n%s\n\nUser request:\n%s", systemPrompt, prompt)
	}

	arguments := []string{"exec", "--json", "--skip-git-repo-check"}

	if modelName != "" {
		arguments = append(arguments, "--model", modelName)
	}
	if len(extraArguments) > 0 {
		arguments = append(arguments, extraArguments...)
	}
	arguments = append(arguments, prompt)

	return arguments
}

func (self *codexTool) executeCommand(ctx context.Context, commandArguments []string, workingDirectory string, timeoutSeconds int) (string, error) {
	_, _, timeout := configurationFromContext(ctx)
	if timeoutSeconds > 0 {
		timeout = time.Duration(timeoutSeconds) * time.Second
		if timeout > maxTimeout {
			timeout = maxTimeout
		}
	}

	if workingDirectory == "" {
		homeDirectory, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("getting home directory: %w", err)
		}
		workingDirectory = homeDirectory
	}

	log.Debugf("exec: %s %v in %s (timeout %s)", self.binaryPath, commandArguments, workingDirectory, timeout)

	commandContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	startTime := time.Now()
	stdout, stderr, exitCode, err := self.runner(commandContext, self.binaryPath, commandArguments, workingDirectory)
	duration := time.Since(startTime).Seconds()
	timedOut := commandContext.Err() == context.DeadlineExceeded

	if err != nil && !timedOut {
		return "", fmt.Errorf("executing codex: %w (stderr: %s)", err, string(stderr))
	}

	if len(stdout) > maxOutputBytes {
		stdout = stdout[:maxOutputBytes]
	}

	return self.parseOutput(stdout, stderr, exitCode, duration, timedOut, workingDirectory)
}

// codexOutput represents the JSON output from `codex exec`.
type codexOutput struct {
	Result             string  `json:"result"`
	IsError            bool    `json:"is_error"`
	CostUSD            float64 `json:"cost_usd"`
	NumberInputTokens  int     `json:"num_input_tokens"`
	NumberOutputTokens int     `json:"num_output_tokens"`
}

func (self *codexTool) parseOutput(stdout, stderr []byte, exitCode int, duration float64, timedOut bool, workingDirectory string) (string, error) {
	var parsed codexOutput
	if err := json.Unmarshal(stdout, &parsed); err != nil {
		log.Debugf("codex output is not JSON, using raw output (parse error: %v)", err)

		rawResult := string(stdout)
		if rawResult == "" && len(stderr) > 0 {
			rawResult = string(stderr)
		}

		result, marshalError := json.Marshal(map[string]interface{}{
			"result":           rawResult,
			"isError":          exitCode != 0,
			"duration":         duration,
			"exitCode":         exitCode,
			"timedOut":         timedOut,
			"workingDirectory": workingDirectory,
		})
		if marshalError != nil {
			return "", fmt.Errorf("marshaling fallback result: %w", marshalError)
		}
		return string(result), nil
	}

	response := map[string]interface{}{
		"result":           parsed.Result,
		"isError":          parsed.IsError,
		"duration":         duration,
		"exitCode":         exitCode,
		"costUsd":          parsed.CostUSD,
		"inputTokens":      parsed.NumberInputTokens,
		"outputTokens":     parsed.NumberOutputTokens,
		"timedOut":         timedOut,
		"workingDirectory": workingDirectory,
	}

	result, err := json.Marshal(response)
	if err != nil {
		return "", fmt.Errorf("marshaling result: %w", err)
	}
	return string(result), nil
}
