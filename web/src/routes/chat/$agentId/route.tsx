import React, { useEffect } from 'react';
import { Outlet, useParams } from '@tanstack/react-router';
import { useAppContext } from '../../../context';

/** /chat/$agentId — layout that syncs the current agent and renders child routes. */
export default function ChatAgentLayout() {
  const { agentId } = useParams({ strict: false }) as { agentId: string };
  const { chat } = useAppContext();

  useEffect(() => {
    if (agentId && agentId !== chat.currentAgentId) {
      chat.setCurrentAgentId(agentId);
    }
  }, [agentId, chat.currentAgentId, chat.setCurrentAgentId]);

  return <Outlet />;
}
