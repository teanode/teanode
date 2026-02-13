import React from 'react';
import type { Session, CronJob } from '../types';
import SessionItem from './SessionItem';
import Logo from './Logo';

export type ViewMode = 'chat' | 'crons';

function relativeTime(ms: number): string {
  const diff = Date.now() - ms;
  if (diff < 60_000) return 'just now';
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m ago`;
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h ago`;
  return `${Math.floor(diff / 86_400_000)}d ago`;
}

interface SidebarProps {
  sessions: Session[];
  activeSessionKey: string | null;
  collapsed: boolean;
  activeView: ViewMode;
  navigate: (path: string) => void;
  onDeleteSession: (key: string) => void;
  cronJobs: CronJob[];
  activeCronJobId: string | null;
}

export default function Sidebar({
  sessions,
  activeSessionKey,
  collapsed,
  activeView,
  navigate,
  onDeleteSession,
  cronJobs,
  activeCronJobId,
}: SidebarProps) {
  return (
    <div
      className={`w-[260px] bg-surface border-r border-border flex flex-col flex-shrink-0 transition-[margin-left] duration-200 max-md:fixed max-md:left-0 max-md:top-0 max-md:h-full max-md:z-10 ${
        collapsed ? '-ml-[260px]' : ''
      }`}
    >
      <div className="p-4 border-b border-border flex items-center justify-between">
        <h1 className="text-base font-semibold text-accent">
          <Logo />
        </h1>
        {activeView === 'chat' ? (
          <button
            className="bg-accent-dim text-white border-none rounded-[8px] px-3 py-1.5 cursor-pointer text-[13px] hover:bg-accent"
            onClick={() => navigate('/chat')}
          >
            + New
          </button>
        ) : (
          <button
            className="bg-accent-dim text-white border-none rounded-[8px] px-3 py-1.5 cursor-pointer text-[13px] hover:bg-accent"
            onClick={() => navigate('/crons/new')}
          >
            + New
          </button>
        )}
      </div>
      <div className="flex border-b border-border">
        <button
          className={`flex-1 py-2 text-xs font-medium text-center transition-colors ${
            activeView === 'chat'
              ? 'text-accent border-b-2 border-accent'
              : 'text-muted hover:text-fg'
          }`}
          onClick={() => navigate(activeSessionKey ? `/chat/${activeSessionKey}` : '/chat')}
        >
          Chat
        </button>
        <button
          className={`flex-1 py-2 text-xs font-medium text-center transition-colors ${
            activeView === 'crons'
              ? 'text-accent border-b-2 border-accent'
              : 'text-muted hover:text-fg'
          }`}
          onClick={() => navigate(activeCronJobId ? `/crons/${activeCronJobId}` : '/crons')}
        >
          Cron Jobs
        </button>
      </div>
      {activeView === 'chat' ? (
        <div className="flex-1 overflow-y-auto p-2">
          {sessions.map((s) => (
            <SessionItem
              key={s.key}
              session={s}
              active={s.key === activeSessionKey}
              onClick={() => navigate(`/chat/${s.key}`)}
              onDelete={() => onDeleteSession(s.key)}
            />
          ))}
        </div>
      ) : (
        <div className="flex-1 overflow-y-auto p-2">
          {cronJobs.length === 0 && (
            <div className="text-xs text-muted text-center py-4">No cron jobs yet</div>
          )}
          {cronJobs.map((job) => (
            <div
              key={job.id}
              className={`px-2.5 py-2 rounded-[8px] cursor-pointer mb-0.5 text-[13px] ${
                activeCronJobId === job.id
                  ? 'bg-accent-dim text-white'
                  : 'text-dim hover:bg-surface2 hover:text-gray-200'
              }`}
              onClick={() => navigate(`/crons/${job.id}`)}
            >
              <div className="flex items-center gap-1.5">
                <span
                  className={`inline-block w-1.5 h-1.5 rounded-full flex-shrink-0 ${
                    job.enabled ? 'bg-green-400' : 'bg-border'
                  }`}
                />
                <span className="block whitespace-nowrap overflow-hidden text-ellipsis" title={job.name}>
                  {job.name}
                </span>
              </div>
              <div className={`text-[10px] mt-px pl-3 ${activeCronJobId === job.id ? 'opacity-80' : 'opacity-70'}`}>
                <span className="font-mono">{job.schedule}</span>
                {job.lastRun ? (
                  <span className="ml-1.5">{relativeTime(job.lastRun)}</span>
                ) : (
                  <span className="ml-1.5">never run</span>
                )}
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
