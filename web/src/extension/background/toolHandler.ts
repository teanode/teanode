/**
 * Tool execution handler for the background service worker.
 * Manages content script injection and routes tool requests by action.
 */

import type {
  ToolExecuteRequest,
  ToolExecuteResponse,
  PageFetchRequest,
  PageFetchResponse,
  PageActionRequest,
  PageActionResponse,
  PageActionType,
} from "../shared/types";
import {
  listCookies,
  getCookie,
  setCookie,
  deleteCookie,
} from "./cookieHandler";

// Actions that require page bridge execution via content script.
const PAGE_ACTIONS: Set<string> = new Set([
  "getLocalStorage",
  "setLocalStorage",
  "removeLocalStorage",
  "snapshotDom",
  "querySelector",
  "eval",
]);

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
  await (chrome.scripting.executeScript as any)({
    target: { tabId },
    world: "MAIN",
    func: (n: string) => {
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
  const { requestId, tabId, arguments: args } = request;
  const action = args.action as string;

  try {
    switch (action) {
      case "fetch":
        return await executeFetch(requestId, tabId, args);
      case "listCookies":
        return await executeListCookies(requestId, args);
      case "getCookie":
        return await executeGetCookie(requestId, args);
      case "setCookie":
        return await executeSetCookie(requestId, args);
      case "deleteCookie":
        return await executeDeleteCookie(requestId, args);
      default:
        if (PAGE_ACTIONS.has(action)) {
          return await executePageAction(
            requestId,
            tabId,
            action as PageActionType,
            args,
          );
        }
        return {
          type: "tool_execute_response",
          requestId,
          error: `unknown action: ${action}`,
        };
    }
  } catch (err) {
    return {
      type: "tool_execute_response",
      requestId,
      error: String(err),
    };
  }
}

async function executeFetch(
  requestId: string,
  tabId: number,
  args: Record<string, unknown>,
): Promise<ToolExecuteResponse> {
  const nonce = await ensureInjected(tabId);

  const fetchRequest: PageFetchRequest = {
    type: "page_fetch_request",
    requestId,
    nonce,
    method: (args.method as string) || "GET",
    url: args.url as string,
    headers: args.headers as Record<string, string> | undefined,
    body: args.body as string | undefined,
    timeoutMs: (args.timeoutMs as number) || 30000,
  };

  const response = (await chrome.tabs.sendMessage(
    tabId,
    fetchRequest,
  )) as PageFetchResponse;

  if (response.error) {
    return { type: "tool_execute_response", requestId, error: response.error };
  }

  return {
    type: "tool_execute_response",
    requestId,
    result: JSON.stringify(response.result),
  };
}

async function executeListCookies(
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

async function executeGetCookie(
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
      error: "url is required for getCookie",
    };
  }
  const cookie = await getCookie({ url, name });
  return {
    type: "tool_execute_response",
    requestId,
    result: JSON.stringify({ cookie }),
  };
}

async function executeSetCookie(
  requestId: string,
  args: Record<string, unknown>,
): Promise<ToolExecuteResponse> {
  const url = args.url as string;
  const name = args.name as string;
  const value = args.value as string;
  if (!url) {
    return {
      type: "tool_execute_response",
      requestId,
      error: "url is required for setCookie",
    };
  }
  if (!name) {
    return {
      type: "tool_execute_response",
      requestId,
      error: "name is required for setCookie",
    };
  }
  const cookie = await setCookie({
    url,
    name,
    value: value ?? "",
    domain: args.domain as string | undefined,
    path: args.path as string | undefined,
    secure: args.secure as boolean | undefined,
    httpOnly: args.httpOnly as boolean | undefined,
    sameSite: args.sameSite as chrome.cookies.SameSiteStatus | undefined,
    expirationDate: args.expirationDate as number | undefined,
  });
  return {
    type: "tool_execute_response",
    requestId,
    result: JSON.stringify({ cookie }),
  };
}

async function executeDeleteCookie(
  requestId: string,
  args: Record<string, unknown>,
): Promise<ToolExecuteResponse> {
  const url = args.url as string;
  const name = args.name as string;
  if (!url) {
    return {
      type: "tool_execute_response",
      requestId,
      error: "url is required for deleteCookie",
    };
  }
  if (!name) {
    return {
      type: "tool_execute_response",
      requestId,
      error: "name is required for deleteCookie",
    };
  }
  const details = await deleteCookie({ url, name });
  return {
    type: "tool_execute_response",
    requestId,
    result: JSON.stringify({ ok: details !== null }),
  };
}

/**
 * Execute a page action (localStorage, DOM, eval) via content script relay.
 * Sends a PageActionRequest to the content script, which forwards it to the
 * page bridge via postMessage.
 */
async function executePageAction(
  requestId: string,
  tabId: number,
  action: PageActionType,
  args: Record<string, unknown>,
): Promise<ToolExecuteResponse> {
  const nonce = await ensureInjected(tabId);

  // Build the params object from the args, excluding meta fields.
  const { action: _action, ...params } = args;

  const actionRequest: PageActionRequest = {
    type: "page_action_request",
    requestId,
    nonce,
    action,
    params,
  };

  const response = (await chrome.tabs.sendMessage(
    tabId,
    actionRequest,
  )) as PageActionResponse;

  if (response.error) {
    return { type: "tool_execute_response", requestId, error: response.error };
  }

  return {
    type: "tool_execute_response",
    requestId,
    result: response.result,
  };
}
