/**
 * Content script injected into ISOLATED world.
 * Acts as relay between background SW (chrome.runtime) and page bridge (postMessage).
 * The nonce is passed via chrome.scripting.executeScript args at injection time
 * and stored in the background SW's injection tracker.
 */

import type {
  PageFetchRequest,
  PageFetchResponse,
  PageActionRequest,
  PageActionResponse,
  BridgeResponse,
  BridgeActionResponse,
} from "../shared/types";

// Nonce is set by the background SW when injecting. It is stored per-tab
// in the background and passed to us via chrome.tabs.sendMessage.
let currentNonce = "";

// Pending fetch requests: requestId → resolve callback
const pendingFetches = new Map<string, (resp: BridgeResponse) => void>();

// Pending action requests: requestId → resolve callback
const pendingActions = new Map<string, (resp: BridgeActionResponse) => void>();

// Listen for messages from the background SW.
chrome.runtime.onMessage.addListener(
  (
    message: PageFetchRequest | PageActionRequest,
    _sender: chrome.runtime.MessageSender,
    sendResponse: (response: PageFetchResponse | PageActionResponse) => void,
  ) => {
    if (message.type === "page_fetch_request") {
      handleFetchRequest(message as PageFetchRequest, sendResponse);
      return true; // Keep the message channel open for async response.
    }

    if (message.type === "page_action_request") {
      handleActionRequest(message as PageActionRequest, sendResponse);
      return true;
    }

    return false;
  },
);

function handleFetchRequest(
  message: PageFetchRequest,
  sendResponse: (response: PageFetchResponse) => void,
): void {
  currentNonce = message.nonce;
  const requestId = message.requestId;

  // Set up a one-time listener for the page bridge response.
  const promise = new Promise<BridgeResponse>((resolve) => {
    pendingFetches.set(requestId, resolve);

    // Timeout: if the page bridge doesn't respond, reject.
    setTimeout(
      () => {
        if (pendingFetches.has(requestId)) {
          pendingFetches.delete(requestId);
          resolve({
            __tn: currentNonce,
            type: "res",
            id: requestId,
            error: "page bridge timeout",
          });
        }
      },
      (message.timeoutMs || 30000) + 5000,
    );
  });

  // Forward to page bridge via postMessage.
  window.postMessage(
    {
      __tn: currentNonce,
      type: "req",
      id: requestId,
      payload: {
        method: message.method,
        url: message.url,
        headers: message.headers,
        body: message.body,
        timeoutMs: message.timeoutMs,
      },
    },
    "*",
  );

  // Async response.
  promise.then((bridgeResp) => {
    const response: PageFetchResponse = {
      type: "page_fetch_response",
      requestId,
      result: bridgeResp.result,
      error: bridgeResp.error,
    };
    sendResponse(response);
  });
}

function handleActionRequest(
  message: PageActionRequest,
  sendResponse: (response: PageActionResponse) => void,
): void {
  currentNonce = message.nonce;
  const requestId = message.requestId;

  const promise = new Promise<BridgeActionResponse>((resolve) => {
    pendingActions.set(requestId, resolve);

    // 30 second timeout for page actions.
    setTimeout(() => {
      if (pendingActions.has(requestId)) {
        pendingActions.delete(requestId);
        resolve({
          __tn: currentNonce,
          type: "action_res",
          id: requestId,
          error: "page bridge timeout",
        });
      }
    }, 35000);
  });

  // Forward to page bridge via postMessage.
  window.postMessage(
    {
      __tn: currentNonce,
      type: "action_req",
      id: requestId,
      action: message.action,
      params: message.params,
    },
    "*",
  );

  promise.then((bridgeResp) => {
    const response: PageActionResponse = {
      type: "page_action_response",
      requestId,
      result:
        bridgeResp.result != null
          ? JSON.stringify(bridgeResp.result)
          : undefined,
      error: bridgeResp.error,
    };
    sendResponse(response);
  });
}

// Listen for postMessage responses from the page bridge.
window.addEventListener("message", (event: MessageEvent) => {
  if (event.source !== window) return;
  const data = event.data;
  if (!data || data.__tn !== currentNonce) return;

  if (data.type === "res") {
    const requestId = data.id as string;
    const resolver = pendingFetches.get(requestId);
    if (!resolver) return;
    pendingFetches.delete(requestId);
    resolver(data as BridgeResponse);
    return;
  }

  if (data.type === "action_res") {
    const requestId = data.id as string;
    const resolver = pendingActions.get(requestId);
    if (!resolver) return;
    pendingActions.delete(requestId);
    resolver(data as BridgeActionResponse);
  }
});
