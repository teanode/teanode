import React, { useState, useMemo, useEffect, useCallback, useImperativeHandle } from 'react';
import { useTranslation } from 'react-i18next';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import TextField from '@mui/material/TextField';
import Button from '@mui/material/Button';
import Chip from '@mui/material/Chip';
import MenuItem from '@mui/material/MenuItem';
import ListSubheader from '@mui/material/ListSubheader';
import Typography from '@mui/material/Typography';
import type { Job, ModelInfo, AgentInfo } from '../types';

const PRESETS = [
  { labelKey: 'jobs.presetEveryMinute', value: '* * * * *' },
  { labelKey: 'jobs.presetEvery5Min', value: '*/5 * * * *' },
  { labelKey: 'jobs.presetHourly', value: '0 * * * *' },
  { labelKey: 'jobs.presetDaily9am', value: '0 9 * * *' },
  { labelKey: 'jobs.presetWeekdays9am', value: '0 9 * * 1-5' },
  { labelKey: 'jobs.presetWeeklyMon9am', value: '0 9 * * 1' },
];

interface JobFormProps {
  initial?: Job;
  models?: ModelInfo[];
  agents?: AgentInfo[];
  onSave: (data: { name: string; schedule: string; message: string; model: string; agentId: string }) => void;
  onCancel?: () => void;
  flat?: boolean;
  showActions?: boolean;
  onDirtyChange?: (dirty: boolean) => void;
  onCanSaveChange?: (canSave: boolean) => void;
}

export interface JobFormHandle {
  save: () => void;
  isDirty: () => boolean;
  canSave: () => boolean;
}

const JobForm = React.forwardRef<JobFormHandle, JobFormProps>(function JobForm({
  initial,
  models = [],
  agents = [],
  onSave,
  onCancel,
  flat = false,
  showActions = true,
  onDirtyChange,
  onCanSaveChange,
}, ref) {
  const { t } = useTranslation();
  const [name, setName] = useState(initial?.name || '');
  const [schedule, setSchedule] = useState(initial?.schedule || '0 * * * *');
  const [message, setMessage] = useState(initial?.message || '');
  const [model, setModel] = useState(initial?.model || '');
  const [agentId, setAgentId] = useState(initial?.agentId || '');

  useEffect(() => {
    setName(initial?.name || '');
    setSchedule(initial?.schedule || '0 * * * *');
    setMessage(initial?.message || '');
    setModel(initial?.model || '');
    setAgentId(initial?.agentId || '');
  }, [initial?.id]);

  const grouped = useMemo(() => {
    const map = new Map<string, ModelInfo[]>();
    for (const modelInfo of models) {
      const list = map.get(modelInfo.provider) || [];
      list.push(modelInfo);
      map.set(modelInfo.provider, list);
    }
    return map;
  }, [models]);

  const canSave = !!name.trim() && !!schedule.trim() && !!message.trim();
  const dirty = initial
    ? name !== (initial.name || '')
      || schedule !== (initial.schedule || '')
      || message !== (initial.message || '')
      || model !== (initial.model || '')
      || agentId !== (initial.agentId || '')
    : true;
  useEffect(() => {
    onDirtyChange?.(dirty);
  }, [dirty, onDirtyChange]);
  useEffect(() => {
    onCanSaveChange?.(canSave);
  }, [canSave, onCanSaveChange]);

  const saveDraft = useCallback(() => {
    if (!canSave) return;
    onSave({ name: name.trim(), schedule: schedule.trim(), message: message.trim(), model: model.trim(), agentId: agentId.trim() });
  }, [canSave, onSave, name, schedule, message, model, agentId]);

  const handleSubmit = useCallback((event: React.FormEvent) => {
    event.preventDefault();
    saveDraft();
  }, [saveDraft]);

  useImperativeHandle(ref, () => ({
    save: saveDraft,
    isDirty: () => dirty,
    canSave: () => canSave,
  }), [saveDraft, dirty, canSave]);

  // Build model select items with group headers.
  const modelMenuItems: React.ReactNode[] = [
    <MenuItem key="__default" value="">{t('common.default')}</MenuItem>,
  ];
  for (const [provider, providerModels] of grouped.entries()) {
    modelMenuItems.push(<ListSubheader key={`header-${provider}`}>{provider}</ListSubheader>);
    for (const modelInfo of providerModels) {
      const qualified = `${modelInfo.provider}:${modelInfo.id}`;
      modelMenuItems.push(
        <MenuItem key={qualified} value={qualified}>{modelInfo.id}</MenuItem>
      );
    }
  }

  const content = (
    <Box component="form" onSubmit={handleSubmit}>
      <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
        <TextField
          label={t('jobs.name')}
          size="small"
          fullWidth
          value={name}
          onChange={(event) => setName(event.target.value)}
          placeholder={t('jobs.namePlaceholder')}
        />

        <Box>
          <TextField
            label={t('jobs.scheduleLabel')}
            size="small"
            fullWidth
            value={schedule}
            onChange={(event) => setSchedule(event.target.value)}
            placeholder={t('jobs.schedulePlaceholder')}
            sx={{ '& .MuiInputBase-input': { fontFamily: 'monospace' } }}
          />
          <Box sx={{ display: 'flex', gap: 0.5, mt: 0.75, flexWrap: 'wrap' }}>
            {PRESETS.map((preset) => (
              <Chip
                key={preset.value}
                label={t(preset.labelKey)}
                size="small"
                variant={schedule === preset.value ? 'filled' : 'outlined'}
                color={schedule === preset.value ? 'primary' : 'default'}
                onClick={() => setSchedule(preset.value)}
                sx={{ fontSize: '10px' }}
              />
            ))}
          </Box>
        </Box>

        <TextField
          label={t('jobs.message')}
          size="small"
          fullWidth
          multiline
          minRows={3}
          value={message}
          onChange={(event) => setMessage(event.target.value)}
          placeholder={t('jobs.messagePlaceholder')}
        />

        {models.length > 0 ? (
          <TextField
            select
            label={t('jobs.modelOptional')}
            size="small"
            fullWidth
            value={model}
            onChange={(event) => setModel(event.target.value)}
          >
            {modelMenuItems}
          </TextField>
        ) : (
          <TextField
            label={t('jobs.modelOptional')}
            size="small"
            fullWidth
            value={model}
            onChange={(event) => setModel(event.target.value)}
            placeholder={t('jobs.modelPlaceholder')}
          />
        )}

        {agents.length > 1 && (
          <TextField
            select
            label={t('jobs.agentOptional')}
            size="small"
            fullWidth
            value={agentId}
            onChange={(event) => setAgentId(event.target.value)}
          >
            <MenuItem value="">{t('jobs.defaultAgent')}</MenuItem>
            {agents.map((agent) => (
              <MenuItem key={agent.id} value={agent.id}>{agent.name || agent.id}</MenuItem>
            ))}
          </TextField>
        )}

        {showActions && (
          <Box sx={{ display: 'flex', gap: 1 }}>
            <Button
              type="submit"
              variant="contained"
              size="small"
              disabled={!name.trim() || !schedule.trim() || !message.trim()}
            >
              {initial ? t('common.save') : t('common.create')}
            </Button>
            {onCancel && (
              <Button variant="text" size="small" onClick={onCancel}>
                {t('common.cancel')}
              </Button>
            )}
          </Box>
        )}
      </Box>
    </Box>
  );

  if (flat) return content;

  return (
    <Paper
      variant="outlined"
      sx={{ p: 2, bgcolor: 'background.paper' }}
    >
      {content}
    </Paper>
  );
});

export default JobForm;
