import React, { useState, useRef, useEffect } from 'react';

interface TopBarProps {
  title: string;
  defaultModel: string;
  onToggleSidebar: () => void;
  onRename?: (title: string) => void;
  modelRef: React.RefObject<HTMLInputElement | null>;
}

export default function TopBar({ title, defaultModel, onToggleSidebar, onRename, modelRef }: TopBarProps) {
  const [editing, setEditing] = useState(false);
  const [editValue, setEditValue] = useState('');
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (editing && inputRef.current) {
      inputRef.current.focus();
      inputRef.current.select();
    }
  }, [editing]);

  function startEdit() {
    if (!onRename) return;
    setEditValue(title);
    setEditing(true);
  }

  function finishEdit(newTitle: string) {
    setEditing(false);
    const trimmed = newTitle.trim() || title;
    if (trimmed !== title && onRename) {
      onRename(trimmed);
    }
  }

  return (
    <div className="flex items-center px-3 py-2 border-b border-border bg-surface gap-2 flex-shrink-0">
      <button
        className="bg-transparent border-none text-dim cursor-pointer text-lg p-0.5 leading-none hover:text-gray-200"
        title="Toggle sidebar"
        onClick={onToggleSidebar}
      >
        &#9776;
      </button>

      {editing ? (
        <input
          ref={inputRef}
          className="text-[13px] text-gray-200 bg-surface2 border border-accent-dim rounded px-1.5 py-0.5 flex-1 outline-none font-sans"
          value={editValue}
          onChange={(e) => setEditValue(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault();
              finishEdit(editValue);
            } else if (e.key === 'Escape') {
              e.preventDefault();
              setEditing(false);
            }
          }}
          onBlur={() => finishEdit(editValue)}
        />
      ) : (
        <span
          className="text-[13px] text-dim flex-1 cursor-default"
          onDoubleClick={startEdit}
        >
          {title}
        </span>
      )}

      <input
        ref={modelRef}
        className="bg-surface2 border border-border rounded text-gray-200 text-xs font-mono px-2 py-0.5 w-[180px] outline-none focus:border-accent-dim"
        type="text"
        placeholder={defaultModel || 'model'}
        title="Model override (blank = default)"
      />
    </div>
  );
}
