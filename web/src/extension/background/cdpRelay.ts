/**
 * CDP (Chrome DevTools Protocol) relay — ported from background.js to TypeScript.
 * Connects to TeaNode's /api/v1/browser WebSocket and relays CDP commands/events
 * between Chrome tabs and the backend.
 *
 * Behavior is unchanged from the original plain-JS implementation.
 */

const BADGE = {
  on: { text: "ON", color: "#FF5A36" },
  off: { text: "", color: "#000000" },
  connecting: { text: "…", color: "#F59E0B" },
  error: { text: "!", color: "#B91C1C" },
} as const;

interface TabState {
  state: "connecting" | "connected";
  sessionId?: string;
  targetId?: string;
  attachOrder?: number;
}

interface PendingRequest {
  resolve: (v: unknown) => void;
  reject: (e: Error) => void;
}

import type { CdpState, CdpStateChanged } from "../shared/types";
import { MSG } from "../shared/messages";

let relayWs: WebSocket | null = null;
let relayConnectPromise: Promise<void> | null = null;
let debuggerListenersInstalled = false;

const tabs = new Map<number, TabState>();
const tabBySession = new Map<string, number>();
const childSessionToTab = new Map<string, number>();
const pending = new Map<number, PendingRequest>();

function broadcastCdpState(tabId: number, state: CdpState): void {
  const message: CdpStateChanged = { type: MSG.CDP_STATE, tabId, state };
  chrome.runtime.sendMessage(message).catch(() => {});
}

/** Return the current CDP state for a given tab. */
export function getCdpStateForTab(tabId: number): CdpState {
  const tab = tabs.get(tabId);
  if (!tab) return "detached";
  if (tab.state === "connecting") return "connecting";
  return "attached";
}

function generateULID(): string {
  const encoding = "0123456789ABCDEFGHJKMNPQRSTVWXYZ";
  const timestamp = Date.now();
  let result = "";
  let remaining = timestamp;
  for (let i = 9; i >= 0; i--) {
    result = encoding[remaining % 32] + result;
    remaining = Math.floor(remaining / 32);
  }
  for (let i = 0; i < 16; i++) {
    result += encoding[Math.floor(Math.random() * 32)];
  }
  return result;
}

export async function getRelayUrl(): Promise<string> {
  const stored = await chrome.storage.local.get(["relayUrl"]);
  return ((stored.relayUrl as string) || "").trim() || "http://127.0.0.1:8833";
}

export async function getRelayToken(): Promise<string> {
  const stored = await chrome.storage.local.get(["relayToken"]);
  return (stored.relayToken as string) || "";
}

function httpToWs(url: string): string {
  if (url.startsWith("https://")) return "wss://" + url.slice(8);
  if (url.startsWith("http://")) return "ws://" + url.slice(7);
  return "ws://" + url;
}

function setBadge(tabId: number, kind: keyof typeof BADGE): void {
  const config = BADGE[kind];
  void chrome.action.setBadgeText({ tabId, text: config.text });
  void chrome.action.setBadgeBackgroundColor({ tabId, color: config.color });
  void chrome.action
    .setBadgeTextColor({ tabId, color: "#FFFFFF" })
    .catch(() => {});
}

async function ensureRelayConnection(): Promise<void> {
  if (relayWs && relayWs.readyState === WebSocket.OPEN) return;
  if (relayConnectPromise) return await relayConnectPromise;

  relayConnectPromise = (async () => {
    const baseUrl = (await getRelayUrl()).replace(/\/+$/, "");
    const token = await getRelayToken();
    let wsUrl = httpToWs(baseUrl) + "/api/v1/browser";
    if (token) wsUrl += `?token=${encodeURIComponent(token)}`;

    try {
      const headers: Record<string, string> = {};
      if (token) headers["Authorization"] = `Bearer ${token}`;
      await fetch(`${baseUrl}/api/v1/health`, {
        method: "HEAD",
        signal: AbortSignal.timeout(2000),
        headers,
      });
    } catch (err) {
      throw new Error(
        `Relay server not reachable at ${baseUrl} (${String(err)})`,
      );
    }

    const ws = new WebSocket(wsUrl);
    relayWs = ws;

    await new Promise<void>((resolve, reject) => {
      const t = setTimeout(
        () => reject(new Error("WebSocket connect timeout")),
        5000,
      );
      ws.onopen = () => {
        clearTimeout(t);
        resolve();
      };
      ws.onerror = () => {
        clearTimeout(t);
        reject(new Error("WebSocket connect failed"));
      };
      ws.onclose = (ev) => {
        clearTimeout(t);
        reject(
          new Error(
            `WebSocket closed (${ev.code} ${ev.reason || "no reason"})`,
          ),
        );
      };
    });

    ws.onmessage = (event) => void onRelayMessage(String(event.data || ""));
    ws.onclose = () => onRelayClosed("closed");
    ws.onerror = () => onRelayClosed("error");

    if (!debuggerListenersInstalled) {
      debuggerListenersInstalled = true;
      chrome.debugger.onEvent.addListener(onDebuggerEvent);
      chrome.debugger.onDetach.addListener(onDebuggerDetach);
    }
  })();

  try {
    await relayConnectPromise;
  } finally {
    relayConnectPromise = null;
  }
}

function onRelayClosed(reason: string): void {
  relayWs = null;
  for (const [id, p] of pending.entries()) {
    pending.delete(id);
    p.reject(new Error(`Relay disconnected (${reason})`));
  }

  for (const tabId of tabs.keys()) {
    void chrome.debugger.detach({ tabId }).catch(() => {});
    setBadge(tabId, "connecting");
    void chrome.action.setTitle({
      tabId,
      title: "TeaNode: disconnected (click to re-attach)",
    });
    broadcastCdpState(tabId, "detached");
  }
  tabs.clear();
  tabBySession.clear();
  childSessionToTab.clear();
}

function sendToRelay(payload: unknown): void {
  const ws = relayWs;
  if (!ws || ws.readyState !== WebSocket.OPEN) {
    throw new Error("Relay not connected");
  }
  ws.send(JSON.stringify(payload));
}

async function maybeOpenHelpOnce(): Promise<void> {
  try {
    const stored = await chrome.storage.local.get(["helpOnErrorShown"]);
    if (stored.helpOnErrorShown === true) return;
    await chrome.storage.local.set({ helpOnErrorShown: true });
    await chrome.runtime.openOptionsPage();
  } catch {
    // ignore
  }
}

async function onRelayMessage(text: string): Promise<void> {
  let parsed: any;
  try {
    parsed = JSON.parse(text);
  } catch {
    return;
  }

  if (parsed && parsed.method === "ping") {
    try {
      sendToRelay({ method: "pong" });
    } catch {
      /* ignore */
    }
    return;
  }

  if (
    parsed &&
    typeof parsed.id === "number" &&
    (parsed.result !== undefined || parsed.error !== undefined)
  ) {
    const p = pending.get(parsed.id);
    if (!p) return;
    pending.delete(parsed.id);
    if (parsed.error) p.reject(new Error(String(parsed.error)));
    else p.resolve(parsed.result);
    return;
  }

  if (parsed && typeof parsed.id === "number" && parsed.method === "forwardCDPCommand") {
    try {
      const result = await handleForwardCdpCommand(parsed);
      sendToRelay({ id: parsed.id, result });
    } catch (err: any) {
      sendToRelay({ id: parsed.id, error: err?.message ?? String(err) });
    }
  }
}

function getTabBySessionId(
  sessionId: string,
): { tabId: number; kind: string } | null {
  const direct = tabBySession.get(sessionId);
  if (direct) return { tabId: direct, kind: "main" };
  const child = childSessionToTab.get(sessionId);
  if (child) return { tabId: child, kind: "child" };
  return null;
}

function getTabByTargetId(targetId: string): number | null {
  for (const [tabId, tab] of tabs.entries()) {
    if (tab.targetId === targetId) return tabId;
  }
  return null;
}

async function attachTab(
  tabId: number,
  opts: { skipAttachedEvent?: boolean } = {},
): Promise<{ sessionId: string; targetId: string }> {
  const debuggee: chrome.debugger.Debuggee = { tabId };
  await chrome.debugger.attach(debuggee, "1.3");
  await chrome.debugger.sendCommand(debuggee, "Page.enable").catch(() => {});

  const info = (await chrome.debugger.sendCommand(
    debuggee,
    "Target.getTargetInfo",
  )) as any;
  const targetInfo = info?.targetInfo;
  const targetId = String(targetInfo?.targetId || "").trim();
  if (!targetId) throw new Error("Target.getTargetInfo returned no targetId");

  const sessionId = generateULID();
  const attachOrder = Date.now();

  tabs.set(tabId, { state: "connected", sessionId, targetId, attachOrder });
  tabBySession.set(sessionId, tabId);
  void chrome.action.setTitle({
    tabId,
    title: "TeaNode: attached (click to detach)",
  });

  if (!opts.skipAttachedEvent) {
    sendToRelay({
      method: "forwardCDPEvent",
      params: {
        method: "Target.attachedToTarget",
        params: {
          sessionId,
          targetInfo: { ...targetInfo, attached: true },
          waitingForDebugger: false,
        },
      },
    });
  }

  setBadge(tabId, "on");
  broadcastCdpState(tabId, "attached");
  return { sessionId, targetId };
}

async function detachTab(tabId: number, reason: string): Promise<void> {
  const tab = tabs.get(tabId);
  if (tab?.sessionId && tab?.targetId) {
    try {
      sendToRelay({
        method: "forwardCDPEvent",
        params: {
          method: "Target.detachedFromTarget",
          params: { sessionId: tab.sessionId, targetId: tab.targetId, reason },
        },
      });
    } catch {
      /* ignore */
    }
  }

  if (tab?.sessionId) tabBySession.delete(tab.sessionId);
  tabs.delete(tabId);

  for (const [childSessionId, parentTabId] of childSessionToTab.entries()) {
    if (parentTabId === tabId) childSessionToTab.delete(childSessionId);
  }

  try {
    await chrome.debugger.detach({ tabId });
  } catch {
    /* ignore */
  }

  setBadge(tabId, "off");
  void chrome.action.setTitle({
    tabId,
    title: "TeaNode (click to attach/detach)",
  });
  broadcastCdpState(tabId, "detached");
}

async function handleForwardCdpCommand(command: any): Promise<unknown> {
  const method = String(command?.params?.method || "").trim();
  const params = command?.params?.params || undefined;
  const sessionId: string | undefined =
    typeof command?.params?.sessionId === "string"
      ? command.params.sessionId
      : undefined;

  const bySession = sessionId ? getTabBySessionId(sessionId) : null;
  const targetId: string | undefined =
    typeof params?.targetId === "string" ? params.targetId : undefined;
  const tabId =
    bySession?.tabId ||
    (targetId ? getTabByTargetId(targetId) : null) ||
    (() => {
      for (const [id, tab] of tabs.entries()) {
        if (tab.state === "connected") return id;
      }
      return null;
    })();

  if (!tabId) throw new Error(`No attached tab for method ${method}`);

  const debuggee: chrome.debugger.Debuggee = { tabId };

  if (method === "Runtime.enable") {
    try {
      await chrome.debugger.sendCommand(debuggee, "Runtime.disable");
      await new Promise((r) => setTimeout(r, 50));
    } catch {
      /* ignore */
    }
    return await chrome.debugger.sendCommand(
      debuggee,
      "Runtime.enable",
      params,
    );
  }

  if (method === "Target.createTarget") {
    const url = typeof params?.url === "string" ? params.url : "about:blank";
    const tab = await chrome.tabs.create({ url, active: false });
    if (!tab.id) throw new Error("Failed to create tab");
    await new Promise((r) => setTimeout(r, 100));
    const attached = await attachTab(tab.id);
    return { targetId: attached.targetId };
  }

  if (method === "Target.closeTarget") {
    const target = typeof params?.targetId === "string" ? params.targetId : "";
    const toClose = target ? getTabByTargetId(target) : tabId;
    if (!toClose) return { success: false };
    try {
      await chrome.tabs.remove(toClose);
    } catch {
      return { success: false };
    }
    return { success: true };
  }

  if (method === "Target.activateTarget") {
    const target = typeof params?.targetId === "string" ? params.targetId : "";
    const toActivate = target ? getTabByTargetId(target) : tabId;
    if (!toActivate) return {};
    const t = await chrome.tabs.get(toActivate).catch(() => null);
    if (!t) return {};
    if (t.windowId)
      await chrome.windows
        .update(t.windowId, { focused: true })
        .catch(() => {});
    await chrome.tabs.update(toActivate, { active: true }).catch(() => {});
    return {};
  }

  const tabState = tabs.get(tabId);
  const mainSessionId = tabState?.sessionId;
  // chrome-types doesn't include sessionId on Debuggee, but Chrome supports it.
  const debuggerSession: any =
    sessionId && mainSessionId && sessionId !== mainSessionId
      ? { ...debuggee, sessionId }
      : debuggee;

  return await chrome.debugger.sendCommand(debuggerSession, method, params);
}

function onDebuggerEvent(
  source: chrome.debugger.Debuggee,
  method: string,
  params?: object,
): void {
  const tabId = source.tabId;
  if (!tabId) return;
  const tab = tabs.get(tabId);
  if (!tab?.sessionId) return;

  if (method === "Target.attachedToTarget" && (params as any)?.sessionId) {
    childSessionToTab.set(String((params as any).sessionId), tabId);
  }
  if (method === "Target.detachedFromTarget" && (params as any)?.sessionId) {
    childSessionToTab.delete(String((params as any).sessionId));
  }

  try {
    sendToRelay({
      method: "forwardCDPEvent",
      params: {
        sessionId: (source as any).sessionId || tab.sessionId,
        method,
        params,
      },
    });
  } catch {
    /* ignore */
  }
}

function onDebuggerDetach(
  source: chrome.debugger.Debuggee,
  reason: string,
): void {
  const tabId = source.tabId;
  if (!tabId) return;
  if (!tabs.has(tabId)) return;
  void detachTab(tabId, reason);
}

/** Toggle CDP relay on the active tab (original click handler). */
export async function connectOrToggleCdpForActiveTab(): Promise<void> {
  const [active] = await chrome.tabs.query({
    active: true,
    currentWindow: true,
  });
  const tabId = active?.id;
  if (!tabId) return;

  const existing = tabs.get(tabId);

  // Ignore toggle while already connecting.
  if (existing?.state === "connecting") return;

  if (existing?.state === "connected") {
    await detachTab(tabId, "toggle");
    return;
  }

  tabs.set(tabId, { state: "connecting" });
  setBadge(tabId, "connecting");
  broadcastCdpState(tabId, "connecting");
  void chrome.action.setTitle({
    tabId,
    title: "TeaNode: connecting to local relay…",
  });

  try {
    await ensureRelayConnection();
    await attachTab(tabId);
  } catch {
    tabs.delete(tabId);
    setBadge(tabId, "error");
    broadcastCdpState(tabId, "error");
    void chrome.action.setTitle({
      tabId,
      title: "TeaNode: relay not running (open options for setup)",
    });
    void maybeOpenHelpOnce();
  }
}
