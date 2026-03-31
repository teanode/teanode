import React, { useEffect, useCallback, useRef, useState } from "react";
import { useParams } from "@tanstack/react-router";
import { useAppContext, useStreamingContext } from "../../../context";
import MessageList from "../../../components/MessageList";
import TodoPanel from "../../../components/TodoPanel";
import InputArea from "../../../components/InputArea";
import QuestionPanel from "../../../components/QuestionPanel";
import ApprovalPanel from "../../../components/ApprovalPanel";
import VoiceCallBar from "../../../components/VoiceCallBar";
import DebugReadout, {
  useDebugEnabled,
} from "../../../components/DebugReadout";
import { useTts } from "../../../hooks/useTts";
import { useAgentVoiceCall } from "./route";
import { profileGetRpc } from "../../../rpc";
import type { Attachment, Profile } from "../../../types";

/** /conversations/$agentId/$conversationId — active conversation. */
export default function ConversationsConversationPage() {
  const { agentId, conversationId } = useParams({ strict: false }) as {
    agentId: string;
    conversationId: string;
  };
  useStreamingContext();
  const {
    backend,
    voiceAutoSend,
    ttsVoice,
    todosPanelCollapsed,
    setTodosPanelCollapsed,
    showToolCalls,
    showTokenUsage,
  } = useAppContext();
  const agent = backend.agents.find((agent) => agent.id === agentId);
  const agentName = agent?.name || agentId;
  const [profile, setProfile] = useState<Profile>({
    name: "",
    avatarMediaId: "",
  });

  const tts = useTts(ttsVoice);
  const [speakingMessageId, setSpeakingMessageId] = useState<string | null>(
    null,
  );
  const [inputFocused, setInputFocused] = useState(false);

  const voiceCall = useAgentVoiceCall();
  const debugEnabled = useDebugEnabled();

  // Load current user profile for message avatar display.
  useEffect(() => {
    if (!backend.connected) return;
    let mounted = true;
    profileGetRpc()
      .then((loaded) => {
        if (!mounted) return;
        setProfile(loaded);
      })
      .catch(() => {});
    return () => {
      mounted = false;
    };
  }, [backend.connected]);

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
  }, [
    conversationId,
    agentId,
    backend.conversationId,
    backend.switchConversation,
  ]);

  // Auto-read: when a final message arrives and user sent via mic, speak the response.
  // Disabled when voice call is active (streaming TTS handles it).
  const prevMessagesLenRef = useRef(backend.messages.length);
  const wasCallActiveRef = useRef(voiceCall.isCallActive);
  useEffect(() => {
    // When a call just ended, clear the mic flag so we don't auto-read the
    // last response that was already handled by the call's streaming TTS.
    if (wasCallActiveRef.current && !voiceCall.isCallActive) {
      backend.lastSentViaMicRef.current = false;
      prevMessagesLenRef.current = backend.messages.length;
    }
    wasCallActiveRef.current = voiceCall.isCallActive;

    if (voiceCall.isCallActive) return;
    if (!backend.lastSentViaMicRef.current) return;
    if (backend.isRunning) return;
    const recentMessages = backend.messages;
    if (recentMessages.length <= prevMessagesLenRef.current) {
      prevMessagesLenRef.current = recentMessages.length;
      return;
    }
    prevMessagesLenRef.current = recentMessages.length;
    // Find the last assistant message.
    for (let index = recentMessages.length - 1; index >= 0; index--) {
      if (
        recentMessages[index].type === "assistant" &&
        recentMessages[index].content
      ) {
        tts.speak(recentMessages[index].content);
        setSpeakingMessageId(recentMessages[index].id);
        backend.lastSentViaMicRef.current = false;
        break;
      }
    }
  }, [backend.isRunning, backend.messages, tts.speak, voiceCall.isCallActive]);

  const handleSend = useCallback(
    (text: string, attachments?: Attachment[]) => {
      backend.markTypedSend();
      backend.sendMessage(text, undefined, attachments);
    },
    [backend.sendMessage, backend.markTypedSend],
  );

  const handleVoiceMessage = useCallback(
    (text: string) => {
      backend.sendVoiceMessage(text);
    },
    [backend.sendVoiceMessage],
  );

  const handleSpeak = useCallback(
    (messageId: string, text: string) => {
      setSpeakingMessageId(messageId);
      tts.speak(text);
    },
    [tts.speak],
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
        showToolCalls={showToolCalls}
        showTokenUsage={showTokenUsage}
        hasMoreHistory={backend.hasMoreHistory}
        loadingOlderMessages={backend.loadingOlderMessages}
        onLoadOlderMessages={backend.loadOlderMessages}
        agentName={agentName}
        agentAvatarMediaId={agent?.avatarMediaId}
        userName={profile.name || "You"}
        userAvatarMediaId={profile.avatarMediaId || undefined}
        voiceEnabled={backend.audioCapability && !voiceCall.isCallActive}
        speakingMessageId={speakingMessageId}
        onSpeak={handleSpeak}
        onStopSpeaking={handleStopSpeaking}
        showAbortOnStatusLine={backend.isRunning && !inputFocused}
        onAbort={backend.abortRun}
      />
      <TodoPanel
        todos={backend.todos}
        collapsed={todosPanelCollapsed}
        onToggleCollapsed={setTodosPanelCollapsed}
      />
      {voiceCall.isCallActive ? (
        <VoiceCallBar
          callDuration={voiceCall.callDuration}
          isMuted={voiceCall.isMuted}
          isUserSpeaking={voiceCall.isUserSpeaking}
          isPlaying={voiceCall.isPlaying}
          isSynthesizing={voiceCall.isSynthesizing}
          onToggleMute={voiceCall.toggleMute}
          onEndCall={voiceCall.endCall}
          onInterrupt={voiceCall.interruptAgent}
        />
      ) : backend.pendingQuestions.length > 0 ? (
        <QuestionPanel
          questions={backend.pendingQuestions}
          onSubmitAll={backend.answerQuestion}
          disabled={!backend.connected}
        />
      ) : backend.pendingApprovals.length > 0 ? (
        <ApprovalPanel
          approvals={backend.pendingApprovals}
          onResolve={backend.resolveApproval}
          disabled={!backend.connected}
        />
      ) : (
        <InputArea
          isRunning={backend.isRunning}
          connected={backend.connected && !backend.connecting}
          agentName={agentName}
          draftKey={conversationId}
          model={backend.conversationModel}
          voiceEnabled={backend.audioCapability}
          voiceAutoSend={voiceAutoSend}
          voiceCallActive={voiceCall.isCallActive}
          voiceCallConnecting={voiceCall.isConnecting}
          onStartVoiceCall={voiceCall.startCall}
          onSend={handleSend}
          onAbort={backend.abortRun}
          onFocusChange={setInputFocused}
          showAbortInCollapsedInput={false}
          onVoiceMessage={handleVoiceMessage}
        />
      )}
      {debugEnabled && (
        <DebugReadout
          conversationId={conversationId}
          connected={backend.connected}
          activeRunId={backend.currentRunId}
          lastActiveRunState={backend.lastActiveRunState}
          isRunning={backend.isRunning}
          status={backend.status}
          isStreaming={backend.isStreaming}
          toolActivity={backend.toolActivity}
          currentRunId={backend.currentRunId}
        />
      )}
    </>
  );
}
