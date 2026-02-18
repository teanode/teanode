import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate, useParams } from '@tanstack/react-router';
import Box from '@mui/material/Box';
import Container from '@mui/material/Container';
import IconButton from '@mui/material/IconButton';
import MenuItem from '@mui/material/MenuItem';
import ListSubheader from '@mui/material/ListSubheader';
import Select from '@mui/material/Select';
import SendRounded from '@mui/icons-material/SendRounded';
import { useAppContext } from '../../../context';
import type { ModelInfo } from '../../../types';

/** /conversations/$agentId/ — new conversation page with centered input. */
export default function ConversationsNewPage() {
  const { t } = useTranslation();
  const { agentId } = useParams({ strict: false }) as { agentId: string };
  const { backend } = useAppContext();
  const agent = backend.agents.find((agent) => agent.id === agentId);
  const agentName = agent?.name || agentId;
  const navigate = useNavigate();
  const textareaRef = useRef<HTMLTextAreaElement>(null);

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

  const draftKey = 'new';

  // Restore draft on mount.
  useEffect(() => {
    const element = textareaRef.current;
    if (!element) return;
    const saved = localStorage.getItem(`draft:${draftKey}`);
    if (saved) {
      element.value = saved;
      element.style.height = 'auto';
      element.style.height = Math.min(element.scrollHeight, 300) + 'px';
    }
  }, []);

  const handleSend = useCallback(() => {
    const element = textareaRef.current;
    if (!element) return;
    const text = element.value.trim();
    if (!text) return;
    backend.sendMessage(text, selectedModel || undefined);
    element.value = '';
    element.style.height = 'auto';
    localStorage.removeItem(`draft:${draftKey}`);
  }, [backend.sendMessage, selectedModel]);

  const handleKeyDown = useCallback(
    (event: React.KeyboardEvent) => {
      if (event.key === 'Enter' && !event.shiftKey) {
        event.preventDefault();
        handleSend();
      }
    },
    [handleSend]
  );

  const handleInput = useCallback(() => {
    const element = textareaRef.current;
    if (!element) return;
    element.style.height = 'auto';
    element.style.height = Math.min(element.scrollHeight, 300) + 'px';
    if (element.value) {
      localStorage.setItem(`draft:${draftKey}`, element.value);
    } else {
      localStorage.removeItem(`draft:${draftKey}`);
    }
  }, []);

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

  return (
    <Box sx={{ flex: 1, display: 'flex', flexDirection: 'column' }}>
      <Container maxWidth="md" sx={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center', px: 2 }}>
        <Box sx={{ width: '100%' }}>
          <Box
            sx={{
              display: 'flex',
              flexDirection: 'column',
              bgcolor: 'surface2',
              borderRadius: 1.5,
              border: 1,
              borderColor: 'divider',
              px: 1.5,
              py: 1,
              gap: 0.5,
              width: '100%',
              '&:focus-within': {
                borderColor: 'primary.main',
              },
            }}
          >
            <Box
              component="textarea"
              ref={textareaRef}
              placeholder={t('conversations.startConversation', { agentName })}
              autoFocus
              onKeyDown={handleKeyDown}
              onInput={handleInput}
              sx={{
                width: '100%',
                minHeight: '2.625rem',
                maxHeight: 300,
                border: 'none',
                outline: 'none',
                bgcolor: 'transparent',
                color: 'text.primary',
                fontSize: '0.875rem',
                fontFamily: 'inherit',
                lineHeight: 1.5,
                resize: 'none',
                overflow: 'auto',
                py: 0.5,
                '&::placeholder': {
                  color: 'text.secondary',
                  opacity: 1,
                },
              }}
            />
            <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'flex-end', gap: 0.5 }}>
              {backend.models.length > 0 && (
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
              )}
              <IconButton
                size="small"
                color="primary"
                onClick={handleSend}
                sx={{
                  flexShrink: 0,
                  width: 32,
                  height: 32,
                }}
              >
                <SendRounded fontSize="small" />
              </IconButton>
            </Box>
          </Box>
        </Box>
      </Container>
    </Box>
  );
}
