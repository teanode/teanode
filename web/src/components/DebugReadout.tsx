import React from "react";
import type { ActiveRunState } from "../types";

interface DebugReadoutProps {
  conversationId: string;
  connected: boolean;
  activeRunId: string | null;
  lastActiveRunState: ActiveRunState | null;
  isRunning: boolean;
  status: string;
  isStreaming: boolean;
  toolActivity: string | null;
  currentRunId: string | null;
}

/** Visible only when ?debug=1 or localStorage.debug === "1". */
export function useDebugEnabled(): boolean {
  if (typeof window === "undefined") return false;
  try {
    if (new URLSearchParams(window.location.search).get("debug") === "1")
      return true;
    if (localStorage.getItem("debug") === "1") return true;
  } catch {
    // ignore
  }
  return false;
}

export default function DebugReadout(props: DebugReadoutProps) {
  const {
    conversationId,
    connected,
    activeRunId,
    lastActiveRunState,
    isRunning,
    status,
    isStreaming,
    toolActivity,
    currentRunId,
  } = props;

  return (
    <div
      style={{
        position: "fixed",
        bottom: 80,
        right: 8,
        zIndex: 9999,
        background: "rgba(0,0,0,0.82)",
        color: "#0f0",
        fontFamily: "monospace",
        fontSize: 11,
        lineHeight: "1.45",
        padding: "6px 10px",
        borderRadius: 6,
        maxWidth: 340,
        pointerEvents: "none",
        whiteSpace: "pre-wrap",
        wordBreak: "break-all",
      }}
    >
      <div>
        <b>conversationId:</b> {conversationId}
      </div>
      <div>
        <b>connected:</b> {String(connected)}
      </div>
      <div>
        <b>activeRunId:</b> {activeRunId ?? "null"}
      </div>
      <div>
        <b>activeRunState.phase:</b>{" "}
        {lastActiveRunState?.phase ?? "null"}
      </div>
      <div>
        <b>activeRunState.toolName:</b>{" "}
        {lastActiveRunState?.toolName ?? "null"}
      </div>
      <hr style={{ borderColor: "#333", margin: "4px 0" }} />
      <div>
        <b>isRunning:</b> {String(isRunning)}
      </div>
      <div>
        <b>status:</b> {status}
      </div>
      <div>
        <b>isStreaming:</b> {String(isStreaming)}
      </div>
      <div>
        <b>toolActivity:</b> {toolActivity ?? "null"}
      </div>
      <div>
        <b>currentRunId:</b> {currentRunId ?? "null"}
      </div>
    </div>
  );
}
