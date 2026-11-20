import type { UseQueryResult } from '@tanstack/react-query';
import { useParams, useRouteContext } from '@tanstack/react-router';
import { createContext, useContext, type ReactNode } from 'react';
import type { ConsoleProjectView } from '../api/generated/models';

export interface ProjectContextValue {
  projectId: string;
  project?: ConsoleProjectView;
  query: UseQueryResult<ConsoleProjectView, unknown>;
}

const ProjectContext = createContext<ProjectContextValue | null>(null);

export function ProjectProvider({
  value,
  children,
}: {
  value: ProjectContextValue | null;
  children: ReactNode;
}) {
  return <ProjectContext.Provider value={value}>{children}</ProjectContext.Provider>;
}

export function useProject() {
  const value = useContext(ProjectContext);
  if (!value) throw new Error('useProject must be used inside a project route');
  return value;
}

export function useOptionalProject() {
  return useContext(ProjectContext);
}

export function useProjectId() {
  return useProject().projectId;
}

export function useCurrentProjectId() {
  const params = useParams({ strict: false }) as Record<string, string | undefined>;
  return params.projectId;
}

export function useCurrentUser() {
  return useRouteContext({ from: '/app' }).user;
}
