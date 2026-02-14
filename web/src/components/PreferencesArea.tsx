import React from 'react';
import Box from '@mui/material/Box';
import Container from '@mui/material/Container';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Switch from '@mui/material/Switch';
import FormControlLabel from '@mui/material/FormControlLabel';
import ToggleButton from '@mui/material/ToggleButton';
import ToggleButtonGroup from '@mui/material/ToggleButtonGroup';
import DarkModeIcon from '@mui/icons-material/DarkMode';
import LightModeIcon from '@mui/icons-material/LightMode';
import { useAppContext, type ThemeMode } from '../context';

export default function PreferencesArea() {
  const {
    themeMode,
    setThemeMode,
    showToolCalls,
    setShowToolCalls,
    showTokenUsage,
    setShowTokenUsage,
  } = useAppContext();

  return (
    <Box sx={{ flex: 1, overflowY: 'auto' }}>
      <Container maxWidth="md" sx={{ py: { xs: 2, md: 3 } }}>
        <Typography variant="subtitle1" sx={{ fontWeight: 600, mb: 3 }}>
          Preferences
        </Typography>

        {/* Theme */}
        <Paper variant="outlined" sx={{ p: 2, mb: 2 }}>
          <Typography variant="subtitle2" sx={{ fontWeight: 600, mb: 1 }}>
            Theme
          </Typography>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 1.5 }}>
            Choose between dark and light mode
          </Typography>
          <ToggleButtonGroup
            value={themeMode}
            exclusive
            onChange={(_event, value: ThemeMode | null) => {
              if (value) setThemeMode(value);
            }}
            size="small"
          >
            <ToggleButton value="dark" aria-label="Dark mode">
              <DarkModeIcon sx={{ fontSize: 18 }} />
            </ToggleButton>
            <ToggleButton value="light" aria-label="Light mode">
              <LightModeIcon sx={{ fontSize: 18 }} />
            </ToggleButton>
          </ToggleButtonGroup>
        </Paper>

        {/* Display */}
        <Paper variant="outlined" sx={{ p: 2 }}>
          <Typography variant="subtitle2" sx={{ fontWeight: 600, mb: 1 }}>
            Display
          </Typography>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 1.5 }}>
            Control what information is shown in the chat
          </Typography>
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1 }}>
            <FormControlLabel
              control={
                <Switch
                  checked={showToolCalls}
                  onChange={(event) => setShowToolCalls(event.target.checked)}
                  color="primary"
                />
              }
              label={
                <Box>
                  <Typography variant="body2" sx={{ fontWeight: 500 }}>Show tool calls</Typography>
                  <Typography variant="caption" color="text.secondary">
                    Display tool invocations and results in the chat
                  </Typography>
                </Box>
              }
              sx={{ alignItems: 'flex-start', ml: 0 }}
            />
            <FormControlLabel
              control={
                <Switch
                  checked={showTokenUsage}
                  onChange={(event) => setShowTokenUsage(event.target.checked)}
                  color="primary"
                />
              }
              label={
                <Box>
                  <Typography variant="body2" sx={{ fontWeight: 500 }}>Show token usage</Typography>
                  <Typography variant="caption" color="text.secondary">
                    Display token counts after each response
                  </Typography>
                </Box>
              }
              sx={{ alignItems: 'flex-start', ml: 0 }}
            />
          </Box>
        </Paper>
      </Container>
    </Box>
  );
}
