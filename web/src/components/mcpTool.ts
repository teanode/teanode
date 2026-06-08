// Helpers for recognizing remote MCP tools in the UI.
//
// Adapted MCP tools are registered by the backend under the namespaced name
// "mcp__<server>__<tool>" (see internal/mcp/adapter.go). Recognizing that shape
// lets the UI warn the user that approving such a call sends its arguments to an
// external server.

export interface McpToolName {
  /** The admin-configured MCP server the tool belongs to. */
  server: string;
  /** The remote tool name as exposed by that server. */
  tool: string;
}

const MCP_PREFIX = "mcp__";
const SEPARATOR = "__";

/**
 * Parse a namespaced MCP tool name into its server and tool components, or
 * return null when the name is not a remote MCP tool. The server component runs
 * up to the first separator; everything after it is the tool name (so tool names
 * that themselves contain "__" are preserved).
 */
export function parseMcpToolName(toolName: string): McpToolName | null {
  if (!toolName.startsWith(MCP_PREFIX)) return null;
  const remainder = toolName.slice(MCP_PREFIX.length);
  const separatorIndex = remainder.indexOf(SEPARATOR);
  if (separatorIndex <= 0) return null;
  const server = remainder.slice(0, separatorIndex);
  const tool = remainder.slice(separatorIndex + SEPARATOR.length);
  if (!server || !tool) return null;
  return { server, tool };
}
