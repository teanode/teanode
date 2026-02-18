import React, { useState, useEffect, useRef, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import Box from '@mui/material/Box';
import Container from '@mui/material/Container';
import IconButton from '@mui/material/IconButton';
import SendRounded from '@mui/icons-material/SendRounded';
import StopRounded from '@mui/icons-material/StopRounded';

interface InputAreaProps {
  isRunning: boolean;
  agentName: string;
  draftKey?: string;
  model?: string | null;
  onSend: (text: string) => void;
  onAbort: () => void;
}

export default function InputArea({
  isRunning,
  agentName,
  draftKey,
  model,
  onSend,
  onAbort,
}: InputAreaProps) {
  const { t } = useTranslation();
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const [hasText, setHasText] = useState(false);
  const [focused, setFocused] = useState(false);
  const draftKeyRef = useRef(draftKey);
  draftKeyRef.current = draftKey;

  // Restore draft when draftKey changes (conversation switch).
  useEffect(() => {
    const element = textareaRef.current;
    if (!element) return;
    const saved = draftKey ? localStorage.getItem(`draft:${draftKey}`) : null;
    element.value = saved || '';
    element.style.height = 'auto';
    if (saved) {
      element.style.height = Math.min(element.scrollHeight, 150) + 'px';
    }
    setHasText(!!element.value.trim());
  }, [draftKey]);

  const handleSend = useCallback(() => {
    const element = textareaRef.current;
    if (!element) return;
    const text = element.value.trim();
    if (!text) return;
    onSend(text);
    element.value = '';
    element.style.height = 'auto';
    setHasText(false);
    if (draftKeyRef.current) {
      localStorage.removeItem(`draft:${draftKeyRef.current}`);
    }
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
    if (draftKeyRef.current) {
      if (element.value) {
        localStorage.setItem(`draft:${draftKeyRef.current}`, element.value);
      } else {
        localStorage.removeItem(`draft:${draftKeyRef.current}`);
      }
    }
  }, []);

  const showStop = isRunning && !hasText;

  // Extract the short model name (after the colon) for display.
  const displayModel = model ? (model.includes(':') ? model.split(':').slice(1).join(':') : model) : null;

  return (
    <Container maxWidth="md" sx={{ py: 1.5 }}>
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
          onFocus={() => setFocused(true)}
          onBlur={() => setFocused(false)}
          sx={{
            width: '100%',
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
        {(focused || showStop) && (
          <Box
            onMouseDown={(event: React.MouseEvent) => event.preventDefault()}
            sx={{ display: 'flex', alignItems: 'center', justifyContent: 'flex-end', gap: 0.5 }}
          >
            {displayModel && focused && (
              <Box
                component="span"
                sx={{
                  fontSize: '0.75rem',
                  color: 'text.secondary',
                }}
              >
                {displayModel}
              </Box>
            )}
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
        )}
      </Box>
    </Container>
  );
}
