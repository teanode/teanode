import React from 'react';
import { Outlet } from '@tanstack/react-router';

/** /chat — layout route that renders child routes via Outlet. */
export default function ChatLayout() {
  return <Outlet />;
}
