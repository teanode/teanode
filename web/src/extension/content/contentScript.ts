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
  PageStepsRequest,
  PageStepsResponse,
  BridgeResponse,
  BridgeActionResponse,
  BridgeStepsResponse,
} from "../shared/types";

type PendingBridgeRequest<T> = {
  nonce: string;
  resolve: (response: T) => void;
};

// Pending fetch requests: requestId → resolve callback
const pendingFetches = new Map<string, PendingBridgeRequest<BridgeResponse>>();

// Pending action requests: requestId → resolve callback
const pendingActions = new Map<
  string,
  PendingBridgeRequest<BridgeActionResponse>
>();

// Pending steps requests: requestId → resolve callback
const pendingSteps = new Map<
  string,
  PendingBridgeRequest<BridgeStepsResponse>
>();

// Listen for messages from the background SW.
chrome.runtime.onMessage.addListener(
  (
    message: PageFetchRequest | PageActionRequest | PageStepsRequest,
    _sender: chrome.runtime.MessageSender,
    sendResponse: (
      response: PageFetchResponse | PageActionResponse | PageStepsResponse,
    ) => void,
  ) => {
    if (message.type === "page_fetch_request") {
      handleFetchRequest(
        message as PageFetchRequest,
        sendResponse as (response: PageFetchResponse) => void,
      );
      return true; // Keep the message channel open for async response.
    }

    if (message.type === "page_action_request") {
      handleActionRequest(
        message as PageActionRequest,
        sendResponse as (response: PageActionResponse) => void,
      );
      return true;
    }

    if (message.type === "page_steps_request") {
      handleStepsRequest(
        message as PageStepsRequest,
        sendResponse as (response: PageStepsResponse) => void,
      );
      return true;
    }

    return false;
  },
);

function handleFetchRequest(
  message: PageFetchRequest,
  sendResponse: (response: PageFetchResponse) => void,
): void {
  const nonce = message.nonce;
  const requestId = message.requestId;

  // Set up a one-time listener for the page bridge response.
  const promise = new Promise<BridgeResponse>((resolve) => {
    pendingFetches.set(requestId, { nonce, resolve });

    // Timeout: if the page bridge doesn't respond, reject.
    setTimeout(
      () => {
        if (pendingFetches.has(requestId)) {
          pendingFetches.delete(requestId);
          resolve({
            __tn: nonce,
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
      __tn: nonce,
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
  const nonce = message.nonce;
  const requestId = message.requestId;

  // Use a longer timeout for wait actions (they can block up to 30s+ themselves).
  const actionTimeoutMs =
    message.action === "wait"
      ? ((message.params.timeoutMs as number) || 30000) + 10000
      : 35000;

  const promise = new Promise<BridgeActionResponse>((resolve) => {
    pendingActions.set(requestId, { nonce, resolve });

    setTimeout(() => {
      if (pendingActions.has(requestId)) {
        pendingActions.delete(requestId);
        resolve({
          __tn: nonce,
          type: "action_res",
          id: requestId,
          error: "page bridge timeout",
        });
      }
    }, actionTimeoutMs);
  });

  // Forward to page bridge via postMessage.
  window.postMessage(
    {
      __tn: nonce,
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

function handleStepsRequest(
  message: PageStepsRequest,
  sendResponse: (response: PageStepsResponse) => void,
): void {
  const nonce = message.nonce;
  const requestId = message.requestId;

  const promise = new Promise<BridgeStepsResponse>((resolve) => {
    pendingSteps.set(requestId, { nonce, resolve });

    // Timeout: steps can take a while (up to 2 minutes by default).
    setTimeout(
      () => {
        if (pendingSteps.has(requestId)) {
          pendingSteps.delete(requestId);
          resolve({
            __tn: nonce,
            type: "steps_res",
            id: requestId,
            error: "page bridge timeout",
          });
        }
      },
      (message.timeoutMs || 120000) + 10000,
    );
  });

  // Forward to page bridge via postMessage.
  window.postMessage(
    {
      __tn: nonce,
      type: "steps_req",
      id: requestId,
      steps: message.steps,
    },
    "*",
  );

  promise.then((bridgeResp) => {
    const response: PageStepsResponse = {
      type: "page_steps_response",
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
  if (!data || typeof data.id !== "string" || typeof data.__tn !== "string") {
    return;
  }

  if (data.type === "res") {
    const requestId = data.id as string;
    const pending = pendingFetches.get(requestId);
    if (!pending || pending.nonce !== data.__tn) return;
    pendingFetches.delete(requestId);
    pending.resolve(data as BridgeResponse);
    return;
  }

  if (data.type === "action_res") {
    const requestId = data.id as string;
    const pending = pendingActions.get(requestId);
    if (!pending || pending.nonce !== data.__tn) return;
    pendingActions.delete(requestId);
    pending.resolve(data as BridgeActionResponse);
    return;
  }

  if (data.type === "steps_res") {
    const requestId = data.id as string;
    const pending = pendingSteps.get(requestId);
    if (!pending || pending.nonce !== data.__tn) return;
    pendingSteps.delete(requestId);
    pending.resolve(data as BridgeStepsResponse);
  }
});
