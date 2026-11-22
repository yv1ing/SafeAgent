import { Link, useNavigate } from '@tanstack/react-router';
import { Cable, Plus } from 'lucide-react';
import { useState } from 'react';
import { useListChannels } from '../../api/generated/channels/channels';
import type { ConsoleChannelInput } from '../../api/generated/models';
import { DataTable, TableSkeleton } from '../../components/data/DataTable';
import { FormDialog } from '../../components/form/FormDialog';
import { Page } from '../../components/layout/Page';
import { PageHeader } from '../../components/layout/PageHeader';
import { QueryState } from '../../components/feedback/QueryState';
import { Badge } from '../../components/ui/badge';
import { Button } from '../../components/ui/button';
import { Card } from '../../components/ui/card';
import { Empty } from '../../components/ui/empty';
import { interpolate, useLocale } from '../../i18n';
import { ChannelFields } from './components/ChannelFields';
import { useCreateChannel } from './hooks';

const blankChannel: ConsoleChannelInput = {
  name: '',
  provider_id: 'anthropic',
  endpoints: [{ format: 'anthropic_messages', base_url: '', path: '' }],
  model_ids: [],
  enabled: true,
};

export function ChannelsPage() {
  const { t } = useLocale();
  const navigate = useNavigate();
  const channels = useListChannels();
  const create = useCreateChannel();
  const [draft, setDraft] = useState<ConsoleChannelInput | null>(null);
  return (
    <Page>
      <PageHeader
        title={t.channels.title}
        description={t.channels.description}
        actions={
          <Button variant="primary" onClick={() => setDraft(blankChannel)}>
            <Plus aria-hidden />
            {t.channels.add}
          </Button>
        }
      />
      <Card>
        <QueryState query={channels} loading={<TableSkeleton />}>
          {(rows) => (
            <DataTable
              rows={rows}
              rowKey={(row) => row.id}
              empty={
                <Empty
                  icon={Cable}
                  title={t.channels.emptyTitle}
                  description={t.channels.emptyHint}
                />
              }
              columns={[
                {
                  key: 'name',
                  header: t.common.name,
                  className: 'font-medium',
                  cell: (row) => (
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
                  className: 'font-mono text-xs',
                  cell: (row) => row.provider_id,
                },
                {
                  key: 'formats',
                  header: t.channels.formats,
                  className: 'font-mono text-xs text-muted',
                  cell: (row) => row.endpoints.map((endpoint) => endpoint.format).join(' · '),
                },
                {
                  key: 'models',
                  header: t.channels.models,
                  className: 'text-muted',
                  cell: (row) => {
                    const count = row.model_ids?.length ?? 0;
                    return interpolate(
                      count === 1 ? t.channels.modelCountOne : t.channels.modelCount,
                      {
                        count,
                      },
                    );
                  },
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
          submitLabel={t.common.create}
          pending={create.isPending}
          onSubmit={() =>
            create.mutate(draft, {
              onSuccess: (channel) => {
                setDraft(null);
                void navigate({ to: '/channels/$channelId', params: { channelId: channel.id } });
              },
            })
          }
        >
          <ChannelFields draft={draft} onChange={setDraft} basic />
        </FormDialog>
      )}
    </Page>
  );
}
