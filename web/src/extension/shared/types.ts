// ---- Chrome runtime messages (between extension components) ----

/** Side panel → Background SW: execute a tab tool */
export interface ToolExecuteRequest {
  type: "tool_execute";
  toolName: "tab";
  requestId: string;
  tabId: number;
  arguments: Record<string, unknown>;
}

/** Background SW → Side panel: tool execution result */
export interface ToolExecuteResponse {
  type: "tool_execute_response";
  requestId: string;
  result?: string;
  error?: string;
}

/** Background SW → Content script (chrome.tabs.sendMessage) */
export interface PageFetchRequest {
  type: "page_fetch_request";
  requestId: string;
  nonce: string;
  method: string;
  url: string;
  headers?: Record<string, string>;
  body?: string;
  timeoutMs: number;
}

/** Content script → Background SW response */
export interface PageFetchResponse {
  type: "page_fetch_response";
  requestId: string;
  result?: FetchResult;
  error?: string;
}

/** Background SW → Side panel: tab URL changed */
export interface TabUrlChanged {
  type: "tab_url_changed";
  tabId: number;
  url: string;
  title: string;
}

/** Background SW → Side panel: tab closed */
export interface TabClosed {
  type: "tab_closed";
  tabId: number;
}

/** Fetch result from page bridge */
export interface FetchResult {
  status: number;
  statusText: string;
  headers: Record<string, string>;
  body: string;
  url: string;
  truncated: boolean;
  durationMs: number;
}

// ---- postMessage between content script and page bridge ----

export interface BridgeRequest {
  __tn: string; // nonce
  type: "req";
  id: string;
  payload: {
    method: string;
    url: string;
    headers?: Record<string, string>;
    body?: string;
    timeoutMs: number;
  };
}

export interface BridgeResponse {
  __tn: string; // nonce
  type: "res";
  id: string;
  result?: FetchResult;
  error?: string;
}

// ---- WebSocket RPC types (subset used by extension) ----

export interface RpcRequestFrame {
  type: "req";
  id: string;
  method: string;
  params?: unknown;
}

export interface RpcResponseFrame {
  type: "res";
  id: string;
  ok: boolean;
  payload?: unknown;
  error?: { code: number; message: string };
}

export interface RpcEventFrame {
  type: "event";
  event: string;
  payload?: unknown;
}

export type RpcFrame = RpcResponseFrame | RpcEventFrame;

export type ExtensionMessage =
  | ToolExecuteRequest
  | ToolExecuteResponse
  | TabUrlChanged
  | TabClosed;
