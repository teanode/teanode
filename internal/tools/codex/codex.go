package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"sync"
	"time"

	"github.com/op/go-logging"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/runners"
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

// DefaultAllowedTools are safe non-interactive tools that won't prompt for approval.
var DefaultAllowedTools = []string{
	"Bash", "Read", "Edit", "Write", "Glob", "Grep", "WebFetch", "WebSearch",
}

// commandRunner abstracts command execution for testing.
type commandRunner func(ctx context.Context, name string, args []string, directory string) (stdout []byte, stderr []byte, exitCode int, err error)

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

// sessionInfo tracks an in-memory session for convenience.
type sessionInfo struct {
	SessionID  string    `json:"sessionId"`
	CreatedAt  time.Time `json:"createdAt"`
	LastUsedAt time.Time `json:"lastUsedAt"`
	TurnCount  int       `json:"turnCount"`
}

// codexTool delegates complex coding tasks to Codex in headless mode.
type codexTool struct {
	binaryPath string
	runner     commandRunner
	sessions   map[string]*sessionInfo
	mutex      sync.Mutex
}

// configFromContext reads the Codex tool configuration from the store.
func configFromContext(ctx context.Context) (extraArgs []string, model string, timeout time.Duration) {
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
		if configuration.Tools != nil && configuration.Tools.Codex != nil {
			config := configuration.Tools.Codex
			extraArgs = config.GetExtraArgs()
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

	return []tools.Tool{&codexTool{
		binaryPath: resolvedPath,
		runner:     defaultCommandRunner,
		sessions:   make(map[string]*sessionInfo),
	}}
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
					"workingDirectory": map[string]interface{}{
						"type":        "string",
						"description": "Working directory used for this Codex invocation.",
					},
					"resume": map[string]interface{}{
						"type":        "object",
						"description": "Canonical payload to continue this session in a later codex tool call.",
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
		return self.executeListSessions(ctx)
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

	commandArguments := self.buildArguments(ctx, prompt, "", systemPrompt)
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

	commandArguments := self.buildArguments(ctx, prompt, sessionId, systemPrompt)
	return self.executeCommand(ctx, commandArguments, workingDirectory, timeoutSeconds)
}

func (self *codexTool) executeListSessions(ctx context.Context) (string, error) {
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

	result, err := json.Marshal(map[string]interface{}{"sessions": sessionList})
	if err != nil {
		return "", fmt.Errorf("marshaling sessions: %w", err)
	}
	return string(result), nil
}

func (self *codexTool) resolveConversationScope(ctx context.Context) (string, string, error) {
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

func (self *codexTool) loadSessionsFromConversationStore(ctx context.Context, userId, agentId string) ([]sessionInfo, error) {
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
			if message.Role == nil || string(*message.Role) != "tool" || message.ToolName == nil || *message.ToolName != "codex" {
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

func (self *codexTool) buildArguments(ctx context.Context, prompt, sessionId, systemPrompt string) []string {
	extraArgs, model, _ := configFromContext(ctx)

	if systemPrompt != "" {
		prompt = fmt.Sprintf("Additional system instructions:\n%s\n\nUser request:\n%s", systemPrompt, prompt)
	}

	arguments := []string{"exec"}
	if sessionId != "" {
		arguments = append(arguments, "resume")
	}

	arguments = append(arguments, "--json", "--skip-git-repo-check")

	if model != "" {
		arguments = append(arguments, "--model", model)
	}
	if len(extraArgs) > 0 {
		arguments = append(arguments, extraArgs...)
	}
	if sessionId != "" {
		arguments = append(arguments, sessionId)
	}
	arguments = append(arguments, prompt)

	return arguments
}

func (self *codexTool) executeCommand(ctx context.Context, commandArguments []string, workingDirectory string, timeoutSeconds int) (string, error) {
	_, _, timeout := configFromContext(ctx)
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

// codexOutput represents the JSON output from `codex -p`.
type codexOutput struct {
	Result          string  `json:"result"`
	SessionID       string  `json:"session_id"`
	IsError         bool    `json:"is_error"`
	CostUSD         float64 `json:"cost_usd"`
	NumInputTokens  int     `json:"num_input_tokens"`
	NumOutputTokens int     `json:"num_output_tokens"`
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
