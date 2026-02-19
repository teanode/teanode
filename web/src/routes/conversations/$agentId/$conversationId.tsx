import React, { useEffect, useCallback, useRef, useState } from 'react';
import { useParams } from '@tanstack/react-router';
import { useAppContext } from '../../../context';
import MessageList from '../../../components/MessageList';
import InputArea from '../../../components/InputArea';
import { useTTS } from '../../../hooks/useTTS';
import type { Attachment } from '../../../types';

/** /conversations/$agentId/$conversationId — active conversation. */
export default function ConversationsConversationPage() {
  const { agentId, conversationId } = useParams({ strict: false }) as {
    agentId: string;
    conversationId: string;
  };
  const { backend, voiceAutoSend, ttsVoice } = useAppContext();
  const agent = backend.agents.find((agent) => agent.id === agentId);
  const agentName = agent?.name || agentId;

  const tts = useTTS(ttsVoice);
  const [speakingMessageId, setSpeakingMessageId] = useState<string | null>(null);

  // Clear speaking state when TTS stops.
  const prevSpeaking = useRef(false);
  useEffect(() => {
    if (prevSpeaking.current && !tts.isSpeaking) {
      setSpeakingMessageId(null);
    }
    prevSpeaking.current = tts.isSpeaking;
  }, [tts.isSpeaking]);

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

  // Auto-read: when a final message arrives and user sent via mic, speak the response.
  const prevMessagesLenRef = useRef(backend.messages.length);
  useEffect(() => {
    if (!backend.lastSentViaMicRef.current) return;
    if (backend.isRunning) return;
    const msgs = backend.messages;
    if (msgs.length <= prevMessagesLenRef.current) {
      prevMessagesLenRef.current = msgs.length;
      return;
    }
    prevMessagesLenRef.current = msgs.length;
    // Find the last assistant message.
    for (let i = msgs.length - 1; i >= 0; i--) {
      if (msgs[i].type === 'assistant' && msgs[i].content) {
        tts.speak(msgs[i].content);
        setSpeakingMessageId(msgs[i].id);
        backend.lastSentViaMicRef.current = false;
        break;
      }
    }
  }, [backend.isRunning, backend.messages, tts.speak]);

  const handleSend = useCallback(
    (text: string, attachments?: Attachment[]) => {
      backend.markTypedSend();
      backend.sendMessage(text, undefined, attachments);
    },
    [backend.sendMessage, backend.markTypedSend]
  );

  const handleVoiceMessage = useCallback(
    (text: string) => {
      backend.sendVoiceMessage(text);
    },
    [backend.sendVoiceMessage]
  );

  const handleSpeak = useCallback(
    (messageId: string, text: string) => {
      setSpeakingMessageId(messageId);
      tts.speak(text);
    },
    [tts.speak]
  );

  const handleStopSpeaking = useCallback(() => {
    tts.stop();
    setSpeakingMessageId(null);
  }, [tts.stop]);

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
        voiceEnabled={backend.audioCapability}
        speakingMessageId={speakingMessageId}
        onSpeak={handleSpeak}
        onStopSpeaking={handleStopSpeaking}
      />
      <InputArea
        isRunning={backend.isRunning}
        agentName={agentName}
        draftKey={conversationId}
        model={backend.conversationModel}
        voiceEnabled={backend.audioCapability}
        voiceAutoSend={voiceAutoSend}
        onSend={handleSend}
        onAbort={backend.abortRun}
        onVoiceMessage={handleVoiceMessage}
      />
    </>
  );
}
