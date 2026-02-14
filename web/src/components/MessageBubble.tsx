import React from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import { renderMarkdown } from '../markdown';

interface MessageBubbleProps {
  role: 'user' | 'assistant';
  content: string;
  isStreaming?: boolean;
  streamText?: string;
  timestamp?: number;
}

function formatTime(timestamp: number): string {
  return new Date(timestamp).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}

export default function MessageBubble({ role, content, isStreaming, streamText, timestamp }: MessageBubbleProps) {
  const isUser = role === 'user';

  const timeElement = timestamp ? (
    <Typography
      variant="caption"
      color="text.secondary"
      sx={{
        fontSize: '10px',
        userSelect: 'none',
        opacity: 0,
        transition: 'opacity 0.15s',
        whiteSpace: 'nowrap',
        '.message-row:hover &': { opacity: 1 },
      }}
    >
      {formatTime(timestamp)}
    </Typography>
  ) : null;

  let bubble: React.ReactNode;

  if (isUser) {
    bubble = (
      <Paper
        elevation={0}
        sx={{
          maxWidth: { xs: '95%', md: '85%' },
          px: 2,
          py: 1.5,
          lineHeight: 1.6,
          wordBreak: 'break-word',
          whiteSpace: 'pre-wrap',
          bgcolor: 'userBg',
          border: 1,
          borderColor: (theme) => theme.palette.mode === 'dark' ? '#3a4a1a' : '#c5d9a5',
        }}
      >
        {content}
      </Paper>
    );
  } else {
    const displayText = isStreaming ? (streamText ?? content) : content;

    if (displayText.startsWith('__error__:')) {
      const errorMessage = displayText.substring('__error__:'.length);
      bubble = (
        <Box sx={{ maxWidth: { xs: '95%', md: '85%' }, px: 2, py: 1.5, lineHeight: 1.6, wordBreak: 'break-word' }}>
          <Typography component="em" color="error.main">Error: {errorMessage}</Typography>
        </Box>
      );
    } else if (displayText === '__aborted__') {
      bubble = (
        <Box sx={{ maxWidth: { xs: '95%', md: '85%' }, px: 2, py: 1.5, lineHeight: 1.6, wordBreak: 'break-word' }}>
          <Typography component="em" color="text.secondary">Aborted</Typography>
        </Box>
      );
    } else if (!displayText) {
      return null;
    } else {
      bubble = (
        <Box
          className="markdown-content"
          sx={{ maxWidth: { xs: '95%', md: '85%' }, px: 2, py: 1.5, lineHeight: 1.6, wordBreak: 'break-word' }}
        >
          <div dangerouslySetInnerHTML={{ __html: renderMarkdown(displayText) }} />
        </Box>
      );
    }
  }

  return (
    <Box
      className="message-row"
      sx={{
        display: 'flex',
        flexDirection: isUser ? 'row-reverse' : 'row',
        alignItems: 'flex-end',
        gap: 1,
        alignSelf: isUser ? 'flex-end' : 'flex-start',
        maxWidth: '100%',
      }}
    >
      {bubble}
      {timeElement}
    </Box>
  );
}
