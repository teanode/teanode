import React, { useState, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import TextField from '@mui/material/TextField';
import Button from '@mui/material/Button';
import Chip from '@mui/material/Chip';
import MenuItem from '@mui/material/MenuItem';
import ListSubheader from '@mui/material/ListSubheader';
import Typography from '@mui/material/Typography';
import type { CronJob, ModelInfo, AgentInfo } from '../types';

const PRESETS = [
  { labelKey: 'cron.presetEveryMinute', value: '* * * * *' },
  { labelKey: 'cron.presetEvery5Min', value: '*/5 * * * *' },
  { labelKey: 'cron.presetHourly', value: '0 * * * *' },
  { labelKey: 'cron.presetDaily9am', value: '0 9 * * *' },
  { labelKey: 'cron.presetWeekdays9am', value: '0 9 * * 1-5' },
  { labelKey: 'cron.presetWeeklyMon9am', value: '0 9 * * 1' },
];

interface CronJobFormProps {
  initial?: CronJob;
  models?: ModelInfo[];
  agents?: AgentInfo[];
  onSave: (data: { name: string; schedule: string; message: string; model: string; agentId: string }) => void;
  onCancel: () => void;
}

export default function CronJobForm({ initial, models = [], agents = [], onSave, onCancel }: CronJobFormProps) {
  const { t } = useTranslation();
  const [name, setName] = useState(initial?.name || '');
  const [schedule, setSchedule] = useState(initial?.schedule || '0 * * * *');
  const [message, setMessage] = useState(initial?.message || '');
  const [model, setModel] = useState(initial?.model || '');
  const [agentId, setAgentId] = useState(initial?.agentId || '');

  const grouped = useMemo(() => {
    const map = new Map<string, ModelInfo[]>();
    for (const modelInfo of models) {
      const list = map.get(modelInfo.provider) || [];
      list.push(modelInfo);
      map.set(modelInfo.provider, list);
    }
    return map;
  }, [models]);

  const handleSubmit = (event: React.FormEvent) => {
    event.preventDefault();
    if (!name.trim() || !schedule.trim() || !message.trim()) return;
    onSave({ name: name.trim(), schedule: schedule.trim(), message: message.trim(), model: model.trim(), agentId: agentId.trim() });
  };

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

  return (
    <Paper
      component="form"
      variant="outlined"
      onSubmit={handleSubmit}
      sx={{ p: 2, bgcolor: 'background.paper' }}
    >
      <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
        <TextField
          label={t('cron.name')}
          size="small"
          fullWidth
          value={name}
          onChange={(event) => setName(event.target.value)}
          placeholder={t('cron.namePlaceholder')}
          autoFocus
        />

        <Box>
          <TextField
            label={t('cron.scheduleLabel')}
            size="small"
            fullWidth
            value={schedule}
            onChange={(event) => setSchedule(event.target.value)}
            placeholder={t('cron.schedulePlaceholder')}
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
          label={t('cron.message')}
          size="small"
          fullWidth
          multiline
          minRows={3}
          value={message}
          onChange={(event) => setMessage(event.target.value)}
          placeholder={t('cron.messagePlaceholder')}
        />

        {models.length > 0 ? (
          <TextField
            select
            label={t('cron.modelOptional')}
            size="small"
            fullWidth
            value={model}
            onChange={(event) => setModel(event.target.value)}
          >
            {modelMenuItems}
          </TextField>
        ) : (
          <TextField
            label={t('cron.modelOptional')}
            size="small"
            fullWidth
            value={model}
            onChange={(event) => setModel(event.target.value)}
            placeholder={t('cron.modelPlaceholder')}
          />
        )}

        {agents.length > 1 && (
          <TextField
            select
            label={t('cron.agentOptional')}
            size="small"
            fullWidth
            value={agentId}
            onChange={(event) => setAgentId(event.target.value)}
          >
            <MenuItem value="">{t('cron.defaultAgent')}</MenuItem>
            {agents.map((agent) => (
              <MenuItem key={agent.id} value={agent.id}>{agent.name || agent.id}</MenuItem>
            ))}
          </TextField>
        )}

        <Box sx={{ display: 'flex', gap: 1 }}>
          <Button
            type="submit"
            variant="contained"
            size="small"
            disabled={!name.trim() || !schedule.trim() || !message.trim()}
          >
            {initial ? t('common.save') : t('common.create')}
          </Button>
          <Button variant="text" size="small" onClick={onCancel}>
            {t('common.cancel')}
          </Button>
        </Box>
      </Box>
    </Paper>
  );
}
