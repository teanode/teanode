import React, { useRef, useCallback } from 'react';
import Box from '@mui/material/Box';
import IconButton from '@mui/material/IconButton';
import SendRounded from '@mui/icons-material/SendRounded';
import StopRounded from '@mui/icons-material/StopRounded';

interface InputAreaProps {
  isRunning: boolean;
  onSend: (text: string) => void;
  onAbort: () => void;
}

export default function InputArea({
  isRunning,
  onSend,
  onAbort,
}: InputAreaProps) {
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  const handleSend = useCallback(() => {
    const element = textareaRef.current;
    if (!element) return;
    const text = element.value.trim();
    if (!text || isRunning) return;
    onSend(text);
    element.value = '';
    element.style.height = 'auto';
  }, [isRunning, onSend]);

  const handleKeyDown = useCallback(
    (event: React.KeyboardEvent) => {
      if (event.key === 'Enter' && !event.shiftKey) {
        event.preventDefault();
        if (!isRunning) handleSend();
      }
    },
    [isRunning, handleSend]
  );

  const handleInput = useCallback(() => {
    const element = textareaRef.current;
    if (!element) return;
    element.style.height = 'auto';
    element.style.height = Math.min(element.scrollHeight, 150) + 'px';
  }, []);

  return (
    <Box sx={{ px: 2, py: 1.5, borderTop: 1, borderColor: 'divider', bgcolor: 'background.paper' }}>
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
          placeholder="Type a message..."
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
          color={isRunning ? 'error' : 'primary'}
          onClick={isRunning ? onAbort : handleSend}
          sx={{
            flexShrink: 0,
            width: 32,
            height: 32,
          }}
        >
          {isRunning ? <StopRounded fontSize="small" /> : <SendRounded fontSize="small" />}
        </IconButton>
      </Box>
    </Box>
  );
}
