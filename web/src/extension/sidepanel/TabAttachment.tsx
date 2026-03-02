import React, { useState, useEffect, useCallback } from "react";
import { sendRpc, onEvent } from "./rpc";
import type { RpcEventFrame } from "../shared/types";

interface TabInfo {
  tabId: number;
  url: string;
  title: string;
}

interface Props {
  agentId: string;
  conversationId: string;
}

export function TabAttachment({ agentId, conversationId }: Props) {
  const [attached, setAttached] = useState(false);
  const [tabInfo, setTabInfo] = useState<TabInfo | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const handleAttach = useCallback(async () => {
    if (!conversationId) return;
    setBusy(true);
    setError("");
    try {
      const [active] = await chrome.tabs.query({ active: true, currentWindow: true });
      if (!active?.id || !active.url) {
        setError("No active tab");
        return;
      }
      await sendRpc("tab.attach", {
        agentId,
        conversationId,
        tabUrl: active.url,
        tabTitle: active.title || "",
        tabId: active.id,
      });
      setTabInfo({ tabId: active.id, url: active.url, title: active.title || "" });
      setAttached(true);
    } catch (err) {
      setError(String(err));
    } finally {
      setBusy(false);
    }
  }, [agentId, conversationId]);

  const handleDetach = useCallback(async () => {
    setBusy(true);
    setError("");
    try {
      await sendRpc("tab.detach", { agentId, conversationId });
      setAttached(false);
      setTabInfo(null);
    } catch (err) {
      setError(String(err));
    } finally {
      setBusy(false);
    }
  }, [agentId, conversationId]);

  // Listen for tab URL changes and tab closure from background SW.
  useEffect(() => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const onMessage = (message: any): undefined => {
      if (!tabInfo) return;
      if (message.type === "tab_url_changed" && message.tabId === tabInfo.tabId) {
        setTabInfo({ tabId: message.tabId, url: message.url, title: message.title });
        sendRpc("tab.attach", {
          agentId,
          conversationId,
          tabUrl: message.url,
          tabTitle: message.title,
          tabId: message.tabId,
        }).catch(() => {});
      }
      if (message.type === "tab_closed" && message.tabId === tabInfo.tabId) {
        sendRpc("tab.detach", { agentId, conversationId }).catch(() => {});
        setAttached(false);
        setTabInfo(null);
      }
      return undefined;
    };
    chrome.runtime.onMessage.addListener(onMessage);
    return () => chrome.runtime.onMessage.removeListener(onMessage);
  }, [tabInfo, agentId, conversationId]);

  // Listen for tab_attachment events from server (e.g., detached by backend).
  useEffect(() => {
    return onEvent((frame: RpcEventFrame) => {
      if (frame.event !== "tab_attachment") return;
      const p = frame.payload as Record<string, unknown>;
      if (p.agentId !== agentId || p.conversationId !== conversationId) return;
      if (p.action === "detached") {
        setAttached(false);
        setTabInfo(null);
      }
    });
  }, [agentId, conversationId]);

  // Reset when conversation changes.
  useEffect(() => {
    setAttached(false);
    setTabInfo(null);
    setError("");
  }, [conversationId]);

  return (
    <div style={{ padding: "8px 12px", borderBottom: "1px solid #e0e0e0", fontSize: 13 }}>
      <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
        <span
          style={{
            width: 8,
            height: 8,
            borderRadius: "50%",
            backgroundColor: attached ? "#4caf50" : "#bdbdbd",
            display: "inline-block",
            flexShrink: 0,
          }}
        />
        {attached && tabInfo ? (
          <span style={{ flex: 1, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
            {tabInfo.title || tabInfo.url}
          </span>
        ) : (
          <span style={{ flex: 1, color: "#999" }}>No tab attached</span>
        )}
        <button
          onClick={attached ? handleDetach : handleAttach}
          disabled={busy || !conversationId}
          style={{
            padding: "2px 10px",
            fontSize: 12,
            cursor: busy ? "wait" : "pointer",
            border: "1px solid #ccc",
            borderRadius: 4,
            background: attached ? "#fff" : "#2d8c5a",
            color: attached ? "#333" : "#fff",
          }}
        >
          {busy ? "…" : attached ? "Detach" : "Attach Tab"}
        </button>
      </div>
      {error && <div style={{ color: "#d32f2f", fontSize: 11, marginTop: 4 }}>{error}</div>}
    </div>
  );
}
