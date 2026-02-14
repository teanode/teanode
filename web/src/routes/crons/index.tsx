import React from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';

/** /crons/ — empty state. */
export default function CronsIndex() {
  return (
    <Box sx={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
      <Typography variant="body2" color="text.secondary">Select a cron job or create a new one</Typography>
    </Box>
  );
}
