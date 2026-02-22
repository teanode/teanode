package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/op/go-logging"
	"github.com/teanode/teanode/internal/agents"
	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/providers"
)

var log = logging.MustGetLogger("codex")

const (
	defaultTimeout = 15 * time.Minute
	maxTimeout     = 30 * time.Minute
	maxOutputBytes = 256 * 1024 // 256 KB
)

// DefaultAllowedTools are safe non-interactive tools that won't prompt for approval.
var DefaultAllowedTools = []string{
	"Bash", "Read", "Edit", "Write", "Glob", "Grep", "WebFetch", "WebSearch",
}

// commandRunner abstracts command execution for testing.
type commandRunner func(ctx context.Context, name string, args []string, directory string) (stdout []byte, stderr []byte, exitCode int, err error)

// defaultCommandRunner executes commands via os/exec.
func defaultCommandRunner(ctx context.Context, name string, args []string, directory string) ([]byte, []byte, int, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = directory
	command.Stdin = nil
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	var stdoutBuffer, stderrBuffer bytes.Buffer
	command.Stdout = &stdoutBuffer
	command.Stderr = &stderrBuffer

	err := command.Run()

	exitCode := 0
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			exitCode = exitError.ExitCode()
		} else {
			return stdoutBuffer.Bytes(), stderrBuffer.Bytes(), -1, err
		}
	}

	return stdoutBuffer.Bytes(), stderrBuffer.Bytes(), exitCode, nil
}

// sessionInfo tracks an in-memory session for convenience.
type sessionInfo struct {
	SessionID  string    `json:"sessionId"`
	CreatedAt  time.Time `json:"createdAt"`
	LastUsedAt time.Time `json:"lastUsedAt"`
	TurnCount  int       `json:"turnCount"`
}

// codexTool delegates complex coding tasks to Codex in headless mode.
type codexTool struct {
	binaryPath   string
	allowedTools []string
	extraArgs    []string
	model        string
	timeout      time.Duration
	runner       commandRunner
	sessions     map[string]*sessionInfo
	mutex        sync.Mutex
}

// RegisterTools adds the codex tool to the registry.
// If the codex binary is not found, no tools are registered.
// A nil config is treated as "use defaults" — tools are registered
// as long as the binary is present on PATH.
func RegisterTools(registry *agents.ToolRegistry, config *configs.CodexConfig) {
	binaryPath := "codex"
	allowedTools := DefaultAllowedTools
	var extraArgs []string
	var model string
	timeout := defaultTimeout

	if config != nil {
		if config.BinaryPath != "" {
			binaryPath = config.BinaryPath
		}
		if len(config.AllowedTools) > 0 {
			allowedTools = config.AllowedTools
		}
		if len(config.ExtraArgs) > 0 {
			extraArgs = append(extraArgs, config.ExtraArgs...)
		}
		model = config.Model
		if config.MaxTurnTimeoutSeconds > 0 {
			timeout = time.Duration(config.MaxTurnTimeoutSeconds) * time.Second
			if timeout > maxTimeout {
				timeout = maxTimeout
			}
		}
	}

	resolvedPath, err := exec.LookPath(binaryPath)
	if err != nil {
		log.Infof("Codex tools skipped: %s binary not found", binaryPath)
		return
	}
	log.Infof("Codex tools enabled (binary: %s)", resolvedPath)

	registry.Register(&codexTool{
		binaryPath:   resolvedPath,
		allowedTools: allowedTools,
		extraArgs:    extraArgs,
		model:        model,
		timeout:      timeout,
		runner:       defaultCommandRunner,
		sessions:     make(map[string]*sessionInfo),
	})
}

func (self *codexTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name: "codex",
			Description: "Delegate complex coding tasks to Codex running in headless mode. " +
				"Codex can autonomously read/edit files, run commands, and reason about code. " +
				"Actions: run (start a new task; when a tracked session exists, prefer resume unless forceNewSession=true), " +
				"resume (continue a previous session), list_sessions (list tracked sessions).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"run", "resume", "list_sessions"},
						"description": "The action to perform.",
					},
					"prompt": map[string]interface{}{
						"type":        "string",
						"description": "The prompt to send to Codex (required for run and resume).",
					},
					"sessionId": map[string]interface{}{
						"type":        "string",
						"description": "Session ID to resume (required for resume action).",
					},
					"forceNewSession": map[string]interface{}{
						"type":        "boolean",
						"description": "Only for run action. Set true to intentionally create a new session when one already exists.",
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
				"required": []string{"action"},
			},
			Returns: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"sessionId": map[string]interface{}{
						"type":        "string",
						"description": "The Codex session ID.",
					},
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
					"timedOut": map[string]interface{}{
						"type":        "boolean",
						"description": "Whether the command timed out.",
					},
					"sessions": map[string]interface{}{
						"type":        "array",
						"description": "List of tracked sessions (for list_sessions action).",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"sessionId":  map[string]interface{}{"type": "string"},
								"createdAt":  map[string]interface{}{"type": "string"},
								"lastUsedAt": map[string]interface{}{"type": "string"},
								"turnCount":  map[string]interface{}{"type": "integer"},
							},
						},
					},
				},
			},
		},
	}
}

func (self *codexTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Action           string `json:"action"`
		Prompt           string `json:"prompt"`
		SessionID        string `json:"sessionId"`
		ForceNewSession  bool   `json:"forceNewSession"`
		SystemPrompt     string `json:"systemPrompt"`
		WorkingDirectory string `json:"workingDirectory"`
		TimeoutSeconds   int    `json:"timeoutSeconds"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}

	switch arguments.Action {
	case "run":
		return self.executeRun(ctx, arguments.Prompt, arguments.SystemPrompt, arguments.WorkingDirectory, arguments.TimeoutSeconds, arguments.ForceNewSession)
	case "resume":
		return self.executeResume(ctx, arguments.SessionID, arguments.Prompt, arguments.SystemPrompt, arguments.WorkingDirectory, arguments.TimeoutSeconds)
	case "list_sessions":
		return self.executeListSessions()
	default:
		return "", fmt.Errorf("unknown codex action: %q (valid: run, resume, list_sessions)", arguments.Action)
	}
}

func (self *codexTool) executeRun(ctx context.Context, prompt, systemPrompt, workingDirectory string, timeoutSeconds int, forceNewSession bool) (string, error) {
	if prompt == "" {
		return "", fmt.Errorf("prompt is required for run action")
	}
	if self.hasTrackedSessions() && !forceNewSession {
		return "", fmt.Errorf("existing session(s) detected — use action=resume with sessionId (see list_sessions), or set forceNewSession=true to start a new session")
	}

	commandArguments := self.buildArguments(prompt, "", systemPrompt)
	return self.executeCommand(ctx, commandArguments, workingDirectory, timeoutSeconds)
}

func (self *codexTool) hasTrackedSessions() bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return len(self.sessions) > 0
}

func (self *codexTool) executeResume(ctx context.Context, sessionId, prompt, systemPrompt, workingDirectory string, timeoutSeconds int) (string, error) {
	if sessionId == "" {
		return "", fmt.Errorf("sessionId is required for resume action")
	}
	if prompt == "" {
		return "", fmt.Errorf("prompt is required for resume action")
	}

	self.mutex.Lock()
	_, exists := self.sessions[sessionId]
	self.mutex.Unlock()
	if !exists {
		return "", fmt.Errorf("unknown session %q — use list_sessions to see tracked sessions, or use run to start a new session", sessionId)
	}

	commandArguments := self.buildArguments(prompt, sessionId, systemPrompt)
	return self.executeCommand(ctx, commandArguments, workingDirectory, timeoutSeconds)
}

func (self *codexTool) executeListSessions() (string, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	sessionList := make([]sessionInfo, 0, len(self.sessions))
	for _, session := range self.sessions {
		sessionList = append(sessionList, *session)
	}

	result, err := json.Marshal(map[string]interface{}{"sessions": sessionList})
	if err != nil {
		return "", fmt.Errorf("marshaling sessions: %w", err)
	}
	return string(result), nil
}

func (self *codexTool) buildArguments(prompt, sessionId, systemPrompt string) []string {
	if systemPrompt != "" {
		prompt = fmt.Sprintf("Additional system instructions:\n%s\n\nUser request:\n%s", systemPrompt, prompt)
	}

	arguments := []string{"exec"}
	if sessionId != "" {
		arguments = append(arguments, "resume")
	}

	arguments = append(arguments, "--json", "--skip-git-repo-check")

	if self.model != "" {
		arguments = append(arguments, "--model", self.model)
	}
	if len(self.extraArgs) > 0 {
		arguments = append(arguments, self.extraArgs...)
	}
	if sessionId != "" {
		arguments = append(arguments, sessionId)
	}
	arguments = append(arguments, prompt)

	return arguments
}

func (self *codexTool) executeCommand(ctx context.Context, commandArguments []string, workingDirectory string, timeoutSeconds int) (string, error) {
	timeout := self.timeout
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

	return self.parseOutput(stdout, stderr, exitCode, duration, timedOut)
}

// codexOutput represents the JSON output from `codex -p`.
type codexOutput struct {
	Result          string  `json:"result"`
	SessionID       string  `json:"session_id"`
	IsError         bool    `json:"is_error"`
	CostUSD         float64 `json:"cost_usd"`
	NumInputTokens  int     `json:"num_input_tokens"`
	NumOutputTokens int     `json:"num_output_tokens"`
}

func (self *codexTool) parseOutput(stdout, stderr []byte, exitCode int, duration float64, timedOut bool) (string, error) {
	var parsed codexOutput
	if err := json.Unmarshal(stdout, &parsed); err != nil {
		log.Debugf("codex output is not JSON, using raw output (parse error: %v)", err)

		rawResult := strings.TrimSpace(string(stdout))
		if rawResult == "" && len(stderr) > 0 {
			rawResult = strings.TrimSpace(string(stderr))
		}

		result, marshalError := json.Marshal(map[string]interface{}{
			"result":   rawResult,
			"isError":  exitCode != 0,
			"duration": duration,
			"exitCode": exitCode,
			"timedOut": timedOut,
		})
		if marshalError != nil {
			return "", fmt.Errorf("marshaling fallback result: %w", marshalError)
		}
		return string(result), nil
	}

	if parsed.SessionID != "" {
		self.trackSession(parsed.SessionID)
	}

	result, err := json.Marshal(map[string]interface{}{
		"sessionId":    parsed.SessionID,
		"result":       parsed.Result,
		"isError":      parsed.IsError,
		"duration":     duration,
		"exitCode":     exitCode,
		"costUsd":      parsed.CostUSD,
		"inputTokens":  parsed.NumInputTokens,
		"outputTokens": parsed.NumOutputTokens,
		"timedOut":     timedOut,
	})
	if err != nil {
		return "", fmt.Errorf("marshaling result: %w", err)
	}
	return string(result), nil
}

func (self *codexTool) trackSession(sessionId string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	now := time.Now()
	session, exists := self.sessions[sessionId]
	if !exists {
		self.sessions[sessionId] = &sessionInfo{
			SessionID:  sessionId,
			CreatedAt:  now,
			LastUsedAt: now,
			TurnCount:  1,
		}
		return
	}
	session.LastUsedAt = now
	session.TurnCount++
}
