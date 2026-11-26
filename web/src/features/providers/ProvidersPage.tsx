import { Pencil, PlugZap, Plus, RefreshCw, ShieldAlert, Trash2 } from 'lucide-react';
import { useState } from 'react';
import { useGetConfigurationHealth } from '../../api/generated/configuration/configuration';
import { useListProviders } from '../../api/generated/providers/providers';
import type { ConsoleProviderView } from '../../api/generated/models';
import { DataTable, TableSkeleton } from '../../components/data/DataTable';
import { QueryState } from '../../components/feedback/QueryState';
import { ConfirmDialog } from '../../components/form/ConfirmDialog';
import { Page } from '../../components/layout/Page';
import { PageHeader } from '../../components/layout/PageHeader';
import { Badge } from '../../components/ui/badge';
import { Button } from '../../components/ui/button';
import { Card, CardHeader } from '../../components/ui/card';
import { Empty } from '../../components/ui/empty';
import { Progress } from '../../components/ui/progress';
import { Switch } from '../../components/ui/switch';
import { interpolate, useLocale } from '../../i18n';
import type { Messages } from '../../i18n/messages';
import { formatNumber } from '../../lib/format';
import { ConnectProviderWizard } from './components/ConnectProviderWizard';
import { ProviderDialog, type ProviderDraft } from './components/ProviderDialog';
import { useCreateProvider, useDeleteProvider, useResetProvider, useUpdateProvider } from './hooks';

function warningMessage(t: Messages, code: string, fallback: string) {
  const codes = t.providers.warningCodes as Record<string, string>;
  return codes[code] ?? fallback;
}

function ConfigurationHealth() {
  const { t } = useLocale();
  const health = useGetConfigurationHealth();
  return (
    <Card>
      <CardHeader title={t.providers.healthTitle} description={t.providers.healthHint} />
      <QueryState query={health} loading={<TableSkeleton rows={2} />}>
        {(view) => (
          <div className="flex flex-col gap-4 p-4">
            <dl className="grid grid-cols-2 gap-3 sm:grid-cols-4">
              {(
                [
                  ['providers', view.providers],
                  ['channels', view.channels],
                  ['models', view.models],
                  ['routes', view.routes],
                ] as const
              ).map(([key, value]) => (
                <div key={key} className="rounded-md border bg-sunken/40 px-3 py-2">
                  <dt className="text-xs text-muted">{t.providers.counts[key]}</dt>
                  <dd className="num text-xl font-semibold">{formatNumber(value)}</dd>
                </div>
              ))}
            </dl>
            {view.warnings.length === 0 ? (
              <p className="flex items-center gap-2 text-sm text-success">
                <ShieldAlert aria-hidden className="size-4" />
                {t.providers.noWarnings}
              </p>
            ) : (
              <div className="flex flex-col gap-2">
                <div className="flex items-center justify-between gap-2">
                  <span className="text-xs font-medium text-warning">
                    {interpolate(t.providers.warningCount, { count: view.warnings.length })}
                  </span>
                  <Progress
                    value={view.warnings.length}
                    max={Math.max(view.warnings.length, 10)}
                    tone="warning"
                    label={t.providers.healthTitle}
                    className="w-32"
                  />
                </div>
                <ul className="scroll-thin flex max-h-40 flex-col gap-1.5 overflow-y-auto">
                  {view.warnings.map((warning) => (
                    <li
                      key={`${warning.code}:${warning.subject}`}
                      className="rounded-md border border-warning/25 bg-warning-soft/50 px-3 py-1.5 text-xs"
                    >
                      <span className="font-mono text-faint">{warning.subject}</span>{' '}
                      <span className="text-ink">
                        {warningMessage(t, warning.code, warning.message)}
                      </span>
                    </li>
                  ))}
                </ul>
              </div>
            )}
          </div>
        )}
      </QueryState>
    </Card>
  );
}

function ProviderToggle({ provider }: { provider: ConsoleProviderView }) {
  const { t } = useLocale();
  const update = useUpdateProvider(provider.id);
  return (
    <Switch
      aria-label={`${provider.label}: ${provider.enabled ? t.common.enabled : t.common.disabled}`}
      checked={provider.enabled}
      onCheckedChange={(enabled) =>
        update.mutate({
          label: provider.label,
          endpoints: provider.endpoints,
          enabled,
          version: provider.version,
        })
      }
    />
  );
}

function ProviderEditor({
  provider,
  onClose,
}: {
  provider: ConsoleProviderView | null;
  onClose: () => void;
}) {
  const create = useCreateProvider();
  const update = useUpdateProvider(provider?.id ?? '');
  const mutation = provider ? update : create;
  return (
    <ProviderDialog
      provider={provider}
      pending={mutation.isPending}
      onClose={onClose}
      onSubmit={(draft: ProviderDraft) => {
        const input = {
          label: draft.label.trim(),
          endpoints: draft.endpoints,
          enabled: draft.enabled,
          version: provider?.version,
        };
        if (provider) {
          update.mutate(input, { onSuccess: onClose });
        } else {
          create.mutate(input, { onSuccess: onClose });
        }
      }}
    />
  );
}

export function ProvidersPage() {
  const { t } = useLocale();
  const providers = useListProviders();
  const remove = useDeleteProvider();
  const reset = useResetProvider();
  const [wizard, setWizard] = useState(false);
  const [editing, setEditing] = useState<ConsoleProviderView | 'new' | null>(null);
  const [deleting, setDeleting] = useState<ConsoleProviderView | null>(null);
  const [resetting, setResetting] = useState<ConsoleProviderView | null>(null);
  const rows = providers.data ?? [];
  return (
    <Page>
      <PageHeader
        title={t.providers.title}
        description={t.providers.description}
        actions={
          <>
            <Button onClick={() => setEditing('new')}>
              <Plus aria-hidden />
              {t.providers.add}
            </Button>
            <Button variant="primary" onClick={() => setWizard(true)}>
              <PlugZap aria-hidden />
              {t.providers.connect}
            </Button>
          </>
        }
      />
      <ConfigurationHealth />
      <Card>
        <QueryState query={providers} loading={<TableSkeleton />}>
          {(list) => (
            <DataTable
              rows={list}
              rowKey={(row) => row.id}
              empty={
                <Empty
                  icon={PlugZap}
                  title={t.providers.emptyTitle}
                  description={t.providers.emptyHint}
                />
              }
              columns={[
                {
                  key: 'label',
                  header: t.common.label,
                  className: 'font-medium',
                  cell: (row) => (
                    <div className="flex items-center gap-2">
                      <span>{row.label}</span>
                      <Badge tone={row.source === 'builtin' ? 'accent' : 'neutral'}>
                        {t.providers.sources[row.source]}
                      </Badge>
                    </div>
                  ),
                },
                {
                  key: 'formats',
                  header: t.providers.formats,
                  cell: (row) => (
                    <div className="flex flex-wrap gap-1 py-1">
                      {(row.formats ?? []).map((format) => (
                        <Badge key={format}>{format}</Badge>
                      ))}
                    </div>
                  ),
                },
                {
                  key: 'channels',
                  header: t.providers.channelCount,
                  className: 'num',
                  cell: (row) => formatNumber(row.channel_count),
                },
                {
                  key: 'enabled',
                  header: t.common.enabled,
                  cell: (row) => <ProviderToggle provider={row} />,
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
                      {row.source === 'builtin' ? (
                        <Button size="sm" variant="ghost" onClick={() => setResetting(row)}>
                          <RefreshCw aria-hidden />
                          {t.providers.reset}
                        </Button>
                      ) : (
                        <Button
                          size="sm"
                          variant="ghost"
                          className="text-danger"
                          onClick={() => setDeleting(row)}
                        >
                          <Trash2 aria-hidden />
                          {t.common.delete}
                        </Button>
                      )}
                    </div>
                  ),
                },
              ]}
            />
          )}
        </QueryState>
      </Card>
      {wizard && <ConnectProviderWizard open onOpenChange={setWizard} providers={rows} />}
      {editing !== null && (
        <ProviderEditor
          provider={editing === 'new' ? null : editing}
          onClose={() => setEditing(null)}
        />
      )}
      <ConfirmDialog
        open={resetting !== null}
        onOpenChange={(open) => !open && setResetting(null)}
        title={interpolate(t.providers.resetTitle, { label: resetting?.label ?? '' })}
        description={t.providers.resetHint}
        confirmLabel={t.providers.reset}
        pending={reset.isPending}
        onConfirm={() =>
          resetting && reset.mutate(resetting.id, { onSuccess: () => setResetting(null) })
        }
      />
      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
        title={interpolate(t.providers.deleteTitle, { label: deleting?.label ?? '' })}
        description={
          (deleting?.channel_count ?? 0) > 0
            ? interpolate(t.providers.deleteReferenced, { count: deleting?.channel_count ?? 0 })
            : t.providers.deleteHint
        }
        confirmLabel={t.common.delete}
        pending={remove.isPending}
        onConfirm={() =>
          deleting && remove.mutate(deleting.id, { onSuccess: () => setDeleting(null) })
        }
      />
    </Page>
  );
}
