import React, { useState } from 'react';
import { useTranslation } from 'react-i18next';
import Box from '@mui/material/Box';
import Container from '@mui/material/Container';
import Typography from '@mui/material/Typography';
import TextField from '@mui/material/TextField';
import Button from '@mui/material/Button';
import Paper from '@mui/material/Paper';
import Alert from '@mui/material/Alert';
import { useAppContext } from '../../context';

/** /settings/password — standalone password management page. */
export default function SettingsPasswordPage() {
  const { t } = useTranslation();
  const { backend } = useAppContext();
  const [currentPassword, setCurrentPassword] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [error, setError] = useState('');
  const [success, setSuccess] = useState('');
  const [loading, setLoading] = useState(false);

  function handleSubmit() {
    setError('');
    setSuccess('');

    if (newPassword.length < 8) {
      setError(t('auth.passwordMinLength'));
      return;
    }
    if (newPassword !== confirmPassword) {
      setError(t('auth.passwordsDoNotMatch'));
      return;
    }

    setLoading(true);
    backend.sendRpc('auth.changePassword', { currentPassword, newPassword })
      .then(() => {
        setSuccess(t('auth.passwordChanged'));
        setCurrentPassword('');
        setNewPassword('');
        setConfirmPassword('');
      })
      .catch((err) => {
        const message = (err as { message?: string })?.message || 'Failed';
        setError(message);
      })
      .finally(() => setLoading(false));
  }

  return (
    <Box sx={{ flex: 1, overflowY: 'auto' }}>
      <Container maxWidth="md" sx={{ py: { xs: 2, md: 3 } }}>
        <Box sx={{ mb: 3 }}>
          <Typography variant="subtitle1" sx={{ fontWeight: 600 }}>{t('auth.passwordTitle')}</Typography>
          <Typography variant="caption" color="text.secondary">{t('auth.passwordDescription')}</Typography>
        </Box>

        <Paper variant="outlined" sx={{ p: 2 }}>
          {error && <Alert severity="error" sx={{ mb: 1.5, fontSize: '0.8rem' }}>{error}</Alert>}
          {success && <Alert severity="success" sx={{ mb: 1.5, fontSize: '0.8rem' }}>{success}</Alert>}
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1.5 }}>
            <TextField
              label={t('auth.currentPassword')}
              type="password"
              value={currentPassword}
              onChange={(event) => setCurrentPassword(event.target.value)}
              size="small"
              fullWidth
              autoComplete="current-password"
            />
            <TextField
              label={t('auth.newPassword')}
              type="password"
              value={newPassword}
              onChange={(event) => setNewPassword(event.target.value)}
              size="small"
              fullWidth
              autoComplete="new-password"
            />
            <TextField
              label={t('auth.confirmNewPassword')}
              type="password"
              value={confirmPassword}
              onChange={(event) => setConfirmPassword(event.target.value)}
              size="small"
              fullWidth
              autoComplete="new-password"
            />
            <Box>
              <Button
                variant="contained"
                size="small"
                disabled={loading || !newPassword}
                onClick={handleSubmit}
              >
                {t('auth.changePassword')}
              </Button>
            </Box>
          </Box>
        </Paper>
      </Container>
    </Box>
  );
}
