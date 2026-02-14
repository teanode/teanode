import React from 'react';
import { Outlet } from '@tanstack/react-router';
import Box from '@mui/material/Box';
import Sidebar from '../components/Sidebar';

export default function RootLayout() {
  return (
    <Box sx={{ display: 'flex', height: '100vh' }}>
      <Sidebar />
      <Box sx={{ flex: 1, display: 'flex', flexDirection: 'column', minWidth: 0 }}>
        <Outlet />
      </Box>
    </Box>
  );
}
