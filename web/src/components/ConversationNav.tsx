import React, { useState, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import Box from '@mui/material/Box';
import List from '@mui/material/List';
import ListItemButton from '@mui/material/ListItemButton';
import ListItemText from '@mui/material/ListItemText';
import Collapse from '@mui/material/Collapse';
import Button from '@mui/material/Button';
import Typography from '@mui/material/Typography';
import ChevronRightIcon from '@mui/icons-material/ChevronRight';
import ExpandMoreIcon from '@mui/icons-material/ExpandMore';

import type { Conversation, AgentInfo } from '../types';
import type { useBackend } from '../hooks/useBackend';
import ConversationItem from './ConversationItem';
import ConfirmDialog from './ConfirmDialog';

interface ConversationNavProps {
  backend: ReturnType<typeof useBackend>;
  activeAgentId: string;
  activeConversationId: string | null;
  onNavigate: (path: string) => void;
}

export default function ConversationNav({ backend, activeAgentId, activeConversationId, onNavigate }: ConversationNavProps) {
  const { t } = useTranslation();
  const { conversations: conversationList, agents } = backend;
  const defaultAgentId = agents.length > 0 ? agents[0].id : 'main';

  const conversationsByAgent = useMemo(() => {
    const groups = new Map<string, Conversation[]>();
    for (const agent of agents) {
      groups.set(agent.id, []);
    }
    for (const conversation of conversationList) {
      const agentId = conversation.agentId || defaultAgentId;
      const list = groups.get(agentId) || [];
      list.push(conversation);
      groups.set(agentId, list);
    }
    return groups;
  }, [conversationList, agents, defaultAgentId]);

  const agentIds = useMemo(() => {
    const ordered: string[] = agents.map((agent: AgentInfo) => agent.id);
    for (const agentId of conversationsByAgent.keys()) {
      if (!ordered.includes(agentId)) {
        ordered.push(agentId);
      }
    }
    return ordered;
  }, [agents, conversationsByAgent]);

  const [collapsedAgents, setCollapsedAgents] = useState<Set<string>>(new Set());
  const [pendingDelete, setPendingDelete] = useState<{ id: string; agentId: string; title: string } | null>(null);

  function toggleAgentCollapsed(agentId: string) {
    setCollapsedAgents((previous) => {
      const next = new Set(previous);
      if (next.has(agentId)) {
        next.delete(agentId);
      } else {
        next.add(agentId);
      }
      return next;
    });
  }

  function handleConfirmDelete() {
    if (!pendingDelete) return;
    backend.deleteConversation(pendingDelete.id, pendingDelete.agentId);
    if (activeConversationId === pendingDelete.id) {
      onNavigate(`/conversations/${activeAgentId}`);
    }
    setPendingDelete(null);
  }

  return (
    <>
      <Box sx={{ flex: 1, overflowY: 'auto', p: 1 }}>
        <List disablePadding>
          {agentIds.map((agentId) => {
            const agentConversations = conversationsByAgent.get(agentId) || [];
            const isCollapsed = collapsedAgents.has(agentId);
            const isActiveAgent = activeAgentId === agentId;

            const agent = agents.find((a: AgentInfo) => a.id === agentId);
            const displayName = agent?.name || agentId;

            return (
              <React.Fragment key={agentId}>
                <ListItemButton
                  dense
                  onClick={() => toggleAgentCollapsed(agentId)}
                  sx={{ borderRadius: 1, mb: 0.25 }}
                >
                  {isCollapsed
                    ? <ChevronRightIcon sx={{ fontSize: 14, mr: 0.5, color: 'text.secondary' }} />
                    : <ExpandMoreIcon sx={{ fontSize: 14, mr: 0.5, color: 'text.secondary' }} />
                  }
                  <ListItemText
                    primary={displayName}
                    primaryTypographyProps={{
                      variant: 'caption',
                      fontWeight: 500,
                      fontFamily: 'monospace',
                      color: isActiveAgent ? 'primary.main' : 'text.secondary',
                      noWrap: true,
                    }}
                  />
                  <Typography variant="caption" color="text.secondary" sx={{ fontVariantNumeric: 'tabular-nums' }}>
                    {agentConversations.length}
                  </Typography>
                </ListItemButton>

                <Collapse in={!isCollapsed}>
                  <Box sx={{ pl: 1 }}>
                    <ListItemButton
                      dense
                      onClick={() => onNavigate(`/conversations/${agentId}`)}
                      sx={{
                        borderRadius: 1,
                        mb: 0.25,
                        ...(isActiveAgent && !activeConversationId
                          ? { bgcolor: 'accentDim', color: '#fff', '&:hover': { bgcolor: 'accentDim' } }
                          : {}),
                      }}
                    >
                      <ListItemText
                        primary={t('conversations.newConversation')}
                        secondary={t('conversations.withAgent', { agentId })}
                        primaryTypographyProps={{
                          variant: 'caption',
                          fontSize: '13px',
                          color: isActiveAgent && !activeConversationId ? '#fff' : 'primary.main',
                        }}
                        secondaryTypographyProps={{
                          variant: 'caption',
                          fontSize: '10px',
                          noWrap: true,
                          color: isActiveAgent && !activeConversationId ? 'rgba(255,255,255,0.7)' : 'text.disabled',
                        }}
                      />
                    </ListItemButton>

                    {agentConversations.map((conversation) => (
                      <ConversationItem
                        key={conversation.id}
                        conversation={conversation}
                        active={conversation.id === activeConversationId}
                        onClick={() => onNavigate(`/conversations/${agentId}/${conversation.id}`)}
                        onDelete={() => setPendingDelete({ id: conversation.id, agentId, title: conversation.title || conversation.id })}
                      />
                    ))}
                  </Box>
                </Collapse>
              </React.Fragment>
            );
          })}
        </List>
      </Box>

      <ConfirmDialog
        open={!!pendingDelete}
        title={t('conversations.deleteConversationTitle')}
        message={t('conversations.deleteConversationMessage', { title: pendingDelete?.title })}
        confirmLabel={t('common.delete')}
        onConfirm={handleConfirmDelete}
        onClose={() => setPendingDelete(null)}
      />
    </>
  );
}
