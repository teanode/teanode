// Package agent exposes tools for inspecting and updating agent state.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"

	"github.com/teanode/teanode/internal/coordinators"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/prompts"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/runners"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/tools"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
)

var validAgentIdPattern = regexp.MustCompile(`^[a-z0-9_-]+$`)

func init() {
	tools.RegisterBuiltinTool(func() []tools.Tool {
		return []tools.Tool{&agentListTool{}, &agentMessageTool{}, &agentCreateTool{}, &subagentSpawnTool{}}
	})
}

func selfAgentId(ctx context.Context) string {
	runner := runners.RunnerFromContext(ctx)
	if runner != nil {
		return runner.AgentID
	}
	return ""
}

// --- agent_create ---

type agentCreateTool struct{}

func (self *agentCreateTool) Policy(ctx context.Context, arguments string) tools.PolicyDecision {
	if tools.IsAdmin(ctx) {
		return tools.AllowPolicy()
	}
	return tools.DenyPolicy("admin access required for agent_create")
}

func (self *agentCreateTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name:        "agent_create",
			Description: "Create a new agent with an ID and name so it can be messaged immediately in this same run.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"agentId": map[string]interface{}{
						"type":        "string",
						"description": "Unique agent ID. Use lowercase letters, numbers, hyphens, and underscores.",
					},
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Friendly display name for the new agent.",
					},
				},
				"required": []string{"agentId", "name"},
			},
			Returns: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"agentId": map[string]interface{}{"type": "string"},
					"name":    map[string]interface{}{"type": "string"},
					"created": map[string]interface{}{"type": "boolean"},
				},
			},
		},
	}
}

func (self *agentCreateTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		AgentID string `json:"agentId"`
		Name    string `json:"name"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}
	if arguments.AgentID == "" || arguments.Name == "" {
		return "", fmt.Errorf("agentId and name are required")
	}
	if !validAgentIdPattern.MatchString(arguments.AgentID) {
		return "", fmt.Errorf("invalid agentId %q: use lowercase letters, numbers, hyphens, and underscores", arguments.AgentID)
	}

	// Check existence and create atomically in a single transaction.
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		_, err := transaction.GetAgent(ctx, arguments.AgentID, nil)
		if err == nil {
			return fmt.Errorf("agent %q already exists", arguments.AgentID)
		}
		if err != store.ErrNotFound {
			return err
		}
		seedWorkspaceFiles := []models.WorkspaceFile{
			{Path: ptrto.Value("AGENT.md"), Content: ptrto.Value([]byte(prompts.DefaultAgentMarkdown()))},
			{Path: ptrto.Value("MEMORY.md"), Content: ptrto.Value([]byte(prompts.DefaultMemoryMarkdown()))},
			{Path: ptrto.Value("SKILLS.md"), Content: ptrto.Value([]byte(prompts.DefaultSkillsMarkdown()))},
		}
		_, createError := transaction.CreateAgent(ctx, &models.Agent{
			ID:   arguments.AgentID,
			Name: &arguments.Name,
		}, seedWorkspaceFiles, nil)
		return createError
	}); err != nil {
		return "", err
	}

	response, _ := json.Marshal(map[string]interface{}{
		"agentId": arguments.AgentID,
		"name":    arguments.Name,
		"created": true,
	})
	return string(response), nil
}

// --- agent_list ---

type agentListTool struct{}

func (self *agentListTool) Policy(ctx context.Context, arguments string) tools.PolicyDecision {
	return tools.AllowPolicy()
}

func (self *agentListTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name:        "agent_list",
			Description: "List all available agents with their capabilities, tools, and models.",
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
			Returns: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"agents": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"id":          map[string]interface{}{"type": "string", "description": "Unique agent identifier."},
								"name":        map[string]interface{}{"type": "string", "description": "Friendly display name."},
								"description": map[string]interface{}{"type": "string", "description": "What this agent specializes in."},
								"model":       map[string]interface{}{"type": "string", "description": "Qualified model (e.g. openai:gpt-5.1)."},
								"isSelf":      map[string]interface{}{"type": "boolean", "description": "True only for the calling agent."},
							},
						},
					},
				},
			},
		},
	}
}

func (self *agentListTool) Execute(ctx context.Context, _ string) (string, error) {
	currentAgentId := selfAgentId(ctx)

	agentIds := make([]string, 0)
	agentsById := make(map[string]*models.Agent)
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		agentsList, listError := transaction.ListAgents(ctx, nil)
		if listError != nil {
			return listError
		}
		for _, agent := range agentsList {
			agentsById[agent.ID] = agent
			agentIds = append(agentIds, agent.ID)
		}
		sort.Strings(agentIds)
		return nil
	}); err != nil {
		return "", err
	}

	agents := make([]map[string]interface{}, 0, len(agentIds))
	for _, agentId := range agentIds {
		entry := map[string]interface{}{
			"id": agentId,
		}
		if agent, ok := agentsById[agentId]; ok {
			if name := agent.GetName(); name != "" {
				entry["name"] = name
			}
			if description := agent.GetDescription(); description != "" {
				entry["description"] = description
			}
			if modelName := agent.GetProviderModelName(); modelName != "" {
				entry["model"] = modelName
			}
		}
		if agentId == currentAgentId {
			entry["isSelf"] = true
		}
		agents = append(agents, entry)
	}
	result, _ := json.Marshal(map[string]interface{}{
		"agents": agents,
	})
	return string(result), nil
}

// --- agent_message ---

type agentMessageTool struct{}

func (self *agentMessageTool) Policy(ctx context.Context, arguments string) tools.PolicyDecision {
	return tools.AllowPolicy()
}

func (self *agentMessageTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name:        "agent_message",
			Description: "Send a message to another agent and receive its response. The message runs synchronously — the tool returns when the target agent completes its response.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"agentId": map[string]interface{}{
						"type":        "string",
						"description": "The ID of the target agent to message. Likely the user will specify agent by name, use agent_list tool to find its ID.",
					},
					"message": map[string]interface{}{
						"type":        "string",
						"description": "The message to send to the target agent.",
					},
					"conversationId": map[string]interface{}{
						"type":        "string",
						"description": "Optional conversation id to continue a previous conversation with this agent. If omitted, a new conversation is created.",
					},
				},
				"required": []string{"agentId", "message"},
			},
			Returns: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"agentId":        map[string]interface{}{"type": "string"},
					"response":       map[string]interface{}{"type": "string"},
					"conversationId": map[string]interface{}{"type": "string"},
				},
			},
		},
	}
}

func (self *agentMessageTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	currentAgentId := selfAgentId(ctx)

	var arguments struct {
		AgentID        string `json:"agentId"`
		Message        string `json:"message"`
		ConversationID string `json:"conversationId"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}
	if arguments.AgentID == "" || arguments.Message == "" {
		return "", fmt.Errorf("agentId and message are required")
	}

	// Verify target agent exists in store.
	var targetAgent *models.Agent
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		foundAgent, err := transaction.GetAgent(ctx, arguments.AgentID, nil)
		if err != nil {
			return err
		}
		targetAgent = foundAgent
		return nil
	}); err != nil {
		return "", fmt.Errorf("looking up agent: %w", err)
	}
	if targetAgent == nil {
		return "", fmt.Errorf("agent %q not found", arguments.AgentID)
	}

	// Get coordinator from context.
	coordinator := coordinators.CoordinatorFromContext(ctx)
	if coordinator == nil {
		return "", fmt.Errorf("coordinator not available")
	}

	// Generate conversation id if not provided.
	conversationId := arguments.ConversationID
	if conversationId == "" {
		conversationId = security.NewULID()
	}

	// Prefix the message with source agent identity.
	prefixedMessage := fmt.Sprintf("[Message from agent '%s']: %s", currentAgentId, arguments.Message)

	// Run synchronously via coordinator.
	handle, sendError := coordinator.Run(ctx, coordinators.RunParameters{
		AgentID:          arguments.AgentID,
		ConversationID:   conversationId,
		Message:          prefixedMessage,
		SystemPromptMode: runners.SystemPromptModeMinimal,
	}, nil)
	if sendError != nil {
		return "", fmt.Errorf("agent %q send failed: %w", arguments.AgentID, sendError)
	}
	result, err := handle.Wait()
	if err != nil {
		return "", fmt.Errorf("agent %q run failed: %w", arguments.AgentID, err)
	}

	response, _ := json.Marshal(map[string]interface{}{
		"agentId":        arguments.AgentID,
		"response":       result.Response,
		"conversationId": conversationId,
	})
	return string(response), nil
}

// --- subagent_spawn ---

type subagentSpawnTool struct{}

func (self *subagentSpawnTool) Policy(ctx context.Context, arguments string) tools.PolicyDecision {
	return tools.AllowPolicy()
}

func (self *subagentSpawnTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name:        "subagent_spawn",
			Description: "Spawn an isolated sub-conversation to handle a subtask. The subagent runs with a fresh conversation history, executes the task, and returns the result. The subagent conversation is ephemeral and deleted after completion.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"task": map[string]interface{}{
						"type":        "string",
						"description": "The task instructions for the subagent.",
					},
					"agentId": map[string]interface{}{
						"type":        "string",
						"description": "The ID of the agent to spawn. Defaults to the current agent (self-spawn).",
					},
					"model": map[string]interface{}{
						"type":        "string",
						"description": "Optional model override for the subagent.",
					},
				},
				"required": []string{"task"},
			},
			Returns: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"agentId":        map[string]interface{}{"type": "string"},
					"response":       map[string]interface{}{"type": "string"},
					"conversationId": map[string]interface{}{"type": "string"},
					"depth":          map[string]interface{}{"type": "integer"},
				},
			},
		},
	}
}

func (self *subagentSpawnTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	currentAgentId := selfAgentId(ctx)

	var arguments struct {
		Task              string `json:"task"`
		AgentID           string `json:"agentId"`
		ProviderModelName string `json:"model"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}
	if arguments.Task == "" {
		return "", fmt.Errorf("task is required")
	}

	// Default to self-spawn.
	targetAgentId := arguments.AgentID
	if targetAgentId == "" {
		targetAgentId = currentAgentId
	}

	// Depth check.
	currentDepth := runners.SpawnDepthFromContext(ctx)
	if currentDepth >= runners.DefaultMaxSpawnDepth {
		return "", fmt.Errorf("subagent spawn depth limit reached (%d)", runners.DefaultMaxSpawnDepth)
	}

	// Verify target agent exists in store.
	var targetAgent *models.Agent
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		foundAgent, err := transaction.GetAgent(ctx, targetAgentId, nil)
		if err != nil {
			return err
		}
		targetAgent = foundAgent
		return nil
	}); err != nil {
		return "", fmt.Errorf("looking up agent: %w", err)
	}
	if targetAgent == nil {
		return "", fmt.Errorf("agent %q not found", targetAgentId)
	}

	// Get coordinator from context.
	coordinator := coordinators.CoordinatorFromContext(ctx)
	if coordinator == nil {
		return "", fmt.Errorf("coordinator not available")
	}

	// Generate ephemeral conversation id.
	conversationId := security.NewULID()

	// Build child context with incremented spawn depth.
	childContext := runners.ContextWithSpawnDepth(ctx, currentDepth+1)

	// Prefix task message with source agent identity and depth.
	prefixedTask := fmt.Sprintf("[Subagent task from '%s' (depth %d)]: %s", currentAgentId, currentDepth+1, arguments.Task)

	// Run synchronously via coordinator.
	sendParameters := coordinators.RunParameters{
		AgentID:          targetAgentId,
		ConversationID:   conversationId,
		Message:          prefixedTask,
		SystemPromptMode: runners.SystemPromptModeMinimal,
	}
	if arguments.ProviderModelName != "" {
		sendParameters.ProviderModelName = arguments.ProviderModelName
	}

	handle, sendError := coordinator.Run(childContext, sendParameters, nil)
	if sendError != nil {
		return "", fmt.Errorf("subagent %q send failed: %w", targetAgentId, sendError)
	}
	result, err := handle.Wait()
	if err != nil {
		return "", fmt.Errorf("subagent %q run failed: %w", targetAgentId, err)
	}

	response, _ := json.Marshal(map[string]interface{}{
		"agentId":        targetAgentId,
		"response":       result.Response,
		"conversationId": conversationId,
		"depth":          currentDepth + 1,
	})
	return string(response), nil
}
