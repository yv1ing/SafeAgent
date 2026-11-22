import {
  Activity,
  Boxes,
  Cable,
  FileClock,
  FolderKanban,
  KeyRound,
  LayoutDashboard,
  ListTree,
  MessagesSquare,
  Route,
  ScanEye,
  Settings,
  ShieldCheck,
  UserCog,
  Users,
  type LucideIcon,
} from 'lucide-react';
import type { Messages } from '../i18n/messages';

export type ProjectSection =
  | 'overview'
  | 'calls'
  | 'conversations'
  | 'risk'
  | 'activity'
  | 'policies'
  | 'redaction'
  | 'keys'
  | 'routes'
  | 'members'
  | 'settings';

type ProjectPath =
  | '/projects/$projectId/overview'
  | '/projects/$projectId/calls'
  | '/projects/$projectId/conversations'
  | '/projects/$projectId/risk'
  | '/projects/$projectId/activity'
  | '/projects/$projectId/policies'
  | '/projects/$projectId/redaction'
  | '/projects/$projectId/keys'
  | '/projects/$projectId/routes'
  | '/projects/$projectId/members'
  | '/projects/$projectId/settings';

export interface ProjectNavItem {
  section: ProjectSection;
  to: ProjectPath;
  icon: LucideIcon;
  label: (t: Messages) => string;
}

export interface ProjectNavGroup {
  key: 'monitor' | 'policy' | 'access' | 'project';
  title: (t: Messages) => string;
  items: ProjectNavItem[];
}

export const projectNav: ProjectNavGroup[] = [
  {
    key: 'monitor',
    title: (t) => t.app.monitorGroup,
    items: [
      {
        section: 'overview',
        to: '/projects/$projectId/overview',
        icon: LayoutDashboard,
        label: (t) => t.nav.overview,
      },
      {
        section: 'calls',
        to: '/projects/$projectId/calls',
        icon: Activity,
        label: (t) => t.nav.calls,
      },
      {
        section: 'conversations',
        to: '/projects/$projectId/conversations',
        icon: MessagesSquare,
        label: (t) => t.nav.conversations,
      },
      {
        section: 'risk',
        to: '/projects/$projectId/risk',
        icon: ShieldCheck,
        label: (t) => t.nav.risk,
      },
      {
        section: 'activity',
        to: '/projects/$projectId/activity',
        icon: FileClock,
        label: (t) => t.nav.activity,
      },
    ],
  },
  {
    key: 'policy',
    title: (t) => t.app.policyGroup,
    items: [
      {
        section: 'policies',
        to: '/projects/$projectId/policies',
        icon: ShieldCheck,
        label: (t) => t.nav.policies,
      },
      {
        section: 'redaction',
        to: '/projects/$projectId/redaction',
        icon: ScanEye,
        label: (t) => t.nav.redaction,
      },
    ],
  },
  {
    key: 'access',
    title: (t) => t.app.accessGroup,
    items: [
      {
        section: 'keys',
        to: '/projects/$projectId/keys',
        icon: KeyRound,
        label: (t) => t.nav.keys,
      },
      {
        section: 'routes',
        to: '/projects/$projectId/routes',
        icon: Route,
        label: (t) => t.nav.routes,
      },
      {
        section: 'members',
        to: '/projects/$projectId/members',
        icon: Users,
        label: (t) => t.nav.members,
      },
    ],
  },
  {
    key: 'project',
    title: (t) => t.app.projectGroup,
    items: [
      {
        section: 'settings',
        to: '/projects/$projectId/settings',
        icon: Settings,
        label: (t) => t.common.settings,
      },
    ],
  },
];

export const projectSections = projectNav.flatMap((group) => group.items);

type GlobalPath = '/monitor' | '/projects' | '/models' | '/channels' | '/users' | '/operations';

export interface GlobalNavItem {
  key: 'monitor' | 'projects' | 'models' | 'channels' | 'users' | 'operations';
  to: GlobalPath;
  icon: LucideIcon;
  label: (t: Messages) => string;
  admin?: boolean;
}

export const workspaceNav: GlobalNavItem[] = [
  {
    key: 'monitor',
    to: '/monitor',
    icon: LayoutDashboard,
    label: (t) => t.nav.monitor,
  },
  {
    key: 'projects',
    to: '/projects',
    icon: FolderKanban,
    label: (t) => t.nav.projects,
  },
];

export const administrationNav: GlobalNavItem[] = [
  {
    key: 'models',
    to: '/models',
    icon: Boxes,
    label: (t) => t.nav.models,
    admin: true,
  },
  {
    key: 'channels',
    to: '/channels',
    icon: Cable,
    label: (t) => t.nav.channels,
    admin: true,
  },
  {
    key: 'users',
    to: '/users',
    icon: UserCog,
    label: (t) => t.nav.users,
    admin: true,
  },
  {
    key: 'operations',
    to: '/operations',
    icon: ListTree,
    label: (t) => t.nav.operations,
    admin: true,
  },
];

export function normalizePath(pathname: string) {
  const withoutBase = pathname.replace(/^\/console(?=\/|$)/, '');
  return withoutBase === '' ? '/' : withoutBase;
}

export function projectSectionFromPath(pathname: string) {
  const match = normalizePath(pathname).match(/^\/projects\/[^/]+\/([^/]+)/);
  const section = match?.[1];
  return projectSections.find((item) => item.section === section)?.section ?? 'overview';
}

export function globalPageLabelKey(pathname: string) {
  const path = normalizePath(pathname);
  if (path.startsWith('/monitor')) return 'monitor' as const;
  if (path.startsWith('/models')) return 'models' as const;
  if (path.startsWith('/channels')) return 'channels' as const;
  if (path.startsWith('/users')) return 'users' as const;
  if (path.startsWith('/operations')) return 'operations' as const;
  return 'projects' as const;
}
