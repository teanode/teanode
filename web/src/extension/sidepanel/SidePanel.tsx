import React, { useState, useEffect, useCallback } from "react";
import { sendRpc, onEvent, ensureConnected } from "./rpc";
import { TabAttachment } from "./TabAttachment";
import { ChatView } from "./ChatView";
import type { RpcEventFrame, ToolExecuteRequest, ToolExecuteResponse } from "../shared/types";

interface Agent {
  id: string;
  name?: string;
}

interface Conversation {
  id: string;
  summary?: string;
  agentId?: string;
}

export function SidePanel() {
  const [connected, setConnected] = useState(false);
  const [agents, setAgents] = useState<Agent[]>([]);
  const [agentId, setAgentId] = useState("default-agent");
  const [conversations, setConversations] = useState<Conversation[]>([]);
  const [conversationId, setConversationId] = useState("");
  const [error, setError] = useState("");

  // Connect on mount.
  useEffect(() => {
    ensureConnected()
      .then(() => {
        setConnected(true);
        setError("");
      })
      .catch((err) => setError(String(err)));
  }, []);

  // Load agents.
  useEffect(() => {
    if (!connected) return;
    sendRpc("agents.list")
      .then((payload) => {
        const data = payload as { agents?: Agent[] };
        if (data.agents) setAgents(data.agents);
      })
      .catch(() => {});
  }, [connected]);

  // Load conversations when agent changes.
  useEffect(() => {
    if (!connected || !agentId) return;
    sendRpc("conversations.list", { agentId })
      .then((payload) => {
        const data = payload as { conversations?: Conversation[] };
        if (data.conversations) {
          setConversations(data.conversations);
          // Select first if none selected.
          if (!conversationId && data.conversations.length > 0) {
            setConversationId(data.conversations[0].id);
          }
        }
      })
      .catch(() => {});
  }, [connected, agentId]);

  // Listen for conversation list changes.
  useEffect(() => {
    return onEvent((frame: RpcEventFrame) => {
      if (frame.event === "conversations") {
        sendRpc("conversations.list", { agentId })
          .then((payload) => {
            const data = payload as { conversations?: Conversation[] };
            if (data.conversations) setConversations(data.conversations);
          })
          .catch(() => {});
      }
    });
  }, [agentId]);

  // Handle tab_tool_call events: forward to background SW, then send result back.
  useEffect(() => {
    if (!connected) return;
    return onEvent(async (frame: RpcEventFrame) => {
      if (frame.event !== "tab_tool_call") return;
      const p = frame.payload as Record<string, unknown>;
      if (p.agentId !== agentId || p.conversationId !== conversationId) return;

      // Get the attached tab ID.
      const [activeTab] = await chrome.tabs.query({ active: true, currentWindow: true });
      const tabId = activeTab?.id;
      if (!tabId) {
        // No tab available — send error result.
        await sendRpc("tab.tool_result", {
          requestId: p.requestId,
          error: "no active tab",
        }).catch(() => {});
        return;
      }

      // Forward to background SW.
      const request: ToolExecuteRequest = {
        type: "tool_execute",
        toolName: p.toolName as ToolExecuteRequest["toolName"],
        requestId: p.requestId as string,
        tabId,
        arguments: p.arguments as Record<string, unknown>,
      };

      try {
        const response = await chrome.runtime.sendMessage(request) as ToolExecuteResponse;
        await sendRpc("tab.tool_result", {
          requestId: p.requestId,
          result: response.result || "",
          error: response.error || "",
        });
      } catch (err) {
        await sendRpc("tab.tool_result", {
          requestId: p.requestId,
          error: String(err),
        }).catch(() => {});
      }
    });
  }, [connected, agentId, conversationId]);

  const handleNewConversation = useCallback(async () => {
    // Simply clear conversation ID — the next send will create a new one.
    setConversationId("");
  }, []);

  if (error) {
    return (
      <div style={{ padding: 16, color: "#d32f2f", fontSize: 13 }}>
        <div style={{ fontWeight: 600, marginBottom: 8 }}>Connection Error</div>
        <div>{error}</div>
        <div style={{ marginTop: 12, fontSize: 12, color: "#666" }}>
          Check that TeaNode is running and the URL/token are configured in the extension options.
        </div>
        <button
          onClick={() => {
            setError("");
            ensureConnected()
              .then(() => { setConnected(true); setError(""); })
              .catch((err) => setError(String(err)));
          }}
          style={{ marginTop: 12, padding: "4px 12px", fontSize: 12, cursor: "pointer" }}
        >
          Retry
        </button>
      </div>
    );
  }

  if (!connected) {
    return <div style={{ padding: 16, fontSize: 13, color: "#999" }}>Connecting…</div>;
  }

  return (
    <div style={{ display: "flex", flexDirection: "column", height: "100%" }}>
      {/* Header: agent + conversation selectors */}
      <div style={{ padding: "8px 12px", borderBottom: "1px solid #e0e0e0" }}>
        {/* Agent selector */}
        <div style={{ marginBottom: 6 }}>
          <select
            value={agentId}
            onChange={(e) => { setAgentId(e.target.value); setConversationId(""); }}
            style={{ width: "100%", padding: "4px 8px", fontSize: 12, borderRadius: 4, border: "1px solid #ccc" }}
          >
            {agents.map((a) => (
              <option key={a.id} value={a.id}>
                {a.name || a.id}
              </option>
            ))}
          </select>
        </div>

        {/* Conversation selector */}
        <div style={{ display: "flex", gap: 6 }}>
          <select
            value={conversationId}
            onChange={(e) => setConversationId(e.target.value)}
            style={{ flex: 1, padding: "4px 8px", fontSize: 12, borderRadius: 4, border: "1px solid #ccc" }}
          >
            <option value="">— Select conversation —</option>
            {conversations.map((c) => (
              <option key={c.id} value={c.id}>
                {c.summary || c.id.slice(0, 12)}
              </option>
            ))}
          </select>
          <button
            onClick={handleNewConversation}
            style={{
              padding: "4px 10px",
              fontSize: 12,
              border: "1px solid #ccc",
              borderRadius: 4,
              cursor: "pointer",
              background: "#fff",
            }}
          >
            New
          </button>
        </div>
      </div>

      {/* Tab attachment */}
      <TabAttachment agentId={agentId} conversationId={conversationId} />

      {/* Chat */}
      <ChatView agentId={agentId} conversationId={conversationId} />
    </div>
  );
}
