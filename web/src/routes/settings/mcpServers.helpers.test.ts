import { describe, expect, it, vi } from "vitest";
import {
  emptyForm,
  formToServer,
  hasErrors,
  loadMcpServers,
  resolveAuthMode,
  resolveTransport,
  saveMcpServers,
  serverToForm,
  summarizeServer,
  validateForm,
  type McpServerFormValues,
} from "./mcpServers.helpers";

function form(overrides: Partial<McpServerFormValues>): McpServerFormValues {
  return { ...emptyForm(), ...overrides };
}

describe("resolveTransport", () => {
  it("honors an explicit transport", () => {
    expect(resolveTransport({ transport: "stdio", url: "https://x" })).toBe(
      "stdio",
    );
    expect(resolveTransport({ transport: "http", command: "x" })).toBe("http");
  });
  it("infers stdio from a bare command", () => {
    expect(resolveTransport({ command: "npx" })).toBe("stdio");
  });
  it("infers http otherwise", () => {
    expect(resolveTransport({ url: "https://x" })).toBe("http");
    expect(resolveTransport({ command: "npx", url: "https://x" })).toBe("http");
    expect(resolveTransport({})).toBe("http");
  });
});

describe("resolveAuthMode", () => {
  it("is always none for stdio", () => {
    expect(resolveAuthMode({ command: "npx", auth: "user" })).toBe("none");
  });
  it("honors an explicit auth mode for http", () => {
    expect(resolveAuthMode({ url: "https://x", auth: "oauth" })).toBe("oauth");
  });
  it("infers static from an authorization value", () => {
    expect(
      resolveAuthMode({ url: "https://x", authorization: "Bearer t" }),
    ).toBe("static");
    expect(resolveAuthMode({ url: "https://x" })).toBe("none");
  });
});

describe("serverToForm / formToServer round trip", () => {
  it("round-trips an http oauth server", () => {
    const stored = {
      name: "remote",
      transport: "http",
      url: "https://example.com/mcp",
      auth: "oauth",
      oauthClientId: "cid",
      oauthScopes: ["read", "write"],
      oauthAuthorizationUrl: "https://auth/authorize",
      enabled: true,
    };
    const rebuilt = formToServer(serverToForm(stored));
    expect(rebuilt.url).toBe("https://example.com/mcp");
    expect(rebuilt.auth).toBe("oauth");
    expect(rebuilt.oauthClientId).toBe("cid");
    expect(rebuilt.oauthScopes).toEqual(["read", "write"]);
    expect(rebuilt.command).toBeUndefined();
  });

  it("round-trips a stdio server with args and env", () => {
    const stored = {
      name: "local",
      transport: "stdio",
      command: "npx",
      args: ["-y", "server", "/tmp"],
      env: { API_KEY: "xyz" },
      workingDir: "/srv",
      enabled: false,
    };
    const formValues = serverToForm(stored);
    expect(formValues.args).toBe("-y\nserver\n/tmp");
    expect(formValues.env).toBe("API_KEY=xyz");
    expect(formValues.enabled).toBe(false);
    const rebuilt = formToServer(formValues);
    expect(rebuilt.transport).toBe("stdio");
    expect(rebuilt.command).toBe("npx");
    expect(rebuilt.args).toEqual(["-y", "server", "/tmp"]);
    expect(rebuilt.env).toEqual({ API_KEY: "xyz" });
    expect(rebuilt.workingDir).toBe("/srv");
    expect(rebuilt.enabled).toBe(false);
    expect(rebuilt.url).toBeUndefined();
    expect(rebuilt.auth).toBeUndefined();
  });
});

describe("formToServer", () => {
  it("parses env lines and ignores malformed ones", () => {
    const server = formToServer(
      form({
        transport: "stdio",
        command: "x",
        env: "A=1\n  B = two \ngarbage\n=bad",
      }),
    );
    expect(server.env).toEqual({ A: "1", B: "two" });
  });
  it("drops empty optional fields", () => {
    const server = formToServer(form({ transport: "stdio", command: "x" }));
    expect(server.args).toBeUndefined();
    expect(server.env).toBeUndefined();
    expect(server.workingDir).toBeUndefined();
    expect(server.timeoutSeconds).toBeUndefined();
  });
  it("keeps a valid timeout", () => {
    const server = formToServer(
      form({ transport: "http", url: "https://x", timeoutSeconds: "45" }),
    );
    expect(server.timeoutSeconds).toBe(45);
  });
});

describe("validateForm", () => {
  it("requires a name", () => {
    expect(validateForm(form({ name: "" }), []).name).toBe("required");
  });
  it("flags duplicate names", () => {
    expect(
      validateForm(form({ name: "dup", url: "https://x" }), ["dup"]).name,
    ).toBe("duplicate");
  });
  it("requires a url for http and validates the scheme", () => {
    expect(
      validateForm(form({ name: "a", transport: "http", url: "" }), []).url,
    ).toBe("required");
    expect(
      validateForm(form({ name: "a", transport: "http", url: "ftp://x" }), [])
        .url,
    ).toBe("invalid");
    expect(
      validateForm(form({ name: "a", transport: "http", url: "https://x" }), [])
        .url,
    ).toBeUndefined();
  });
  it("requires a command for stdio", () => {
    expect(
      validateForm(form({ name: "a", transport: "stdio", command: "" }), [])
        .command,
    ).toBe("required");
  });
  it("rejects a non-positive or non-integer timeout", () => {
    expect(
      validateForm(
        form({ name: "a", url: "https://x", timeoutSeconds: "0" }),
        [],
      ).timeoutSeconds,
    ).toBe("invalid");
    expect(
      validateForm(
        form({ name: "a", url: "https://x", timeoutSeconds: "1.5" }),
        [],
      ).timeoutSeconds,
    ).toBe("invalid");
  });
  it("passes a valid http server", () => {
    expect(
      hasErrors(validateForm(form({ name: "a", url: "https://x" }), [])),
    ).toBe(false);
  });
});

describe("summarizeServer", () => {
  it("shows the url for http", () => {
    expect(summarizeServer({ url: "https://x/mcp" })).toBe("https://x/mcp");
  });
  it("shows the command line for stdio", () => {
    expect(summarizeServer({ command: "npx", args: ["-y", "srv"] })).toBe(
      "npx -y srv",
    );
  });
});

describe("loadMcpServers / saveMcpServers", () => {
  it("extracts the servers array from config.get", async () => {
    const sendRpc = vi.fn().mockResolvedValue({
      config: { tools: { mcp: { servers: [{ name: "a" }] } } },
    });
    expect(await loadMcpServers({ sendRpc })).toEqual([{ name: "a" }]);
    expect(sendRpc).toHaveBeenCalledWith("config.get", {});
  });
  it("returns an empty array when no servers are configured", async () => {
    const sendRpc = vi.fn().mockResolvedValue({ config: {} });
    expect(await loadMcpServers({ sendRpc })).toEqual([]);
  });
  it("writes the full servers array via config.update", async () => {
    const sendRpc = vi.fn().mockResolvedValue({ ok: true });
    await saveMcpServers({ sendRpc }, [{ name: "a" }]);
    expect(sendRpc).toHaveBeenCalledWith("config.update", {
      config: { tools: { mcp: { servers: [{ name: "a" }] } } },
    });
  });
});
