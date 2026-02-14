import React from 'react';
import { highlightJson } from '../markdown';

interface ToolResultProps {
  toolName: string;
  content: string;
}

interface MediaInfo {
  base64?: string;
  mediaId?: string;
  format?: string;
}

const imageFormats = new Set(['png', 'jpeg', 'jpg', 'gif', 'webp']);

function detectMedia(content: string): MediaInfo | null {
  try {
    const parsed = JSON.parse(content);
    if (!parsed || typeof parsed !== 'object' || !parsed.format) return null;
    if (!imageFormats.has(parsed.format.toLowerCase())) return null;
    if (parsed.base64) return { base64: parsed.base64, format: parsed.format };
    if (parsed.mediaId) return { mediaId: parsed.mediaId, format: parsed.format };
    return null;
  } catch {
    return null;
  }
}

function escapeHtml(str: string): string {
  return String(str)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
}

export default function ToolResult({ toolName, content }: ToolResultProps) {
  const mediaInfo = detectMedia(content);

  if (mediaInfo) {
    const source = mediaInfo.base64
      ? `data:image/${mediaInfo.format};base64,${mediaInfo.base64}`
      : `/media/${mediaInfo.mediaId}`;

    return (
      <div className="self-start max-w-[75%] px-3 py-2 rounded-[8px] text-xs bg-[#161a10] border border-[#2a3a1a]">
        <span className="inline-block bg-[#2a3a1a] text-accent text-[10px] font-semibold px-1.5 py-px rounded-[3px] uppercase font-mono tracking-wide mr-1.5 align-middle">
          {toolName}
        </span>
        <span>result</span>
        <div className="mt-1 rounded overflow-hidden">
          <img
            src={source}
            alt={`${toolName} output`}
            className="max-w-full max-h-[400px] rounded"
            loading="lazy"
          />
        </div>
      </div>
    );
  }

  let isJson = false;
  try {
    JSON.parse(content);
    isJson = true;
  } catch {
    // not JSON
  }

  const inner = isJson ? highlightJson(content) : escapeHtml(content);

  return (
    <div className="self-start max-w-[75%] px-3 py-2 rounded-[8px] text-xs bg-[#161a10] border border-[#2a3a1a]">
      <span className="inline-block bg-[#2a3a1a] text-accent text-[10px] font-semibold px-1.5 py-px rounded-[3px] uppercase font-mono tracking-wide mr-1.5 align-middle">
        {toolName}
      </span>
      <span>result</span>
      <pre className="text-dim font-mono text-[11px] mt-1 px-2 py-1.5 bg-black/20 rounded max-h-40 overflow-y-auto overflow-x-auto">
        {isJson ? (
          <code
            className="hljs language-json text-[11px] font-mono bg-transparent p-0"
            dangerouslySetInnerHTML={{ __html: inner }}
          />
        ) : (
          <span dangerouslySetInnerHTML={{ __html: inner }} />
        )}
      </pre>
    </div>
  );
}
