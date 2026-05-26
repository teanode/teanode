import {
  RequestFrame,
  ResponseFrame,
  EventFrame,
  RPCError,
  AuthStatusResult,
  Profile,
} from "./types";

type EventHandler = (frame: EventFrame) => void;
type BinaryHandler = (data: ArrayBuffer) => void;
type VoiceMessageHandler = (message: Record<string, unknown>) => void;

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
let shouldReconnect = false;
let connectOpenHandler: (() => void) | undefined;
const binaryHandlers: BinaryHandler[] = [];
const voiceMessageHandlers: VoiceMessageHandler[] = [];
const RECONNECT_DELAY_MS = 1200;
const WAIT_FOR_CONNECTION_MS = 5000;

// Waiters resolved when the WebSocket reaches the OPEN state.
let connectionWaiters: Array<{
  resolve: () => void;
  reject: (error: RPCError) => void;
}> = [];

function resolveConnectionWaiters(): void {
  const waiters = connectionWaiters;
  connectionWaiters = [];
  for (const waiter of waiters) waiter.resolve();
}

function rejectConnectionWaiters(message: string): void {
  const waiters = connectionWaiters;
  connectionWaiters = [];
  for (const waiter of waiters) waiter.reject({ code: -1, message });
}

function getToken(): string {
  const params = new URLSearchParams(window.location.search);
  return params.get("token") || "";
}

export function withToken(path: string): string {
  const token = getToken();
  if (!token) return path;
  if (/[?&]token=/.test(path)) return path;
  const separator = path.includes("?") ? "&" : "?";
  return `${path}${separator}token=${encodeURIComponent(token)}`;
}

function buildAuthHeaders(init?: HeadersInit): Headers {
  const headers = new Headers(init);
  const token = getToken();
  if (token) headers.set("Authorization", `Bearer ${token}`);
  return headers;
}

export async function apiFetch(
  input: string,
  init?: RequestInit,
): Promise<Response> {
  return fetch(withToken(input), {
    credentials: "same-origin",
    ...init,
    headers: buildAuthHeaders(init?.headers),
  });
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

function clearReconnectTimer(): void {
  if (reconnectTimer) {
    clearTimeout(reconnectTimer);
    reconnectTimer = null;
  }
}

function rejectPendingCalls(message: string): void {
  for (const [id, pending] of pendingCalls) {
    pending.reject({ code: -1, message });
    pendingCalls.delete(id);
  }
}

function canAttemptConnectNow(): boolean {
  // iOS often suspends sockets while backgrounded; wait for visibility/focus.
  if (
    typeof document !== "undefined" &&
    document.visibilityState === "hidden"
  ) {
    setStatus("disconnected - waiting for app focus...");
    return false;
  }
  if (typeof navigator !== "undefined" && navigator.onLine === false) {
    setStatus("offline - waiting for network...");
    return false;
  }
  return true;
}

function scheduleReconnect(): void {
  if (!shouldReconnect || reconnectTimer) return;
  if (!canAttemptConnectNow()) return;

  setStatus("disconnected - reconnecting...");
  reconnectTimer = setTimeout(() => {
    reconnectTimer = null;
    openSocket();
  }, RECONNECT_DELAY_MS);
}

function openSocket(): void {
  if (!shouldReconnect) return;
  if (!canAttemptConnectNow()) return;
  if (
    webSocket &&
    (webSocket.readyState === WebSocket.OPEN ||
      webSocket.readyState === WebSocket.CONNECTING)
  ) {
    return;
  }

  clearReconnectTimer();
  setStatus("connecting...");

  const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
  let url = `${proto}//${window.location.host}/api/websocket`;
  const token = getToken();
  if (token) url += `?token=${encodeURIComponent(token)}`;

  const socket = new WebSocket(url);
  webSocket = socket;

  socket.onopen = () => {
    console.debug("[voice][ws] open");
    setStatus("connected");
    resolveConnectionWaiters();
    connectOpenHandler?.();
  };

  socket.onclose = () => {
    console.debug("[voice][ws] close");
    rejectPendingCalls("disconnected");
    if (webSocket === socket) webSocket = null;
    scheduleReconnect();
  };

  socket.onerror = () => {
    if (socket.readyState !== WebSocket.OPEN) {
      setStatus("connection error - reconnecting...");
    }
  };

  socket.onmessage = async (e) => {
    if (e.data instanceof ArrayBuffer) {
      console.debug("[voice][ws] binary message", { bytes: e.data.byteLength });
      for (const handler of binaryHandlers) handler(e.data);
      return;
    }
    if (e.data instanceof Blob) {
      const data = await e.data.arrayBuffer();
      console.debug("[voice][ws] binary blob message", {
        bytes: data.byteLength,
      });
      for (const handler of binaryHandlers) handler(data);
      return;
    }

    const frame = JSON.parse(e.data as string);
    console.debug("[voice][ws] text message", {
      type: frame.type,
      id: frame.id,
      event: frame.event,
    });
    if (
      typeof frame === "object" &&
      frame !== null &&
      frame.v === 1 &&
      typeof frame.type === "string"
    ) {
      for (const handler of voiceMessageHandlers) {
        handler(frame as Record<string, unknown>);
      }
      return;
    }

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

export function connect(onOpen?: () => void): void {
  shouldReconnect = true;
  connectOpenHandler = onOpen;
  openSocket();
}

export function reconnect(): void {
  if (!shouldReconnect) return;
  openSocket();
}

function sendRpcImmediate<T = unknown>(
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
    const frame: RequestFrame = {
      type: "request",
      id,
      method,
      parameters: params,
    };
    console.debug("[voice][ws] send rpc", { method, id });
    webSocket.send(JSON.stringify(frame));
  });
}

export function sendRpc<T = unknown>(
  method: string,
  params: unknown,
): Promise<T> {
  // Fast path: socket is already open.
  if (webSocket && webSocket.readyState === WebSocket.OPEN) {
    return sendRpcImmediate<T>(method, params);
  }

  // If we're reconnecting (or about to), wait for the socket to open rather
  // than failing immediately.  This avoids silent failures when the user
  // interacts right after switching back to the tab.
  if (shouldReconnect) {
    return new Promise<T>((resolve, reject) => {
      const timeout = setTimeout(() => {
        // Remove this waiter so it doesn't fire twice.
        connectionWaiters = connectionWaiters.filter(
          (w) => w.resolve !== onConnected,
        );
        reject({ code: -1, message: "not connected" });
      }, WAIT_FOR_CONNECTION_MS);

      const onConnected = () => {
        clearTimeout(timeout);
        sendRpcImmediate<T>(method, params).then(resolve, reject);
      };

      connectionWaiters.push({
        resolve: onConnected,
        reject: (error) => {
          clearTimeout(timeout);
          reject(error);
        },
      });
    });
  }

  return Promise.reject({ code: -1, message: "not connected" });
}

export function disconnect(): void {
  shouldReconnect = false;
  clearReconnectTimer();
  if (webSocket) {
    webSocket.onclose = null;
    webSocket.close();
    webSocket = null;
  }
  rejectPendingCalls("disconnected");
  rejectConnectionWaiters("disconnected");
}

export function sendBinary(data: ArrayBuffer | Uint8Array): void {
  if (!webSocket || webSocket.readyState !== WebSocket.OPEN) {
    console.debug("[voice][ws] drop binary send: socket not open");
    return;
  }
  if (data instanceof Uint8Array) {
    console.debug("[voice][ws] send binary", { bytes: data.byteLength });
    webSocket.send(
      data.buffer.slice(data.byteOffset, data.byteOffset + data.byteLength),
    );
    return;
  }
  console.debug("[voice][ws] send binary", { bytes: data.byteLength });
  webSocket.send(data);
}

export function onBinaryMessage(handler: BinaryHandler): () => void {
  binaryHandlers.push(handler);
  return () => {
    const idx = binaryHandlers.indexOf(handler);
    if (idx >= 0) binaryHandlers.splice(idx, 1);
  };
}

export function onVoiceMessage(handler: VoiceMessageHandler): () => void {
  voiceMessageHandlers.push(handler);
  return () => {
    const idx = voiceMessageHandlers.indexOf(handler);
    if (idx >= 0) voiceMessageHandlers.splice(idx, 1);
  };
}

// --- REST auth helpers (work before WebSocket is established) ---

export async function authStatus(): Promise<AuthStatusResult> {
  const response = await apiFetch("/api/auth/status");
  if (!response.ok) throw new Error(`auth/status: ${response.status}`);
  return response.json();
}

export async function authLogin(
  username: string,
  password: string,
): Promise<void> {
  const response = await apiFetch("/api/auth/login", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ username, password }),
  });
  if (!response.ok) {
    const data = await response
      .json()
      .catch(() => ({ error: { message: "Login failed" } }));
    throw new Error(data.error?.message || "Login failed");
  }
}

export async function authSetup(
  username: string,
  password: string,
  name?: string,
): Promise<void> {
  const response = await apiFetch("/api/auth/setup", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ username, password, name }),
  });
  if (!response.ok) {
    const data = await response
      .json()
      .catch(() => ({ error: { message: "Setup failed" } }));
    throw new Error(data.error?.message || "Setup failed");
  }
}

export async function authLogout(): Promise<void> {
  await apiFetch("/api/auth/logout", { method: "POST" });
}

interface RpcProfile {
  name: string;
  description?: string;
  avatarMediaId?: string;
}

function fromRpcProfile(profile: RpcProfile): Profile {
  return {
    name: profile.name || "",
    description: profile.description || "",
    avatarMediaId: profile.avatarMediaId || "",
  };
}

export async function profileGetRpc(): Promise<Profile> {
  const response = await sendRpc<RpcProfile>("profile.get", {});
  return fromRpcProfile(response);
}

export async function profileUpdateRpc(
  profile: Partial<Profile>,
): Promise<Profile> {
  const response = await sendRpc<RpcProfile>("profile.update", {
    ...(profile.name !== undefined ? { name: profile.name } : {}),
    ...(profile.description !== undefined
      ? { description: profile.description }
      : {}),
    ...(profile.avatarMediaId !== undefined
      ? { avatarMediaId: profile.avatarMediaId }
      : {}),
  });
  return fromRpcProfile(response);
}

export async function uploadAgentAvatar(
  agentId: string,
  file: File,
): Promise<void> {
  const formData = new FormData();
  formData.append("file", file);
  const response = await apiFetch("/api/media/upload", {
    method: "POST",
    body: formData,
  });
  if (!response.ok) throw new Error(await response.text());
  const uploaded = (await response.json()) as { mediaId?: string };
  if (!uploaded.mediaId) {
    throw new Error("Upload failed: missing mediaId");
  }
  await sendRpc("agents.avatar.set", {
    id: agentId,
    avatarMediaId: uploaded.mediaId,
  });
}

export async function removeAgentAvatar(agentId: string): Promise<void> {
  await sendRpc("agents.avatar.remove", { id: agentId });
}

export async function uploadProfileAvatar(file: File): Promise<Profile> {
  const formData = new FormData();
  formData.append("file", file);
  const response = await apiFetch("/api/media/upload", {
    method: "POST",
    body: formData,
  });
  if (!response.ok) throw new Error(await response.text());
  const uploaded = (await response.json()) as { mediaId?: string };
  if (!uploaded.mediaId) {
    throw new Error("Upload failed: missing mediaId");
  }
  return profileUpdateRpc({ avatarMediaId: uploaded.mediaId });
}

export async function removeProfileAvatar(): Promise<Profile> {
  return profileUpdateRpc({ avatarMediaId: "" });
}

export async function removeProfileAvatarRpc(): Promise<Profile> {
  const response = await sendRpc<RpcProfile>("profile.avatar.remove", {});
  return fromRpcProfile(response);
}

// Backward-compatible aliases.
export const getProfile = profileGetRpc;
export const updateProfile = profileUpdateRpc;
