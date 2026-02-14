import React, { useEffect } from 'react';
import { Outlet } from '@tanstack/react-router';
import { useAppContext } from '../../context';

/** /crons — layout that loads jobs and renders child routes. */
export default function CronsLayout() {
  const { chat, cronJobs } = useAppContext();

  useEffect(() => {
    if (chat.connected) cronJobs.loadJobs();
  }, [chat.connected, cronJobs.loadJobs]);

  return <Outlet />;
}
