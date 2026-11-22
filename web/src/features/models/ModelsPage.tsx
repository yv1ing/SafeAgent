import { Boxes, Pencil, Plus } from 'lucide-react';
import { useState } from 'react';
import { useListModels } from '../../api/generated/models/models';
import type {
  ConsoleCatalogModelInput,
  ConsoleCatalogModelView,
  ConsoleUpdateCatalogModelInput,
} from '../../api/generated/models';
import { ConfirmDialog } from '../../components/form/ConfirmDialog';
import { DataTable, TableSkeleton } from '../../components/data/DataTable';
import { QueryState } from '../../components/feedback/QueryState';
import { Page } from '../../components/layout/Page';
import { PageHeader } from '../../components/layout/PageHeader';
import { Badge } from '../../components/ui/badge';
import { Button } from '../../components/ui/button';
import { Card } from '../../components/ui/card';
import { Empty } from '../../components/ui/empty';
import { interpolate, useLocale } from '../../i18n';
import { formatCost } from '../../lib/format';
import { ModelDialog } from './components/ModelDialog';
import { useModelMutations } from './hooks';

export function ModelsPage() {
  const { t } = useLocale();
  const models = useListModels();
  const mutations = useModelMutations();
  const [editing, setEditing] = useState<ConsoleCatalogModelView | 'new' | null>(null);
  const [deleting, setDeleting] = useState<ConsoleCatalogModelView | null>(null);
  const submit = (input: ConsoleCatalogModelInput) => {
    if (editing === 'new') {
      mutations.create.mutate(input, { onSuccess: () => setEditing(null) });
      return;
    }
    if (editing) {
      mutations.update.mutate(
        { modelId: editing.id, input: input as ConsoleUpdateCatalogModelInput },
        { onSuccess: () => setEditing(null) },
      );
    }
  };
  return (
    <Page>
      <PageHeader
        title={t.models.title}
        description={t.models.description}
        actions={
          <Button variant="primary" onClick={() => setEditing('new')}>
            <Plus aria-hidden />
            {t.models.add}
          </Button>
        }
      />
      <Card>
        <QueryState query={models} loading={<TableSkeleton />}>
          {(rows) => (
            <DataTable
              rows={rows}
              rowKey={(row) => row.id}
              empty={
                <Empty icon={Boxes} title={t.models.emptyTitle} description={t.models.emptyHint} />
              }
              columns={[
                {
                  key: 'name',
                  header: t.models.model,
                  className: 'font-medium',
                  cell: (row) => (
                    <div className="flex flex-col">
                      <span className="font-mono">{row.upstream_name}</span>
                      {row.display_name && (
                        <span className="text-xs text-muted">{row.display_name}</span>
                      )}
                    </div>
                  ),
                },
                {
                  key: 'provider',
                  header: t.models.provider,
                  className: 'font-mono text-xs',
                  cell: (row) => row.provider_id,
                },
                {
                  key: 'kind',
                  header: t.models.kind,
                  cell: (row) => <Badge>{t.models.kinds[row.kind]}</Badge>,
                },
                {
                  key: 'formats',
                  header: t.models.formats,
                  className: 'font-mono text-xs text-muted',
                  cell: (row) => row.formats.join(', '),
                },
                {
                  key: 'price',
                  header: t.models.price,
                  className: 'num text-xs',
                  cell: (row) =>
                    `${formatCost(row.input_price, row.currency)} / ${formatCost(row.output_price, row.currency)} ${row.currency}`,
                },
                {
                  key: 'enabled',
                  header: t.common.enabled,
                  cell: (row) =>
                    row.enabled ? (
                      <Badge tone="success">{t.common.enabled}</Badge>
                    ) : (
                      <Badge>{t.common.disabled}</Badge>
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
        <ModelDialog
          model={editing === 'new' ? null : editing}
          pending={mutations.create.isPending || mutations.update.isPending}
          onClose={() => setEditing(null)}
          onSubmit={submit}
        />
      )}
      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
        title={interpolate(t.models.deleteTitle, { name: deleting?.upstream_name ?? '' })}
        description={t.models.deleteHint}
        confirmLabel={t.common.delete}
        pending={mutations.remove.isPending}
        onConfirm={() =>
          deleting && mutations.remove.mutate(deleting.id, { onSuccess: () => setDeleting(null) })
        }
      />
    </Page>
  );
}
