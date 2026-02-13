import React from 'react';

interface ToolActivityProps {
  toolName: string;
}

export default function ToolActivity({ toolName }: ToolActivityProps) {
  return (
    <div className="self-start px-3 py-1 text-xs text-dim italic flex items-center gap-1.5">
      <span className="spinner" />
      Calling {toolName}...
    </div>
  );
}
