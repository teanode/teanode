import React from 'react';
import { renderMarkdown } from '../markdown';

interface MessageBubbleProps {
  role: 'user' | 'assistant';
  content: string;
  isStreaming?: boolean;
  streamText?: string;
  timestamp?: number;
}

function formatTime(ts: number): string {
  return new Date(ts).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}

export default function MessageBubble({ role, content, isStreaming, streamText, timestamp }: MessageBubbleProps) {
  const timeEl = timestamp ? (
    <div className="text-[10px] text-muted mt-1 select-none">{formatTime(timestamp)}</div>
  ) : null;

  if (role === 'user') {
    return (
      <div className="self-end max-w-[85%] max-md:max-w-[95%] px-4 py-3 rounded-[8px] leading-relaxed break-words bg-user-bg border border-[#3a4a1a] whitespace-pre-wrap">
        {content}
        {timeEl}
      </div>
    );
  }

  // Assistant message
  const displayText = isStreaming ? (streamText ?? content) : content;

  // Handle special states
  if (displayText.startsWith('__error__:')) {
    const errorMsg = displayText.substring('__error__:'.length);
    return (
      <div className="self-start max-w-[85%] max-md:max-w-[95%] px-4 py-3 rounded-[8px] leading-relaxed break-words bg-surface border border-border">
        <em className="text-danger">Error: {errorMsg}</em>
        {timeEl}
      </div>
    );
  }

  if (displayText === '__aborted__') {
    return (
      <div className="self-start max-w-[85%] max-md:max-w-[95%] px-4 py-3 rounded-[8px] leading-relaxed break-words bg-surface border border-border">
        <em className="text-dim">Aborted</em>
        {timeEl}
      </div>
    );
  }

  if (!displayText) {
    return null;
  }

  return (
    <div className="self-start max-w-[85%] max-md:max-w-[95%] px-4 py-3 rounded-[8px] leading-relaxed break-words bg-surface border border-border markdown-content">
      <div dangerouslySetInnerHTML={{ __html: renderMarkdown(displayText) }} />
      {timeEl}
    </div>
  );
}
