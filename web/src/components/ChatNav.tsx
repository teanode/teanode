import React, { useState, useMemo } from 'react';
import Box from '@mui/material/Box';
import List from '@mui/material/List';
import ListItemButton from '@mui/material/ListItemButton';
import ListItemText from '@mui/material/ListItemText';
import Collapse from '@mui/material/Collapse';
import Button from '@mui/material/Button';
import Typography from '@mui/material/Typography';
import Dialog from '@mui/material/Dialog';
import DialogTitle from '@mui/material/DialogTitle';
import DialogContent from '@mui/material/DialogContent';
import DialogContentText from '@mui/material/DialogContentText';
import DialogActions from '@mui/material/DialogActions';
import ChevronRightIcon from '@mui/icons-material/ChevronRight';
import ExpandMoreIcon from '@mui/icons-material/ExpandMore';

import type { Session, AgentInfo } from '../types';
import type { useChat } from '../hooks/useChat';
import SessionItem from './SessionItem';

interface ChatNavProps {
  chat: ReturnType<typeof useChat>;
  activeAgentId: string;
  activeSessionKey: string | null;
  onNavigate: (path: string) => void;
}

export default function ChatNav({ chat, activeAgentId, activeSessionKey, onNavigate }: ChatNavProps) {
  const { sessions, agents } = chat;
  const defaultAgentId = agents.length > 0 ? agents[0].id : 'main';

  const sessionsByAgent = useMemo(() => {
    const groups = new Map<string, Session[]>();
    for (const agent of agents) {
      groups.set(agent.id, []);
    }
    for (const session of sessions) {
      const agentId = session.agentId || defaultAgentId;
      const list = groups.get(agentId) || [];
      list.push(session);
      groups.set(agentId, list);
    }
    return groups;
  }, [sessions, agents, defaultAgentId]);

  const agentIds = useMemo(() => {
    const ordered: string[] = agents.map((agent: AgentInfo) => agent.id);
    for (const agentId of sessionsByAgent.keys()) {
      if (!ordered.includes(agentId)) {
        ordered.push(agentId);
      }
    }
    return ordered;
  }, [agents, sessionsByAgent]);

  const [collapsedAgents, setCollapsedAgents] = useState<Set<string>>(new Set());
  const [pendingDelete, setPendingDelete] = useState<{ key: string; agentId: string; title: string } | null>(null);

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
    chat.deleteSession(pendingDelete.key, pendingDelete.agentId);
    if (activeSessionKey === pendingDelete.key) {
      onNavigate(`/chat/${activeAgentId}`);
    }
    setPendingDelete(null);
  }

  return (
    <>
      <Box sx={{ flex: 1, overflowY: 'auto', p: 1 }}>
        <List disablePadding>
          {agentIds.map((agentId) => {
            const agentSessions = sessionsByAgent.get(agentId) || [];
            const isCollapsed = collapsedAgents.has(agentId);
            const isActiveAgent = activeAgentId === agentId;

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
                    primary={agentId}
                    primaryTypographyProps={{
                      variant: 'caption',
                      fontWeight: 500,
                      fontFamily: 'monospace',
                      color: isActiveAgent ? 'primary.main' : 'text.secondary',
                      noWrap: true,
                    }}
                  />
                  <Typography variant="caption" color="text.secondary" sx={{ fontVariantNumeric: 'tabular-nums' }}>
                    {agentSessions.length}
                  </Typography>
                </ListItemButton>

                <Collapse in={!isCollapsed}>
                  <Box sx={{ pl: 1 }}>
                    <ListItemButton
                      dense
                      onClick={() => onNavigate(`/chat/${agentId}`)}
                      sx={{
                        borderRadius: 1,
                        mb: 0.25,
                        ...(isActiveAgent && !activeSessionKey
                          ? { bgcolor: 'accentDim', color: '#fff', '&:hover': { bgcolor: 'accentDim' } }
                          : {}),
                      }}
                    >
                      <ListItemText
                        primary="New Chat"
                        secondary={`with ${agentId}`}
                        primaryTypographyProps={{
                          variant: 'caption',
                          fontSize: '13px',
                          color: isActiveAgent && !activeSessionKey ? '#fff' : 'primary.main',
                        }}
                        secondaryTypographyProps={{
                          variant: 'caption',
                          fontSize: '10px',
                          noWrap: true,
                          color: isActiveAgent && !activeSessionKey ? 'rgba(255,255,255,0.7)' : 'text.disabled',
                        }}
                      />
                    </ListItemButton>

                    {agentSessions.map((session) => (
                      <SessionItem
                        key={session.key}
                        session={session}
                        active={session.key === activeSessionKey}
                        onClick={() => onNavigate(`/chat/${agentId}/${session.key}`)}
                        onDelete={() => setPendingDelete({ key: session.key, agentId, title: session.title || session.key })}
                      />
                    ))}
                  </Box>
                </Collapse>
              </React.Fragment>
            );
          })}
        </List>
      </Box>

      <Dialog open={!!pendingDelete} onClose={() => setPendingDelete(null)} maxWidth="xs" fullWidth>
        <DialogTitle sx={{ fontSize: '0.875rem', fontWeight: 500 }}>Delete session?</DialogTitle>
        <DialogContent>
          <DialogContentText sx={{ fontSize: '0.8rem', wordBreak: 'break-word' }}>
            This will permanently delete <strong>{pendingDelete?.title}</strong>.
          </DialogContentText>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setPendingDelete(null)} size="small">Cancel</Button>
          <Button onClick={handleConfirmDelete} color="error" variant="contained" size="small">Delete</Button>
        </DialogActions>
      </Dialog>
    </>
  );
}
