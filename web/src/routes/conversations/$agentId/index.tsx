import React, { useCallback, useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate, useParams } from '@tanstack/react-router';
import Box from '@mui/material/Box';
import Container from '@mui/material/Container';
import IconButton from '@mui/material/IconButton';
import SendRounded from '@mui/icons-material/SendRounded';
import { useAppContext } from '../../../context';

/** /conversations/$agentId/ — new conversation page with centered input. */
export default function ConversationsNewPage() {
  const { t } = useTranslation();
  const { agentId } = useParams({ strict: false }) as { agentId: string };
  const { backend } = useAppContext();
  const agent = backend.agents.find((agent) => agent.id === agentId);
  const agentName = agent?.name || agentId;
  const navigate = useNavigate();
  const textareaRef = useRef<HTMLTextAreaElement>(null);

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
      element.style.height = Math.min(element.scrollHeight, 150) + 'px';
    }
  }, []);

  const handleSend = useCallback(() => {
    const element = textareaRef.current;
    if (!element) return;
    const text = element.value.trim();
    if (!text) return;
    backend.sendMessage(text);
    element.value = '';
    element.style.height = 'auto';
    localStorage.removeItem(`draft:${draftKey}`);
  }, [backend.sendMessage]);

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
    element.style.height = Math.min(element.scrollHeight, 150) + 'px';
    if (element.value) {
      localStorage.setItem(`draft:${draftKey}`, element.value);
    } else {
      localStorage.removeItem(`draft:${draftKey}`);
    }
  }, []);

  return (
    <Box sx={{ flex: 1, display: 'flex', flexDirection: 'column' }}>
      <Container maxWidth="md" sx={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center', px: 2 }}>
          <Box
            sx={{
              display: 'flex',
              alignItems: 'flex-end',
              bgcolor: 'surface2',
              borderRadius: 1.5,
              border: 1,
              borderColor: 'divider',
              px: 1.5,
              py: 1,
              gap: 1,
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
              rows={2}
              autoFocus
              onKeyDown={handleKeyDown}
              onInput={handleInput}
              sx={{
                flex: 1,
                border: 'none',
                outline: 'none',
                bgcolor: 'transparent',
                color: 'text.primary',
                fontSize: '0.875rem',
                fontFamily: 'inherit',
                lineHeight: 1.5,
                resize: 'none',
                py: 0.5,
                '&::placeholder': {
                  color: 'text.secondary',
                  opacity: 1,
                },
              }}
            />
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
      </Container>
    </Box>
  );
}
