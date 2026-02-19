package homeassistant

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/teanode/teanode/internal/agents"
	"github.com/teanode/teanode/internal/configs"
)

// --- mock client ---

type mockClient struct {
	states      []EntityState
	stateByID   map[string]*EntityState
	serviceResp json.RawMessage
	historyResp json.RawMessage
	configResp  json.RawMessage
	err         error

	// track calls
	callServiceCalls []serviceCall
}

type serviceCall struct {
	Domain  string
	Service string
	Data    map[string]interface{}
}

func (self *mockClient) GetStates(ctx context.Context) ([]EntityState, error) {
	if self.err != nil {
		return nil, self.err
	}
	return self.states, nil
}

func (self *mockClient) GetState(ctx context.Context, entityID string) (*EntityState, error) {
	if self.err != nil {
		return nil, self.err
	}
	if state, exists := self.stateByID[entityID]; exists {
		return state, nil
	}
	return nil, fmt.Errorf("Home Assistant returned HTTP 404 for GET /api/states/%s", entityID)
}

func (self *mockClient) CallService(ctx context.Context, domain string, service string, data map[string]interface{}) (json.RawMessage, error) {
	if self.err != nil {
		return nil, self.err
	}
	self.callServiceCalls = append(self.callServiceCalls, serviceCall{
		Domain:  domain,
		Service: service,
		Data:    data,
	})
	if self.serviceResp != nil {
		return self.serviceResp, nil
	}
	return json.RawMessage(`[]`), nil
}

func (self *mockClient) GetHistory(ctx context.Context, entityID string, hours int) (json.RawMessage, error) {
	if self.err != nil {
		return nil, self.err
	}
	if self.historyResp != nil {
		return self.historyResp, nil
	}
	return json.RawMessage(`[[]]`), nil
}

func (self *mockClient) GetConfig(ctx context.Context) (json.RawMessage, error) {
	if self.err != nil {
		return nil, self.err
	}
	if self.configResp != nil {
		return self.configResp, nil
	}
	return json.RawMessage(`{}`), nil
}

func newMockClient() *mockClient {
	return &mockClient{
		stateByID: make(map[string]*EntityState),
	}
}

func newTestTool(client *mockClient, config *configs.HomeAssistantConfig) *homeAssistantTool {
	if config == nil {
		config = &configs.HomeAssistantConfig{}
	}
	return &homeAssistantTool{
		client:  client,
		checker: NewAccessChecker(config),
	}
}

// --- AccessChecker tests ---

func TestAccessChecker_DefaultDomains(testing *testing.T) {
	checker := NewAccessChecker(nil)

	// Default allowed domains should pass.
	for _, domain := range DefaultAllowedDomains {
		if !checker.IsDomainAllowed(domain) {
			testing.Errorf("expected default domain %q to be allowed", domain)
		}
	}

	// Default blocked domains should fail.
	for _, domain := range DefaultBlockedDomains {
		if checker.IsDomainAllowed(domain) {
			testing.Errorf("expected default domain %q to be blocked", domain)
		}
	}

	// Unknown domains should fail.
	if checker.IsDomainAllowed("custom_domain") {
		testing.Error("expected unknown domain to be blocked")
	}
}

func TestAccessChecker_DefaultEntityAccess(testing *testing.T) {
	checker := NewAccessChecker(nil)

	if !checker.IsEntityAllowed("light.living_room") {
		testing.Error("expected light entity to be allowed by default")
	}
	if !checker.IsEntityAllowed("sensor.temperature") {
		testing.Error("expected sensor entity to be allowed by default")
	}
	if checker.IsEntityAllowed("lock.front_door") {
		testing.Error("expected lock entity to be blocked by default")
	}
	if checker.IsEntityAllowed("alarm_control_panel.home") {
		testing.Error("expected alarm entity to be blocked by default")
	}
}

func TestAccessChecker_CustomAllowedDomains(testing *testing.T) {
	config := &configs.HomeAssistantConfig{
		AllowedDomains: []string{"light", "switch"},
	}
	checker := NewAccessChecker(config)

	if !checker.IsEntityAllowed("light.kitchen") {
		testing.Error("expected light entity to be allowed")
	}
	if !checker.IsEntityAllowed("switch.garage") {
		testing.Error("expected switch entity to be allowed")
	}
	// Sensor is in defaults but not in custom list.
	if checker.IsEntityAllowed("sensor.temperature") {
		testing.Error("expected sensor entity to be blocked with custom allowlist")
	}
}

func TestAccessChecker_CustomBlockedDomains(testing *testing.T) {
	config := &configs.HomeAssistantConfig{
		BlockedDomains: []string{"lock", "alarm_control_panel", "light"},
	}
	checker := NewAccessChecker(config)

	// Light is both in default allowed and custom blocked — blocked wins.
	if checker.IsEntityAllowed("light.living_room") {
		testing.Error("expected light to be blocked when in custom blocked list")
	}
	if checker.IsDomainAllowed("light") {
		testing.Error("expected light domain to be blocked")
	}
}

func TestAccessChecker_AllowedEntities(testing *testing.T) {
	config := &configs.HomeAssistantConfig{
		AllowedEntities: []string{"light.kitchen", "switch.garage"},
	}
	checker := NewAccessChecker(config)

	if !checker.IsEntityAllowed("light.kitchen") {
		testing.Error("expected allowed entity to pass")
	}
	if !checker.IsEntityAllowed("switch.garage") {
		testing.Error("expected allowed entity to pass")
	}
	// light.living_room is in an allowed domain but not in the entity allowlist.
	if checker.IsEntityAllowed("light.living_room") {
		testing.Error("expected entity not in allowlist to be blocked")
	}
}

func TestAccessChecker_BlockedDomainOverridesAllowedEntity(testing *testing.T) {
	config := &configs.HomeAssistantConfig{
		AllowedEntities: []string{"lock.front_door"},
		// lock is blocked by default
	}
	checker := NewAccessChecker(config)

	if checker.IsEntityAllowed("lock.front_door") {
		testing.Error("expected blocked domain to override entity allowlist")
	}
}

func TestAccessChecker_ReadOnly(testing *testing.T) {
	config := &configs.HomeAssistantConfig{ReadOnly: true}
	checker := NewAccessChecker(config)

	if checker.IsWriteAllowed() {
		testing.Error("expected write to be blocked in read-only mode")
	}

	config2 := &configs.HomeAssistantConfig{ReadOnly: false}
	checker2 := NewAccessChecker(config2)
	if !checker2.IsWriteAllowed() {
		testing.Error("expected write to be allowed when not read-only")
	}
}

func TestAccessChecker_NilConfig(testing *testing.T) {
	checker := NewAccessChecker(nil)

	// Should use defaults.
	if !checker.IsEntityAllowed("light.test") {
		testing.Error("expected default allowed domain with nil config")
	}
	if checker.IsEntityAllowed("lock.test") {
		testing.Error("expected default blocked domain with nil config")
	}
	if !checker.IsWriteAllowed() {
		testing.Error("expected write allowed with nil config (readOnly defaults false)")
	}
}

// --- DomainOf tests ---

func TestDomainOf(testing *testing.T) {
	tests := []struct {
		entityID string
		want     string
	}{
		{"light.living_room", "light"},
		{"sensor.temperature", "sensor"},
		{"binary_sensor.motion", "binary_sensor"},
		{"scene.movie_night", "scene"},
		{"nodot", "nodot"},
		{"", ""},
	}
	for _, testCase := range tests {
		got := DomainOf(testCase.entityID)
		if got != testCase.want {
			testing.Errorf("DomainOf(%q) = %q, want %q", testCase.entityID, got, testCase.want)
		}
	}
}

// --- Tool Definition tests ---

func TestToolDefinition(testing *testing.T) {
	tool := newTestTool(newMockClient(), nil)
	definition := tool.Definition()

	if definition.Type != "function" {
		testing.Errorf("expected type 'function', got %q", definition.Type)
	}
	if definition.Function.Name != "home_assistant" {
		testing.Errorf("expected name 'home_assistant', got %q", definition.Function.Name)
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

	// Verify action enum.
	params := definition.Function.Parameters.(map[string]interface{})
	properties := params["properties"].(map[string]interface{})
	action := properties["action"].(map[string]interface{})
	actionEnum := action["enum"].([]string)
	expectedActions := []string{"list_entities", "get_state", "control", "trigger_scene", "list_areas", "get_history"}
	if len(actionEnum) != len(expectedActions) {
		testing.Fatalf("expected %d actions, got %d", len(expectedActions), len(actionEnum))
	}
	for index, want := range expectedActions {
		if actionEnum[index] != want {
			testing.Errorf("action[%d] = %q, want %q", index, actionEnum[index], want)
		}
	}
}

// --- list_entities tests ---

func TestListEntities_Basic(testing *testing.T) {
	client := newMockClient()
	client.states = []EntityState{
		{EntityID: "light.living_room", State: "on", Attributes: map[string]interface{}{"friendly_name": "Living Room Light"}},
		{EntityID: "sensor.temperature", State: "22.5", Attributes: map[string]interface{}{"friendly_name": "Temperature"}},
		{EntityID: "lock.front_door", State: "locked", Attributes: map[string]interface{}{"friendly_name": "Front Door"}},
	}
	tool := newTestTool(client, nil)

	arguments, _ := json.Marshal(map[string]interface{}{"action": "list_entities"})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}

	var response map[string]interface{}
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		testing.Fatalf("invalid JSON response: %v", err)
	}

	if response["action"] != "list_entities" {
		testing.Errorf("expected action 'list_entities', got %v", response["action"])
	}

	entities := response["entities"].([]interface{})
	// lock should be filtered out.
	if len(entities) != 2 {
		testing.Fatalf("expected 2 entities (lock filtered), got %d", len(entities))
	}
}

func TestListEntities_FilterByDomain(testing *testing.T) {
	client := newMockClient()
	client.states = []EntityState{
		{EntityID: "light.living_room", State: "on"},
		{EntityID: "light.bedroom", State: "off"},
		{EntityID: "sensor.temperature", State: "22"},
	}
	tool := newTestTool(client, nil)

	arguments, _ := json.Marshal(map[string]interface{}{"action": "list_entities", "domain": "light"})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}

	var response map[string]interface{}
	json.Unmarshal([]byte(result), &response)

	entities := response["entities"].([]interface{})
	if len(entities) != 2 {
		testing.Fatalf("expected 2 light entities, got %d", len(entities))
	}
}

func TestListEntities_ClientError(testing *testing.T) {
	client := newMockClient()
	client.err = fmt.Errorf("connection refused")
	tool := newTestTool(client, nil)

	arguments, _ := json.Marshal(map[string]interface{}{"action": "list_entities"})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil {
		testing.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "listing entities") {
		testing.Errorf("expected 'listing entities' in error, got: %v", err)
	}
}

// --- get_state tests ---

func TestGetState_Basic(testing *testing.T) {
	client := newMockClient()
	client.stateByID["light.living_room"] = &EntityState{
		EntityID:   "light.living_room",
		State:      "on",
		Attributes: map[string]interface{}{"brightness": float64(255)},
	}
	tool := newTestTool(client, nil)

	arguments, _ := json.Marshal(map[string]interface{}{"action": "get_state", "entityId": "light.living_room"})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}

	var response map[string]interface{}
	json.Unmarshal([]byte(result), &response)

	if response["action"] != "get_state" {
		testing.Errorf("expected action 'get_state', got %v", response["action"])
	}
	state := response["state"].(map[string]interface{})
	if state["entity_id"] != "light.living_room" {
		testing.Errorf("expected entity_id 'light.living_room', got %v", state["entity_id"])
	}
}

func TestGetState_MissingEntityID(testing *testing.T) {
	tool := newTestTool(newMockClient(), nil)

	arguments, _ := json.Marshal(map[string]interface{}{"action": "get_state"})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "entityId is required") {
		testing.Errorf("expected 'entityId is required' error, got: %v", err)
	}
}

func TestGetState_BlockedEntity(testing *testing.T) {
	tool := newTestTool(newMockClient(), nil)

	arguments, _ := json.Marshal(map[string]interface{}{"action": "get_state", "entityId": "lock.front_door"})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "not accessible") {
		testing.Errorf("expected access denied error, got: %v", err)
	}
}

// --- control tests ---

func TestControl_TurnOn(testing *testing.T) {
	client := newMockClient()
	tool := newTestTool(client, nil)

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":   "control",
		"entityId": "light.living_room",
		"command":  "turn_on",
		"attributes": map[string]interface{}{
			"brightness": 128,
		},
	})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}

	var response map[string]interface{}
	json.Unmarshal([]byte(result), &response)

	if response["action"] != "control" {
		testing.Errorf("expected action 'control', got %v", response["action"])
	}
	if response["command"] != "turn_on" {
		testing.Errorf("expected command 'turn_on', got %v", response["command"])
	}

	// Verify service was called correctly.
	if len(client.callServiceCalls) != 1 {
		testing.Fatalf("expected 1 service call, got %d", len(client.callServiceCalls))
	}
	call := client.callServiceCalls[0]
	if call.Domain != "light" {
		testing.Errorf("expected domain 'light', got %q", call.Domain)
	}
	if call.Service != "turn_on" {
		testing.Errorf("expected service 'turn_on', got %q", call.Service)
	}
	if call.Data["entity_id"] != "light.living_room" {
		testing.Errorf("expected entity_id in data, got %v", call.Data)
	}
	if call.Data["brightness"] != float64(128) {
		testing.Errorf("expected brightness attribute, got %v", call.Data["brightness"])
	}
}

func TestControl_Toggle(testing *testing.T) {
	client := newMockClient()
	tool := newTestTool(client, nil)

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":   "control",
		"entityId": "switch.garage",
		"command":  "toggle",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}

	call := client.callServiceCalls[0]
	if call.Service != "toggle" {
		testing.Errorf("expected service 'toggle', got %q", call.Service)
	}
}

func TestControl_TurnOff(testing *testing.T) {
	client := newMockClient()
	tool := newTestTool(client, nil)

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":   "control",
		"entityId": "fan.bedroom",
		"command":  "turn_off",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}

	call := client.callServiceCalls[0]
	if call.Service != "turn_off" {
		testing.Errorf("expected service 'turn_off', got %q", call.Service)
	}
}

func TestControl_ReadOnlyBlocked(testing *testing.T) {
	config := &configs.HomeAssistantConfig{ReadOnly: true}
	tool := newTestTool(newMockClient(), config)

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":   "control",
		"entityId": "light.living_room",
		"command":  "turn_on",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "read-only mode") {
		testing.Errorf("expected read-only error, got: %v", err)
	}
}

func TestControl_BlockedDomain(testing *testing.T) {
	tool := newTestTool(newMockClient(), nil)

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":   "control",
		"entityId": "lock.front_door",
		"command":  "turn_on",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "not accessible") {
		testing.Errorf("expected access denied error, got: %v", err)
	}
}

func TestControl_InvalidCommand(testing *testing.T) {
	tool := newTestTool(newMockClient(), nil)

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":   "control",
		"entityId": "light.living_room",
		"command":  "set_brightness",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		testing.Errorf("expected command not allowed error, got: %v", err)
	}
}

func TestControl_MissingEntityID(testing *testing.T) {
	tool := newTestTool(newMockClient(), nil)

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":  "control",
		"command": "turn_on",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "entityId is required") {
		testing.Errorf("expected 'entityId is required' error, got: %v", err)
	}
}

func TestControl_MissingCommand(testing *testing.T) {
	tool := newTestTool(newMockClient(), nil)

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":   "control",
		"entityId": "light.living_room",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "command is required") {
		testing.Errorf("expected 'command is required' error, got: %v", err)
	}
}

// --- trigger_scene tests ---

func TestTriggerScene_Basic(testing *testing.T) {
	client := newMockClient()
	tool := newTestTool(client, nil)

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":   "trigger_scene",
		"entityId": "scene.movie_night",
	})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}

	var response map[string]interface{}
	json.Unmarshal([]byte(result), &response)

	if response["action"] != "trigger_scene" {
		testing.Errorf("expected action 'trigger_scene', got %v", response["action"])
	}

	call := client.callServiceCalls[0]
	if call.Domain != "scene" || call.Service != "turn_on" {
		testing.Errorf("expected scene/turn_on call, got %s/%s", call.Domain, call.Service)
	}
}

func TestTriggerScene_ReadOnlyBlocked(testing *testing.T) {
	config := &configs.HomeAssistantConfig{ReadOnly: true}
	tool := newTestTool(newMockClient(), config)

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":   "trigger_scene",
		"entityId": "scene.movie_night",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "read-only mode") {
		testing.Errorf("expected read-only error, got: %v", err)
	}
}

func TestTriggerScene_NonSceneEntity(testing *testing.T) {
	tool := newTestTool(newMockClient(), nil)

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":   "trigger_scene",
		"entityId": "light.living_room",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "requires a scene entity") {
		testing.Errorf("expected scene entity error, got: %v", err)
	}
}

func TestTriggerScene_MissingEntityID(testing *testing.T) {
	tool := newTestTool(newMockClient(), nil)

	arguments, _ := json.Marshal(map[string]interface{}{
		"action": "trigger_scene",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "entityId is required") {
		testing.Errorf("expected 'entityId is required' error, got: %v", err)
	}
}

// --- list_areas tests ---

func TestListAreas_ReturnsEmpty(testing *testing.T) {
	tool := newTestTool(newMockClient(), nil)

	arguments, _ := json.Marshal(map[string]interface{}{"action": "list_areas"})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}

	var response map[string]interface{}
	json.Unmarshal([]byte(result), &response)

	if response["action"] != "list_areas" {
		testing.Errorf("expected action 'list_areas', got %v", response["action"])
	}
	areas := response["areas"].([]interface{})
	if len(areas) != 0 {
		testing.Errorf("expected empty areas list, got %d", len(areas))
	}
}

// --- get_history tests ---

func TestGetHistory_Basic(testing *testing.T) {
	client := newMockClient()
	client.historyResp = json.RawMessage(`[[{"state":"on","last_changed":"2026-01-01T00:00:00Z"},{"state":"off","last_changed":"2026-01-01T01:00:00Z"}]]`)
	tool := newTestTool(client, nil)

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":   "get_history",
		"entityId": "light.living_room",
		"hours":    2,
	})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}

	var response map[string]interface{}
	json.Unmarshal([]byte(result), &response)

	if response["action"] != "get_history" {
		testing.Errorf("expected action 'get_history', got %v", response["action"])
	}
	history := response["history"].([]interface{})
	if len(history) != 2 {
		testing.Fatalf("expected 2 history entries, got %d", len(history))
	}
}

func TestGetHistory_Truncation(testing *testing.T) {
	// Build a history response with more than maxHistoryEntries.
	var entries []map[string]string
	for index := 0; index < 60; index++ {
		entries = append(entries, map[string]string{
			"state":        "on",
			"last_changed": fmt.Sprintf("2026-01-01T%02d:00:00Z", index%24),
		})
	}
	entriesJSON, _ := json.Marshal(entries)
	historyResp, _ := json.Marshal([]json.RawMessage{json.RawMessage(entriesJSON)})

	client := newMockClient()
	client.historyResp = json.RawMessage(historyResp)
	tool := newTestTool(client, nil)

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":   "get_history",
		"entityId": "sensor.temperature",
		"hours":    24,
	})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}

	var response map[string]interface{}
	json.Unmarshal([]byte(result), &response)

	count := int(response["count"].(float64))
	if count != maxHistoryEntries {
		testing.Errorf("expected %d entries after truncation, got %d", maxHistoryEntries, count)
	}
	if response["truncated"] != true {
		testing.Error("expected truncated flag to be true")
	}
}

func TestGetHistory_MissingEntityID(testing *testing.T) {
	tool := newTestTool(newMockClient(), nil)

	arguments, _ := json.Marshal(map[string]interface{}{"action": "get_history"})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "entityId is required") {
		testing.Errorf("expected 'entityId is required' error, got: %v", err)
	}
}

func TestGetHistory_BlockedEntity(testing *testing.T) {
	tool := newTestTool(newMockClient(), nil)

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":   "get_history",
		"entityId": "lock.front_door",
	})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "not accessible") {
		testing.Errorf("expected access denied error, got: %v", err)
	}
}

func TestGetHistory_EmptyResult(testing *testing.T) {
	client := newMockClient()
	client.historyResp = json.RawMessage(`[]`)
	tool := newTestTool(client, nil)

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":   "get_history",
		"entityId": "light.living_room",
	})
	result, err := tool.Execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}

	var response map[string]interface{}
	json.Unmarshal([]byte(result), &response)

	history := response["history"].([]interface{})
	if len(history) != 0 {
		testing.Errorf("expected empty history, got %d entries", len(history))
	}
}

// --- unknown action tests ---

func TestUnknownAction(testing *testing.T) {
	tool := newTestTool(newMockClient(), nil)

	arguments, _ := json.Marshal(map[string]interface{}{"action": "unknown"})
	_, err := tool.Execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "unknown home_assistant action") {
		testing.Errorf("expected unknown action error, got: %v", err)
	}
}

func TestInvalidJSON(testing *testing.T) {
	tool := newTestTool(newMockClient(), nil)

	_, err := tool.Execute(context.Background(), "not json")
	if err == nil || !strings.Contains(err.Error(), "parsing arguments") {
		testing.Errorf("expected parsing error, got: %v", err)
	}
}

// --- RegisterTools tests ---

func TestRegisterTools_NilConfig(testing *testing.T) {
	registry := agents.NewToolRegistry()
	RegisterTools(registry, nil)
	if registry.Get("home_assistant") != nil {
		testing.Error("expected no tool registered with nil config")
	}
}

func TestRegisterTools_MissingBaseURL(testing *testing.T) {
	registry := agents.NewToolRegistry()
	RegisterTools(registry, &configs.HomeAssistantConfig{
		Token: "test-token",
	})
	if registry.Get("home_assistant") != nil {
		testing.Error("expected no tool registered without baseUrl")
	}
}

func TestRegisterTools_MissingToken(testing *testing.T) {
	registry := agents.NewToolRegistry()
	RegisterTools(registry, &configs.HomeAssistantConfig{
		BaseURL: "http://localhost:8123",
	})
	if registry.Get("home_assistant") != nil {
		testing.Error("expected no tool registered without token")
	}
}

func TestRegisterTools_ValidConfig(testing *testing.T) {
	registry := agents.NewToolRegistry()
	RegisterTools(registry, &configs.HomeAssistantConfig{
		BaseURL: "http://localhost:8123",
		Token:   "test-token",
	})
	if registry.Get("home_assistant") == nil {
		testing.Error("expected home_assistant tool to be registered")
	}
}
