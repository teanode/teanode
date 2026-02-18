import React from 'react';
import { Outlet, useLocation } from '@tanstack/react-router';
import Box from '@mui/material/Box';
import CircularProgress from '@mui/material/CircularProgress';
import Sidebar from '../components/Sidebar';
import { useAppContext } from '../context';

export default function RootLayout() {
  const location = useLocation();
  const { backend } = useAppContext();
  const isAuthPage = location.pathname === '/login' || location.pathname === '/setup';

  if (isAuthPage) {
    return <Outlet />;
  }

  if (!backend.connected) {
    return (
      <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100vh' }}>
        <CircularProgress />
      </Box>
    );
  }

  return (
    <Box sx={{ display: 'flex', height: '100vh' }}>
      <Sidebar />
      <Box sx={{ flex: 1, display: 'flex', flexDirection: 'column', minWidth: 0 }}>
        <Outlet />
      </Box>
    </Box>
  );
}
