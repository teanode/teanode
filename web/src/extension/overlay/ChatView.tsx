import React, { useState, useEffect, useRef, useCallback } from "react";
import { useTranslation } from "react-i18next";
import Box from "@mui/material/Box";
import Chip from "@mui/material/Chip";
import IconButton from "@mui/material/IconButton";
import Typography from "@mui/material/Typography";
import AttachFileRounded from "@mui/icons-material/AttachFileRounded";
import SendRounded from "@mui/icons-material/SendRounded";
import MessageList from "../../components/MessageList";
import { sendRpc, onEvent, getBaseUrl } from "./rpc";
import type { RpcEventFrame } from "../shared/types";
import type {
  Attachment,
  DisplayMessage,
  ToolCall,
  Message,
  Usage,
  PendingQuestion,
  PendingQuestionsListResult,
  ConversationQuestionsEvent,
  Todo,
  ConversationTodosEvent,
  ConversationTodosListResult,
} from "../../types";
import { normalizeContent } from "../../contentUtils";
import QuestionPanel from "../../components/QuestionPanel";
import TodoPanel from "../../components/TodoPanel";

interface PendingFile {
  file: File;
  previewUrl?: string;
}

interface Props {
  agentId: string;
  conversationId: string;
  onConversationCreated?: (id: string) => void;
  agentAvatarMediaId?: string;
  agentName?: string;
  userAvatarMediaId?: string;
  userName?: string;
  showToolCalls?: boolean;
  showTokenUsage?: boolean;
}

function isImageFile(file: File): boolean {
  return file.type.startsWith("image/");
}

async function uploadMedia(file: File): Promise<Attachment> {
  const { url, token } = await getBaseUrl();
  const base = url.replace(/\/+$/, "");
  const formData = new FormData();
  formData.append("file", file);
  const headers: Record<string, string> = {};
  if (token) headers["Authorization"] = `Bearer ${token}`;
  const response = await fetch(`${base}/api/v1/media/upload`, {
    method: "POST",
    body: formData,
    headers,
  });
  if (!response.ok) throw new Error(`Upload failed: ${response.status}`);
  return response.json();
}

function compactNumber(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
  return String(n);
}

function formatUsageText(usage: Record<string, unknown>): string {
  const input = (usage.input ?? usage.Input ?? 0) as number;
  const output = (usage.output ?? usage.Output ?? 0) as number;
  const total = (usage.total ?? usage.Total ?? input + output) as number;
  if (!total) return "";
  return `${compactNumber(input)} in / ${compactNumber(output)} out \u00b7 ${compactNumber(total)} tokens`;
}

// --- DisplayMessage helpers (mirrors useBackend patterns) ---

let messageIdCounter = 0;
function nextMessageId(): string {
  return `ext-msg-${++messageIdCounter}`;
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

function convertHistory(messages: Message[]): DisplayMessage[] {
  const displayMessages: DisplayMessage[] = [];
  for (const message of messages) {
    const extracted = normalizeContent(message.content);
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
          displayMessages.push({
            id: nextMessageId(),
            type: "usage",
            content: `${usageNumbers.input} in / ${usageNumbers.output} out \u00b7 ${usageNumbers.total} tokens`,
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
      if (message.toolCallId) {
        let inserted = false;
        for (let index = displayMessages.length - 1; index >= 0; index--) {
          if (
            displayMessages[index].type === "tool-invoke" &&
            displayMessages[index].toolCallId === message.toolCallId
          ) {
            displayMessages.splice(index + 1, 0, toolResult);
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

/** Find the last assistant message for a given runId. */
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

export function ChatView({
  agentId,
  conversationId,
  onConversationCreated,
  agentAvatarMediaId,
  agentName,
  userAvatarMediaId,
  userName,
  showToolCalls = false,
  showTokenUsage = false,
}: Props) {
  const [messages, setMessages] = useState<DisplayMessage[]>([]);
  const [input, setInput] = useState("");
  const [streaming, setStreaming] = useState(false);
  const [streamingText, setStreamingText] = useState("");
  const [isRunning, setIsRunning] = useState(false);
  const [toolActivity, setToolActivity] = useState<string | null>(null);
  const [pendingFiles, setPendingFiles] = useState<PendingFile[]>([]);
  const [uploading, setUploading] = useState(false);
  const [activeRunId, setActiveRunId] = useState<string | null>(null);
  const [pendingQuestions, setPendingQuestions] = useState<PendingQuestion[]>(
    [],
  );
  const [todos, setTodos] = useState<Todo[]>([]);
  const [todosPanelCollapsed, setTodosPanelCollapsed] = useState(false);

  // Keep refs for event handler closures.
  const streamingTextRef = useRef("");
  const activeRunIdRef = useRef<string | null>(null);
  const afterToolCallsRef = useRef(false);

  useEffect(() => {
    streamingTextRef.current = streamingText;
  }, [streamingText]);
  useEffect(() => {
    activeRunIdRef.current = activeRunId;
  }, [activeRunId]);

  // Resolve backend base URL + token so avatar media URLs work in the
  // extension context (chrome-extension:// origin can't use relative paths).
  const [baseInfo, setBaseInfo] = useState<{
    url: string;
    token: string;
  } | null>(null);
  useEffect(() => {
    getBaseUrl().then(setBaseInfo);
  }, []);

  function resolveMediaUrl(mediaId: string | undefined): string | undefined {
    if (!mediaId || !baseInfo) return undefined;
    return resolveMediaId(mediaId);
  }

  function resolveMediaId(mediaId: string): string {
    const base = (baseInfo?.url || "").replace(/\/+$/, "");
    let url = `${base}/api/v1/media/${mediaId}`;
    if (baseInfo?.token) url += `?token=${encodeURIComponent(baseInfo.token)}`;
    return url;
  }

  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);

  // Track the active conversation id via ref so event handlers always see the
  // latest value, even before the parent re-renders with a new prop.
  const activeConvIdRef = useRef(conversationId);
  useEffect(() => {
    activeConvIdRef.current = conversationId;
  }, [conversationId]);

  // When a new conversation is created via handleSend, skip the next history
  // load triggered by the conversationId prop change (optimistic messages are
  // already in state).
  const skipNextHistoryLoadRef = useRef(false);

  // Helper to finish a run.
  function finishCurrentRun() {
    streamingTextRef.current = "";
    afterToolCallsRef.current = false;
    setStreamingText("");
    setStreaming(false);
    setToolActivity(null);
    setActiveRunId(null);
    activeRunIdRef.current = null;
    setIsRunning(false);
  }

  const answerQuestion = useCallback(
    async (
      answers: { questionId: string; answer: string; other?: string }[],
    ) => {
      await sendRpc("questions.answer", { answers });
      const answeredIds = new Set(answers.map((a) => a.questionId));
      setPendingQuestions((prev) => prev.filter((q) => !answeredIds.has(q.id)));
    },
    [],
  );

  // Load history on conversation change.
  useEffect(() => {
    if (!conversationId) {
      setMessages([]);
      setIsRunning(false);
      setToolActivity(null);
      setActiveRunId(null);
      activeRunIdRef.current = null;
      setPendingQuestions([]);
      setTodos([]);
      return;
    }
    if (skipNextHistoryLoadRef.current) {
      skipNextHistoryLoadRef.current = false;
      return;
    }
    setMessages([]);
    setStreamingText("");
    streamingTextRef.current = "";
    setIsRunning(false);
    setToolActivity(null);
    setActiveRunId(null);
    activeRunIdRef.current = null;
    afterToolCallsRef.current = false;
    sendRpc("conversations.history", { conversationId })
      .then((payload) => {
        const data = payload as {
          messages?: Message[];
          activeRunId?: string;
          activeRunState?: { phase: string; toolName?: string };
        };
        if (data.messages) {
          setMessages(convertHistory(data.messages));
        }
        // Restore busy state from server if a run is active.
        if (data.activeRunId) {
          setIsRunning(true);
          setActiveRunId(data.activeRunId);
          activeRunIdRef.current = data.activeRunId;
          if (data.activeRunState?.phase === "tool") {
            setToolActivity(data.activeRunState.toolName || null);
          }
          // Add an empty assistant placeholder for the active run so
          // MessageList shows the thinking spinner.
          setMessages((prev) => {
            const alreadyHasPlaceholder = prev.some(
              (message) =>
                message.type === "assistant" &&
                message.runId === data.activeRunId,
            );
            if (alreadyHasPlaceholder) return prev;
            return [
              ...prev,
              {
                id: nextMessageId(),
                type: "assistant",
                content: "",
                runId: data.activeRunId,
              },
            ];
          });
        }
        // Load todos for this conversation.
        sendRpc("conversations.todos.list", { conversationId })
          .then((result) => {
            const todosData = result as ConversationTodosListResult;
            if (activeConvIdRef.current === conversationId) {
              setTodos(todosData.todos || []);
            }
          })
          .catch(() => {});

        // Load pending questions for this conversation.
        sendRpc("questions.list", { conversationId })
          .then((result) => {
            const qData = result as PendingQuestionsListResult;
            if (activeConvIdRef.current === conversationId) {
              setPendingQuestions(qData?.questions ?? []);
            }
          })
          .catch(() => {});
      })
      .catch(() => {});
  }, [conversationId]);

  // Subscribe to conversation events.
  useEffect(() => {
    return onEvent((frame: RpcEventFrame) => {
      if (frame.event !== "conversation") return;
      const payload = frame.payload as Record<string, unknown>;
      if (
        !activeConvIdRef.current ||
        payload.conversationId !== activeConvIdRef.current
      )
        return;

      const eventRunId = (payload.runId as string) || null;

      switch (payload.state) {
        case "queued":
          setIsRunning(true);
          setToolActivity(null);
          if (eventRunId) {
            setActiveRunId(eventRunId);
            activeRunIdRef.current = eventRunId;
          }
          break;

        case "delta": {
          setIsRunning(true);
          setToolActivity(null);

          if (afterToolCallsRef.current) {
            // New LLM round after tool calls — finalize old text and start fresh.
            const previousText = streamingTextRef.current;
            if (previousText) {
              setMessages((prev) => {
                const updated = [...prev];
                const assistantIndex = findRunAssistantIndex(
                  updated,
                  activeRunIdRef.current,
                );
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
                  runId: activeRunIdRef.current || undefined,
                };
                updated.splice(assistantIndex + 1, 0, newAssistant);
                return updated;
              });
            } else {
              setMessages((prev) => {
                const updated = [...prev];
                const assistantIndex = findRunAssistantIndex(
                  updated,
                  activeRunIdRef.current,
                );
                if (
                  assistantIndex >= 0 &&
                  updated[assistantIndex].type === "assistant" &&
                  !updated[assistantIndex].content
                ) {
                  // Reuse existing empty placeholder
                } else {
                  const newAssistant: DisplayMessage = {
                    id: nextMessageId(),
                    type: "assistant",
                    content: "",
                    runId: activeRunIdRef.current || undefined,
                  };
                  updated.splice(assistantIndex + 1, 0, newAssistant);
                }
                return updated;
              });
            }
            streamingTextRef.current = "";
            setStreamingText("");
            afterToolCallsRef.current = false;
          }

          streamingTextRef.current += (payload.text as string) || "";
          setStreamingText(streamingTextRef.current);
          setStreaming(true);
          break;
        }

        case "text_done": {
          // Text streaming ended; tool calls will follow. Commit streamed
          // text and show the thinking spinner during the gap.
          const textDoneAccumulated = streamingTextRef.current;
          streamingTextRef.current = "";
          setStreamingText("");
          setStreaming(false);
          if (textDoneAccumulated) {
            setMessages((prev) => {
              const updated = [...prev];
              const assistantIndex = findRunAssistantIndex(
                updated,
                activeRunIdRef.current,
              );
              if (
                assistantIndex >= 0 &&
                updated[assistantIndex].type === "assistant"
              ) {
                updated[assistantIndex] = {
                  ...updated[assistantIndex],
                  content: textDoneAccumulated,
                };
                const newTail: DisplayMessage = {
                  id: nextMessageId(),
                  type: "assistant",
                  content: "",
                  runId: activeRunIdRef.current || undefined,
                };
                updated.splice(assistantIndex + 1, 0, newTail);
              }
              return updated;
            });
          }
          setToolActivity(null);
          break;
        }

        case "tool_call": {
          afterToolCallsRef.current = true;
          setIsRunning(true);
          setStreaming(false);
          setToolActivity((payload.toolName as string) || null);

          const accumulatedText = streamingTextRef.current;
          streamingTextRef.current = "";
          setStreamingText("");

          setMessages((prev) => {
            const updated = [...prev];
            const assistantIndex = findRunAssistantIndex(
              updated,
              activeRunIdRef.current,
            );
            const toolMessage: DisplayMessage = {
              id: nextMessageId(),
              type: "tool-invoke",
              content: (payload.arguments as string) || "{}",
              toolName: payload.toolName as string,
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
                runId: activeRunIdRef.current || undefined,
              };
              updated.splice(assistantIndex + 2, 0, newTail);
            } else {
              updated.splice(assistantIndex, 0, toolMessage);
            }
            return updated;
          });
          break;
        }

        case "tool_result":
          setToolActivity(null);
          setMessages((prev) => {
            const updated = [...prev];
            const assistantIndex = findRunAssistantIndex(
              updated,
              activeRunIdRef.current,
            );
            const toolMessage: DisplayMessage = {
              id: nextMessageId(),
              type: "tool-result",
              content: ((payload.result as string) || "").slice(0, 500),
              toolName: payload.toolName as string,
              timestamp: Date.now(),
            };
            let insertPosition = assistantIndex;
            for (let index = assistantIndex - 1; index >= 0; index--) {
              if (updated[index].type === "tool-invoke") {
                insertPosition = index + 1;
                break;
              }
            }
            updated.splice(insertPosition, 0, toolMessage);
            return updated;
          });
          break;

        case "final": {
          const finalTimestamp = Date.now();
          const capturedStreamText = streamingTextRef.current;
          setMessages((prev) => {
            const updated = [...prev];
            const assistantIndex = findRunAssistantIndex(
              updated,
              activeRunIdRef.current,
            );
            const hasToolSplits = updated.some(
              (message, index) =>
                index !== assistantIndex &&
                message.type === "assistant" &&
                message.runId === activeRunIdRef.current,
            );
            const finalText = hasToolSplits
              ? capturedStreamText
              : (payload.text as string) || capturedStreamText;
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
                updated[assistantIndex] = {
                  ...updated[assistantIndex],
                  timestamp: finalTimestamp,
                };
              } else {
                updated.splice(assistantIndex, 1);
              }
            }
            // Add usage indicator.
            const usage = payload.usage as Record<string, unknown> | undefined;
            if (usage) {
              const usageText = formatUsageText(usage);
              if (usageText) {
                const insertPosition = finalText
                  ? assistantIndex + 1
                  : assistantIndex;
                updated.splice(insertPosition, 0, {
                  id: nextMessageId(),
                  type: "usage",
                  content: usageText,
                  timestamp: finalTimestamp,
                });
              }
            }
            return updated;
          });
          finishCurrentRun();
          break;
        }

        case "error": {
          const capturedText = streamingTextRef.current;
          setMessages((prev) => {
            const updated = [...prev];
            const assistantIndex = findRunAssistantIndex(
              updated,
              activeRunIdRef.current,
            );
            if (
              assistantIndex >= 0 &&
              updated[assistantIndex].type === "assistant"
            ) {
              if (capturedText) {
                updated[assistantIndex] = {
                  ...updated[assistantIndex],
                  content: capturedText,
                };
              } else {
                updated[assistantIndex] = {
                  ...updated[assistantIndex],
                  content: `__error__:${(payload.error as string) || "Unknown error"}`,
                };
              }
            }
            return updated;
          });
          finishCurrentRun();
          break;
        }

        case "aborted": {
          const capturedAbortText = streamingTextRef.current;
          setMessages((prev) => {
            const updated = [...prev];
            const assistantIndex = findRunAssistantIndex(
              updated,
              activeRunIdRef.current,
            );
            if (
              assistantIndex >= 0 &&
              updated[assistantIndex].type === "assistant"
            ) {
              if (capturedAbortText) {
                updated[assistantIndex] = {
                  ...updated[assistantIndex],
                  content: capturedAbortText,
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
          break;
        }
      }
    });
  }, []);

  // Subscribe to question and todo events.
  useEffect(() => {
    return onEvent((frame: RpcEventFrame) => {
      if (frame.event === "conversationQuestions") {
        const payload = frame.payload as ConversationQuestionsEvent | undefined;
        if (!payload) return;
        if (
          payload.action === "asked" &&
          payload.conversationId === activeConvIdRef.current
        ) {
          setPendingQuestions((prev) => {
            if (prev.some((q) => q.id === payload.questionId)) return prev;
            const q: PendingQuestion = {
              id: payload.questionId!,
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
        } else if (payload.action === "answered") {
          setPendingQuestions((prev) =>
            prev.filter((q) => q.id !== payload.questionId),
          );
        }
      }

      if (frame.event === "conversationTodos") {
        const payload = frame.payload as ConversationTodosEvent | undefined;
        if (payload && payload.conversationId === activeConvIdRef.current) {
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
            // Full reload on prune.
            const convId = activeConvIdRef.current;
            if (convId) {
              sendRpc("conversations.todos.list", { conversationId: convId })
                .then((result) => {
                  const todosData = result as ConversationTodosListResult;
                  if (activeConvIdRef.current === convId) {
                    setTodos(todosData.todos || []);
                  }
                })
                .catch(() => {});
            }
          }
        }
      }
    });
  }, []);

  // Clean up file preview URLs.
  useEffect(() => {
    return () => {
      pendingFiles.forEach((pendingFile) => {
        if (pendingFile.previewUrl) URL.revokeObjectURL(pendingFile.previewUrl);
      });
    };
  }, [pendingFiles]);

  const addFiles = useCallback((files: FileList | File[]) => {
    const newFiles: PendingFile[] = Array.from(files).map((file) => ({
      file,
      previewUrl: isImageFile(file) ? URL.createObjectURL(file) : undefined,
    }));
    setPendingFiles((prev) => [...prev, ...newFiles]);
  }, []);

  const removeFile = useCallback((index: number) => {
    setPendingFiles((prev) => {
      const removed = prev[index];
      if (removed?.previewUrl) URL.revokeObjectURL(removed.previewUrl);
      return prev.filter((_, filterIndex) => filterIndex !== index);
    });
  }, []);

  const handleSend = useCallback(async () => {
    const text = input.trim();
    if ((!text && pendingFiles.length === 0) || streaming) return;

    setInput("");
    if (textareaRef.current) {
      textareaRef.current.style.height = "auto";
    }

    let attachments: Attachment[] | undefined;
    if (pendingFiles.length > 0) {
      setUploading(true);
      try {
        attachments = await Promise.all(
          pendingFiles.map((pendingFile) => uploadMedia(pendingFile.file)),
        );
      } catch (error) {
        console.error("File upload failed:", error);
        setUploading(false);
        return;
      }
      pendingFiles.forEach((pendingFile) => {
        if (pendingFile.previewUrl) URL.revokeObjectURL(pendingFile.previewUrl);
      });
      setPendingFiles([]);
      setUploading(false);
    }

    const userMessageId = nextMessageId();
    const assistantPlaceholderId = nextMessageId();

    setMessages((prev) => [
      ...prev,
      {
        id: userMessageId,
        type: "user",
        content: text,
        attachments,
        timestamp: Date.now(),
      },
      {
        id: assistantPlaceholderId,
        type: "assistant",
        content: "",
        // runId will be set when we get the queued/delta event
      },
    ]);
    setIsRunning(true);
    setToolActivity(null);

    try {
      const result = (await sendRpc("conversations.send", {
        conversationId: conversationId || "",
        agentId,
        message: text,
        attachments,
        origin: "webui",
      })) as { conversationId?: string; runId?: string } | undefined;

      // Tag the assistant placeholder with the runId from the response.
      if (result?.runId) {
        const runId = result.runId;
        setActiveRunId(runId);
        activeRunIdRef.current = runId;
        setMessages((prev) =>
          prev.map((message) =>
            message.id === assistantPlaceholderId
              ? { ...message, runId }
              : message,
          ),
        );
      }

      // When sending without a conversationId, the server creates a new one.
      if (!conversationId && result?.conversationId) {
        activeConvIdRef.current = result.conversationId;
        skipNextHistoryLoadRef.current = true;
        onConversationCreated?.(result.conversationId);
      }
    } catch {
      // Error will come via event.
    }
  }, [
    input,
    conversationId,
    agentId,
    streaming,
    pendingFiles,
    onConversationCreated,
  ]);

  const handleKeyDown = useCallback(
    (event: React.KeyboardEvent) => {
      if (event.key === "Enter" && !event.shiftKey) {
        event.preventDefault();
        handleSend();
      }
    },
    [handleSend],
  );

  const handleInput = useCallback(() => {
    const element = textareaRef.current;
    if (!element) return;
    element.style.height = "auto";
    element.style.height = Math.min(element.scrollHeight, 120) + "px";
  }, []);

  const handleFileChange = useCallback(
    (event: React.ChangeEvent<HTMLInputElement>) => {
      if (event.target.files && event.target.files.length > 0) {
        addFiles(event.target.files);
        event.target.value = "";
      }
    },
    [addFiles],
  );

  const handleDrop = useCallback(
    (event: React.DragEvent) => {
      event.preventDefault();
      if (event.dataTransfer.files.length > 0)
        addFiles(event.dataTransfer.files);
    },
    [addFiles],
  );

  const handlePaste = useCallback(
    (event: React.ClipboardEvent<HTMLTextAreaElement>) => {
      const items = event.clipboardData.items;
      const images: File[] = [];
      for (let index = 0; index < items.length; index++) {
        if (items[index].type.startsWith("image/")) {
          const file = items[index].getAsFile();
          if (file) images.push(file);
        }
      }
      if (images.length > 0) {
        event.preventDefault();
        addFiles(images);
      }
    },
    [addFiles],
  );

  const { t } = useTranslation();
  const hasContent = !!input.trim() || pendingFiles.length > 0;

  return (
    <Box
      sx={{
        flex: 1,
        display: "flex",
        flexDirection: "column",
        overflow: "hidden",
      }}
    >
      {/* Messages — virtualized via shared MessageList */}
      <MessageList
        messages={messages}
        isRunning={isRunning}
        isStreaming={streaming}
        streamText={streamingText}
        toolActivity={toolActivity}
        status={isRunning ? "thinking..." : "connected"}
        activeRunId={activeRunId}
        showToolCalls={showToolCalls}
        showTokenUsage={showTokenUsage}
        agentName={agentName}
        agentAvatarMediaId={agentAvatarMediaId}
        agentAvatarSrc={resolveMediaUrl(agentAvatarMediaId)}
        userName={userName}
        userAvatarMediaId={userAvatarMediaId}
        userAvatarSrc={resolveMediaUrl(userAvatarMediaId)}
        resolveMediaUrl={resolveMediaId}
      />
      <TodoPanel
        todos={todos}
        collapsed={todosPanelCollapsed}
        onToggleCollapsed={setTodosPanelCollapsed}
      />

      {/* Question panel or input area */}
      {pendingQuestions.length > 0 ? (
        <QuestionPanel
          questions={pendingQuestions}
          onSubmitAll={answerQuestion}
        />
      ) : (
        <Box
          sx={{ px: 1, py: 1, borderTop: 1, borderColor: "divider" }}
          onDragOver={(event: React.DragEvent) => event.preventDefault()}
          onDrop={handleDrop}
        >
          {/* Pending files chips */}
          {pendingFiles.length > 0 && (
            <Box sx={{ display: "flex", gap: 0.5, flexWrap: "wrap", mb: 0.5 }}>
              {pendingFiles.map((pendingFile, index) => (
                <Chip
                  key={index}
                  label={pendingFile.file.name}
                  size="small"
                  onDelete={() => removeFile(index)}
                  avatar={
                    pendingFile.previewUrl ? (
                      <Box
                        component="img"
                        src={pendingFile.previewUrl}
                        sx={{
                          width: 20,
                          height: 20,
                          borderRadius: "50%",
                          objectFit: "cover",
                        }}
                      />
                    ) : undefined
                  }
                  sx={{ maxWidth: 180, fontSize: 11 }}
                />
              ))}
            </Box>
          )}
          <Box
            sx={{
              display: "flex",
              alignItems: "flex-end",
              gap: 0.5,
              bgcolor: "surface2",
              borderRadius: 1.5,
              border: 1,
              borderColor: "divider",
              px: 1,
              py: 0.5,
              "&:focus-within": { borderColor: "primary.main" },
            }}
          >
            <Box
              component="textarea"
              ref={textareaRef}
              value={input}
              onChange={(event: React.ChangeEvent<HTMLTextAreaElement>) =>
                setInput(event.target.value)
              }
              onKeyDown={handleKeyDown}
              onInput={handleInput}
              onPaste={handlePaste}
              placeholder={
                conversationId
                  ? "Type a message..."
                  : "Start a new conversation..."
              }
              disabled={streaming}
              rows={1}
              sx={{
                flex: 1,
                border: "none",
                outline: "none",
                bgcolor: "transparent",
                color: "text.primary",
                fontSize: "0.8125rem",
                fontFamily: "inherit",
                lineHeight: 1.5,
                resize: "none",
                overflow: "auto",
                py: 0.5,
                "&::placeholder": { color: "text.secondary", opacity: 1 },
              }}
            />
            <input
              type="file"
              ref={fileInputRef}
              multiple
              onChange={handleFileChange}
              style={{ display: "none" }}
            />
            <IconButton
              size="small"
              onClick={() => fileInputRef.current?.click()}
              disabled={streaming}
              sx={{
                width: 28,
                height: 28,
                color: "text.secondary",
                "&:hover": { color: "primary.main" },
              }}
            >
              <AttachFileRounded sx={{ fontSize: 16 }} />
            </IconButton>
            <IconButton
              size="small"
              color="primary"
              onClick={handleSend}
              disabled={uploading || streaming || !hasContent}
              sx={{ width: 28, height: 28 }}
            >
              <SendRounded sx={{ fontSize: 16 }} />
            </IconButton>
          </Box>
          {uploading && (
            <Typography
              variant="caption"
              color="text.secondary"
              sx={{ mt: 0.5, display: "block" }}
            >
              Uploading...
            </Typography>
          )}
        </Box>
      )}
    </Box>
  );
}
