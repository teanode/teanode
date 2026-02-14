import React, { useEffect } from 'react';
import { useNavigate } from '@tanstack/react-router';

/** /settings/ — redirects to preferences page. */
export default function SettingsIndexPage() {
  const navigate = useNavigate();

  useEffect(() => {
    navigate({ to: '/settings/preferences', replace: true });
  }, [navigate]);

  return null;
}
