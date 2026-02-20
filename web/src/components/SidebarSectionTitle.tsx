import React from 'react';
import Typography from '@mui/material/Typography';

interface SidebarSectionTitleProps {
  children: React.ReactNode;
  mt?: number;
}

export default function SidebarSectionTitle({ children, mt = 1 }: SidebarSectionTitleProps) {
  return (
    <Typography
      variant="overline"
      sx={{
        display: 'block',
        px: 1.25,
        mt,
        mb: 0.25,
        fontSize: '10px',
        color: 'text.secondary',
        letterSpacing: '0.08em',
      }}
    >
      {children}
    </Typography>
  );
}
