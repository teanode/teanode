import React, { useEffect } from 'react';
import { useNavigate } from '@tanstack/react-router';
import Box from '@mui/material/Box';
import CircularProgress from '@mui/material/CircularProgress';
import { useAppContext } from '../../context';
import type { JobsListResult } from '../../types';

/** /jobs/ — redirect to first job, or fallback to new job page. */
export default function JobsIndex() {
  const { backend } = useAppContext();
  const navigate = useNavigate();

  useEffect(() => {
    if (!backend.connected) return;
    let cancelled = false;

    backend.sendRpc<JobsListResult>('jobs.list', {})
      .then((result) => {
        if (cancelled) return;
        const jobsList = result.jobs || [];
        if (jobsList.length > 0) {
          const sorted = [...jobsList].sort((a, b) => (b.lastRun ?? b.createdAt) - (a.lastRun ?? a.createdAt));
          navigate({ to: '/jobs/$jobId', params: { jobId: sorted[0].id }, replace: true });
        } else {
          navigate({ to: '/jobs/new', replace: true });
        }
      })
      .catch(() => {});

    return () => { cancelled = true; };
  }, [backend.connected, backend.sendRpc, navigate]);

  return (
    <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', flex: 1 }}>
      <CircularProgress />
    </Box>
  );
}
