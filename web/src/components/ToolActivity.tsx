import React from 'react';
import { useTranslation } from 'react-i18next';
import Box from '@mui/material/Box';
import CircularProgress from '@mui/material/CircularProgress';
import Typography from '@mui/material/Typography';

interface ToolActivityProps {
  toolName: string;
}

export default function ToolActivity({ toolName }: ToolActivityProps) {
  const { t } = useTranslation();
  return (
    <Box sx={{ alignSelf: 'flex-start', px: 1.5, py: 0.5, display: 'flex', alignItems: 'center', gap: 1 }}>
      <CircularProgress size={12} color="primary" />
      <Typography variant="caption" color="text.secondary" sx={{ fontStyle: 'italic' }}>
        {t('conversations.callingTool', { toolName })}
      </Typography>
    </Box>
  );
}
