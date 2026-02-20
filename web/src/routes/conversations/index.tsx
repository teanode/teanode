import React, { useEffect } from 'react';
import { useNavigate } from '@tanstack/react-router';
import Box from '@mui/material/Box';
import CircularProgress from '@mui/material/CircularProgress';
import { useAppContext } from '../../context';

/** /conversations/ — redirect to the default agent's default conversation, or new conversation page. */
export default function ConversationsIndex() {
  const { backend } = useAppContext();
  const navigate = useNavigate();

  useEffect(() => {
    if (!backend.connected) return;

    const defaultAgentId = backend.serverDefaultAgentId
      || (backend.agents.length > 0 ? backend.agents[0].id : 'main');
    const defaultAgent = backend.agents.find((agent) => agent.id === defaultAgentId);
    const defaultConversationId = defaultAgent?.defaultConversationId;

    if (defaultConversationId) {
      navigate({
        to: '/conversations/$agentId/$conversationId',
        params: { agentId: defaultAgentId, conversationId: defaultConversationId },
        replace: true,
      });
    } else {
      navigate({
        to: '/conversations/$agentId',
        params: { agentId: defaultAgentId },
        replace: true,
      });
    }
  }, [navigate, backend.connected, backend.serverDefaultAgentId, backend.agents]);

  return (
    <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', flex: 1 }}>
      <CircularProgress />
    </Box>
  );
}
