/**
 * Tool execution handler for the background service worker.
 * Manages content script injection and routes tool requests.
 */

import type {
  ToolExecuteRequest,
  ToolExecuteResponse,
  PageFetchRequest,
  PageFetchResponse,
} from "../shared/types";
import { listCookies, getCookie } from "./cookieHandler";

// Tracks injected tabs: tabId → nonce
const injectedTabs = new Map<number, string>();

function generateNonce(): string {
  const arr = new Uint8Array(16);
  crypto.getRandomValues(arr);
  return Array.from(arr, (b) => b.toString(16).padStart(2, "0")).join("");
}

async function ensureInjected(tabId: number): Promise<string> {
  const existing = injectedTabs.get(tabId);
  if (existing) return existing;
  return injectScripts(tabId);
}

async function injectScripts(tabId: number): Promise<string> {
  const nonce = generateNonce();
  injectedTabs.set(tabId, nonce);

  // Inject page bridge first (MAIN world) with nonce as global.
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  await (chrome.scripting.executeScript as any)({
    target: { tabId },
    world: "MAIN",
    func: (n: string) => {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      (globalThis as any).__tn_nonce = n;
    },
    args: [nonce],
  });

  // Then inject the bundled page bridge.
  await chrome.scripting.executeScript({
    target: { tabId },
    world: "MAIN",
    files: ["dist/page-bridge.js"],
  });

  // Inject content script (ISOLATED world).
  await chrome.scripting.executeScript({
    target: { tabId },
    world: "ISOLATED",
    files: ["dist/content-script.js"],
  });

  return nonce;
}

/** Re-inject scripts on navigation (called from index.ts on tab update). */
export function handleTabNavigation(tabId: number): void {
  if (injectedTabs.has(tabId)) {
    injectedTabs.delete(tabId);
    // Re-inject will happen on next tool request (lazy).
  }
}

/** Clean up injection tracking when a tab is closed. */
export function handleTabRemoved(tabId: number): void {
  injectedTabs.delete(tabId);
}

/** Handle a tool execution request from the side panel. */
export async function handleToolExecute(
  request: ToolExecuteRequest,
): Promise<ToolExecuteResponse> {
  const { toolName, requestId, tabId, arguments: args } = request;

  try {
    if (toolName === "tab.http_request") {
      return await executeHttpRequest(requestId, tabId, args);
    }
    if (toolName === "tab.cookies.list") {
      return await executeCookiesList(requestId, args);
    }
    if (toolName === "tab.cookies.get") {
      return await executeCookiesGet(requestId, args);
    }
    return {
      type: "tool_execute_response",
      requestId,
      error: `unknown tool: ${toolName}`,
    };
  } catch (err) {
    return {
      type: "tool_execute_response",
      requestId,
      error: String(err),
    };
  }
}

async function executeHttpRequest(
  requestId: string,
  tabId: number,
  args: Record<string, unknown>,
): Promise<ToolExecuteResponse> {
  const nonce = await ensureInjected(tabId);

  const fetchReq: PageFetchRequest = {
    type: "page_fetch_request",
    requestId,
    nonce,
    method: (args.method as string) || "GET",
    url: args.url as string,
    headers: args.headers as Record<string, string> | undefined,
    body: args.body as string | undefined,
    timeout_ms: (args.timeout_ms as number) || 30000,
  };

  const response = await chrome.tabs.sendMessage(tabId, fetchReq) as PageFetchResponse;

  if (response.error) {
    return { type: "tool_execute_response", requestId, error: response.error };
  }

  return {
    type: "tool_execute_response",
    requestId,
    result: JSON.stringify(response.result),
  };
}

async function executeCookiesList(
  requestId: string,
  args: Record<string, unknown>,
): Promise<ToolExecuteResponse> {
  const cookies = await listCookies({
    url: args.url as string | undefined,
    domain: args.domain as string | undefined,
    name: args.name as string | undefined,
  });
  return {
    type: "tool_execute_response",
    requestId,
    result: JSON.stringify({ cookies }),
  };
}

async function executeCookiesGet(
  requestId: string,
  args: Record<string, unknown>,
): Promise<ToolExecuteResponse> {
  const url = args.url as string;
  const name = args.name as string;
  if (!name) {
    return {
      type: "tool_execute_response",
      requestId,
      error: "name is required",
    };
  }
  if (!url) {
    return {
      type: "tool_execute_response",
      requestId,
      error: "url is required for cookies.get",
    };
  }
  const cookie = await getCookie({ url, name });
  return {
    type: "tool_execute_response",
    requestId,
    result: JSON.stringify({ cookie }),
  };
}
