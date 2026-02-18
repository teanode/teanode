import React, { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import Button from '@mui/material/Button';
import CircularProgress from '@mui/material/CircularProgress';
import { DataGrid, type GridColDef } from '@mui/x-data-grid';
import type { SessionInfo } from '../types';
import type { useBackend } from '../hooks/useBackend';

interface SessionsManagerProps {
  backend: ReturnType<typeof useBackend>;
}

function formatDate(iso: string): string {
  try {
    return new Date(iso).toLocaleString();
  } catch {
    return iso;
  }
}

function shortenUserAgent(ua: string): string {
  if (!ua) return '-';
  if (ua.length > 60) return ua.slice(0, 57) + '...';
  return ua;
}

export default function SessionsManager({ backend }: SessionsManagerProps) {
  const { t } = useTranslation();
  const [sessions, setSessions] = useState<SessionInfo[]>([]);
  const [loading, setLoading] = useState(true);

  function loadSessions() {
    setLoading(true);
    backend.sendRpc<{ sessions: SessionInfo[] }>('sessions.list', {})
      .then((result) => setSessions(result.sessions || []))
      .catch((error) => console.error('sessions.list:', error))
      .finally(() => setLoading(false));
  }

  useEffect(() => {
    if (backend.connected) loadSessions();
  }, [backend.connected]);

  function handleRevoke(sessionId: string) {
    backend.sendRpc('sessions.revoke', { sessionId })
      .then(() => {
        setSessions((prev) => prev.filter((s) => s.id !== sessionId));
      })
      .catch((error) => console.error('sessions.revoke:', error));
  }

  const columns: GridColDef[] = [
    {
      field: 'createdAt',
      headerName: t('auth.sessionCreated'),
      flex: 1,
      minWidth: 160,
      valueFormatter: (value: string) => formatDate(value),
    },
    {
      field: 'lastSeenAt',
      headerName: t('auth.sessionLastSeen'),
      flex: 1,
      minWidth: 160,
      valueFormatter: (value: string) => formatDate(value),
    },
    {
      field: 'userAgent',
      headerName: t('auth.sessionUserAgent'),
      flex: 1.5,
      minWidth: 200,
      valueFormatter: (value: string) => shortenUserAgent(value),
    },
    {
      field: 'remoteAddr',
      headerName: t('auth.sessionIP'),
      flex: 0.7,
      minWidth: 120,
    },
    {
      field: 'actions',
      headerName: '',
      width: 100,
      sortable: false,
      filterable: false,
      renderCell: (params) => (
        <Button
          size="small"
          color="error"
          variant="outlined"
          onClick={() => handleRevoke(params.row.id)}
        >
          {t('auth.revoke')}
        </Button>
      ),
    },
  ];

  if (loading) {
    return (
      <Box sx={{ p: 3, textAlign: 'center' }}>
        <CircularProgress size={24} />
      </Box>
    );
  }

  return (
    <Box sx={{ p: 3, maxWidth: 900 }}>
      <Typography variant="h6" sx={{ mb: 2 }}>
        {t('auth.sessions')}
      </Typography>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
        {t('auth.sessionsDescription')}
      </Typography>
      {sessions.length === 0 ? (
        <Typography variant="body2" color="text.secondary">
          {t('auth.noSessions')}
        </Typography>
      ) : (
        <DataGrid
          rows={sessions}
          columns={columns}
          getRowId={(row) => row.id}
          autoHeight
          disableRowSelectionOnClick
          pageSizeOptions={[10, 25]}
          initialState={{
            pagination: { paginationModel: { pageSize: 10 } },
          }}
          sx={{
            '& .MuiDataGrid-cell': { fontSize: '0.8rem' },
            '& .MuiDataGrid-columnHeaderTitle': { fontSize: '0.8rem' },
          }}
        />
      )}
    </Box>
  );
}
