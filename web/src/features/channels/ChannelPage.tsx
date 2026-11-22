import { Link, useNavigate, useParams } from '@tanstack/react-router';
import { ChevronLeft, Sparkles, Trash2 } from 'lucide-react';
import { useState } from 'react';
import { useGetChannel, useGetChannelHealth } from '../../api/generated/channels/channels';
import { useListModels } from '../../api/generated/models/models';
import type { ConsoleChannelInput, ConsoleChannelView } from '../../api/generated/models';
import { ConfirmDialog } from '../../components/form/ConfirmDialog';
import { HealthList } from '../../components/data/HealthList';
import { Page } from '../../components/layout/Page';
import { PageHeader } from '../../components/layout/PageHeader';
import { QueryState } from '../../components/feedback/QueryState';
import { TableSkeleton } from '../../components/data/DataTable';
import { Button } from '../../components/ui/button';
import { Card, CardHeader } from '../../components/ui/card';
import { useLocale } from '../../i18n';
import { ChannelFields } from './components/ChannelFields';
import { DiscoveryDialog } from './components/DiscoveryDialog';
import { HeaderEditor, type HeaderRow } from './components/HeaderEditor';
import { KeyPool } from './components/KeyPool';
import { ModelPicker } from './components/ModelPicker';
import { useDeleteChannel, useUpdateChannel } from './hooks';

function channelInput(channel: ConsoleChannelView): ConsoleChannelInput {
  return {
    name: channel.name,
    provider_id: channel.provider_id,
    endpoints: channel.endpoints,
    model_ids: channel.model_ids,
    forward_headers: channel.forward_headers,
    default_max_output_tokens: channel.default_max_output_tokens,
    timeout_seconds: channel.timeout_seconds,
    first_byte_timeout_seconds: channel.first_byte_timeout_seconds,
    allow_private_network: channel.allow_private_network,
    enabled: channel.enabled,
    version: channel.version,
  };
}

function ChannelEditor({ channel }: { channel: ConsoleChannelView }) {
  const { t } = useLocale();
  const navigate = useNavigate();
  const update = useUpdateChannel(channel.id);
  const remove = useDeleteChannel();
  const models = useListModels();
  const [draft, setDraft] = useState<ConsoleChannelInput>(() => channelInput(channel));
  const [headers, setHeaders] = useState<HeaderRow[]>(() => Object.entries(channel.headers ?? {}));
  const [discovering, setDiscovering] = useState<string | null>(null);
  const [deleting, setDeleting] = useState(false);
  const setDraftAndPrune = (next: ConsoleChannelInput) => {
    const formats = new Set(next.endpoints.map((endpoint) => endpoint.format));
    const catalog = models.data ?? [];
    const valid = catalog.length
      ? next.model_ids.filter((modelId) => {
          const model = catalog.find((entry) => entry.id === modelId);
          return !model || model.formats.some((format) => formats.has(format));
        })
      : next.model_ids;
    setDraft({ ...next, model_ids: valid });
  };
  const save = () =>
    update.mutate({
      ...draft,
      headers: Object.fromEntries(headers.filter(([name]) => name.trim() !== '')),
    });
  return (
    <>
      <form
        className="flex flex-col gap-4"
        onSubmit={(event) => {
          event.preventDefault();
          save();
        }}
      >
        <Card>
          <CardHeader title={t.channels.settings} />
          <div className="p-4">
            <ChannelFields draft={draft} onChange={setDraftAndPrune} />
          </div>
        </Card>
        <Card>
          <CardHeader title={t.channels.models} description={t.channels.modelsHint} />
          <QueryState query={models} loading={<TableSkeleton rows={3} />}>
            {(catalog) => (
              <ModelPicker
                draft={draft}
                models={catalog}
                onChange={(model_ids) => setDraft({ ...draft, model_ids })}
              />
            )}
          </QueryState>
        </Card>
        <Card>
          <CardHeader title={t.channels.discoverModels} description={t.channels.discoverHint} />
          <div className="flex flex-wrap gap-2 px-4 py-3">
            {channel.endpoints
              .filter((endpoint) =>
                draft.endpoints.some((entry) => entry.format === endpoint.format),
              )
              .map((endpoint) => (
                <Button
                  key={endpoint.format}
                  size="sm"
                  onClick={() => setDiscovering(endpoint.format)}
                >
                  <Sparkles aria-hidden />
                  {endpoint.format}
                </Button>
              ))}
          </div>
        </Card>
        <Card>
          <CardHeader title={t.channels.headers} />
          <HeaderEditor rows={headers} onChange={setHeaders} />
        </Card>
        <div className="flex justify-between gap-2">
          <Button
            type="button"
            variant="ghost"
            className="text-danger"
            onClick={() => setDeleting(true)}
          >
            <Trash2 aria-hidden />
            {t.common.delete}
          </Button>
          <Button type="submit" variant="primary" loading={update.isPending}>
            {t.common.save}
          </Button>
        </div>
      </form>
      {discovering && (
        <DiscoveryDialog
          channelId={channel.id}
          format={discovering}
          onClose={() => setDiscovering(null)}
          onImported={(added) => {
            setDraft((current) => ({
              ...current,
              model_ids: Array.from(new Set([...current.model_ids, ...added])),
            }));
            setDiscovering(null);
          }}
        />
      )}
      <ConfirmDialog
        open={deleting}
        onOpenChange={setDeleting}
        title={t.channels.deleteTitle.replace('{name}', channel.name)}
        description={t.channels.deleteHint}
        confirmLabel={t.common.delete}
        pending={remove.isPending}
        onConfirm={() =>
          remove.mutate(channel.id, {
            onSuccess: () => {
              setDeleting(false);
              void navigate({ to: '/channels' });
            },
          })
        }
      />
    </>
  );
}

export function ChannelPage() {
  const { t } = useLocale();
  const { channelId } = useParams({ from: '/app/admin/channels/$channelId' });
  const channel = useGetChannel(channelId);
  const health = useGetChannelHealth(channelId);
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
        {(loaded) => (
          <div className="flex flex-col gap-5">
            <PageHeader
              title={loaded.name}
              description={[loaded.provider_id, loaded.endpoints.map((e) => e.format).join(' · ')]
                .filter(Boolean)
                .join(' · ')}
            />
            <div className="grid items-start gap-4 xl:grid-cols-3">
              <div className="min-w-0 xl:col-span-2">
                <ChannelEditor key={loaded.version} channel={loaded} />
              </div>
              <div className="flex min-w-0 flex-col gap-4">
                <KeyPool channelId={channelId} />
                <Card>
                  <CardHeader title={t.channels.health} />
                  <QueryState query={health} loading={<TableSkeleton rows={2} />}>
                    {(entries) => <HealthList health={entries} empty={t.channels.noHealth} />}
                  </QueryState>
                </Card>
              </div>
            </div>
          </div>
        )}
      </QueryState>
    </Page>
  );
}
