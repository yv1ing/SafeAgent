import { Plus, Route as RouteIcon, Sparkles, Trash2 } from 'lucide-react';
import { useState } from 'react';
import { useListChannelOptions } from '../../api/generated/channel-options/channel-options';
import type { ConsoleRouteView } from '../../api/generated/models';
import { useProject } from '../../app/context';
import { DataTable, TableSkeleton } from '../../components/data/DataTable';
import { FilterBar } from '../../components/data/FilterBar';
import { EnabledBadge } from '../../components/feedback/Badges';
import { QueryState } from '../../components/feedback/QueryState';
import { ConfirmDialog } from '../../components/form/ConfirmDialog';
import { Page } from '../../components/layout/Page';
import { PageHeader } from '../../components/layout/PageHeader';
import { Badge } from '../../components/ui/badge';
import { Button } from '../../components/ui/button';
import { Card } from '../../components/ui/card';
import { Empty } from '../../components/ui/empty';
import { Switch } from '../../components/ui/switch';
import { interpolate, useLocale } from '../../i18n';
import { FromModelsDialog } from './components/FromModelsDialog';
import { RouteDialog } from './components/RouteDialog';
import { useRouteMutations, useRoutes, useToggleRoute } from './hooks';

function routeDegraded(route: ConsoleRouteView) {
  return route.targets.some(
    (target) => (target.disabled_reason ?? '') !== '' || target.enabled === false,
  );
}

function RouteEnabledToggle({ route }: { route: ConsoleRouteView }) {
  const { t } = useLocale();
  const projectId = useProject().projectId;
  const toggle = useToggleRoute(projectId);
  return (
    <Switch
      aria-label={`${route.name}: ${route.enabled ? t.common.enabled : t.common.disabled}`}
      checked={route.enabled ?? false}
      onCheckedChange={(enabled) => toggle.mutate({ route, enabled })}
    />
  );
}

export function RoutesPage() {
  const { t } = useLocale();
  const project = useProject();
  const projectId = project.projectId;
  const [includeDeleted, setShowDeleted] = useState(false);
  const [search, setSearch] = useState('');
  const [status, setStatus] = useState('');
  const routes = useRoutes(projectId, includeDeleted);
  const channels = useListChannelOptions(projectId, { query: { refetchOnMount: 'always' } });
  const mutations = useRouteMutations(projectId);
  const [editing, setEditing] = useState<ConsoleRouteView | 'new' | null>(null);
  const [creatingFromModels, setCreatingFromModels] = useState(false);
  const [deleting, setDeleting] = useState<ConsoleRouteView | null>(null);
  const channelName = (channelId: string) =>
    channels.data?.find((channel) => channel.id === channelId)?.name ?? channelId;
  const modelName = (channelId: string, modelId: string) => {
    const model = channels.data
      ?.find((channel) => channel.id === channelId)
      ?.models?.find((entry) => entry.id === modelId);
    return model?.display_name || model?.upstream_name || modelId;
  };
  const rows = (routes.data ?? []).filter((route) => {
    if (status === 'enabled' && route.enabled !== true) return false;
    if (status === 'disabled' && route.enabled === true) return false;
    if (status === 'degraded' && !routeDegraded(route)) return false;
    if (search.trim() !== '') {
      const needle = search.trim().toLowerCase();
      const aliases = route.aliases.map((alias) => alias.alias).join(' ');
      if (!`${route.name} ${aliases}`.toLowerCase().includes(needle)) return false;
    }
    return true;
  });
  const deleteDescription = (route: ConsoleRouteView) => {
    const parts = [t.routes.deleteHint];
    const aliases = route.aliases.length;
    if (aliases > 0) {
      parts.push(interpolate(t.routes.deleteAliases, { count: aliases }));
    }
    if (project.project?.default_route_id === route.id) {
      parts.push(t.routes.deleteDefault);
    }
    return parts.join(' ');
  };
  return (
    <Page>
      <PageHeader
        title={t.routes.title}
        description={t.routes.description}
        actions={
          <>
            <Button onClick={() => setCreatingFromModels(true)}>
              <Sparkles aria-hidden />
              {t.routes.fromModels}
            </Button>
            <Button variant="primary" onClick={() => setEditing('new')}>
              <Plus aria-hidden />
              {t.routes.add}
            </Button>
          </>
        }
      />
      <Card>
        <div className="border-b p-3">
          <FilterBar
            searchLabel={t.routes.search}
            searchPlaceholder={t.routes.searchPlaceholder}
            search={search}
            onSearchChange={setSearch}
            selects={[
              {
                key: 'status',
                label: t.routes.status,
                value: status,
                options: [
                  { value: '', label: t.routes.allStates },
                  { value: 'enabled', label: t.common.enabled },
                  { value: 'disabled', label: t.common.disabled },
                  { value: 'degraded', label: t.routes.degraded },
                ],
                onChange: setStatus,
              },
              {
                key: 'deleted',
                label: t.routes.deletedFilter,
                value: includeDeleted ? 'show' : '',
                options: [
                  { value: '', label: t.routes.hideDeleted },
                  { value: 'show', label: t.routes.showDeleted },
                ],
                onChange: (value) => setShowDeleted(value === 'show'),
              },
            ]}
            onClear={() => {
              setSearch('');
              setStatus('');
              setShowDeleted(false);
            }}
          />
        </div>
        <QueryState query={routes} loading={<TableSkeleton />}>
          {() => (
            <DataTable
              rows={rows}
              rowKey={(row) => row.id}
              empty={
                <Empty
                  icon={RouteIcon}
                  title={
                    rows.length === 0 && (routes.data?.length ?? 0) > 0
                      ? t.common.noResults
                      : t.routes.emptyTitle
                  }
                  description={t.routes.emptyHint}
                />
              }
              columns={[
                {
                  key: 'name',
                  header: t.common.name,
                  className: 'font-mono font-medium',
                  cell: (route) => (
                    <div className="flex flex-col gap-1 py-1.5">
                      <span>{route.name}</span>
                      {route.aliases.length > 0 && (
                        <div className="flex flex-wrap gap-1">
                          {route.aliases.map((alias) => (
                            <Badge
                              key={alias.alias}
                              tone={alias.source === 'renamed' ? 'neutral' : 'accent'}
                            >
                              {alias.alias}
                            </Badge>
                          ))}
                        </div>
                      )}
                    </div>
                  ),
                },
                {
                  key: 'targets',
                  header: t.routes.targets,
                  cell: (route) => (
                    <ul className="flex flex-col gap-1 py-1.5">
                      {[...route.targets]
                        .sort((left, right) => left.priority - right.priority)
                        .map((target) => (
                          <li
                            key={`${target.channel_id}/${target.model_id}/${target.format ?? ''}`}
                            className="flex flex-wrap items-center gap-1 text-xs"
                          >
                            <span className="text-muted">
                              P{target.priority} · w{target.weight}
                            </span>
                            {target.format && <Badge tone="info">{target.format}</Badge>}
                            <span>{channelName(target.channel_id)}</span>
                            <span>/</span>
                            <span className="font-mono">
                              {modelName(target.channel_id, target.model_id)}
                            </span>
                            {target.disabled_reason && (
                              <Badge tone="warning">{target.disabled_reason}</Badge>
                            )}
                            {!target.disabled_reason && target.enabled === false && (
                              <Badge tone="warning">{t.common.disabled}</Badge>
                            )}
                          </li>
                        ))}
                    </ul>
                  ),
                },
                {
                  key: 'state',
                  header: t.routes.state,
                  cell: (route) => (
                    <div className="flex flex-wrap items-center gap-1">
                      {route.deleted_at ? (
                        <EnabledBadge enabled={false} label={t.routes.deleted} />
                      ) : (
                        <EnabledBadge enabled={route.enabled} />
                      )}
                      {routeDegraded(route) && <Badge tone="warning">{t.routes.degraded}</Badge>}
                    </div>
                  ),
                },
                {
                  key: 'enabled',
                  header: t.common.enabled,
                  cell: (route) =>
                    route.deleted_at ? (
                      <span className="text-xs text-faint">—</span>
                    ) : (
                      <RouteEnabledToggle route={route} />
                    ),
                },
                {
                  key: 'actions',
                  header: t.common.actions,
                  align: 'right',
                  cell: (route) => (
                    <div className="flex justify-end gap-1">
                      {route.deleted_at ? (
                        <Button
                          size="sm"
                          variant="ghost"
                          loading={mutations.restore.isPending}
                          onClick={() => mutations.restore.mutate(route.id)}
                        >
                          {t.common.restore}
                        </Button>
                      ) : (
                        <>
                          <Button size="sm" variant="ghost" onClick={() => setEditing(route)}>
                            {t.common.edit}
                          </Button>
                          <Button
                            size="sm"
                            variant="ghost"
                            className="text-danger"
                            onClick={() => setDeleting(route)}
                          >
                            <Trash2 aria-hidden />
                            {t.common.delete}
                          </Button>
                        </>
                      )}
                    </div>
                  ),
                },
              ]}
            />
          )}
        </QueryState>
      </Card>
      {editing !== null && (
        <RouteDialog
          projectId={projectId}
          route={editing === 'new' ? null : editing}
          pending={mutations.create.isPending || mutations.update.isPending}
          onClose={() => setEditing(null)}
          onSubmit={(input) => {
            if (editing === 'new') {
              mutations.create.mutate(input, { onSuccess: () => setEditing(null) });
            } else {
              mutations.update.mutate(
                { routeId: editing.id, input: { ...input, version: editing.version } },
                { onSuccess: () => setEditing(null) },
              );
            }
          }}
        />
      )}
      {creatingFromModels && (
        <FromModelsDialog projectId={projectId} onClose={() => setCreatingFromModels(false)} />
      )}
      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
        title={interpolate(t.routes.deleteTitle, { name: deleting?.name ?? '' })}
        description={deleting ? deleteDescription(deleting) : t.routes.deleteHint}
        confirmLabel={t.common.delete}
        pending={mutations.remove.isPending}
        onConfirm={() =>
          deleting && mutations.remove.mutate(deleting.id, { onSuccess: () => setDeleting(null) })
        }
      />
    </Page>
  );
}
