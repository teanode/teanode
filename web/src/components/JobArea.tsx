import React, { useState, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import Button from '@mui/material/Button';
import Switch from '@mui/material/Switch';
import Chip from '@mui/material/Chip';
import type { Job, JobCreateParams, JobUpdateParams, ModelInfo, AgentInfo, Conversation } from '../types';
import JobForm from './JobForm';
import ConfirmDialog from './ConfirmDialog';

function relativeTime(ms: number, t: (key: string, options?: Record<string, unknown>) => string): string {
  const diff = Date.now() - ms;
  if (diff < 60_000) return t('jobs.justNow');
  if (diff < 3_600_000) return t('jobs.minutesAgo', { count: Math.floor(diff / 60_000) });
  if (diff < 86_400_000) return t('jobs.hoursAgo', { count: Math.floor(diff / 3_600_000) });
  return t('jobs.daysAgo', { count: Math.floor(diff / 86_400_000) });
}

interface JobAreaProps {
  job: Job | null;
  creating: boolean;
  models: ModelInfo[];
  agents: AgentInfo[];
  conversations: Conversation[];
  onLoad: () => void;
  onCreate: (params: JobCreateParams) => Promise<void>;
  onUpdate: (params: JobUpdateParams) => Promise<void>;
  onDelete: (id: string) => Promise<void>;
  onTrigger: (id: string) => Promise<void>;
  onCancelCreate: () => void;
  onViewConversation: (conversationId: string) => void;
}

export default function JobArea({
  job,
  creating,
  models,
  agents,
  conversations,
  onLoad,
  onCreate,
  onUpdate,
  onDelete,
  onTrigger,
  onCancelCreate,
  onViewConversation,
}: JobAreaProps) {
  const { t } = useTranslation();
  const [editing, setEditing] = useState(false);
  const [deleteConfirm, setDeleteConfirm] = useState(false);

  useEffect(() => {
    onLoad();
  }, [onLoad]);

  useEffect(() => {
    setEditing(false);
    setDeleteConfirm(false);
  }, [job?.id]);

  if (creating) {
    return (
      <Box sx={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
        <Box sx={{ p: 2, borderBottom: 1, borderColor: 'divider' }}>
          <Typography variant="subtitle2" sx={{ fontWeight: 600 }}>{t('jobs.newJob')}</Typography>
        </Box>
        <Box sx={{ flex: 1, overflowY: 'auto', p: 2 }}>
          <JobForm
            models={models}
            agents={agents}
            onSave={(data) => {
              const params: JobCreateParams = { name: data.name, schedule: data.schedule, message: data.message };
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
        <Typography variant="body2" color="text.secondary">{t('jobs.selectOrCreate')}</Typography>
      </Box>
    );
  }

  if (editing) {
    return (
      <Box sx={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
        <Box sx={{ p: 2, borderBottom: 1, borderColor: 'divider' }}>
          <Typography variant="subtitle2" sx={{ fontWeight: 600 }}>{t('jobs.editJob', { name: job.name })}</Typography>
        </Box>
        <Box sx={{ flex: 1, overflowY: 'auto', p: 2 }}>
          <JobForm
            initial={job}
            models={models}
            agents={agents}
            onSave={(data) => {
              const params: JobUpdateParams = { id: job.id };
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
          {!job.oneShot && (
            <Button
              size="small"
              variant="contained"
              onClick={() => onTrigger(job.id).then(() => onViewConversation(job.conversationId))}
            >
              {t('jobs.runNow')}
            </Button>
          )}
          {!job.oneShot && (
            <Button size="small" variant="text" onClick={() => setEditing(true)}>
              {t('common.edit')}
            </Button>
          )}
          <Button size="small" color="error" variant="text" onClick={() => setDeleteConfirm(true)}>
            {t('common.delete')}
          </Button>
        </Box>
      </Box>

      <ConfirmDialog
        open={deleteConfirm}
        title={t('common.delete')}
        message={t('jobs.deleteConfirm', { name: job.name })}
        confirmLabel={t('common.delete')}
        onConfirm={() => onDelete(job.id)}
        onClose={() => setDeleteConfirm(false)}
      />
      <Box sx={{ flex: 1, overflowY: 'auto', p: 2 }}>
        {/* Status toggle */}
        {!job.oneShot && (
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5, mb: 2 }}>
            <Switch
              checked={job.enabled}
              onChange={() => onUpdate({ id: job.id, enabled: !job.enabled })}
              color="primary"
            />
            <Typography variant="body2">{job.enabled ? t('jobs.enabled') : t('jobs.disabled')}</Typography>
          </Box>
        )}

        {/* Details */}
        <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1.5, mb: 2 }}>
          <Box>
            <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 0.5 }}>
              {job.runAt ? t('jobs.firesAt') : t('jobs.schedule')}
            </Typography>
            <Typography variant="body2" sx={{ fontFamily: 'monospace' }}>
              {job.runAt ? new Date(job.runAt).toLocaleString() : job.schedule}
            </Typography>
          </Box>
          <Box>
            <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 0.5 }}>{t('jobs.message')}</Typography>
            <Typography
              variant="body2"
              sx={{ whiteSpace: 'pre-wrap', bgcolor: 'background.default', border: 1, borderColor: 'divider', borderRadius: 1, p: 1.5 }}
            >
              {job.message}
            </Typography>
          </Box>
          {job.model && (
            <Box>
              <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 0.5 }}>{t('jobs.model')}</Typography>
              <Typography variant="body2" sx={{ fontFamily: 'monospace' }}>{job.model}</Typography>
            </Box>
          )}
          {job.agentId && (
            <Box>
              <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 0.5 }}>{t('jobs.agent')}</Typography>
              <Typography variant="body2" sx={{ fontFamily: 'monospace' }}>{job.agentId}</Typography>
            </Box>
          )}
        </Box>

        {/* Last run info */}
        <Box sx={{ borderTop: 1, borderColor: 'divider', pt: 1.5, mb: 2 }}>
          <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 1 }}>{t('jobs.lastRun')}</Typography>
          {job.lastRun ? (
            <Box sx={{ display: 'flex', flexDirection: 'column', gap: 0.5 }}>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                <Typography variant="body2">{relativeTime(job.lastRun, t)}</Typography>
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
            <Typography variant="body2" color="text.secondary">{t('jobs.neverRun')}</Typography>
          )}
        </Box>

        {/* View conversation link */}
        {job.conversationId && conversations.some((conversation) => conversation.id === job.conversationId) && (
          <Box sx={{ borderTop: 1, borderColor: 'divider', pt: 1.5 }}>
            <Button size="small" color="primary" variant="text" onClick={() => onViewConversation(job.conversationId)}>
              {t('jobs.viewHistory')}
            </Button>
          </Box>
        )}
      </Box>
    </Box>
  );
}
