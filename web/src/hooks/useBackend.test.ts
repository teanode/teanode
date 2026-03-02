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
  convertHistory,
} from "./useBackend";

// ─── Types (subset of src/types.ts needed for the test) ────────────

interface DisplayMessage {
  id: string;
  type: "user" | "assistant" | "tool-invoke" | "tool-result" | "usage";
  content: string;
  toolName?: string;
  toolCallId?: string;
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
    // Insert after the last tool-invoke (mirrors updated useBackend logic).
    let insertPos = assistantIndex;
    for (let i = assistantIndex - 1; i >= 0; i--) {
      if (updated[i].type === "tool-invoke") {
        insertPos = i + 1;
        break;
      }
    }
    updated.splice(insertPos, 0, toolMessage);
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

// ─── convertHistory tool ordering ────────────────────────────────────

import type { Message } from "../types";

describe("convertHistory tool ordering", () => {
  it("places tool-result after its matching tool-invoke by toolCallId", () => {
    const msgs: Message[] = [
      { role: "user", content: "do both" },
      {
        role: "assistant",
        content: null,
        toolCalls: [
          { id: "tc1", function: { name: "search", arguments: '{"q":"a"}' } },
          { id: "tc2", function: { name: "fetch", arguments: '{"url":"b"}' } },
        ],
      },
      { role: "tool", content: "result-a", toolCallId: "tc1", toolName: "search" },
      { role: "tool", content: "result-b", toolCallId: "tc2", toolName: "fetch" },
      { role: "assistant", content: "Done." },
    ];

    const display = convertHistory(msgs, []);
    const types = display.map((m) => m.type);

    // Expected: invoke-tc1, result-tc1, invoke-tc2, result-tc2, assistant
    expect(types).toEqual([
      "user",
      "tool-invoke",  // search
      "tool-result",  // search result
      "tool-invoke",  // fetch
      "tool-result",  // fetch result
      "assistant",    // "Done."
    ]);
    expect(display[1].toolCallId).toBe("tc1");
    expect(display[2].toolCallId).toBe("tc1");
    expect(display[3].toolCallId).toBe("tc2");
    expect(display[4].toolCallId).toBe("tc2");
  });

  it("falls back to append when toolCallId is missing", () => {
    const msgs: Message[] = [
      { role: "user", content: "hi" },
      {
        role: "assistant",
        content: null,
        toolCalls: [
          { function: { name: "search", arguments: "{}" } },
        ],
      },
      { role: "tool", content: "ok", toolName: "search" },
    ];

    const display = convertHistory(msgs, []);
    const types = display.map((m) => m.type);
    expect(types).toEqual(["user", "tool-invoke", "tool-result"]);
  });

  it("handles single tool call correctly", () => {
    const msgs: Message[] = [
      { role: "user", content: "search" },
      {
        role: "assistant",
        content: "Let me search",
        toolCalls: [
          { id: "tc1", function: { name: "search", arguments: '{"q":"test"}' } },
        ],
      },
      { role: "tool", content: "found", toolCallId: "tc1", toolName: "search" },
      { role: "assistant", content: "Here it is." },
    ];

    const display = convertHistory(msgs, []);
    const types = display.map((m) => m.type);
    expect(types).toEqual([
      "user",
      "assistant",    // "Let me search"
      "tool-invoke",  // search
      "tool-result",  // found
      "assistant",    // "Here it is."
    ]);
  });

  it("handles three parallel tool calls", () => {
    const msgs: Message[] = [
      { role: "user", content: "go" },
      {
        role: "assistant",
        content: null,
        toolCalls: [
          { id: "a", function: { name: "t1", arguments: "{}" } },
          { id: "b", function: { name: "t2", arguments: "{}" } },
          { id: "c", function: { name: "t3", arguments: "{}" } },
        ],
      },
      { role: "tool", content: "r1", toolCallId: "a", toolName: "t1" },
      { role: "tool", content: "r2", toolCallId: "b", toolName: "t2" },
      { role: "tool", content: "r3", toolCallId: "c", toolName: "t3" },
    ];

    const display = convertHistory(msgs, []);
    const toolMsgs = display.filter(
      (m) => m.type === "tool-invoke" || m.type === "tool-result",
    );
    // Each invoke immediately followed by its result
    expect(toolMsgs.map((m) => `${m.type}:${m.toolCallId}`)).toEqual([
      "tool-invoke:a",
      "tool-result:a",
      "tool-invoke:b",
      "tool-result:b",
      "tool-invoke:c",
      "tool-result:c",
    ]);
  });
});

// ─── Streaming tool-result ordering ──────────────────────────────────

describe("streaming tool-result ordering", () => {
  let simulation: StreamingSimulation;
  const runId = "run-1";

  beforeEach(() => {
    messageIdCounter = 0;
    simulation = new StreamingSimulation();
    simulation.startRun(runId, "Hello");
  });

  it("places tool-result directly after tool-invoke (no pre-tool text)", () => {
    simulation.toolCall(runId, "search", "{}");
    simulation.toolResult(runId, "search", "found");

    const types = simulation.messages
      .filter((m) => m.type !== "user")
      .map((m) => m.type);
    expect(types).toEqual(["tool-invoke", "tool-result", "assistant"]);
  });

  it("places tool-result directly after tool-invoke (with pre-tool text)", () => {
    simulation.delta(runId, "text");
    simulation.toolCall(runId, "search", "{}");
    simulation.toolResult(runId, "search", "found");

    const types = simulation.messages
      .filter((m) => m.type !== "user")
      .map((m) => m.type);
    expect(types).toEqual([
      "assistant",    // "text"
      "tool-invoke",
      "tool-result",
      "assistant",    // streaming tail
    ]);
  });

  it("consecutive tool calls keep invoke-result pairs together", () => {
    simulation.delta(runId, "A");
    simulation.toolCall(runId, "tool1", "{}");
    simulation.toolResult(runId, "tool1", "r1");
    simulation.toolCall(runId, "tool2", "{}");
    simulation.toolResult(runId, "tool2", "r2");
    simulation.final(runId);

    const types = simulation.messages
      .filter((m) => m.type !== "user")
      .map((m) => m.type);

    // Each tool-result should immediately follow its tool-invoke
    const toolMsgs = types.filter((t) => t.startsWith("tool"));
    expect(toolMsgs).toEqual([
      "tool-invoke",  // tool1
      "tool-result",  // tool1 result
      "tool-invoke",  // tool2
      "tool-result",  // tool2 result
    ]);
  });
});

// ─── useDebugEnabled ─────────────────────────────────────────────────

import { useDebugEnabled } from "../components/DebugReadout";
import { vi } from "vitest";

describe("useDebugEnabled", () => {
  it("returns true when URL has ?debug=1", () => {
    vi.stubGlobal("window", {
      location: { search: "?debug=1" },
    });
    vi.stubGlobal("localStorage", {
      getItem: () => null,
    });
    expect(useDebugEnabled()).toBe(true);
    vi.unstubAllGlobals();
  });

  it("returns true when localStorage debug is 1", () => {
    vi.stubGlobal("window", {
      location: { search: "" },
    });
    vi.stubGlobal("localStorage", {
      getItem: (key: string) => (key === "debug" ? "1" : null),
    });
    expect(useDebugEnabled()).toBe(true);
    vi.unstubAllGlobals();
  });

  it("returns false when neither flag is set", () => {
    vi.stubGlobal("window", {
      location: { search: "" },
    });
    vi.stubGlobal("localStorage", {
      getItem: () => null,
    });
    expect(useDebugEnabled()).toBe(false);
    vi.unstubAllGlobals();
  });
});
