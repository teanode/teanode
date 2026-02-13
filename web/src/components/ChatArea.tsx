import React from 'react';
import type { DisplayMessage } from '../types';
import MessageList from './MessageList';
import InputArea from './InputArea';

interface ChatAreaProps {
  messages: DisplayMessage[];
  isRunning: boolean;
  status: string;
  showToolCalls: boolean;
  isStreaming: boolean;
  streamText: string;
  toolActivity: string | null;
  onSend: (text: string) => void;
  onAbort: () => void;
  onToggleTools: () => void;
}

export default function ChatArea({
  messages,
  isRunning,
  status,
  showToolCalls,
  isStreaming,
  streamText,
  toolActivity,
  onSend,
  onAbort,
  onToggleTools,
}: ChatAreaProps) {
  return (
    <>
      <MessageList
        messages={messages}
        showToolCalls={showToolCalls}
        isStreaming={isStreaming}
        streamText={streamText}
        toolActivity={toolActivity}
      />
      <InputArea
        isRunning={isRunning}
        status={status}
        showToolCalls={showToolCalls}
        onSend={onSend}
        onAbort={onAbort}
        onToggleTools={onToggleTools}
      />
    </>
  );
}
