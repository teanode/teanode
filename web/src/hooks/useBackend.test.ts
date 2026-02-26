/**
 * Tests for the streaming message merge logic in useBackend.
 *
 * These tests simulate the state mutations that occur in the handleEvent
 * callback when processing streaming conversation events.  Rather than
 * rendering the React hook, we reproduce the core array-manipulation
 * algorithm (the setMessages updater functions) so we can verify
 * correctness of the merge at each stage of a streaming run.
 */
import { describe, it, expect, beforeEach } from "vitest";
import { reconcileRunStateFromHistory } from "./useBackend";

// ─── Types (subset of src/types.ts needed for the test) ────────────

interface DisplayMessage {
  id: string;
  type: "user" | "assistant" | "tool-invoke" | "tool-result" | "usage";
  content: string;
  toolName?: string;
  runnerId?: string;
  timestamp?: number;
}

// ─── Helpers (mirrored from useBackend.ts) ──────────────────────────

let messageIdCounter = 0;
function nextMessageId(): string {
  return `msg-${++messageIdCounter}`;
}

function findRunAssistantIndex(
  messages: DisplayMessage[],
  runnerId: string | null,
): number {
  if (!runnerId) return messages.length - 1;
  for (let index = messages.length - 1; index >= 0; index--) {
    if (
      messages[index].type === "assistant" &&
      messages[index].runnerId === runnerId
    ) {
      return index;
    }
  }
  return messages.length - 1;
}

// ─── Streaming simulation ───────────────────────────────────────────
//
// The following class wraps the mutable state that useBackend tracks via
// refs (streamTextRef, afterToolCallsRef) and the messages array (React
// state).  Each method corresponds to one event-type branch in handleEvent.

class StreamingSimulation {
  messages: DisplayMessage[] = [];
  streamText = "";
  afterToolCalls = false;
  isStreaming = false;

  /** Start a new run with user message + empty assistant placeholder. */
  startRun(runnerId: string, userText: string) {
    this.messages.push(
      {
        id: nextMessageId(),
        type: "user",
        content: userText,
        timestamp: Date.now(),
      },
      { id: nextMessageId(), type: "assistant", content: "", runnerId },
    );
    this.streamText = "";
    this.afterToolCalls = false;
    this.isStreaming = false;
  }

  /** Process a delta event (streaming text chunk). */
  delta(runnerId: string, text: string) {
    if (this.afterToolCalls) {
      const previousText = this.streamText;
      if (previousText) {
        const updated = [...this.messages];
        const assistantIndex = findRunAssistantIndex(updated, runnerId);
        if (
          assistantIndex >= 0 &&
          updated[assistantIndex].type === "assistant"
        ) {
          updated[assistantIndex] = {
            ...updated[assistantIndex],
            content: previousText,
          };
        }
        const newAssistant: DisplayMessage = {
          id: nextMessageId(),
          type: "assistant",
          content: "",
          runnerId,
        };
        updated.splice(assistantIndex + 1, 0, newAssistant);
        this.messages = updated;
      } else {
        const updated = [...this.messages];
        const assistantIndex = findRunAssistantIndex(updated, runnerId);
        if (
          assistantIndex >= 0 &&
          updated[assistantIndex].type === "assistant" &&
          !updated[assistantIndex].content
        ) {
          // Reuse empty placeholder
        } else {
          const newAssistant: DisplayMessage = {
            id: nextMessageId(),
            type: "assistant",
            content: "",
            runnerId,
          };
          updated.splice(assistantIndex + 1, 0, newAssistant);
        }
        this.messages = updated;
      }
      this.streamText = "";
      this.afterToolCalls = false;
    }
    this.streamText += text;
    this.isStreaming = true;
  }

  /** Process a tool_call event. */
  toolCall(runnerId: string, toolName: string, args: string) {
    this.afterToolCalls = true;
    const accumulatedText = this.streamText;
    this.streamText = "";
    const updated = [...this.messages];
    const assistantIndex = findRunAssistantIndex(updated, runnerId);
    const toolMessage: DisplayMessage = {
      id: nextMessageId(),
      type: "tool-invoke",
      content: args,
      toolName,
      timestamp: Date.now(),
    };
    if (
      accumulatedText &&
      assistantIndex >= 0 &&
      updated[assistantIndex].type === "assistant"
    ) {
      updated[assistantIndex] = {
        ...updated[assistantIndex],
        content: accumulatedText,
      };
      updated.splice(assistantIndex + 1, 0, toolMessage);
      const newTail: DisplayMessage = {
        id: nextMessageId(),
        type: "assistant",
        content: "",
        runnerId,
      };
      updated.splice(assistantIndex + 2, 0, newTail);
    } else {
      updated.splice(assistantIndex, 0, toolMessage);
    }
    this.messages = updated;
    this.isStreaming = false;
  }

  /** Process a tool_result event. */
  toolResult(runnerId: string, toolName: string, result: string) {
    const updated = [...this.messages];
    const assistantIndex = findRunAssistantIndex(updated, runnerId);
    const toolMessage: DisplayMessage = {
      id: nextMessageId(),
      type: "tool-result",
      content: result,
      toolName,
      timestamp: Date.now(),
    };
    updated.splice(assistantIndex, 0, toolMessage);
    this.messages = updated;
  }

  /** Process a final event. */
  final(runnerId: string, serverText?: string) {
    const updated = [...this.messages];
    const assistantIndex = findRunAssistantIndex(updated, runnerId);
    const hasToolSplits = updated.some(
      (message, index) =>
        index !== assistantIndex &&
        message.type === "assistant" &&
        message.runnerId === runnerId,
    );
    const finalText = hasToolSplits
      ? this.streamText
      : serverText || this.streamText;
    if (assistantIndex >= 0 && updated[assistantIndex].type === "assistant") {
      if (finalText) {
        updated[assistantIndex] = {
          ...updated[assistantIndex],
          content: finalText,
          timestamp: Date.now(),
        };
      } else if (updated[assistantIndex].content) {
        // Assistant already has committed content — preserve it.
        updated[assistantIndex] = {
          ...updated[assistantIndex],
          timestamp: Date.now(),
        };
      } else {
        updated.splice(assistantIndex, 1);
      }
    }
    this.messages = updated;
    this.streamText = "";
    this.afterToolCalls = false;
    this.isStreaming = false;
  }

  /** Resolve the display text for each assistant message (what the UI would render). */
  renderedAssistantTexts(activeRunnerId: string): string[] {
    // Determine the last streaming assistant ID (mirrors MessageList logic).
    let lastStreamingAssistantId: string | null = null;
    if (this.isStreaming) {
      for (let index = this.messages.length - 1; index >= 0; index--) {
        if (
          this.messages[index].type === "assistant" &&
          this.messages[index].runnerId === activeRunnerId
        ) {
          lastStreamingAssistantId = this.messages[index].id;
          break;
        }
      }
    }

    return this.messages
      .filter((message) => message.type === "assistant")
      .map((message) => {
        const isStreamingMessage = message.id === lastStreamingAssistantId;
        if (isStreamingMessage) {
          return this.streamText || message.content;
        }
        return message.content;
      });
  }
}

// ─── Tests ──────────────────────────────────────────────────────────

describe("streaming message merge", () => {
  let simulation: StreamingSimulation;
  const runnerId = "run-1";

  beforeEach(() => {
    messageIdCounter = 0;
    simulation = new StreamingSimulation();
    simulation.startRun(runnerId, "Hello");
  });

  it("accumulates deltas into a single assistant message", () => {
    simulation.delta(runnerId, "Hi ");
    simulation.delta(runnerId, "there!");

    const rendered = simulation.renderedAssistantTexts(runnerId);
    expect(rendered).toEqual(["Hi there!"]);
  });

  it("preserves pre-tool text when a tool call arrives", () => {
    simulation.delta(runnerId, "Before tool");
    simulation.toolCall(runnerId, "search", '{"q":"test"}');

    // The committed assistant keeps the pre-tool text; a new empty tail is
    // created after the tool for post-tool streaming.
    const assistants = simulation.messages.filter(
      (message) => message.type === "assistant",
    );
    expect(assistants).toHaveLength(2);
    expect(assistants[0].content).toBe("Before tool");
    expect(assistants[1].content).toBe("");
  });

  it("does not duplicate text across tool call boundaries", () => {
    simulation.delta(runnerId, "Before");
    simulation.toolCall(runnerId, "search", "{}");
    simulation.toolResult(runnerId, "search", "result");
    simulation.delta(runnerId, "After");

    const rendered = simulation.renderedAssistantTexts(runnerId);
    // Two assistant messages: pre-tool and post-tool, with distinct content.
    expect(rendered).toHaveLength(2);
    expect(rendered[0]).toBe("Before");
    expect(rendered[1]).toBe("After");
  });

  it("orders messages as assistant text, then tool calls (matches history)", () => {
    simulation.delta(runnerId, "Let me search");
    simulation.toolCall(runnerId, "search", '{"q":"test"}');
    simulation.toolResult(runnerId, "search", "found");
    simulation.delta(runnerId, "Here are the results");
    simulation.final(runnerId);

    // Message order should match what convertHistory produces on reload:
    // assistant text → tool-invoke → tool-result → assistant text
    const types = simulation.messages
      .filter((message) => message.type !== "user")
      .map((message) => message.type);
    expect(types).toEqual([
      "assistant", // "Let me search"
      "tool-invoke",
      "tool-result",
      "assistant", // "Here are the results"
    ]);
  });

  it("final event does not overwrite split messages with full server text", () => {
    simulation.delta(runnerId, "Part1");
    simulation.toolCall(runnerId, "tool", "{}");
    simulation.toolResult(runnerId, "tool", "ok");
    simulation.delta(runnerId, "Part2");
    // Server sends full concatenated text in final event.
    simulation.final(runnerId, "Part1Part2");

    const assistants = simulation.messages.filter(
      (message) => message.type === "assistant",
    );
    expect(assistants).toHaveLength(2);
    expect(assistants[0].content).toBe("Part1");
    expect(assistants[1].content).toBe("Part2");
  });

  it("simple stream without tool calls works with server final text", () => {
    simulation.delta(runnerId, "Complete");
    simulation.final(runnerId, "Complete response");

    const assistants = simulation.messages.filter(
      (message) => message.type === "assistant",
    );
    expect(assistants).toHaveLength(1);
    // Without tool splits, server text is preferred.
    expect(assistants[0].content).toBe("Complete response");
  });

  it("handles tool call with no preceding text", () => {
    // Model immediately calls a tool without streaming text first.
    simulation.toolCall(runnerId, "search", '{"q":"test"}');
    simulation.toolResult(runnerId, "search", "found");
    simulation.delta(runnerId, "Response");
    simulation.final(runnerId);

    const assistants = simulation.messages.filter(
      (message) => message.type === "assistant",
    );
    expect(assistants).toHaveLength(1);
    expect(assistants[0].content).toBe("Response");
  });

  it("handles multiple sequential tool calls", () => {
    simulation.delta(runnerId, "A");
    simulation.toolCall(runnerId, "tool1", "{}");
    simulation.toolResult(runnerId, "tool1", "r1");
    simulation.delta(runnerId, "B");
    simulation.toolCall(runnerId, "tool2", "{}");
    simulation.toolResult(runnerId, "tool2", "r2");
    simulation.delta(runnerId, "C");
    simulation.final(runnerId);

    const assistants = simulation.messages.filter(
      (message) => message.type === "assistant",
    );
    expect(assistants).toHaveLength(3);
    expect(assistants[0].content).toBe("A");
    expect(assistants[1].content).toBe("B");
    expect(assistants[2].content).toBe("C");
  });

  it("handles consecutive tool calls without text between them", () => {
    simulation.delta(runnerId, "Intro");
    simulation.toolCall(runnerId, "tool1", "{}");
    simulation.toolResult(runnerId, "tool1", "r1");
    // Second tool call without any delta between.
    simulation.toolCall(runnerId, "tool2", "{}");
    simulation.toolResult(runnerId, "tool2", "r2");
    simulation.delta(runnerId, "Done");
    simulation.final(runnerId);

    const assistants = simulation.messages.filter(
      (message) => message.type === "assistant",
    );
    // "Intro" committed on first tool_call; second tool_call has no new text;
    // "Done" streamed after tool results.
    const nonEmpty = assistants.filter((message) => message.content);
    expect(nonEmpty.map((message) => message.content)).toEqual([
      "Intro",
      "Done",
    ]);
  });

  it("removes empty trailing assistant on final with no post-tool text", () => {
    simulation.delta(runnerId, "Text");
    simulation.toolCall(runnerId, "tool", "{}");
    simulation.toolResult(runnerId, "tool", "ok");
    // Final arrives without any post-tool deltas.
    simulation.final(runnerId);

    const assistants = simulation.messages.filter(
      (message) => message.type === "assistant",
    );
    // Only the pre-tool message should remain.
    expect(assistants).toHaveLength(1);
    expect(assistants[0].content).toBe("Text");
  });

  it("rendered texts are never empty while streaming", () => {
    simulation.delta(runnerId, "Hello");
    expect(simulation.renderedAssistantTexts(runnerId)).toEqual(["Hello"]);

    simulation.toolCall(runnerId, "tool", "{}");
    // After tool call, streaming is false; content was committed.
    const texts = simulation.renderedAssistantTexts(runnerId);
    expect(texts.filter((text) => text)).toEqual(["Hello"]);

    simulation.toolResult(runnerId, "tool", "ok");
    simulation.delta(runnerId, "World");
    const textsAfter = simulation.renderedAssistantTexts(runnerId);
    expect(textsAfter).toEqual(["Hello", "World"]);
  });

  it("preserves committed content when final has no post-tool text (two tool rounds)", () => {
    // Sequence: delta → tool_call → tool_result → delta → tool_call → tool_result → final
    // The second tool_call clears streamTextRef, but the assistant already has
    // committed content from the second delta segment.  Final must not remove it.
    simulation.delta(runnerId, "Before");
    simulation.toolCall(runnerId, "tool1", "{}");
    simulation.toolResult(runnerId, "tool1", "r1");
    simulation.delta(runnerId, "Middle");
    simulation.toolCall(runnerId, "tool2", "{}");
    simulation.toolResult(runnerId, "tool2", "r2");
    // Final arrives without any post-tool-2 deltas — streamText is ''.
    simulation.final(runnerId, "BeforeMiddle");

    const assistants = simulation.messages.filter(
      (message) => message.type === "assistant",
    );
    const nonEmpty = assistants.filter((message) => message.content);
    expect(nonEmpty.map((message) => message.content)).toEqual([
      "Before",
      "Middle",
    ]);
  });

  it("does not lose content when final arrives without server text after a tool call", () => {
    // Tool call with no post-tool deltas and server sends no text in final.
    simulation.delta(runnerId, "Hello");
    simulation.toolCall(runnerId, "tool", "{}");
    simulation.toolResult(runnerId, "tool", "ok");
    // Final with no server text and no post-tool deltas.
    simulation.final(runnerId);

    const assistants = simulation.messages.filter(
      (message) => message.type === "assistant",
    );
    expect(assistants).toHaveLength(1);
    expect(assistants[0].content).toBe("Hello");
  });

  it("rendered text falls back to content when streamText is empty", () => {
    // Simulate a state where isStreaming is true but streamText is '' —
    // the UI should fall back to the committed content, not show nothing.
    simulation.delta(runnerId, "Hello");
    simulation.toolCall(runnerId, "tool", "{}");
    // After tool_call, content is committed to 'Hello' and streamText is ''.
    // Force isStreaming true to simulate a transient edge case.
    simulation.isStreaming = true;
    const texts = simulation.renderedAssistantTexts(runnerId);
    // With || instead of ??, the empty streamText falls back to content.
    expect(texts.filter((text) => text)).toEqual(["Hello"]);
  });

  it("preserves all segments across three tool-call rounds", () => {
    simulation.delta(runnerId, "A");
    simulation.toolCall(runnerId, "t1", "{}");
    simulation.toolResult(runnerId, "t1", "r1");
    simulation.delta(runnerId, "B");
    simulation.toolCall(runnerId, "t2", "{}");
    simulation.toolResult(runnerId, "t2", "r2");
    simulation.delta(runnerId, "C");
    simulation.toolCall(runnerId, "t3", "{}");
    simulation.toolResult(runnerId, "t3", "r3");
    // Final with no post-tool-3 deltas.
    simulation.final(runnerId);

    const assistants = simulation.messages.filter(
      (message) => message.type === "assistant",
    );
    const nonEmpty = assistants.filter((message) => message.content);
    expect(nonEmpty.map((message) => message.content)).toEqual(["A", "B", "C"]);
  });
});

describe("findRunAssistantIndex", () => {
  it("returns the last assistant with matching runnerId", () => {
    const messages: DisplayMessage[] = [
      { id: "1", type: "assistant", content: "A", runnerId: "r1" },
      { id: "2", type: "tool-invoke", content: "{}", toolName: "t" },
      { id: "3", type: "assistant", content: "", runnerId: "r1" },
    ];
    expect(findRunAssistantIndex(messages, "r1")).toBe(2);
  });

  it("falls back to last index when no match found", () => {
    const messages: DisplayMessage[] = [
      { id: "1", type: "user", content: "hi" },
    ];
    expect(findRunAssistantIndex(messages, "r-unknown")).toBe(0);
  });

  it("falls back to last index when runnerId is null", () => {
    const messages: DisplayMessage[] = [
      { id: "1", type: "user", content: "hi" },
      { id: "2", type: "assistant", content: "", runnerId: "r1" },
    ];
    expect(findRunAssistantIndex(messages, null)).toBe(1);
  });
});

describe("reconcileRunStateFromHistory", () => {
  it("clears stale active run tracking when history has no active run", () => {
    const activeRuns = new Map<string, string>([["conv-1", "stale-run"]]);
    const reconciled = reconcileRunStateFromHistory(
      activeRuns,
      "conv-1",
      undefined,
    );

    expect(reconciled).toEqual({
      currentRunnerId: null,
      runQueue: [],
      isRunning: false,
    });
    expect(activeRuns.has("conv-1")).toBe(false);
  });

  it("tracks active run when history reports one", () => {
    const activeRuns = new Map<string, string>();
    const reconciled = reconcileRunStateFromHistory(
      activeRuns,
      "conv-1",
      "run-123",
    );

    expect(reconciled).toEqual({
      currentRunnerId: "run-123",
      runQueue: ["run-123"],
      isRunning: true,
    });
    expect(activeRuns.get("conv-1")).toBe("run-123");
  });
});
