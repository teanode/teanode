import React from 'react';
import dayjs from 'dayjs';
import relativeTime from 'dayjs/plugin/relativeTime';
import ListItemButton from '@mui/material/ListItemButton';
import ListItemText from '@mui/material/ListItemText';
import IconButton from '@mui/material/IconButton';
import CloseIcon from '@mui/icons-material/Close';
import type { Session } from '../types';

dayjs.extend(relativeTime);

interface SessionItemProps {
  session: Session;
  active: boolean;
  onClick: () => void;
  onDelete: () => void;
}

function displayLabel(session: Session): string {
  if (session.title) return session.title;
  const key = session.key;
  return key.length > 28 ? key.substring(0, 12) + '...' + key.substring(key.length - 8) : key;
}

export default function SessionItem({ session, active, onClick, onDelete }: SessionItemProps) {
  return (
    <ListItemButton
      dense
      onClick={onClick}
      sx={{
        borderRadius: 1,
        mb: 0.25,
        '& .delete-btn': { display: 'none' },
        '&:hover .delete-btn': { display: 'inline-flex' },
        ...(active
          ? { bgcolor: 'accentDim', color: '#fff', '&:hover': { bgcolor: 'accentDim' } }
          : {}),
      }}
    >
      <ListItemText
        primary={displayLabel(session)}
        secondary={session.lastActive ? dayjs(session.lastActive).fromNow() : undefined}
        primaryTypographyProps={{
          variant: 'caption',
          fontSize: '13px',
          noWrap: true,
          title: session.title || session.key,
          color: active ? '#fff' : 'text.secondary',
        }}
        secondaryTypographyProps={{
          variant: 'caption',
          fontSize: '10px',
          title: session.lastActive ? dayjs(session.lastActive).format('YYYY-MM-DD HH:mm:ss') : undefined,
          color: active ? 'rgba(255,255,255,0.7)' : 'text.disabled',
        }}
      />
      <IconButton
        className="delete-btn"
        size="small"
        title="Delete session"
        onClick={(event) => {
          event.stopPropagation();
          onDelete();
        }}
        sx={{ p: 0.25, ml: 0.5, flexShrink: 0, color: active ? 'rgba(255,255,255,0.7)' : 'text.disabled', '&:hover': { color: 'error.main' } }}
      >
        <CloseIcon sx={{ fontSize: 14 }} />
      </IconButton>
    </ListItemButton>
  );
}
