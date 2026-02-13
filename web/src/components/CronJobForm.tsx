import React, { useState } from 'react';
import type { CronJob } from '../types';

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
  onSave: (data: { name: string; schedule: string; message: string; model: string }) => void;
  onCancel: () => void;
}

export default function CronJobForm({ initial, onSave, onCancel }: CronJobFormProps) {
  const [name, setName] = useState(initial?.name || '');
  const [schedule, setSchedule] = useState(initial?.schedule || '0 * * * *');
  const [message, setMessage] = useState(initial?.message || '');
  const [model, setModel] = useState(initial?.model || '');

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim() || !schedule.trim() || !message.trim()) return;
    onSave({ name: name.trim(), schedule: schedule.trim(), message: message.trim(), model: model.trim() });
  };

  return (
    <form onSubmit={handleSubmit} className="border border-border rounded-lg p-3 mb-3 bg-surface">
      <div className="mb-2">
        <label className="block text-xs text-muted mb-1">Name</label>
        <input
          className="w-full bg-bg border border-border rounded px-2 py-1.5 text-sm focus:outline-none focus:border-accent"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="Morning briefing"
          autoFocus
        />
      </div>
      <div className="mb-2">
        <label className="block text-xs text-muted mb-1">Schedule (cron expression)</label>
        <input
          className="w-full bg-bg border border-border rounded px-2 py-1.5 text-sm font-mono focus:outline-none focus:border-accent"
          value={schedule}
          onChange={(e) => setSchedule(e.target.value)}
          placeholder="0 9 * * *"
        />
        <div className="flex gap-1 mt-1 flex-wrap">
          {PRESETS.map((p) => (
            <button
              key={p.value}
              type="button"
              className={`text-[10px] px-1.5 py-0.5 rounded border ${
                schedule === p.value ? 'border-accent text-accent' : 'border-border text-muted hover:border-accent'
              }`}
              onClick={() => setSchedule(p.value)}
            >
              {p.label}
            </button>
          ))}
        </div>
      </div>
      <div className="mb-2">
        <label className="block text-xs text-muted mb-1">Message</label>
        <textarea
          className="w-full bg-bg border border-border rounded px-2 py-1.5 text-sm focus:outline-none focus:border-accent resize-y min-h-[60px]"
          value={message}
          onChange={(e) => setMessage(e.target.value)}
          placeholder="Give me a morning briefing..."
          rows={3}
        />
      </div>
      <div className="mb-3">
        <label className="block text-xs text-muted mb-1">Model (optional)</label>
        <input
          className="w-full bg-bg border border-border rounded px-2 py-1.5 text-sm focus:outline-none focus:border-accent"
          value={model}
          onChange={(e) => setModel(e.target.value)}
          placeholder="Leave blank for default"
        />
      </div>
      <div className="flex gap-2">
        <button
          type="submit"
          className="bg-accent text-white px-3 py-1.5 rounded text-sm hover:bg-accent-dim"
          disabled={!name.trim() || !schedule.trim() || !message.trim()}
        >
          {initial ? 'Save' : 'Create'}
        </button>
        <button
          type="button"
          className="px-3 py-1.5 rounded text-sm hover:bg-border"
          onClick={onCancel}
        >
          Cancel
        </button>
      </div>
    </form>
  );
}
