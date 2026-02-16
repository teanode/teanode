import React from 'react';
import dayjs from 'dayjs';
import relativeTime from 'dayjs/plugin/relativeTime';
import ListItemButton from '@mui/material/ListItemButton';
import ListItemText from '@mui/material/ListItemText';
import type { Conversation } from '../types';

dayjs.extend(relativeTime);

interface ConversationItemProps {
  conversation: Conversation;
  active: boolean;
  onClick: () => void;
}

function displayLabel(conversation: Conversation): string {
  if (conversation.title) return conversation.title;
  const id = conversation.id;
  return id.length > 28 ? id.substring(0, 12) + '...' + id.substring(id.length - 8) : id;
}

export default function ConversationItem({ conversation, active, onClick }: ConversationItemProps) {
  return (
    <ListItemButton
      dense
      onClick={onClick}
      sx={{
        borderRadius: 1,
        mb: 0.25,
        ...(active
          ? { bgcolor: 'accentDim', color: '#fff', '&:hover': { bgcolor: 'accentDim' } }
          : {}),
      }}
    >
      <ListItemText
        primary={displayLabel(conversation)}
        secondary={conversation.lastActive ? dayjs(conversation.lastActive).fromNow() : ''}
        primaryTypographyProps={{
          variant: 'caption',
          fontSize: '13px',
          noWrap: true,
          title: conversation.title || conversation.id,
          color: active ? '#fff' : 'text.secondary',
        }}
        secondaryTypographyProps={{
          variant: 'caption',
          fontSize: '10px',
          title: conversation.lastActive ? dayjs(conversation.lastActive).format('YYYY-MM-DD HH:mm:ss') : undefined,
          color: active ? 'rgba(255,255,255,0.7)' : 'text.disabled',
        }}
      />
    </ListItemButton>
  );
}
