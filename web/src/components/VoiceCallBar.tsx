import React from 'react';
import Box from '@mui/material/Box';
import Container from '@mui/material/Container';
import IconButton from '@mui/material/IconButton';
import Typography from '@mui/material/Typography';
import CallEndRounded from '@mui/icons-material/CallEndRounded';
import MicRounded from '@mui/icons-material/MicRounded';
import MicOffRounded from '@mui/icons-material/MicOffRounded';

interface VoiceCallBarProps {
  callDuration: number;
  isMuted: boolean;
  isUserSpeaking: boolean;
  isPlaying: boolean;
  isSynthesizing: boolean;
  onToggleMute: () => void;
  onEndCall: () => void;
  /** When true, renders without a Container wrapper. */
  bare?: boolean;
}

function formatDuration(seconds: number): string {
  const minutes = Math.floor(seconds / 60);
  const remainingSeconds = seconds % 60;
  return `${minutes}:${String(remainingSeconds).padStart(2, '0')}`;
}

function getStatusText(isUserSpeaking: boolean, isSynthesizing: boolean, isPlaying: boolean): string {
  if (isUserSpeaking) return 'Listening...';
  if (isSynthesizing && !isPlaying) return 'Thinking...';
  if (isPlaying) return 'Speaking...';
  return 'Connected';
}

export default function VoiceCallBar({
  callDuration,
  isMuted,
  isUserSpeaking,
  isPlaying,
  isSynthesizing,
  onToggleMute,
  onEndCall,
  bare,
}: VoiceCallBarProps) {
  const statusText = getStatusText(isUserSpeaking, isSynthesizing, isPlaying);

  const callBar = (
    <Box
      sx={{
        display: 'flex',
        alignItems: 'center',
        height: 48,
        px: 1.5,
        gap: 1.5,
        bgcolor: 'surface2',
        borderRadius: 1.5,
        border: 1,
        borderColor: 'divider',
        flexShrink: 0,
      }}
    >
      <IconButton
        size="small"
        onClick={onToggleMute}
        sx={{
          color: isMuted ? 'error.main' : 'text.primary',
        }}
      >
        {isMuted ? <MicOffRounded fontSize="small" /> : <MicRounded fontSize="small" />}
      </IconButton>

      <Box
        sx={{
          display: 'flex',
          alignItems: 'center',
          gap: 1,
          flex: 1,
          minWidth: 0,
        }}
      >
        {isUserSpeaking && (
          <Box
            sx={{
              width: 8,
              height: 8,
              borderRadius: '50%',
              bgcolor: 'success.main',
              flexShrink: 0,
              animation: 'pulse 1.5s infinite',
              '@keyframes pulse': {
                '0%, 100%': { opacity: 1 },
                '50%': { opacity: 0.4 },
              },
            }}
          />
        )}
        <Typography
          variant="body2"
          color="text.secondary"
          sx={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
        >
          {statusText}
        </Typography>
      </Box>

      <Typography variant="body2" color="text.secondary" sx={{ fontVariantNumeric: 'tabular-nums', flexShrink: 0 }}>
        {formatDuration(callDuration)}
      </Typography>

      <IconButton
        size="small"
        onClick={onEndCall}
        sx={{
          bgcolor: 'error.main',
          color: 'error.contrastText',
          '&:hover': { bgcolor: 'error.dark' },
          width: 32,
          height: 32,
        }}
      >
        <CallEndRounded fontSize="small" />
      </IconButton>
    </Box>
  );

  if (bare) return callBar;

  return (
    <Container maxWidth="md" sx={{ py: 1.5 }}>
      {callBar}
    </Container>
  );
}
