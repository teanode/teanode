import React from 'react';
import { Outlet } from '@tanstack/react-router';
import { useAppContext } from '../../context';
import { useSettings } from '../../hooks/useSettings';
import { SettingsProvider } from '../../hooks/useSettingsContext';

/** /settings — layout that loads settings state and shares it via context. */
export default function SettingsLayout() {
  const { chat } = useAppContext();
  const settings = useSettings(chat.sendRpc, true, chat.connected);

  return (
    <SettingsProvider settings={settings}>
      <Outlet />
    </SettingsProvider>
  );
}
