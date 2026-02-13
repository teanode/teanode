import React from 'react';

interface UsageIndicatorProps {
  text: string;
}

export default function UsageIndicator({ text }: UsageIndicatorProps) {
  return (
    <div className="self-start text-[11px] text-dim font-mono px-4 py-0.5 opacity-60">
      {text}
    </div>
  );
}
