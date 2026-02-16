import React, { useState, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import Box from '@mui/material/Box';
import List from '@mui/material/List';
import ListItemButton from '@mui/material/ListItemButton';
import ListItemText from '@mui/material/ListItemText';
import IconButton from '@mui/material/IconButton';
import TextField from '@mui/material/TextField';
import Typography from '@mui/material/Typography';
import Tooltip from '@mui/material/Tooltip';
import AddIcon from '@mui/icons-material/Add';
import CloseIcon from '@mui/icons-material/Close';
import RadioButtonCheckedIcon from '@mui/icons-material/RadioButtonChecked';
import RadioButtonUncheckedIcon from '@mui/icons-material/RadioButtonUnchecked';
import ConfirmDialog from './ConfirmDialog';
import type { AgentInfo, SchemaSection, ConfigSchemaResult } from '../types';
import type { useBackend } from '../hooks/useBackend';

interface SettingsNavProps {
  backend: ReturnType<typeof useBackend>;
  agents: AgentInfo[];
  activeSectionId: string | null;
  viewingAgentId: string | null;
  onNavigate: (path: string) => void;
}

export default function SettingsNav({ backend, agents, activeSectionId, viewingAgentId, onNavigate }: SettingsNavProps) {
  const { t } = useTranslation();
  const [sections, setSections] = useState<SchemaSection[]>([]);
  const [addingAgent, setAddingAgent] = useState(false);
  const [newAgentId, setNewAgentId] = useState('');
  const [pendingDelete, setPendingDelete] = useState<{ id: string; name: string } | null>(null);

  useEffect(() => {
    if (backend.connected && sections.length === 0) {
      backend.sendRpc<ConfigSchemaResult>('config.schema', {})
        .then((result) => setSections(result.schema?.['x-sections'] || []))
        .catch((error) => console.error('config.schema:', error));
    }
  }, [backend.connected, backend.sendRpc, sections.length]);

  function handleAddAgent() {
    const trimmed = newAgentId.trim();
    if (!trimmed) return;
    setAddingAgent(false);
    setNewAgentId('');
    backend.sendRpc('agents.config.save', { agent: { id: trimmed } })
      .then(() => {
        backend.refreshAgents();
        onNavigate(`/settings/agents/${trimmed}`);
      })
      .catch((error) => console.error('agents.config.save:', error));
  }

  function handleDeleteAgent() {
    if (!pendingDelete) return;
    backend.sendRpc('agents.config.delete', { id: pendingDelete.id })
      .then(() => {
        backend.refreshAgents();
        if (viewingAgentId === pendingDelete.id) {
          onNavigate('/settings');
        }
      })
      .catch((error) => console.error('agents.config.delete:', error));
    setPendingDelete(null);
  }

  return (
    <>
    <Box sx={{ flex: 1, overflowY: 'auto', p: 1 }}>
      <List disablePadding>
        {/* Preferences */}
        <ListItemButton
          dense
          onClick={() => onNavigate('/settings/preferences')}
          sx={{
            borderRadius: 1,
            mb: 0.25,
            ...(activeSectionId === 'preferences'
              ? { bgcolor: 'accentDim', color: '#fff', '&:hover': { bgcolor: 'accentDim' } }
              : {}),
          }}
        >
          <ListItemText
            primary={t('settings.preferences')}
            primaryTypographyProps={{
              variant: 'caption',
              fontSize: '13px',
              color: activeSectionId === 'preferences' ? '#fff' : 'text.secondary',
            }}
          />
        </ListItemButton>

        {/* Dynamic schema sections */}
        {sections.map((section) => {
          const isActive = activeSectionId === section.id;
          return (
            <ListItemButton
              key={section.id}
              dense
              onClick={() => onNavigate(`/settings/${section.id}`)}
              sx={{
                borderRadius: 1,
                mb: 0.25,
                ...(isActive
                  ? { bgcolor: 'accentDim', color: '#fff', '&:hover': { bgcolor: 'accentDim' } }
                  : {}),
              }}
            >
              <ListItemText
                primary={section.title}
                primaryTypographyProps={{
                  variant: 'caption',
                  fontSize: '13px',
                  color: isActive ? '#fff' : 'text.secondary',
                }}
              />
            </ListItemButton>
          );
        })}

        {/* Agents heading */}
        <Typography variant="overline" sx={{ display: 'block', px: 1.25, mt: 1.5, mb: 0.5, fontSize: '10px', color: 'text.secondary', letterSpacing: '0.08em' }}>
          {t('settings.agents')}
        </Typography>

        {agents.map((agent, index) => {
          const isViewing = viewingAgentId === agent.id;
          const isDefaultAgent = index === 0;
          const isActiveAgent = backend.serverActiveAgentId === agent.id;
          const canDelete = !isDefaultAgent && !isActiveAgent;
          return (
            <ListItemButton
              key={agent.id}
              dense
              onClick={() => onNavigate(`/settings/agents/${agent.id}`)}
              sx={{
                borderRadius: 1,
                mb: 0.25,
                '& .delete-btn': { display: 'none' },
                '&:hover .delete-btn': { display: 'inline-flex' },
                ...(isViewing
                  ? { bgcolor: 'accentDim', color: '#fff', '&:hover': { bgcolor: 'accentDim' } }
                  : {}),
              }}
            >
              <ListItemText
                primary={agent.name || agent.id}
                primaryTypographyProps={{
                  variant: 'caption',
                  fontSize: '13px',
                  fontFamily: agent.name ? undefined : 'monospace',
                  noWrap: true,
                  color: isViewing ? '#fff' : 'text.secondary',
                }}
              />
              {isActiveAgent ? (
                <Tooltip title={t('conversations.activeAgent')} placement="top">
                  <RadioButtonCheckedIcon sx={{ fontSize: 12, color: isViewing ? 'rgba(255,255,255,0.9)' : 'primary.main', flexShrink: 0, mr: 0.5 }} />
                </Tooltip>
              ) : (
                <Tooltip title={t('conversations.setActive')} placement="top">
                  <IconButton
                    size="small"
                    onClick={(event) => {
                      event.stopPropagation();
                      backend.setActiveAgent(agent.id);
                    }}
                    sx={{ p: 0, mr: 0.5, flexShrink: 0, color: isViewing ? 'rgba(255,255,255,0.5)' : 'text.disabled', '&:hover': { color: 'primary.main' } }}
                  >
                    <RadioButtonUncheckedIcon sx={{ fontSize: 12 }} />
                  </IconButton>
                </Tooltip>
              )}
              {canDelete && (
                <IconButton
                  className="delete-btn"
                  size="small"
                  title={t('agent.deleteAgent')}
                  onClick={(event) => {
                    event.stopPropagation();
                    setPendingDelete({ id: agent.id, name: agent.name || agent.id });
                  }}
                  sx={{ p: 0.25, flexShrink: 0, color: isViewing ? 'rgba(255,255,255,0.7)' : 'text.disabled', '&:hover': { color: 'error.main' } }}
                >
                  <CloseIcon sx={{ fontSize: 14 }} />
                </IconButton>
              )}
            </ListItemButton>
          );
        })}

        {/* Add agent */}
        {addingAgent ? (
          <Box sx={{ px: 0.5, mt: 0.5 }}>
            <TextField
              size="small"
              fullWidth
              placeholder={t('settings.agentIdPlaceholder')}
              autoFocus
              value={newAgentId}
              onChange={(event) => setNewAgentId(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === 'Enter' && newAgentId.trim()) {
                  event.preventDefault();
                  handleAddAgent();
                }
                if (event.key === 'Escape') {
                  setAddingAgent(false);
                  setNewAgentId('');
                }
              }}
              onBlur={() => {
                if (!newAgentId.trim()) {
                  setAddingAgent(false);
                  setNewAgentId('');
                }
              }}
              sx={{ '& .MuiInputBase-input': { fontSize: '0.75rem' } }}
            />
          </Box>
        ) : (
          <ListItemButton
            dense
            onClick={() => setAddingAgent(true)}
            sx={{ borderRadius: 1, mb: 0.25 }}
          >
            <AddIcon sx={{ fontSize: 14, mr: 0.5, color: 'text.secondary' }} />
            <ListItemText
              primary={t('settings.addAgent')}
              primaryTypographyProps={{
                variant: 'caption',
                fontSize: '13px',
                color: 'text.secondary',
              }}
            />
          </ListItemButton>
        )}
      </List>
    </Box>

    <ConfirmDialog
      open={!!pendingDelete}
      title={t('agent.deleteAgent')}
      message={t('agent.deleteConfirm', { name: pendingDelete?.name })}
      confirmLabel={t('common.delete')}
      onConfirm={handleDeleteAgent}
      onClose={() => setPendingDelete(null)}
    />
    </>
  );
}
