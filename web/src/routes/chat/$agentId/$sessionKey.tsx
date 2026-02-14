import React, { useEffect, useCallback, useRef } from 'react';
import { useParams } from '@tanstack/react-router';
import { useAppContext } from '../../../context';
import MessageList from '../../../components/MessageList';
import InputArea from '../../../components/InputArea';

/** /chat/$agentId/$sessionKey — active chat session. */
export default function ChatSessionPage() {
  const { agentId, sessionKey } = useParams({ strict: false }) as {
    agentId: string;
    sessionKey: string;
  };
  const { chat } = useAppContext();

  // Switch to this session when params change.
  const previousKeyRef = useRef<string | null>(null);
  useEffect(() => {
    if (sessionKey && sessionKey !== previousKeyRef.current) {
      previousKeyRef.current = sessionKey;
      if (sessionKey !== chat.sessionKey) {
        chat.switchSession(sessionKey, agentId);
      }
    }
  }, [sessionKey, agentId, chat.sessionKey, chat.switchSession]);

  const handleSend = useCallback(
    (text: string) => {
      chat.sendMessage(text);
    },
    [chat.sendMessage]
  );

  return (
    <>
      <MessageList
        messages={chat.messages}
        isRunning={chat.isRunning}
        isStreaming={chat.isStreaming}
        streamText={chat.streamText}
        toolActivity={chat.toolActivity}
        status={chat.status}
      />
      <InputArea
        isRunning={chat.isRunning}
        onSend={handleSend}
        onAbort={chat.abortRun}
      />
    </>
  );
}
