package claudecode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/op/go-logging"
	"github.com/teanode/teanode/internal/runners"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/store"
	toolregistry "github.com/teanode/teanode/internal/tools"
)

var log = logging.MustGetLogger("claudecode")

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
	command.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

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

// claudeCodeTool delegates complex coding tasks to Claude Code in headless mode.
type claudeCodeTool struct {
	binaryPath string
	runner     commandRunner
	sessions   map[string]*sessionInfo
	mutex      sync.Mutex
}

// configFromContext reads the Claude Code tool configuration from the store.
func configFromContext(ctx context.Context) (allowedTools []string, model string, timeout time.Duration) {
	allowedTools = DefaultAllowedTools
	timeout = defaultTimeout
	dataStore := store.StoreFromContextSafe(ctx)
	if dataStore == nil {
		return
	}
	_ = dataStore.Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		configuration, err := transaction.GetConfiguration(ctx, nil)
		if err != nil {
			return err
		}
		if configuration.Tools != nil && configuration.Tools.ClaudeCode != nil {
			config := configuration.Tools.ClaudeCode
			if tools := config.GetAllowedTools(); len(tools) > 0 {
				allowedTools = tools
			}
			model = config.GetModel()
			if seconds := config.GetMaxTurnTimeoutSeconds(); seconds > 0 {
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

// RegisterTools adds the claude_code tool to the registry.
// If the claude binary is not found, no tools are registered.
func RegisterTools(registry *toolregistry.ToolRegistry) {
	binaryPath := "claude"

	resolvedPath, err := exec.LookPath(binaryPath)
	if err != nil {
		log.Infof("Claude Code tools skipped: %s binary not found", binaryPath)
		return
	}
	log.Infof("Claude Code tools enabled (binary: %s)", resolvedPath)

	registry.Register(&claudeCodeTool{
		binaryPath: resolvedPath,
		runner:     defaultCommandRunner,
		sessions:   make(map[string]*sessionInfo),
	})
}

func (self *claudeCodeTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name: "claude_code",
			Description: "Delegate complex coding tasks to Claude Code running in headless mode. " +
				"Claude Code can autonomously read/edit files, run commands, and reason about code. " +
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
						"description": "The prompt to send to Claude Code (required for run and resume).",
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
						"description": "Additional system prompt instructions appended to Claude Code's system prompt.",
					},
					"workingDirectory": map[string]interface{}{
						"type":        "string",
						"description": "Working directory for the subprocess. Defaults to the user's home directory.",
					},
					"timeoutSeconds": map[string]interface{}{
						"type":        "integer",
						"description": "Per-call timeout override in seconds (default 300, max 600).",
					},
				},
				"required": []string{"action"},
			},
			Returns: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"sessionId": map[string]interface{}{
						"type":        "string",
						"description": "The Claude Code session ID.",
					},
					"result": map[string]interface{}{
						"type":        "string",
						"description": "The text result from Claude Code.",
					},
					"isError": map[string]interface{}{
						"type":        "boolean",
						"description": "Whether Claude Code reported an error.",
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
						"description": "Cost in USD reported by Claude Code.",
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
						"description": "Working directory used for this Claude Code invocation.",
					},
					"resume": map[string]interface{}{
						"type":        "object",
						"description": "Canonical payload to continue this session in a later claude_code tool call.",
						"properties": map[string]interface{}{
							"action":           map[string]interface{}{"type": "string"},
							"sessionId":        map[string]interface{}{"type": "string"},
							"workingDirectory": map[string]interface{}{"type": "string"},
						},
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

func (self *claudeCodeTool) Execute(ctx context.Context, rawArguments string) (string, error) {
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
		return self.executeListSessions(ctx)
	default:
		return "", fmt.Errorf("unknown claude_code action: %q (valid: run, resume, list_sessions)", arguments.Action)
	}
}

func (self *claudeCodeTool) executeRun(ctx context.Context, prompt, systemPrompt, workingDirectory string, timeoutSeconds int, forceNewSession bool) (string, error) {
	if prompt == "" {
		return "", fmt.Errorf("prompt is required for run action")
	}
	if self.hasTrackedSessions() && !forceNewSession {
		return "", fmt.Errorf("existing session(s) detected — use action=resume with sessionId (see list_sessions), or set forceNewSession=true to start a new session")
	}

	commandArguments := self.buildArguments(ctx, prompt, "", systemPrompt)
	return self.executeCommand(ctx, commandArguments, workingDirectory, timeoutSeconds)
}

func (self *claudeCodeTool) hasTrackedSessions() bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return len(self.sessions) > 0
}

func (self *claudeCodeTool) executeResume(ctx context.Context, sessionId, prompt, systemPrompt, workingDirectory string, timeoutSeconds int) (string, error) {
	if sessionId == "" {
		return "", fmt.Errorf("sessionId is required for resume action")
	}
	if prompt == "" {
		return "", fmt.Errorf("prompt is required for resume action")
	}

	commandArguments := self.buildArguments(ctx, prompt, sessionId, systemPrompt)
	return self.executeCommand(ctx, commandArguments, workingDirectory, timeoutSeconds)
}

func (self *claudeCodeTool) executeListSessions(ctx context.Context) (string, error) {
	userId, agentId, scopeError := self.resolveConversationScope(ctx)
	if scopeError == nil {
		sessions, err := self.loadSessionsFromConversationStore(ctx, userId, agentId)
		if err == nil {
			result, marshalErr := json.Marshal(map[string]interface{}{"sessions": sessions})
			if marshalErr != nil {
				return "", fmt.Errorf("marshaling sessions: %w", marshalErr)
			}
			return string(result), nil
		}
		log.Debugf("list_sessions: failed to load from conversation history, falling back to in-memory sessions: %v", err)
	} else {
		log.Debugf("list_sessions: no conversation scope available, falling back to in-memory sessions: %v", scopeError)
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	sessionList := make([]sessionInfo, 0, len(self.sessions))
	for _, session := range self.sessions {
		sessionList = append(sessionList, *session)
	}
	sort.Slice(sessionList, func(left, right int) bool {
		if !sessionList[left].LastUsedAt.Equal(sessionList[right].LastUsedAt) {
			return sessionList[left].LastUsedAt.After(sessionList[right].LastUsedAt)
		}
		return sessionList[left].SessionID < sessionList[right].SessionID
	})

	result, err := json.Marshal(map[string]interface{}{
		"sessions": sessionList,
	})
	if err != nil {
		return "", fmt.Errorf("marshaling sessions: %w", err)
	}
	return string(result), nil
}

func (self *claudeCodeTool) resolveConversationScope(ctx context.Context) (string, string, error) {
	runner := runners.RunnerFromContext(ctx)
	if runner == nil {
		return "", "", fmt.Errorf("runner context missing")
	}
	user := models.UserFromContext(ctx)
	if user == nil || user.ID == "" {
		return "", "", fmt.Errorf("user context missing")
	}
	return user.ID, runner.AgentID, nil
}

func (self *claudeCodeTool) loadSessionsFromConversationStore(ctx context.Context, userId, agentId string) ([]sessionInfo, error) {
	conversationList := make([]*models.Conversation, 0)
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		items, listError := transaction.ListConversations(ctx, store.ConversationListOptions{
			UserID:  &userId,
			AgentID: &agentId,
		}, nil)
		if listError != nil {
			return listError
		}
		conversationList = append(conversationList, items...)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("listing conversations: %w", err)
	}

	sessionsById := make(map[string]*sessionInfo)
	for _, conversation := range conversationList {
		messages := make([]*models.ConversationMessage, 0)
		if loadError := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
			items, err := transaction.ListConversationMessages(ctx, conversation.ID, nil)
			if err != nil {
				return err
			}
			messages = append(messages, items...)
			return nil
		}); loadError != nil {
			log.Debugf("list_sessions: skipping conversation %q (load error: %v)", conversation.ID, loadError)
			continue
		}
		for _, message := range messages {
			if message.Role == nil || string(*message.Role) != "tool" || message.ToolName == nil || *message.ToolName != "claude_code" {
				continue
			}
			content := ""
			if len(message.Content) > 0 {
				_ = json.Unmarshal(message.Content, &content)
			}
			sessionId := extractSessionIdFromToolResult(content)
			if sessionId == "" {
				continue
			}

			timestamp := time.Now()
			if message.CreatedAt != nil {
				timestamp = *message.CreatedAt
			}
			existing := sessionsById[sessionId]
			if existing == nil {
				sessionsById[sessionId] = &sessionInfo{
					SessionID:  sessionId,
					CreatedAt:  timestamp,
					LastUsedAt: timestamp,
					TurnCount:  1,
				}
				continue
			}
			if timestamp.Before(existing.CreatedAt) {
				existing.CreatedAt = timestamp
			}
			if timestamp.After(existing.LastUsedAt) {
				existing.LastUsedAt = timestamp
			}
			existing.TurnCount++
		}
	}

	sessionList := make([]sessionInfo, 0, len(sessionsById))
	for _, session := range sessionsById {
		sessionList = append(sessionList, *session)
	}
	sort.Slice(sessionList, func(left, right int) bool {
		if !sessionList[left].LastUsedAt.Equal(sessionList[right].LastUsedAt) {
			return sessionList[left].LastUsedAt.After(sessionList[right].LastUsedAt)
		}
		return sessionList[left].SessionID < sessionList[right].SessionID
	})
	return sessionList, nil
}

func extractSessionIdFromToolResult(result string) string {
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		return ""
	}

	if sessionId, ok := payload["sessionId"].(string); ok && sessionId != "" {
		return sessionId
	}
	if sessionId, ok := payload["session_id"].(string); ok && sessionId != "" {
		return sessionId
	}
	if resume, ok := payload["resume"].(map[string]interface{}); ok {
		if sessionId, ok := resume["sessionId"].(string); ok && sessionId != "" {
			return sessionId
		}
	}
	return ""
}

func (self *claudeCodeTool) buildArguments(ctx context.Context, prompt, sessionId, systemPrompt string) []string {
	allowedTools, model, _ := configFromContext(ctx)

	arguments := []string{"-p", prompt, "--output-format", "json"}

	if sessionId != "" {
		arguments = append(arguments, "--resume", sessionId)
	}

	if model != "" {
		arguments = append(arguments, "--model", model)
	}

	// Always pass --allowedTools to prevent interactive tool approval prompts.
	arguments = append(arguments, "--allowedTools")
	arguments = append(arguments, allowedTools...)

	if systemPrompt != "" {
		arguments = append(arguments, "--append-system-prompt", systemPrompt)
	}

	return arguments
}

func (self *claudeCodeTool) executeCommand(ctx context.Context, commandArguments []string, workingDirectory string, timeoutSeconds int) (string, error) {
	// Resolve timeout.
	_, _, configTimeout := configFromContext(ctx)
	timeout := configTimeout
	if timeoutSeconds > 0 {
		timeout = time.Duration(timeoutSeconds) * time.Second
		if timeout > maxTimeout {
			timeout = maxTimeout
		}
	}

	// Resolve working directory.
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
		return "", fmt.Errorf("executing claude: %w (stderr: %s)", err, string(stderr))
	}

	// Truncate output if needed.
	if len(stdout) > maxOutputBytes {
		stdout = stdout[:maxOutputBytes]
	}

	// Try to parse Claude Code's JSON output.
	return self.parseOutput(stdout, stderr, exitCode, duration, timedOut, workingDirectory)
}

// claudeCodeOutput represents the JSON output from `claude -p --output-format json`.
type claudeCodeOutput struct {
	Result          string  `json:"result"`
	SessionID       string  `json:"session_id"`
	IsError         bool    `json:"is_error"`
	CostUSD         float64 `json:"cost_usd"`
	NumInputTokens  int     `json:"num_input_tokens"`
	NumOutputTokens int     `json:"num_output_tokens"`
}

func (self *claudeCodeTool) parseOutput(stdout, stderr []byte, exitCode int, duration float64, timedOut bool, workingDirectory string) (string, error) {
	var parsed claudeCodeOutput
	if err := json.Unmarshal(stdout, &parsed); err != nil {
		// Fallback: return raw stdout as result if JSON parsing fails.
		log.Debugf("claude output is not JSON, using raw output (parse error: %v)", err)

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

	// Track session.
	if parsed.SessionID != "" {
		self.trackSession(parsed.SessionID)
	}

	response := map[string]interface{}{
		"sessionId":        parsed.SessionID,
		"result":           parsed.Result,
		"isError":          parsed.IsError,
		"duration":         duration,
		"exitCode":         exitCode,
		"costUsd":          parsed.CostUSD,
		"inputTokens":      parsed.NumInputTokens,
		"outputTokens":     parsed.NumOutputTokens,
		"timedOut":         timedOut,
		"workingDirectory": workingDirectory,
	}
	if parsed.SessionID != "" {
		response["resume"] = map[string]interface{}{
			"action":           "resume",
			"sessionId":        parsed.SessionID,
			"workingDirectory": workingDirectory,
		}
	}

	result, err := json.Marshal(response)
	if err != nil {
		return "", fmt.Errorf("marshaling result: %w", err)
	}
	return string(result), nil
}

func (self *claudeCodeTool) trackSession(sessionId string) {
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
