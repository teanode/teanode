import React from 'react';
import Box from '@mui/material/Box';
import Container from '@mui/material/Container';
import { useAppContext } from '../../context';
import SessionsManager from '../../components/SessionsManager';

/** /settings/sessions — login session management page. */
export default function SettingsSessionsPage() {
  const { backend } = useAppContext();
  return (
    <Box sx={{ flex: 1, overflowY: 'auto' }}>
      <Container maxWidth="md" sx={{ py: { xs: 2, md: 3 } }}>
        <SessionsManager backend={backend} />
      </Container>
    </Box>
  );
}
