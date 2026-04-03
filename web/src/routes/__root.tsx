import React, { useCallback } from "react";
import { Outlet, useLocation } from "@tanstack/react-router";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import CircularProgress from "@mui/material/CircularProgress";
import useMediaQuery from "@mui/material/useMediaQuery";
import { useTheme } from "@mui/material/styles";
import { useTranslation } from "react-i18next";
import Sidebar from "../components/Sidebar";
import { useAppContext } from "../context";
import { useEdgeSwipe } from "../hooks/useEdgeSwipe";

function StaleBuildBanner({ t }: { t: (key: string) => string }) {
  return (
    <Box
      sx={{
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        gap: 2,
        px: 2,
        py: 0.75,
        bgcolor: "warning.main",
        color: "warning.contrastText",
        fontSize: "0.875rem",
        flexShrink: 0,
      }}
    >
      {t("common.staleBuild")}
      <Button
        size="small"
        variant="outlined"
        onClick={() => window.location.reload()}
        sx={{
          color: "inherit",
          borderColor: "currentColor",
          textTransform: "none",
          py: 0,
          minHeight: 28,
          "&:hover": { borderColor: "inherit", bgcolor: "rgba(0,0,0,0.08)" },
        }}
      >
        {t("common.reload")}
      </Button>
    </Box>
  );
}

export default function RootLayout() {
  const location = useLocation();
  const { backend, mobileSidebarOpen, setMobileSidebarOpen } = useAppContext();
  const theme = useTheme();
  const isMobile = !useMediaQuery(theme.breakpoints.up("md"));
  const { t } = useTranslation();
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
    <Box sx={{ display: "flex", flexDirection: "column", height: "100vh" }}>
      {backend.frontendBuildChanged && <StaleBuildBanner t={t} />}
      <Box sx={{ display: "flex", flex: 1, minHeight: 0 }}>
        <Sidebar />
        <Box
          sx={{
            flex: 1,
            display: "flex",
            flexDirection: "column",
            minWidth: 0,
          }}
        >
          <Outlet />
        </Box>
      </Box>
    </Box>
  );
}
