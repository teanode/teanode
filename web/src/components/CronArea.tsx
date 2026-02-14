import React, { useState, useEffect } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import Button from '@mui/material/Button';
import Switch from '@mui/material/Switch';
import Chip from '@mui/material/Chip';
import type { CronJob, CronJobCreateParams, CronJobUpdateParams, ModelInfo, AgentInfo } from '../types';
import CronJobForm from './CronJobForm';

function relativeTime(ms: number): string {
  const diff = Date.now() - ms;
  if (diff < 60_000) return 'just now';
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m ago`;
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h ago`;
  return `${Math.floor(diff / 86_400_000)}d ago`;
}

interface CronAreaProps {
  job: CronJob | null;
  creating: boolean;
  models: ModelInfo[];
  agents: AgentInfo[];
  onLoad: () => void;
  onCreate: (params: CronJobCreateParams) => Promise<void>;
  onUpdate: (params: CronJobUpdateParams) => Promise<void>;
  onDelete: (id: string) => Promise<void>;
  onTrigger: (id: string) => Promise<void>;
  onCancelCreate: () => void;
  onViewSession: (sessionKey: string) => void;
}

export default function CronArea({
  job,
  creating,
  models,
  agents,
  onLoad,
  onCreate,
  onUpdate,
  onDelete,
  onTrigger,
  onCancelCreate,
  onViewSession,
}: CronAreaProps) {
  const [editing, setEditing] = useState(false);

  useEffect(() => {
    onLoad();
  }, [onLoad]);

  useEffect(() => {
    setEditing(false);
  }, [job?.id]);

  if (creating) {
    return (
      <Box sx={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
        <Box sx={{ p: 2, borderBottom: 1, borderColor: 'divider' }}>
          <Typography variant="subtitle2" sx={{ fontWeight: 600 }}>New Cron Job</Typography>
        </Box>
        <Box sx={{ flex: 1, overflowY: 'auto', p: 2 }}>
          <CronJobForm
            models={models}
            agents={agents}
            onSave={(data) => {
              const params: CronJobCreateParams = { name: data.name, schedule: data.schedule, message: data.message };
              if (data.model) params.model = data.model;
              if (data.agentId) params.agentId = data.agentId;
              onCreate(params).catch(() => {});
            }}
            onCancel={onCancelCreate}
          />
        </Box>
      </Box>
    );
  }

  if (!job) {
    return (
      <Box sx={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
        <Typography variant="body2" color="text.secondary">Select a cron job or create a new one</Typography>
      </Box>
    );
  }

  if (editing) {
    return (
      <Box sx={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
        <Box sx={{ p: 2, borderBottom: 1, borderColor: 'divider' }}>
          <Typography variant="subtitle2" sx={{ fontWeight: 600 }}>Edit: {job.name}</Typography>
        </Box>
        <Box sx={{ flex: 1, overflowY: 'auto', p: 2 }}>
          <CronJobForm
            initial={job}
            models={models}
            agents={agents}
            onSave={(data) => {
              const params: CronJobUpdateParams = { id: job.id };
              if (data.name !== job.name) params.name = data.name;
              if (data.schedule !== job.schedule) params.schedule = data.schedule;
              if (data.message !== job.message) params.message = data.message;
              if (data.model !== (job.model || '')) params.model = data.model;
              if (data.agentId !== (job.agentId || '')) params.agentId = data.agentId;
              onUpdate(params).then(() => setEditing(false)).catch(() => {});
            }}
            onCancel={() => setEditing(false)}
          />
        </Box>
      </Box>
    );
  }

  return (
    <Box sx={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
      <Box sx={{ p: 2, borderBottom: 1, borderColor: 'divider', display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <Typography variant="subtitle2" sx={{ fontWeight: 600 }}>{job.name}</Typography>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
          <Button
            size="small"
            variant="contained"
            onClick={() => onTrigger(job.id).then(() => onViewSession(job.sessionKey))}
          >
            Run Now
          </Button>
          <Button size="small" variant="text" onClick={() => setEditing(true)}>
            Edit
          </Button>
          <Button size="small" color="error" variant="text" onClick={() => onDelete(job.id)}>
            Delete
          </Button>
        </Box>
      </Box>
      <Box sx={{ flex: 1, overflowY: 'auto', p: 2 }}>
        {/* Status toggle */}
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5, mb: 2 }}>
          <Switch
            checked={job.enabled}
            onChange={() => onUpdate({ id: job.id, enabled: !job.enabled })}
            color="primary"
          />
          <Typography variant="body2">{job.enabled ? 'Enabled' : 'Disabled'}</Typography>
        </Box>

        {/* Details */}
        <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1.5, mb: 2 }}>
          <Box>
            <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 0.5 }}>Schedule</Typography>
            <Typography variant="body2" sx={{ fontFamily: 'monospace' }}>{job.schedule}</Typography>
          </Box>
          <Box>
            <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 0.5 }}>Message</Typography>
            <Typography
              variant="body2"
              sx={{ whiteSpace: 'pre-wrap', bgcolor: 'background.default', border: 1, borderColor: 'divider', borderRadius: 1, p: 1.5 }}
            >
              {job.message}
            </Typography>
          </Box>
          {job.model && (
            <Box>
              <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 0.5 }}>Model</Typography>
              <Typography variant="body2" sx={{ fontFamily: 'monospace' }}>{job.model}</Typography>
            </Box>
          )}
          {job.agentId && (
            <Box>
              <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 0.5 }}>Agent</Typography>
              <Typography variant="body2" sx={{ fontFamily: 'monospace' }}>{job.agentId}</Typography>
            </Box>
          )}
        </Box>

        {/* Last run info */}
        <Box sx={{ borderTop: 1, borderColor: 'divider', pt: 1.5, mb: 2 }}>
          <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 1 }}>Last Run</Typography>
          {job.lastRun ? (
            <Box sx={{ display: 'flex', flexDirection: 'column', gap: 0.5 }}>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                <Typography variant="body2">{relativeTime(job.lastRun)}</Typography>
                <Chip
                  label={job.lastStatus}
                  size="small"
                  color={job.lastStatus === 'success' ? 'success' : 'error'}
                  sx={{ height: 20, fontSize: '10px' }}
                />
              </Box>
              {job.lastError && (
                <Typography variant="caption" color="error.main">{job.lastError}</Typography>
              )}
            </Box>
          ) : (
            <Typography variant="body2" color="text.secondary">Never run</Typography>
          )}
        </Box>

        {/* View session link */}
        <Box sx={{ borderTop: 1, borderColor: 'divider', pt: 1.5 }}>
          <Button size="small" color="primary" variant="text" onClick={() => onViewSession(job.sessionKey)}>
            View session history
          </Button>
        </Box>
      </Box>
    </Box>
  );
}
