import {
  createRouter,
  createRootRoute,
  createRoute,
  redirect,
} from '@tanstack/react-router';
import RootLayout from './routes/__root';
import ConversationsLayout from './routes/conversations/route';
import ConversationsIndex from './routes/conversations/index';
import ConversationsAgentLayout from './routes/conversations/$agentId/route';
import ConversationsNewPage from './routes/conversations/$agentId/index';
import ConversationsConversationPage from './routes/conversations/$agentId/$conversationId';
import ConversationsAllPage from './routes/conversations/all';
import JobsLayout from './routes/jobs/route';
import JobsIndex from './routes/jobs/index';
import JobsNewPage from './routes/jobs/new';
import JobDetailPage from './routes/jobs/$jobId';
import SettingsLayout from './routes/settings/route';
import SettingsIndexPage from './routes/settings/index';
import SettingsSectionPage from './routes/settings/$sectionId';
import SettingsPreferencesPage from './routes/settings/preferences';
import SettingsTokenPage from './routes/settings/token';
import SettingsPasswordPage from './routes/settings/password';
import SettingsSessionsPage from './routes/settings/sessions';
import SettingsAgentPage from './routes/settings/agents/$agentId';

// ── Route tree ──────────────────────────────────────────────────────

const rootRoute = createRootRoute({
  component: RootLayout,
});

// / → redirect to /conversations
const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/',
  beforeLoad: () => {
    throw redirect({ to: '/conversations' });
  },
});

// /conversations
const conversationsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: 'conversations',
  component: ConversationsLayout,
});

// /conversations/ (index) → redirect to default agent (handled by component)
const conversationsIndexRoute = createRoute({
  getParentRoute: () => conversationsRoute,
  path: '/',
  component: ConversationsIndex,
});

// /conversations/$agentId
const conversationsAgentRoute = createRoute({
  getParentRoute: () => conversationsRoute,
  path: '$agentId',
  component: ConversationsAgentLayout,
});

// /conversations/$agentId/ (index) → new conversation page
const conversationsAgentIndexRoute = createRoute({
  getParentRoute: () => conversationsAgentRoute,
  path: '/',
  component: ConversationsNewPage,
});

// /conversations/all → browse all conversations across all agents
const conversationsAllRoute = createRoute({
  getParentRoute: () => conversationsRoute,
  path: 'all',
  component: ConversationsAllPage,
});

// /conversations/$agentId/$conversationId → active conversation
const conversationsConversationRoute = createRoute({
  getParentRoute: () => conversationsAgentRoute,
  path: '$conversationId',
  component: ConversationsConversationPage,
});

// /jobs
const jobsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: 'jobs',
  component: JobsLayout,
});

// /jobs/ (index)
const jobsIndexRoute = createRoute({
  getParentRoute: () => jobsRoute,
  path: '/',
  component: JobsIndex,
});

// /jobs/new
const jobsNewRoute = createRoute({
  getParentRoute: () => jobsRoute,
  path: 'new',
  component: JobsNewPage,
});

// /jobs/$jobId
const jobDetailRoute = createRoute({
  getParentRoute: () => jobsRoute,
  path: '$jobId',
  component: JobDetailPage,
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

// /settings/token → auth token management
const settingsTokenRoute = createRoute({
  getParentRoute: () => settingsRoute,
  path: 'token',
  component: SettingsTokenPage,
});

// /settings/password → password management
const settingsPasswordRoute = createRoute({
  getParentRoute: () => settingsRoute,
  path: 'password',
  component: SettingsPasswordPage,
});

// /settings/sessions → login session management
const settingsSessionsRoute = createRoute({
  getParentRoute: () => settingsRoute,
  path: 'sessions',
  component: SettingsSessionsPage,
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
  conversationsRoute.addChildren([
    conversationsIndexRoute,
    conversationsAllRoute,
    conversationsAgentRoute.addChildren([conversationsAgentIndexRoute, conversationsConversationRoute]),
  ]),
  jobsRoute.addChildren([jobsIndexRoute, jobsNewRoute, jobDetailRoute]),
  settingsRoute.addChildren([settingsIndexRoute, settingsPreferencesRoute, settingsTokenRoute, settingsPasswordRoute, settingsSessionsRoute, settingsAgentRoute, settingsSectionRoute]),
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
