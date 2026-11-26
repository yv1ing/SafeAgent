import { Link, useNavigate } from '@tanstack/react-router';
import { Cable, Plus } from 'lucide-react';
import { useMemo, useState } from 'react';
import { useListProviders } from '../../api/generated/providers/providers';
import type { ConsoleChannelInput, ConsoleChannelView } from '../../api/generated/models';
import { DataTable, TableSkeleton } from '../../components/data/DataTable';
import { FilterBar } from '../../components/data/FilterBar';
import { EnabledBadge } from '../../components/feedback/Badges';
import { QueryState } from '../../components/feedback/QueryState';
import { FormDialog } from '../../components/form/FormDialog';
import { Page } from '../../components/layout/Page';
import { PageHeader } from '../../components/layout/PageHeader';
import { Badge } from '../../components/ui/badge';
import { Button } from '../../components/ui/button';
import { Card } from '../../components/ui/card';
import { Empty } from '../../components/ui/empty';
import { Switch } from '../../components/ui/switch';
import { interpolate, useLocale } from '../../i18n';
import { formatDate, formatNumber } from '../../lib/format';
import { ChannelFields } from './components/ChannelFields';
import { useChannels, useCreateChannel, useRestoreChannel, useToggleChannel } from './hooks';

function ChannelEnabledToggle({ channel }: { channel: ConsoleChannelView }) {
  const { t } = useLocale();
  const toggle = useToggleChannel(channel.id);
  return (
    <Switch
      aria-label={`${channel.name}: ${channel.enabled ? t.common.enabled : t.common.disabled}`}
      checked={channel.enabled ?? false}
      onCheckedChange={(enabled) => toggle.mutate({ channel, enabled })}
    />
  );
}

function ChannelRestore({ channel }: { channel: ConsoleChannelView }) {
  const { t } = useLocale();
  const restore = useRestoreChannel(channel.id);
  return (
    <Button size="sm" variant="ghost" loading={restore.isPending} onClick={() => restore.mutate()}>
      {t.common.restore}
    </Button>
  );
}

export function ChannelsPage() {
  const { t, locale } = useLocale();
  const providers = useListProviders();
  const navigate = useNavigate();
  const [includeDeleted, setIncludeDeleted] = useState(false);
  const channels = useChannels(includeDeleted);
  const create = useCreateChannel();
  const [search, setSearch] = useState('');
  const [providerId, setProviderId] = useState('');
  const [status, setStatus] = useState('');
  const [draft, setDraft] = useState<ConsoleChannelInput | null>(null);
  const providerRows = providers.data ?? [];
  const providerLabels = useMemo(
    () => new Map(providerRows.map((provider) => [provider.id, provider.label])),
    [providerRows],
  );
  const providerFormats = useMemo(
    () => new Map(providerRows.map((provider) => [provider.id, provider.formats ?? []])),
    [providerRows],
  );
  const rows = (channels.data ?? []).filter((channel) => {
    if (providerId !== '' && channel.provider_id !== providerId) return false;
    if (status === 'enabled' && channel.enabled !== true) return false;
    if (status === 'disabled' && channel.enabled === true) return false;
    if (search.trim() !== '') {
      const needle = search.trim().toLowerCase();
      const haystack = `${channel.name} ${channel.base_url ?? ''} ${channel.provider_label}`;
      if (!haystack.toLowerCase().includes(needle)) return false;
    }
    return true;
  });
  const startCreate = () => {
    setDraft({
      name: '',
      provider_id: providerRows[0]?.id ?? '',
      base_url: '',
      enabled: true,
    });
  };
  return (
    <Page>
      <PageHeader
        title={t.channels.title}
        description={t.channels.description}
        actions={
          <Button variant="primary" onClick={startCreate} disabled={providerRows.length === 0}>
            <Plus aria-hidden />
            {t.channels.add}
          </Button>
        }
      />
      <Card>
        <div className="border-b p-3">
          <FilterBar
            searchLabel={t.channels.search}
            searchPlaceholder={t.channels.searchPlaceholder}
            search={search}
            onSearchChange={setSearch}
            selects={[
              {
                key: 'provider',
                label: t.channels.provider,
                value: providerId,
                options: [
                  { value: '', label: t.channels.allProviders },
                  ...providerRows.map((provider) => ({
                    value: provider.id,
                    label: provider.label,
                  })),
                ],
                onChange: setProviderId,
              },
              {
                key: 'status',
                label: t.channels.status,
                value: status,
                options: [
                  { value: '', label: t.channels.allStates },
                  { value: 'enabled', label: t.common.enabled },
                  { value: 'disabled', label: t.common.disabled },
                ],
                onChange: setStatus,
              },
              {
                key: 'deleted',
                label: t.channels.deletedFilter,
                value: includeDeleted ? 'show' : '',
                options: [
                  { value: '', label: t.channels.hideDeleted },
                  { value: 'show', label: t.channels.showDeleted },
                ],
                onChange: (value) => setIncludeDeleted(value === 'show'),
              },
            ]}
            onClear={() => {
              setSearch('');
              setProviderId('');
              setStatus('');
              setIncludeDeleted(false);
            }}
          />
        </div>
        <QueryState query={channels} loading={<TableSkeleton />}>
          {() => (
            <DataTable
              rows={rows}
              rowKey={(row) => row.id}
              empty={
                <Empty
                  icon={Cable}
                  title={
                    rows.length === 0 && (channels.data?.length ?? 0) > 0
                      ? t.common.noResults
                      : t.channels.emptyTitle
                  }
                  description={t.channels.emptyHint}
                />
              }
              columns={[
                {
                  key: 'name',
                  header: t.common.name,
                  className: 'font-medium',
                  cell: (row) =>
                    row.deleted_at ? (
                      <div className="flex flex-col">
                        <span>{row.name}</span>
                        <span className="text-xs text-danger">
                          {interpolate(t.channels.deletedAt, {
                            time: formatDate(row.deleted_at, locale),
                          })}
                        </span>
                      </div>
                    ) : (
                      <Link
                        to="/channels/$channelId"
                        params={{ channelId: row.id }}
                        className="hover:text-accent hover:underline"
                      >
                        {row.name}
                      </Link>
                    ),
                },
                {
                  key: 'provider',
                  header: t.channels.provider,
                  cell: (row) => (
                    <span className="text-sm">
                      {row.provider_label || providerLabels.get(row.provider_id) || row.provider_id}
                    </span>
                  ),
                },
                {
                  key: 'base_url',
                  header: t.channels.baseUrl,
                  className: 'max-w-56 truncate font-mono text-xs text-muted',
                  cell: (row) => row.base_url || t.channels.inheritedHost,
                },
                {
                  key: 'formats',
                  header: t.channels.formats,
                  cell: (row) => (
                    <div className="flex flex-wrap gap-1 py-1">
                      {(providerFormats.get(row.provider_id) ?? []).map((format) => (
                        <Badge key={format}>{format}</Badge>
                      ))}
                    </div>
                  ),
                },
                {
                  key: 'routes',
                  header: t.channels.routeCount,
                  className: 'num',
                  cell: (row) => formatNumber(row.route_count),
                },
                {
                  key: 'enabled',
                  header: t.common.enabled,
                  cell: (row) =>
                    row.deleted_at ? (
                      <EnabledBadge enabled={false} label={t.channels.deleted} />
                    ) : (
                      <ChannelEnabledToggle channel={row} />
                    ),
                },
                {
                  key: 'actions',
                  header: t.common.actions,
                  align: 'right',
                  cell: (row) =>
                    row.deleted_at ? (
                      <ChannelRestore channel={row} />
                    ) : (
                      <Link
                        to="/channels/$channelId"
                        params={{ channelId: row.id }}
                        className="text-sm text-accent hover:underline"
                      >
                        {t.common.edit}
                      </Link>
                    ),
                },
              ]}
            />
          )}
        </QueryState>
      </Card>
      {draft && (
        <FormDialog
          open
          wide
          onOpenChange={(open) => !open && setDraft(null)}
          title={t.channels.add}
          description={t.channels.addHint}
          submitLabel={t.common.create}
          pending={create.isPending}
          disabled={draft.name.trim() === '' || draft.provider_id === ''}
          onSubmit={() =>
            create.mutate(draft, {
              onSuccess: (channel) => {
                setDraft(null);
                void navigate({ to: '/channels/$channelId', params: { channelId: channel.id } });
              },
            })
          }
        >
          <ChannelFields draft={draft} providers={providerRows} basic onChange={setDraft} />
        </FormDialog>
      )}
    </Page>
  );
}
