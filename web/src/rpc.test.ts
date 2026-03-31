import { afterEach, describe, expect, it, vi } from "vitest";
import {
  connect,
  disconnect,
  onVoiceMessage,
  reconnect,
  sendRpc,
  setEventHandler,
  setStatusHandler,
} from "./rpc";

class MockWebSocket {
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSED = 3;
  static instances: MockWebSocket[] = [];

  readonly url: string;
  readyState = MockWebSocket.OPEN;
  onopen: ((event: Event) => void) | null = null;
  onclose: ((event: CloseEvent) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;
  onmessage:
    | ((
        event: MessageEvent<string | ArrayBuffer | Blob>,
      ) => void | Promise<void>)
    | null = null;

  constructor(url: string) {
    this.url = url;
    MockWebSocket.instances.push(this);
  }

  send(_data: string | ArrayBuffer | Blob): void {}

  close(): void {
    this.readyState = MockWebSocket.CLOSED;
  }
}

describe("rpc voice message routing", () => {
  afterEach(() => {
    disconnect();
    MockWebSocket.instances = [];
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it("routes backend voice envelopes to voice handlers only", async () => {
    vi.stubGlobal("window", {
      location: {
        protocol: "http:",
        host: "localhost:8833",
        search: "",
      },
    });
    vi.stubGlobal("WebSocket", MockWebSocket as unknown as typeof WebSocket);
    const eventHandler = vi.fn();
    const voiceHandler = vi.fn();
    const offVoice = onVoiceMessage(voiceHandler);
    setEventHandler(eventHandler);

    connect();

    expect(MockWebSocket.instances).toHaveLength(1);
    const socket = MockWebSocket.instances[0];
    expect(socket.onmessage).toBeTruthy();

    await socket.onmessage?.(
      new MessageEvent("message", {
        data: JSON.stringify({
          v: 1,
          type: "turn.event",
          payload: { event: "turn_committed", turnId: "t1" },
        }),
      }),
    );

    expect(voiceHandler).toHaveBeenCalledTimes(1);
    expect(voiceHandler.mock.calls[0]?.[0]).toMatchObject({
      v: 1,
      type: "turn.event",
    });
    expect(eventHandler).not.toHaveBeenCalled();

    offVoice();
  });

  it("waits to reconnect while page is hidden and reconnects on visibility", () => {
    vi.stubGlobal("window", {
      location: {
        protocol: "http:",
        host: "localhost:8833",
        search: "",
      },
    });
    vi.stubGlobal("document", {} as Document);
    vi.stubGlobal("WebSocket", MockWebSocket as unknown as typeof WebSocket);
    const statusHandler = vi.fn();
    setStatusHandler(statusHandler);

    Object.defineProperty(document, "visibilityState", {
      configurable: true,
      get: () => "hidden",
    });
    connect();
    expect(MockWebSocket.instances).toHaveLength(0);
    expect(statusHandler).toHaveBeenLastCalledWith(
      "disconnected - waiting for app focus...",
    );

    Object.defineProperty(document, "visibilityState", {
      configurable: true,
      get: () => "visible",
    });
    reconnect();
    expect(MockWebSocket.instances).toHaveLength(1);
  });

  it("does not open duplicate sockets while already connected", () => {
    vi.stubGlobal("window", {
      location: {
        protocol: "http:",
        host: "localhost:8833",
        search: "",
      },
    });
    vi.stubGlobal("WebSocket", MockWebSocket as unknown as typeof WebSocket);

    connect();
    connect();

    expect(MockWebSocket.instances).toHaveLength(1);
  });
});

describe("sendRpc waits for reconnection", () => {
  afterEach(() => {
    disconnect();
    MockWebSocket.instances = [];
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it("queues RPC until socket opens when reconnecting", async () => {
    vi.useFakeTimers();
    vi.stubGlobal("window", {
      location: {
        protocol: "http:",
        host: "localhost:8833",
        search: "",
      },
    });
    vi.stubGlobal("document", { visibilityState: "visible" } as Document);
    vi.stubGlobal("WebSocket", MockWebSocket as unknown as typeof WebSocket);

    // Start connected, then simulate a disconnect + reconnect cycle.
    connect();
    const socket1 = MockWebSocket.instances[0];
    socket1.onopen?.(new Event("open"));

    // Simulate tab-background disconnect: socket closes.
    socket1.readyState = MockWebSocket.CLOSED;
    socket1.onclose?.({} as CloseEvent);

    // Advance past reconnect delay so the new socket is created.
    vi.advanceTimersByTime(1200);
    const socket2 = MockWebSocket.instances[1];
    expect(socket2).toBeDefined();
    socket2.readyState = MockWebSocket.CONNECTING;

    // Send an RPC while socket is CONNECTING — it should wait, not reject.
    const sendSpy = vi.spyOn(socket2, "send");
    const rpcPromise = sendRpc("approvals.resolve", { test: true });

    // RPC hasn't been sent yet (socket not open).
    expect(sendSpy).not.toHaveBeenCalled();

    // Socket opens — queued RPC should now be sent.
    socket2.readyState = MockWebSocket.OPEN;
    socket2.onopen?.(new Event("open"));

    expect(sendSpy).toHaveBeenCalledTimes(1);
    const sent = JSON.parse(sendSpy.mock.calls[0][0] as string);
    expect(sent.method).toBe("approvals.resolve");

    // Simulate server response so the promise resolves.
    await socket2.onmessage?.(
      new MessageEvent("message", {
        data: JSON.stringify({
          type: "res",
          id: sent.id,
          ok: true,
          payload: { success: true },
        }),
      }),
    );

    await expect(rpcPromise).resolves.toEqual({ success: true });
    vi.useRealTimers();
  });

  it("rejects immediately when not reconnecting", async () => {
    vi.stubGlobal("window", {
      location: {
        protocol: "http:",
        host: "localhost:8833",
        search: "",
      },
    });
    vi.stubGlobal("WebSocket", MockWebSocket as unknown as typeof WebSocket);

    // Never called connect(), so shouldReconnect is false.
    await expect(sendRpc("test.method", {})).rejects.toMatchObject({
      message: "not connected",
    });
  });

  it("queues questions.answer RPC during reconnect", async () => {
    vi.useFakeTimers();
    vi.stubGlobal("window", {
      location: {
        protocol: "http:",
        host: "localhost:8833",
        search: "",
      },
    });
    vi.stubGlobal("document", { visibilityState: "visible" } as Document);
    vi.stubGlobal("WebSocket", MockWebSocket as unknown as typeof WebSocket);

    connect();
    const socket1 = MockWebSocket.instances[0];
    socket1.onopen?.(new Event("open"));

    // Socket closes mid-session (e.g. iOS background).
    socket1.readyState = MockWebSocket.CLOSED;
    socket1.onclose?.({} as CloseEvent);

    // User submits an answer while disconnected — RPC should queue.
    const rpcPromise = sendRpc("questions.answer", {
      answers: [{ questionId: "q1", answer: "Yes" }],
    });

    // Advance past reconnect delay so the new socket is created.
    vi.advanceTimersByTime(1200);
    const socket2 = MockWebSocket.instances[1];
    expect(socket2).toBeDefined();

    const sendSpy = vi.spyOn(socket2, "send");
    socket2.readyState = MockWebSocket.OPEN;
    socket2.onopen?.(new Event("open"));

    expect(sendSpy).toHaveBeenCalledTimes(1);
    const sent = JSON.parse(sendSpy.mock.calls[0][0] as string);
    expect(sent.method).toBe("questions.answer");

    await socket2.onmessage?.(
      new MessageEvent("message", {
        data: JSON.stringify({
          type: "res",
          id: sent.id,
          ok: true,
          payload: {},
        }),
      }),
    );

    await expect(rpcPromise).resolves.toEqual({});
    vi.useRealTimers();
  });

  it("rejects queued RPCs on disconnect", async () => {
    vi.stubGlobal("window", {
      location: {
        protocol: "http:",
        host: "localhost:8833",
        search: "",
      },
    });
    vi.stubGlobal("document", { visibilityState: "visible" } as Document);
    vi.stubGlobal("WebSocket", MockWebSocket as unknown as typeof WebSocket);

    connect();
    const socket1 = MockWebSocket.instances[0];
    socket1.onopen?.(new Event("open"));

    // Simulate disconnect.
    socket1.readyState = MockWebSocket.CLOSED;
    socket1.onclose?.({} as CloseEvent);

    // Queue an RPC while reconnecting.
    const rpcPromise = sendRpc("test.method", {});

    // Explicit disconnect should reject queued RPCs.
    disconnect();

    await expect(rpcPromise).rejects.toMatchObject({
      message: "disconnected",
    });
  });
});
