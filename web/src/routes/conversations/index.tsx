import React, { useEffect } from 'react';
import { useNavigate } from '@tanstack/react-router';
import Box from '@mui/material/Box';
import CircularProgress from '@mui/material/CircularProgress';
import { useAppContext } from '../../context';

/** /conversations/ — redirect to the active agent's active conversation, or new conversation page. */
export default function ConversationsIndex() {
  const { backend } = useAppContext();
  const navigate = useNavigate();

  useEffect(() => {
    if (!backend.connected) return;

    const activeAgentId = backend.serverActiveAgentId
      || (backend.agents.length > 0 ? backend.agents[0].id : 'main');
    const activeAgent = backend.agents.find((agent) => agent.id === activeAgentId);
    const activeConversationId = activeAgent?.activeConversationId;

    if (activeConversationId) {
      navigate({
        to: '/conversations/$agentId/$conversationId',
        params: { agentId: activeAgentId, conversationId: activeConversationId },
        replace: true,
      });
    } else {
      navigate({
        to: '/conversations/$agentId',
        params: { agentId: activeAgentId },
        replace: true,
      });
    }
  }, [navigate, backend.connected, backend.serverActiveAgentId, backend.agents]);

  return (
    <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', flex: 1 }}>
      <CircularProgress />
    </Box>
  );
}
