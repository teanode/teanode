import React, { useEffect, useCallback } from 'react';
import { useNavigate, useParams } from '@tanstack/react-router';
import { useAppContext } from '../../../context';
import { useAgents } from '../../../hooks/useAgents';
import AgentEditor from '../../../components/AgentEditor';

/** /settings/agents/$agentId — individual agent editor. */
export default function SettingsAgentPage() {
  const { agentId } = useParams({ strict: false }) as { agentId: string };
  const { backend } = useAppContext();
  const navigate = useNavigate();
  const agentsHook = useAgents(backend.sendRpc);

  useEffect(() => {
    if (backend.connected) {
      agentsHook.loadAgents();
      agentsHook.loadSchema();
    }
  }, [backend.connected, agentsHook.loadAgents, agentsHook.loadSchema]);

  const agent = agentsHook.agents.find((item) => item.id === agentId) ?? null;

  useEffect(() => {
    if (!agentsHook.loading && !agent) {
      navigate({ to: '/settings' });
    }
  }, [agentsHook.loading, agent, navigate]);

  const handleSave = useCallback(
    (agent: Parameters<typeof agentsHook.saveAgent>[0]) => {
      return agentsHook.saveAgent(agent).then(() => backend.refreshAgents());
    },
    [agentsHook.saveAgent, backend.refreshAgents]
  );

  return (
    <AgentEditor
      agent={agent}
      models={backend.models}
      schema={agentsHook.schema}
      suggestions={agentsHook.suggestions}
      onSave={handleSave}
    />
  );
}
