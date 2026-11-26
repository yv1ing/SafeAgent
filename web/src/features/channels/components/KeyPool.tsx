import { Plus } from 'lucide-react';
import { useState } from 'react';
import type { ConsoleChannelKeyView } from '../../../api/generated/models';
import { useListChannelKeys } from '../../../api/generated/channels/channels';
import { ConfirmDialog } from '../../../components/form/ConfirmDialog';
import { DataTable, TableSkeleton } from '../../../components/data/DataTable';
import { Field } from '../../../components/form/Field';
import { FormDialog } from '../../../components/form/FormDialog';
import { QueryState } from '../../../components/feedback/QueryState';
import { EnabledBadge } from '../../../components/feedback/Badges';
import { Button } from '../../../components/ui/button';
import { Card, CardHeader } from '../../../components/ui/card';
import { Input } from '../../../components/ui/input';
import { interpolate, useLocale } from '../../../i18n';
import { useChannelKeyMutations } from '../hooks';

export function KeyPool({ channelId }: { channelId: string }) {
  const { t } = useLocale();
  const keys = useListChannelKeys(channelId);
  const mutations = useChannelKeyMutations(channelId);
  const [adding, setAdding] = useState(false);
  const [label, setLabel] = useState('');
  const [secret, setSecret] = useState('');
  const [deleting, setDeleting] = useState<ConsoleChannelKeyView | null>(null);
  const closeAdd = () => {
    setAdding(false);
    setLabel('');
    setSecret('');
  };
  return (
    <Card>
      <CardHeader
        title={t.channels.keys}
        description={t.channels.keysHint}
        actions={
          <Button size="sm" onClick={() => setAdding(true)}>
            <Plus aria-hidden />
            {t.channels.addKey}
          </Button>
        }
      />
      <QueryState query={keys} loading={<TableSkeleton rows={2} />}>
        {(rows) => (
          <DataTable
            rows={rows}
            rowKey={(row) => row.id}
            empty={<p className="px-4 py-6 text-sm text-muted">{t.channels.noKeys}</p>}
            columns={[
              {
                key: 'label',
                header: t.common.label,
                className: 'font-medium',
                cell: (row) => row.label,
              },
              {
                key: 'suffix',
                header: t.channels.keySuffix,
                className: 'font-mono text-xs',
                cell: (row) => `…${row.suffix}`,
              },
              {
                key: 'status',
                header: t.common.enabled,
                cell: (row) => <EnabledBadge enabled={row.enabled} />,
              },
              {
                key: 'actions',
                header: t.common.actions,
                className: 'text-right',
                cell: (row) => (
                  <Button
                    size="sm"
                    variant="ghost"
                    className="text-danger"
                    onClick={() => setDeleting(row)}
                  >
                    {t.common.delete}
                  </Button>
                ),
              },
            ]}
          />
        )}
      </QueryState>
      <FormDialog
        open={adding}
        onOpenChange={(open) => !open && closeAdd()}
        title={t.channels.addKey}
        submitLabel={t.common.create}
        pending={mutations.create.isPending}
        onSubmit={() => mutations.create.mutate({ label, secret }, { onSuccess: closeAdd })}
      >
        <Field label={t.common.label}>
          {(id) => (
            <Input id={id} value={label} onChange={(event) => setLabel(event.target.value)} />
          )}
        </Field>
        <Field label={t.channels.secret}>
          {(id) => (
            <Input
              id={id}
              required
              type="password"
              autoComplete="off"
              className="font-mono"
              value={secret}
              onChange={(event) => setSecret(event.target.value)}
            />
          )}
        </Field>
      </FormDialog>
      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
        title={interpolate(t.channels.deleteKeyTitle, {
          label: deleting?.label || `…${deleting?.suffix ?? ''}`,
        })}
        description={t.common.irreversible}
        confirmLabel={t.common.delete}
        pending={mutations.remove.isPending}
        onConfirm={() =>
          deleting && mutations.remove.mutate(deleting.id, { onSuccess: () => setDeleting(null) })
        }
      />
    </Card>
  );
}
