import React, { useState, useEffect, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import Box from '@mui/material/Box';
import Container from '@mui/material/Container';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Button from '@mui/material/Button';
import SchemaField from './SchemaField';
import ConfirmDialog from './ConfirmDialog';
import type { AgentConfig, ModelInfo, ConfigSchema } from '../types';

interface AgentEditorProps {
  agent: AgentConfig | null;
  models: ModelInfo[];
  schema: ConfigSchema | null;
  onSave: (agent: AgentConfig) => void;
  onDelete: (id: string) => void;
}

export default function AgentEditor({ agent, models, schema, onSave, onDelete }: AgentEditorProps) {
  const { t } = useTranslation();
  const [draft, setDraft] = useState<Record<string, unknown>>({});
  const [deleteConfirm, setDeleteConfirm] = useState(false);

  useEffect(() => {
    setDraft(agent ? { ...agent } : {});
    setDeleteConfirm(false);
  }, [agent?.id]);

  const getValue = useCallback(
    (dotPath: string): unknown => {
      const parts = dotPath.split('.');
      let current: unknown = draft;
      for (const part of parts) {
        if (current == null || typeof current !== 'object') return undefined;
        current = (current as Record<string, unknown>)[part];
      }
      return current;
    },
    [draft]
  );

  const setValue = useCallback(
    (dotPath: string, value: unknown) => {
      setDraft((previous) => {
        const parts = dotPath.split('.');
        const result = structuredClone(previous);
        let current: Record<string, unknown> = result;
        for (let index = 0; index < parts.length - 1; index++) {
          const part = parts[index];
          if (current[part] == null || typeof current[part] !== 'object') {
            current[part] = {};
          }
          current = current[part] as Record<string, unknown>;
        }
        current[parts[parts.length - 1]] = value;
        return result;
      });
    },
    []
  );

  if (!agent || !draft.id) {
    return (
      <Box sx={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
        <Typography variant="body2" color="text.secondary">{t('agent.selectToEdit')}</Typography>
      </Box>
    );
  }

  const isDirty = JSON.stringify(draft) !== JSON.stringify(agent);
  const modelSuggestions = models.map((modelInfo) => `${modelInfo.provider}:${modelInfo.id}`);

  return (
    <Box sx={{ flex: 1, overflowY: 'auto' }}>
      <Container maxWidth="md" sx={{ py: { xs: 2, md: 3 } }}>
        <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', mb: 3 }}>
          <Box>
            <Typography variant="subtitle1" sx={{ fontWeight: 600 }}>{agent.name || agent.id}</Typography>
            {agent.name && (
              <Typography variant="caption" color="text.secondary" sx={{ fontFamily: 'monospace' }}>{agent.id}</Typography>
            )}
          </Box>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
            {isDirty && (
              <Typography variant="caption" color="warning.main">{t('common.unsavedChanges')}</Typography>
            )}
            <Button
              variant={isDirty ? 'contained' : 'outlined'}
              size="small"
              disabled={!isDirty}
              onClick={() => onSave(draft as unknown as AgentConfig)}
            >
              {t('common.save')}
            </Button>
          </Box>
        </Box>

        {schema?.sections.map((section) => (
          <Paper key={section.id} variant="outlined" sx={{ p: 2, mb: 2 }}>
            <Typography variant="caption" color="text.secondary" sx={{ fontWeight: 500, display: 'block', mb: 1.5 }}>
              {section.label}
            </Typography>
            <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
              {section.fields.map((field) => {
                const isModelField =
                  field.type === 'string' &&
                  field.key.toLowerCase().includes('model');
                return (
                  <SchemaField
                    key={field.key}
                    field={field}
                    value={getValue(field.key)}
                    onChange={(value) => setValue(field.key, value)}
                    suggestions={isModelField ? modelSuggestions : undefined}
                  />
                );
              })}
            </Box>
          </Paper>
        ))}

        {/* Delete */}
        <Paper variant="outlined" sx={{ p: 2 }}>
          <Button size="small" color="error" onClick={() => setDeleteConfirm(true)}>
            {t('agent.deleteAgent')}
          </Button>
        </Paper>

        <ConfirmDialog
          open={deleteConfirm}
          title={t('agent.deleteAgent')}
          message={t('agent.deleteConfirm')}
          confirmLabel={t('agent.confirmDelete')}
          onConfirm={() => onDelete(agent.id)}
          onClose={() => setDeleteConfirm(false)}
        />
      </Container>
    </Box>
  );
}
