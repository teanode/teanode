import React from 'react';
import { useTranslation } from 'react-i18next';
import Box from '@mui/material/Box';
import Chip from '@mui/material/Chip';
import Typography from '@mui/material/Typography';
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
  const { t } = useTranslation();
  const mediaInfo = detectMedia(content);

  const resultBorderColor = (theme: any) => theme.palette.mode === 'dark' ? '#2a3a1a' : '#c5d5a5';
  const resultBgColor = (theme: any) => theme.palette.mode === 'dark' ? '#161a10' : '#f0f5e5';

  if (mediaInfo) {
    const source = mediaInfo.base64
      ? `data:image/${mediaInfo.format};base64,${mediaInfo.base64}`
      : `/api/media/${mediaInfo.mediaId}`;

    return (
      <Box
        sx={{
          alignSelf: 'flex-start',
          maxWidth: '75%',
          px: 1.5,
          py: 1,
          borderRadius: 1,
          fontSize: '0.75rem',
          bgcolor: resultBgColor,
          border: 1,
          borderColor: resultBorderColor,
        }}
      >
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75, mb: 0.5 }}>
          <Chip
            label={toolName}
            size="small"
            variant="outlined"
            color="primary"
            sx={{ height: 18, fontSize: '10px', fontWeight: 600, fontFamily: 'monospace', textTransform: 'uppercase', letterSpacing: '0.05em' }}
          />
          <Typography variant="caption">{t('tool.result')}</Typography>
        </Box>
        <Box sx={{ borderRadius: 0.5, overflow: 'hidden' }}>
          <img
            src={source}
            alt={t('tool.outputAlt', { toolName })}
            style={{ maxWidth: '100%', maxHeight: 400, borderRadius: 4 }}
            loading="lazy"
          />
        </Box>
      </Box>
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
    <Box
      sx={{
        alignSelf: 'flex-start',
        maxWidth: '75%',
        px: 1.5,
        py: 1,
        borderRadius: 1,
        fontSize: '0.75rem',
        bgcolor: resultBgColor,
        border: 1,
        borderColor: resultBorderColor,
      }}
    >
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75 }}>
        <Chip
          label={toolName}
          size="small"
          variant="outlined"
          color="primary"
          sx={{ height: 18, fontSize: '10px', fontWeight: 600, fontFamily: 'monospace', textTransform: 'uppercase', letterSpacing: '0.05em' }}
        />
        <Typography variant="caption">{t('tool.result')}</Typography>
      </Box>
      <Box
        component="pre"
        sx={{
          color: 'text.secondary',
          fontFamily: 'monospace',
          fontSize: '11px',
          mt: 0.5,
          px: 1,
          py: 0.75,
          bgcolor: (theme) => theme.palette.mode === 'dark' ? 'rgba(0,0,0,0.15)' : 'rgba(0,0,0,0.05)',
          borderRadius: 0.5,
          maxHeight: 160,
          overflowY: 'auto',
          overflowX: 'auto',
          m: 0,
        }}
      >
        {isJson ? (
          <code
            className="hljs language-json"
            style={{ fontSize: '11px', fontFamily: 'monospace', backgroundColor: 'transparent', padding: 0 }}
            dangerouslySetInnerHTML={{ __html: inner }}
          />
        ) : (
          <span dangerouslySetInnerHTML={{ __html: inner }} />
        )}
      </Box>
    </Box>
  );
}
