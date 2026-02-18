import React, { useState, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import Box from '@mui/material/Box';
import Container from '@mui/material/Container';
import Typography from '@mui/material/Typography';
import TextField from '@mui/material/TextField';
import Button from '@mui/material/Button';
import IconButton from '@mui/material/IconButton';
import InputAdornment from '@mui/material/InputAdornment';
import Paper from '@mui/material/Paper';
import VisibilityIcon from '@mui/icons-material/Visibility';
import VisibilityOffIcon from '@mui/icons-material/VisibilityOff';
import ContentCopyIcon from '@mui/icons-material/ContentCopy';
import ConfirmDialog from '../../components/ConfirmDialog';
import { useAppContext } from '../../context';

/** /settings/token — standalone auth token management page. */
export default function SettingsTokenPage() {
  const { t } = useTranslation();
  const { backend } = useAppContext();
  const [token, setToken] = useState('');
  const [showToken, setShowToken] = useState(false);
  const [copied, setCopied] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);

  useEffect(() => {
    if (backend.connected) {
      backend.sendRpc<{ token: string }>('auth.getToken', {})
        .then((result) => setToken(result.token || ''))
        .catch((error) => console.error('auth.getToken:', error));
    }
  }, [backend.connected, backend.sendRpc]);

  function handleRegenerate() {
    setConfirmOpen(false);
    backend.sendRpc<{ token: string }>('auth.regenerateToken', {})
      .then((result) => setToken(result.token))
      .catch((error) => console.error('auth.regenerateToken:', error));
  }

  function handleCopy() {
    navigator.clipboard.writeText(token).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    });
  }

  return (
    <Box sx={{ flex: 1, overflowY: 'auto' }}>
      <Container maxWidth="md" sx={{ py: { xs: 2, md: 3 } }}>
        <Box sx={{ mb: 3 }}>
          <Typography variant="subtitle1" sx={{ fontWeight: 600 }}>{t('auth.tokenTitle')}</Typography>
          <Typography variant="caption" color="text.secondary">{t('auth.tokenDescription')}</Typography>
        </Box>

        <Paper variant="outlined" sx={{ p: 2 }}>
          <TextField
            label={t('auth.tokenTitle')}
            helperText={t('auth.tokenDescription')}
            type={showToken ? 'text' : 'password'}
            value={token}
            size="small"
            fullWidth
            slotProps={{
              input: {
                readOnly: true,
                sx: { fontFamily: 'monospace', fontSize: '13px' },
                endAdornment: (
                  <InputAdornment position="end">
                    <IconButton size="small" onClick={handleCopy} edge="end" title="Copy">
                      <ContentCopyIcon fontSize="small" sx={{ color: copied ? 'primary.main' : undefined }} />
                    </IconButton>
                    <IconButton size="small" onClick={() => setShowToken(!showToken)} edge="end">
                      {showToken ? <VisibilityOffIcon fontSize="small" /> : <VisibilityIcon fontSize="small" />}
                    </IconButton>
                  </InputAdornment>
                ),
              },
            }}
          />
          <Button
            size="small"
            color="warning"
            variant="outlined"
            onClick={() => setConfirmOpen(true)}
            sx={{ mt: 1.5 }}
          >
            {t('auth.regenerateToken')}
          </Button>
        </Paper>

        <ConfirmDialog
          open={confirmOpen}
          title={t('auth.regenerateTokenTitle')}
          message={t('auth.regenerateTokenMessage')}
          confirmLabel={t('auth.regenerateToken')}
          onConfirm={handleRegenerate}
          onClose={() => setConfirmOpen(false)}
        />
      </Container>
    </Box>
  );
}
