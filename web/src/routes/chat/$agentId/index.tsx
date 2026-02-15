import React, { useCallback, useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate, useParams } from '@tanstack/react-router';
import Box from '@mui/material/Box';
import IconButton from '@mui/material/IconButton';
import SendRounded from '@mui/icons-material/SendRounded';
import { useAppContext } from '../../../context';

/** /chat/$agentId/ — new chat page with centered input. */
export default function ChatNewPage() {
  const { t } = useTranslation();
  const { agentId } = useParams({ strict: false }) as { agentId: string };
  const { chat } = useAppContext();
  const navigate = useNavigate();
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  // Track whether the page is ready to accept a new session key.
  // Starts false; becomes true once any prior session has been cleared.
  const [ready, setReady] = useState(!chat.sessionKey);

  useEffect(() => {
    if (!ready) {
      chat.newSession();
      setReady(true);
    }
  }, [ready, chat.newSession]);

  // Navigate to the session page only when a NEW session key appears after we're ready.
  useEffect(() => {
    if (ready && chat.sessionKey) {
      navigate({
        to: '/chat/$agentId/$sessionKey',
        params: { agentId, sessionKey: chat.sessionKey },
        replace: true,
      });
    }
  }, [ready, chat.sessionKey, agentId, navigate]);

  const handleSend = useCallback(() => {
    const element = textareaRef.current;
    if (!element) return;
    const text = element.value.trim();
    if (!text) return;
    chat.sendMessage(text);
    element.value = '';
    element.style.height = 'auto';
  }, [chat.sendMessage]);

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
  }, []);

  return (
    <Box sx={{ flex: 1, display: 'flex', flexDirection: 'column' }}>
      <Box sx={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center', p: 2 }}>
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
              maxWidth: 600,
              '&:focus-within': {
                borderColor: 'primary.main',
              },
            }}
          >
            <Box
              component="textarea"
              ref={textareaRef}
              placeholder={t('chat.startConversation')}
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
      </Box>
    </Box>
  );
}
