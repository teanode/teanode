import React from 'react';
import { useAppContext } from '../../context';
import SessionsManager from '../../components/SessionsManager';

/** /settings/sessions — login session management page. */
export default function SettingsSessionsPage() {
  const { backend } = useAppContext();
  return <SessionsManager backend={backend} />;
}
