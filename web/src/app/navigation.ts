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
  Server,
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
  | 'trajectories'
  | 'activity'
  | 'policies'
  | 'redaction'
  | 'keys'
  | 'routes'
  | 'members'
  | 'settings';

export type ProjectPath = `/projects/$projectId/${ProjectSection}`;

export type GlobalPath =
  | '/monitor'
  | '/projects'
  | '/providers'
  | '/models'
  | '/channels'
  | '/users'
  | '/operations';

type Label = (t: Messages) => string;
type BadgeSlot = (t: Messages) => string | undefined;

interface NavItemBase {
  key: string;
  label: Label;
  icon: LucideIcon;
  admin?: boolean;
  badge?: BadgeSlot;
}

export interface ProjectNavItem extends NavItemBase {
  scope: 'project';
  section: ProjectSection;
  to: ProjectPath;
}

export interface GlobalNavItem extends NavItemBase {
  scope: 'global';
  to: GlobalPath;
  exact?: boolean;
}

export type NavItem = ProjectNavItem | GlobalNavItem;

export interface ProjectNavSection {
  key: string;
  scope: 'project';
  title: Label;
  items: ProjectNavItem[];
}

export interface GlobalNavSection {
  key: string;
  scope: 'global';
  title: Label;
  items: GlobalNavItem[];
}

export type NavSection = ProjectNavSection | GlobalNavSection;

export const navigation: NavSection[] = [
  {
    key: 'monitor',
    scope: 'project',
    title: (t) => t.app.monitorGroup,
    items: [
      {
        key: 'overview',
        scope: 'project',
        section: 'overview',
        to: '/projects/$projectId/overview',
        icon: LayoutDashboard,
        label: (t) => t.nav.overview,
      },
      {
        key: 'calls',
        scope: 'project',
        section: 'calls',
        to: '/projects/$projectId/calls',
        icon: Activity,
        label: (t) => t.nav.calls,
      },
      {
        key: 'trajectories',
        scope: 'project',
        section: 'trajectories',
        to: '/projects/$projectId/trajectories',
        icon: MessagesSquare,
        label: (t) => t.nav.trajectories,
      },
      {
        key: 'activity',
        scope: 'project',
        section: 'activity',
        to: '/projects/$projectId/activity',
        icon: FileClock,
        label: (t) => t.nav.activity,
      },
    ],
  },
  {
    key: 'policy',
    scope: 'project',
    title: (t) => t.app.policyGroup,
    items: [
      {
        key: 'policies',
        scope: 'project',
        section: 'policies',
        to: '/projects/$projectId/policies',
        icon: ShieldCheck,
        label: (t) => t.nav.policies,
      },
      {
        key: 'redaction',
        scope: 'project',
        section: 'redaction',
        to: '/projects/$projectId/redaction',
        icon: ScanEye,
        label: (t) => t.nav.redaction,
      },
    ],
  },
  {
    key: 'access',
    scope: 'project',
    title: (t) => t.app.accessGroup,
    items: [
      {
        key: 'keys',
        scope: 'project',
        section: 'keys',
        to: '/projects/$projectId/keys',
        icon: KeyRound,
        label: (t) => t.nav.keys,
      },
      {
        key: 'routes',
        scope: 'project',
        section: 'routes',
        to: '/projects/$projectId/routes',
        icon: Route,
        label: (t) => t.nav.routes,
      },
      {
        key: 'members',
        scope: 'project',
        section: 'members',
        to: '/projects/$projectId/members',
        icon: Users,
        label: (t) => t.nav.members,
      },
    ],
  },
  {
    key: 'project',
    scope: 'project',
    title: (t) => t.app.projectGroup,
    items: [
      {
        key: 'settings',
        scope: 'project',
        section: 'settings',
        to: '/projects/$projectId/settings',
        icon: Settings,
        label: (t) => t.common.settings,
      },
    ],
  },
  {
    key: 'workspace',
    scope: 'global',
    title: (t) => t.app.workspaceGroup,
    items: [
      {
        key: 'monitor',
        scope: 'global',
        to: '/monitor',
        icon: LayoutDashboard,
        label: (t) => t.nav.monitor,
      },
      {
        key: 'projects',
        scope: 'global',
        to: '/projects',
        icon: FolderKanban,
        label: (t) => t.nav.projects,
        exact: true,
      },
    ],
  },
  {
    key: 'administration',
    scope: 'global',
    title: (t) => t.app.adminGroup,
    items: [
      {
        key: 'providers',
        scope: 'global',
        to: '/providers',
        icon: Server,
        label: (t) => t.nav.providers,
        admin: true,
      },
      {
        key: 'models',
        scope: 'global',
        to: '/models',
        icon: Boxes,
        label: (t) => t.nav.models,
        admin: true,
      },
      {
        key: 'channels',
        scope: 'global',
        to: '/channels',
        icon: Cable,
        label: (t) => t.nav.channels,
        admin: true,
      },
      {
        key: 'users',
        scope: 'global',
        to: '/users',
        icon: UserCog,
        label: (t) => t.nav.users,
        admin: true,
      },
      {
        key: 'operations',
        scope: 'global',
        to: '/operations',
        icon: ListTree,
        label: (t) => t.nav.operations,
        admin: true,
      },
    ],
  },
];

export const projectGroups = navigation.filter(
  (section): section is ProjectNavSection => section.scope === 'project',
);

export const globalGroups = navigation.filter(
  (section): section is GlobalNavSection => section.scope === 'global',
);

export const projectSections = projectGroups.flatMap((group) => group.items);

export const globalItems = globalGroups.flatMap((group) => group.items);

export function visibleItems<T extends NavItem>(items: T[], administrator: boolean) {
  return items.filter((item) => !item.admin || administrator);
}

export function normalizePath(pathname: string) {
  const withoutBase = pathname.replace(/^\/console(?=\/|$)/, '');
  return withoutBase === '' ? '/' : withoutBase;
}

export function projectSectionFromPath(pathname: string): ProjectSection {
  const match = normalizePath(pathname).match(/^\/projects\/[^/]+\/([^/]+)/);
  const section = match?.[1];
  return projectSections.find((item) => item.section === section)?.section ?? 'overview';
}

export function projectItemForPath(pathname: string): ProjectNavItem {
  const section = projectSectionFromPath(pathname);
  return projectSections.find((item) => item.section === section) ?? projectSections[0]!;
}

export function projectPath(section: ProjectSection): ProjectPath {
  return (projectSections.find((item) => item.section === section) ?? projectSections[0]!).to;
}

export function globalItemForPath(pathname: string): GlobalNavItem | undefined {
  const path = normalizePath(pathname);
  return [...globalItems]
    .sort((left, right) => right.to.length - left.to.length)
    .find((item) => path === item.to || path.startsWith(`${item.to}/`));
}
