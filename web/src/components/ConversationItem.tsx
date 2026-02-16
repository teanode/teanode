import React from 'react';
import { useTranslation } from 'react-i18next';
import dayjs from 'dayjs';
import relativeTime from 'dayjs/plugin/relativeTime';
import ListItemButton from '@mui/material/ListItemButton';
import ListItemText from '@mui/material/ListItemText';
import IconButton from '@mui/material/IconButton';
import CloseIcon from '@mui/icons-material/Close';
import type { Conversation } from '../types';

dayjs.extend(relativeTime);

interface ConversationItemProps {
  conversation: Conversation;
  active: boolean;
  onClick: () => void;
  onDelete: () => void;
}

function displayLabel(conversation: Conversation): string {
  if (conversation.title) return conversation.title;
  const id = conversation.id;
  return id.length > 28 ? id.substring(0, 12) + '...' + id.substring(id.length - 8) : id;
}

export default function ConversationItem({ conversation, active, onClick, onDelete }: ConversationItemProps) {
  const { t } = useTranslation();
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
        primary={displayLabel(conversation)}
        secondary={conversation.lastActive ? dayjs(conversation.lastActive).fromNow() : undefined}
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
      <IconButton
        className="delete-btn"
        size="small"
        title={t('conversations.deleteConversationTooltip')}
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
