import { Link, useNavigate } from '@tanstack/react-router';
import { FolderKanban, MoreHorizontal, Plus, Search, Settings } from 'lucide-react';
import { useMemo, useState } from 'react';
import type { ConsoleProjectView } from '../../api/generated/models';
import { useListProjects } from '../../api/generated/projects/projects';
import { useCurrentUser } from '../../app/context';
import { FilterBar } from '../../components/data/FilterBar';
import { ConfirmDialog } from '../../components/form/ConfirmDialog';
import { Page } from '../../components/layout/Page';
import { PageHeader } from '../../components/layout/PageHeader';
import { QueryState } from '../../components/feedback/QueryState';
import { Button } from '../../components/ui/button';
import { Card } from '../../components/ui/card';
import {
  Dropdown,
  DropdownContent,
  DropdownItem,
  DropdownSeparator,
  DropdownTrigger,
} from '../../components/ui/dropdown';
import { Empty } from '../../components/ui/empty';
import { Progress } from '../../components/ui/progress';
import { Skeleton } from '../../components/ui/skeleton';
import { interpolate, useLocale } from '../../i18n';
import { formatDate, formatNumber } from '../../lib/format';
import { ProjectDialog } from './components/ProjectDialog';
import { useProjectMutations } from './hooks';

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0 bg-surface px-3 py-2.5">
      <p className="text-xs text-muted">{label}</p>
      <p className="num mt-0.5 truncate text-sm font-semibold">{value}</p>
    </div>
  );
}

function ProjectCard({
  project,
  onDelete,
}: {
  project: ConsoleProjectView;
  onDelete?: () => void;
}) {
  const { t, locale } = useLocale();
  const navigate = useNavigate();
  const quota = project.quota_usage_percent ?? 0;
  const quotaTone = quota >= 90 ? 'danger' : quota >= 70 ? 'warning' : 'accent';

  return (
    <Card className="flex flex-col">
      <div className="flex items-start justify-between gap-2 p-4">
        <div className="min-w-0">
          <Link
            to="/projects/$projectId/overview"
            params={{ projectId: project.id }}
            className="block truncate text-base font-semibold hover:text-accent"
          >
            {project.name}
          </Link>
          <p className="mt-1 truncate text-xs text-muted">
            {project.last_activity_at
              ? interpolate(t.projects.lastActive, {
                  time: formatDate(project.last_activity_at, locale),
                })
              : t.projects.noTraffic}
          </p>
        </div>
        <Dropdown>
          <DropdownTrigger asChild>
            <Button size="icon" variant="ghost" aria-label={`${t.common.actions}: ${project.name}`}>
              <MoreHorizontal aria-hidden />
            </Button>
          </DropdownTrigger>
          <DropdownContent>
            <DropdownItem
              onSelect={() =>
                void navigate({
                  to: '/projects/$projectId/settings',
                  params: { projectId: project.id },
                })
              }
            >
              <Settings aria-hidden />
              {t.common.settings}
            </DropdownItem>
            {onDelete && (
              <>
                <DropdownSeparator />
                <DropdownItem className="text-danger" onSelect={onDelete}>
                  {t.common.delete}
                </DropdownItem>
              </>
            )}
          </DropdownContent>
        </Dropdown>
      </div>

      <div className="grid grid-cols-3 gap-px border-y bg-line">
        <Metric label={t.projects.calls24h} value={formatNumber(project.calls_24h ?? 0)} />
        <Metric label={t.projects.blocked24h} value={formatNumber(project.blocked_24h ?? 0)} />
        <Metric label={t.projects.failureRate} value={project.failure_rate || '0.0%'} />
      </div>

      <div className="flex flex-1 flex-col justify-between gap-3 p-4">
        <div className="flex flex-col gap-1.5">
          <div className="flex items-center justify-between text-xs">
            <span className="text-muted">{t.projects.quotaUsage}</span>
            <span className="num text-faint">
              {project.daily_token_quota > 0 ? `${quota}%` : t.common.unlimited}
            </span>
          </div>
          <Progress
            value={project.daily_token_quota > 0 ? quota : 0}
            tone={quotaTone}
            label={`${t.projects.quotaUsage}: ${project.name}`}
          />
        </div>
        <Link
          to="/projects/$projectId/overview"
          params={{ projectId: project.id }}
          className="text-xs font-medium text-accent hover:underline"
        >
          {t.projects.openProject}
        </Link>
      </div>
    </Card>
  );
}

export function ProjectsPage() {
  const { t } = useLocale();
  const user = useCurrentUser();
  const projects = useListProjects();
  const mutations = useProjectMutations();
  const [creating, setCreating] = useState(false);
  const [deleting, setDeleting] = useState<ConsoleProjectView | null>(null);
  const [search, setSearch] = useState('');
  const filtered = useMemo(() => {
    const needle = search.trim().toLocaleLowerCase();
    if (!needle) return projects.data ?? [];
    return (projects.data ?? []).filter((project) =>
      project.name.toLocaleLowerCase().includes(needle),
    );
  }, [projects.data, search]);

  return (
    <Page>
      <PageHeader
        title={t.projects.title}
        description={t.projects.description}
        actions={
          user.administrator && (
            <Button variant="primary" onClick={() => setCreating(true)}>
              <Plus aria-hidden />
              {t.projects.add}
            </Button>
          )
        }
      />

      <FilterBar
        searchLabel={t.projects.search}
        searchPlaceholder={t.projects.searchPlaceholder}
        search={search}
        onSearchChange={setSearch}
        onClear={() => setSearch('')}
        searchClassName="sm:w-full sm:max-w-xs"
      />

      <QueryState
        query={projects}
        loading={
          <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
            {Array.from({ length: 3 }, (_, index) => (
              <Skeleton key={index} className="h-48" />
            ))}
          </div>
        }
      >
        {() =>
          (projects.data ?? []).length === 0 ? (
            <Card>
              <Empty
                icon={FolderKanban}
                title={t.projects.emptyTitle}
                description={user.administrator ? t.projects.emptyHint : t.projects.memberEmptyHint}
                action={
                  user.administrator && (
                    <Button variant="primary" onClick={() => setCreating(true)}>
                      <Plus aria-hidden />
                      {t.projects.add}
                    </Button>
                  )
                }
              />
            </Card>
          ) : filtered.length === 0 ? (
            <Card>
              <Empty
                icon={Search}
                title={t.common.noResults}
                description={t.common.noResultsHint}
                action={<Button onClick={() => setSearch('')}>{t.common.clearFilters}</Button>}
              />
            </Card>
          ) : (
            <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
              {filtered.map((project) => (
                <ProjectCard
                  key={project.id}
                  project={project}
                  onDelete={user.administrator ? () => setDeleting(project) : undefined}
                />
              ))}
            </div>
          )
        }
      </QueryState>

      {creating && (
        <ProjectDialog
          pending={mutations.create.isPending}
          onClose={() => setCreating(false)}
          onSubmit={(name) =>
            mutations.create.mutate({ name }, { onSuccess: () => setCreating(false) })
          }
        />
      )}

      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
        title={interpolate(t.projects.deleteTitle, { name: deleting?.name ?? '' })}
        description={t.projects.deleteHint}
        confirmLabel={t.common.delete}
        pending={mutations.remove.isPending}
        onConfirm={() =>
          deleting && mutations.remove.mutate(deleting.id, { onSuccess: () => setDeleting(null) })
        }
      />
    </Page>
  );
}
