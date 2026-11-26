import { Link, useNavigate, useParams } from '@tanstack/react-router';
import { ChevronLeft, RotateCcw, Sparkles, Trash2, Unplug } from 'lucide-react';
import { useState } from 'react';
import { useGetChannel, useGetChannelHealth } from '../../api/generated/channels/channels';
import { useListProjects } from '../../api/generated/projects/projects';
import { useListProviders } from '../../api/generated/providers/providers';
import type { ConsoleChannelView, ConsoleProviderView } from '../../api/generated/models';
import { TableSkeleton } from '../../components/data/DataTable';
import { HealthList } from '../../components/data/HealthList';
import { Callout } from '../../components/feedback/Callout';
import { QueryState } from '../../components/feedback/QueryState';
import { ConfirmDialog } from '../../components/form/ConfirmDialog';
import { Page } from '../../components/layout/Page';
import { PageHeader } from '../../components/layout/PageHeader';
import { Button } from '../../components/ui/button';
import { Card, CardHeader } from '../../components/ui/card';
import { interpolate, useLocale } from '../../i18n';
import { formatDate } from '../../lib/format';
import { DeleteImpactDialog } from '../providers/components/DeleteImpactDialog';
import { discoveryFormats, parseEndpointCatalog, providerFormats } from '../providers/endpoints';
import { useConfigurationImpact } from '../providers/hooks';
import { projectNameMap, replacementOptions } from '../providers/impact';
import { channelInput } from './channel';
import { CapabilityOverrides } from './components/CapabilityOverrides';
import { ChannelFields } from './components/ChannelFields';
import { DiscoveryDialog } from './components/DiscoveryDialog';
import { HeaderEditor, type HeaderRow } from './components/HeaderEditor';
import { KeyPool } from './components/KeyPool';
import {
  useChannels,
  useDeleteChannel,
  useDetachChannelTargets,
  useRestoreChannel,
  useUpdateChannel,
} from './hooks';

function ChannelEditor({
  channel,
  providers,
  siblings,
}: {
  channel: ConsoleChannelView;
  providers: ConsoleProviderView[];
  siblings: ConsoleChannelView[];
}) {
  const { t, locale } = useLocale();
  const navigate = useNavigate();
  const update = useUpdateChannel(channel.id);
  const remove = useDeleteChannel();
  const restore = useRestoreChannel(channel.id);
  const detach = useDetachChannelTargets(channel.id);
  const projects = useListProjects();
  const [draft, setDraft] = useState(() => channelInput(channel));
  const [headers, setHeaders] = useState<HeaderRow[]>(() => Object.entries(channel.headers ?? {}));
  const [discovering, setDiscovering] = useState<string | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [detaching, setDetaching] = useState(false);
  const impact = useConfigurationImpact(deleting ? { channelId: channel.id } : null);
  const deleted = channel.deleted_at !== undefined;
  const provider = providers.find((entry) => entry.id === channel.provider_id);
  const catalog = parseEndpointCatalog(provider?.endpoints);
  const discovery = catalog ? discoveryFormats(catalog) : [];
  const replacements = replacementOptions(
    siblings.filter((entry) => entry.provider_id === channel.provider_id && !entry.deleted_at),
    channel.id,
    (entry) => entry.name,
  );
  const save = () =>
    update.mutate({
      ...draft,
      headers: Object.fromEntries(headers.filter(([name]) => name.trim() !== '')),
    });
  return (
    <>
      <div className="flex flex-col gap-4">
        {deleted && (
          <Callout tone="warning">
            {interpolate(t.channels.deletedAt, {
              time: formatDate(channel.deleted_at, locale),
            })}{' '}
            {t.channels.deletedHint}
          </Callout>
        )}
        <Card>
          <CardHeader title={t.channels.settings} description={t.channels.settingsHint} />
          <div className="p-4">
            <ChannelFields draft={draft} providers={providers} editing onChange={setDraft} />
          </div>
        </Card>
        <Card>
          <CardHeader title={t.channels.headers} description={t.channels.headersHint} />
          <HeaderEditor rows={headers} onChange={setHeaders} />
        </Card>
        <Card>
          <CardHeader
            title={t.channels.discoverModels}
            description={t.channels.discoverChannelHint}
          />
          <div className="flex flex-wrap gap-2 px-4 py-3">
            {discovery.length === 0 && (
              <p className="text-sm text-muted">{t.channels.noDiscoveryFormat}</p>
            )}
            {discovery.map((format) => (
              <Button
                key={format}
                size="sm"
                onClick={() => setDiscovering(format)}
                disabled={deleted}
              >
                <Sparkles aria-hidden />
                {format}
              </Button>
            ))}
          </div>
          <p className="px-4 pb-3 text-xs text-faint">{t.channels.discoverImportHint}</p>
        </Card>
        <Card>
          <CardHeader title={t.channels.dangerZone} />
          <div className="flex flex-wrap items-center justify-between gap-3 px-4 py-3">
            <div className="min-w-0">
              <p className="text-sm font-medium">{t.channels.detach}</p>
              <p className="text-xs text-muted">{t.channels.detachHint}</p>
            </div>
            <Button
              size="sm"
              onClick={() => setDetaching(true)}
              disabled={deleted || channel.route_count === 0}
            >
              <Unplug aria-hidden />
              {t.channels.detach}
            </Button>
          </div>
          <div className="flex flex-wrap items-center justify-between gap-3 border-t px-4 py-3">
            <div className="min-w-0">
              <p className="text-sm font-medium">{t.common.delete}</p>
              <p className="text-xs text-muted">{t.channels.deleteHint}</p>
            </div>
            {deleted ? (
              <Button size="sm" loading={restore.isPending} onClick={() => restore.mutate()}>
                <RotateCcw aria-hidden />
                {t.common.restore}
              </Button>
            ) : (
              <Button size="sm" variant="danger" onClick={() => setDeleting(true)}>
                <Trash2 aria-hidden />
                {t.common.delete}
              </Button>
            )}
          </div>
        </Card>
        {!deleted && (
          <div className="flex justify-end">
            <Button variant="primary" loading={update.isPending} onClick={save}>
              {t.common.save}
            </Button>
          </div>
        )}
      </div>
      {discovering && (
        <DiscoveryDialog
          channelId={channel.id}
          format={discovering}
          onClose={() => setDiscovering(null)}
        />
      )}
      <DeleteImpactDialog
        open={deleting}
        onOpenChange={setDeleting}
        title={interpolate(t.channels.deleteTitle, { name: channel.name })}
        description={t.impact.warning}
        confirmLabel={t.common.delete}
        pending={remove.isPending}
        impact={impact.data}
        loading={impact.isPending}
        error={impact.error}
        projectNames={projectNameMap(projects.data)}
        replacements={replacements}
        onConfirm={(replacementId) => {
          if (!impact.data) return;
          remove.mutate(
            { channel, impact: impact.data, replacementId },
            {
              onSuccess: () => {
                setDeleting(false);
                void navigate({ to: '/channels' });
              },
            },
          );
        }}
      />
      <ConfirmDialog
        open={detaching}
        onOpenChange={setDetaching}
        title={interpolate(t.channels.detachTitle, { name: channel.name })}
        description={t.channels.detachConfirm}
        confirmLabel={t.channels.detach}
        pending={detach.isPending}
        onConfirm={() => detach.mutate(undefined, { onSuccess: () => setDetaching(false) })}
      />
    </>
  );
}

export function ChannelPage() {
  const { t } = useLocale();
  const { channelId } = useParams({ from: '/app/admin/channels/$channelId' });
  const channel = useGetChannel(channelId);
  const health = useGetChannelHealth(channelId);
  const providers = useListProviders();
  const siblings = useChannels(false);
  return (
    <Page>
      <Link
        to="/channels"
        className="inline-flex items-center gap-1 self-start text-sm text-muted hover:text-ink"
      >
        <ChevronLeft aria-hidden className="size-4" />
        {t.nav.channels}
      </Link>
      <QueryState query={channel} loading={<TableSkeleton rows={10} />}>
        {(loaded) => {
          const provider = providers.data?.find((entry) => entry.id === loaded.provider_id);
          const catalog = parseEndpointCatalog(provider?.endpoints);
          const formats = catalog ? providerFormats(catalog) : (provider?.formats ?? []);
          return (
            <div className="flex flex-col gap-5">
              <PageHeader
                title={loaded.name}
                description={[loaded.provider_label, ...formats].filter(Boolean).join(' · ')}
              />
              <div className="grid items-start gap-4 xl:grid-cols-3">
                <div className="min-w-0 xl:col-span-2">
                  <ChannelEditor
                    key={`${loaded.id}:${loaded.version}`}
                    channel={loaded}
                    providers={providers.data ?? []}
                    siblings={siblings.data ?? []}
                  />
                </div>
                <div className="flex min-w-0 flex-col gap-4">
                  <KeyPool channelId={channelId} />
                  <Card>
                    <CardHeader title={t.channels.health} />
                    <QueryState query={health} loading={<TableSkeleton rows={2} />}>
                      {(entries) => <HealthList health={entries} empty={t.channels.noHealth} />}
                    </QueryState>
                  </Card>
                  <CapabilityOverrides overrides={loaded.capability_overrides} />
                </div>
              </div>
            </div>
          );
        }}
      </QueryState>
    </Page>
  );
}
