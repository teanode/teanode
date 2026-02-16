import React, { useEffect } from 'react';
import { Outlet, useParams } from '@tanstack/react-router';
import { useAppContext } from '../../../context';

/** /conversations/$agentId — layout that syncs the current agent and renders child routes. */
export default function ConversationsAgentLayout() {
  const { agentId } = useParams({ strict: false }) as { agentId: string };
  const { backend } = useAppContext();

  useEffect(() => {
    if (agentId && agentId !== backend.currentAgentId) {
      backend.setCurrentAgentId(agentId);
    }
  }, [agentId, backend.currentAgentId, backend.setCurrentAgentId]);

  return <Outlet />;
}
