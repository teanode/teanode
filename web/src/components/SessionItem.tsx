import React from 'react';
import dayjs from 'dayjs';
import relativeTime from 'dayjs/plugin/relativeTime';
import type { Session } from '../types';

dayjs.extend(relativeTime);

interface SessionItemProps {
  session: Session;
  active: boolean;
  onClick: () => void;
  onDelete: () => void;
}

function displayLabel(s: Session): string {
  if (s.title) return s.title;
  const k = s.key;
  return k.length > 28 ? k.substring(0, 12) + '...' + k.substring(k.length - 8) : k;
}

export default function SessionItem({ session, active, onClick, onDelete }: SessionItemProps) {
  return (
    <div
      className={`group flex items-center px-2.5 py-2 rounded-[8px] cursor-pointer mb-0.5 text-[13px] ${
        active
          ? 'bg-accent-dim text-white'
          : 'text-dim hover:bg-surface2 hover:text-gray-200'
      }`}
      onClick={onClick}
    >
      <div className="flex-1 min-w-0">
        <span
          className="block whitespace-nowrap overflow-hidden text-ellipsis"
          title={session.title || session.key}
        >
          {displayLabel(session)}
        </span>
        {session.lastActive && (
          <span
            className={`block text-[10px] text-dim mt-px ${active ? 'opacity-80' : 'opacity-70'}`}
            title={dayjs(session.lastActive).format('YYYY-MM-DD HH:mm:ss')}
          >
            {dayjs(session.lastActive).fromNow()}
          </span>
        )}
      </div>
      <button
        className="hidden group-hover:block bg-transparent border-none text-dim cursor-pointer text-sm p-0 pl-1.5 leading-none flex-shrink-0 hover:text-danger"
        title="Delete session"
        onClick={(e) => {
          e.stopPropagation();
          onDelete();
        }}
      >
        &times;
      </button>
    </div>
  );
}
