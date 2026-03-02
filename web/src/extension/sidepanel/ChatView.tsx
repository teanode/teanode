import React, { useState, useEffect, useRef, useCallback } from "react";
import { sendRpc, onEvent } from "./rpc";
import type { RpcEventFrame } from "../shared/types";

interface Message {
  role: "user" | "assistant" | "tool_call" | "tool_result";
  content: string;
  toolName?: string;
}

interface Props {
  agentId: string;
  conversationId: string;
}

export function ChatView({ agentId, conversationId }: Props) {
  const [messages, setMessages] = useState<Message[]>([]);
  const [input, setInput] = useState("");
  const [streaming, setStreaming] = useState(false);
  const [streamingText, setStreamingText] = useState("");
  const bottomRef = useRef<HTMLDivElement>(null);

  // Load history on conversation change.
  useEffect(() => {
    if (!conversationId) {
      setMessages([]);
      return;
    }
    setMessages([]);
    setStreamingText("");
    sendRpc("conversations.history", { conversationId })
      .then((payload) => {
        const data = payload as { messages?: Array<{ role: string; content: string; name?: string }> };
        if (data.messages) {
          setMessages(
            data.messages
              .filter((m) => m.role === "user" || m.role === "assistant")
              .map((m) => ({ role: m.role as "user" | "assistant", content: m.content })),
          );
        }
      })
      .catch(() => {});
  }, [conversationId]);

  // Subscribe to conversation events.
  useEffect(() => {
    return onEvent((frame: RpcEventFrame) => {
      if (frame.event !== "conversation") return;
      const p = frame.payload as Record<string, unknown>;
      if (p.conversationId !== conversationId) return;

      switch (p.state) {
        case "delta":
          setStreaming(true);
          setStreamingText((prev) => prev + (p.text as string));
          break;
        case "tool_call":
          setMessages((prev) => [
            ...prev,
            { role: "tool_call", content: p.arguments as string, toolName: p.toolName as string },
          ]);
          break;
        case "tool_result":
          setMessages((prev) => [
            ...prev,
            {
              role: "tool_result",
              content: (p.result as string).slice(0, 500),
              toolName: p.toolName as string,
            },
          ]);
          break;
        case "final":
          setStreaming(false);
          setMessages((prev) => [...prev, { role: "assistant", content: (p.text as string) || "" }]);
          setStreamingText("");
          break;
        case "error":
          setStreaming(false);
          setStreamingText("");
          break;
        case "aborted":
          setStreaming(false);
          setStreamingText("");
          break;
      }
    });
  }, [conversationId]);

  // Auto-scroll.
  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages, streamingText]);

  const handleSend = useCallback(async () => {
    const text = input.trim();
    if (!text || !conversationId || streaming) return;
    setInput("");
    setMessages((prev) => [...prev, { role: "user", content: text }]);
    try {
      await sendRpc("conversations.send", {
        conversationId,
        agentId,
        message: text,
        origin: "webui",
      });
    } catch {
      // Error will come via event.
    }
  }, [input, conversationId, agentId, streaming]);

  return (
    <div style={{ flex: 1, display: "flex", flexDirection: "column", overflow: "hidden" }}>
      {/* Messages */}
      <div style={{ flex: 1, overflowY: "auto", padding: "8px 12px" }}>
        {messages.map((msg, i) => (
          <div
            key={i}
            style={{
              marginBottom: 8,
              padding: "6px 10px",
              borderRadius: 8,
              fontSize: 13,
              lineHeight: 1.4,
              background:
                msg.role === "user"
                  ? "#e3f2fd"
                  : msg.role === "tool_call" || msg.role === "tool_result"
                    ? "#f5f5f5"
                    : "#fff",
              border: "1px solid #e0e0e0",
              wordBreak: "break-word",
              whiteSpace: "pre-wrap",
            }}
          >
            {msg.role === "tool_call" && (
              <div style={{ fontSize: 11, color: "#666", marginBottom: 2 }}>
                Tool: {msg.toolName}
              </div>
            )}
            {msg.role === "tool_result" && (
              <div style={{ fontSize: 11, color: "#666", marginBottom: 2 }}>
                Result: {msg.toolName}
              </div>
            )}
            {msg.content}
          </div>
        ))}
        {streaming && streamingText && (
          <div
            style={{
              marginBottom: 8,
              padding: "6px 10px",
              borderRadius: 8,
              fontSize: 13,
              lineHeight: 1.4,
              background: "#fff",
              border: "1px solid #e0e0e0",
              whiteSpace: "pre-wrap",
            }}
          >
            {streamingText}
            <span style={{ opacity: 0.5 }}>▌</span>
          </div>
        )}
        <div ref={bottomRef} />
      </div>

      {/* Input */}
      <div style={{ padding: "8px 12px", borderTop: "1px solid #e0e0e0", display: "flex", gap: 8 }}>
        <input
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && !e.shiftKey && handleSend()}
          placeholder={conversationId ? "Type a message…" : "Select a conversation"}
          disabled={!conversationId || streaming}
          style={{
            flex: 1,
            padding: "6px 10px",
            border: "1px solid #ccc",
            borderRadius: 6,
            fontSize: 13,
            outline: "none",
          }}
        />
        <button
          onClick={handleSend}
          disabled={!input.trim() || !conversationId || streaming}
          style={{
            padding: "6px 14px",
            fontSize: 13,
            border: "none",
            borderRadius: 6,
            background: "#2d8c5a",
            color: "#fff",
            cursor: "pointer",
          }}
        >
          Send
        </button>
      </div>
    </div>
  );
}
