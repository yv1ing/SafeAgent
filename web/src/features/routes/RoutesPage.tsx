import { Pencil, Plus, Route } from 'lucide-react';
import { useState } from 'react';
import type { ConsoleRouteView } from '../../api/generated/models';
import { useListChannelOptions } from '../../api/generated/channel-options/channel-options';
import { useListRoutes } from '../../api/generated/routes/routes';
import { useProjectId } from '../../app/context';
import { ConfirmDialog } from '../../components/form/ConfirmDialog';
import { DataTable, TableSkeleton } from '../../components/data/DataTable';
import { Page } from '../../components/layout/Page';
import { PageHeader } from '../../components/layout/PageHeader';
import { QueryState } from '../../components/feedback/QueryState';
import { Button } from '../../components/ui/button';
import { Card } from '../../components/ui/card';
import { Empty } from '../../components/ui/empty';
import { interpolate, useLocale } from '../../i18n';
import { RouteDialog } from './components/RouteDialog';
import { useRouteMutations } from './hooks';

export function RoutesPage() {
  const { t } = useLocale();
  const projectId = useProjectId();
  const routes = useListRoutes(projectId);
  const channels = useListChannelOptions(projectId);
  const mutations = useRouteMutations(projectId);
  const [editing, setEditing] = useState<ConsoleRouteView | 'new' | null>(null);
  const [deleting, setDeleting] = useState<ConsoleRouteView | null>(null);
  const channelName = (channelId: string) =>
    channels.data?.find((channel) => channel.id === channelId)?.name ?? channelId;
  const close = () => setEditing(null);

  return (
    <Page>
      <PageHeader
        title={t.routes.title}
        description={t.routes.description}
        actions={
          <Button variant="primary" onClick={() => setEditing('new')}>
            <Plus aria-hidden />
            {t.routes.add}
          </Button>
        }
      />
      <Card>
        <QueryState query={routes} loading={<TableSkeleton />}>
          {(rows) => (
            <DataTable
              rows={rows}
              rowKey={(row) => row.id}
              empty={
                <Empty icon={Route} title={t.routes.emptyTitle} description={t.routes.emptyHint} />
              }
              columns={[
                {
                  key: 'name',
                  header: t.common.name,
                  className: 'font-mono font-medium',
                  cell: (row) => row.name,
                },
                {
                  key: 'targets',
                  header: t.routes.targets,
                  cell: (row) => (
                    <ul className="flex flex-col gap-0.5 py-1.5">
                      {[...row.targets]
                        .sort((left, right) => left.priority - right.priority)
                        .map((target) => (
                          <li
                            key={`${target.channel_id}/${target.upstream_model}`}
                            className="text-xs"
                          >
                            <span className="text-muted">
                              P{target.priority} · w{target.weight} ·{' '}
                            </span>
                            {channelName(target.channel_id)} /{' '}
                            <span className="font-mono">{target.upstream_model}</span>
                          </li>
                        ))}
                    </ul>
                  ),
                },
                {
                  key: 'actions',
                  header: t.common.actions,
                  className: 'text-right',
                  cell: (row) => (
                    <div className="flex justify-end gap-1">
                      <Button size="sm" variant="ghost" onClick={() => setEditing(row)}>
                        <Pencil aria-hidden />
                        {t.common.edit}
                      </Button>
                      <Button
                        size="sm"
                        variant="ghost"
                        className="text-danger"
                        onClick={() => setDeleting(row)}
                      >
                        {t.common.delete}
                      </Button>
                    </div>
                  ),
                },
              ]}
            />
          )}
        </QueryState>
      </Card>
      {editing && (
        <RouteDialog
          projectId={projectId}
          route={editing === 'new' ? null : editing}
          pending={mutations.create.isPending || mutations.update.isPending}
          onClose={close}
          onSubmit={(input) =>
            editing === 'new'
              ? mutations.create.mutate(input, { onSuccess: close })
              : mutations.update.mutate({ routeId: editing.id, input }, { onSuccess: close })
          }
        />
      )}
      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
        title={interpolate(t.routes.deleteTitle, { name: deleting?.name ?? '' })}
        description={t.common.irreversible}
        confirmLabel={t.common.delete}
        pending={mutations.remove.isPending}
        onConfirm={() =>
          deleting && mutations.remove.mutate(deleting.id, { onSuccess: () => setDeleting(null) })
        }
      />
    </Page>
  );
}
