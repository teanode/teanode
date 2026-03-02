/**
 * Background service worker entry point.
 * Merges the legacy CDP relay with the new tab tool handler.
 * Supports two UI modes: "sidepanel" (default) and "overlay" (floating iframe).
 */

import { connectOrToggleCdpForActiveTab } from "./cdpRelay";
import { handleToolExecute, handleTabNavigation, handleTabRemoved } from "./toolHandler";
import type { ToolExecuteRequest, ToolExecuteResponse, TabUrlChanged, TabClosed } from "../shared/types";

// Track which tabs have the overlay content script injected.
const overlayInjectedTabs = new Set<number>();

async function getUiMode(): Promise<"sidepanel" | "overlay"> {
  const stored = await chrome.storage.local.get(["uiMode"]);
  return stored.uiMode === "overlay" ? "overlay" : "sidepanel";
}

async function injectOverlay(tabId: number): Promise<void> {
  // Inject the overlay content script which creates the shadow DOM + iframe.
  await chrome.scripting.executeScript({
    target: { tabId },
    files: ["dist/overlay-content.js"],
  });
  overlayInjectedTabs.add(tabId);
}

// ---- Extension icon click → open side panel or inject overlay ----
chrome.action.onClicked.addListener(async (tab) => {
  if (!tab.id) return;
  const mode = await getUiMode();

  if (mode === "overlay") {
    if (overlayInjectedTabs.has(tab.id)) {
      // Already injected — send toggle message.
      chrome.tabs.sendMessage(tab.id, { type: "tn:toggle-overlay" }).catch(() => {});
    } else {
      try {
        await injectOverlay(tab.id);
      } catch (err) {
        console.error("Failed to inject overlay:", err);
      }
    }
  } else {
    // Side panel mode
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

    // Re-inject overlay if it was previously active on this tab.
    if (overlayInjectedTabs.has(tabId)) {
      overlayInjectedTabs.delete(tabId);
      // Re-inject after navigation completes.
      injectOverlay(tabId).catch(() => {});
    }

    // Notify side panel / overlay about URL change.
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

  // Notify side panel about tab close.
  const msg: TabClosed = {
    type: "tab_closed",
    tabId,
  };
  chrome.runtime.sendMessage(msg).catch(() => {});
});

// ---- Handle tool execution requests from side panel / overlay ----
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
