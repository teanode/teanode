import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate, useParams } from '@tanstack/react-router';
import Box from '@mui/material/Box';
import Container from '@mui/material/Container';
import MenuItem from '@mui/material/MenuItem';
import ListSubheader from '@mui/material/ListSubheader';
import Select from '@mui/material/Select';
import { useAppContext } from '../../../context';
import InputArea from '../../../components/InputArea';
import VoiceCallBar from '../../../components/VoiceCallBar';
import { useAgentVoiceCall } from './route';
import type { Attachment, ModelInfo } from '../../../types';

/** /conversations/$agentId/ — new conversation page with centered input. */
export default function ConversationsNewPage() {
  const { t } = useTranslation();
  const { agentId } = useParams({ strict: false }) as { agentId: string };
  const { backend, voiceAutoSend } = useAppContext();
  const agent = backend.agents.find((agent) => agent.id === agentId);
  const agentName = agent?.name || agentId;
  const navigate = useNavigate();

  const voiceCall = useAgentVoiceCall();

  // Model picker state — default to empty (agent's configured default).
  const [selectedModel, setSelectedModel] = useState('');

  // Track whether the page is ready to accept a new conversation id.
  // Starts false; becomes true once any prior conversation has been cleared.
  const [ready, setReady] = useState(!backend.conversationId);

  useEffect(() => {
    if (!ready) {
      backend.newConversation();
      setReady(true);
    }
  }, [ready, backend.newConversation]);

  // Navigate to the conversation page only when a NEW conversation id appears after we're ready.
  useEffect(() => {
    if (ready && backend.conversationId) {
      navigate({
        to: '/conversations/$agentId/$conversationId',
        params: { agentId, conversationId: backend.conversationId },
        replace: true,
      });
    }
  }, [ready, backend.conversationId, agentId, navigate]);

  const handleSend = useCallback(
    (text: string, attachments?: Attachment[]) => {
      backend.markTypedSend();
      backend.sendMessage(text, selectedModel || undefined, attachments);
    },
    [backend.sendMessage, backend.markTypedSend, selectedModel]
  );

  const handleVoiceMessage = useCallback(
    (text: string) => {
      backend.sendVoiceMessage(text, selectedModel || undefined,
        'The user dictated this message using voice input and the response may be read aloud. Keep the response concise and avoid heavy markdown formatting.');
    },
    [backend.sendVoiceMessage, selectedModel]
  );

  // Group models by provider for the select menu.
  const grouped = useMemo(() => {
    const map = new Map<string, ModelInfo[]>();
    for (const modelInfo of backend.models) {
      const list = map.get(modelInfo.provider) || [];
      list.push(modelInfo);
      map.set(modelInfo.provider, list);
    }
    return map;
  }, [backend.models]);

  const modelMenuItems: React.ReactNode[] = [
    <MenuItem key="__default" value="">{t('common.default')}</MenuItem>,
  ];
  for (const [provider, providerModels] of grouped.entries()) {
    modelMenuItems.push(<ListSubheader key={`header-${provider}`}>{provider}</ListSubheader>);
    for (const modelInfo of providerModels) {
      const qualified = `${modelInfo.provider}:${modelInfo.id}`;
      modelMenuItems.push(
        <MenuItem key={qualified} value={qualified}>{modelInfo.id}</MenuItem>
      );
    }
  }

  const modelPicker = backend.models.length > 0 ? (
    <Select
      size="small"
      variant="standard"
      displayEmpty
      disableUnderline
      value={selectedModel}
      onChange={(event) => setSelectedModel(event.target.value as string)}
      renderValue={(value) => {
        if (!value) return t('common.default');
        return value.includes(':') ? value.split(':').slice(1).join(':') : value;
      }}
      IconComponent={() => null}
      sx={{
        fontSize: '0.75rem',
        color: 'text.secondary',
        '& .MuiSelect-select': {
          py: 0.5,
          pr: '0.5rem !important',
          pl: 0.5,
        },
      }}
    >
      {modelMenuItems}
    </Select>
  ) : undefined;

  return (
    <Box sx={{ flex: 1, display: 'flex', flexDirection: 'column' }}>
      <Container maxWidth="md" sx={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center', px: 2 }}>
        <Box sx={{ width: '100%' }}>
          {voiceCall.isCallActive ? (
            <VoiceCallBar
              callDuration={voiceCall.callDuration}
              isMuted={voiceCall.isMuted}
              isUserSpeaking={voiceCall.isUserSpeaking}
              isPlaying={voiceCall.isPlaying}
              isSynthesizing={voiceCall.isSynthesizing}
              onToggleMute={voiceCall.toggleMute}
              onEndCall={voiceCall.endCall}
              bare
            />
          ) : (
            <InputArea
              agentName={agentName}
              draftKey="new"
              placeholder={t('conversations.startConversation', { agentName })}
              autoFocus
              modelPicker={modelPicker}
              bare
              alwaysExpanded
              voiceEnabled={backend.audioCapability}
              voiceAutoSend={voiceAutoSend}
              voiceCallConnecting={voiceCall.isConnecting}
              onStartVoiceCall={voiceCall.startCall}
              onSend={handleSend}
              onVoiceMessage={handleVoiceMessage}
            />
          )}
        </Box>
      </Container>
    </Box>
  );
}
