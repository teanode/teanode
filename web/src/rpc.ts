import {
  RequestFrame,
  ResponseFrame,
  EventFrame,
  RPCError,
  AuthStatusResult,
} from "./types";

type EventHandler = (frame: EventFrame) => void;
type BinaryHandler = (data: ArrayBuffer) => void;

interface PendingCall {
  resolve: (payload: unknown) => void;
  reject: (error: RPCError) => void;
}

let webSocket: WebSocket | null = null;
let callId = 0;
const pendingCalls: Map<string, PendingCall> = new Map();
let eventHandler: EventHandler | null = null;
let onStatusChange: ((status: string) => void) | null = null;
let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
const binaryHandlers: BinaryHandler[] = [];

function getToken(): string {
  const params = new URLSearchParams(window.location.search);
  return params.get("token") || "";
}

export function setEventHandler(handler: EventHandler): void {
  eventHandler = handler;
}

export function setStatusHandler(handler: (status: string) => void): void {
  onStatusChange = handler;
}

function setStatus(status: string): void {
  onStatusChange?.(status);
}

export function connect(onOpen?: () => void): void {
  if (reconnectTimer) {
    clearTimeout(reconnectTimer);
    reconnectTimer = null;
  }

  const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
  let url = `${proto}//${window.location.host}/api/v1/websocket`;
  const token = getToken();
  if (token) url += `?token=${encodeURIComponent(token)}`;

  webSocket = new WebSocket(url);

  webSocket.onopen = () => {
    setStatus("connected");
    onOpen?.();
  };

  webSocket.onclose = () => {
    setStatus("disconnected - reconnecting...");
    // Reject all pending RPCs
    for (const [id, pending] of pendingCalls) {
      pending.reject({ code: -1, message: "disconnected" });
      pendingCalls.delete(id);
    }
    reconnectTimer = setTimeout(() => connect(onOpen), 2000);
  };

  webSocket.onerror = () => {};

  webSocket.onmessage = async (e) => {
    if (e.data instanceof ArrayBuffer) {
      for (const handler of binaryHandlers) handler(e.data);
      return;
    }
    if (e.data instanceof Blob) {
      const data = await e.data.arrayBuffer();
      for (const handler of binaryHandlers) handler(data);
      return;
    }

    const frame = JSON.parse(e.data as string);
    if (frame.type === "res") {
      const response = frame as ResponseFrame;
      const pending = pendingCalls.get(response.id);
      if (pending) {
        pendingCalls.delete(response.id);
        if (response.ok) {
          pending.resolve(response.payload);
        } else {
          pending.reject(
            response.error || { code: -1, message: "unknown error" },
          );
        }
      }
    } else if (frame.type === "event") {
      eventHandler?.(frame as EventFrame);
    }
  };
}

export function sendRpc<T = unknown>(
  method: string,
  params: unknown,
): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    if (!webSocket || webSocket.readyState !== WebSocket.OPEN) {
      reject({ code: -1, message: "not connected" });
      return;
    }
    const id = String(++callId);
    pendingCalls.set(id, {
      resolve: resolve as (payload: unknown) => void,
      reject,
    });
    const frame: RequestFrame = { type: "req", id, method, params };
    webSocket.send(JSON.stringify(frame));
  });
}

export function disconnect(): void {
  if (reconnectTimer) {
    clearTimeout(reconnectTimer);
    reconnectTimer = null;
  }
  if (webSocket) {
    webSocket.onclose = null;
    webSocket.close();
    webSocket = null;
  }
}

export function sendBinary(data: ArrayBuffer | Uint8Array): void {
  if (!webSocket || webSocket.readyState !== WebSocket.OPEN) {
    return;
  }
  if (data instanceof Uint8Array) {
    webSocket.send(data.buffer.slice(data.byteOffset, data.byteOffset + data.byteLength));
    return;
  }
  webSocket.send(data);
}

export function onBinaryMessage(handler: BinaryHandler): () => void {
  binaryHandlers.push(handler);
  return () => {
    const idx = binaryHandlers.indexOf(handler);
    if (idx >= 0) binaryHandlers.splice(idx, 1);
  };
}

// --- REST auth helpers (work before WebSocket is established) ---

export async function authStatus(): Promise<AuthStatusResult> {
  const response = await fetch("/api/v1/auth/status");
  if (!response.ok) throw new Error(`auth/status: ${response.status}`);
  return response.json();
}

export async function authLogin(password: string): Promise<void> {
  const response = await fetch("/api/v1/auth/login", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ password }),
  });
  if (!response.ok) {
    const data = await response
      .json()
      .catch(() => ({ error: { message: "Login failed" } }));
    throw new Error(data.error?.message || "Login failed");
  }
}

export async function authSetup(password: string): Promise<void> {
  const response = await fetch("/api/v1/auth/setup", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ password }),
  });
  if (!response.ok) {
    const data = await response
      .json()
      .catch(() => ({ error: { message: "Setup failed" } }));
    throw new Error(data.error?.message || "Setup failed");
  }
}

export async function authLogout(): Promise<void> {
  await fetch("/api/v1/auth/logout", { method: "POST" });
}
