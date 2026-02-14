import React, { useEffect } from 'react';
import { useNavigate } from '@tanstack/react-router';
import { useAppContext } from '../../context';

/** /chat/ — index redirect to /chat/$defaultAgentId. */
export default function ChatIndex() {
  const { chat } = useAppContext();
  const navigate = useNavigate();
  const defaultAgentId = chat.agents.length > 0 ? chat.agents[0].id : 'main';

  useEffect(() => {
    navigate({ to: '/chat/$agentId', params: { agentId: defaultAgentId }, replace: true });
  }, [navigate, defaultAgentId]);

  return null;
}
