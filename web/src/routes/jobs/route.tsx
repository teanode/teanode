import React, { useEffect } from 'react';
import { Outlet } from '@tanstack/react-router';
import { useAppContext } from '../../context';

/** /jobs — layout that loads jobs and renders child routes. */
export default function JobsLayout() {
  const { backend } = useAppContext();

  useEffect(() => {
    if (backend.connected) backend.loadJobs();
  }, [backend.connected, backend.loadJobs]);

  return <Outlet />;
}
