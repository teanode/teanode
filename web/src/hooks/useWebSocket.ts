import { useEffect, useRef, useCallback } from "react";
import {
  connect,
  disconnect,
  sendRpc,
  setEventHandler,
  setStatusHandler,
} from "../rpc";
import type { EventFrame, ConnectResult } from "../types";

interface UseWebSocketOptions {
  onEvent: (frame: EventFrame) => void;
  onConnect: (result: ConnectResult) => void;
  onStatusChange: (status: string) => void;
}

export function useWebSocket({
  onEvent,
  onConnect,
  onStatusChange,
}: UseWebSocketOptions) {
  const onEventRef = useRef(onEvent);
  const onConnectRef = useRef(onConnect);
  const onStatusRef = useRef(onStatusChange);

  onEventRef.current = onEvent;
  onConnectRef.current = onConnect;
  onStatusRef.current = onStatusChange;

  useEffect(() => {
    setEventHandler((frame) => onEventRef.current(frame));
    setStatusHandler((status) => onStatusRef.current(status));

    connect(() => {
      sendRpc<ConnectResult>("connect", {})
        .then((response) => onConnectRef.current(response))
        .catch(() => {});
    });

    return () => {
      disconnect();
    };
  }, []);

  const rpc = useCallback(
    <T = unknown>(method: string, params: unknown): Promise<T> => {
      return sendRpc<T>(method, params);
    },
    [],
  );

  return { sendRpc: rpc };
}
