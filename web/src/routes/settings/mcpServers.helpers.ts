// Helpers for the admin MCP server management page. These are pure functions
// (no React, no MUI) so the conversion and validation logic is unit-testable.
//
// MCP server definitions live in the node configuration under
// `tools.mcp.servers`. The admin page reads them via the admin-gated
// `config.get` RPC and writes the full array back via `config.update` (which
// deep-merges, replacing the servers array) — the same mechanism the generic
// config editor uses, so no MCP-specific backend surface is required.

export type McpTransport = "http" | "stdio";
export type McpAuthMode = "none" | "static" | "user" | "oauth";

/** A single configured MCP server, as stored in `tools.mcp.servers`. */
export interface RawMcpServer {
  name?: string;
  transport?: string;
  url?: string;
  command?: string;
  args?: string[];
  env?: Record<string, string>;
  workingDir?: string;
  enabled?: boolean;
  auth?: string;
  authorization?: string;
  timeoutSeconds?: number;
  oauthClientId?: string;
  oauthClientSecret?: string;
  oauthScopes?: string[];
  oauthAuthorizationUrl?: string;
  oauthTokenUrl?: string;
}

/** Flat, string-based view of a server suited to form inputs. */
export interface McpServerFormValues {
  name: string;
  transport: McpTransport;
  enabled: boolean;
  // http transport
  url: string;
  authMode: McpAuthMode;
  authorization: string;
  oauthClientId: string;
  oauthClientSecret: string;
  oauthScopes: string;
  oauthAuthorizationUrl: string;
  oauthTokenUrl: string;
  // stdio transport
  command: string;
  args: string;
  env: string;
  workingDir: string;
  // common
  timeoutSeconds: string;
}

/** Minimal RPC surface the page needs. */
export interface McpAdminRpcClient {
  sendRpc<T = unknown>(
    method: string,
    params?: Record<string, unknown>,
  ): Promise<T>;
}

/**
 * Resolve the effective transport, mirroring the backend's inference: explicit
 * value wins; otherwise a bare command with no URL is treated as stdio.
 */
export function resolveTransport(server: RawMcpServer): McpTransport {
  if (server.transport === "stdio") return "stdio";
  if (server.transport === "http") return "http";
  const hasCommand = !!server.command?.trim();
  const hasUrl = !!server.url?.trim();
  return hasCommand && !hasUrl ? "stdio" : "http";
}

/**
 * Resolve the effective auth mode for an http server, mirroring the backend:
 * explicit value wins; otherwise a static credential is inferred from the
 * presence of an Authorization value. Stdio servers always use no auth.
 */
export function resolveAuthMode(server: RawMcpServer): McpAuthMode {
  if (resolveTransport(server) === "stdio") return "none";
  if (
    server.auth === "static" ||
    server.auth === "user" ||
    server.auth === "oauth" ||
    server.auth === "none"
  ) {
    return server.auth;
  }
  return server.authorization?.trim() ? "static" : "none";
}

/** A one-line summary (URL or command line) for the server list. */
export function summarizeServer(server: RawMcpServer): string {
  if (resolveTransport(server) === "stdio") {
    return [server.command ?? "", ...(server.args ?? [])].join(" ").trim();
  }
  return server.url ?? "";
}

/** An empty form for the "add server" flow. */
export function emptyForm(): McpServerFormValues {
  return {
    name: "",
    transport: "http",
    enabled: true,
    url: "",
    authMode: "none",
    authorization: "",
    oauthClientId: "",
    oauthClientSecret: "",
    oauthScopes: "",
    oauthAuthorizationUrl: "",
    oauthTokenUrl: "",
    command: "",
    args: "",
    env: "",
    workingDir: "",
    timeoutSeconds: "",
  };
}

/** Convert a stored server into editable form values. */
export function serverToForm(server: RawMcpServer): McpServerFormValues {
  return {
    name: server.name ?? "",
    transport: resolveTransport(server),
    // A missing enabled flag means enabled (the backend treats nil as true).
    enabled: server.enabled !== false,
    url: server.url ?? "",
    authMode: resolveAuthMode(server),
    authorization: server.authorization ?? "",
    oauthClientId: server.oauthClientId ?? "",
    oauthClientSecret: server.oauthClientSecret ?? "",
    oauthScopes: (server.oauthScopes ?? []).join(" "),
    oauthAuthorizationUrl: server.oauthAuthorizationUrl ?? "",
    oauthTokenUrl: server.oauthTokenUrl ?? "",
    command: server.command ?? "",
    args: (server.args ?? []).join("\n"),
    env: Object.entries(server.env ?? {})
      .map(([key, value]) => `${key}=${value}`)
      .join("\n"),
    workingDir: server.workingDir ?? "",
    timeoutSeconds:
      server.timeoutSeconds != null ? String(server.timeoutSeconds) : "",
  };
}

function parseLines(text: string): string[] {
  return text
    .split("\n")
    .map((line) => line.trim())
    .filter((line) => line.length > 0);
}

function parseEnv(text: string): Record<string, string> {
  const env: Record<string, string> = {};
  for (const line of text.split("\n")) {
    const trimmed = line.trim();
    if (!trimmed) continue;
    const separator = trimmed.indexOf("=");
    if (separator <= 0) continue;
    const key = trimmed.slice(0, separator).trim();
    const value = trimmed.slice(separator + 1).trim();
    if (key) env[key] = value;
  }
  return env;
}

function parseScopes(text: string): string[] {
  return text
    .split(/[\s,]+/)
    .map((scope) => scope.trim())
    .filter((scope) => scope.length > 0);
}

/**
 * Build the stored server object from form values, including only the fields
 * relevant to the chosen transport and auth mode so the persisted config stays
 * clean (omitted fields are dropped, matching the backend's omitempty tags).
 */
export function formToServer(form: McpServerFormValues): RawMcpServer {
  const server: RawMcpServer = {
    name: form.name.trim(),
    enabled: form.enabled,
  };

  const timeout = Number.parseInt(form.timeoutSeconds, 10);
  if (form.timeoutSeconds.trim() && Number.isInteger(timeout) && timeout > 0) {
    server.timeoutSeconds = timeout;
  }

  if (form.transport === "stdio") {
    server.transport = "stdio";
    server.command = form.command.trim();
    const args = parseLines(form.args);
    if (args.length) server.args = args;
    const env = parseEnv(form.env);
    if (Object.keys(env).length) server.env = env;
    const workingDir = form.workingDir.trim();
    if (workingDir) server.workingDir = workingDir;
    return server;
  }

  server.transport = "http";
  server.url = form.url.trim();
  server.auth = form.authMode;
  if (form.authMode === "static") {
    if (form.authorization) server.authorization = form.authorization;
  } else if (form.authMode === "oauth") {
    if (form.oauthClientId.trim())
      server.oauthClientId = form.oauthClientId.trim();
    if (form.oauthClientSecret)
      server.oauthClientSecret = form.oauthClientSecret;
    const scopes = parseScopes(form.oauthScopes);
    if (scopes.length) server.oauthScopes = scopes;
    if (form.oauthAuthorizationUrl.trim()) {
      server.oauthAuthorizationUrl = form.oauthAuthorizationUrl.trim();
    }
    if (form.oauthTokenUrl.trim())
      server.oauthTokenUrl = form.oauthTokenUrl.trim();
  }
  return server;
}

/** Per-field validation error keys (i18n keys resolved by the page). */
export interface FormErrors {
  name?: "required" | "duplicate";
  url?: "required" | "invalid";
  command?: "required";
  timeoutSeconds?: "invalid";
}

/**
 * Validate the form. `otherNames` is the set of names already in use by other
 * servers (excluding the one being edited) so renames and duplicates are caught.
 */
export function validateForm(
  form: McpServerFormValues,
  otherNames: string[],
): FormErrors {
  const errors: FormErrors = {};
  const name = form.name.trim();
  if (!name) {
    errors.name = "required";
  } else if (otherNames.includes(name)) {
    errors.name = "duplicate";
  }

  if (form.transport === "http") {
    const url = form.url.trim();
    if (!url) {
      errors.url = "required";
    } else if (!/^https?:\/\//i.test(url)) {
      errors.url = "invalid";
    }
  } else if (!form.command.trim()) {
    errors.command = "required";
  }

  if (form.timeoutSeconds.trim()) {
    const seconds = Number(form.timeoutSeconds);
    if (!Number.isInteger(seconds) || seconds <= 0) {
      errors.timeoutSeconds = "invalid";
    }
  }
  return errors;
}

export function hasErrors(errors: FormErrors): boolean {
  return Object.keys(errors).length > 0;
}

/** Load the configured MCP servers via the admin config RPC. */
export async function loadMcpServers(
  backend: McpAdminRpcClient,
): Promise<RawMcpServer[]> {
  const result = await backend.sendRpc<{
    config?: { tools?: { mcp?: { servers?: RawMcpServer[] } } };
  }>("config.get", {});
  return result.config?.tools?.mcp?.servers ?? [];
}

/** Persist the full MCP server array (replaces `tools.mcp.servers`). */
export async function saveMcpServers(
  backend: McpAdminRpcClient,
  servers: RawMcpServer[],
): Promise<void> {
  await backend.sendRpc("config.update", {
    config: { tools: { mcp: { servers } } },
  });
}
