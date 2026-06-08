package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/teanode/teanode/internal/models"
)

func TestNamespacedNameSanitizes(t *testing.T) {
	cases := []struct {
		server string
		tool   string
		want   string
	}{
		{"robinhood", "get_quote", "mcp__robinhood__get_quote"},
		{"my server", "do/thing", "mcp__my_server__do_thing"},
		{"a.b", "c:d", "mcp__a_b__c_d"},
	}
	for _, testCase := range cases {
		if got := namespacedName(testCase.server, testCase.tool); got != testCase.want {
			t.Errorf("namespacedName(%q, %q) = %q, want %q", testCase.server, testCase.tool, got, testCase.want)
		}
	}
}

func TestAdapterDefinition(t *testing.T) {
	adapter := newToolAdapter(
		ServerConfiguration{Name: "robinhood"},
		RemoteTool{
			Name:        "get_quote",
			Description: "Get a stock quote",
			InputSchema: map[string]interface{}{"type": "object"},
		},
	)
	definition := adapter.Definition()
	if definition.Function.Name != "mcp__robinhood__get_quote" {
		t.Errorf("name = %q", definition.Function.Name)
	}
	if definition.Function.Description != "Get a stock quote" {
		t.Errorf("description = %q", definition.Function.Description)
	}
	if definition.Function.Parameters == nil {
		t.Errorf("parameters must not be nil")
	}
}

func TestAdapterDefinitionDefaultsSchemaAndDescription(t *testing.T) {
	adapter := newToolAdapter(
		ServerConfiguration{Name: "srv"},
		RemoteTool{Name: "bare"},
	)
	definition := adapter.Definition()
	if definition.Function.Description == "" {
		t.Errorf("description should fall back to a generated value")
	}
	schema, ok := definition.Function.Parameters.(map[string]interface{})
	if !ok || schema["type"] != "object" {
		t.Errorf("parameters should default to an empty object schema, got %#v", definition.Function.Parameters)
	}
}

func TestAdapterPolicyGroupsAreConservative(t *testing.T) {
	adapter := newToolAdapter(ServerConfiguration{Name: "srv"}, RemoteTool{Name: "x"})
	groups := adapter.PolicyGroups()
	if len(groups) != 1 {
		t.Fatalf("len(groups) = %d, want 1", len(groups))
	}
	if groups[0].Default != models.ToolPolicyAdminApproval {
		t.Errorf("default policy = %q, want admin_approval", groups[0].Default)
	}
}

func TestAdapterExecute(t *testing.T) {
	server := newTestMCPServer(t)
	adapter := newToolAdapter(
		ServerConfiguration{Name: "test", URL: server.url(), Timeout: 5 * time.Second},
		RemoteTool{Name: "get_quote"},
	)
	result, err := adapter.Execute(context.Background(), `{"symbol":"TSLA"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result != `ok:get_quote:{"symbol":"TSLA"}` {
		t.Errorf("result = %q", result)
	}
}

func TestAdapterExecuteSurfacesToolError(t *testing.T) {
	server := newTestMCPServer(t)
	server.callHandler = func(name string, arguments json.RawMessage) ([]map[string]interface{}, bool) {
		return []map[string]interface{}{{"type": "text", "text": "remote failure"}}, true
	}
	adapter := newToolAdapter(
		ServerConfiguration{Name: "test", URL: server.url(), Timeout: 5 * time.Second},
		RemoteTool{Name: "explode"},
	)
	_, err := adapter.Execute(context.Background(), "")
	if err == nil {
		t.Fatalf("expected error from tool-reported failure")
	}
	if err.Error() != "mcp: remote failure" {
		t.Errorf("err = %q, want 'mcp: remote failure'", err.Error())
	}
}
