import React, { useEffect, useCallback } from 'react';
import { useNavigate, useParams } from '@tanstack/react-router';
import { useAppContext } from '../../../context';
import { useAgents } from '../../../hooks/useAgents';
import AgentEditor from '../../../components/AgentEditor';

/** /settings/agents/$agentId — individual agent editor. */
export default function SettingsAgentPage() {
  const { agentId } = useParams({ strict: false }) as { agentId: string };
  const { chat } = useAppContext();
  const navigate = useNavigate();
  const agentsHook = useAgents(chat.sendRpc);

  useEffect(() => {
    if (chat.connected) {
      agentsHook.loadAgents();
      agentsHook.loadSchema();
    }
  }, [chat.connected, agentsHook.loadAgents, agentsHook.loadSchema]);

  const agent = agentsHook.agents.find((item) => item.id === agentId) ?? null;

  useEffect(() => {
    if (!agentsHook.loading && !agent) {
      navigate({ to: '/settings' });
    }
  }, [agentsHook.loading, agent, navigate]);

  const handleSave = useCallback(
    (agent: Parameters<typeof agentsHook.saveAgent>[0]) => {
      return agentsHook.saveAgent(agent).then(() => chat.refreshAgents());
    },
    [agentsHook.saveAgent, chat.refreshAgents]
  );

  const handleDelete = useCallback(
    (id: string) => {
      agentsHook.deleteAgent(id).then(() => {
        chat.refreshAgents();
        navigate({ to: '/settings' });
      });
    },
    [agentsHook.deleteAgent, chat.refreshAgents, navigate]
  );

  return (
    <AgentEditor
      agent={agent}
      models={chat.models}
      schema={agentsHook.schema}
      onSave={handleSave}
      onDelete={handleDelete}
    />
  );
}
