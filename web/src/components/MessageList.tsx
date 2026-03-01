import React, {
  useState,
  useEffect,
  useRef,
  useMemo,
  useCallback,
} from "react";
import { useTranslation } from "react-i18next";
import { Virtuoso, VirtuosoHandle } from "react-virtuoso";
import Box from "@mui/material/Box";
import Container from "@mui/material/Container";
import Divider from "@mui/material/Divider";
import Paper from "@mui/material/Paper";
import Typography from "@mui/material/Typography";
import CircularProgress from "@mui/material/CircularProgress";
import IconButton from "@mui/material/IconButton";
import HourglassEmptyRounded from "@mui/icons-material/HourglassEmptyRounded";
import KeyboardArrowDownRounded from "@mui/icons-material/KeyboardArrowDownRounded";
import StopRounded from "@mui/icons-material/StopRounded";
import type { DisplayMessage } from "../types";
import { useAppContext } from "../context";
import MessageBubble from "./MessageBubble";
import ToolInvoke from "./ToolInvoke";
import ToolResult, { detectMedia } from "./ToolResult";
import UsageIndicator from "./UsageIndicator";
import ConversationAvatar from "./ConversationAvatar";

interface MessageListProps {
  messages: DisplayMessage[];
  isRunning: boolean;
  isStreaming: boolean;
  streamText: string;
  toolActivity: string | null;
  status: string;
  activeRunId: string | null;
  hasMoreHistory?: boolean;
  loadingOlderMessages?: boolean;
  onLoadOlderMessages?: () => void;
  agentName?: string;
  agentAvatarMediaId?: string;
  userName?: string;
  userAvatarMediaId?: string;
  voiceEnabled?: boolean;
  speakingMessageId?: string | null;
  onSpeak?: (messageId: string, text: string) => void;
  onStopSpeaking?: () => void;
  showAbortOnStatusLine?: boolean;
  onAbort?: () => void;
}

const VIRTUAL_START = 1_000_000;

type ListItem =
  | { kind: "separator"; label: string; key: string }
  | { kind: "message"; message: DisplayMessage };

function dateLabelFor(timestamp: number, t: (key: string) => string): string {
  const messageDate = new Date(timestamp);
  const now = new Date();

  const messageDay = new Date(
    messageDate.getFullYear(),
    messageDate.getMonth(),
    messageDate.getDate(),
  );
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  const diff = today.getTime() - messageDay.getTime();

  if (diff === 0) return t("conversations.today");
  if (diff === 86_400_000) return t("conversations.yesterday");
  return messageDate.toLocaleDateString([], {
    weekday: "short",
    month: "short",
    day: "numeric",
    year:
      messageDate.getFullYear() !== now.getFullYear() ? "numeric" : undefined,
  });
}

function buildItems(
  messages: DisplayMessage[],
  t: (key: string) => string,
  showToolCalls: boolean,
  showTokenUsage: boolean,
): ListItem[] {
  const items: ListItem[] = [];
  let lastDateLabel = "";

  for (const message of messages) {
    if (message.type === "tool-invoke" && !showToolCalls) continue;
    if (
      message.type === "tool-result" &&
      !showToolCalls &&
      detectMedia(message.content) === null
    )
      continue;
    if (message.type === "usage" && !showTokenUsage) continue;

    if (
      message.timestamp &&
      (message.type === "user" || message.type === "assistant")
    ) {
      const label = dateLabelFor(message.timestamp, t);
      if (label !== lastDateLabel) {
        lastDateLabel = label;
        items.push({ kind: "separator", label, key: `sep-${message.id}` });
      }
    }

    items.push({ kind: "message", message });
  }

  return items;
}

export default function MessageList({
  messages,
  isRunning,
  isStreaming,
  streamText,
  toolActivity,
  status,
  activeRunId,
  hasMoreHistory,
  loadingOlderMessages,
  onLoadOlderMessages,
  agentName,
  agentAvatarMediaId,
  userName,
  userAvatarMediaId,
  voiceEnabled,
  speakingMessageId,
  onSpeak,
  onStopSpeaking,
  showAbortOnStatusLine,
  onAbort,
}: MessageListProps) {
  const { t } = useTranslation();
  const { showToolCalls, showTokenUsage } = useAppContext();
  const virtuosoRef = useRef<VirtuosoHandle>(null);
  const atBottomRef = useRef(true);
  const [showScrollToBottom, setShowScrollToBottom] = useState(false);

  const normalizedUserFallback = (userName || "You").trim() || "You";
  const normalizedAgentFallback = (agentName || "Agent").trim() || "Agent";

  const items = useMemo(() => {
    const filteredItems = buildItems(
      messages,
      t,
      showToolCalls,
      showTokenUsage,
    );
    const hasVisibleMessage = filteredItems.some(
      (item) => item.kind === "message",
    );
    if (hasVisibleMessage || messages.length === 0) {
      return filteredItems;
    }

    // If filters hide everything (e.g. a conversation that starts with tool
    // messages), show the raw timeline so the page never appears empty.
    return buildItems(messages, t, true, true);
  }, [messages, t, showToolCalls, showTokenUsage]);

  // Only the last assistant message for the active run should show streaming
  // text.  Earlier assistant messages (from before tool call boundaries) have
  // their content committed and must not be overwritten by the current stream.
  const lastStreamingAssistantId = useMemo(() => {
    if (!activeRunId || !isStreaming) return null;
    for (let index = messages.length - 1; index >= 0; index--) {
      if (
        messages[index].type === "assistant" &&
        messages[index].runId === activeRunId
      ) {
        return messages[index].id;
      }
    }
    return null;
  }, [messages, activeRunId, isStreaming]);

  const itemsLengthRef = useRef(items.length);
  itemsLengthRef.current = items.length;

  // Scroll to bottom when a conversation's history loads (items go from empty to non-empty).
  // initialTopMostItemIndex only applies on Virtuoso mount — this handles conversation switches.
  const wasEmptyRef = useRef(true);
  useEffect(() => {
    if (wasEmptyRef.current && items.length > 0 && virtuosoRef.current) {
      requestAnimationFrame(() => {
        virtuosoRef.current?.scrollToIndex({
          index: itemsLengthRef.current - 1,
          align: "end",
        });
      });
      atBottomRef.current = true;
      setShowScrollToBottom(false);
    }
    wasEmptyRef.current = items.length === 0;
  }, [items.length]);

  // Scroll to bottom when streaming text or interim transcript updates and user
  // hasn't scrolled up. Virtuoso's followOutput handles new items, but content
  // updates don't add new items.
  useEffect(() => {
    if (atBottomRef.current && virtuosoRef.current && items.length > 0) {
      virtuosoRef.current.scrollToIndex({
        index: items.length - 1,
        align: "end",
        behavior: "auto",
      });
    }
  }, [streamText]);

  const handleAtBottomStateChange = useCallback((atBottom: boolean) => {
    atBottomRef.current = atBottom;
    setShowScrollToBottom(!atBottom);
  }, []);

  const scrollToBottom = useCallback(() => {
    if (virtuosoRef.current && itemsLengthRef.current > 0) {
      virtuosoRef.current.scrollToIndex({
        index: itemsLengthRef.current - 1,
        align: "end",
        behavior: "smooth",
      });
    }
  }, []);

  function handleClick(event: React.MouseEvent) {
    const target = event.target as HTMLElement;
    const button = target.closest(".copy-btn") as HTMLButtonElement | null;
    if (!button) return;
    const block = button.closest(".code-block");
    if (!block) return;
    const code = block.querySelector("pre code");
    if (!code) return;
    const copyIcon =
      '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>';
    const checkIcon =
      '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>';
    navigator.clipboard.writeText(code.textContent || "").then(() => {
      button.innerHTML = checkIcon;
      button.classList.add("copied");
      setTimeout(() => {
        button.innerHTML = copyIcon;
        button.classList.remove("copied");
      }, 1500);
    });
  }

  const renderItem = useCallback(
    (index: number, item: ListItem) => {
      if (item.kind === "separator") {
        return (
          <Container
            maxWidth="md"
            sx={{ py: 0.5, display: "flex", flexDirection: "column" }}
          >
            <Divider sx={{ my: 1 }}>
              <Typography
                variant="caption"
                color="text.secondary"
                sx={{ fontSize: "11px", fontWeight: 500 }}
              >
                {item.label}
              </Typography>
            </Divider>
          </Container>
        );
      }

      const message = item.message;
      const isActiveRun = message.runId === activeRunId;
      const isStreamingMessage = message.id === lastStreamingAssistantId;

      if (message.type === "user") {
        return (
          <Container
            maxWidth="md"
            sx={{ py: 0.5, display: "flex", flexDirection: "column" }}
          >
            <MessageBubble
              role="user"
              content={message.content}
              timestamp={message.timestamp}
              attachments={message.attachments}
              avatarMediaId={userAvatarMediaId}
              avatarFallback={normalizedUserFallback}
            />
          </Container>
        );
      }

      if (message.type === "assistant") {
        // Active run, waiting for response — show thinking spinner.
        if (
          !message.content &&
          !isStreaming &&
          isRunning &&
          (isActiveRun || !message.runId)
        ) {
          return (
            <Container
              maxWidth="md"
              sx={{ py: 0.5, display: "flex", flexDirection: "column" }}
            >
              <Box
                sx={{
                  alignSelf: "flex-start",
                  px: 0.25,
                  py: 0.5,
                  display: "flex",
                  alignItems: "center",
                  gap: 0.75,
                  width: "100%",
                }}
              >
                <Box
                  sx={{
                    minWidth: 0,
                    display: "flex",
                    alignItems: "center",
                    gap: 0.75,
                    flex: 1,
                  }}
                >
                  <ConversationAvatar
                    avatarMediaId={agentAvatarMediaId}
                    fallback={normalizedAgentFallback}
                  />
                  <CircularProgress size={12} color="primary" />
                  <Typography
                    variant="caption"
                    color="text.secondary"
                    sx={{ fontStyle: "italic" }}
                  >
                    {toolActivity
                      ? t("conversations.callingTool", {
                          toolName: toolActivity,
                        })
                      : t("conversations.thinking")}
                  </Typography>
                </Box>
                {showAbortOnStatusLine && onAbort && (
                  <IconButton
                    size="small"
                    color="error"
                    onClick={onAbort}
                    aria-label={t("common.cancel")}
                    title={t("common.cancel")}
                    sx={{ flexShrink: 0, width: 28, height: 28 }}
                  >
                    <StopRounded sx={{ fontSize: 16 }} />
                  </IconButton>
                )}
              </Box>
            </Container>
          );
        }

        // Queued run — show queued indicator
        if (!isActiveRun && !message.content && message.runId) {
          return (
            <Container
              maxWidth="md"
              sx={{ py: 0.5, display: "flex", flexDirection: "column" }}
            >
              <Box
                sx={{
                  alignSelf: "flex-start",
                  px: 0.25,
                  py: 0.5,
                  display: "flex",
                  alignItems: "center",
                  gap: 0.75,
                }}
              >
                <ConversationAvatar
                  avatarMediaId={agentAvatarMediaId}
                  fallback={normalizedAgentFallback}
                />
                <HourglassEmptyRounded sx={{ fontSize: 12 }} color="disabled" />
                <Typography
                  variant="caption"
                  color="text.secondary"
                  sx={{ fontStyle: "italic" }}
                >
                  {t("conversations.queued")}
                </Typography>
              </Box>
            </Container>
          );
        }

        // Normal assistant message rendering
        return (
          <Container
            maxWidth="md"
            sx={{ py: 0.5, display: "flex", flexDirection: "column" }}
          >
            <MessageBubble
              role="assistant"
              content={message.content}
              isStreaming={isStreamingMessage}
              streamText={isStreamingMessage ? streamText : undefined}
              timestamp={message.timestamp}
              avatarMediaId={agentAvatarMediaId}
              avatarFallback={normalizedAgentFallback}
              voiceEnabled={voiceEnabled}
              isSpeakingThis={speakingMessageId === message.id}
              onSpeak={(text) => onSpeak?.(message.id, text)}
              onStopSpeaking={onStopSpeaking}
            />
          </Container>
        );
      }

      if (message.type === "tool-invoke") {
        return (
          <Container maxWidth="md" sx={{ py: 0.5 }}>
            <ToolInvoke
              toolName={message.toolName || "tool"}
              args={message.content}
            />
          </Container>
        );
      }

      if (message.type === "tool-result") {
        return (
          <Container maxWidth="md" sx={{ py: 0.5 }}>
            <ToolResult
              toolName={message.toolName || "tool"}
              content={message.content}
            />
          </Container>
        );
      }

      if (message.type === "usage") {
        return (
          <Container maxWidth="md" sx={{ py: 0.5 }}>
            <UsageIndicator text={message.content} />
          </Container>
        );
      }

      return <div />;
    },
    [
      activeRunId,
      agentAvatarMediaId,
      isRunning,
      isStreaming,
      lastStreamingAssistantId,
      normalizedAgentFallback,
      normalizedUserFallback,
      onSpeak,
      onStopSpeaking,
      speakingMessageId,
      streamText,
      t,
      toolActivity,
      showAbortOnStatusLine,
      onAbort,
      userAvatarMediaId,
      voiceEnabled,
    ],
  );

  const computeItemKey = useCallback((_index: number, item: ListItem) => {
    return item.kind === "separator" ? item.key : item.message.id;
  }, []);

  // Track firstItemIndex in a ref — only decreases when older messages are
  // prepended, NOT when new messages are appended at the bottom.
  const firstItemIndexRef = useRef(VIRTUAL_START);
  const prevMessagesRef = useRef(messages);
  const prevItemsRef = useRef(items);

  // Render-time detection: determine if items were prepended, appended, or reset.
  const prevMessages = prevMessagesRef.current;
  const prevItems = prevItemsRef.current;

  if (messages !== prevMessages) {
    if (messages.length === 0 || prevMessages.length === 0) {
      // Conversation cleared or fresh load from empty — reset.
      firstItemIndexRef.current = VIRTUAL_START;
    } else {
      const prevFirstId = prevMessages[0].id;
      const currentFirstId = messages[0].id;
      if (prevFirstId !== currentFirstId) {
        // First message changed — either prepend or full reload.
        // A prepend preserves existing messages; a reload replaces them.
        const prevLastId = prevMessages[prevMessages.length - 1].id;
        const isReload = !messages.some((message) => message.id === prevLastId);
        if (isReload) {
          firstItemIndexRef.current = VIRTUAL_START;
        } else if (prevItems.length > 0) {
          // Prepend — find the old first MESSAGE item (skip separators, which
          // can appear/disappear when prepended messages change the date sequence).
          let oldFirstMessageId: string | null = null;
          for (const item of prevItems) {
            if (item.kind === "message") {
              oldFirstMessageId = item.message.id;
              break;
            }
          }
          if (oldFirstMessageId) {
            for (let index = 0; index < items.length; index++) {
              if (
                items[index].kind === "message" &&
                (items[index] as { kind: "message"; message: DisplayMessage })
                  .message.id === oldFirstMessageId
              ) {
                firstItemIndexRef.current -= index;
                break;
              }
            }
          }
        }
      }
      // If firstId unchanged, items were appended — no adjustment needed.
    }
  }

  prevMessagesRef.current = messages;
  prevItemsRef.current = items;

  const firstItemIndex = firstItemIndexRef.current;

  // Track whether the user is at the top of the list.
  const atTopRef = useRef(false);

  const handleStartReached = useCallback(() => {
    atTopRef.current = true;
    if (hasMoreHistory && !loadingOlderMessages && onLoadOlderMessages) {
      onLoadOlderMessages();
    }
  }, [hasMoreHistory, loadingOlderMessages, onLoadOlderMessages]);

  // Clear atTop when the visible range moves away from the first item.
  const handleRangeChanged = useCallback(
    ({ startIndex }: { startIndex: number }) => {
      if (startIndex > firstItemIndexRef.current + 2) {
        atTopRef.current = false;
      }
    },
    [],
  );

  // Auto-load more when a finished load didn't push the user away from the top
  // (e.g. all prepended messages were filtered out because tool calls are hidden).
  useEffect(() => {
    if (
      !loadingOlderMessages &&
      atTopRef.current &&
      hasMoreHistory &&
      onLoadOlderMessages
    ) {
      onLoadOlderMessages();
    }
  }, [loadingOlderMessages, hasMoreHistory, onLoadOlderMessages]);

  const headerComponent = useCallback(() => {
    if (!loadingOlderMessages) return <div />;
    return (
      <Box sx={{ display: "flex", justifyContent: "center", py: 2 }}>
        <CircularProgress size={20} />
      </Box>
    );
  }, [loadingOlderMessages]);

  return (
    <Box
      onClick={handleClick}
      sx={{ flex: 1, minHeight: 0, position: "relative" }}
    >
      <Virtuoso
        ref={virtuosoRef}
        style={{ height: "100%" }}
        data={items}
        computeItemKey={computeItemKey}
        firstItemIndex={firstItemIndex}
        initialTopMostItemIndex={items.length > 0 ? items.length - 1 : 0}
        defaultItemHeight={56}
        followOutput="auto"
        atBottomThreshold={80}
        atBottomStateChange={handleAtBottomStateChange}
        startReached={handleStartReached}
        rangeChanged={handleRangeChanged}
        increaseViewportBy={{ top: 500, bottom: 200 }}
        itemContent={renderItem}
        components={{
          Header: headerComponent,
        }}
      />
      {showScrollToBottom && items.length > 0 && (
        <IconButton
          onClick={scrollToBottom}
          size="small"
          sx={{
            position: "absolute",
            bottom: 16,
            right: 24,
            bgcolor: "background.paper",
            boxShadow: 2,
            "&:hover": { bgcolor: "action.hover" },
            zIndex: 1,
          }}
        >
          <KeyboardArrowDownRounded />
        </IconButton>
      )}
    </Box>
  );
}
