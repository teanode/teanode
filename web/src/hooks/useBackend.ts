import { useState, useCallback, useRef, useEffect } from "react";
import type {
  ActiveRunState,
  Attachment,
  Conversation,
  DisplayMessage,
  ConversationEvent,
  EventFrame,
  ConnectResult,
  ConversationSendResult,
  ConversationHistoryResult,
  ConversationsListResult,
  ModelsListResult,
  AgentsConfigListResult,
  AgentsSetDefaultResult,
  ConversationsSetDefaultResult,
  ModelInfo,
  AgentInfo,
  Message,
  ToolCall,
  Usage,
  Job,
  JobCreateParams,
  JobUpdateParams,
  JobsListResult,
  Todo,
  ConversationTodosEvent,
  ConversationTodosListResult,
  PendingQuestion,
  PendingQuestionsListResult,
  ConversationQuestionsEvent,
} from "../types";
import { useWebSocket } from "./useWebSocket";
import { normalizeContent, type ExtractedContent } from "../contentUtils";

let messageIdCounter = 0;
function nextMessageId(): string {
  return `msg-${++messageIdCounter}`;
}

function extractContent(message: Message): string {
  return extractContentWithAttachments(message).text;
}

function extractContentWithAttachments(message: Message): ExtractedContent {
  return normalizeContent(message.content);
}

function parseToolCalls(raw: ToolCall[] | string | undefined): ToolCall[] {
  if (!raw) return [];
  if (typeof raw === "string") {
    try {
      return JSON.parse(raw);
    } catch {
      return [];
    }
  }
  return raw;
}

function getUsageNumbers(
  usage: Usage | undefined,
): { input: number; output: number; total: number } | null {
  if (!usage) return null;
  const input = usage.input ?? usage.Input ?? 0;
  const output = usage.output ?? usage.Output ?? 0;
  const total = usage.total ?? usage.Total ?? input + output;
  if (!total) return null;
  return { input, output, total };
}

function compactNumber(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
  return String(n);
}

function formatUsageText(
  usageNumbers: { input: number; output: number; total: number },
  contextWindow?: number,
): string {
  let text = `${compactNumber(usageNumbers.input)} in / ${compactNumber(usageNumbers.output)} out \u00b7 ${compactNumber(usageNumbers.total)} tokens`;
  if (contextWindow && contextWindow > 0 && usageNumbers.input > 0) {
    const percentage = (usageNumbers.input / contextWindow) * 100;
    text += ` \u00b7 ${percentage < 1 ? "<1" : Math.round(percentage)}% context`;
  }
  return text;
}

/** Look up context_length for a model from the models list. */
function findContextWindow(
  models: ModelInfo[],
  model?: string,
): number | undefined {
  if (!model || !models.length) return undefined;
  // Try exact match on id first.
  const match = models.find((modelInfo) => modelInfo.id === model);
  return match?.context_length;
}

/** Find the assistant message placeholder for a given runId. */
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
  return messages.length - 1; // fallback
}

interface ReconciledRunState {
  currentRunId: string | null;
  runQueue: string[];
  isRunning: boolean;
}

/**
 * Decides whether handleConnect should hydrate the default conversation.
 * Returns false when the user is deliberately on the new-conversation page.
 */
export function shouldHydrateConversation(
  currentConversationId: string | null,
  hydrationDefaultConversationId: string | undefined,
  wantsNewConversation: boolean,
): boolean {
  return (
    !currentConversationId &&
    !!hydrationDefaultConversationId &&
    !wantsNewConversation
  );
}

export function reconcileRunStateFromHistory(
  activeRuns: Map<string, string>,
  conversationKey: string,
  activeRunId?: string,
): ReconciledRunState {
  if (activeRunId) {
    activeRuns.set(conversationKey, activeRunId);
    return {
      currentRunId: activeRunId,
      runQueue: [activeRunId],
      isRunning: true,
    };
  }

  activeRuns.delete(conversationKey);
  return {
    currentRunId: null,
    runQueue: [],
    isRunning: false,
  };
}

export function convertHistory(
  msgs: Message[],
  models: ModelInfo[],
): DisplayMessage[] {
  const displayMessages: DisplayMessage[] = [];
  for (const message of msgs) {
    const extracted = extractContentWithAttachments(message);
    const content = extracted.text;
    const timestamp = message.timestamp;
    if (message.role === "user") {
      displayMessages.push({
        id: nextMessageId(),
        type: "user",
        content,
        timestamp,
        attachments: extracted.attachments,
      });
    } else if (message.role === "assistant") {
      const toolCalls = parseToolCalls(message.toolCalls);
      if (toolCalls.length > 0) {
        if (content?.trim()) {
          displayMessages.push({
            id: nextMessageId(),
            type: "assistant",
            content,
            timestamp,
          });
        }
        for (const toolCall of toolCalls) {
          const functionData =
            toolCall.function ||
            (toolCall as unknown as { name: string; arguments: string });
          displayMessages.push({
            id: nextMessageId(),
            type: "tool-invoke",
            content: functionData.arguments || "{}",
            toolName: functionData.name || "tool",
            toolCallId: toolCall.id,
            timestamp,
          });
        }
      } else if (content?.trim()) {
        displayMessages.push({
          id: nextMessageId(),
          type: "assistant",
          content,
          timestamp,
        });
        const usageNumbers = getUsageNumbers(message.usage);
        if (usageNumbers) {
          const contextWindow = findContextWindow(
            models,
            message.providerModelName,
          );
          displayMessages.push({
            id: nextMessageId(),
            type: "usage",
            content: formatUsageText(usageNumbers, contextWindow),
            usage: message.usage,
            timestamp,
          });
        }
      }
    } else if (message.role === "tool") {
      const toolResult: DisplayMessage = {
        id: nextMessageId(),
        type: "tool-result",
        content,
        toolName: message.toolName || "tool",
        toolCallId: message.toolCallId,
        timestamp,
      };
      // Place tool-result immediately after its matching tool-invoke so
      // invoke always renders above result, even with parallel tool calls.
      if (message.toolCallId) {
        let inserted = false;
        for (let i = displayMessages.length - 1; i >= 0; i--) {
          if (
            displayMessages[i].type === "tool-invoke" &&
            displayMessages[i].toolCallId === message.toolCallId
          ) {
            displayMessages.splice(i + 1, 0, toolResult);
            inserted = true;
            break;
          }
        }
        if (!inserted) displayMessages.push(toolResult);
      } else {
        displayMessages.push(toolResult);
      }
    }
  }
  return displayMessages;
}

export function useBackend() {
  const [conversations, setConversations] = useState<Conversation[]>([]);
  const [conversationId, setConversationId] = useState<string | null>(null);
  const [messages, setMessages] = useState<DisplayMessage[]>([]);
  const [isRunning, setIsRunning] = useState(false);
  const [status, setStatus] = useState("connecting...");
  const [defaultProviderModelName, setDefaultProviderModelName] = useState("");
  const [models, setModels] = useState<ModelInfo[]>([]);
  const [streamText, setStreamText] = useState("");
  const [isStreaming, setIsStreaming] = useState(false);
  const [toolActivity, setToolActivity] = useState<string | null>(null);
  const [agents, setAgents] = useState<AgentInfo[]>([]);
  const [currentAgentId, setCurrentAgentId] = useState<string>("");
  const [serverDefaultAgentId, setServerDefaultAgentId] = useState<string>("");
  const [connected, setConnected] = useState(false);
  const [connecting, setConnecting] = useState(true);
  const [hasConnectedOnce, setHasConnectedOnce] = useState(false);
  const [isAdmin, setIsAdmin] = useState(false);
  const [currentUserId, setCurrentUserId] = useState<string>("");
  const [conversationModel, setConversationModel] = useState<string | null>(
    null,
  );
  const [audioCapability, setAudioCapability] = useState(false);
  const [lastActiveRunState, setLastActiveRunState] =
    useState<ActiveRunState | null>(null);
  const lastSentViaMicRef = useRef(false);
  const currentAgentIdRef = useRef(currentAgentId);
  const modelsRef = useRef(models);

  const conversationIdRef = useRef(conversationId);
  const conversationModelRef = useRef(conversationModel);
  conversationIdRef.current = conversationId;
  conversationModelRef.current = conversationModel;
  currentAgentIdRef.current = currentAgentId;
  modelsRef.current = models;

  const currentRunIdRef = useRef<string | null>(null);
  const activeRunsRef = useRef<Map<string, string>>(new Map());
  const afterToolCallsRef = useRef(false);
  const streamTextRef = useRef("");
  const conversationsRef = useRef(conversations);
  conversationsRef.current = conversations;

  // When true, the user deliberately navigated to the "new conversation" page.
  // Prevents handleConnect from hydrating a default conversation on reconnect.
  const wantsNewConversationRef = useRef(false);

  const conversationsRefreshRef = useRef(0);
  const historyLoadedRef = useRef(true);
  const pendingEventsRef = useRef<EventFrame[]>([]);
  const runQueueRef = useRef<string[]>([]); // ordered run IDs: [active, queued1, queued2, ...]
  const selfOriginIdsRef = useRef<Set<string>>(new Set()); // origin IDs for self-sent messages
  const hasConnectedOnceRef = useRef(hasConnectedOnce);
  hasConnectedOnceRef.current = hasConnectedOnce;
  const disconnectGraceTimerRef = useRef<ReturnType<typeof setTimeout> | null>(
    null,
  );

  // Pagination state
  const [hasMoreHistory, setHasMoreHistory] = useState(false);
  const [loadingOlderMessages, setLoadingOlderMessages] = useState(false);
  const oldestLoadedIndexRef = useRef(0);

  function touchConversation(conversationId: string) {
    const now = Date.now();
    setConversations((previous) => {
      const updated = previous.map((conversation) =>
        conversation.id === conversationId
          ? { ...conversation, lastActive: now }
          : conversation,
      );
      conversationsRef.current = updated;
      return updated;
    });
  }

  function finishCurrentRun() {
    streamTextRef.current = "";
    afterToolCallsRef.current = false;
    setStreamText("");
    setIsStreaming(false);
    setToolActivity(null);

    // Remove finished run from queue
    if (currentRunIdRef.current) {
      const index = runQueueRef.current.indexOf(currentRunIdRef.current);
      if (index !== -1) runQueueRef.current.splice(index, 1);
    }

    // Promote next queued run or finish
    if (runQueueRef.current.length > 0) {
      currentRunIdRef.current = runQueueRef.current[0];
      setStatus("thinking...");
      // Keep isRunning = true
    } else {
      currentRunIdRef.current = null;
      if (conversationIdRef.current) {
        activeRunsRef.current.delete(conversationIdRef.current);
      }
      setIsRunning(false);
      setStatus("connected");
    }
  }

  const handleEvent = useCallback((frame: EventFrame) => {
    if (frame.event === "defaultAgent") {
      const payload = frame.payload as
        | { defaultAgentId?: string; defaultConversationId?: string }
        | undefined;
      if (payload?.defaultAgentId) {
        setServerDefaultAgentId(payload.defaultAgentId);
        setAgents((previous) =>
          previous.map((agent) =>
            agent.id === payload.defaultAgentId
              ? {
                  ...agent,
                  defaultConversationId: payload.defaultConversationId,
                }
              : agent,
          ),
        );
      }
      return;
    }

    if (frame.event === "defaultConversation") {
      const payload = frame.payload as
        | { agentId?: string; defaultConversationId?: string }
        | undefined;
      if (payload?.agentId) {
        setAgents((previous) =>
          previous.map((agent) =>
            agent.id === payload.agentId
              ? {
                  ...agent,
                  defaultConversationId: payload.defaultConversationId,
                }
              : agent,
          ),
        );
      }
      return;
    }

    if (frame.event === "conversationTodos") {
      const payload = frame.payload as ConversationTodosEvent | undefined;
      if (payload && payload.conversationId === conversationIdRef.current) {
        if (payload.action === "batch" && payload.results) {
          setTodos((prev) => {
            let updated = [...prev];
            for (const result of payload.results!) {
              if (!result.success) continue;
              if (result.op === "add" && result.todo) {
                if (!updated.some((t) => t.id === result.todo!.id)) {
                  updated = [result.todo!, ...updated];
                }
              } else if (
                (result.op === "update" ||
                  result.op === "complete" ||
                  result.op === "reopen") &&
                result.todo
              ) {
                updated = updated.map((t) =>
                  t.id === result.todo!.id ? result.todo! : t,
                );
              } else if (result.op === "delete" && result.todoId) {
                updated = updated.filter((t) => t.id !== result.todoId);
              }
            }
            return updated;
          });
        } else if (payload.action === "prune") {
          loadTodos();
        }
      }
      return;
    }

    if (frame.event === "conversationQuestions") {
      const payload = frame.payload as ConversationQuestionsEvent | undefined;
      if (payload) {
        if (
          payload.action === "asked" &&
          payload.conversationId === conversationIdRef.current
        ) {
          setPendingQuestions((prev) => {
            if (prev.some((q) => q.id === payload.questionId)) return prev;
            const q: PendingQuestion = {
              id: payload.questionId,
              conversationId: payload.conversationId!,
              agentId: payload.agentId || "",
              runId: payload.runId || "",
              question: payload.question || "",
              choices: payload.choices || [],
            };
            if (payload.allowOther) {
              q.allowOther = true;
              if (payload.otherLabel) q.otherLabel = payload.otherLabel;
              if (payload.otherPlaceholder)
                q.otherPlaceholder = payload.otherPlaceholder;
            }
            return [...prev, q];
          });
          setStatus("waiting for your answer...");
        } else if (payload.action === "answered") {
          setPendingQuestions((prev) =>
            prev.filter((q) => q.id !== payload.questionId),
          );
        }
      }
      return;
    }

    if (frame.event === "conversations") {
      const now = Date.now();
      if (now - conversationsRefreshRef.current < 2000) return;
      conversationsRefreshRef.current = now;
      sendRpcRef
        .current<ConversationsListResult>("conversations.list", {})
        .then((res) => {
          const list = res.conversations || [];
          setConversations(list);
          conversationsRef.current = list;
        })
        .catch((error: unknown) =>
          console.error("conversations.list (event):", error),
        );
      return;
    }

    if (frame.event !== "conversation") return;
    const conversationEvent = frame.payload as ConversationEvent;
    if (!conversationEvent) return;

    // Clean up activeRuns for completed runs (no message mutation)
    if (
      conversationEvent.state === "final" ||
      conversationEvent.state === "error" ||
      conversationEvent.state === "aborted"
    ) {
      if (
        conversationEvent.conversationId &&
        activeRunsRef.current.get(conversationEvent.conversationId) ===
          conversationEvent.runId
      ) {
        activeRunsRef.current.delete(conversationEvent.conversationId);
      }
    }

    // Buffer events for the current conversation while history is loading
    if (
      !historyLoadedRef.current &&
      conversationEvent.conversationId === conversationIdRef.current
    ) {
      pendingEventsRef.current.push(frame);
      return;
    }

    // Handle queued events early — no UI update needed, placeholder is already visible
    if (conversationEvent.state === "queued") {
      return;
    }

    // Handle user messages from external sources (Discord, Telegram, scheduled jobs)
    if (conversationEvent.state === "user_message") {
      const eventOrigin = (
        conversationEvent as ConversationEvent & { origin?: string }
      ).origin;
      // Voice sessions can start before a specific conversation route is active.
      // Auto-bind to the first voice user_message conversation so UI renders it.
      if (
        !conversationIdRef.current &&
        eventOrigin === "voice" &&
        conversationEvent.conversationId
      ) {
        setConversationId(conversationEvent.conversationId);
        conversationIdRef.current = conversationEvent.conversationId;
        setMessages([]);
      }
      if (conversationEvent.conversationId)
        touchConversation(conversationEvent.conversationId);
      if (conversationEvent.conversationId === conversationIdRef.current) {
        // Skip self-sent messages — sendMessage already added them optimistically.
        if (
          conversationEvent.originId &&
          selfOriginIdsRef.current.delete(conversationEvent.originId)
        ) {
          return;
        }
        // If this run is already tracked (e.g. from history load), skip adding
        // duplicate messages — the history already contains them.
        const alreadyTracked =
          currentRunIdRef.current === conversationEvent.runId;
        currentRunIdRef.current = conversationEvent.runId || null;
        if (conversationEvent.runId && conversationEvent.conversationId) {
          activeRunsRef.current.set(
            conversationEvent.conversationId,
            conversationEvent.runId,
          );
        }
        if (conversationEvent.runId && !alreadyTracked) {
          runQueueRef.current.push(conversationEvent.runId);
        }
        setIsRunning(true);
        setStatus("thinking...");
        if (!alreadyTracked) {
          const assistantMessageId = nextMessageId();
          setMessages((prev) => [
            ...prev,
            {
              id: nextMessageId(),
              type: "user",
              content: conversationEvent.text || "",
              timestamp: Date.now(),
              attachments: conversationEvent.attachments,
            },
            {
              id: assistantMessageId,
              type: "assistant",
              content: "",
              runId: conversationEvent.runId || undefined,
            },
          ]);
          streamTextRef.current = "";
          setStreamText("");
        }
        // Don't set isStreaming — let the delta event set it
      }
      return;
    }

    // Auto-detect new runs on current conversation from broadcast events.
    // This catches events that arrive before the RPC response sets currentRunIdRef
    // (e.g. when the run fails immediately, the "error" event races the RPC response).
    if (
      conversationEvent.runId &&
      conversationEvent.conversationId === conversationIdRef.current &&
      !currentRunIdRef.current
    ) {
      if (
        conversationEvent.state === "delta" ||
        conversationEvent.state === "text_done" ||
        conversationEvent.state === "tool_call" ||
        conversationEvent.state === "final" ||
        conversationEvent.state === "error" ||
        conversationEvent.state === "aborted"
      ) {
        currentRunIdRef.current = conversationEvent.runId;
        activeRunsRef.current.set(
          conversationEvent.conversationId,
          conversationEvent.runId,
        );
        if (!runQueueRef.current.includes(conversationEvent.runId)) {
          runQueueRef.current.push(conversationEvent.runId);
        }
        setIsRunning(true);
        setStatus("thinking...");
        setMessages((prev) => {
          // If sendMessage already created an untagged assistant placeholder,
          // tag it with the runId instead of creating a duplicate.
          const lastMessage = prev[prev.length - 1];
          if (
            lastMessage &&
            lastMessage.type === "assistant" &&
            !lastMessage.content &&
            !lastMessage.runId
          ) {
            return prev.map((message, index) =>
              index === prev.length - 1
                ? { ...message, runId: conversationEvent.runId }
                : message,
            );
          }
          return [
            ...prev,
            {
              id: nextMessageId(),
              type: "assistant",
              content: "",
              runId: conversationEvent.runId || undefined,
            },
          ];
        });
      }
    }

    // Only update UI for the currently viewed conversation
    if (
      !conversationIdRef.current ||
      conversationEvent.conversationId !== conversationIdRef.current
    )
      return;
    if (
      currentRunIdRef.current &&
      conversationEvent.runId !== currentRunIdRef.current
    )
      return;

    // Guard: skip final/error/aborted if we have no active run (avoids corrupting history)
    if (
      !currentRunIdRef.current &&
      (conversationEvent.state === "final" ||
        conversationEvent.state === "error" ||
        conversationEvent.state === "aborted")
    )
      return;

    const eventRunId = conversationEvent.runId || null;

    if (conversationEvent.state === "delta") {
      setToolActivity(null);
      if (afterToolCallsRef.current) {
        // New LLM round after tool calls — finalize old text and start fresh
        const prevText = streamTextRef.current;
        if (prevText) {
          setMessages((prev) => {
            const updated = [...prev];
            const assistantIndex = findRunAssistantIndex(updated, eventRunId);
            if (
              assistantIndex >= 0 &&
              updated[assistantIndex].type === "assistant"
            ) {
              updated[assistantIndex] = {
                ...updated[assistantIndex],
                content: prevText,
              };
            }
            // Add new streaming message after the finalized one
            const newAssistant: DisplayMessage = {
              id: nextMessageId(),
              type: "assistant",
              content: "",
              runId: eventRunId || undefined,
            };
            updated.splice(assistantIndex + 1, 0, newAssistant);
            return updated;
          });
        } else {
          // Empty old stream — just reset, reuse existing placeholder
          setMessages((prev) => {
            const updated = [...prev];
            const assistantIndex = findRunAssistantIndex(updated, eventRunId);
            if (
              assistantIndex >= 0 &&
              updated[assistantIndex].type === "assistant" &&
              !updated[assistantIndex].content
            ) {
              // Reuse it
            } else {
              const newAssistant: DisplayMessage = {
                id: nextMessageId(),
                type: "assistant",
                content: "",
                runId: eventRunId || undefined,
              };
              updated.splice(assistantIndex + 1, 0, newAssistant);
            }
            return updated;
          });
        }
        streamTextRef.current = "";
        setStreamText("");
        afterToolCallsRef.current = false;
      }
      streamTextRef.current += conversationEvent.text || "";
      setStreamText(streamTextRef.current);
      setIsStreaming(true);
    } else if (conversationEvent.state === "text_done") {
      // Text streaming ended; tool calls will follow. Commit streamed text
      // to the assistant message and transition to "thinking" state so the
      // spinner appears during the gap before the first tool_call event.
      const accumulatedText = streamTextRef.current;
      streamTextRef.current = "";
      setStreamText("");
      setIsStreaming(false);
      if (accumulatedText) {
        setMessages((prev) => {
          const updated = [...prev];
          const assistantIndex = findRunAssistantIndex(updated, eventRunId);
          if (
            assistantIndex >= 0 &&
            updated[assistantIndex].type === "assistant"
          ) {
            updated[assistantIndex] = {
              ...updated[assistantIndex],
              content: accumulatedText,
            };
            const newTail: DisplayMessage = {
              id: nextMessageId(),
              type: "assistant",
              content: "",
              runId: eventRunId || undefined,
            };
            updated.splice(assistantIndex + 1, 0, newTail);
          }
          return updated;
        });
      }
      setToolActivity(null);
      setStatus("thinking...");
    } else if (conversationEvent.state === "tool_call") {
      afterToolCallsRef.current = true;
      // Commit accumulated stream text to the assistant message before clearing
      // streaming state.  Without this, the text disappears when isStreaming
      // becomes false because content is still empty.
      const accumulatedText = streamTextRef.current;
      streamTextRef.current = "";
      setStreamText("");
      setMessages((prev) => {
        const updated = [...prev];
        const assistantIndex = findRunAssistantIndex(updated, eventRunId);
        const toolMsg: DisplayMessage = {
          id: nextMessageId(),
          type: "tool-invoke",
          content: conversationEvent.arguments || "{}",
          toolName: conversationEvent.toolName,
          timestamp: Date.now(),
        };
        if (
          accumulatedText &&
          assistantIndex >= 0 &&
          updated[assistantIndex].type === "assistant"
        ) {
          // Commit pre-tool text, place tool after it, and add a new
          // empty assistant as the streaming tail for post-tool content.
          updated[assistantIndex] = {
            ...updated[assistantIndex],
            content: accumulatedText,
          };
          updated.splice(assistantIndex + 1, 0, toolMsg);
          const newTail: DisplayMessage = {
            id: nextMessageId(),
            type: "assistant",
            content: "",
            runId: eventRunId || undefined,
          };
          updated.splice(assistantIndex + 2, 0, newTail);
        } else {
          // No pre-tool text — insert tool before the empty assistant tail.
          updated.splice(assistantIndex, 0, toolMsg);
        }
        return updated;
      });
      setIsStreaming(false);
      setToolActivity(conversationEvent.toolName || null);
      setStatus(`calling ${conversationEvent.toolName}...`);
    } else if (conversationEvent.state === "tool_result") {
      setMessages((prev) => {
        const updated = [...prev];
        const assistantIndex = findRunAssistantIndex(updated, eventRunId);
        const toolMsg: DisplayMessage = {
          id: nextMessageId(),
          type: "tool-result",
          content: conversationEvent.result || "",
          toolName: conversationEvent.toolName,
          timestamp: Date.now(),
        };
        // Place tool-result right after the last tool-invoke (before the
        // streaming assistant placeholder) so invoke always renders above
        // result.  Falls back to assistantIndex when no invoke is found.
        let insertPos = assistantIndex;
        for (let i = assistantIndex - 1; i >= 0; i--) {
          if (updated[i].type === "tool-invoke") {
            insertPos = i + 1;
            break;
          }
        }
        updated.splice(insertPos, 0, toolMsg);
        return updated;
      });
      setToolActivity(null);
      setStatus("tool done, thinking...");
    } else if (conversationEvent.state === "final") {
      if (conversationEvent.conversationId)
        touchConversation(conversationEvent.conversationId);
      setToolActivity(null);
      const finalTimestamp = Date.now();
      // Capture stream text before finishCurrentRun clears it — the
      // setMessages updater runs asynchronously (React batching) so reading
      // the ref inside the updater would see the cleared value.
      const capturedStreamText = streamTextRef.current;
      setMessages((prev) => {
        const updated = [...prev];
        const assistantIndex = findRunAssistantIndex(updated, eventRunId);
        // When tool calls split the response into multiple assistant messages,
        // use only the current segment's stream text to avoid duplicating
        // pre-tool content already committed to earlier messages.
        const hasToolSplits = updated.some(
          (message, index) =>
            index !== assistantIndex &&
            message.type === "assistant" &&
            message.runId === eventRunId,
        );
        const finalText = hasToolSplits
          ? capturedStreamText
          : conversationEvent.text || capturedStreamText;
        if (
          assistantIndex >= 0 &&
          updated[assistantIndex].type === "assistant"
        ) {
          if (finalText) {
            updated[assistantIndex] = {
              ...updated[assistantIndex],
              content: finalText,
              timestamp: finalTimestamp,
            };
          } else if (updated[assistantIndex].content) {
            // Assistant already has committed content from an earlier
            // tool_call — preserve it instead of removing the message.
            updated[assistantIndex] = {
              ...updated[assistantIndex],
              timestamp: finalTimestamp,
            };
          } else {
            // Remove truly empty placeholder
            updated.splice(assistantIndex, 1);
          }
        }
        // Add usage
        const usageNumbers = getUsageNumbers(conversationEvent.usage);
        if (usageNumbers) {
          const contextWindow =
            conversationEvent.contextWindow ||
            findContextWindow(
              modelsRef.current,
              conversationEvent.providerModelName,
            );
          // Insert usage after the assistant message (or at the position it was)
          const insertPosition = finalText
            ? assistantIndex + 1
            : assistantIndex;
          updated.splice(insertPosition, 0, {
            id: nextMessageId(),
            type: "usage",
            content: formatUsageText(usageNumbers, contextWindow),
            usage: conversationEvent.usage,
            timestamp: finalTimestamp,
          });
        }
        return updated;
      });
      finishCurrentRun();
    } else if (conversationEvent.state === "error") {
      setToolActivity(null);
      const capturedStreamText = streamTextRef.current;
      setMessages((prev) => {
        const updated = [...prev];
        const assistantIndex = findRunAssistantIndex(updated, eventRunId);
        if (
          assistantIndex >= 0 &&
          updated[assistantIndex].type === "assistant"
        ) {
          if (capturedStreamText) {
            updated[assistantIndex] = {
              ...updated[assistantIndex],
              content: capturedStreamText,
            };
          } else {
            updated[assistantIndex] = {
              ...updated[assistantIndex],
              content: `__error__:${conversationEvent.error || "Unknown error"}`,
            };
          }
        }
        return updated;
      });
      finishCurrentRun();
    } else if (conversationEvent.state === "aborted") {
      setToolActivity(null);
      const capturedStreamText = streamTextRef.current;
      setMessages((prev) => {
        const updated = [...prev];
        const assistantIndex = findRunAssistantIndex(updated, eventRunId);
        if (
          assistantIndex >= 0 &&
          updated[assistantIndex].type === "assistant"
        ) {
          if (capturedStreamText) {
            updated[assistantIndex] = {
              ...updated[assistantIndex],
              content: capturedStreamText,
            };
          } else {
            updated[assistantIndex] = {
              ...updated[assistantIndex],
              content: "__aborted__",
            };
          }
        }
        return updated;
      });
      finishCurrentRun();
    }
  }, []);

  // sendRpc is defined below but we need it in handleConnect — use a ref
  const sendRpcRef = useRef<
    <T = unknown>(method: string, params: unknown) => Promise<T>
  >(null!);

  const handleConnect = useCallback((result: ConnectResult) => {
    if (disconnectGraceTimerRef.current) {
      clearTimeout(disconnectGraceTimerRef.current);
      disconnectGraceTimerRef.current = null;
    }
    setConnecting(false);
    setConnected(true);
    setHasConnectedOnce(true);
    setIsAdmin(!!result.isAdmin);
    setCurrentUserId(result.userId || "");
    setAudioCapability(result.capabilities?.includes("audio") ?? false);
    if (result.defaultProviderModelName) {
      setDefaultProviderModelName(result.defaultProviderModelName);
    }
    if (result.agents) {
      setAgents(result.agents);
    }
    if (result.defaultAgentId) {
      setServerDefaultAgentId(result.defaultAgentId);
    }

    // Resolve the active agent for reconnect hydration. If the UI already has
    // one selected, keep it; otherwise fall back to server default.
    let hydrationAgentId =
      currentAgentIdRef.current || result.defaultAgentId || "";
    if (!currentAgentIdRef.current && hydrationAgentId) {
      setCurrentAgentId(hydrationAgentId);
      currentAgentIdRef.current = hydrationAgentId;
    }

    // Resolve default conversation for the active agent (not always the server default agent).
    const hydrationAgent = result.agents?.find(
      (agent) => agent.id === hydrationAgentId,
    );
    const hydrationDefaultConversationId =
      hydrationAgent?.defaultConversationId ||
      (hydrationAgentId === result.defaultAgentId
        ? result.defaultConversationId
        : undefined);

    if (
      shouldHydrateConversation(
        conversationIdRef.current,
        hydrationDefaultConversationId,
        wantsNewConversationRef.current,
      )
    ) {
      setConversationId(hydrationDefaultConversationId!);
      conversationIdRef.current = hydrationDefaultConversationId!;
    }
    // Fetch available models
    sendRpcRef
      .current<ModelsListResult>("models.list", {})
      .then((res) => {
        if (res.models) setModels(res.models);
      })
      .catch((error: unknown) => console.error("models.list:", error));

    // Load conversations on every (re)connect
    sendRpcRef
      .current<ConversationsListResult>("conversations.list", {})
      .then((res) => {
        const list = res.conversations || [];
        setConversations(list);
        conversationsRef.current = list;
      })
      .catch((error: unknown) => console.error("conversations.list:", error));

    // Reload current conversation's history on (re)connect.
    // If nothing is selected yet, use the server's default conversation.
    const key = conversationIdRef.current || hydrationDefaultConversationId;
    if (key) {
      historyLoadedRef.current = false;
      pendingEventsRef.current = [];
      sendRpcRef
        .current<ConversationHistoryResult>("conversations.history", {
          conversationId: key,
          agentId: hydrationAgentId || undefined,
          limit: 50,
        })
        .then((res) => {
          if (conversationIdRef.current !== key) return;
          const displayMessages = convertHistory(
            res.messages || [],
            modelsRef.current,
          );

          // Store pagination metadata
          oldestLoadedIndexRef.current = res.oldestLoadedIndex ?? 0;
          setHasMoreHistory(res.hasMore ?? false);

          setConversationModel(res.providerModelName || null);
          setLastActiveRunState(res.activeRunState || null);

          const reconciledRunState = reconcileRunStateFromHistory(
            activeRunsRef.current,
            key,
            res.activeRunId,
          );
          currentRunIdRef.current = reconciledRunState.currentRunId;
          runQueueRef.current = reconciledRunState.runQueue;
          setIsRunning(reconciledRunState.isRunning);
          if (reconciledRunState.isRunning) {
            setStatus("thinking...");
            displayMessages.push({
              id: nextMessageId(),
              type: "assistant",
              content: "",
              runId: res.activeRunId,
            });
          } else {
            streamTextRef.current = "";
            afterToolCallsRef.current = false;
            setStreamText("");
            setIsStreaming(false);
            setToolActivity(null);
            setStatus("connected");
          }
          setMessages(displayMessages);
          historyLoadedRef.current = true;
          // Replay buffered events (only if run is still active — otherwise history is complete)
          if (
            reconciledRunState.isRunning &&
            pendingEventsRef.current.length > 0
          ) {
            for (const event of pendingEventsRef.current) {
              handleEvent(event);
            }
          }
          pendingEventsRef.current = [];

          // Recover pending questions from the in-memory broker.
          sendRpcRef
            .current<PendingQuestionsListResult>("questions.list", {
              conversationId: key,
            })
            .then((qResult) => {
              if (conversationIdRef.current !== key) return;
              setPendingQuestions(qResult?.questions ?? []);
            })
            .catch((error: unknown) => console.error("questions.list:", error));
        })
        .catch((error: unknown) =>
          console.error("conversations.history reconnect:", error),
        );
    }
  }, []);

  const handleStatusChange = useCallback((nextStatus: string) => {
    setStatus(nextStatus);
    if (nextStatus === "connected") {
      if (disconnectGraceTimerRef.current) {
        clearTimeout(disconnectGraceTimerRef.current);
        disconnectGraceTimerRef.current = null;
      }
      setConnecting(false);
      setConnected(true);
      return;
    }

    setConnecting(true);

    // On first load, expose disconnect immediately so root can keep blocking.
    if (!hasConnectedOnceRef.current) {
      setConnected(false);
      return;
    }

    // iOS Safari may emit very short disconnects during app visibility changes.
    // Keep UI stable unless disconnection persists past a short grace period.
    if (disconnectGraceTimerRef.current) return;
    disconnectGraceTimerRef.current = setTimeout(() => {
      setConnected(false);
      disconnectGraceTimerRef.current = null;
    }, 1500);
  }, []);

  useEffect(() => {
    return () => {
      if (disconnectGraceTimerRef.current) {
        clearTimeout(disconnectGraceTimerRef.current);
        disconnectGraceTimerRef.current = null;
      }
    };
  }, []);

  const { sendRpc, sendBinary, onBinaryMessage, onVoiceMessage } = useWebSocket(
    {
      onEvent: handleEvent,
      onConnect: handleConnect,
      onStatusChange: handleStatusChange,
    },
  );

  sendRpcRef.current = sendRpc;

  // Load conversations (callable externally if needed)
  const loadConversations = useCallback(() => {
    sendRpc<ConversationsListResult>("conversations.list", {})
      .then((res) => {
        const list = res.conversations || [];
        setConversations(list);
        conversationsRef.current = list;
      })
      .catch((error) => console.error("conversations.list:", error));
  }, [sendRpc]);

  const switchConversation = useCallback(
    (key: string, agentId?: string) => {
      wantsNewConversationRef.current = false;
      // Detach current streaming state
      currentRunIdRef.current = null;
      streamTextRef.current = "";
      afterToolCallsRef.current = false;
      runQueueRef.current = [];
      setStreamText("");
      setIsStreaming(false);
      setToolActivity(null);

      // Switch agent if a different one is specified.
      if (agentId && agentId !== currentAgentIdRef.current) {
        setCurrentAgentId(agentId);
        currentAgentIdRef.current = agentId;
      }

      const resolvedAgentId = agentId || currentAgentIdRef.current || undefined;

      setConversationId(key);
      conversationIdRef.current = key;
      setMessages([]);

      // Ensure conversation appears in the sidebar list immediately
      setConversations((previous) => {
        const exists = previous.some((conversation) => conversation.id === key);
        if (!exists) {
          const updated = [{ id: key, lastActive: Date.now() }, ...previous];
          conversationsRef.current = updated;
          return updated;
        }
        return previous;
      });

      setIsRunning(false);
      setStatus("connected");

      // Reset pagination state
      oldestLoadedIndexRef.current = 0;
      setHasMoreHistory(false);

      // Reset todos and pending questions for new conversation
      setTodos([]);
      todosConversationIdRef.current = key;
      setPendingQuestions([]);

      // Buffer events while history is loading
      historyLoadedRef.current = false;
      pendingEventsRef.current = [];

      sendRpc<ConversationHistoryResult>("conversations.history", {
        conversationId: key,
        agentId: resolvedAgentId,
        limit: 50,
      })
        .then((res) => {
          if (conversationIdRef.current !== key) return;
          const displayMessages = convertHistory(
            res.messages || [],
            modelsRef.current,
          );

          // Store pagination metadata
          oldestLoadedIndexRef.current = res.oldestLoadedIndex ?? 0;
          setHasMoreHistory(res.hasMore ?? false);

          setConversationModel(res.providerModelName || null);
          setLastActiveRunState(res.activeRunState || null);

          // Use activeRunId from server response to detect active runs
          if (res.activeRunId) {
            currentRunIdRef.current = res.activeRunId;
            activeRunsRef.current.set(key, res.activeRunId);
            runQueueRef.current = [res.activeRunId];
            setIsRunning(true);
            setStatus("thinking...");
            displayMessages.push({
              id: nextMessageId(),
              type: "assistant",
              content: "",
              runId: res.activeRunId,
            });
          }

          setMessages(displayMessages);
          historyLoadedRef.current = true;

          // Replay buffered events (only if run is still active — otherwise history is complete)
          if (res.activeRunId && pendingEventsRef.current.length > 0) {
            for (const event of pendingEventsRef.current) {
              handleEvent(event);
            }
          }
          pendingEventsRef.current = [];

          // Load todos for this conversation
          sendRpc<ConversationTodosListResult>("conversations.todos.list", {
            conversationId: key,
          })
            .then((todosResult) => {
              if (conversationIdRef.current !== key) return;
              setTodos(todosResult.todos || []);
            })
            .catch((error) =>
              console.error("conversations.todos.list:", error),
            );

          // Recover pending questions from the in-memory broker.
          sendRpc<PendingQuestionsListResult>("questions.list", {
            conversationId: key,
          })
            .then((qResult) => {
              if (conversationIdRef.current !== key) return;
              setPendingQuestions(qResult?.questions ?? []);
            })
            .catch((error) => console.error("questions.list:", error));
        })
        .catch((error) => console.error("conversations.history:", error));
    },
    [sendRpc, handleEvent],
  );

  const newConversation = useCallback(() => {
    currentRunIdRef.current = null;
    streamTextRef.current = "";
    afterToolCallsRef.current = false;
    runQueueRef.current = [];
    setStreamText("");
    setIsStreaming(false);
    setIsRunning(false);
    setToolActivity(null);
    setConversationId(null);
    conversationIdRef.current = null;
    wantsNewConversationRef.current = true;
    setMessages([]);
    setStatus("connected");
    setConversationModel(null);
    // Reset pagination state
    oldestLoadedIndexRef.current = 0;
    setHasMoreHistory(false);
    // Clear todos and pending questions
    setTodos([]);
    todosConversationIdRef.current = null;
    setPendingQuestions([]);
  }, []);

  const loadOlderMessages = useCallback(() => {
    const key = conversationIdRef.current;
    if (!key || loadingOlderMessages || !hasMoreHistory) return;
    setLoadingOlderMessages(true);

    const agentId = currentAgentIdRef.current || undefined;
    sendRpc<ConversationHistoryResult>("conversations.history", {
      conversationId: key,
      agentId,
      limit: 50,
      beforeIndex: oldestLoadedIndexRef.current,
    })
      .then((res) => {
        if (conversationIdRef.current !== key) return;
        const olderDisplayMessages = convertHistory(
          res.messages || [],
          modelsRef.current,
        );
        oldestLoadedIndexRef.current = res.oldestLoadedIndex ?? 0;
        setHasMoreHistory(res.hasMore ?? false);
        setMessages((previous) => [...olderDisplayMessages, ...previous]);
      })
      .catch((error) => console.error("conversations.history (older):", error))
      .finally(() => setLoadingOlderMessages(false));
  }, [sendRpc, loadingOlderMessages, hasMoreHistory]);

  const deleteConversation = useCallback(
    (conversationId: string, agentId?: string) => {
      sendRpc("conversations.delete", { conversationId, agentId })
        .then(() => {
          setConversations((prev) => {
            const updated = prev.filter(
              (conversation) => conversation.id !== conversationId,
            );
            conversationsRef.current = updated;
            return updated;
          });
          if (conversationIdRef.current === conversationId) {
            setConversationId(null);
            conversationIdRef.current = null;
            setMessages([]);
          }
        })
        .catch((error) => console.error("conversations.delete:", error));
    },
    [sendRpc],
  );

  const sendMessage = useCallback(
    (
      text: string,
      model?: string,
      attachments?: Attachment[],
      voiceMode?: "call" | "input",
    ) => {
      if (!text.trim() && (!attachments || attachments.length === 0)) return;
      // Allow sending while running — backend queues per-conversation

      const now = Date.now();
      const assistantMessageId = nextMessageId();
      setMessages((prev) => [
        ...prev,
        {
          id: nextMessageId(),
          type: "user",
          content: text,
          timestamp: now,
          attachments,
        },
        {
          id: assistantMessageId,
          type: "assistant",
          content: "",
          timestamp: now,
        },
      ]);

      // Generate an origin ID so we can recognize our own broadcast echo.
      const originId = crypto.randomUUID();
      selfOriginIdsRef.current.add(originId);

      if (!isRunning) {
        // First message — set running state
        streamTextRef.current = "";
        setStreamText("");
        setIsRunning(true);
        setStatus("thinking...");
      }
      // Don't set isStreaming true — let the delta event set it
      setIsStreaming(false);

      const rpcParams: Record<string, unknown> = {
        conversationId: conversationIdRef.current || "",
        message: text,
        originId,
      };
      // Use conversation's locked model, fall back to explicit model param.
      const resolvedModel = conversationModelRef.current || model;
      if (resolvedModel) rpcParams.providerModelName = resolvedModel;
      if (currentAgentIdRef.current)
        rpcParams.agentId = currentAgentIdRef.current;
      if (attachments && attachments.length > 0)
        rpcParams.attachments = attachments;
      if (voiceMode) rpcParams.voiceMode = voiceMode;

      sendRpc<ConversationSendResult>("conversations.send", rpcParams)
        .then((res) => {
          // Tag assistant placeholder with runId
          setMessages((prev) =>
            prev.map((message) =>
              message.id === assistantMessageId
                ? { ...message, runId: res.runId }
                : message,
            ),
          );
          if (!runQueueRef.current.includes(res.runId)) {
            runQueueRef.current.push(res.runId);
          }
          if (!currentRunIdRef.current) {
            currentRunIdRef.current = res.runId;
          }
          activeRunsRef.current.set(res.conversationId, res.runId);
          touchConversation(res.conversationId);
          // Lock conversation model on first send.
          if (!conversationModelRef.current && resolvedModel) {
            setConversationModel(resolvedModel);
            conversationModelRef.current = resolvedModel;
          }
          if (!conversationIdRef.current) {
            wantsNewConversationRef.current = false;
            setConversationId(res.conversationId);
            conversationIdRef.current = res.conversationId;
            setConversations((prev) => {
              const exists = prev.some(
                (conversation) => conversation.id === res.conversationId,
              );
              if (!exists) {
                const updated = [
                  { id: res.conversationId, lastActive: Date.now() },
                  ...prev,
                ];
                conversationsRef.current = updated;
                return updated;
              }
              return prev;
            });
          }
        })
        .catch((error) => {
          selfOriginIdsRef.current.delete(originId);
          // Remove both user message and empty assistant placeholder
          setMessages((prev) => {
            const updated = [...prev];
            // Remove empty assistant placeholder
            if (
              updated.length > 0 &&
              updated[updated.length - 1].type === "assistant" &&
              updated[updated.length - 1].id === assistantMessageId
            ) {
              updated.pop();
              // Also remove the user message we just added
              if (
                updated.length > 0 &&
                updated[updated.length - 1].type === "user"
              ) {
                updated.pop();
              }
            }
            return updated;
          });
          setStatus(
            `error: ${(error as { message?: string }).message || error}`,
          );
          // Only clear running state if no other runs in queue
          if (runQueueRef.current.length === 0) {
            setIsRunning(false);
            setIsStreaming(false);
          }
        });
    },
    [isRunning, sendRpc],
  );

  const abortRun = useCallback(() => {
    const conversationId = conversationIdRef.current || undefined;
    const runId =
      currentRunIdRef.current ||
      (conversationId
        ? activeRunsRef.current.get(conversationId) || undefined
        : undefined);

    if (!runId && !conversationId) return;

    sendRpc("conversations.abort", {
      runId,
      conversationId,
    }).catch(() => {});
  }, [sendRpc]);

  const setDefaultAgent = useCallback(
    (agentId: string) => {
      setServerDefaultAgentId(agentId);
      sendRpc<AgentsSetDefaultResult>("agents.setDefault", { agentId })
        .then((result) => {
          setServerDefaultAgentId(result.defaultAgentId);
          // Update the agent's defaultConversationId in the agents list.
          setAgents((previous) =>
            previous.map((agent) =>
              agent.id === result.defaultAgentId
                ? {
                    ...agent,
                    defaultConversationId: result.defaultConversationId,
                  }
                : agent,
            ),
          );
        })
        .catch((error: unknown) => console.error("agents.setDefault:", error));
    },
    [sendRpc],
  );

  const setDefaultConversation = useCallback(
    (agentId: string, conversationId: string) => {
      // Optimistic update.
      setAgents((previous) =>
        previous.map((agent) =>
          agent.id === agentId
            ? { ...agent, defaultConversationId: conversationId }
            : agent,
        ),
      );
      sendRpc<ConversationsSetDefaultResult>("conversations.setDefault", {
        agentId,
        conversationId,
      }).catch((error: unknown) =>
        console.error("conversations.setDefault:", error),
      );
    },
    [sendRpc],
  );

  const refreshAgents = useCallback(() => {
    sendRpc<AgentsConfigListResult>("agents.config.list", {})
      .then((result) => {
        if (result.agents) {
          setAgents((previous) =>
            result.agents.map((agent) => {
              const existing = previous.find((entry) => entry.id === agent.id);
              return {
                id: agent.id,
                name: agent.name,
                avatarMediaId: agent.avatarMediaId,
                defaultConversationId: existing?.defaultConversationId,
              };
            }),
          );
        }
      })
      .catch((error: unknown) => console.error("agents.config.list:", error));
  }, [sendRpc]);

  // ── Jobs ────────────────────────────────────────────────────────────

  const [jobs, setJobs] = useState<Job[]>([]);
  const [jobsLoading, setJobsLoading] = useState(false);

  const loadJobs = useCallback(() => {
    setJobsLoading(true);
    sendRpc<JobsListResult>("jobs.list", {})
      .then((result) => setJobs(result.jobs || []))
      .catch((error) => console.error("jobs.list:", error))
      .finally(() => setJobsLoading(false));
  }, [sendRpc]);

  const createJob = useCallback(
    (params: JobCreateParams) => {
      return sendRpc<{ job: Job }>("jobs.create", { job: params })
        .then((result) => {
          loadJobs();
          return result.job;
        })
        .catch((error) => {
          console.error("jobs.create:", error);
          throw error;
        });
    },
    [sendRpc, loadJobs],
  );

  const updateJob = useCallback(
    (params: JobUpdateParams) => {
      return sendRpc<{ job: Job }>("jobs.update", { job: params })
        .then(() => {
          loadJobs();
        })
        .catch((error) => {
          console.error("jobs.update:", error);
          throw error;
        });
    },
    [sendRpc, loadJobs],
  );

  const deleteJob = useCallback(
    (id: string) => {
      return sendRpc("jobs.delete", { id })
        .then(() => {
          loadJobs();
        })
        .catch((error) => {
          console.error("jobs.delete:", error);
          throw error;
        });
    },
    [sendRpc, loadJobs],
  );

  const triggerJob = useCallback(
    (id: string): Promise<void> => {
      return sendRpc("jobs.trigger", { id })
        .then(() => {})
        .catch((error) => {
          console.error("jobs.trigger:", error);
          throw error;
        });
    },
    [sendRpc],
  );

  const sendVoiceMessage = useCallback(
    (text: string, model?: string, voiceMode?: "call" | "input") => {
      lastSentViaMicRef.current = true;
      sendMessage(text, model, undefined, voiceMode ?? "input");
    },
    [sendMessage],
  );

  const markTypedSend = useCallback(() => {
    lastSentViaMicRef.current = false;
  }, []);

  // ── Conversation Todos ──────────────────────────────────────────────

  const [todos, setTodos] = useState<Todo[]>([]);
  const todosConversationIdRef = useRef<string | null>(null);

  const loadTodos = useCallback(
    (targetConversationId?: string) => {
      const convId = targetConversationId || conversationIdRef.current;
      if (!convId) {
        setTodos([]);
        return;
      }
      todosConversationIdRef.current = convId;
      sendRpc<ConversationTodosListResult>("conversations.todos.list", {
        conversationId: convId,
      })
        .then((result) => {
          if (todosConversationIdRef.current !== convId) return;
          setTodos(result.todos || []);
        })
        .catch((error) => console.error("conversations.todos.list:", error));
    },
    [sendRpc],
  );

  // ── Pending Questions (ask_user_question tool) ────────────────────

  const [pendingQuestions, setPendingQuestions] = useState<PendingQuestion[]>(
    [],
  );

  const answerQuestion = useCallback(
    async (
      answers: { questionId: string; answer: string; other?: string }[],
    ) => {
      await sendRpc("questions.answer", { answers });
      const answeredIds = new Set(answers.map((a) => a.questionId));
      setPendingQuestions((prev) => prev.filter((q) => !answeredIds.has(q.id)));
    },
    [sendRpc],
  );

  const loadPendingQuestions = useCallback(
    (targetConversationId?: string) => {
      const convId = targetConversationId || conversationIdRef.current;
      if (!convId) {
        setPendingQuestions([]);
        return;
      }
      sendRpc<PendingQuestionsListResult>("questions.list", {
        conversationId: convId,
      })
        .then((result) => {
          if (conversationIdRef.current !== convId) return;
          setPendingQuestions(result?.questions ?? []);
        })
        .catch((error) => console.error("questions.list:", error));
    },
    [sendRpc],
  );

  // Rehydrate pending questions whenever the WebSocket (re)connects and a
  // conversation is active.  The questions.list calls inside handleConnect and
  // switchConversation are nested in conversations.history .then() chains —
  // if history loading fails or runs for the wrong conversation, those calls
  // are skipped.  This standalone effect guarantees rehydration on reconnect.
  useEffect(() => {
    if (connected && conversationId) {
      loadPendingQuestions(conversationId);
    }
  }, [connected, conversationId, loadPendingQuestions]);

  return {
    conversations,
    conversationId,
    messages,
    isRunning,
    status,
    defaultProviderModelName,
    models,
    streamText,
    isStreaming,
    toolActivity,
    agents,
    currentAgentId,
    connected,
    connecting,
    hasConnectedOnce,
    isAdmin,
    currentUserId,
    currentRunId: currentRunIdRef.current,
    conversationModel,
    serverDefaultAgentId,
    audioCapability,
    lastSentViaMicRef,
    setCurrentAgentId,
    setDefaultAgent,
    setDefaultConversation,
    sendMessage,
    sendVoiceMessage,
    markTypedSend,
    abortRun,
    switchConversation,
    newConversation,
    deleteConversation,
    loadConversations,
    refreshAgents,
    sendRpc,
    sendBinary,
    onBinaryMessage,
    onVoiceMessage,
    hasMoreHistory,
    loadingOlderMessages,
    loadOlderMessages,
    jobs,
    jobsLoading,
    loadJobs,
    createJob,
    updateJob,
    deleteJob,
    triggerJob,
    todos,
    loadTodos,
    pendingQuestions,
    answerQuestion,
    loadPendingQuestions,
    lastActiveRunState,
  };
}
