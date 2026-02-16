import React from 'react';
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

export default function JobNav({ jobs, activeJobId, isNewPage, onNavigate }: JobNavProps) {
  const { t } = useTranslation();
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

        {jobs.map((job) => (
          <ListItemButton
            key={job.id}
            dense
            onClick={() => onNavigate(`/jobs/${job.id}`)}
            sx={{
              borderRadius: 1,
              mb: 0.25,
              ...(activeJobId === job.id
                ? { bgcolor: 'accentDim', color: '#fff', '&:hover': { bgcolor: 'accentDim' } }
                : {}),
            }}
          >
            <Box sx={{ flex: 1, minWidth: 0 }}>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75 }}>
                <Typography
                  variant="caption"
                  noWrap
                  title={job.name}
                  sx={{
                    fontSize: '13px',
                    color: activeJobId === job.id ? '#fff' : 'text.secondary',
                  }}
                >
                  {job.name}
                </Typography>
              </Box>
              <Typography
                variant="caption"
                sx={{
                  fontSize: '10px',
                  fontFamily: 'monospace',
                  opacity: activeJobId === job.id ? 0.8 : 0.7,
                  color: activeJobId === job.id ? '#fff' : 'text.secondary',
                }}
              >
                {job.runAt ? new Date(job.runAt).toLocaleString() : job.schedule}
              </Typography>
            </Box>
            <Box
              sx={{
                width: 6,
                height: 6,
                borderRadius: '50%',
                flexShrink: 0,
                bgcolor: job.enabled ? 'success.main' : 'divider',
              }}
            />
          </ListItemButton>
        ))}
      </List>
    </Box>
  );
}
