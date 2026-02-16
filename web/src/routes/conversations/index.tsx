import React, { useEffect } from 'react';
import { useNavigate } from '@tanstack/react-router';
import { useAppContext } from '../../context';

/** /conversations/ — index redirect to /conversations/$defaultAgentId. */
export default function ConversationsIndex() {
  const { backend } = useAppContext();
  const navigate = useNavigate();
  const defaultAgentId = backend.agents.length > 0 ? backend.agents[0].id : 'main';

  useEffect(() => {
    navigate({ to: '/conversations/$agentId', params: { agentId: defaultAgentId }, replace: true });
  }, [navigate, defaultAgentId]);

  return null;
}
