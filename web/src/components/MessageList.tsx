import React, { useEffect, useRef } from 'react';
import type { DisplayMessage } from '../types';
import MessageBubble from './MessageBubble';
import ToolInvoke from './ToolInvoke';
import ToolResult from './ToolResult';
import ToolActivity from './ToolActivity';
import UsageIndicator from './UsageIndicator';

interface MessageListProps {
  messages: DisplayMessage[];
  showToolCalls: boolean;
  isStreaming: boolean;
  streamText: string;
  toolActivity: string | null;
}

function formatTime(ts: number): string {
  const d = new Date(ts);
  return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}

function dateLabelFor(ts: number): string {
  const msgDate = new Date(ts);
  const now = new Date();

  const msgDay = new Date(msgDate.getFullYear(), msgDate.getMonth(), msgDate.getDate());
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  const diff = today.getTime() - msgDay.getTime();

  if (diff === 0) return 'Today';
  if (diff === 86_400_000) return 'Yesterday';
  return msgDate.toLocaleDateString([], { weekday: 'short', month: 'short', day: 'numeric', year: msgDate.getFullYear() !== now.getFullYear() ? 'numeric' : undefined });
}

export default function MessageList({
  messages,
  showToolCalls,
  isStreaming,
  streamText,
  toolActivity,
}: MessageListProps) {
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (containerRef.current) {
      containerRef.current.scrollTop = containerRef.current.scrollHeight;
    }
  }, [messages, streamText, toolActivity]);

  // Handle copy button clicks via event delegation
  function handleClick(e: React.MouseEvent) {
    const target = e.target as HTMLElement;
    const btn = target.closest('.copy-btn') as HTMLButtonElement | null;
    if (!btn) return;
    const block = btn.closest('.code-block');
    if (!block) return;
    const code = block.querySelector('pre code');
    if (!code) return;
    navigator.clipboard.writeText(code.textContent || '').then(() => {
      btn.textContent = 'Copied!';
      btn.classList.add('copied');
      setTimeout(() => {
        btn.textContent = 'Copy';
        btn.classList.remove('copied');
      }, 1500);
    });
  }

  // Track the last date label we emitted so we can insert separators.
  let lastDateLabel = '';

  return (
    <div
      ref={containerRef}
      className="flex-1 overflow-y-auto p-4 flex flex-col gap-2"
      onClick={handleClick}
    >
      {messages.map((msg, idx) => {
        const isLast = idx === messages.length - 1;
        const isStreamingMsg = isLast && msg.type === 'assistant' && isStreaming;

        // Date separator — only for user/assistant messages with timestamps
        let dateSeparator: React.ReactNode = null;
        if (msg.timestamp && (msg.type === 'user' || msg.type === 'assistant')) {
          const label = dateLabelFor(msg.timestamp);
          if (label !== lastDateLabel) {
            lastDateLabel = label;
            dateSeparator = (
              <div className="flex items-center gap-3 my-2" key={`sep-${msg.id}`}>
                <div className="flex-1 h-px bg-border" />
                <span className="text-[11px] text-muted font-medium">{label}</span>
                <div className="flex-1 h-px bg-border" />
              </div>
            );
          }
        }

        if (msg.type === 'user') {
          return (
            <React.Fragment key={msg.id}>
              {dateSeparator}
              <MessageBubble role="user" content={msg.content} timestamp={msg.timestamp} />
            </React.Fragment>
          );
        }

        if (msg.type === 'assistant') {
          return (
            <React.Fragment key={msg.id}>
              {dateSeparator}
              <MessageBubble
                role="assistant"
                content={msg.content}
                isStreaming={isStreamingMsg}
                streamText={isStreamingMsg ? streamText : undefined}
                timestamp={msg.timestamp}
              />
            </React.Fragment>
          );
        }

        if (msg.type === 'tool-invoke') {
          if (!showToolCalls) return null;
          return (
            <ToolInvoke
              key={msg.id}
              toolName={msg.toolName || 'tool'}
              args={msg.content}
            />
          );
        }

        if (msg.type === 'tool-result') {
          if (!showToolCalls) return null;
          return (
            <ToolResult
              key={msg.id}
              toolName={msg.toolName || 'tool'}
              content={msg.content}
            />
          );
        }

        if (msg.type === 'usage') {
          return <UsageIndicator key={msg.id} text={msg.content} />;
        }

        return null;
      })}

      {toolActivity && !showToolCalls && (
        <ToolActivity toolName={toolActivity} />
      )}
    </div>
  );
}
