import { Boxes, DatabaseZap, Pencil, Plus, RotateCcw, Trash2 } from 'lucide-react';
import { useMemo, useState } from 'react';
import { useListProjects } from '../../api/generated/projects/projects';
import { useListProviders } from '../../api/generated/providers/providers';
import type { ConsoleCatalogModelInput, ConsoleCatalogModelView } from '../../api/generated/models';
import { DataTable, TableSkeleton } from '../../components/data/DataTable';
import { FilterBar } from '../../components/data/FilterBar';
import { EnabledBadge } from '../../components/feedback/Badges';
import { QueryState } from '../../components/feedback/QueryState';
import { Page } from '../../components/layout/Page';
import { PageHeader } from '../../components/layout/PageHeader';
import { Badge } from '../../components/ui/badge';
import { Button } from '../../components/ui/button';
import { Card, CardHeader } from '../../components/ui/card';
import { Empty } from '../../components/ui/empty';
import { Switch } from '../../components/ui/switch';
import { interpolate, useLocale } from '../../i18n';
import { formatCost, formatDate } from '../../lib/format';
import { DeleteImpactDialog } from '../providers/components/DeleteImpactDialog';
import { formatsForKind, parseEndpointCatalog, providerFormats } from '../providers/endpoints';
import { useConfigurationImpact } from '../providers/hooks';
import { projectNameMap, replacementOptions } from '../providers/impact';
import { ImportModelsDialog } from './components/ImportModelsDialog';
import { ModelDialog } from './components/ModelDialog';
import {
  useCreateModel,
  useDeleteModel,
  useModels,
  useRestoreModel,
  useToggleModel,
  useUpdateModel,
} from './hooks';
import { groupModelsByProvider } from './models';

function ModelToggle({ model }: { model: ConsoleCatalogModelView }) {
  const { t } = useLocale();
  const toggle = useToggleModel(model.id);
  return (
    <Switch
      aria-label={`${model.upstream_name}: ${model.enabled ? t.common.enabled : t.common.disabled}`}
      checked={model.enabled ?? false}
      onCheckedChange={(enabled) => toggle.mutate({ model, enabled })}
    />
  );
}

function ModelRestore({ model }: { model: ConsoleCatalogModelView }) {
  const { t } = useLocale();
  const restore = useRestoreModel(model.id);
  return (
    <Button size="sm" variant="ghost" loading={restore.isPending} onClick={() => restore.mutate()}>
      <RotateCcw aria-hidden />
      {t.common.restore}
    </Button>
  );
}

export function ModelsPage() {
  const { t, locale } = useLocale();
  const providers = useListProviders();
  const projects = useListProjects();
  const [search, setSearch] = useState('');
  const [providerId, setProviderId] = useState('');
  const [kind, setKind] = useState('');
  const [usage, setUsage] = useState('');
  const [showDeleted, setShowDeleted] = useState(false);
  const models = useModels({
    providerId: providerId || undefined,
    unassociated: usage === 'unassociated',
    includeDeleted: showDeleted,
  });
  const create = useCreateModel();
  const [editing, setEditing] = useState<ConsoleCatalogModelView | 'new' | null>(null);
  const update = useUpdateModel(editing !== null && editing !== 'new' ? editing.id : '');
  const remove = useDeleteModel();
  const [deleting, setDeleting] = useState<ConsoleCatalogModelView | null>(null);
  const [importing, setImporting] = useState(false);
  const impact = useConfigurationImpact(deleting ? { modelId: deleting.id } : null);
  const providerRows = providers.data ?? [];
  const providerLabels = useMemo(
    () => new Map(providerRows.map((provider) => [provider.id, provider.label])),
    [providerRows],
  );
  const providerFormatsByID = useMemo(
    () =>
      new Map(
        providerRows.map((provider) => [provider.id, parseEndpointCatalog(provider.endpoints)]),
      ),
    [providerRows],
  );
  const rows = (models.data ?? []).filter((model) => {
    if (kind !== '' && model.kind !== kind) return false;
    if (search.trim() !== '') {
      const needle = search.trim().toLowerCase();
      const haystack = `${model.upstream_name} ${model.display_name ?? ''}`;
      if (!haystack.toLowerCase().includes(needle)) return false;
    }
    return true;
  });
  const groups = groupModelsByProvider(rows, providerLabels);
  const replacements =
    deleting === null
      ? []
      : replacementOptions(
          (models.data ?? []).filter(
            (model) =>
              model.provider_id === deleting.provider_id &&
              model.kind === deleting.kind &&
              !model.deleted_at,
          ),
          deleting.id,
          (model) => model.upstream_name,
        );
  const submit = (input: ConsoleCatalogModelInput) => {
    if (editing === 'new') {
      create.mutate(input, { onSuccess: () => setEditing(null) });
      return;
    }
    if (editing) {
      update.mutate(input, { onSuccess: () => setEditing(null) });
    }
  };
  const formatsOf = (model: ConsoleCatalogModelView) => {
    const catalog = providerFormatsByID.get(model.provider_id);
    return catalog ? providerFormats(catalog) : model.formats;
  };
  return (
    <Page>
      <PageHeader
        title={t.models.title}
        description={t.models.description}
        actions={
          <>
            <Button onClick={() => setImporting(true)}>
              <DatabaseZap aria-hidden />
              {t.models.import}
            </Button>
            <Button variant="primary" onClick={() => setEditing('new')}>
              <Plus aria-hidden />
              {t.models.add}
            </Button>
          </>
        }
      />
      <Card>
        <div className="border-b p-3">
          <FilterBar
            searchLabel={t.models.search}
            searchPlaceholder={t.models.searchPlaceholder}
            search={search}
            onSearchChange={setSearch}
            selects={[
              {
                key: 'provider',
                label: t.models.provider,
                value: providerId,
                options: [
                  { value: '', label: t.models.allProviders },
                  ...providerRows.map((provider) => ({
                    value: provider.id,
                    label: provider.label,
                  })),
                ],
                onChange: setProviderId,
              },
              {
                key: 'kind',
                label: t.models.kind,
                value: kind,
                options: [
                  { value: '', label: t.models.allKinds },
                  ...(['generation', 'embedding', 'moderation'] as const).map((entry) => ({
                    value: entry,
                    label: t.models.kinds[entry],
                  })),
                ],
                onChange: setKind,
              },
              {
                key: 'usage',
                label: t.models.usage,
                value: usage,
                options: [
                  { value: '', label: t.models.allModels },
                  { value: 'unassociated', label: t.models.unassociated },
                ],
                onChange: setUsage,
              },
              {
                key: 'deleted',
                label: t.models.deletedFilter,
                value: showDeleted ? 'show' : '',
                options: [
                  { value: '', label: t.models.hideDeleted },
                  { value: 'show', label: t.models.showDeleted },
                ],
                onChange: (value) => setShowDeleted(value === 'show'),
              },
            ]}
            onClear={() => {
              setSearch('');
              setProviderId('');
              setKind('');
              setUsage('');
              setShowDeleted(false);
            }}
          />
        </div>
        <QueryState query={models} loading={<TableSkeleton rows={6} />}>
          {() =>
            groups.length === 0 ? (
              <Empty
                icon={Boxes}
                title={(models.data?.length ?? 0) > 0 ? t.common.noResults : t.models.emptyTitle}
                description={t.models.emptyHint}
              />
            ) : (
              <div className="flex flex-col gap-4 p-3">
                {groups.map((group) => {
                  const catalog = providerFormatsByID.get(group.providerId);
                  return (
                    <Card key={group.providerId}>
                      <CardHeader
                        title={group.label}
                        description={catalog ? providerFormats(catalog).join(' · ') : undefined}
                      />
                      <DataTable
                        rows={group.models}
                        rowKey={(model) => model.id}
                        columns={[
                          {
                            key: 'model',
                            header: t.models.model,
                            className: 'font-medium',
                            cell: (model) => (
                              <div className="flex flex-col">
                                <span className="font-mono">{model.upstream_name}</span>
                                {model.deleted_at ? (
                                  <span className="text-xs text-danger">
                                    {interpolate(t.models.deletedAt, {
                                      time: formatDate(model.deleted_at, locale),
                                    })}
                                  </span>
                                ) : (
                                  model.display_name && (
                                    <span className="text-xs text-muted">{model.display_name}</span>
                                  )
                                )}
                              </div>
                            ),
                          },
                          {
                            key: 'kind',
                            header: t.models.kind,
                            cell: (model) => <Badge>{t.models.kinds[model.kind]}</Badge>,
                          },
                          {
                            key: 'formats',
                            header: t.models.formats,
                            cell: (model) => {
                              const formats = formatsOf(model);
                              const derived = formatsForKind(catalog ?? {}, model.kind);
                              return (
                                <div className="flex flex-wrap gap-1 py-1">
                                  {(formats.length > 0 ? formats : derived).map((format) => (
                                    <Badge key={format}>{format}</Badge>
                                  ))}
                                </div>
                              );
                            },
                          },
                          {
                            key: 'price',
                            header: t.models.price,
                            className: 'num text-xs',
                            cell: (model) => (
                              <div className="flex flex-col">
                                <span>
                                  {formatCost(model.input_price, model.currency)} /{' '}
                                  {formatCost(model.output_price, model.currency)}
                                </span>
                                <span className="text-[11px] text-faint">{model.currency}</span>
                              </div>
                            ),
                          },
                          {
                            key: 'enabled',
                            header: t.common.enabled,
                            cell: (model) =>
                              model.deleted_at ? (
                                <EnabledBadge enabled={false} label={t.models.deleted} />
                              ) : (
                                <ModelToggle model={model} />
                              ),
                          },
                          {
                            key: 'actions',
                            header: t.common.actions,
                            align: 'right',
                            cell: (model) => (
                              <div className="flex justify-end gap-1">
                                {model.deleted_at ? (
                                  <ModelRestore model={model} />
                                ) : (
                                  <>
                                    <Button
                                      size="sm"
                                      variant="ghost"
                                      onClick={() => setEditing(model)}
                                    >
                                      <Pencil aria-hidden />
                                      {t.common.edit}
                                    </Button>
                                    <Button
                                      size="sm"
                                      variant="ghost"
                                      className="text-danger"
                                      onClick={() => setDeleting(model)}
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
                    </Card>
                  );
                })}
              </div>
            )
          }
        </QueryState>
      </Card>
      {editing !== null && (
        <ModelDialog
          model={editing === 'new' ? null : editing}
          pending={create.isPending || update.isPending}
          onClose={() => setEditing(null)}
          onSubmit={submit}
        />
      )}
      {importing && <ImportModelsDialog onClose={() => setImporting(false)} />}
      <DeleteImpactDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
        title={interpolate(t.models.deleteTitle, { name: deleting?.upstream_name ?? '' })}
        description={t.impact.warning}
        confirmLabel={t.common.delete}
        pending={remove.isPending}
        impact={impact.data}
        loading={impact.isPending}
        error={impact.error}
        projectNames={projectNameMap(projects.data)}
        replacements={replacements}
        onConfirm={(replacementId) => {
          if (!deleting || !impact.data) return;
          remove.mutate(
            { model: deleting, impact: impact.data, replacementId },
            { onSuccess: () => setDeleting(null) },
          );
        }}
      />
    </Page>
  );
}
