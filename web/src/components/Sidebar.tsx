import React from 'react';
import { useNavigate, useRouterState } from '@tanstack/react-router';
import Box from '@mui/material/Box';
import Drawer from '@mui/material/Drawer';
import Tabs from '@mui/material/Tabs';
import Tab from '@mui/material/Tab';
import IconButton from '@mui/material/IconButton';
import Tooltip from '@mui/material/Tooltip';
import useMediaQuery from '@mui/material/useMediaQuery';
import { useTheme } from '@mui/material/styles';
import ChatIcon from '@mui/icons-material/ChatBubbleOutline';
import ScheduleIcon from '@mui/icons-material/Schedule';
import SettingsIcon from '@mui/icons-material/SettingsOutlined';
import ChevronRightIcon from '@mui/icons-material/ChevronRight';
import CloseIcon from '@mui/icons-material/Close';
import { useAppContext } from '../context';
import Logo from './Logo';
import ChatNav from './ChatNav';
import CronNav from './CronNav';
import SettingsNav from './SettingsNav';

const DRAWER_WIDTH = 260;

export default function Sidebar() {
  const {
    chat,
    cronJobs,
    mobileSidebarOpen,
    setMobileSidebarOpen,
  } = useAppContext();

  const navigate = useNavigate();
  const routerState = useRouterState();
  const pathname = routerState.location.pathname;
  const theme = useTheme();
  const isDesktop = useMediaQuery(theme.breakpoints.up('md'));

  const activeView: 'chat' | 'crons' | 'settings' = pathname.startsWith('/crons')
    ? 'crons'
    : pathname.startsWith('/settings')
      ? 'settings'
      : 'chat';

  const pathParts = pathname.replace(/^\//, '').split('/').filter(Boolean);
  const routeAgentId = activeView === 'chat' && pathParts[1] ? pathParts[1] : null;
  const routeSessionKey = activeView === 'chat' && pathParts[2] ? pathParts[2] : null;
  const routeCronJobId = activeView === 'crons' && pathParts[1] && pathParts[1] !== 'new' ? pathParts[1] : null;
  const isNewCronPage = activeView === 'crons' && pathParts[1] === 'new';
  const routeSettingsSection = activeView === 'settings' ? (pathParts[1] || null) : null;
  const routeSettingsAgentId = activeView === 'settings' && pathParts[1] === 'agents' && pathParts[2] ? pathParts[2] : null;

  const { agents, currentAgentId } = chat;
  const defaultAgentId = agents.length > 0 ? agents[0].id : 'main';
  const activeAgentId = routeAgentId || currentAgentId || defaultAgentId;
  const activeSessionKey = routeSessionKey || chat.sessionKey;

  function handleNavigate(path: string) {
    navigate({ to: path });
    setMobileSidebarOpen(false);
  }

  const tabValue = activeView === 'chat' ? 0 : activeView === 'crons' ? 1 : 2;

  function handleTabChange(_event: React.SyntheticEvent, newValue: number) {
    if (newValue === 0) {
      const agentId = activeAgentId || defaultAgentId;
      handleNavigate(activeSessionKey ? `/chat/${agentId}/${activeSessionKey}` : `/chat/${agentId}`);
    } else if (newValue === 1) {
      handleNavigate(routeCronJobId ? `/crons/${routeCronJobId}` : '/crons');
    } else {
      handleNavigate('/settings');
    }
  }

  const drawerContent = (
    <Box sx={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
      {/* Header */}
      <Box sx={{ px: 2, py: 1.5, borderBottom: 1, borderColor: 'divider', display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
          <Logo />
        </Box>
        {!isDesktop && (
          <IconButton size="small" onClick={() => setMobileSidebarOpen(false)}>
            <CloseIcon fontSize="small" />
          </IconButton>
        )}
      </Box>

      {/* Tabs */}
      <Tabs
        value={tabValue}
        onChange={handleTabChange}
        variant="fullWidth"
        indicatorColor="primary"
        textColor="primary"
        sx={{
          borderBottom: 1,
          borderColor: 'divider',
          minHeight: 36,
          '& .MuiTab-root': { minHeight: 36, minWidth: 0, py: 0.75 },
        }}
      >
        <Tab icon={<Tooltip title="Chat"><ChatIcon sx={{ fontSize: 18 }} /></Tooltip>} aria-label="Chat" />
        <Tab icon={<Tooltip title="Jobs"><ScheduleIcon sx={{ fontSize: 18 }} /></Tooltip>} aria-label="Jobs" />
        <Tab icon={<Tooltip title="Settings"><SettingsIcon sx={{ fontSize: 18 }} /></Tooltip>} aria-label="Settings" />
      </Tabs>

      {/* View-specific nav */}
      {activeView === 'chat' && (
        <ChatNav
          chat={chat}
          activeAgentId={activeAgentId}
          activeSessionKey={activeSessionKey}
          onNavigate={handleNavigate}
        />
      )}
      {activeView === 'crons' && (
        <CronNav
          jobs={cronJobs.jobs}
          activeJobId={routeCronJobId}
          isNewPage={isNewCronPage}
          onNavigate={handleNavigate}
        />
      )}
      {activeView === 'settings' && (
        <SettingsNav
          chat={chat}
          agents={agents}
          activeSectionId={routeSettingsSection}
          activeAgentId={routeSettingsAgentId}
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
          title="Open sidebar"
          sx={{
            position: 'fixed',
            top: 12,
            left: 0,
            zIndex: (currentTheme) => currentTheme.zIndex.drawer + 1,
            bgcolor: 'background.paper',
            border: 1,
            borderLeft: 0,
            borderColor: 'divider',
            borderRadius: '0 8px 8px 0',
            px: 0.75,
            py: 1,
            '&:hover': { bgcolor: 'action.hover' },
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
            '& .MuiDrawer-paper': { width: DRAWER_WIDTH, bgcolor: 'background.paper' },
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
            bgcolor: 'background.paper',
            borderRight: 1,
            borderColor: 'divider',
          }}
        >
          {drawerContent}
        </Box>
      )}
    </>
  );
}
