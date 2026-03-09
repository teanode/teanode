/**
 * Minimal WebSocket RPC client for the extension side panel.
 * Connects to TeaNode's /api/v1/websocket with the stored token.
 */

import type {
  RpcRequestFrame,
  RpcResponseFrame,
  RpcEventFrame,
  RpcFrame,
} from "../shared/types";

type EventHandler = (event: RpcEventFrame) => void;
type ConnectionStateHandler = (connected: boolean) => void;

let ws: WebSocket | null = null;
let connectPromise: Promise<void> | null = null;
const pending = new Map<
  string,
  { resolve: (r: RpcResponseFrame) => void; reject: (e: Error) => void }
>();
const eventHandlers = new Set<EventHandler>();
const connectionStateHandlers = new Set<ConnectionStateHandler>();
let idCounter = 0;
let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
let wasConnected = false;

async function getConfig(): Promise<{ url: string; token: string }> {
  const stored = await chrome.storage.local.get(["relayUrl", "relayToken"]);
  const url =
    ((stored.relayUrl as string) || "").trim() || "http://127.0.0.1:8833";
  const token = (stored.relayToken as string) || "";
  return { url, token };
}

function httpToWs(url: string): string {
  if (url.startsWith("https://")) return "wss://" + url.slice(8);
  if (url.startsWith("http://")) return "ws://" + url.slice(7);
  return "ws://" + url;
}

export async function ensureConnected(): Promise<void> {
  if (ws && ws.readyState === WebSocket.OPEN) return;
  if (connectPromise) return connectPromise;

  connectPromise = (async () => {
    const { url, token } = await getConfig();
    const baseUrl = url.replace(/\/+$/, "");
    let wsUrl = httpToWs(baseUrl) + "/api/v1/websocket";
    if (token) wsUrl += `?token=${encodeURIComponent(token)}`;

    const socket = new WebSocket(wsUrl);
    ws = socket;

    await new Promise<void>((resolve, reject) => {
      const t = setTimeout(() => reject(new Error("WS connect timeout")), 5000);
      socket.onopen = () => {
        clearTimeout(t);
        resolve();
      };
      socket.onerror = () => {
        clearTimeout(t);
        reject(new Error("WS connect error"));
      };
      socket.onclose = (e) => {
        clearTimeout(t);
        reject(new Error(`WS closed: ${e.code}`));
      };
    });

    socket.onmessage = (event) => {
      try {
        const frame: RpcFrame = JSON.parse(event.data as string);
        if (frame.type === "res") {
          const p = pending.get(frame.id);
          if (p) {
            pending.delete(frame.id);
            p.resolve(frame);
          }
        } else if (frame.type === "event") {
          for (const handler of eventHandlers) {
            try {
              handler(frame as RpcEventFrame);
            } catch {
              /* ignore */
            }
          }
        }
      } catch {
        /* ignore parse errors */
      }
    };

    socket.onclose = () => {
      ws = null;
      for (const [, p] of pending) p.reject(new Error("WS disconnected"));
      pending.clear();
      if (wasConnected) {
        wasConnected = false;
        for (const handler of connectionStateHandlers) {
          try {
            handler(false);
          } catch {
            /* ignore */
          }
        }
      }
      scheduleReconnect();
    };
    socket.onerror = () => {
      /* onclose will fire */
    };

    wasConnected = true;
    for (const handler of connectionStateHandlers) {
      try {
        handler(true);
      } catch {
        /* ignore */
      }
    }
  })();

  try {
    await connectPromise;
  } finally {
    connectPromise = null;
  }
}

function scheduleReconnect(): void {
  if (reconnectTimer) return;
  reconnectTimer = setTimeout(() => {
    reconnectTimer = null;
    ensureConnected().catch(() => scheduleReconnect());
  }, 3000);
}

export async function sendRpc(
  method: string,
  params?: unknown,
): Promise<unknown> {
  await ensureConnected();
  const id = `ext-${++idCounter}`;
  const frame: RpcRequestFrame = { type: "req", id, method, params };

  return new Promise((resolve, reject) => {
    pending.set(id, {
      resolve: (response: RpcResponseFrame) => {
        if (response.ok) resolve(response.payload);
        else reject(new Error(response.error?.message || "RPC error"));
      },
      reject,
    });
    const timeout = setTimeout(() => {
      if (pending.has(id)) {
        pending.delete(id);
        reject(new Error("RPC timeout"));
      }
    }, 60000);
    pending.set(id, {
      resolve: (response: RpcResponseFrame) => {
        clearTimeout(timeout);
        if (response.ok) resolve(response.payload);
        else reject(new Error(response.error?.message || "RPC error"));
      },
      reject: (err: Error) => {
        clearTimeout(timeout);
        reject(err);
      },
    });
    ws!.send(JSON.stringify(frame));
  });
}

export function onEvent(handler: EventHandler): () => void {
  eventHandlers.add(handler);
  return () => {
    eventHandlers.delete(handler);
  };
}

export function onConnectionStateChange(
  handler: ConnectionStateHandler,
): () => void {
  connectionStateHandlers.add(handler);
  return () => {
    connectionStateHandlers.delete(handler);
  };
}

export async function getBaseUrl(): Promise<{ url: string; token: string }> {
  return getConfig();
}

export function disconnect(): void {
  if (reconnectTimer) {
    clearTimeout(reconnectTimer);
    reconnectTimer = null;
  }
  if (ws) {
    ws.close();
    ws = null;
  }
}
