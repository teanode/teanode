import React, { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import Box from '@mui/material/Box';
import List from '@mui/material/List';
import ListItemButton from '@mui/material/ListItemButton';
import ListItemText from '@mui/material/ListItemText';
import Typography from '@mui/material/Typography';

import type { Job } from '../types';

interface JobNavProps {
  jobs: Job[];
  activeJobId: string | null;
  isNewPage: boolean;
  onNavigate: (path: string) => void;
}

function sortJobs(jobs: Job[]): Job[] {
  return [...jobs].sort((first, second) => {
    const firstTimestamp = first.lastRun || first.createdAt || 0;
    const secondTimestamp = second.lastRun || second.createdAt || 0;
    return secondTimestamp - firstTimestamp;
  });
}

export default function JobNav({ jobs, activeJobId, isNewPage, onNavigate }: JobNavProps) {
  const { t } = useTranslation();

  const { enabledJobs, disabledJobs } = useMemo(() => {
    const enabled: Job[] = [];
    const disabled: Job[] = [];
    for (const job of jobs) {
      if (job.enabled) {
        enabled.push(job);
      } else {
        disabled.push(job);
      }
    }
    return { enabledJobs: sortJobs(enabled), disabledJobs: sortJobs(disabled) };
  }, [jobs]);

  function renderJobItem(job: Job) {
    const active = activeJobId === job.id;
    return (
      <ListItemButton
        key={job.id}
        dense
        onClick={() => onNavigate(`/jobs/${job.id}`)}
        sx={{
          borderRadius: 1,
          mb: 0.25,
          ...(active
            ? { bgcolor: 'accentDim', color: '#fff', '&:hover': { bgcolor: 'accentDim' } }
            : {}),
        }}
      >
        <ListItemText
          primary={job.name}
          secondary={job.runAt ? new Date(job.runAt).toLocaleString() : job.schedule}
          primaryTypographyProps={{
            variant: 'caption',
            fontSize: '13px',
            noWrap: true,
            title: job.name,
            color: active ? '#fff' : 'text.secondary',
          }}
          secondaryTypographyProps={{
            variant: 'caption',
            fontSize: '10px',
            sx: { fontFamily: 'monospace' },
            color: active ? 'rgba(255,255,255,0.7)' : 'text.disabled',
          }}
        />
      </ListItemButton>
    );
  }

  return (
    <Box sx={{ flex: 1, overflowY: 'auto', p: 1 }}>
      <List disablePadding>
        <ListItemButton
          dense
          onClick={() => onNavigate('/jobs/new')}
          sx={{
            borderRadius: 1,
            mb: 0.25,
            ...(isNewPage
              ? { bgcolor: 'accentDim', color: '#fff', '&:hover': { bgcolor: 'accentDim' } }
              : {}),
          }}
        >
          <ListItemText
            primary={t('jobs.newJob')}
            secondary={t('jobs.schedulePeriodicJobs')}
            primaryTypographyProps={{
              variant: 'caption',
              fontSize: '13px',
              color: isNewPage ? '#fff' : 'primary.main',
            }}
            secondaryTypographyProps={{
              variant: 'caption',
              fontSize: '10px',
            }}
          />
        </ListItemButton>

        {enabledJobs.length > 0 && (
          <>
            <Typography variant="overline" sx={{ display: 'block', px: 1.25, mt: 1, mb: 0.25, fontSize: '10px', color: 'text.secondary', letterSpacing: '0.08em' }}>
              {t('jobs.activeSection')}
            </Typography>
            {enabledJobs.map(renderJobItem)}
          </>
        )}

        {disabledJobs.length > 0 && (
          <>
            <Typography variant="overline" sx={{ display: 'block', px: 1.25, mt: 1, mb: 0.25, fontSize: '10px', color: 'text.secondary', letterSpacing: '0.08em' }}>
              {t('jobs.inactiveSection')}
            </Typography>
            {disabledJobs.map(renderJobItem)}
          </>
        )}
      </List>
    </Box>
  );
}
