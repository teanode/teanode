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
import {
  reconcileRunStateFromHistory,
  shouldHydrateConversation,
} from "./useBackend";

// ─── Types (subset of src/types.ts needed for the test) ────────────

interface DisplayMessage {
  id: string;
  type: "user" | "assistant" | "tool-invoke" | "tool-result" | "usage";
  content: string;
  toolName?: string;
  runId?: string;
  timestamp?: number;
}

// ─── Helpers (mirrored from useBackend.ts) ──────────────────────────

let messageIdCounter = 0;
function nextMessageId(): string {
  return `msg-${++messageIdCounter}`;
}

function findRunAssistantIndex(
  messages: DisplayMessage[],
  runId: string | null,
): number {
  if (!runId) return messages.length - 1;
  for (let index = messages.length - 1; index >= 0; index--) {
    if (
      messages[index].type === "assistant" &&
      messages[index].runId === runId
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
  startRun(runId: string, userText: string) {
    this.messages.push(
      {
        id: nextMessageId(),
        type: "user",
        content: userText,
        timestamp: Date.now(),
      },
      { id: nextMessageId(), type: "assistant", content: "", runId },
    );
    this.streamText = "";
    this.afterToolCalls = false;
    this.isStreaming = false;
  }

  /** Process a delta event (streaming text chunk). */
  delta(runId: string, text: string) {
    if (this.afterToolCalls) {
      const previousText = this.streamText;
      if (previousText) {
        const updated = [...this.messages];
        const assistantIndex = findRunAssistantIndex(updated, runId);
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
          runId,
        };
        updated.splice(assistantIndex + 1, 0, newAssistant);
        this.messages = updated;
      } else {
        const updated = [...this.messages];
        const assistantIndex = findRunAssistantIndex(updated, runId);
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
            runId,
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
  toolCall(runId: string, toolName: string, args: string) {
    this.afterToolCalls = true;
    const accumulatedText = this.streamText;
    this.streamText = "";
    const updated = [...this.messages];
    const assistantIndex = findRunAssistantIndex(updated, runId);
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
        runId,
      };
      updated.splice(assistantIndex + 2, 0, newTail);
    } else {
      updated.splice(assistantIndex, 0, toolMessage);
    }
    this.messages = updated;
    this.isStreaming = false;
  }

  /** Process a tool_result event. */
  toolResult(runId: string, toolName: string, result: string) {
    const updated = [...this.messages];
    const assistantIndex = findRunAssistantIndex(updated, runId);
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
  final(runId: string, serverText?: string) {
    const updated = [...this.messages];
    const assistantIndex = findRunAssistantIndex(updated, runId);
    const hasToolSplits = updated.some(
      (message, index) =>
        index !== assistantIndex &&
        message.type === "assistant" &&
        message.runId === runId,
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
  renderedAssistantTexts(activeRunId: string): string[] {
    // Determine the last streaming assistant ID (mirrors MessageList logic).
    let lastStreamingAssistantId: string | null = null;
    if (this.isStreaming) {
      for (let index = this.messages.length - 1; index >= 0; index--) {
        if (
          this.messages[index].type === "assistant" &&
          this.messages[index].runId === activeRunId
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
  const runId = "run-1";

  beforeEach(() => {
    messageIdCounter = 0;
    simulation = new StreamingSimulation();
    simulation.startRun(runId, "Hello");
  });

  it("accumulates deltas into a single assistant message", () => {
    simulation.delta(runId, "Hi ");
    simulation.delta(runId, "there!");

    const rendered = simulation.renderedAssistantTexts(runId);
    expect(rendered).toEqual(["Hi there!"]);
  });

  it("preserves pre-tool text when a tool call arrives", () => {
    simulation.delta(runId, "Before tool");
    simulation.toolCall(runId, "search", '{"q":"test"}');

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
    simulation.delta(runId, "Before");
    simulation.toolCall(runId, "search", "{}");
    simulation.toolResult(runId, "search", "result");
    simulation.delta(runId, "After");

    const rendered = simulation.renderedAssistantTexts(runId);
    // Two assistant messages: pre-tool and post-tool, with distinct content.
    expect(rendered).toHaveLength(2);
    expect(rendered[0]).toBe("Before");
    expect(rendered[1]).toBe("After");
  });

  it("orders messages as assistant text, then tool calls (matches history)", () => {
    simulation.delta(runId, "Let me search");
    simulation.toolCall(runId, "search", '{"q":"test"}');
    simulation.toolResult(runId, "search", "found");
    simulation.delta(runId, "Here are the results");
    simulation.final(runId);

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
    simulation.delta(runId, "Part1");
    simulation.toolCall(runId, "tool", "{}");
    simulation.toolResult(runId, "tool", "ok");
    simulation.delta(runId, "Part2");
    // Server sends full concatenated text in final event.
    simulation.final(runId, "Part1Part2");

    const assistants = simulation.messages.filter(
      (message) => message.type === "assistant",
    );
    expect(assistants).toHaveLength(2);
    expect(assistants[0].content).toBe("Part1");
    expect(assistants[1].content).toBe("Part2");
  });

  it("simple stream without tool calls works with server final text", () => {
    simulation.delta(runId, "Complete");
    simulation.final(runId, "Complete response");

    const assistants = simulation.messages.filter(
      (message) => message.type === "assistant",
    );
    expect(assistants).toHaveLength(1);
    // Without tool splits, server text is preferred.
    expect(assistants[0].content).toBe("Complete response");
  });

  it("handles tool call with no preceding text", () => {
    // Model immediately calls a tool without streaming text first.
    simulation.toolCall(runId, "search", '{"q":"test"}');
    simulation.toolResult(runId, "search", "found");
    simulation.delta(runId, "Response");
    simulation.final(runId);

    const assistants = simulation.messages.filter(
      (message) => message.type === "assistant",
    );
    expect(assistants).toHaveLength(1);
    expect(assistants[0].content).toBe("Response");
  });

  it("handles multiple sequential tool calls", () => {
    simulation.delta(runId, "A");
    simulation.toolCall(runId, "tool1", "{}");
    simulation.toolResult(runId, "tool1", "r1");
    simulation.delta(runId, "B");
    simulation.toolCall(runId, "tool2", "{}");
    simulation.toolResult(runId, "tool2", "r2");
    simulation.delta(runId, "C");
    simulation.final(runId);

    const assistants = simulation.messages.filter(
      (message) => message.type === "assistant",
    );
    expect(assistants).toHaveLength(3);
    expect(assistants[0].content).toBe("A");
    expect(assistants[1].content).toBe("B");
    expect(assistants[2].content).toBe("C");
  });

  it("handles consecutive tool calls without text between them", () => {
    simulation.delta(runId, "Intro");
    simulation.toolCall(runId, "tool1", "{}");
    simulation.toolResult(runId, "tool1", "r1");
    // Second tool call without any delta between.
    simulation.toolCall(runId, "tool2", "{}");
    simulation.toolResult(runId, "tool2", "r2");
    simulation.delta(runId, "Done");
    simulation.final(runId);

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
    simulation.delta(runId, "Text");
    simulation.toolCall(runId, "tool", "{}");
    simulation.toolResult(runId, "tool", "ok");
    // Final arrives without any post-tool deltas.
    simulation.final(runId);

    const assistants = simulation.messages.filter(
      (message) => message.type === "assistant",
    );
    // Only the pre-tool message should remain.
    expect(assistants).toHaveLength(1);
    expect(assistants[0].content).toBe("Text");
  });

  it("rendered texts are never empty while streaming", () => {
    simulation.delta(runId, "Hello");
    expect(simulation.renderedAssistantTexts(runId)).toEqual(["Hello"]);

    simulation.toolCall(runId, "tool", "{}");
    // After tool call, streaming is false; content was committed.
    const texts = simulation.renderedAssistantTexts(runId);
    expect(texts.filter((text) => text)).toEqual(["Hello"]);

    simulation.toolResult(runId, "tool", "ok");
    simulation.delta(runId, "World");
    const textsAfter = simulation.renderedAssistantTexts(runId);
    expect(textsAfter).toEqual(["Hello", "World"]);
  });

  it("preserves committed content when final has no post-tool text (two tool rounds)", () => {
    // Sequence: delta → tool_call → tool_result → delta → tool_call → tool_result → final
    // The second tool_call clears streamTextRef, but the assistant already has
    // committed content from the second delta segment.  Final must not remove it.
    simulation.delta(runId, "Before");
    simulation.toolCall(runId, "tool1", "{}");
    simulation.toolResult(runId, "tool1", "r1");
    simulation.delta(runId, "Middle");
    simulation.toolCall(runId, "tool2", "{}");
    simulation.toolResult(runId, "tool2", "r2");
    // Final arrives without any post-tool-2 deltas — streamText is ''.
    simulation.final(runId, "BeforeMiddle");

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
    simulation.delta(runId, "Hello");
    simulation.toolCall(runId, "tool", "{}");
    simulation.toolResult(runId, "tool", "ok");
    // Final with no server text and no post-tool deltas.
    simulation.final(runId);

    const assistants = simulation.messages.filter(
      (message) => message.type === "assistant",
    );
    expect(assistants).toHaveLength(1);
    expect(assistants[0].content).toBe("Hello");
  });

  it("rendered text falls back to content when streamText is empty", () => {
    // Simulate a state where isStreaming is true but streamText is '' —
    // the UI should fall back to the committed content, not show nothing.
    simulation.delta(runId, "Hello");
    simulation.toolCall(runId, "tool", "{}");
    // After tool_call, content is committed to 'Hello' and streamText is ''.
    // Force isStreaming true to simulate a transient edge case.
    simulation.isStreaming = true;
    const texts = simulation.renderedAssistantTexts(runId);
    // With || instead of ??, the empty streamText falls back to content.
    expect(texts.filter((text) => text)).toEqual(["Hello"]);
  });

  it("preserves all segments across three tool-call rounds", () => {
    simulation.delta(runId, "A");
    simulation.toolCall(runId, "t1", "{}");
    simulation.toolResult(runId, "t1", "r1");
    simulation.delta(runId, "B");
    simulation.toolCall(runId, "t2", "{}");
    simulation.toolResult(runId, "t2", "r2");
    simulation.delta(runId, "C");
    simulation.toolCall(runId, "t3", "{}");
    simulation.toolResult(runId, "t3", "r3");
    // Final with no post-tool-3 deltas.
    simulation.final(runId);

    const assistants = simulation.messages.filter(
      (message) => message.type === "assistant",
    );
    const nonEmpty = assistants.filter((message) => message.content);
    expect(nonEmpty.map((message) => message.content)).toEqual(["A", "B", "C"]);
  });
});

describe("findRunAssistantIndex", () => {
  it("returns the last assistant with matching runId", () => {
    const messages: DisplayMessage[] = [
      { id: "1", type: "assistant", content: "A", runId: "r1" },
      { id: "2", type: "tool-invoke", content: "{}", toolName: "t" },
      { id: "3", type: "assistant", content: "", runId: "r1" },
    ];
    expect(findRunAssistantIndex(messages, "r1")).toBe(2);
  });

  it("falls back to last index when no match found", () => {
    const messages: DisplayMessage[] = [
      { id: "1", type: "user", content: "hi" },
    ];
    expect(findRunAssistantIndex(messages, "r-unknown")).toBe(0);
  });

  it("falls back to last index when runId is null", () => {
    const messages: DisplayMessage[] = [
      { id: "1", type: "user", content: "hi" },
      { id: "2", type: "assistant", content: "", runId: "r1" },
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
      currentRunId: null,
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
      currentRunId: "run-123",
      runQueue: ["run-123"],
      isRunning: true,
    });
    expect(activeRuns.get("conv-1")).toBe("run-123");
  });
});

describe("shouldHydrateConversation", () => {
  it("hydrates when no current conversation and default exists", () => {
    expect(shouldHydrateConversation(null, "conv-1", false)).toBe(true);
  });

  it("does not hydrate when user wants a new conversation", () => {
    expect(shouldHydrateConversation(null, "conv-1", true)).toBe(false);
  });

  it("does not hydrate when a conversation is already active", () => {
    expect(shouldHydrateConversation("conv-2", "conv-1", false)).toBe(false);
  });

  it("does not hydrate when no default conversation exists", () => {
    expect(shouldHydrateConversation(null, undefined, false)).toBe(false);
  });

  it("does not hydrate when no default and user wants new", () => {
    expect(shouldHydrateConversation(null, undefined, true)).toBe(false);
  });
});

// ─── Reconnect question rehydration ─────────────────────────────────
//
// Simulates the useEffect added in useBackend that rehydrates pending
// questions on WebSocket reconnect.  The effect fires when `connected`
// becomes true and there is an active `conversationId`, calling
// loadPendingQuestions which issues a questions.list RPC.  These tests
// verify the guard conditions, the RPC response processing, and the
// stale-response guard.

import type { PendingQuestion } from "../types";

/** Simulates the reconnect rehydration effect + loadPendingQuestions. */
class ReconnectRehydrationSimulation {
  connected = false;
  conversationId: string | null = null;
  conversationIdRef: { current: string | null } = { current: null };
  pendingQuestions: PendingQuestion[] = [];
  rpcCalls: Array<{ method: string; params: unknown }> = [];

  /** Mocked sendRpc — records calls and resolves with mock data. */
  private mockResponses = new Map<string, unknown>();

  setMockResponse(conversationId: string, questions: PendingQuestion[]) {
    this.mockResponses.set(conversationId, { questions });
  }

  /** Simulates what loadPendingQuestions does. */
  loadPendingQuestions(targetConversationId?: string) {
    const convId = targetConversationId || this.conversationIdRef.current;
    if (!convId) {
      this.pendingQuestions = [];
      return;
    }
    this.rpcCalls.push({
      method: "questions.list",
      params: { conversationId: convId },
    });
    // Simulate async response (synchronous for test).
    const response = this.mockResponses.get(convId) as
      | { questions: PendingQuestion[] }
      | undefined;
    // Stale guard: if conversation changed before response arrived, discard.
    if (this.conversationIdRef.current !== convId) return;
    this.pendingQuestions = response?.questions ?? [];
  }

  /** Simulates the useEffect guard + call. */
  runRehydrationEffect() {
    if (this.connected && this.conversationId) {
      this.loadPendingQuestions(this.conversationId);
    }
  }

  /** Helper to set both state and ref (mirrors useBackend line 321). */
  setConversation(id: string | null) {
    this.conversationId = id;
    this.conversationIdRef.current = id;
  }
}

function makePendingQuestion(
  id: string,
  conversationId = "conv-1",
): PendingQuestion {
  return {
    id,
    conversationId,
    agentId: "agent-1",
    runId: "run-1",
    question: "Pick one?",
    choices: ["A", "B"],
  };
}

describe("reconnect question rehydration", () => {
  let sim: ReconnectRehydrationSimulation;

  beforeEach(() => {
    sim = new ReconnectRehydrationSimulation();
  });

  it("calls questions.list on reconnect when a conversation is active", () => {
    sim.setConversation("conv-1");
    sim.connected = true;
    sim.setMockResponse("conv-1", [makePendingQuestion("q1")]);

    sim.runRehydrationEffect();

    expect(sim.rpcCalls).toHaveLength(1);
    expect(sim.rpcCalls[0]).toEqual({
      method: "questions.list",
      params: { conversationId: "conv-1" },
    });
    expect(sim.pendingQuestions).toHaveLength(1);
    expect(sim.pendingQuestions[0].id).toBe("q1");
  });

  it("does not call questions.list when disconnected", () => {
    sim.setConversation("conv-1");
    sim.connected = false;

    sim.runRehydrationEffect();

    expect(sim.rpcCalls).toHaveLength(0);
    expect(sim.pendingQuestions).toHaveLength(0);
  });

  it("does not call questions.list without an active conversation", () => {
    sim.setConversation(null);
    sim.connected = true;

    sim.runRehydrationEffect();

    expect(sim.rpcCalls).toHaveLength(0);
    expect(sim.pendingQuestions).toHaveLength(0);
  });

  it("sets pendingQuestions from broker response so dialog would open", () => {
    const q1 = makePendingQuestion("q1", "conv-1");
    const q2 = makePendingQuestion("q2", "conv-1");
    sim.setConversation("conv-1");
    sim.connected = true;
    sim.setMockResponse("conv-1", [q1, q2]);

    sim.runRehydrationEffect();

    // QuestionPanel renders when pendingQuestions.length > 0.
    expect(sim.pendingQuestions.length > 0).toBe(true);
    expect(sim.pendingQuestions).toEqual([q1, q2]);
  });

  it("returns empty when broker has no pending questions", () => {
    sim.setConversation("conv-1");
    sim.connected = true;
    sim.setMockResponse("conv-1", []);

    sim.runRehydrationEffect();

    expect(sim.pendingQuestions).toHaveLength(0);
  });

  it("ignores stale response if conversation changed before RPC returns", () => {
    sim.setConversation("conv-1");
    sim.connected = true;
    sim.setMockResponse("conv-1", [makePendingQuestion("q1", "conv-1")]);

    // Simulate: user switches conversation before RPC response arrives.
    // Override loadPendingQuestions to switch conversation mid-call.
    const originalLoad = sim.loadPendingQuestions.bind(sim);
    sim.loadPendingQuestions = (targetConversationId?: string) => {
      const convId = targetConversationId || sim.conversationIdRef.current;
      if (!convId) {
        sim.pendingQuestions = [];
        return;
      }
      sim.rpcCalls.push({
        method: "questions.list",
        params: { conversationId: convId },
      });
      // Simulate conversation switch happening before response.
      sim.conversationIdRef.current = "conv-2";
      // Now simulate the response arriving — stale guard should discard.
      const response = sim.pendingQuestions;
      if (sim.conversationIdRef.current !== convId) return; // stale!
      sim.pendingQuestions = [makePendingQuestion("q1", "conv-1")];
    };

    sim.runRehydrationEffect();

    // Stale guard prevented setting questions for conv-1 since we're on conv-2.
    expect(sim.pendingQuestions).toHaveLength(0);
  });

  it("rehydrates correctly on reconnect cycle (disconnect → reconnect)", () => {
    // Initial state: connected with a pending question.
    sim.setConversation("conv-1");
    sim.connected = true;
    sim.pendingQuestions = [makePendingQuestion("q1")];

    // Simulate disconnect — pendingQuestions should NOT be cleared.
    sim.connected = false;
    // (No code path clears pendingQuestions on disconnect.)
    expect(sim.pendingQuestions).toHaveLength(1);

    // Simulate reconnect — effect fires, rehydrates from broker.
    sim.connected = true;
    sim.setMockResponse("conv-1", [makePendingQuestion("q1")]);
    sim.runRehydrationEffect();

    expect(sim.pendingQuestions).toHaveLength(1);
    expect(sim.pendingQuestions[0].id).toBe("q1");
  });
});

// ─── Reconnect busy-state rehydration ───────────────────────────────
//
// Simulates the state transitions that occur when a WebSocket reconnects
// (or the page is refreshed) while a run is active.  The key invariant:
// after history loads with an activeRunId, the UI must show a spinner
// (isRunning=true, isStreaming=false) until streaming events arrive.

interface ReconnectState {
  isRunning: boolean;
  isStreaming: boolean;
  streamText: string;
  toolActivity: string | null;
  status: string;
  currentRunId: string | null;
  messages: DisplayMessage[];
  historyLoaded: boolean;
  pendingEvents: Array<{ state: string; runId?: string; text?: string; toolName?: string }>;
  loadVersion: number;
}

/**
 * Simulates the reconnect flow: history load → state reconciliation →
 * event replay.  Mirrors the logic in handleConnect and switchConversation.
 */
class ReconnectSimulation {
  state: ReconnectState;

  constructor() {
    this.state = {
      isRunning: false,
      isStreaming: false,
      streamText: "",
      toolActivity: null,
      status: "connected",
      currentRunId: null,
      messages: [],
      historyLoaded: true,
      pendingEvents: [],
      loadVersion: 0,
    };
  }

  /** Simulate starting a run (pre-disconnect state). */
  setPreDisconnectState(opts: {
    isRunning: boolean;
    isStreaming: boolean;
    streamText: string;
    currentRunId: string | null;
    messages: DisplayMessage[];
  }) {
    this.state.isRunning = opts.isRunning;
    this.state.isStreaming = opts.isStreaming;
    this.state.streamText = opts.streamText;
    this.state.currentRunId = opts.currentRunId;
    this.state.messages = opts.messages;
  }

  /** Simulate handleStatusChange("connected") on reconnect. */
  onSocketOpen() {
    this.state.historyLoaded = false;
    this.state.pendingEvents = [];
  }

  /** Buffer an event (arrives while history is loading). */
  bufferEvent(event: { state: string; runId?: string; text?: string; toolName?: string }) {
    if (!this.state.historyLoaded) {
      this.state.pendingEvents.push(event);
    }
  }

  /**
   * Simulate handleConnect's history load completing.
   * Mirrors the .then() in handleConnect / switchConversation.
   */
  processHistoryResponse(opts: {
    activeRunId?: string;
    historyMessages: DisplayMessage[];
    loadVersion: number;
  }) {
    // Load version guard
    if (this.state.loadVersion !== opts.loadVersion) return;

    const displayMessages = [...opts.historyMessages];

    // Reconcile run state
    const reconciled = reconcileRunStateFromHistory(
      new Map(),
      "conv-1",
      opts.activeRunId,
    );
    this.state.currentRunId = reconciled.currentRunId;
    this.state.isRunning = reconciled.isRunning;

    // Always reset streaming state (the fix)
    this.state.streamText = "";
    this.state.isStreaming = false;
    this.state.toolActivity = null;

    if (reconciled.isRunning) {
      this.state.status = "thinking...";
      displayMessages.push({
        id: nextMessageId(),
        type: "assistant",
        content: "",
        runId: opts.activeRunId,
      });
    } else {
      this.state.status = "connected";
    }

    this.state.messages = displayMessages;
    this.state.historyLoaded = true;

    // Replay buffered events
    if (reconciled.isRunning && this.state.pendingEvents.length > 0) {
      for (const event of this.state.pendingEvents) {
        this.replayEvent(event);
      }
    }
    this.state.pendingEvents = [];
  }

  /** Start a history load (increments version). */
  startHistoryLoad(): number {
    this.state.historyLoaded = false;
    this.state.pendingEvents = [];
    return ++this.state.loadVersion;
  }

  /** Apply a replayed or live event (simplified). */
  private replayEvent(event: { state: string; runId?: string; text?: string; toolName?: string }) {
    if (event.state === "delta") {
      this.state.streamText += event.text || "";
      this.state.isStreaming = true;
      this.state.toolActivity = null;
    } else if (event.state === "tool_call") {
      this.state.isStreaming = false;
      this.state.toolActivity = event.toolName || null;
      this.state.status = `calling ${event.toolName}...`;
    } else if (event.state === "tool_result") {
      this.state.toolActivity = null;
      this.state.status = "tool done, thinking...";
    } else if (event.state === "final") {
      this.state.isRunning = false;
      this.state.isStreaming = false;
      this.state.streamText = "";
      this.state.currentRunId = null;
      this.state.status = "connected";
    }
  }

  /** Check if the spinner should be visible (mirrors MessageList condition). */
  spinnerVisible(): boolean {
    if (!this.state.isRunning) return false;
    if (this.state.isStreaming) return false;
    // Check for empty assistant placeholder matching active run
    const placeholder = this.state.messages.find(
      (m) =>
        m.type === "assistant" &&
        !m.content &&
        (m.runId === this.state.currentRunId || !m.runId),
    );
    return !!placeholder;
  }
}

describe("reconnect busy-state rehydration", () => {
  let sim: ReconnectSimulation;

  beforeEach(() => {
    messageIdCounter = 100; // avoid collision with other tests
    sim = new ReconnectSimulation();
  });

  it("shows spinner when history reports an active run", () => {
    sim.onSocketOpen();
    const version = sim.startHistoryLoad();

    sim.processHistoryResponse({
      activeRunId: "run-1",
      historyMessages: [
        { id: "h1", type: "user", content: "Hello" },
      ],
      loadVersion: version,
    });

    expect(sim.state.isRunning).toBe(true);
    expect(sim.state.isStreaming).toBe(false);
    expect(sim.state.status).toBe("thinking...");
    expect(sim.state.currentRunId).toBe("run-1");
    expect(sim.spinnerVisible()).toBe(true);
  });

  it("resets stale isStreaming so spinner is visible after reconnect", () => {
    // Simulate pre-disconnect state: streaming was active
    sim.setPreDisconnectState({
      isRunning: true,
      isStreaming: true,
      streamText: "partial response...",
      currentRunId: "run-1",
      messages: [
        { id: "m1", type: "user", content: "Hello" },
        { id: "m2", type: "assistant", content: "", runId: "run-1" },
      ],
    });

    // Reconnect
    sim.onSocketOpen();
    const version = sim.startHistoryLoad();

    sim.processHistoryResponse({
      activeRunId: "run-1",
      historyMessages: [
        { id: "h1", type: "user", content: "Hello" },
      ],
      loadVersion: version,
    });

    // The stale isStreaming must be reset to false so the spinner shows
    expect(sim.state.isStreaming).toBe(false);
    expect(sim.state.streamText).toBe("");
    expect(sim.spinnerVisible()).toBe(true);
  });

  it("replays buffered delta events after history loads", () => {
    sim.onSocketOpen();
    const version = sim.startHistoryLoad();

    // Events arrive while history is loading
    sim.bufferEvent({ state: "delta", runId: "run-1", text: "Hello " });
    sim.bufferEvent({ state: "delta", runId: "run-1", text: "world" });

    sim.processHistoryResponse({
      activeRunId: "run-1",
      historyMessages: [
        { id: "h1", type: "user", content: "Hi" },
      ],
      loadVersion: version,
    });

    // After replay, streaming should be active with accumulated text
    expect(sim.state.isRunning).toBe(true);
    expect(sim.state.isStreaming).toBe(true);
    expect(sim.state.streamText).toBe("Hello world");
  });

  it("replays buffered tool_call event after history loads", () => {
    sim.onSocketOpen();
    const version = sim.startHistoryLoad();

    sim.bufferEvent({ state: "tool_call", runId: "run-1", toolName: "search" });

    sim.processHistoryResponse({
      activeRunId: "run-1",
      historyMessages: [
        { id: "h1", type: "user", content: "Search for X" },
        { id: "h2", type: "assistant", content: "Let me search" },
        { id: "h3", type: "tool-invoke", content: "{}", toolName: "search" },
      ],
      loadVersion: version,
    });

    expect(sim.state.isRunning).toBe(true);
    expect(sim.state.isStreaming).toBe(false);
    expect(sim.state.toolActivity).toBe("search");
    expect(sim.state.status).toBe("calling search...");
  });

  it("does not show spinner when history reports no active run", () => {
    sim.onSocketOpen();
    const version = sim.startHistoryLoad();

    sim.processHistoryResponse({
      activeRunId: undefined,
      historyMessages: [
        { id: "h1", type: "user", content: "Hello" },
        { id: "h2", type: "assistant", content: "Hi there!" },
      ],
      loadVersion: version,
    });

    expect(sim.state.isRunning).toBe(false);
    expect(sim.state.status).toBe("connected");
    expect(sim.spinnerVisible()).toBe(false);
  });

  it("handles run completing during history load (final event buffered)", () => {
    sim.onSocketOpen();
    const version = sim.startHistoryLoad();

    // Run completes while history is loading
    sim.bufferEvent({ state: "final", runId: "run-1", text: "Done!" });

    // History arrives still showing the run as active (race window)
    sim.processHistoryResponse({
      activeRunId: "run-1",
      historyMessages: [
        { id: "h1", type: "user", content: "Hello" },
      ],
      loadVersion: version,
    });

    // The replayed final event should clear the running state
    expect(sim.state.isRunning).toBe(false);
    expect(sim.state.status).toBe("connected");
    expect(sim.spinnerVisible()).toBe(false);
  });

  it("discards stale history response when load version is superseded", () => {
    sim.onSocketOpen();
    const version1 = sim.startHistoryLoad();

    // A newer load (e.g. switchConversation) supersedes
    const version2 = sim.startHistoryLoad();

    // Stale response arrives first
    sim.processHistoryResponse({
      activeRunId: "run-old",
      historyMessages: [
        { id: "h1", type: "user", content: "Old" },
      ],
      loadVersion: version1,
    });

    // State should NOT have changed (stale response discarded)
    expect(sim.state.isRunning).toBe(false);
    expect(sim.state.historyLoaded).toBe(false);

    // Fresh response arrives
    sim.processHistoryResponse({
      activeRunId: "run-new",
      historyMessages: [
        { id: "h2", type: "user", content: "New" },
      ],
      loadVersion: version2,
    });

    // Now state should be updated
    expect(sim.state.isRunning).toBe(true);
    expect(sim.state.currentRunId).toBe("run-new");
    expect(sim.spinnerVisible()).toBe(true);
  });

  it("drops buffered events when history shows run is not active", () => {
    sim.onSocketOpen();
    const version = sim.startHistoryLoad();

    // Events buffered during history load
    sim.bufferEvent({ state: "delta", runId: "run-1", text: "stale" });

    // History shows no active run (run already finished)
    sim.processHistoryResponse({
      activeRunId: undefined,
      historyMessages: [
        { id: "h1", type: "user", content: "Hello" },
        { id: "h2", type: "assistant", content: "Done" },
      ],
      loadVersion: version,
    });

    // Buffered events should have been dropped, not replayed
    expect(sim.state.isStreaming).toBe(false);
    expect(sim.state.streamText).toBe("");
    expect(sim.state.pendingEvents).toHaveLength(0);
  });

  it("clears tool activity on reconnect even when run is active", () => {
    // Pre-disconnect: tool was running
    sim.setPreDisconnectState({
      isRunning: true,
      isStreaming: false,
      streamText: "",
      currentRunId: "run-1",
      messages: [
        { id: "m1", type: "user", content: "Hello" },
        { id: "m2", type: "assistant", content: "", runId: "run-1" },
      ],
    });
    sim.state.toolActivity = "web_search";

    sim.onSocketOpen();
    const version = sim.startHistoryLoad();

    sim.processHistoryResponse({
      activeRunId: "run-1",
      historyMessages: [
        { id: "h1", type: "user", content: "Hello" },
      ],
      loadVersion: version,
    });

    // toolActivity should be reset (will be re-set if a tool_call event replays)
    expect(sim.state.toolActivity).toBeNull();
    expect(sim.spinnerVisible()).toBe(true);
  });
});
