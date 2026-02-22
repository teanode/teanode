package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/util/security"
)

var validAgentIdPattern = regexp.MustCompile(`^[a-z0-9_-]+$`)

// RegisterInterAgentTools adds agent_list and agent_message tools to the registry.
func RegisterInterAgentTools(registry *ToolRegistry, selfAgentId string, agentRegistry *AgentRegistry, configuration *configs.Config) {
	agentConfig := configuration.AgentByID(selfAgentId)
	if agentConfig == nil {
		return
	}

	registry.Register(&agentListTool{
		selfAgentId:   selfAgentId,
		agentRegistry: agentRegistry,
		configuration: configuration,
	})
	registry.Register(&agentMessageTool{
		selfAgentId:   selfAgentId,
		agentRegistry: agentRegistry,
		configuration: configuration,
	})
	registry.Register(&agentCreateTool{
		selfAgentId:   selfAgentId,
		agentRegistry: agentRegistry,
		configuration: configuration,
	})
	registry.Register(&subagentSpawnTool{
		selfAgentId:   selfAgentId,
		agentRegistry: agentRegistry,
		configuration: configuration,
	})
}

// --- agent_create ---

type agentCreateTool struct {
	selfAgentId   string
	agentRegistry *AgentRegistry
	configuration *configs.Config
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

func (self *agentCreateTool) Execute(_ context.Context, rawArguments string) (string, error) {
	var arguments struct {
		AgentID string `json:"agentId"`
		Name    string `json:"name"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}
	arguments.AgentID = strings.TrimSpace(arguments.AgentID)
	arguments.Name = strings.TrimSpace(arguments.Name)
	if arguments.AgentID == "" || arguments.Name == "" {
		return "", fmt.Errorf("agentId and name are required")
	}
	if !validAgentIdPattern.MatchString(arguments.AgentID) {
		return "", fmt.Errorf("invalid agentId %q: use lowercase letters, numbers, hyphens, and underscores", arguments.AgentID)
	}
	if self.agentRegistry.Get(arguments.AgentID) != nil {
		return "", fmt.Errorf("agent %q already exists", arguments.AgentID)
	}

	agentConfig := configs.AgentConfig{
		ID:   arguments.AgentID,
		Name: arguments.Name,
	}
	if err := self.agentRegistry.CreateAgent(agentConfig); err != nil {
		return "", fmt.Errorf("creating agent %q: %w", arguments.AgentID, err)
	}

	response, _ := json.Marshal(map[string]interface{}{
		"agentId": arguments.AgentID,
		"name":    arguments.Name,
		"created": true,
	})
	return string(response), nil
}

// --- agent_list ---

type agentListTool struct {
	selfAgentId   string
	agentRegistry *AgentRegistry
	configuration *configs.Config
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
								"tools":       map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Tool names available to this agent."},
								"isSelf":      map[string]interface{}{"type": "boolean", "description": "True only for the calling agent."},
							},
						},
					},
				},
			},
		},
	}
}

func (self *agentListTool) Execute(_ context.Context, _ string) (string, error) {
	agentIds := self.agentRegistry.AgentIDs()
	agents := make([]map[string]interface{}, 0, len(agentIds))
	for _, agentId := range agentIds {
		entry := map[string]interface{}{
			"id": agentId,
		}
		if agentConfig := self.configuration.AgentByID(agentId); agentConfig != nil {
			if agentConfig.Name != "" {
				entry["name"] = agentConfig.Name
			}
		}
		if state, err := configs.LoadAgentState(agentId); err == nil && state.Description != "" {
			entry["description"] = state.Description
		}
		entry["model"] = self.configuration.AgentModel(agentId)
		if runner := self.agentRegistry.Get(agentId); runner != nil {
			_, _, tools, _, _, _ := runner.Snapshot()
			entry["tools"] = tools.Names()
		}
		if agentId == self.selfAgentId {
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

type agentMessageTool struct {
	selfAgentId   string
	agentRegistry *AgentRegistry
	configuration *configs.Config
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

	// Resolve target runner.
	targetRunner := self.agentRegistry.Get(arguments.AgentID)
	if targetRunner == nil {
		return "", fmt.Errorf("agent %q not found", arguments.AgentID)
	}

	// Generate conversation id if not provided.
	conversationId := arguments.ConversationID
	if conversationId == "" {
		conversationId = security.NewULID()
	}

	// Prefix the message with source agent identity.
	prefixedMessage := fmt.Sprintf("[Message from agent '%s']: %s", self.selfAgentId, arguments.Message)

	// Run synchronously against the target agent.
	result, err := targetRunner.Run(ctx, RunParams{
		ConversationID: conversationId,
		Message:        prefixedMessage,
	}, nil)
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

type subagentSpawnTool struct {
	selfAgentId   string
	agentRegistry *AgentRegistry
	configuration *configs.Config
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
	var arguments struct {
		Task    string `json:"task"`
		AgentID string `json:"agentId"`
		Model   string `json:"model"`
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
		targetAgentId = self.selfAgentId
	}

	// Depth check.
	currentDepth := SpawnDepthFromContext(ctx)
	if currentDepth >= DefaultMaxSpawnDepth {
		return "", fmt.Errorf("subagent spawn depth limit reached (%d)", DefaultMaxSpawnDepth)
	}

	// Resolve target runner.
	targetRunner := self.agentRegistry.Get(targetAgentId)
	if targetRunner == nil {
		return "", fmt.Errorf("agent %q not found", targetAgentId)
	}

	// Generate ephemeral conversation id.
	conversationId := security.NewULID()

	// Build child context with incremented spawn depth.
	childContext := ContextWithSpawnDepth(ctx, currentDepth+1)

	// Prefix task message with source agent identity and depth.
	prefixedTask := fmt.Sprintf("[Subagent task from '%s' (depth %d)]: %s", self.selfAgentId, currentDepth+1, arguments.Task)

	// Run synchronously against the target agent.
	runParams := RunParams{
		ConversationID: conversationId,
		Message:        prefixedTask,
	}
	if arguments.Model != "" {
		runParams.Model = arguments.Model
	}

	result, err := targetRunner.Run(childContext, runParams, nil)

	// Always clean up the ephemeral conversation, even on error.
	_ = targetRunner.Conversations.Delete(conversationId)

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
