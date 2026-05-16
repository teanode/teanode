import React, {
  useState,
  useEffect,
  useLayoutEffect,
  useRef,
  useMemo,
  useCallback,
  useSyncExternalStore,
} from "react";
import { useTranslation } from "react-i18next";
import { Virtuoso, VirtuosoHandle } from "react-virtuoso";
import { useTheme } from "@mui/material/styles";
import useMediaQuery from "@mui/material/useMediaQuery";
import Box from "@mui/material/Box";
import Container from "@mui/material/Container";
import Paper from "@mui/material/Paper";
import Typography from "@mui/material/Typography";
import CircularProgress from "@mui/material/CircularProgress";
import IconButton from "@mui/material/IconButton";
import KeyboardArrowDownRounded from "@mui/icons-material/KeyboardArrowDownRounded";
import StopRounded from "@mui/icons-material/StopRounded";
import type { DisplayMessage } from "../types";
import { dateLabelFor } from "../dateUtils";
import DateSeparator from "./DateSeparator";
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
  showToolCalls: boolean;
  showTokenUsage: boolean;
  hasMoreHistory?: boolean;
  loadingOlderMessages?: boolean;
  onLoadOlderMessages?: () => void;
  agentName?: string;
  agentAvatarMediaId?: string;
  /** Pre-resolved agent avatar URL (for extension contexts). */
  agentAvatarSrc?: string;
  userName?: string;
  userAvatarMediaId?: string;
  /** Pre-resolved user avatar URL (for extension contexts). */
  userAvatarSrc?: string;
  /** Resolve a media ID to a full URL (for extension contexts). */
  resolveMediaUrl?: (mediaId: string) => string;
  voiceEnabled?: boolean;
  speakingMessageId?: string | null;
  onSpeak?: (messageId: string, text: string) => void;
  onStopSpeaking?: () => void;
  showAbortOnStatusLine?: boolean;
  onAbort?: () => void;
}

const VIRTUAL_START = 1_000_000;

// Below this item count, desktop uses the same non-virtualized simple scroll
// container as mobile.  Virtuoso's height-estimation → measurement → scrollTop
// correction cycle causes visible jumps when scrolling back through mixed-height
// items (code blocks, tool results, images).  For conversations with fewer
// items the DOM overhead is negligible, so we avoid Virtuoso entirely.
//
// 200 items ≈ 50–100 user/assistant turns (depending on tool-call visibility),
// well within comfortable DOM-node budgets on desktop browsers.
export const SIMPLE_LIST_THRESHOLD = 200;

// ---------------------------------------------------------------------------
// StreamTextStore — allows only the actively-streaming message to re-render
// when new tokens arrive, instead of recreating renderItem for every token.
// ---------------------------------------------------------------------------
export type StreamTextStore = {
  get: () => string;
  set: (value: string) => void;
  subscribe: (callback: () => void) => () => void;
};

export function createStreamTextStore(): StreamTextStore {
  let current = "";
  const listeners = new Set<() => void>();
  return {
    get: () => current,
    set: (value: string) => {
      if (value === current) return;
      current = value;
      for (const listener of listeners) listener();
    },
    subscribe: (callback: () => void) => {
      listeners.add(callback);
      return () => {
        listeners.delete(callback);
      };
    },
  };
}

/** Hook that subscribes a single component to stream text changes. */
function useStreamText(store: StreamTextStore): string {
  return useSyncExternalStore(store.subscribe, store.get, store.get);
}

// ---------------------------------------------------------------------------
// StreamingBubble — wrapper that isolates stream-text re-renders to only the
// message that is actively streaming, preventing full-list re-measurement.
// ---------------------------------------------------------------------------
function StreamingBubble({
  store,
  content,
  messageId,
  avatarMediaId,
  avatarSrc,
  avatarFallback,
  resolveMediaUrl,
  voiceEnabled,
  isSpeakingThis,
  onSpeak,
  onStopSpeaking,
}: {
  store: StreamTextStore;
  content: string;
  messageId: string;
  avatarMediaId?: string;
  avatarSrc?: string;
  avatarFallback: string;
  resolveMediaUrl?: (mediaId: string) => string;
  voiceEnabled?: boolean;
  isSpeakingThis: boolean;
  onSpeak?: (text: string) => void;
  onStopSpeaking?: () => void;
}) {
  const streamText = useStreamText(store);
  return (
    <MessageBubble
      role="assistant"
      messageId={messageId}
      content={content}
      isStreaming={true}
      streamText={streamText}
      avatarMediaId={avatarMediaId}
      avatarSrc={avatarSrc}
      avatarFallback={avatarFallback}
      resolveMediaUrl={resolveMediaUrl}
      voiceEnabled={voiceEnabled}
      isSpeakingThis={isSpeakingThis}
      onSpeak={onSpeak ? (text) => onSpeak(text) : undefined}
      onStopSpeaking={onStopSpeaking}
    />
  );
}

export type ListItem =
  | { kind: "separator"; label: string; key: string }
  | { kind: "message"; message: DisplayMessage };

export function buildItems(
  messages: DisplayMessage[],
  t: (key: string) => string,
  showToolCalls: boolean,
  showTokenUsage: boolean,
): ListItem[] {
  const items: ListItem[] = [];
  let lastDateLabel = "";

  for (const message of messages) {
    if (
      message.type === "tool-invoke" &&
      !showToolCalls &&
      message.toolName !== "ask_user_question"
    )
      continue;
    if (
      message.type === "tool-result" &&
      !showToolCalls &&
      detectMedia(message.content) === null &&
      message.toolName !== "ask_user_question"
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
  showToolCalls,
  showTokenUsage,
  agentName,
  agentAvatarMediaId,
  agentAvatarSrc,
  userName,
  userAvatarMediaId,
  userAvatarSrc,
  resolveMediaUrl,
  voiceEnabled,
  speakingMessageId,
  onSpeak,
  onStopSpeaking,
  showAbortOnStatusLine,
  onAbort,
}: MessageListProps) {
  const { t } = useTranslation();
  const virtuosoRef = useRef<VirtuosoHandle>(null);
  const scrollContainerRef = useRef<HTMLElement | null>(null);
  const atBottomRef = useRef(true);
  const anchoredRef = useRef(false);
  // How far (px) we're allowed to scroll before anchoring. Measured as the
  // distance from the last user message to the top of the scroll container
  // when streaming begins.
  const anchorBudgetRef = useRef<number | null>(null);
  const streamStartScrollTopRef = useRef<number | null>(null);
  const [showScrollToBottom, setShowScrollToBottom] = useState(false);

  // Stream text store — only the streaming bubble subscribes to this.
  const streamTextStoreRef = useRef<StreamTextStore>(null!);
  if (!streamTextStoreRef.current) {
    streamTextStoreRef.current = createStreamTextStore();
  }
  // Sync incoming streamText prop into the store (fires subscriber in
  // StreamingBubble only, NOT the full renderItem).
  streamTextStoreRef.current.set(streamText);

  // Track the last scroll direction to suppress auto-scroll when user is
  // actively scrolling upward (iOS momentum scroll detection).
  const lastScrollTopRef = useRef(0);
  const scrollingUpRef = useRef(false);

  // --- Simple-list mode (bypasses Virtuoso) ---
  // On mobile, react-virtuoso's height estimation → measurement → scrollTop
  // correction cycle interrupts iOS momentum scrolling, causing visible jumps.
  // On desktop, the same issue occurs when scrolling back through mixed-height
  // items (code blocks, tool results, images).  For short conversations the
  // DOM overhead of rendering all items is negligible, so we skip Virtuoso
  // until the item count exceeds SIMPLE_LIST_THRESHOLD.
  const theme = useTheme();
  const isMobile = !useMediaQuery(theme.breakpoints.up("md"));
  const topSentinelRef = useRef<HTMLDivElement>(null);
  const wasPrependRef = useRef(false);
  const prevScrollHeightRef = useRef(0);

  const normalizedUserFallback = (userName || "You").trim() || "You";
  const normalizedAgentFallback = (agentName || "Agent").trim() || "Agent";

  const items = useMemo(() => {
    let filteredItems = buildItems(messages, t, showToolCalls, showTokenUsage);
    const hasVisibleMessage = filteredItems.some(
      (item) => item.kind === "message",
    );
    if (!hasVisibleMessage && messages.length > 0) {
      // If filters hide everything (e.g. a conversation that starts with tool
      // messages), show the raw timeline so the page never appears empty.
      filteredItems = buildItems(messages, t, true, true);
    }

    return filteredItems;
  }, [messages, t, showToolCalls, showTokenUsage]);

  // Use the non-virtualized simple scroll container when on mobile OR when the
  // visible item count is below the threshold.  Mobile always uses simple list;
  // desktop switches to Virtuoso only for larger histories.
  const simpleList = isMobile || items.length <= SIMPLE_LIST_THRESHOLD;

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
    if (wasEmptyRef.current && items.length > 0) {
      if (simpleList) {
        requestAnimationFrame(() => {
          const el = scrollContainerRef.current;
          if (el) el.scrollTop = el.scrollHeight;
        });
      } else if (virtuosoRef.current) {
        requestAnimationFrame(() => {
          virtuosoRef.current?.scrollToIndex({
            index: itemsLengthRef.current - 1,
            align: "end",
          });
        });
      }
      atBottomRef.current = true;
      setShowScrollToBottom(false);
    }
    wasEmptyRef.current = items.length === 0;
  }, [items.length, simpleList]);

  // Auto-scroll to bottom on new items, but stop once the user's message has
  // scrolled to the top of the viewport (one viewport height of scroll from
  // where streaming began). Stream-text changes no longer trigger this effect
  // because the StreamingBubble handles its own re-renders.
  useEffect(() => {
    if (!atBottomRef.current || items.length === 0) return;
    if (anchoredRef.current) return;
    // If the user is actively scrolling upward (iOS momentum), don't fight it.
    if (scrollingUpRef.current) return;

    const scrollEl = scrollContainerRef.current;

    // Anchor budget check (shared between mobile and desktop).
    if (
      scrollEl &&
      streamStartScrollTopRef.current !== null &&
      anchorBudgetRef.current !== null
    ) {
      const maxScrollTop =
        streamStartScrollTopRef.current + anchorBudgetRef.current;
      if (scrollEl.scrollTop >= maxScrollTop) {
        anchoredRef.current = true;
        return;
      }
      const endScrollTop = scrollEl.scrollHeight - scrollEl.clientHeight;
      if (endScrollTop > maxScrollTop) {
        scrollEl.scrollTop = maxScrollTop;
        anchoredRef.current = true;
        return;
      }
    }

    if (simpleList) {
      if (scrollEl) {
        scrollEl.scrollTop = scrollEl.scrollHeight - scrollEl.clientHeight;
      }
    } else if (virtuosoRef.current) {
      virtuosoRef.current.scrollToIndex({
        index: items.length - 1,
        align: "end",
        behavior: "auto",
      });
    }
  }, [items.length, simpleList]);

  // Capture the scroll position and the distance from the last user message
  // to the container top when streaming begins; reset when it ends.
  const wasStreamingRef = useRef(false);
  useEffect(() => {
    if (isStreaming && !wasStreamingRef.current) {
      const scrollEl = scrollContainerRef.current;
      streamStartScrollTopRef.current = scrollEl ? scrollEl.scrollTop : null;
      // Find the last user message element in the DOM and measure its offset.
      if (scrollEl) {
        const userEls = scrollEl.querySelectorAll("[data-user-message]");
        const lastUserEl = userEls[userEls.length - 1] as HTMLElement | null;
        if (lastUserEl) {
          const containerRect = scrollEl.getBoundingClientRect();
          const userRect = lastUserEl.getBoundingClientRect();
          anchorBudgetRef.current = userRect.top - containerRect.top;
        } else {
          anchorBudgetRef.current = scrollEl.clientHeight;
        }
      }
    }
    if (!isStreaming) {
      anchoredRef.current = false;
      streamStartScrollTopRef.current = null;
      anchorBudgetRef.current = null;
    }
    wasStreamingRef.current = isStreaming;
  }, [isStreaming]);

  // Scroll direction tracking — detects upward momentum on iOS to suppress
  // auto-scroll that would fight the user's gesture.
  //
  // IMPORTANT: The listener is attached via scrollerRef callback (below) rather
  // than a deps-free useEffect, because re-registering the scroll listener on
  // every render during iOS momentum scrolling causes micro-jumps (the listener
  // teardown/re-add interrupts the browser's momentum scroll bookkeeping).
  const scrollListenerCleanupRef = useRef<(() => void) | null>(null);
  const attachScrollListener = useCallback((el: HTMLElement | null) => {
    // Clean up any previous listener.
    scrollListenerCleanupRef.current?.();
    scrollListenerCleanupRef.current = null;

    if (!el) return;

    // Capture in a const so TypeScript keeps the non-null narrowing inside
    // the requestAnimationFrame closure.
    const scrollElement = el;
    let rafId: number | null = null;
    function onScroll() {
      if (rafId !== null) return;
      rafId = requestAnimationFrame(() => {
        rafId = null;
        const currentTop = scrollElement.scrollTop;
        scrollingUpRef.current = currentTop < lastScrollTopRef.current;
        lastScrollTopRef.current = currentTop;
      });
    }
    scrollElement.addEventListener("scroll", onScroll, { passive: true });
    scrollListenerCleanupRef.current = () => {
      scrollElement.removeEventListener("scroll", onScroll);
      if (rafId !== null) cancelAnimationFrame(rafId);
    };
  }, []);
  // Clean up on unmount.
  useEffect(() => {
    return () => scrollListenerCleanupRef.current?.();
  }, []);

  // Simple-list scroll container ref callback — sets the shared
  // scrollContainerRef and attaches the scroll direction listener.
  const simpleListScrollRefCallback = useCallback(
    (node: HTMLDivElement | null) => {
      scrollContainerRef.current = node;
      attachScrollListener(node);
    },
    [attachScrollListener],
  );

  const handleAtBottomStateChange = useCallback((atBottom: boolean) => {
    atBottomRef.current = atBottom;
    // When at bottom, reset scroll-up tracking so auto-scroll works again.
    if (atBottom) scrollingUpRef.current = false;
    setShowScrollToBottom(!atBottom);
  }, []);

  // Mobile: detect atBottom from scroll position (Virtuoso provides this
  // via atBottomStateChange, but the mobile simple list needs its own).
  useEffect(() => {
    if (!simpleList) return;
    const el = scrollContainerRef.current;
    if (!el) return;
    function handleScroll() {
      const atBottom = el!.scrollHeight - el!.scrollTop - el!.clientHeight < 80;
      handleAtBottomStateChange(atBottom);
    }
    el.addEventListener("scroll", handleScroll, { passive: true });
    return () => el.removeEventListener("scroll", handleScroll);
  }, [simpleList, handleAtBottomStateChange]);

  const scrollToBottom = useCallback(() => {
    if (simpleList) {
      const el = scrollContainerRef.current;
      if (el) {
        el.scrollTo({ top: el.scrollHeight, behavior: "smooth" });
      }
    } else if (virtuosoRef.current && itemsLengthRef.current > 0) {
      virtuosoRef.current.scrollToIndex({
        index: itemsLengthRef.current - 1,
        align: "end",
        behavior: "smooth",
      });
    }
  }, [simpleList]);

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
            <DateSeparator label={item.label} />
          </Container>
        );
      }

      const message = item.message;
      const isActiveRun = message.runId === activeRunId;
      const isStreamingMessage = message.id === lastStreamingAssistantId;

      if (message.type === "user") {
        return (
          <Container
            data-user-message
            maxWidth="md"
            sx={{
              pt: "100px",
              pb: 0.5,
              display: "flex",
              flexDirection: "column",
            }}
          >
            <MessageBubble
              role="user"
              content={message.content}
              timestamp={message.timestamp}
              attachments={message.attachments}
              avatarMediaId={userAvatarMediaId}
              avatarSrc={userAvatarSrc}
              avatarFallback={normalizedUserFallback}
              resolveMediaUrl={resolveMediaUrl}
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
                    src={agentAvatarSrc}
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

        // Streaming message — use isolated StreamingBubble to avoid re-rendering
        // the entire virtual list on every token.
        if (isStreamingMessage) {
          return (
            <Container
              maxWidth="md"
              sx={{ py: 0.5, display: "flex", flexDirection: "column" }}
            >
              <StreamingBubble
                store={streamTextStoreRef.current}
                content={message.content}
                messageId={message.id}
                avatarMediaId={agentAvatarMediaId}
                avatarSrc={agentAvatarSrc}
                avatarFallback={normalizedAgentFallback}
                resolveMediaUrl={resolveMediaUrl}
                voiceEnabled={voiceEnabled}
                isSpeakingThis={speakingMessageId === message.id}
                onSpeak={(text) => onSpeak?.(message.id, text)}
                onStopSpeaking={onStopSpeaking}
              />
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
              messageId={message.id}
              content={message.content}
              isStreaming={false}
              timestamp={message.timestamp}
              avatarMediaId={agentAvatarMediaId}
              avatarSrc={agentAvatarSrc}
              avatarFallback={normalizedAgentFallback}
              resolveMediaUrl={resolveMediaUrl}
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
              resolveMediaUrl={resolveMediaUrl}
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
      agentAvatarSrc,
      isRunning,
      isStreaming,
      lastStreamingAssistantId,
      normalizedAgentFallback,
      normalizedUserFallback,
      onSpeak,
      onStopSpeaking,
      resolveMediaUrl,
      speakingMessageId,
      t,
      toolActivity,
      showAbortOnStatusLine,
      onAbort,
      userAvatarMediaId,
      userAvatarSrc,
      voiceEnabled,
    ],
  );

  const computeItemKey = useCallback((_index: number, item: ListItem) => {
    if (item.kind === "separator") return item.key;
    return item.message.id;
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
    wasPrependRef.current = false;
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
                wasPrependRef.current = true;
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

  // Mobile: preserve scroll position when older messages are prepended.
  // useLayoutEffect fires after DOM mutation but before paint, so the user
  // never sees the incorrect position. iOS Safari lacks overflow-anchor
  // support, so manual adjustment is required.
  useLayoutEffect(() => {
    if (!simpleList) return;
    const el = scrollContainerRef.current;
    if (!el) return;

    if (wasPrependRef.current) {
      wasPrependRef.current = false;
      const delta = el.scrollHeight - prevScrollHeightRef.current;
      if (delta > 0) {
        el.scrollTop += delta;
      }
    }

    prevScrollHeightRef.current = el.scrollHeight;
  });

  // Mobile: load older messages when the top sentinel enters the viewport.
  useEffect(() => {
    if (!simpleList) return;
    const sentinel = topSentinelRef.current;
    const root = scrollContainerRef.current;
    if (!sentinel || !root) return;
    if (!hasMoreHistory || !onLoadOlderMessages) return;

    const observer = new IntersectionObserver(
      ([entry]) => {
        if (entry.isIntersecting && !loadingOlderMessages) {
          onLoadOlderMessages();
        }
      },
      { root, threshold: 0, rootMargin: "200px 0px 0px 0px" },
    );
    observer.observe(sentinel);
    return () => observer.disconnect();
  }, [simpleList, hasMoreHistory, loadingOlderMessages, onLoadOlderMessages]);

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
      {simpleList ? (
        <Box
          ref={simpleListScrollRefCallback}
          sx={{
            height: "100%",
            overflowY: "auto",
            overscrollBehavior: "contain",
          }}
        >
          {/* Sentinel for loading older messages via IntersectionObserver */}
          <div ref={topSentinelRef} style={{ height: 1 }} />
          {loadingOlderMessages && (
            <Box sx={{ display: "flex", justifyContent: "center", py: 2 }}>
              <CircularProgress size={20} />
            </Box>
          )}
          {items.map((item, index) => (
            <div key={computeItemKey(index, item)}>
              {renderItem(index, item)}
            </div>
          ))}
        </Box>
      ) : (
        <Virtuoso
          ref={virtuosoRef}
          scrollerRef={(ref) => {
            const element = ref as HTMLElement;
            scrollContainerRef.current = element;
            if (element) {
              // Disable browser scroll anchoring — Virtuoso manages its own,
              // and the browser's version causes double-corrections on mobile.
              element.style.overflowAnchor = "none";
              // Prevent scroll-chaining to the parent document on iOS, which
              // causes the rubber-band effect to interfere with virtual scroll.
              element.style.overscrollBehavior = "contain";
            }
            // Attach scroll direction listener once per element (not per render).
            attachScrollListener(element);
          }}
          style={{ height: "100%" }}
          data={items}
          computeItemKey={computeItemKey}
          firstItemIndex={firstItemIndex}
          initialTopMostItemIndex={items.length > 0 ? items.length - 1 : 0}
          defaultItemHeight={150}
          followOutput={false}
          atBottomThreshold={80}
          atBottomStateChange={handleAtBottomStateChange}
          startReached={handleStartReached}
          rangeChanged={handleRangeChanged}
          increaseViewportBy={{ top: 800, bottom: 200 }}
          itemContent={renderItem}
          components={{
            Header: headerComponent,
          }}
        />
      )}
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
