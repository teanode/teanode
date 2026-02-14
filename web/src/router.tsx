import {
  createRouter,
  createRootRoute,
  createRoute,
  redirect,
} from '@tanstack/react-router';
import RootLayout from './routes/__root';
import ChatLayout from './routes/chat/route';
import ChatIndex from './routes/chat/index';
import ChatAgentLayout from './routes/chat/$agentId/route';
import ChatNewPage from './routes/chat/$agentId/index';
import ChatSessionPage from './routes/chat/$agentId/$sessionKey';
import CronsLayout from './routes/crons/route';
import CronsIndex from './routes/crons/index';
import CronsNewPage from './routes/crons/new';
import CronDetailPage from './routes/crons/$jobId';
import SettingsLayout from './routes/settings/route';
import SettingsIndexPage from './routes/settings/index';
import SettingsSectionPage from './routes/settings/$sectionId';
import SettingsPreferencesPage from './routes/settings/preferences';
import SettingsAgentPage from './routes/settings/agents/$agentId';

// ── Route tree ──────────────────────────────────────────────────────

const rootRoute = createRootRoute({
  component: RootLayout,
});

// / → redirect to /chat
const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/',
  beforeLoad: () => {
    throw redirect({ to: '/chat' });
  },
});

// /chat
const chatRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: 'chat',
  component: ChatLayout,
});

// /chat/ (index) → redirect to default agent (handled by component)
const chatIndexRoute = createRoute({
  getParentRoute: () => chatRoute,
  path: '/',
  component: ChatIndex,
});

// /chat/$agentId
const chatAgentRoute = createRoute({
  getParentRoute: () => chatRoute,
  path: '$agentId',
  component: ChatAgentLayout,
});

// /chat/$agentId/ (index) → new chat page
const chatAgentIndexRoute = createRoute({
  getParentRoute: () => chatAgentRoute,
  path: '/',
  component: ChatNewPage,
});

// /chat/$agentId/$sessionKey → active session
const chatSessionRoute = createRoute({
  getParentRoute: () => chatAgentRoute,
  path: '$sessionKey',
  component: ChatSessionPage,
});

// /crons
const cronsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: 'crons',
  component: CronsLayout,
});

// /crons/ (index)
const cronsIndexRoute = createRoute({
  getParentRoute: () => cronsRoute,
  path: '/',
  component: CronsIndex,
});

// /crons/new
const cronsNewRoute = createRoute({
  getParentRoute: () => cronsRoute,
  path: 'new',
  component: CronsNewPage,
});

// /crons/$jobId
const cronDetailRoute = createRoute({
  getParentRoute: () => cronsRoute,
  path: '$jobId',
  component: CronDetailPage,
});

// /settings
const settingsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: 'settings',
  component: SettingsLayout,
});

// /settings/ (index) → redirect to first section
const settingsIndexRoute = createRoute({
  getParentRoute: () => settingsRoute,
  path: '/',
  component: SettingsIndexPage,
});

// /settings/$sectionId → individual config section
const settingsSectionRoute = createRoute({
  getParentRoute: () => settingsRoute,
  path: '$sectionId',
  component: SettingsSectionPage,
});

// /settings/preferences → client-side preferences
const settingsPreferencesRoute = createRoute({
  getParentRoute: () => settingsRoute,
  path: 'preferences',
  component: SettingsPreferencesPage,
});

// /settings/agents/$agentId → individual agent editor
const settingsAgentRoute = createRoute({
  getParentRoute: () => settingsRoute,
  path: 'agents/$agentId',
  component: SettingsAgentPage,
});

// ── Assemble tree ───────────────────────────────────────────────────

const routeTree = rootRoute.addChildren([
  indexRoute,
  chatRoute.addChildren([
    chatIndexRoute,
    chatAgentRoute.addChildren([chatAgentIndexRoute, chatSessionRoute]),
  ]),
  cronsRoute.addChildren([cronsIndexRoute, cronsNewRoute, cronDetailRoute]),
  settingsRoute.addChildren([settingsIndexRoute, settingsPreferencesRoute, settingsAgentRoute, settingsSectionRoute]),
]);

// ── Create router ───────────────────────────────────────────────────

export const router = createRouter({
  routeTree,
});

// ── Type registration ───────────────────────────────────────────────

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router;
  }
}
