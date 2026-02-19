import React from 'react';
import { useTranslation } from 'react-i18next';
import Box from '@mui/material/Box';
import Chip from '@mui/material/Chip';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import InsertDriveFileRounded from '@mui/icons-material/InsertDriveFileRounded';
import { renderMarkdown } from '../markdown';
import type { Attachment } from '../types';

interface MessageBubbleProps {
  role: 'user' | 'assistant';
  content: string;
  isStreaming?: boolean;
  streamText?: string;
  timestamp?: number;
  attachments?: Attachment[];
}

function formatTime(timestamp: number): string {
  return new Date(timestamp).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}

function isImageFormat(format: string): boolean {
  return ['png', 'jpeg', 'jpg', 'gif', 'webp'].includes(format.toLowerCase());
}

function AttachmentDisplay({ attachment }: { attachment: Attachment }) {
  if (isImageFormat(attachment.format)) {
    return (
      <Box
        component="img"
        src={`/api/v1/media/${attachment.mediaId}`}
        alt={attachment.filename}
        sx={{
          maxWidth: 300,
          maxHeight: 200,
          borderRadius: 1,
          objectFit: 'contain',
          cursor: 'pointer',
        }}
        onClick={() => window.open(`/api/v1/media/${attachment.mediaId}`, '_blank')}
      />
    );
  }
  return (
    <Chip
      icon={<InsertDriveFileRounded />}
      label={attachment.filename || `${attachment.mediaId}.${attachment.format}`}
      size="small"
      variant="outlined"
      component="a"
      href={`/api/v1/media/${attachment.mediaId}`}
      target="_blank"
      clickable
      sx={{ maxWidth: 250 }}
    />
  );
}

export default function MessageBubble({ role, content, isStreaming, streamText, timestamp, attachments }: MessageBubbleProps) {
  const { t } = useTranslation();
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
        {attachments && attachments.length > 0 && (
          <Box sx={{ display: 'flex', gap: 1, flexWrap: 'wrap', mt: content ? 1 : 0 }}>
            {attachments.map((att, index) => (
              <AttachmentDisplay key={index} attachment={att} />
            ))}
          </Box>
        )}
      </Paper>
    );
  } else {
    const displayText = isStreaming ? (streamText ?? content) : content;

    if (displayText.startsWith('__error__:')) {
      const errorMessage = displayText.substring('__error__:'.length);
      bubble = (
        <Box sx={{ maxWidth: { xs: '95%', md: '85%' }, px: 2, py: 1.5, lineHeight: 1.6, wordBreak: 'break-word' }}>
          <Typography component="em" color="error.main">{t('conversations.error', { message: errorMessage })}</Typography>
        </Box>
      );
    } else if (displayText === '__aborted__') {
      bubble = (
        <Box sx={{ maxWidth: { xs: '95%', md: '85%' }, px: 2, py: 1.5, lineHeight: 1.6, wordBreak: 'break-word' }}>
          <Typography component="em" color="text.secondary">{t('conversations.aborted')}</Typography>
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
