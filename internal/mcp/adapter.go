package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/tools"
)

// namePrefix is prepended to every adapted MCP tool name.
const namePrefix = "mcp"

// nameSeparator joins the prefix, server name, and remote tool name. The double
// underscore mirrors the widely used "mcp__server__tool" convention and keeps
// the namespaced name within the [a-zA-Z0-9_-] character set tools expect.
const nameSeparator = "__"

// unsafeNameCharacters matches anything not permitted in a tool name so it can
// be replaced with an underscore.
var unsafeNameCharacters = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// emptyObjectSchema is used when a remote tool declares no input schema, so the
// provider still receives a valid JSON Schema object.
var emptyObjectSchema = map[string]interface{}{
	"type":       "object",
	"properties": map[string]interface{}{},
}

// toolAdapter exposes a single remote MCP tool through the TeaNode Tool
// interface. It carries the resolved server configuration rather than a live
// client: each Execute opens a fresh session, which keeps the adapter free of
// stale-session state at the cost of one initialization round-trip per call.
//
// TODO: reuse a connected session across calls once session lifecycle and
// reconnection are handled.
type toolAdapter struct {
	server      ServerConfiguration
	remoteName  string
	displayName string
	description string
	inputSchema map[string]interface{}
}

// newToolAdapter builds an adapter for a discovered remote tool.
func newToolAdapter(server ServerConfiguration, remote RemoteTool) *toolAdapter {
	return &toolAdapter{
		server:      server,
		remoteName:  remote.Name,
		displayName: namespacedName(server.Name, remote.Name),
		description: remote.Description,
		inputSchema: remote.InputSchema,
	}
}

// namespacedName builds the registry name "mcp__<server>__<tool>", sanitizing
// the server and tool components to the permitted character set.
func namespacedName(serverName, toolName string) string {
	return strings.Join([]string{
		namePrefix,
		sanitizeNameComponent(serverName),
		sanitizeNameComponent(toolName),
	}, nameSeparator)
}

func sanitizeNameComponent(value string) string {
	return unsafeNameCharacters.ReplaceAllString(value, "_")
}

// Definition implements tools.Tool.
func (self *toolAdapter) Definition() providers.ToolDefinition {
	parameters := interface{}(self.inputSchema)
	if self.inputSchema == nil {
		parameters = emptyObjectSchema
	}
	description := self.description
	if description == "" {
		description = fmt.Sprintf("Remote MCP tool %q from server %q.", self.remoteName, self.server.Name)
	}
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name:        self.displayName,
			Description: description,
			Parameters:  parameters,
		},
	}
}

// PolicyGroups implements tools.Tool. Remote MCP tools are treated
// conservatively: every call requires admin approval by default. Operators can
// relax this per tool via tool policies keyed by the namespaced tool name.
func (self *toolAdapter) PolicyGroups() []tools.PolicyGroup {
	return []tools.PolicyGroup{
		{Group: models.ToolPolicyGroupAll, Default: models.ToolPolicyAdminApproval},
	}
}

// Execute implements tools.Tool by forwarding the call to the remote server.
func (self *toolAdapter) Execute(ctx context.Context, arguments string) (string, error) {
	client := NewClient(self.server)
	if connectError := client.Connect(ctx); connectError != nil {
		return "", connectError
	}
	result, callError := client.CallTool(ctx, self.remoteName, json.RawMessage(arguments))
	if callError != nil {
		return "", callError
	}
	text := result.Text()
	if result.IsError {
		// Surface tool-reported errors to the model as an error result rather
		// than swallowing them, matching how local tools report failures.
		if text == "" {
			text = "remote tool reported an error"
		}
		return "", fmt.Errorf("mcp: %s", text)
	}
	return text, nil
}
