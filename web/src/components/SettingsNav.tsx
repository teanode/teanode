import React, { useState, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import Box from '@mui/material/Box';
import List from '@mui/material/List';
import ListItemButton from '@mui/material/ListItemButton';
import ListItemText from '@mui/material/ListItemText';
import TextField from '@mui/material/TextField';
import Typography from '@mui/material/Typography';
import AddIcon from '@mui/icons-material/Add';
import type { AgentInfo, SchemaSection, ConfigSchemaResult } from '../types';
import type { useChat } from '../hooks/useChat';

interface SettingsNavProps {
  chat: ReturnType<typeof useChat>;
  agents: AgentInfo[];
  activeSectionId: string | null;
  activeAgentId: string | null;
  onNavigate: (path: string) => void;
}

export default function SettingsNav({ chat, agents, activeSectionId, activeAgentId, onNavigate }: SettingsNavProps) {
  const { t } = useTranslation();
  const [sections, setSections] = useState<SchemaSection[]>([]);
  const [addingAgent, setAddingAgent] = useState(false);
  const [newAgentId, setNewAgentId] = useState('');

  useEffect(() => {
    if (chat.connected && sections.length === 0) {
      chat.sendRpc<ConfigSchemaResult>('config.schema', {})
        .then((result) => setSections(result.schema?.sections || []))
        .catch((error) => console.error('config.schema:', error));
    }
  }, [chat.connected, chat.sendRpc, sections.length]);

  function handleAddAgent() {
    const trimmed = newAgentId.trim();
    if (!trimmed) return;
    setAddingAgent(false);
    setNewAgentId('');
    chat.sendRpc('agents.config.save', { agent: { id: trimmed } })
      .then(() => {
        chat.refreshAgents();
        onNavigate(`/settings/agents/${trimmed}`);
      })
      .catch((error) => console.error('agents.config.save:', error));
  }

  return (
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
                primary={section.label}
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

        {agents.map((agent) => (
          <ListItemButton
            key={agent.id}
            dense
            onClick={() => onNavigate(`/settings/agents/${agent.id}`)}
            sx={{
              borderRadius: 1,
              mb: 0.25,
              ...(activeAgentId === agent.id
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
                color: activeAgentId === agent.id ? '#fff' : 'text.secondary',
              }}
            />
          </ListItemButton>
        ))}

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
  );
}
