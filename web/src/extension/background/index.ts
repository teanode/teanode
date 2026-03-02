/**
 * Background service worker entry point.
 * Merges the legacy CDP relay with the new tab tool handler.
 */

import { connectOrToggleCdpForActiveTab } from "./cdpRelay";
import { handleToolExecute, handleTabNavigation, handleTabRemoved } from "./toolHandler";
import type { ToolExecuteRequest, ToolExecuteResponse, TabUrlChanged, TabClosed } from "../shared/types";

// ---- Extension icon click → open side panel ----
chrome.action.onClicked.addListener(async (tab) => {
  if (tab.id) {
    // Try to open side panel; if the API is unavailable, fall back to CDP toggle.
    try {
      await chrome.sidePanel.open({ tabId: tab.id });
    } catch {
      await connectOrToggleCdpForActiveTab();
    }
  }
});

// ---- First install → open options page ----
chrome.runtime.onInstalled.addListener(() => {
  void chrome.runtime.openOptionsPage();
});

// ---- Tab lifecycle events (for content script re-injection + side panel notifications) ----
chrome.tabs.onUpdated.addListener((tabId, changeInfo, tab) => {
  if (changeInfo.status === "complete") {
    handleTabNavigation(tabId);

    // Notify side panel about URL change.
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

  // Notify side panel about tab close.
  const msg: TabClosed = {
    type: "tab_closed",
    tabId,
  };
  chrome.runtime.sendMessage(msg).catch(() => {});
});

// ---- Handle tool execution requests from side panel ----
chrome.runtime.onMessage.addListener(
  (
    message: ToolExecuteRequest,
    _sender: chrome.runtime.MessageSender,
    sendResponse: (response: ToolExecuteResponse) => void,
  ) => {
    if (message.type !== "tool_execute") return false;

    handleToolExecute(message).then(sendResponse).catch((err) => {
      sendResponse({
        type: "tool_execute_response",
        requestId: message.requestId,
        error: String(err),
      });
    });

    return true; // Keep channel open for async response.
  },
);
