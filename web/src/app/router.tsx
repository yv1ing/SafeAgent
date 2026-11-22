import type { QueryClient } from '@tanstack/react-query';
import {
  createRootRouteWithContext,
  createRoute,
  createRouter,
  lazyRouteComponent,
  Outlet,
  redirect,
} from '@tanstack/react-router';
import { Loader2 } from 'lucide-react';
import { ApiError } from '../api/client';
import { getMeQueryOptions } from '../api/generated/session/session';
import { validateCallsSearch } from '../features/calls/search';
import { useLocale } from '../i18n';
import { NotFoundPage } from './NotFoundPage';
import { ProjectScope } from './ProjectScope';
import { RouteError } from './RouteError';
import { Shell } from './Shell';

const LoginPage = lazyRouteComponent(() => import('../features/auth/LoginPage'), 'LoginPage');
const PasswordPage = lazyRouteComponent(
  () => import('../features/auth/PasswordPage'),
  'PasswordPage',
);
const ProjectsPage = lazyRouteComponent(
  () => import('../features/projects/ProjectsPage'),
  'ProjectsPage',
);
const ProjectSettingsPage = lazyRouteComponent(
  () => import('../features/projects/ProjectSettingsPage'),
  'ProjectSettingsPage',
);
const ProjectActivityPage = lazyRouteComponent(
  () => import('../features/projects/ProjectActivityPage'),
  'ProjectActivityPage',
);
const OverviewPage = lazyRouteComponent(
  () => import('../features/overview/OverviewPage'),
  'OverviewPage',
);
const CallsPage = lazyRouteComponent(() => import('../features/calls/CallsPage'), 'CallsPage');
const ConversationsPage = lazyRouteComponent(
  () => import('../features/conversations/ConversationsPage'),
  'ConversationsPage',
);
const ConversationPage = lazyRouteComponent(
  () => import('../features/conversations/ConversationPage'),
  'ConversationPage',
);
const MonitorPage = lazyRouteComponent(
  () => import('../features/monitor/MonitorPage'),
  'MonitorPage',
);
const RiskPage = lazyRouteComponent(() => import('../features/risk/RiskPage'), 'RiskPage');
const PoliciesPage = lazyRouteComponent(
  () => import('../features/policies/PoliciesPage'),
  'PoliciesPage',
);
const RedactionPage = lazyRouteComponent(
  () => import('../features/redaction/RedactionPage'),
  'RedactionPage',
);
const KeysPage = lazyRouteComponent(() => import('../features/keys/KeysPage'), 'KeysPage');
const RoutesPage = lazyRouteComponent(() => import('../features/routes/RoutesPage'), 'RoutesPage');
const MembersPage = lazyRouteComponent(
  () => import('../features/members/MembersPage'),
  'MembersPage',
);
const ModelsPage = lazyRouteComponent(() => import('../features/models/ModelsPage'), 'ModelsPage');
const ChannelsPage = lazyRouteComponent(
  () => import('../features/channels/ChannelsPage'),
  'ChannelsPage',
);
const ChannelPage = lazyRouteComponent(
  () => import('../features/channels/ChannelPage'),
  'ChannelPage',
);
const UsersPage = lazyRouteComponent(() => import('../features/users/UsersPage'), 'UsersPage');
const OperationsPage = lazyRouteComponent(
  () => import('../features/operations/OperationsPage'),
  'OperationsPage',
);

const rootRoute = createRootRouteWithContext<{ queryClient: QueryClient }>()({
  component: Outlet,
});

const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: 'login',
  component: LoginPage,
});

async function loadSession(queryClient: QueryClient) {
  try {
    return (await queryClient.ensureQueryData(getMeQueryOptions())).user;
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) throw redirect({ to: '/login' });
    throw error;
  }
}

const passwordRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: 'password',
  beforeLoad: ({ context }) => loadSession(context.queryClient),
  component: PasswordPage,
});

const appRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: 'app',
  beforeLoad: async ({ context }) => {
    const user = await loadSession(context.queryClient);
    if (user.must_change_password) throw redirect({ to: '/password' });
    return { user };
  },
  component: Shell,
});

const homeRoute = createRoute({
  getParentRoute: () => appRoute,
  path: '/',
  beforeLoad: () => {
    throw redirect({ to: '/monitor' });
  },
});

const monitorRoute = createRoute({
  getParentRoute: () => appRoute,
  path: 'monitor',
  component: MonitorPage,
});

const projectsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: 'projects',
  component: Outlet,
});

const projectListRoute = createRoute({
  getParentRoute: () => projectsRoute,
  path: '/',
  component: ProjectsPage,
});

const projectRoute = createRoute({
  getParentRoute: () => projectsRoute,
  path: '$projectId',
  component: ProjectScope,
});

const projectIndexRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: '/',
  beforeLoad: ({ params }) => {
    throw redirect({ to: '/projects/$projectId/overview', params });
  },
});

const callsRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: 'calls',
  validateSearch: validateCallsSearch,
  component: CallsPage,
});

const conversationRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: 'conversations/$sessionId',
  component: ConversationPage,
});

const adminRoute = createRoute({
  getParentRoute: () => appRoute,
  id: 'admin',
  beforeLoad: ({ context }) => {
    if (!context.user.administrator) throw redirect({ to: '/' });
  },
  component: Outlet,
});

const routeTree = rootRoute.addChildren([
  loginRoute,
  passwordRoute,
  appRoute.addChildren([
    homeRoute,
    monitorRoute,
    projectsRoute.addChildren([
      projectListRoute,
      projectRoute.addChildren([
        projectIndexRoute,
        createRoute({
          getParentRoute: () => projectRoute,
          path: 'overview',
          component: OverviewPage,
        }),
        callsRoute,
        createRoute({
          getParentRoute: () => projectRoute,
          path: 'conversations',
          component: ConversationsPage,
        }),
        conversationRoute,
        createRoute({
          getParentRoute: () => projectRoute,
          path: 'risk',
          component: RiskPage,
        }),
        createRoute({
          getParentRoute: () => projectRoute,
          path: 'activity',
          component: ProjectActivityPage,
        }),
        createRoute({
          getParentRoute: () => projectRoute,
          path: 'policies',
          component: PoliciesPage,
        }),
        createRoute({
          getParentRoute: () => projectRoute,
          path: 'redaction',
          component: RedactionPage,
        }),
        createRoute({
          getParentRoute: () => projectRoute,
          path: 'keys',
          component: KeysPage,
        }),
        createRoute({
          getParentRoute: () => projectRoute,
          path: 'routes',
          component: RoutesPage,
        }),
        createRoute({
          getParentRoute: () => projectRoute,
          path: 'members',
          component: MembersPage,
        }),
        createRoute({
          getParentRoute: () => projectRoute,
          path: 'settings',
          component: ProjectSettingsPage,
        }),
      ]),
    ]),
    adminRoute.addChildren([
      createRoute({
        getParentRoute: () => adminRoute,
        path: 'models',
        component: ModelsPage,
      }),
      createRoute({
        getParentRoute: () => adminRoute,
        path: 'channels',
        component: ChannelsPage,
      }),
      createRoute({
        getParentRoute: () => adminRoute,
        path: 'channels/$channelId',
        component: ChannelPage,
      }),
      createRoute({
        getParentRoute: () => adminRoute,
        path: 'users',
        component: UsersPage,
      }),
      createRoute({
        getParentRoute: () => adminRoute,
        path: 'operations',
        component: OperationsPage,
      }),
    ]),
  ]),
]);

function RouterPending() {
  const { t } = useLocale();
  return (
    <div className="flex min-h-64 items-center justify-center">
      <Loader2 aria-label={t.common.loading} className="size-5 animate-spin text-muted" />
    </div>
  );
}

export function createAppRouter(queryClient: QueryClient) {
  return createRouter({
    routeTree,
    basepath: '/console',
    context: { queryClient },
    defaultPreload: 'intent',
    defaultPendingMs: 100,
    defaultPendingComponent: RouterPending,
    defaultErrorComponent: RouteError,
    defaultNotFoundComponent: NotFoundPage,
    scrollRestoration: true,
  });
}

declare module '@tanstack/react-router' {
  interface Register {
    router: ReturnType<typeof createAppRouter>;
  }
}
