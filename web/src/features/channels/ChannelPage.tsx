import { Link, useParams } from '@tanstack/react-router';
import { ChevronLeft } from 'lucide-react';
import { useState } from 'react';
import { useGetChannel, useGetChannelHealth } from '../../api/generated/channels/channels';
import type { ConsoleChannelView, ConsoleUpdateChannelInput } from '../../api/generated/models';
import { HealthList } from '../../components/data/HealthList';
import { Page } from '../../components/layout/Page';
import { PageHeader } from '../../components/layout/PageHeader';
import { QueryState } from '../../components/feedback/QueryState';
import { TableSkeleton } from '../../components/data/DataTable';
import { Button } from '../../components/ui/button';
import { Card, CardHeader } from '../../components/ui/card';
import { useLocale } from '../../i18n';
import { ChannelFields } from './components/ChannelFields';
import { HeaderEditor, type HeaderRow } from './components/HeaderEditor';
import { KeyPool } from './components/KeyPool';
import { ModelPrices } from './components/ModelPrices';
import { useUpdateChannel } from './hooks';

function channelInput(channel: ConsoleChannelView): ConsoleUpdateChannelInput {
  return {
    name: channel.name,
    format: channel.format ?? '',
    vendor: channel.vendor,
    endpoint: channel.endpoint ?? '',
    count_tokens_endpoint: channel.count_tokens_endpoint,
    currency: channel.currency,
    forward_headers: channel.forward_headers,
    default_max_output_tokens: channel.default_max_output_tokens,
    timeout_seconds: channel.timeout_seconds,
    first_byte_timeout_seconds: channel.first_byte_timeout_seconds,
    allow_private_network: channel.allow_private_network,
    enabled: channel.enabled,
    models: channel.models ?? [],
    version: channel.version,
  };
}

function ChannelEditor({ channel }: { channel: ConsoleChannelView }) {
  const { t } = useLocale();
  const update = useUpdateChannel(channel.id);
  const [draft, setDraft] = useState(() => channelInput(channel));
  const [headers, setHeaders] = useState<HeaderRow[]>(() => Object.entries(channel.headers ?? {}));
  const save = () =>
    update.mutate({
      ...draft,
      headers: Object.fromEntries(headers.filter(([name]) => name.trim() !== '')),
    });
  return (
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
          <ChannelFields draft={draft} onChange={setDraft} />
        </div>
      </Card>
      <Card>
        <CardHeader title={t.channels.models} description={t.channels.modelsHint} />
        <ModelPrices
          models={draft.models ?? []}
          onChange={(models) => setDraft({ ...draft, models })}
        />
      </Card>
      <Card>
        <CardHeader title={t.channels.headers} />
        <HeaderEditor rows={headers} onChange={setHeaders} />
      </Card>
      <div className="flex justify-end">
        <Button type="submit" variant="primary" loading={update.isPending}>
          {t.common.save}
        </Button>
      </div>
    </form>
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
            <PageHeader title={loaded.name} description={loaded.endpoint} />
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
