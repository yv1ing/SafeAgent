import { useNavigate, useRouterState } from '@tanstack/react-router';
import { Check, ChevronsUpDown, FolderKanban } from 'lucide-react';
import { useListProjects } from '../api/generated/projects/projects';
import { Button } from '../components/ui/button';
import {
  Dropdown,
  DropdownContent,
  DropdownItem,
  DropdownLabel,
  DropdownSeparator,
  DropdownTrigger,
} from '../components/ui/dropdown';
import { useLocale } from '../i18n';
import { useProject } from './context';
import { projectSectionFromPath, type ProjectSection } from './navigation';

function useSwitchProject() {
  const navigate = useNavigate();
  return (projectId: string, section: ProjectSection) => {
    switch (section) {
      case 'overview':
        void navigate({ to: '/projects/$projectId/overview', params: { projectId } });
        return;
      case 'calls':
        void navigate({ to: '/projects/$projectId/calls', params: { projectId } });
        return;
      case 'conversations':
        void navigate({ to: '/projects/$projectId/conversations', params: { projectId } });
        return;
      case 'activity':
        void navigate({ to: '/projects/$projectId/activity', params: { projectId } });
        return;
      case 'risk':
        void navigate({ to: '/projects/$projectId/risk', params: { projectId } });
        return;
      case 'policies':
        void navigate({ to: '/projects/$projectId/policies', params: { projectId } });
        return;
      case 'redaction':
        void navigate({ to: '/projects/$projectId/redaction', params: { projectId } });
        return;
      case 'keys':
        void navigate({ to: '/projects/$projectId/keys', params: { projectId } });
        return;
      case 'routes':
        void navigate({ to: '/projects/$projectId/routes', params: { projectId } });
        return;
      case 'members':
        void navigate({ to: '/projects/$projectId/members', params: { projectId } });
        return;
      case 'settings':
        void navigate({ to: '/projects/$projectId/settings', params: { projectId } });
        return;
    }
  };
}

export function ProjectSwitcher() {
  const { t } = useLocale();
  const { project, projectId, query } = useProject();
  const projects = useListProjects();
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const switchProject = useSwitchProject();
  const navigate = useNavigate();
  const section = projectSectionFromPath(pathname);

  return (
    <Dropdown>
      <DropdownTrigger asChild>
        <Button
          variant="ghost"
          size="sm"
          className="max-w-40 justify-between px-2 font-medium sm:max-w-56"
        >
          <span className="truncate">
            {project?.name ?? (query.isPending ? t.common.loading : projectId)}
          </span>
          <ChevronsUpDown aria-hidden className="text-faint" />
        </Button>
      </DropdownTrigger>
      <DropdownContent align="start" className="max-h-80 overflow-y-auto">
        <DropdownLabel>{t.app.switchProject}</DropdownLabel>
        {(projects.data ?? []).map((entry) => (
          <DropdownItem
            key={entry.id}
            onSelect={() => switchProject(entry.id, section)}
            className="justify-between"
          >
            <span className="truncate">{entry.name}</span>
            {entry.id === projectId && <Check aria-hidden className="text-accent" />}
          </DropdownItem>
        ))}
        <DropdownSeparator />
        <DropdownItem onSelect={() => void navigate({ to: '/projects' })}>
          <FolderKanban aria-hidden />
          {t.app.allProjects}
        </DropdownItem>
      </DropdownContent>
    </Dropdown>
  );
}
