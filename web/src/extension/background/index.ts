/**
 * Background service worker entry point.
 * Merges the legacy CDP relay with the new tab tool handler.
 * UI mode: floating overlay (injected iframe on the active tab).
 */

import {
  handleToolExecute,
  handleTabNavigation,
  handleTabRemoved,
} from "./toolHandler";
import { connectOrToggleCdpForActiveTab, getCdpStateForTab } from "./cdpRelay";
import { MSG } from "../shared/messages";
import type {
  ToolExecuteRequest,
  ToolExecuteResponse,
  TabUrlChanged,
  TabClosed,
  CdpStateChanged,
} from "../shared/types";

// Track which tabs have the overlay content script injected.
const overlayInjectedTabs = new Set<number>();

async function injectOverlay(tabId: number): Promise<void> {
  // Inject the overlay content script which creates the shadow DOM + iframe.
  await chrome.scripting.executeScript({
    target: { tabId },
    files: ["dist/overlay-content.js"],
  });
  overlayInjectedTabs.add(tabId);
}

// ---- Extension icon click → toggle floating overlay ----
chrome.action.onClicked.addListener(async (tab) => {
  if (!tab.id) return;

  if (overlayInjectedTabs.has(tab.id)) {
    // Already injected — send toggle message.
    chrome.tabs
      .sendMessage(tab.id, { type: "tn:toggle-overlay" })
      .catch(() => {});
  } else {
    try {
      await injectOverlay(tab.id);
    } catch (err) {
      console.error("Failed to inject overlay:", err);
    }
  }
});

// ---- First install → open options page ----
chrome.runtime.onInstalled.addListener(() => {
  void chrome.runtime.openOptionsPage();
});

// ---- Tab lifecycle events (for content script re-injection + overlay notifications) ----
chrome.tabs.onUpdated.addListener((tabId, changeInfo, tab) => {
  if (changeInfo.status === "complete") {
    handleTabNavigation(tabId);

    // Re-inject overlay if it was previously active on this tab.
    if (overlayInjectedTabs.has(tabId)) {
      overlayInjectedTabs.delete(tabId);
      // Re-inject after navigation completes.
      injectOverlay(tabId).catch(() => {});
    }

    // Notify overlay about URL change.
    const msg: TabUrlChanged = {
      type: "tab_url_changed",
      tabId,
      url: tab.url || "",
      title: tab.title || "",
    };
    chrome.runtime.sendMessage(msg).catch(() => {});
  }
});

chrome.tabs.onRemoved.addListener((tabId) => {
  handleTabRemoved(tabId);
  overlayInjectedTabs.delete(tabId);

  // Notify overlay about tab close.
  const msg: TabClosed = {
    type: "tab_closed",
    tabId,
  };
  chrome.runtime.sendMessage(msg).catch(() => {});
});

// ---- Handle messages from overlay / content scripts ----
chrome.runtime.onMessage.addListener(
  (
    message: { type: string; [key: string]: unknown },
    sender: chrome.runtime.MessageSender,
    sendResponse: (response: ToolExecuteResponse) => void,
  ) => {
    // Overlay content script notifies that it was closed.
    if (message.type === "tn:overlay-closed") {
      const tabId = sender.tab?.id;
      if (tabId) overlayInjectedTabs.delete(tabId);
      return false;
    }

    // CDP toggle request from overlay.
    if (message.type === MSG.CDP_TOGGLE) {
      connectOrToggleCdpForActiveTab().catch(() => {});
      return false;
    }

    // CDP state query from overlay.
    if (message.type === MSG.CDP_STATE_QUERY) {
      const tabId = message.tabId as number;
      const state = getCdpStateForTab(tabId);
      const resp: CdpStateChanged = { type: MSG.CDP_STATE, tabId, state };
      sendResponse(resp as unknown as ToolExecuteResponse);
      return true;
    }

    if (message.type !== "tool_execute") return false;

    handleToolExecute(message as unknown as ToolExecuteRequest)
      .then(sendResponse)
      .catch((err) => {
        sendResponse({
          type: "tool_execute_response",
          requestId: (message as unknown as ToolExecuteRequest).requestId,
          error: String(err),
        });
      });

    return true; // Keep channel open for async response.
  },
);
