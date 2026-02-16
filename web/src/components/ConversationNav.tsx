import React, { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import Box from '@mui/material/Box';
import List from '@mui/material/List';
import ListItemButton from '@mui/material/ListItemButton';
import ListItemText from '@mui/material/ListItemText';
import Typography from '@mui/material/Typography';
import AddIcon from '@mui/icons-material/Add';
import MoreHorizIcon from '@mui/icons-material/MoreHoriz';

import type { useBackend } from '../hooks/useBackend';
import ConversationItem from './ConversationItem';

const DEFAULT_RECENT_LIMIT = 10;

interface ConversationNavProps {
  backend: ReturnType<typeof useBackend>;
  viewingAgentId: string;
  activeConversationId: string | null;
  highlightViewAll: boolean;
  recentLimit?: number;
  onNavigate: (path: string) => void;
}

export default function ConversationNav({ backend, viewingAgentId, activeConversationId, highlightViewAll, recentLimit = DEFAULT_RECENT_LIMIT, onNavigate }: ConversationNavProps) {
  const { t } = useTranslation();
  const { conversations: conversationList, agents, serverActiveAgentId } = backend;
  const defaultAgentId = agents.length > 0 ? agents[0].id : 'main';

  const activeAgentId = serverActiveAgentId || defaultAgentId;
  const activeAgent = agents.find((agent) => agent.id === activeAgentId);
  const activeAgentConversationId = activeAgent?.activeConversationId || null;

  // Filter conversations for the active agent only.
  const agentConversations = useMemo(() => {
    return conversationList.filter((conversation) => {
      const conversationAgentId = conversation.agentId || defaultAgentId;
      return conversationAgentId === activeAgentId;
    });
  }, [conversationList, activeAgentId, defaultAgentId]);

  // Active conversation (pinned at top).
  const pinnedConversation = useMemo(() => {
    if (!activeAgentConversationId) return null;
    return agentConversations.find((conversation) => conversation.id === activeAgentConversationId) || null;
  }, [agentConversations, activeAgentConversationId]);

  // Recent conversations excluding the active one, limited.
  const recentConversations = useMemo(() => {
    return agentConversations
      .filter((conversation) => conversation.id !== activeAgentConversationId)
      .slice(0, recentLimit);
  }, [agentConversations, activeAgentConversationId, recentLimit]);

  const isNewConversationActive = !highlightViewAll && viewingAgentId === activeAgentId && !activeConversationId;

  return (
    <Box sx={{ flex: 1, overflowY: 'auto', p: 1 }}>
      <List disablePadding>
        <Box sx={{ mt: 1 }}>
          <ListItemButton
            dense
            onClick={() => onNavigate(`/conversations/${activeAgentId}`)}
            sx={{
              borderRadius: 1,
              mb: 0.25,
              ...(isNewConversationActive
                ? { bgcolor: 'accentDim', color: '#fff', '&:hover': { bgcolor: 'accentDim' } }
                : {}),
            }}
          >
            <AddIcon sx={{ fontSize: 14, mr: 0.5, color: isNewConversationActive ? '#fff' : 'text.secondary' }} />
            <ListItemText
              primary={t('conversations.newConversation')}
              primaryTypographyProps={{
                variant: 'caption',
                fontSize: '13px',
                color: isNewConversationActive ? '#fff' : 'primary.main',
              }}
            />
          </ListItemButton>
        </Box>

        {/* Active conversation */}
        {pinnedConversation && (
          <>
            <Typography variant="overline" sx={{ display: 'block', px: 1.25, mt: 0.5, mb: 0.25, fontSize: '10px', color: 'text.secondary', letterSpacing: '0.08em' }}>
              {t('conversations.activeConversation')}
            </Typography>
            <ConversationItem
              conversation={pinnedConversation}
              active={!highlightViewAll && pinnedConversation.id === activeConversationId}
              onClick={() => onNavigate(`/conversations/${activeAgentId}/${pinnedConversation.id}`)}
            />
          </>
        )}

        {/* Recent conversations */}
        {recentConversations.length > 0 && (
          <>
            <Typography variant="overline" sx={{ display: 'block', px: 1.25, mt: 1, mb: 0.25, fontSize: '10px', color: 'text.secondary', letterSpacing: '0.08em' }}>
              {t('conversations.recentConversations')}
            </Typography>
            {recentConversations.map((conversation) => (
              <ConversationItem
                key={conversation.id}
                conversation={conversation}
                active={!highlightViewAll && conversation.id === activeConversationId}
                onClick={() => onNavigate(`/conversations/${activeAgentId}/${conversation.id}`)}
              />
            ))}
          </>
        )}

        {/* Actions */}
        <Box sx={{ mt: 1 }}>
          <ListItemButton
            dense
            onClick={() => onNavigate('/conversations/all')}
            sx={{
              borderRadius: 1,
              mb: 0.25,
              ...(highlightViewAll
                ? { bgcolor: 'accentDim', color: '#fff', '&:hover': { bgcolor: 'accentDim' } }
                : {}),
            }}
          >
            <MoreHorizIcon sx={{ fontSize: 14, mr: 0.5, color: highlightViewAll ? '#fff' : 'text.secondary' }} />
            <ListItemText
              primary={t('conversations.viewMore')}
              primaryTypographyProps={{
                variant: 'caption',
                fontSize: '13px',
                color: highlightViewAll ? '#fff' : 'text.secondary',
              }}
            />
          </ListItemButton>

        </Box>
      </List>
    </Box>
  );
}
