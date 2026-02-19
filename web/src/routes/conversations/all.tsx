import React, { useState, useMemo, useCallback, useRef, useEffect } from 'react';
import { useNavigate } from '@tanstack/react-router';
import { useTranslation } from 'react-i18next';
import Box from '@mui/material/Box';
import Container from '@mui/material/Container';
import Typography from '@mui/material/Typography';
import TextField from '@mui/material/TextField';
import Button from '@mui/material/Button';
import Checkbox from '@mui/material/Checkbox';
import List from '@mui/material/List';
import ListItemButton from '@mui/material/ListItemButton';
import ListItemText from '@mui/material/ListItemText';
import IconButton from '@mui/material/IconButton';
import Tooltip from '@mui/material/Tooltip';
import InputAdornment from '@mui/material/InputAdornment';
import SearchIcon from '@mui/icons-material/Search';
import DeleteIcon from '@mui/icons-material/Delete';
import EditIcon from '@mui/icons-material/Edit';
import RadioButtonCheckedIcon from '@mui/icons-material/RadioButtonChecked';
import RadioButtonUncheckedIcon from '@mui/icons-material/RadioButtonUnchecked';
import ChatIcon from '@mui/icons-material/ChatBubbleOutline';
import AddIcon from '@mui/icons-material/Add';
import dayjs from 'dayjs';
import relativeTime from 'dayjs/plugin/relativeTime';
import { useAppContext } from '../../context';
import ConfirmDialog from '../../components/ConfirmDialog';
import type { Conversation, AgentInfo } from '../../types';

dayjs.extend(relativeTime);

/** Highlight occurrences of `query` within `text`. Returns React nodes. */
function highlightText(text: string, query: string): React.ReactNode {
  if (!query) return text;
  const lowerText = text.toLowerCase();
  const lowerQuery = query.toLowerCase();
  const parts: React.ReactNode[] = [];
  let cursor = 0;
  let matchIndex = lowerText.indexOf(lowerQuery, cursor);
  while (matchIndex !== -1) {
    if (matchIndex > cursor) {
      parts.push(text.substring(cursor, matchIndex));
    }
    parts.push(
      <Box
        key={matchIndex}
        component="span"
        sx={{ bgcolor: 'warning.main', color: 'warning.contrastText', borderRadius: 0.5, px: 0.25 }}
      >
        {text.substring(matchIndex, matchIndex + query.length)}
      </Box>
    );
    cursor = matchIndex + query.length;
    matchIndex = lowerText.indexOf(lowerQuery, cursor);
  }
  if (cursor < text.length) {
    parts.push(text.substring(cursor));
  }
  return parts.length > 0 ? parts : text;
}

function displayLabel(conversation: Conversation): string {
  if (conversation.title) return conversation.title;
  const id = conversation.id;
  return id.length > 28 ? id.substring(0, 12) + '...' + id.substring(id.length - 8) : id;
}

/** /conversations/all — browse all conversations across all agents. */
export default function ConversationsAllPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { backend } = useAppContext();
  const { conversations, agents } = backend;
  const defaultAgentId = agents.length > 0 ? agents[0].id : 'main';

  const stickyHeaderRef = useRef<HTMLDivElement>(null);
  const [stickyHeaderHeight, setStickyHeaderHeight] = useState(0);

  useEffect(() => {
    const element = stickyHeaderRef.current;
    if (!element) return;
    const observer = new ResizeObserver(() => {
      setStickyHeaderHeight(element.getBoundingClientRect().height);
    });
    observer.observe(element);
    return () => observer.disconnect();
  }, []);

  const [filterText, setFilterText] = useState('');
  const [selectMode, setSelectMode] = useState(false);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [confirmDeleteOpen, setConfirmDeleteOpen] = useState(false);

  // Build active conversation map: agentId → activeConversationId
  const activeConversationByAgent = useMemo(() => {
    const mapping = new Map<string, string>();
    for (const agent of agents) {
      if (agent.activeConversationId) {
        mapping.set(agent.id, agent.activeConversationId);
      }
    }
    return mapping;
  }, [agents]);

  // Group conversations by agent.
  const conversationsByAgent = useMemo(() => {
    const groups = new Map<string, Conversation[]>();
    for (const agent of agents) {
      groups.set(agent.id, []);
    }
    for (const conversation of conversations) {
      const agentId = conversation.agentId || defaultAgentId;
      const list = groups.get(agentId) || [];
      list.push(conversation);
      groups.set(agentId, list);
    }
    return groups;
  }, [conversations, agents, defaultAgentId]);

  // Ordered agent IDs (registered agents first, then any extra from conversations).
  const agentIds = useMemo(() => {
    const ordered: string[] = agents.map((agent: AgentInfo) => agent.id);
    for (const agentId of conversationsByAgent.keys()) {
      if (!ordered.includes(agentId)) {
        ordered.push(agentId);
      }
    }
    return ordered;
  }, [agents, conversationsByAgent]);

  // Filter conversations by keyword.
  const trimmedFilter = filterText.trim();
  const lowerFilter = trimmedFilter.toLowerCase();
  const filteredByAgent = useMemo(() => {
    const result = new Map<string, Conversation[]>();
    for (const agentId of agentIds) {
      const agentConversations = conversationsByAgent.get(agentId) || [];
      const filtered = lowerFilter
        ? agentConversations.filter((conversation) => {
            const title = (conversation.title || '').toLowerCase();
            const summary = (conversation.summary || '').toLowerCase();
            const id = conversation.id.toLowerCase();
            return title.includes(lowerFilter) || summary.includes(lowerFilter) || id.includes(lowerFilter);
          })
        : agentConversations;
      if (filtered.length > 0) {
        result.set(agentId, filtered);
      }
    }
    return result;
  }, [agentIds, conversationsByAgent, lowerFilter]);

  function isActiveConversation(agentId: string, conversationId: string): boolean {
    return activeConversationByAgent.get(agentId) === conversationId;
  }

  function toggleSelected(conversationId: string) {
    setSelected((previous) => {
      const next = new Set(previous);
      if (next.has(conversationId)) {
        next.delete(conversationId);
      } else {
        next.add(conversationId);
      }
      return next;
    });
  }

  const handleDeleteSelected = useCallback(() => {
    for (const conversationId of selected) {
      const conversation = conversations.find((conversation) => conversation.id === conversationId);
      const agentId = conversation?.agentId || defaultAgentId;
      if (!isActiveConversation(agentId, conversationId)) {
        backend.deleteConversation(conversationId, agentId);
      }
    }
    setSelected(new Set());
    setSelectMode(false);
    setConfirmDeleteOpen(false);
  }, [selected, conversations, defaultAgentId, backend]);

  // When exactly one non-active conversation is selected, allow setting it as active.
  const singleSelectedConversation = useMemo(() => {
    if (selected.size !== 1) return null;
    const conversationId = Array.from(selected)[0];
    const conversation = conversations.find((conversation) => conversation.id === conversationId);
    if (!conversation) return null;
    const agentId = conversation.agentId || defaultAgentId;
    if (isActiveConversation(agentId, conversationId)) return null;
    return { conversationId, agentId };
  }, [selected, conversations, defaultAgentId]);

  function handleSetActive() {
    if (!singleSelectedConversation) return;
    backend.setActiveConversation(singleSelectedConversation.agentId, singleSelectedConversation.conversationId);
    setSelected(new Set());
    setSelectMode(false);
  }

  function handleExitSelectMode() {
    setSelectMode(false);
    setSelected(new Set());
  }

  function handleConversationClick(agentId: string, conversationId: string) {
    if (selectMode) {
      if (!isActiveConversation(agentId, conversationId)) {
        toggleSelected(conversationId);
      }
    } else {
      navigate({ to: `/conversations/${agentId}/${conversationId}` });
    }
  }

  return (
    <Box sx={{ flex: 1, overflowY: 'auto' }}>
      <Container maxWidth="md">
        {/* Header + Search — sticky at top */}
        <Box ref={stickyHeaderRef} sx={{ position: 'sticky', top: 0, zIndex: 2, bgcolor: 'background.default', pt: 2, pb: 1 }}>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5, mb: 1 }}>
            <Typography variant="h6" sx={{ fontSize: '16px', fontWeight: 600, flex: 1, minWidth: 120 }}>
              {t('conversations.allConversations')}
            </Typography>
            {selectMode ? (
              <Box sx={{ display: 'flex', gap: 1, alignItems: 'center' }}>
                {singleSelectedConversation && (
                  <Tooltip title={t('conversations.setActive')}>
                    <Button
                      size="small"
                      variant="outlined"
                      startIcon={<RadioButtonUncheckedIcon sx={{ fontSize: 14 }} />}
                      onClick={handleSetActive}
                      sx={{
                        fontSize: '12px',
                        textTransform: 'none',
                        minWidth: { xs: 'auto', sm: undefined },
                        px: { xs: 1, sm: 1.5 },
                        '& .MuiButton-startIcon': { mr: { xs: 0, sm: 0.5 } },
                      }}
                    >
                      <Box component="span" sx={{ display: { xs: 'none', sm: 'inline' } }}>
                        {t('conversations.setActive')}
                      </Box>
                    </Button>
                  </Tooltip>
                )}
                {selected.size > 0 && (
                  <Tooltip title={t('conversations.deleteSelected', { count: selected.size })}>
                    <Button
                      size="small"
                      color="error"
                      variant="outlined"
                      startIcon={<DeleteIcon sx={{ fontSize: 14 }} />}
                      onClick={() => setConfirmDeleteOpen(true)}
                      sx={{
                        fontSize: '12px',
                        textTransform: 'none',
                        minWidth: { xs: 'auto', sm: undefined },
                        px: { xs: 1, sm: 1.5 },
                        '& .MuiButton-startIcon': { mr: { xs: 0, sm: 0.5 } },
                      }}
                    >
                      <Box component="span" sx={{ display: { xs: 'none', sm: 'inline' } }}>
                        {t('conversations.deleteSelected', { count: selected.size })}
                      </Box>
                    </Button>
                  </Tooltip>
                )}
                <Button
                  size="small"
                  variant="text"
                  onClick={handleExitSelectMode}
                  sx={{ fontSize: '12px', textTransform: 'none' }}
                >
                  {t('common.cancel')}
                </Button>
              </Box>
            ) : (
              <IconButton
                size="small"
                onClick={() => setSelectMode(true)}
                sx={{ color: 'text.secondary' }}
              >
                <EditIcon sx={{ fontSize: 16 }} />
              </IconButton>
            )}
          </Box>

          <TextField
            size="small"
            fullWidth
            placeholder={t('conversations.searchPlaceholder')}
            value={filterText}
            onChange={(event) => setFilterText(event.target.value)}
            InputProps={{
              startAdornment: (
                <InputAdornment position="start">
                  <SearchIcon sx={{ fontSize: 16, color: 'text.disabled' }} />
                </InputAdornment>
              ),
            }}
            sx={{ '& .MuiInputBase-input': { fontSize: '0.8rem' } }}
          />
        </Box>

        {/* Conversation list grouped by agent */}
        <Box sx={{ pb: 2 }}>
          {Array.from(filteredByAgent.entries()).map(([agentId, agentConversations]) => {
            const agent = agents.find((agent: AgentInfo) => agent.id === agentId);
            const displayName = agent?.name || agentId;

            return (
              <Box key={agentId} sx={{ mb: 1.5 }}>
                <Box sx={{ position: 'sticky', top: stickyHeaderHeight, zIndex: 1, bgcolor: 'background.default', display: 'flex', alignItems: 'center', py: 0.5 }}>
                  <Typography variant="overline" sx={{ flex: 1, fontSize: '10px', color: 'text.secondary', letterSpacing: '0.08em' }}>
                    {displayName}
                  </Typography>
                  <Tooltip title={t('conversations.newConversation')} placement="top">
                    <IconButton
                      size="small"
                      onClick={() => navigate({ to: `/conversations/${agentId}` })}
                      sx={{ p: 0, color: 'text.disabled', '&:hover': { color: 'primary.main' } }}
                    >
                      <AddIcon sx={{ fontSize: 12,  color: 'text.disabled', mr: 0.5 }} />
                      <Typography variant="caption" color="text.secondary" sx={{ fontVariantNumeric: 'tabular-nums', mr: 0.5 }}>
                        {t('conversations.newConversation')}
                      </Typography>
                    </IconButton>
                  </Tooltip>
                  <Tooltip title={t('conversations.newConversation')} placement="top">
                    <IconButton
                      size="small"
                      sx={{ p: 0, color: 'text.disabled', '&:hover': { color: 'primary.main' } }}
                    >
                      <ChatIcon sx={{ fontSize: 12, color: 'text.disabled', mr: 0.5 }} />
                      <Typography variant="caption" color="text.secondary" sx={{ fontVariantNumeric: 'tabular-nums', mr: 0.5 }}>
                        {agentConversations.length}
                      </Typography>
                    </IconButton>
                  </Tooltip>
                </Box>

                <List disablePadding>
                  {agentConversations.map((conversation) => {
                    const isActive = isActiveConversation(agentId, conversation.id);
                    const isSelected = selected.has(conversation.id);
                    const label = displayLabel(conversation);
                    return (
                      <ListItemButton
                        key={conversation.id}
                        dense
                        onClick={() => handleConversationClick(agentId, conversation.id)}
                        sx={{ borderRadius: 1, mb: 0.25 }}
                      >
                        {selectMode && !isActive && (
                          <Checkbox
                            size="small"
                            checked={isSelected}
                            sx={{ p: 0.25, mr: 0.5 }}
                          />
                        )}
                        <ListItemText
                          primary={trimmedFilter ? highlightText(label, trimmedFilter) : label}
                          secondary={
                            <Box component="span" sx={{ display: 'flex', flexDirection: 'column' }}>
                              {conversation.summary && (
                                <Box component="span" sx={{ display: 'block' }}>
                                  {trimmedFilter ? highlightText(conversation.summary, trimmedFilter) : conversation.summary}
                                </Box>
                              )}
                              <Box component="span" sx={{ display: 'block' }}>
                                {conversation.lastActive ? dayjs(conversation.lastActive).fromNow() : ''}
                              </Box>
                            </Box>
                          }
                          primaryTypographyProps={{
                            variant: 'body2',
                            fontSize: '13px',
                            noWrap: true,
                            title: conversation.title || conversation.id,
                            color: 'text.primary',
                          }}
                          secondaryTypographyProps={{
                            variant: 'caption',
                            fontSize: '11px',
                            component: 'div',
                            color: 'text.disabled',
                            sx: { mt: 0.25 },
                          }}
                        />
                        {isActive && (
                          <Tooltip title={t('conversations.activeConversationLabel')} placement="top">
                            <RadioButtonCheckedIcon sx={{ fontSize: 12, color: 'primary.main', flexShrink: 0, ml: 1 }} />
                          </Tooltip>
                        )}
                      </ListItemButton>
                    );
                  })}
                </List>
              </Box>
            );
          })}

          {filteredByAgent.size === 0 && (
            <Box sx={{ textAlign: 'center', py: 4 }}>
              <Typography variant="body2" color="text.secondary">
                {filterText ? t('conversations.noResults') : t('conversations.noConversations')}
              </Typography>
            </Box>
          )}
        </Box>
      </Container>

      <ConfirmDialog
        open={confirmDeleteOpen}
        title={t('conversations.deleteSelectedTitle')}
        message={t('conversations.deleteSelectedMessage', { count: selected.size })}
        confirmLabel={t('common.delete')}
        onConfirm={handleDeleteSelected}
        onClose={() => setConfirmDeleteOpen(false)}
      />
    </Box>
  );
}
