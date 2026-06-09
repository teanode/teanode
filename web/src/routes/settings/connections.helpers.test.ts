import { describe, expect, it, vi } from "vitest";
import {
  authorizeMcpConnection,
  createMcpConnection,
  deleteMcpConnection,
  listMcpServers,
  parseOAuthCallback,
  serverAction,
} from "./connections.helpers";
import type { MCPServerListItem } from "../../types";

function server(overrides: Partial<MCPServerListItem>): MCPServerListItem {
  return {
    name: "srv",
    transport: "http",
    url: "https://example.com/mcp",
    authMode: "none",
    enabled: true,
    requiresConnection: false,
    connected: false,
    ...overrides,
  };
}

describe("listMcpServers", () => {
  it("calls mcp.servers.list and returns the servers array", async () => {
    const sendRpc = vi
      .fn()
      .mockResolvedValue({ servers: [server({ name: "a" })] });
    const result = await listMcpServers({ sendRpc });
    expect(sendRpc).toHaveBeenCalledWith("mcp.servers.list", {});
    expect(result).toHaveLength(1);
    expect(result[0].name).toBe("a");
  });

  it("tolerates a missing servers field", async () => {
    const sendRpc = vi.fn().mockResolvedValue({});
    expect(await listMcpServers({ sendRpc })).toEqual([]);
  });
});

describe("createMcpConnection", () => {
  it("calls mcp.connections.create with server name and credential", async () => {
    const sendRpc = vi.fn().mockResolvedValue({ connection: {} });
    await createMcpConnection({ sendRpc }, "srv", "Bearer xyz");
    expect(sendRpc).toHaveBeenCalledWith("mcp.connections.create", {
      serverName: "srv",
      authorization: "Bearer xyz",
    });
  });
});

describe("authorizeMcpConnection", () => {
  it("calls mcp.connections.authorize and returns the authorization URL", async () => {
    const sendRpc = vi
      .fn()
      .mockResolvedValue({ authorizationUrl: "https://auth/x" });
    const url = await authorizeMcpConnection({ sendRpc }, "srv");
    expect(sendRpc).toHaveBeenCalledWith("mcp.connections.authorize", {
      serverName: "srv",
    });
    expect(url).toBe("https://auth/x");
  });
});

describe("deleteMcpConnection", () => {
  it("calls mcp.connections.delete with the connection id", async () => {
    const sendRpc = vi.fn().mockResolvedValue({ deleted: true });
    await deleteMcpConnection({ sendRpc }, "conn-1");
    expect(sendRpc).toHaveBeenCalledWith("mcp.connections.delete", {
      connectionId: "conn-1",
    });
  });
});

describe("serverAction", () => {
  it("returns shared when no connection is required", () => {
    expect(serverAction(server({ requiresConnection: false }))).toBe("shared");
  });

  it("returns connect for a user-token server without a connection", () => {
    expect(
      serverAction(server({ authMode: "user", requiresConnection: true })),
    ).toBe("connect");
  });

  it("returns authorize for a fresh oauth server", () => {
    expect(
      serverAction(server({ authMode: "oauth", requiresConnection: true })),
    ).toBe("authorize");
  });

  it("returns reauthorize for an errored or pending oauth connection", () => {
    expect(
      serverAction(
        server({
          authMode: "oauth",
          requiresConnection: true,
          connectionId: "c1",
          status: "error",
        }),
      ),
    ).toBe("reauthorize");
    expect(
      serverAction(
        server({
          authMode: "oauth",
          requiresConnection: true,
          connectionId: "c1",
          status: "pending",
        }),
      ),
    ).toBe("reauthorize");
  });

  it("returns connected for any connected server", () => {
    expect(
      serverAction(
        server({
          authMode: "oauth",
          requiresConnection: true,
          connectionId: "c1",
          status: "connected",
        }),
      ),
    ).toBe("connected");
    expect(
      serverAction(
        server({
          authMode: "user",
          requiresConnection: true,
          connectionId: "c2",
          status: "connected",
        }),
      ),
    ).toBe("connected");
  });
});

describe("parseOAuthCallback", () => {
  it("returns null when no MCP markers are present", () => {
    expect(parseOAuthCallback("")).toBeNull();
    expect(parseOAuthCallback("?foo=bar")).toBeNull();
  });

  it("parses a success callback", () => {
    expect(parseOAuthCallback("?server=acme&mcpConnected=1")).toEqual({
      server: "acme",
      ok: true,
    });
  });

  it("parses an error callback and prefers the error over success", () => {
    expect(parseOAuthCallback("?server=acme&mcpError=denied")).toEqual({
      server: "acme",
      ok: false,
      error: "denied",
    });
  });

  it("handles a missing server name", () => {
    expect(parseOAuthCallback("?mcpError=boom")).toEqual({
      server: "",
      ok: false,
      error: "boom",
    });
  });
});
