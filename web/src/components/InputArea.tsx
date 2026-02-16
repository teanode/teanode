import React, { useState, useRef, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import Box from '@mui/material/Box';
import Container from '@mui/material/Container';
import IconButton from '@mui/material/IconButton';
import SendRounded from '@mui/icons-material/SendRounded';
import StopRounded from '@mui/icons-material/StopRounded';

interface InputAreaProps {
  isRunning: boolean;
  agentName: string;
  onSend: (text: string) => void;
  onAbort: () => void;
}

export default function InputArea({
  isRunning,
  agentName,
  onSend,
  onAbort,
}: InputAreaProps) {
  const { t } = useTranslation();
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const [hasText, setHasText] = useState(false);

  const handleSend = useCallback(() => {
    const element = textareaRef.current;
    if (!element) return;
    const text = element.value.trim();
    if (!text) return;
    onSend(text);
    element.value = '';
    element.style.height = 'auto';
    setHasText(false);
  }, [onSend]);

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
    setHasText(!!element.value.trim());
  }, []);

  const showStop = isRunning && !hasText;

  return (
    <Container maxWidth="md" sx={{ py: 1.5 }}>
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
          '&:focus-within': {
            borderColor: 'primary.main',
          },
        }}
      >
        <Box
          component="textarea"
          ref={textareaRef}
          placeholder={t('conversations.reply', { agentName })}
          rows={1}
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
          color={showStop ? 'error' : 'primary'}
          onClick={showStop ? onAbort : handleSend}
          disabled={!showStop && !hasText}
          sx={{
            flexShrink: 0,
            width: 32,
            height: 32,
          }}
        >
          {showStop ? <StopRounded fontSize="small" /> : <SendRounded fontSize="small" />}
        </IconButton>
      </Box>
    </Container>
  );
}
