import React from 'react';
import { useParams } from '@tanstack/react-router';
import Box from '@mui/material/Box';
import Container from '@mui/material/Container';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Button from '@mui/material/Button';
import { useAppContext } from '../../context';
import { useSettingsContext } from '../../hooks/useSettingsContext';
import SchemaField from '../../components/SchemaField';

/** /settings/$sectionId — individual config section page. */
export default function SettingsSectionPage() {
  const { sectionId } = useParams({ strict: false }) as { sectionId: string };
  const settings = useSettingsContext();
  const { chat } = useAppContext();

  if (settings.loading && !settings.schema) {
    return (
      <Box sx={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
        <Typography variant="body2" color="text.secondary">Loading settings...</Typography>
      </Box>
    );
  }

  const section = settings.schema?.sections.find(
    (candidate) => candidate.id === sectionId
  );

  if (!section) {
    return (
      <Box sx={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
        <Typography variant="body2" color="text.secondary">Section not found</Typography>
      </Box>
    );
  }

  const modelSuggestions = chat.models.map(
    (modelInfo) => `${modelInfo.provider}:${modelInfo.id}`
  );

  return (
    <Box sx={{ flex: 1, overflowY: 'auto' }}>
      <Container maxWidth="md" sx={{ py: { xs: 2, md: 3 } }}>
        <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', mb: 3 }}>
          <Box>
            <Typography variant="subtitle1" sx={{ fontWeight: 600 }}>{section.label}</Typography>
            {section.description && (
              <Typography variant="caption" color="text.secondary">{section.description}</Typography>
            )}
          </Box>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
            {settings.dirty && (
              <Typography variant="caption" color="warning.main">Unsaved changes</Typography>
            )}
            <Button
              variant={settings.dirty ? 'contained' : 'outlined'}
              size="small"
              disabled={!settings.dirty || settings.saving}
              onClick={settings.save}
            >
              {settings.saving ? 'Saving...' : 'Save'}
            </Button>
          </Box>
        </Box>

        <Paper variant="outlined" sx={{ p: 2 }}>
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
            {section.fields.map((field) => {
              const isModelField =
                field.type === 'string' &&
                field.key.toLowerCase().includes('model');
              return (
                <SchemaField
                  key={field.key}
                  field={field}
                  value={settings.getValue(field.key)}
                  onChange={(value) => settings.setValue(field.key, value)}
                  suggestions={isModelField ? modelSuggestions : undefined}
                />
              );
            })}
          </Box>
        </Paper>
      </Container>
    </Box>
  );
}
