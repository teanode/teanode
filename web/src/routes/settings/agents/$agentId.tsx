import React, { useEffect } from 'react';
import { useNavigate } from '@tanstack/react-router';

/** /settings/agents/$agentId — legacy route, redirect to unified agents page. */
export default function SettingsAgentPage() {
  const navigate = useNavigate();

  useEffect(() => {
    navigate({ to: '/settings/agents', replace: true });
  }, [navigate]);

  return null;
}
