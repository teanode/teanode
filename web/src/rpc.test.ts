import { afterEach, describe, expect, it, vi } from "vitest";
import { connect, disconnect, onVoiceMessage, setEventHandler } from "./rpc";

class MockWebSocket {
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
});
