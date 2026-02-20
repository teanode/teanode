import React, { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import Box from '@mui/material/Box';
import List from '@mui/material/List';
import ListItemButton from '@mui/material/ListItemButton';
import ListItemText from '@mui/material/ListItemText';
import AddIcon from '@mui/icons-material/Add';
import MoreHorizIcon from '@mui/icons-material/MoreHoriz';

import type { useBackend } from '../hooks/useBackend';
import ConversationItem from './ConversationItem';
import SidebarSectionTitle from './SidebarSectionTitle';

const DEFAULT_RECENT_LIMIT = 10;

interface ConversationNavProps {
  backend: ReturnType<typeof useBackend>;
  viewingAgentId: string;
  viewingConversationId: string | null;
  highlightViewAll: boolean;
  recentLimit?: number;
  onNavigate: (path: string) => void;
}

export default function ConversationNav({ backend, viewingAgentId, viewingConversationId, highlightViewAll, recentLimit = DEFAULT_RECENT_LIMIT, onNavigate }: ConversationNavProps) {
  const { t } = useTranslation();
  const { conversations: conversationList, agents, serverDefaultAgentId } = backend;
  const fallbackAgentId = agents.length > 0 ? agents[0].id : 'main';

  const defaultAgentId = serverDefaultAgentId || fallbackAgentId;
  const defaultAgent = agents.find((agent) => agent.id === defaultAgentId);
  const defaultConversationId = defaultAgent?.defaultConversationId || null;

  // Filter conversations for the default agent.
  const agentConversations = useMemo(() => {
    return conversationList.filter((conversation) => {
      const conversationAgentId = conversation.agentId || fallbackAgentId;
      return conversationAgentId === defaultAgentId;
    });
  }, [conversationList, defaultAgentId, fallbackAgentId]);

  // Default conversation (pinned at top).
  const pinnedConversation = useMemo(() => {
    if (!defaultConversationId) return null;
    return agentConversations.find((conversation) => conversation.id === defaultConversationId) || null;
  }, [agentConversations, defaultConversationId]);

  // Recent conversations excluding the default, limited.
  const recentConversations = useMemo(() => {
    return agentConversations
      .filter((conversation) => conversation.id !== defaultConversationId)
      .slice(0, recentLimit);
  }, [agentConversations, defaultConversationId, recentLimit]);

  const isViewingNewConversation = !highlightViewAll && viewingAgentId === defaultAgentId && !viewingConversationId;

  return (
    <Box sx={{ flex: 1, overflowY: 'auto', p: 1 }}>
      <List disablePadding>
        <Box sx={{ mt: 1 }}>
          <ListItemButton
            dense
            onClick={() => onNavigate(`/conversations/${defaultAgentId}`)}
            sx={{
              borderRadius: 1,
              mb: 0.25,
              ...(isViewingNewConversation
                ? { bgcolor: 'accentDim', color: '#fff', '&:hover': { bgcolor: 'accentDim' } }
                : {}),
            }}
          >
            <AddIcon sx={{ fontSize: 14, mr: 0.5, color: isViewingNewConversation ? '#fff' : 'text.secondary' }} />
            <ListItemText
              primary={t('conversations.newConversation')}
              primaryTypographyProps={{
                variant: 'caption',
                fontSize: '13px',
                color: isViewingNewConversation ? '#fff' : 'primary.main',
              }}
            />
          </ListItemButton>
        </Box>

        {/* Default conversation */}
        {pinnedConversation && (
          <>
            <SidebarSectionTitle mt={0.5}>
              {t('conversations.defaultConversation')}
            </SidebarSectionTitle>
            <ConversationItem
              conversation={pinnedConversation}
              active={!highlightViewAll && pinnedConversation.id === viewingConversationId}
              onClick={() => onNavigate(`/conversations/${defaultAgentId}/${pinnedConversation.id}`)}
            />
          </>
        )}

        {/* Recent conversations */}
        {recentConversations.length > 0 && (
          <>
            <SidebarSectionTitle>
              {t('conversations.recentConversations')}
            </SidebarSectionTitle>
            {recentConversations.map((conversation) => (
              <ConversationItem
                key={conversation.id}
                conversation={conversation}
                active={!highlightViewAll && conversation.id === viewingConversationId}
                onClick={() => onNavigate(`/conversations/${defaultAgentId}/${conversation.id}`)}
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
