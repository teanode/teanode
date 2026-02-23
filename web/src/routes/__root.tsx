import React, { useCallback } from "react";
import { Outlet, useLocation } from "@tanstack/react-router";
import Box from "@mui/material/Box";
import CircularProgress from "@mui/material/CircularProgress";
import useMediaQuery from "@mui/material/useMediaQuery";
import { useTheme } from "@mui/material/styles";
import Sidebar from "../components/Sidebar";
import { useAppContext } from "../context";
import { useEdgeSwipe } from "../hooks/useEdgeSwipe";

export default function RootLayout() {
  const location = useLocation();
  const { backend, mobileSidebarOpen, setMobileSidebarOpen } = useAppContext();
  const theme = useTheme();
  const isMobile = !useMediaQuery(theme.breakpoints.up("md"));
  const isAuthPage =
    location.pathname === "/login" || location.pathname === "/setup";

  const openSidebar = useCallback(() => {
    setMobileSidebarOpen(true);
  }, [setMobileSidebarOpen]);

  // Enable edge-swipe to open the sidebar on mobile when it's closed.
  useEdgeSwipe(openSidebar, isMobile && !mobileSidebarOpen);

  if (isAuthPage) {
    return <Outlet />;
  }

  if (!backend.hasConnectedOnce && (backend.connecting || !backend.connected)) {
    return (
      <Box
        sx={{
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          height: "100vh",
        }}
      >
        <CircularProgress />
      </Box>
    );
  }

  return (
    <Box sx={{ display: "flex", height: "100vh" }}>
      <Sidebar />
      <Box
        sx={{ flex: 1, display: "flex", flexDirection: "column", minWidth: 0 }}
      >
        <Outlet />
      </Box>
    </Box>
  );
}
