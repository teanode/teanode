import React, { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useRouterState } from "@tanstack/react-router";
import Box from "@mui/material/Box";
import Drawer from "@mui/material/Drawer";
import IconButton from "@mui/material/IconButton";
import Tooltip from "@mui/material/Tooltip";
import useMediaQuery from "@mui/material/useMediaQuery";
import { useTheme } from "@mui/material/styles";
import ChatIcon from "@mui/icons-material/ChatBubbleOutline";
import SettingsIcon from "@mui/icons-material/SettingsOutlined";
import ChevronRightIcon from "@mui/icons-material/ChevronRight";
import { useAppContext } from "../context";
import Logo from "./Logo";
import ConversationNav from "./ConversationNav";
import SettingsNav from "./SettingsNav";

const DRAWER_WIDTH = 260;

export default function Sidebar() {
  const { t } = useTranslation();
  const { backend, mobileSidebarOpen, setMobileSidebarOpen } = useAppContext();

  const navigate = useNavigate();
  const routerState = useRouterState();
  const pathname = routerState.location.pathname;
  const theme = useTheme();
  const isDesktop = useMediaQuery(theme.breakpoints.up("md"));

  const activeView: "conversations" | "settings" =
    pathname.startsWith("/settings") || pathname.startsWith("/jobs")
      ? "settings"
      : "conversations";

  const isAllConversationsPage = pathname === "/conversations/all";

  const pathParts = pathname.replace(/^\//, "").split("/").filter(Boolean);
  const routeAgentId =
    activeView === "conversations" && !isAllConversationsPage && pathParts[1]
      ? pathParts[1]
      : null;
  const routeConversationId =
    activeView === "conversations" && !isAllConversationsPage && pathParts[2]
      ? pathParts[2]
      : null;
  const routeSettingsSection =
    activeView === "settings" ? pathParts[1] || null : null;

  const {
    agents,
    currentAgentId,
    conversations: conversationList,
    serverDefaultAgentId,
  } = backend;
  const fallbackAgentId = agents.length > 0 ? agents[0].id : "main";
  const defaultAgentId = serverDefaultAgentId || fallbackAgentId;
  const defaultConversationId = agents.find(
    (agent) => agent.id === defaultAgentId,
  )?.defaultConversationId;
  const viewingAgentId = routeAgentId || currentAgentId || fallbackAgentId;
  const viewingConversationId = routeConversationId || backend.conversationId;

  // Determine if the "View all conversations" button should be highlighted.
  const highlightViewAll = useMemo(() => {
    if (isAllConversationsPage) return true;
    // If viewing a specific conversation, check if it's in the nav's list.
    if (!routeConversationId) return false;
    const defaultAgentId = serverDefaultAgentId || fallbackAgentId;
    const defaultAgent = agents.find((agent) => agent.id === defaultAgentId);
    const pinnedConversationId = defaultAgent?.defaultConversationId || null;
    // If it's the pinned conversation, it's in the nav.
    if (routeConversationId === pinnedConversationId) return false;
    // Check if it's in the recent list (top 10 non-pinned conversations for the default agent).
    const agentConversations = conversationList.filter((conversation) => {
      const conversationAgentId = conversation.agentId || fallbackAgentId;
      return conversationAgentId === defaultAgentId;
    });
    const recentConversations = agentConversations
      .filter((conversation) => conversation.id !== pinnedConversationId)
      .slice(0, 10);
    const isInRecentList = recentConversations.some(
      (conversation) => conversation.id === routeConversationId,
    );
    return !isInRecentList;
  }, [
    isAllConversationsPage,
    routeConversationId,
    serverDefaultAgentId,
    fallbackAgentId,
    agents,
    conversationList,
  ]);

  function handleNavigate(path: string) {
    navigate({ to: path });
    setMobileSidebarOpen(false);
  }

  const drawerContent = (
    <Box sx={{ display: "flex", flexDirection: "column", height: "100%" }}>
      {/* Header */}
      <Box
        sx={{
          px: 2,
          py: 1.5,
          borderBottom: 1,
          borderColor: "divider",
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
        }}
      >
        <IconButton
          size="small"
          onClick={() => {
            if (defaultConversationId) {
              handleNavigate(
                `/conversations/${defaultAgentId}/${defaultConversationId}`,
              );
            } else {
              handleNavigate(`/conversations/${defaultAgentId}`);
            }
          }}
          sx={{ p: 0.25 }}
        >
          <Logo />
        </IconButton>
        <Tooltip
          title={
            activeView === "settings"
              ? t("sidebar.conversations")
              : t("sidebar.settings")
          }
        >
          <IconButton
            size="small"
            onClick={() => {
              if (activeView === "settings") {
                const agentId = viewingAgentId || fallbackAgentId;
                handleNavigate(
                  viewingConversationId
                    ? `/conversations/${agentId}/${viewingConversationId}`
                    : `/conversations/${agentId}`,
                );
              } else {
                handleNavigate("/settings");
              }
            }}
          >
            {activeView === "settings" ? (
              <ChatIcon sx={{ fontSize: 16 }} />
            ) : (
              <SettingsIcon sx={{ fontSize: 16 }} />
            )}
          </IconButton>
        </Tooltip>
      </Box>

      {/* View-specific nav */}
      {activeView === "conversations" && (
        <ConversationNav
          backend={backend}
          viewingAgentId={viewingAgentId}
          viewingConversationId={viewingConversationId}
          highlightViewAll={highlightViewAll}
          onNavigate={handleNavigate}
        />
      )}
      {activeView === "settings" && (
        <SettingsNav
          backend={backend}
          activeSectionId={routeSettingsSection}
          onNavigate={handleNavigate}
        />
      )}
    </Box>
  );

  return (
    <>
      {/* Mobile pull tab */}
      {!isDesktop && !mobileSidebarOpen && (
        <IconButton
          onClick={() => setMobileSidebarOpen(true)}
          title={t("sidebar.openSidebar")}
          sx={{
            position: "fixed",
            top: 72,
            left: 0,
            zIndex: (currentTheme) => currentTheme.zIndex.drawer + 1,
            bgcolor: "transparent",
            border: 1,
            borderLeft: 0,
            borderColor: "transparent",
            borderRadius: "0 8px 8px 0",
            px: 0.75,
            py: 1,
            opacity: 0.4,
            transition:
              "opacity 150ms ease, background-color 150ms ease, border-color 150ms ease",
            "&:hover, &:focus-visible": {
              opacity: 0.95,
              bgcolor: "background.paper",
              borderColor: "divider",
            },
          }}
        >
          <ChevronRightIcon sx={{ fontSize: 16 }} />
        </IconButton>
      )}

      {/* Mobile drawer */}
      {!isDesktop && (
        <Drawer
          variant="temporary"
          open={mobileSidebarOpen}
          onClose={() => setMobileSidebarOpen(false)}
          ModalProps={{ keepMounted: true }}
          sx={{
            "& .MuiDrawer-paper": {
              width: DRAWER_WIDTH,
              bgcolor: "background.paper",
            },
          }}
        >
          {drawerContent}
        </Drawer>
      )}

      {/* Desktop permanent sidebar */}
      {isDesktop && (
        <Box
          sx={{
            width: DRAWER_WIDTH,
            flexShrink: 0,
            bgcolor: "background.paper",
            borderRight: 1,
            borderColor: "divider",
          }}
        >
          {drawerContent}
        </Box>
      )}
    </>
  );
}
