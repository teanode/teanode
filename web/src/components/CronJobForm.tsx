import React, { useState, useMemo } from 'react';
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
  { label: 'Every minute', value: '* * * * *' },
  { label: 'Every 5 min', value: '*/5 * * * *' },
  { label: 'Hourly', value: '0 * * * *' },
  { label: 'Daily 9am', value: '0 9 * * *' },
  { label: 'Weekdays 9am', value: '0 9 * * 1-5' },
  { label: 'Weekly Mon 9am', value: '0 9 * * 1' },
];

interface CronJobFormProps {
  initial?: CronJob;
  models?: ModelInfo[];
  agents?: AgentInfo[];
  onSave: (data: { name: string; schedule: string; message: string; model: string; agentId: string }) => void;
  onCancel: () => void;
}

export default function CronJobForm({ initial, models = [], agents = [], onSave, onCancel }: CronJobFormProps) {
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
    <MenuItem key="__default" value="">Default</MenuItem>,
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
          label="Name"
          size="small"
          fullWidth
          value={name}
          onChange={(event) => setName(event.target.value)}
          placeholder="Morning briefing"
          autoFocus
        />

        <Box>
          <TextField
            label="Schedule (cron expression)"
            size="small"
            fullWidth
            value={schedule}
            onChange={(event) => setSchedule(event.target.value)}
            placeholder="0 9 * * *"
            sx={{ '& .MuiInputBase-input': { fontFamily: 'monospace' } }}
          />
          <Box sx={{ display: 'flex', gap: 0.5, mt: 0.75, flexWrap: 'wrap' }}>
            {PRESETS.map((preset) => (
              <Chip
                key={preset.value}
                label={preset.label}
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
          label="Message"
          size="small"
          fullWidth
          multiline
          minRows={3}
          value={message}
          onChange={(event) => setMessage(event.target.value)}
          placeholder="Give me a morning briefing..."
        />

        {models.length > 0 ? (
          <TextField
            select
            label="Model (optional)"
            size="small"
            fullWidth
            value={model}
            onChange={(event) => setModel(event.target.value)}
          >
            {modelMenuItems}
          </TextField>
        ) : (
          <TextField
            label="Model (optional)"
            size="small"
            fullWidth
            value={model}
            onChange={(event) => setModel(event.target.value)}
            placeholder="Leave blank for default"
          />
        )}

        {agents.length > 1 && (
          <TextField
            select
            label="Agent (optional)"
            size="small"
            fullWidth
            value={agentId}
            onChange={(event) => setAgentId(event.target.value)}
          >
            <MenuItem value="">Default (main)</MenuItem>
            {agents.map((agent) => (
              <MenuItem key={agent.id} value={agent.id}>{agent.id}</MenuItem>
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
            {initial ? 'Save' : 'Create'}
          </Button>
          <Button variant="text" size="small" onClick={onCancel}>
            Cancel
          </Button>
        </Box>
      </Box>
    </Paper>
  );
}
