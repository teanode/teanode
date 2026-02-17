import React, { useEffect, useRef, useMemo, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { Virtuoso, VirtuosoHandle } from 'react-virtuoso';
import Box from '@mui/material/Box';
import Container from '@mui/material/Container';
import Divider from '@mui/material/Divider';
import Typography from '@mui/material/Typography';
import CircularProgress from '@mui/material/CircularProgress';
import HourglassEmptyRounded from '@mui/icons-material/HourglassEmptyRounded';
import type { DisplayMessage } from '../types';
import { useAppContext } from '../context';
import MessageBubble from './MessageBubble';
import ToolInvoke from './ToolInvoke';
import ToolResult, { detectMedia } from './ToolResult';
import UsageIndicator from './UsageIndicator';

interface MessageListProps {
  messages: DisplayMessage[];
  isRunning: boolean;
  isStreaming: boolean;
  streamText: string;
  toolActivity: string | null;
  status: string;
  activeRunId: string | null;
}

type ListItem =
  | { kind: 'separator'; label: string; key: string }
  | { kind: 'message'; message: DisplayMessage };

function dateLabelFor(timestamp: number, t: (key: string) => string): string {
  const messageDate = new Date(timestamp);
  const now = new Date();

  const messageDay = new Date(messageDate.getFullYear(), messageDate.getMonth(), messageDate.getDate());
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  const diff = today.getTime() - messageDay.getTime();

  if (diff === 0) return t('conversations.today');
  if (diff === 86_400_000) return t('conversations.yesterday');
  return messageDate.toLocaleDateString([], { weekday: 'short', month: 'short', day: 'numeric', year: messageDate.getFullYear() !== now.getFullYear() ? 'numeric' : undefined });
}

function buildItems(
  messages: DisplayMessage[],
  t: (key: string) => string,
  showToolCalls: boolean,
  showTokenUsage: boolean,
): ListItem[] {
  const items: ListItem[] = [];
  let lastDateLabel = '';

  for (const message of messages) {
    if (message.type === 'tool-invoke' && !showToolCalls) continue;
    if (message.type === 'tool-result' && !showToolCalls && detectMedia(message.content) === null) continue;
    if (message.type === 'usage' && !showTokenUsage) continue;

    if (message.timestamp && (message.type === 'user' || message.type === 'assistant')) {
      const label = dateLabelFor(message.timestamp, t);
      if (label !== lastDateLabel) {
        lastDateLabel = label;
        items.push({ kind: 'separator', label, key: `sep-${message.id}` });
      }
    }

    items.push({ kind: 'message', message });
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
}: MessageListProps) {
  const { t } = useTranslation();
  const { showToolCalls, showTokenUsage } = useAppContext();
  const virtuosoRef = useRef<VirtuosoHandle>(null);
  const atBottomRef = useRef(true);

  const items = useMemo(
    () => buildItems(messages, t, showToolCalls, showTokenUsage),
    [messages, t, showToolCalls, showTokenUsage],
  );

  // Scroll to bottom when streaming text updates and user hasn't scrolled up.
  // Virtuoso's followOutput handles new items, but streaming updates only change
  // content of the last item without adding new ones.
  useEffect(() => {
    if (atBottomRef.current && virtuosoRef.current && items.length > 0) {
      virtuosoRef.current.scrollToIndex({ index: items.length - 1, align: 'end', behavior: 'smooth' });
    }
  }, [streamText, toolActivity]);

  const handleAtBottomStateChange = useCallback((atBottom: boolean) => {
    atBottomRef.current = atBottom;
  }, []);

  function handleClick(event: React.MouseEvent) {
    const target = event.target as HTMLElement;
    const button = target.closest('.copy-btn') as HTMLButtonElement | null;
    if (!button) return;
    const block = button.closest('.code-block');
    if (!block) return;
    const code = block.querySelector('pre code');
    if (!code) return;
    const copyIcon = '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>';
    const checkIcon = '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>';
    navigator.clipboard.writeText(code.textContent || '').then(() => {
      button.innerHTML = checkIcon;
      button.classList.add('copied');
      setTimeout(() => {
        button.innerHTML = copyIcon;
        button.classList.remove('copied');
      }, 1500);
    });
  }

  const renderItem = useCallback((index: number, item: ListItem) => {
    if (item.kind === 'separator') {
      return (
        <Container maxWidth="md" sx={{ py: 0.5, display: 'flex', flexDirection: 'column' }}>
          <Divider sx={{ my: 1 }}>
            <Typography variant="caption" color="text.secondary" sx={{ fontSize: '11px', fontWeight: 500 }}>
              {item.label}
            </Typography>
          </Divider>
        </Container>
      );
    }

    const message = item.message;
    const isActiveRun = message.runId === activeRunId;
    const isStreamingMessage = isActiveRun && isStreaming;

    if (message.type === 'user') {
      return (
        <Container maxWidth="md" sx={{ py: 0.5, display: 'flex', flexDirection: 'column' }}>
          <MessageBubble role="user" content={message.content} timestamp={message.timestamp} />
        </Container>
      );
    }

    if (message.type === 'assistant') {
      // Active run, waiting for response — show thinking spinner.
      if (!message.content && !isStreaming && isRunning && (isActiveRun || !message.runId)) {
        return (
          <Container maxWidth="md" sx={{ py: 0.5, display: 'flex', flexDirection: 'column' }}>
            <Box sx={{ alignSelf: 'flex-start', px: 1.5, py: 0.5, display: 'flex', alignItems: 'center', gap: 1 }}>
              <CircularProgress size={12} color="primary" />
              <Typography variant="caption" color="text.secondary" sx={{ fontStyle: 'italic' }}>
                {toolActivity ? t('conversations.callingTool', { toolName: toolActivity }) : t('conversations.thinking')}
              </Typography>
            </Box>
          </Container>
        );
      }

      // Queued run — show queued indicator
      if (!isActiveRun && !message.content && message.runId) {
        return (
          <Container maxWidth="md" sx={{ py: 0.5, display: 'flex', flexDirection: 'column' }}>
            <Box sx={{ alignSelf: 'flex-start', px: 1.5, py: 0.5, display: 'flex', alignItems: 'center', gap: 1 }}>
              <HourglassEmptyRounded sx={{ fontSize: 12 }} color="disabled" />
              <Typography variant="caption" color="text.secondary" sx={{ fontStyle: 'italic' }}>
                {t('conversations.queued')}
              </Typography>
            </Box>
          </Container>
        );
      }

      // Normal assistant message rendering
      return (
        <Container maxWidth="md" sx={{ py: 0.5, display: 'flex', flexDirection: 'column' }}>
          <MessageBubble
            role="assistant"
            content={message.content}
            isStreaming={isStreamingMessage}
            streamText={isStreamingMessage ? streamText : undefined}
            timestamp={message.timestamp}
          />
        </Container>
      );
    }

    if (message.type === 'tool-invoke') {
      return (
        <Container maxWidth="md" sx={{ py: 0.5 }}>
          <ToolInvoke toolName={message.toolName || 'tool'} args={message.content} />
        </Container>
      );
    }

    if (message.type === 'tool-result') {
      return (
        <Container maxWidth="md" sx={{ py: 0.5 }}>
          <ToolResult toolName={message.toolName || 'tool'} content={message.content} />
        </Container>
      );
    }

    if (message.type === 'usage') {
      return (
        <Container maxWidth="md" sx={{ py: 0.5 }}>
          <UsageIndicator text={message.content} />
        </Container>
      );
    }

    return <div />;
  }, [activeRunId, isStreaming, isRunning, streamText, toolActivity, t]);

  const computeItemKey = useCallback((_index: number, item: ListItem) => {
    return item.kind === 'separator' ? item.key : item.message.id;
  }, []);

  return (
    <Box onClick={handleClick} sx={{ flex: 1, minHeight: 0 }}>
      <Virtuoso
        ref={virtuosoRef}
        style={{ height: '100%' }}
        data={items}
        computeItemKey={computeItemKey}
        initialTopMostItemIndex={items.length > 0 ? items.length - 1 : 0}
        followOutput="smooth"
        atBottomThreshold={80}
        atBottomStateChange={handleAtBottomStateChange}
        increaseViewportBy={{ top: 500, bottom: 200 }}
        itemContent={renderItem}
      />
    </Box>
  );
}
