import React, { useEffect, useCallback, useRef } from 'react';
import { useParams } from '@tanstack/react-router';
import { useAppContext } from '../../../context';
import MessageList from '../../../components/MessageList';
import InputArea from '../../../components/InputArea';

/** /conversations/$agentId/$conversationId — active conversation. */
export default function ConversationsConversationPage() {
  const { agentId, conversationId } = useParams({ strict: false }) as {
    agentId: string;
    conversationId: string;
  };
  const { backend } = useAppContext();
  const agent = backend.agents.find((agent) => agent.id === agentId);
  const agentName = agent?.name || agentId;

  // Switch to this conversation when params change.
  const previousKeyRef = useRef<string | null>(null);
  useEffect(() => {
    if (conversationId && conversationId !== previousKeyRef.current) {
      previousKeyRef.current = conversationId;
      if (conversationId !== backend.conversationId) {
        backend.switchConversation(conversationId, agentId);
      }
    }
  }, [conversationId, agentId, backend.conversationId, backend.switchConversation]);

  const handleSend = useCallback(
    (text: string) => {
      backend.sendMessage(text);
    },
    [backend.sendMessage]
  );

  return (
    <>
      <MessageList
        messages={backend.messages}
        isRunning={backend.isRunning}
        isStreaming={backend.isStreaming}
        streamText={backend.streamText}
        toolActivity={backend.toolActivity}
        status={backend.status}
        activeRunId={backend.currentRunId}
        hasMoreHistory={backend.hasMoreHistory}
        loadingOlderMessages={backend.loadingOlderMessages}
        onLoadOlderMessages={backend.loadOlderMessages}
      />
      <InputArea
        isRunning={backend.isRunning}
        agentName={agentName}
        draftKey={conversationId}
        onSend={handleSend}
        onAbort={backend.abortRun}
      />
    </>
  );
}
