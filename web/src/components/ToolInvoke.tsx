import React from 'react';
import { highlightJson } from '../markdown';

interface ToolInvokeProps {
  toolName: string;
  args: string;
}

export default function ToolInvoke({ toolName, args }: ToolInvokeProps) {
  return (
    <div className="self-start max-w-[75%] px-3 py-2 rounded-[8px] text-xs bg-tool-bg border border-[#3a3a20]">
      <span className="inline-block bg-accent-dim text-white text-[10px] font-semibold px-1.5 py-px rounded-[3px] uppercase font-mono tracking-wide mr-1.5 align-middle">
        {toolName}
      </span>
      <span>called</span>
      <pre className="text-dim font-mono text-[11px] mt-1 px-2 py-1.5 bg-black/20 rounded max-h-40 overflow-y-auto overflow-x-auto">
        <code
          className="hljs language-json text-[11px] font-mono bg-transparent p-0"
          dangerouslySetInnerHTML={{ __html: highlightJson(args) }}
        />
      </pre>
    </div>
  );
}
