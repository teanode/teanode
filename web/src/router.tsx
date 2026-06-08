import {
  createRouter,
  createRootRoute,
  createRoute,
  redirect,
} from "@tanstack/react-router";
import RootLayout from "./routes/__root";
import ConversationsLayout from "./routes/conversations/route";
import ConversationsIndex from "./routes/conversations/index";
import ConversationsAgentLayout from "./routes/conversations/$agentId/route";
import ConversationsNewPage from "./routes/conversations/$agentId/index";
import ConversationsConversationPage from "./routes/conversations/$agentId/$conversationId";
import ConversationsAllPage from "./routes/conversations/all";
import JobsLayout from "./routes/jobs/route";
import JobsIndex from "./routes/jobs/index";
import JobsNewPage from "./routes/jobs/new";
import JobDetailPage from "./routes/jobs/$jobId";
import SettingsLayout from "./routes/settings/route";
import SettingsIndexPage from "./routes/settings/index";
import SettingsSectionPage from "./routes/settings/$sectionId";
import SettingsPreferencesPage from "./routes/settings/preferences";
import SettingsProfilePage from "./routes/settings/profile";
import SettingsTokensPage from "./routes/settings/tokens";
import SettingsConnectionsPage from "./routes/settings/connections";
import SettingsPasswordPage from "./routes/settings/password";
import SettingsSessionsPage from "./routes/settings/sessions";
import SettingsAgentsPage from "./routes/settings/agents";
import SettingsUsersPage from "./routes/settings/users";
import SettingsAgentPage from "./routes/settings/agents/$agentId";
import SettingsJobsPage from "./routes/settings/jobs";
import SettingsProjectsPage from "./routes/settings/projects";
import SettingsSkillsPage from "./routes/settings/skills";
import SettingsSecretsPage from "./routes/settings/secrets";
import SettingsUsagePage from "./routes/settings/usage";
import SettingsMemoryPage from "./routes/settings/memory";
import SettingsToolPoliciesPage from "./routes/settings/policies";
import SettingsUpdatesPage from "./routes/settings/updates";
import LoginRoute from "./routes/login";
import SetupRoute from "./routes/setup";

// ── Route tree ──────────────────────────────────────────────────────

const rootRoute = createRootRoute({
  component: RootLayout,
});

// / → redirect to /conversations
const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  beforeLoad: () => {
    throw redirect({ to: "/conversations" });
  },
});

// /conversations
const conversationsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "conversations",
  component: ConversationsLayout,
});

// /conversations/ (index) → redirect to default agent (handled by component)
const conversationsIndexRoute = createRoute({
  getParentRoute: () => conversationsRoute,
  path: "/",
  component: ConversationsIndex,
});

// /conversations/$agentId
const conversationsAgentRoute = createRoute({
  getParentRoute: () => conversationsRoute,
  path: "$agentId",
  component: ConversationsAgentLayout,
});

// /conversations/$agentId/ (index) → new conversation page
const conversationsAgentIndexRoute = createRoute({
  getParentRoute: () => conversationsAgentRoute,
  path: "/",
  component: ConversationsNewPage,
});

// /conversations/all → browse all conversations across all agents
const conversationsAllRoute = createRoute({
  getParentRoute: () => conversationsRoute,
  path: "all",
  component: ConversationsAllPage,
});

// /conversations/$agentId/$conversationId → active conversation
const conversationsConversationRoute = createRoute({
  getParentRoute: () => conversationsAgentRoute,
  path: "$conversationId",
  component: ConversationsConversationPage,
});

// /jobs
const jobsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "jobs",
  component: JobsLayout,
});

// /jobs/ (index)
const jobsIndexRoute = createRoute({
  getParentRoute: () => jobsRoute,
  path: "/",
  component: JobsIndex,
});

// /jobs/new
const jobsNewRoute = createRoute({
  getParentRoute: () => jobsRoute,
  path: "new",
  component: JobsNewPage,
});

// /jobs/$jobId
const jobDetailRoute = createRoute({
  getParentRoute: () => jobsRoute,
  path: "$jobId",
  component: JobDetailPage,
});

// /settings
const settingsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "settings",
  component: SettingsLayout,
});

// /settings/ (index) → redirect to first section
const settingsIndexRoute = createRoute({
  getParentRoute: () => settingsRoute,
  path: "/",
  component: SettingsIndexPage,
});

// /settings/$sectionId → individual config section
const settingsSectionRoute = createRoute({
  getParentRoute: () => settingsRoute,
  path: "$sectionId",
  component: SettingsSectionPage,
});

// /settings/preferences → client-side preferences
const settingsProfileRoute = createRoute({
  getParentRoute: () => settingsRoute,
  path: "profile",
  component: SettingsProfilePage,
});

// /settings/preferences → client-side preferences
const settingsPreferencesRoute = createRoute({
  getParentRoute: () => settingsRoute,
  path: "preferences",
  component: SettingsPreferencesPage,
});

// /settings/tokens → auth token management
const settingsTokensRoute = createRoute({
  getParentRoute: () => settingsRoute,
  path: "tokens",
  component: SettingsTokensPage,
});

// /settings/connections → per-user MCP server connections (also the OAuth landing)
const settingsConnectionsRoute = createRoute({
  getParentRoute: () => settingsRoute,
  path: "connections",
  component: SettingsConnectionsPage,
});

// /settings/password → password management
const settingsPasswordRoute = createRoute({
  getParentRoute: () => settingsRoute,
  path: "password",
  component: SettingsPasswordPage,
});

// /settings/sessions → login session management
const settingsSessionsRoute = createRoute({
  getParentRoute: () => settingsRoute,
  path: "sessions",
  component: SettingsSessionsPage,
});

// /settings/agents → agent management page
const settingsAgentsRoute = createRoute({
  getParentRoute: () => settingsRoute,
  path: "agents",
  component: SettingsAgentsPage,
});

// /settings/jobs → jobs management page
const settingsJobsRoute = createRoute({
  getParentRoute: () => settingsRoute,
  path: "jobs",
  component: SettingsJobsPage,
});

// /settings/users → users management page (admin only)
const settingsUsersRoute = createRoute({
  getParentRoute: () => settingsRoute,
  path: "users",
  component: SettingsUsersPage,
});

// /settings/projects → projects management page
const settingsProjectsRoute = createRoute({
  getParentRoute: () => settingsRoute,
  path: "projects",
  component: SettingsProjectsPage,
});

// /settings/skills → skills management page
const settingsSkillsRoute = createRoute({
  getParentRoute: () => settingsRoute,
  path: "skills",
  component: SettingsSkillsPage,
});

// /settings/secrets → secrets management page
const settingsSecretsRoute = createRoute({
  getParentRoute: () => settingsRoute,
  path: "secrets",
  component: SettingsSecretsPage,
});

// /settings/usage → usage statistics page
const settingsUsageRoute = createRoute({
  getParentRoute: () => settingsRoute,
  path: "usage",
  component: SettingsUsagePage,
});

// /settings/memory — memory management page
const settingsMemoryRoute = createRoute({
  getParentRoute: () => settingsRoute,
  path: "memory",
  component: SettingsMemoryPage,
});

// /settings/policies — tool approval policy management
const settingsToolPoliciesRoute = createRoute({
  getParentRoute: () => settingsRoute,
  path: "policies",
  component: SettingsToolPoliciesPage,
});

// /settings/updates — auto-update management (admin only)
const settingsUpdatesRoute = createRoute({
  getParentRoute: () => settingsRoute,
  path: "updates",
  component: SettingsUpdatesPage,
});

// /settings/agents/$agentId → individual agent editor
const settingsAgentRoute = createRoute({
  getParentRoute: () => settingsRoute,
  path: "agents/$agentId",
  component: SettingsAgentPage,
});

// /login
const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "login",
  component: LoginRoute,
});

// /setup
const setupRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "setup",
  component: SetupRoute,
});

// ── Assemble tree ───────────────────────────────────────────────────

const routeTree = rootRoute.addChildren([
  indexRoute,
  loginRoute,
  setupRoute,
  conversationsRoute.addChildren([
    conversationsIndexRoute,
    conversationsAllRoute,
    conversationsAgentRoute.addChildren([
      conversationsAgentIndexRoute,
      conversationsConversationRoute,
    ]),
  ]),
  jobsRoute.addChildren([jobsIndexRoute, jobsNewRoute, jobDetailRoute]),
  settingsRoute.addChildren([
    settingsIndexRoute,
    settingsProfileRoute,
    settingsPreferencesRoute,
    settingsTokensRoute,
    settingsConnectionsRoute,
    settingsPasswordRoute,
    settingsSessionsRoute,
    settingsAgentsRoute,
    settingsUsersRoute,
    settingsJobsRoute,
    settingsProjectsRoute,
    settingsSkillsRoute,
    settingsSecretsRoute,
    settingsUsageRoute,
    settingsAgentRoute,
    settingsMemoryRoute,
    settingsToolPoliciesRoute,
    settingsUpdatesRoute,
    settingsSectionRoute,
  ]),
]);

// ── Create router ───────────────────────────────────────────────────

export const router = createRouter({
  routeTree,
});

// ── Type registration ───────────────────────────────────────────────

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
