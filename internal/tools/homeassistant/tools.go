package homeassistant

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/teanode/teanode/internal/providers"
)

const maxHistoryEntries = 50

// homeAssistantTool implements the consolidated home_assistant tool.
type homeAssistantTool struct{}

// homeAssistantExecution holds per-call client and access checker
// built from the current store configuration.
type homeAssistantExecution struct {
	client  Client
	checker *AccessChecker
}

func (self *homeAssistantTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name: "home_assistant",
			Description: "Interact with Home Assistant smart home. Actions: list_entities (list all entities, optionally filtered by domain), " +
				"get_state (get current state of an entity), control (turn on/off/toggle a device), " +
				"trigger_scene (activate a scene), list_areas (list configured areas), " +
				"get_history (get state history for an entity).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"list_entities", "get_state", "control", "trigger_scene", "list_areas", "get_history"},
						"description": "The Home Assistant action to perform.",
					},
					"domain": map[string]interface{}{
						"type":        "string",
						"description": "Entity domain to filter by (e.g. \"light\", \"switch\", \"sensor\"). Used with list_entities.",
					},
					"entityId": map[string]interface{}{
						"type":        "string",
						"description": "The entity ID (e.g. \"light.living_room\", \"scene.movie_night\"). Required for get_state, control, trigger_scene, get_history.",
					},
					"command": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"turn_on", "turn_off", "toggle"},
						"description": "The command to execute (for control action).",
					},
					"attributes": map[string]interface{}{
						"type":        "object",
						"description": "Additional service data attributes (e.g. {\"brightness\": 128, \"color_temp\": 400}). Used with control.",
					},
					"hours": map[string]interface{}{
						"type":        "integer",
						"description": "Number of hours of history to retrieve (for get_history action, default 1).",
					},
				},
				"required": []string{"action"},
			},
			Returns: map[string]interface{}{
				"type":        "object",
				"description": "Action-dependent JSON result.",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"description": "The action that was performed.",
					},
					"entities": map[string]interface{}{
						"type":        "array",
						"description": "List of entities (list_entities).",
					},
					"state": map[string]interface{}{
						"type":        "object",
						"description": "Entity state object (get_state).",
					},
					"result": map[string]interface{}{
						"type":        "object",
						"description": "Service call result (control, trigger_scene).",
					},
					"areas": map[string]interface{}{
						"type":        "array",
						"description": "List of areas (list_areas).",
					},
					"history": map[string]interface{}{
						"type":        "array",
						"description": "State history entries (get_history).",
					},
				},
			},
		},
	}
}

func (self *homeAssistantTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	configuration := configurationFromContext(ctx)
	if configuration.baseUrl == "" {
		return "", fmt.Errorf("home assistant tool is not configured: baseUrl is missing")
	}
	if configuration.token == "" {
		return "", fmt.Errorf("home assistant tool is not configured: token is missing")
	}

	execution := &homeAssistantExecution{
		client:  NewHTTPClient(configuration.baseUrl, configuration.token, configuration.timeoutSeconds),
		checker: NewAccessChecker(configuration),
	}

	return execution.execute(ctx, rawArguments)
}

func (self *homeAssistantExecution) execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Action     string                 `json:"action"`
		Domain     string                 `json:"domain"`
		EntityID   string                 `json:"entityId"`
		Command    string                 `json:"command"`
		Attributes map[string]interface{} `json:"attributes"`
		Hours      int                    `json:"hours"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}

	switch arguments.Action {
	case "list_entities":
		return self.executeListEntities(ctx, arguments.Domain)
	case "get_state":
		return self.executeGetState(ctx, arguments.EntityID)
	case "control":
		return self.executeControl(ctx, arguments.EntityID, arguments.Command, arguments.Attributes)
	case "trigger_scene":
		return self.executeTriggerScene(ctx, arguments.EntityID)
	case "list_areas":
		return self.executeListAreas(ctx)
	case "get_history":
		return self.executeGetHistory(ctx, arguments.EntityID, arguments.Hours)
	default:
		return "", fmt.Errorf("unknown home_assistant action: %s", arguments.Action)
	}
}

func (self *homeAssistantExecution) executeListEntities(ctx context.Context, domain string) (string, error) {
	states, err := self.client.GetStates(ctx)
	if err != nil {
		return "", fmt.Errorf("listing entities: %w", err)
	}

	type entitySummary struct {
		EntityID     string `json:"entity_id"`
		State        string `json:"state"`
		FriendlyName string `json:"friendly_name,omitempty"`
	}

	var entities []entitySummary
	for _, state := range states {
		entityDomain := DomainOf(state.EntityID)

		// Filter by domain if specified.
		if domain != "" && entityDomain != domain {
			continue
		}

		// Apply access rules.
		if !self.checker.IsEntityAllowed(state.EntityID) {
			continue
		}

		friendlyName, _ := state.Attributes["friendly_name"].(string)
		entities = append(entities, entitySummary{
			EntityID:     state.EntityID,
			State:        state.State,
			FriendlyName: friendlyName,
		})
	}

	return marshalResult(map[string]interface{}{
		"action":   "list_entities",
		"entities": entities,
		"count":    len(entities),
	})
}

func (self *homeAssistantExecution) executeGetState(ctx context.Context, entityId string) (string, error) {
	if entityId == "" {
		return "", fmt.Errorf("entityId is required for get_state action")
	}
	if !self.checker.IsEntityAllowed(entityId) {
		return "", fmt.Errorf("entity %q is not accessible (blocked by access rules)", entityId)
	}

	state, err := self.client.GetState(ctx, entityId)
	if err != nil {
		return "", fmt.Errorf("getting state: %w", err)
	}

	return marshalResult(map[string]interface{}{
		"action": "get_state",
		"state":  state,
	})
}

func (self *homeAssistantExecution) executeControl(ctx context.Context, entityId string, command string, attributes map[string]interface{}) (string, error) {
	if !self.checker.IsWriteAllowed() {
		return "", fmt.Errorf("control action is blocked: Home Assistant is configured in read-only mode")
	}
	if entityId == "" {
		return "", fmt.Errorf("entityId is required for control action")
	}
	if command == "" {
		return "", fmt.Errorf("command is required for control action")
	}

	// Restrict commands to the safe set.
	switch command {
	case "turn_on", "turn_off", "toggle":
		// allowed
	default:
		return "", fmt.Errorf("command %q is not allowed; must be one of: turn_on, turn_off, toggle", command)
	}

	if !self.checker.IsEntityAllowed(entityId) {
		return "", fmt.Errorf("entity %q is not accessible (blocked by access rules)", entityId)
	}

	domain := DomainOf(entityId)
	serviceData := map[string]interface{}{
		"entity_id": entityId,
	}
	for key, value := range attributes {
		serviceData[key] = value
	}

	result, err := self.client.CallService(ctx, domain, command, serviceData)
	if err != nil {
		return "", fmt.Errorf("calling service: %w", err)
	}

	return marshalResult(map[string]interface{}{
		"action":   "control",
		"entityId": entityId,
		"command":  command,
		"result":   json.RawMessage(result),
	})
}

func (self *homeAssistantExecution) executeTriggerScene(ctx context.Context, entityId string) (string, error) {
	if !self.checker.IsWriteAllowed() {
		return "", fmt.Errorf("trigger_scene action is blocked: Home Assistant is configured in read-only mode")
	}
	if entityId == "" {
		return "", fmt.Errorf("entityId is required for trigger_scene action")
	}

	if DomainOf(entityId) != "scene" {
		return "", fmt.Errorf("trigger_scene requires a scene entity (e.g. scene.movie_night), got %q", entityId)
	}

	if !self.checker.IsEntityAllowed(entityId) {
		return "", fmt.Errorf("entity %q is not accessible (blocked by access rules)", entityId)
	}

	serviceData := map[string]interface{}{
		"entity_id": entityId,
	}

	result, err := self.client.CallService(ctx, "scene", "turn_on", serviceData)
	if err != nil {
		return "", fmt.Errorf("triggering scene: %w", err)
	}

	return marshalResult(map[string]interface{}{
		"action":   "trigger_scene",
		"entityId": entityId,
		"result":   json.RawMessage(result),
	})
}

func (self *homeAssistantExecution) executeListAreas(ctx context.Context) (string, error) {
	// TODO: HA areas require WebSocket API (config/area_registry/list).
	// For now, return an empty list. A future version should use the WS API.
	log.Debugf("list_areas: returning empty list (WebSocket API not yet implemented)")
	return marshalResult(map[string]interface{}{
		"action": "list_areas",
		"areas":  []interface{}{},
		"note":   "Area listing requires the Home Assistant WebSocket API, which is not yet implemented. Areas will be available in a future update.",
	})
}

func (self *homeAssistantExecution) executeGetHistory(ctx context.Context, entityId string, hours int) (string, error) {
	if entityId == "" {
		return "", fmt.Errorf("entityId is required for get_history action")
	}
	if !self.checker.IsEntityAllowed(entityId) {
		return "", fmt.Errorf("entity %q is not accessible (blocked by access rules)", entityId)
	}
	if hours <= 0 {
		hours = 1
	}

	rawHistory, err := self.client.GetHistory(ctx, entityId, hours)
	if err != nil {
		return "", fmt.Errorf("getting history: %w", err)
	}

	// HA returns [[entries]] — an array of arrays. Parse and cap results.
	var historyArrays []json.RawMessage
	if err := json.Unmarshal(rawHistory, &historyArrays); err != nil {
		// Return the raw data if structure is unexpected.
		return marshalResult(map[string]interface{}{
			"action":   "get_history",
			"entityId": entityId,
			"history":  json.RawMessage(rawHistory),
		})
	}

	if len(historyArrays) == 0 {
		return marshalResult(map[string]interface{}{
			"action":   "get_history",
			"entityId": entityId,
			"history":  []interface{}{},
		})
	}

	// Parse the first (and usually only) array of entries.
	var entries []json.RawMessage
	if err := json.Unmarshal(historyArrays[0], &entries); err != nil {
		return marshalResult(map[string]interface{}{
			"action":   "get_history",
			"entityId": entityId,
			"history":  json.RawMessage(historyArrays[0]),
		})
	}

	truncated := false
	if len(entries) > maxHistoryEntries {
		entries = entries[len(entries)-maxHistoryEntries:]
		truncated = true
	}

	return marshalResult(map[string]interface{}{
		"action":    "get_history",
		"entityId":  entityId,
		"history":   entries,
		"count":     len(entries),
		"truncated": truncated,
	})
}

// marshalResult marshals a result map to a JSON string.
func marshalResult(result map[string]interface{}) (string, error) {
	data, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("marshaling result: %w", err)
	}
	return string(data), nil
}
