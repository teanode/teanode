import { RequestFrame, ResponseFrame, EventFrame, RPCError } from './types';

type EventHandler = (frame: EventFrame) => void;

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

function getToken(): string {
  const params = new URLSearchParams(window.location.search);
  return params.get('token') || '';
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

  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  let url = `${proto}//${window.location.host}/ws`;
  const token = getToken();
  if (token) url += `?token=${encodeURIComponent(token)}`;

  webSocket = new WebSocket(url);

  webSocket.onopen = () => {
    setStatus('connected');
    onOpen?.();
  };

  webSocket.onclose = () => {
    setStatus('disconnected - reconnecting...');
    // Reject all pending RPCs
    for (const [id, pending] of pendingCalls) {
      pending.reject({ code: -1, message: 'disconnected' });
      pendingCalls.delete(id);
    }
    reconnectTimer = setTimeout(() => connect(onOpen), 2000);
  };

  webSocket.onerror = () => {};

  webSocket.onmessage = (e) => {
    const frame = JSON.parse(e.data as string);
    if (frame.type === 'res') {
      const response = frame as ResponseFrame;
      const pending = pendingCalls.get(response.id);
      if (pending) {
        pendingCalls.delete(response.id);
        if (response.ok) {
          pending.resolve(response.payload);
        } else {
          pending.reject(response.error || { code: -1, message: 'unknown error' });
        }
      }
    } else if (frame.type === 'event') {
      eventHandler?.(frame as EventFrame);
    }
  };
}

export function sendRpc<T = unknown>(method: string, params: unknown): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    if (!webSocket || webSocket.readyState !== WebSocket.OPEN) {
      reject({ code: -1, message: 'not connected' });
      return;
    }
    const id = String(++callId);
    pendingCalls.set(id, {
      resolve: resolve as (payload: unknown) => void,
      reject,
    });
    const frame: RequestFrame = { type: 'req', id, method, params };
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
