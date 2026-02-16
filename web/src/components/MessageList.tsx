import React, { useEffect, useRef, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import Box from '@mui/material/Box';
import Divider from '@mui/material/Divider';
import Typography from '@mui/material/Typography';
import CircularProgress from '@mui/material/CircularProgress';
import HourglassEmptyRounded from '@mui/icons-material/HourglassEmptyRounded';
import type { DisplayMessage } from '../types';
import { useAppContext } from '../context';
import MessageBubble from './MessageBubble';
import ToolInvoke from './ToolInvoke';
import ToolResult from './ToolResult';
import UsageIndicator from './UsageIndicator';

const SCROLL_THRESHOLD = 80;

interface MessageListProps {
  messages: DisplayMessage[];
  isRunning: boolean;
  isStreaming: boolean;
  streamText: string;
  toolActivity: string | null;
  status: string;
  activeRunId: string | null;
}

function formatTime(timestamp: number): string {
  const date = new Date(timestamp);
  return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}

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

function isNearBottom(element: HTMLElement): boolean {
  return element.scrollHeight - element.scrollTop - element.clientHeight < SCROLL_THRESHOLD;
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
  const containerRef = useRef<HTMLDivElement>(null);
  const userScrolledUp = useRef(false);

  const handleScroll = useCallback(() => {
    if (containerRef.current) {
      userScrolledUp.current = !isNearBottom(containerRef.current);
    }
  }, []);

  useEffect(() => {
    if (containerRef.current && !userScrolledUp.current) {
      containerRef.current.scrollTop = containerRef.current.scrollHeight;
    }
  }, [messages, streamText, toolActivity, isRunning]);

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

  let lastDateLabel = '';

  return (
    <Box
      ref={containerRef}
      onClick={handleClick}
      onScroll={handleScroll}
      sx={{
        flex: 1,
        overflowY: 'auto',
        p: 2,
        display: 'flex',
        flexDirection: 'column',
        gap: 1,
      }}
    >
      {messages.map((message, index) => {
        const isActiveRun = message.runId === activeRunId;
        const isStreamingMessage = isActiveRun && isStreaming;

        let dateSeparator: React.ReactNode = null;
        if (message.timestamp && (message.type === 'user' || message.type === 'assistant')) {
          const label = dateLabelFor(message.timestamp, t);
          if (label !== lastDateLabel) {
            lastDateLabel = label;
            dateSeparator = (
              <Divider key={`sep-${message.id}`} sx={{ my: 1 }}>
                <Typography variant="caption" color="text.secondary" sx={{ fontSize: '11px', fontWeight: 500 }}>
                  {label}
                </Typography>
              </Divider>
            );
          }
        }

        if (message.type === 'user') {
          return (
            <React.Fragment key={message.id}>
              {dateSeparator}
              <MessageBubble role="user" content={message.content} timestamp={message.timestamp} />
            </React.Fragment>
          );
        }

        if (message.type === 'assistant') {
          // Active run, waiting for response — show thinking spinner.
          // Also show thinking for messages not yet tagged with a runId (pre-RPC-response).
          if (!message.content && !isStreaming && isRunning && (isActiveRun || !message.runId)) {
            return (
              <React.Fragment key={message.id}>
                {dateSeparator}
                <Box sx={{ alignSelf: 'flex-start', px: 1.5, py: 0.5, display: 'flex', alignItems: 'center', gap: 1 }}>
                  <CircularProgress size={12} color="primary" />
                  <Typography variant="caption" color="text.secondary" sx={{ fontStyle: 'italic' }}>
                    {toolActivity ? t('conversations.callingTool', { toolName: toolActivity }) : t('conversations.thinking')}
                  </Typography>
                </Box>
              </React.Fragment>
            );
          }

          // Queued run — show queued indicator
          if (!isActiveRun && !message.content && message.runId) {
            return (
              <React.Fragment key={message.id}>
                {dateSeparator}
                <Box sx={{ alignSelf: 'flex-start', px: 1.5, py: 0.5, display: 'flex', alignItems: 'center', gap: 1 }}>
                  <HourglassEmptyRounded sx={{ fontSize: 12 }} color="disabled" />
                  <Typography variant="caption" color="text.secondary" sx={{ fontStyle: 'italic' }}>
                    {t('conversations.queued')}
                  </Typography>
                </Box>
              </React.Fragment>
            );
          }

          // Normal assistant message rendering
          return (
            <React.Fragment key={message.id}>
              {dateSeparator}
              <MessageBubble
                role="assistant"
                content={message.content}
                isStreaming={isStreamingMessage}
                streamText={isStreamingMessage ? streamText : undefined}
                timestamp={message.timestamp}
              />
            </React.Fragment>
          );
        }

        if (message.type === 'tool-invoke') {
          if (!showToolCalls) return null;
          return (
            <ToolInvoke
              key={message.id}
              toolName={message.toolName || 'tool'}
              args={message.content}
            />
          );
        }

        if (message.type === 'tool-result') {
          if (!showToolCalls) return null;
          return (
            <ToolResult
              key={message.id}
              toolName={message.toolName || 'tool'}
              content={message.content}
            />
          );
        }

        if (message.type === 'usage') {
          if (!showTokenUsage) return null;
          return <UsageIndicator key={message.id} text={message.content} />;
        }

        return null;
      })}
    </Box>
  );
}
