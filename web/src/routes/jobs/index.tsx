import React from 'react';
import { useTranslation } from 'react-i18next';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';

/** /jobs/ — empty state. */
export default function JobsIndex() {
  const { t } = useTranslation();
  return (
    <Box sx={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
      <Typography variant="body2" color="text.secondary">{t('jobs.selectOrCreate')}</Typography>
    </Box>
  );
}
