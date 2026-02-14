import React, { useState, useEffect } from 'react';
import type { CronJob, CronJobCreateParams, CronJobUpdateParams, ModelInfo } from '../types';
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

  // Reset editing when job changes
  useEffect(() => {
    setEditing(false);
  }, [job?.id]);

  if (creating) {
    return (
      <div className="flex-1 flex flex-col overflow-hidden">
        <div className="p-4 border-b border-border">
          <h2 className="text-sm font-semibold">New Cron Job</h2>
        </div>
        <div className="flex-1 overflow-y-auto p-4">
          <CronJobForm
            models={models}
            onSave={(data) => {
              const params: CronJobCreateParams = { name: data.name, schedule: data.schedule, message: data.message };
              if (data.model) params.model = data.model;
              onCreate(params).catch(() => {});
            }}
            onCancel={onCancelCreate}
          />
        </div>
      </div>
    );
  }

  if (!job) {
    return (
      <div className="flex-1 flex items-center justify-center">
        <div className="text-sm text-muted">Select a cron job or create a new one</div>
      </div>
    );
  }

  if (editing) {
    return (
      <div className="flex-1 flex flex-col overflow-hidden">
        <div className="p-4 border-b border-border">
          <h2 className="text-sm font-semibold">Edit: {job.name}</h2>
        </div>
        <div className="flex-1 overflow-y-auto p-4">
          <CronJobForm
            initial={job}
            models={models}
            onSave={(data) => {
              const params: CronJobUpdateParams = { id: job.id };
              if (data.name !== job.name) params.name = data.name;
              if (data.schedule !== job.schedule) params.schedule = data.schedule;
              if (data.message !== job.message) params.message = data.message;
              if (data.model !== (job.model || '')) params.model = data.model;
              onUpdate(params).then(() => setEditing(false)).catch(() => {});
            }}
            onCancel={() => setEditing(false)}
          />
        </div>
      </div>
    );
  }

  // Detail view
  return (
    <div className="flex-1 flex flex-col overflow-hidden">
      <div className="p-4 border-b border-border flex items-center justify-between">
        <h2 className="text-sm font-semibold">{job.name}</h2>
        <div className="flex items-center gap-2">
          <button
            className="text-xs px-3 py-1.5 rounded bg-accent-dim text-white hover:bg-accent"
            onClick={() => onTrigger(job.id).then(() => onViewSession(job.sessionKey))}
          >
            Run Now
          </button>
          <button
            className="text-xs px-3 py-1.5 rounded hover:bg-border"
            onClick={() => setEditing(true)}
          >
            Edit
          </button>
          <button
            className="text-xs px-3 py-1.5 rounded text-red-400 hover:bg-red-900/20"
            onClick={() => onDelete(job.id)}
          >
            Delete
          </button>
        </div>
      </div>
      <div className="flex-1 overflow-y-auto p-4 space-y-4">
        {/* Status toggle */}
        <div className="flex items-center gap-3">
          <button
            className={`w-10 h-6 rounded-full relative transition-colors ${
              job.enabled ? 'bg-accent' : 'bg-border'
            }`}
            onClick={() => onUpdate({ id: job.id, enabled: !job.enabled })}
          >
            <span
              className={`absolute top-1 w-4 h-4 rounded-full bg-white transition-[left] ${
                job.enabled ? 'left-5' : 'left-1'
              }`}
            />
          </button>
          <span className="text-sm">{job.enabled ? 'Enabled' : 'Disabled'}</span>
        </div>

        {/* Details */}
        <div className="space-y-3">
          <div>
            <div className="text-xs text-muted mb-1">Schedule</div>
            <div className="text-sm font-mono">{job.schedule}</div>
          </div>
          <div>
            <div className="text-xs text-muted mb-1">Message</div>
            <div className="text-sm whitespace-pre-wrap bg-bg border border-border rounded p-3">{job.message}</div>
          </div>
          {job.model && (
            <div>
              <div className="text-xs text-muted mb-1">Model</div>
              <div className="text-sm font-mono">{job.model}</div>
            </div>
          )}
        </div>

        {/* Last run info */}
        <div className="border-t border-border pt-3">
          <div className="text-xs text-muted mb-2">Last Run</div>
          {job.lastRun ? (
            <div className="space-y-1">
              <div className="flex items-center gap-2 text-sm">
                <span>{relativeTime(job.lastRun)}</span>
                <span
                  className={`px-1.5 py-0.5 rounded text-[10px] font-medium ${
                    job.lastStatus === 'success'
                      ? 'bg-green-900/30 text-green-400'
                      : 'bg-red-900/30 text-red-400'
                  }`}
                >
                  {job.lastStatus}
                </span>
              </div>
              {job.lastError && (
                <div className="text-xs text-red-400">{job.lastError}</div>
              )}
            </div>
          ) : (
            <div className="text-sm text-muted">Never run</div>
          )}
        </div>

        {/* View session link */}
        <div className="border-t border-border pt-3">
          <button
            className="text-xs text-accent hover:underline"
            onClick={() => onViewSession(job.sessionKey)}
          >
            View session history
          </button>
        </div>
      </div>
    </div>
  );
}
