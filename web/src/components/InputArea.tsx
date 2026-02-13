import React, { useRef, useCallback } from 'react';

interface InputAreaProps {
  isRunning: boolean;
  status: string;
  showToolCalls: boolean;
  onSend: (text: string) => void;
  onAbort: () => void;
  onToggleTools: () => void;
}

export default function InputArea({
  isRunning,
  status,
  showToolCalls,
  onSend,
  onAbort,
  onToggleTools,
}: InputAreaProps) {
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  const handleInput = useCallback(() => {
    const el = textareaRef.current;
    if (!el) return;
    el.style.height = 'auto';
    el.style.height = Math.min(el.scrollHeight, 200) + 'px';
  }, []);

  const handleSend = useCallback(() => {
    const el = textareaRef.current;
    if (!el) return;
    const text = el.value.trim();
    if (!text || isRunning) return;
    onSend(text);
    el.value = '';
    el.style.height = 'auto';
  }, [isRunning, onSend]);

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (e.key === 'Enter' && !e.shiftKey) {
        e.preventDefault();
        if (!isRunning) handleSend();
      }
    },
    [isRunning, handleSend]
  );

  return (
    <div className="px-4 py-3 border-t border-border bg-surface">
      <div className="flex gap-2">
        <textarea
          ref={textareaRef}
          className="flex-1 bg-surface2 border border-border rounded-[8px] px-3.5 py-2.5 text-gray-200 font-sans text-sm resize-none min-h-[42px] max-h-[200px] outline-none focus:border-accent-dim"
          rows={1}
          placeholder="Type a message..."
          onInput={handleInput}
          onKeyDown={handleKeyDown}
        />
        <button
          className={`border-none rounded-[8px] px-5 py-2.5 cursor-pointer font-semibold text-sm self-end min-w-[70px] text-white hover:opacity-90 disabled:opacity-40 disabled:cursor-not-allowed ${
            isRunning ? 'bg-danger' : 'bg-accent'
          }`}
          onClick={isRunning ? onAbort : handleSend}
        >
          {isRunning ? 'Stop' : 'Send'}
        </button>
      </div>
      <div className="flex items-center justify-between pt-1">
        <span className="text-[11px] text-dim">{status}</span>
        <button
          className="bg-transparent border-none text-dim text-[11px] cursor-pointer font-sans p-0 hover:text-gray-200"
          onClick={onToggleTools}
        >
          {showToolCalls ? 'Hide tool calls' : 'Show tool calls'}
        </button>
      </div>
    </div>
  );
}
