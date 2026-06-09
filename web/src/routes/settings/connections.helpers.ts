import type {
  MCPConnectionAuthorizeResult,
  MCPConnectionCreateResult,
  MCPServerListItem,
  MCPServersListResult,
} from "../../types";

/** Minimal RPC surface needed by the connection helpers. */
export interface ConnectionsRpcClient {
  sendRpc<T = unknown>(
    method: string,
    params?: Record<string, unknown>,
  ): Promise<T>;
}

/** Fetch the configured MCP servers and the user's connection state for each. */
export function listMcpServers(
  backend: ConnectionsRpcClient,
): Promise<MCPServerListItem[]> {
  return backend
    .sendRpc<MCPServersListResult>("mcp.servers.list", {})
    .then((result) => result.servers || []);
}

/** Store a per-user credential for a "user" auth-mode server. */
export function createMcpConnection(
  backend: ConnectionsRpcClient,
  serverName: string,
  authorization: string,
): Promise<MCPConnectionCreateResult> {
  return backend.sendRpc<MCPConnectionCreateResult>("mcp.connections.create", {
    serverName,
    authorization,
  });
}

/** The OAuth callback path the backend serves; appended to a chosen origin. */
export const MCP_OAUTH_CALLBACK_PATH = "/api/mcp/oauth/callback";

/**
 * Start the OAuth flow and resolve to the provider authorization URL. When
 * `redirectUri` is given it overrides the node public URL callback — used to
 * point the authorization server back at the address the user is browsing from
 * (which may be a loopback URL the server requires).
 */
export function authorizeMcpConnection(
  backend: ConnectionsRpcClient,
  serverName: string,
  redirectUri?: string,
): Promise<string> {
  return backend
    .sendRpc<MCPConnectionAuthorizeResult>("mcp.connections.authorize", {
      serverName,
      ...(redirectUri ? { redirectUri } : {}),
    })
    .then((result) => result.authorizationUrl);
}

/** Remove one of the user's MCP connections. */
export function deleteMcpConnection(
  backend: ConnectionsRpcClient,
  connectionId: string,
): Promise<unknown> {
  return backend.sendRpc("mcp.connections.delete", { connectionId });
}

/**
 * The primary action a user can take for a server, derived from its auth mode
 * and the current connection state. Drives which button the row renders.
 *
 * - "shared": no per-user connection needed (none/static); nothing to do.
 * - "connect": user-token server without a connection — prompt for a credential.
 * - "authorize": oauth server without a usable connection — start the flow.
 * - "reauthorize": oauth server whose connection is pending or errored.
 * - "connected": the server has a usable connection — offer disconnect.
 */
export type ServerAction =
  | "shared"
  | "connect"
  | "authorize"
  | "reauthorize"
  | "connected";

export function serverAction(server: MCPServerListItem): ServerAction {
  if (!server.requiresConnection) {
    return "shared";
  }
  if (server.status === "connected") {
    return "connected";
  }
  if (server.authMode === "oauth") {
    // Pending or errored oauth connections can be retried; a fresh one starts
    // the flow for the first time. Both initiate the authorization redirect.
    return server.connectionId ? "reauthorize" : "authorize";
  }
  return "connect";
}

/** Outcome of the OAuth callback redirect, parsed from the landing URL. */
export interface OAuthCallbackOutcome {
  server: string;
  ok: boolean;
  error?: string;
}

/**
 * Parse the OAuth callback query string the backend redirects to
 * (`/settings/mcp?server=...&mcpConnected=1` on success, or `...&mcpError=...`
 * on failure). Returns null when the URL carries no MCP callback markers so
 * callers can ignore ordinary navigations.
 */
export function parseOAuthCallback(
  search: string,
): OAuthCallbackOutcome | null {
  const params = new URLSearchParams(search);
  const error = params.get("mcpError");
  const connected = params.get("mcpConnected");
  if (error === null && connected === null) {
    return null;
  }
  const server = params.get("server") || "";
  if (error !== null) {
    return { server, ok: false, error };
  }
  return { server, ok: true };
}
