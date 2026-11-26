import { KeyRound, Pencil, Plus } from 'lucide-react';
import { useState } from 'react';
import { useListKeys } from '../../api/generated/keys/keys';
import type { ConsoleAgentKeyView } from '../../api/generated/models';
import { useProjectId } from '../../app/context';
import { ConfirmDialog } from '../../components/form/ConfirmDialog';
import { DataTable, TableSkeleton } from '../../components/data/DataTable';
import { Page } from '../../components/layout/Page';
import { PageHeader } from '../../components/layout/PageHeader';
import { QueryState } from '../../components/feedback/QueryState';
import { SecretDialog } from '../../components/form/SecretDialog';
import { Badge } from '../../components/ui/badge';
import { EnabledBadge } from '../../components/feedback/Badges';
import { Button } from '../../components/ui/button';
import { Card } from '../../components/ui/card';
import { Empty } from '../../components/ui/empty';
import { interpolate, useLocale } from '../../i18n';
import { formatDate, formatLimit, placeholder } from '../../lib/format';
import { KeyDialog } from './components/KeyDialog';
import { useKeyMutations } from './hooks';

export function KeysPage() {
  const { t, locale } = useLocale();
  const projectId = useProjectId();
  const keys = useListKeys(projectId);
  const mutations = useKeyMutations(projectId);
  const [editing, setEditing] = useState<ConsoleAgentKeyView | 'new' | null>(null);
  const [revoking, setRevoking] = useState<ConsoleAgentKeyView | null>(null);
  const [secret, setSecret] = useState<string | null>(null);

  return (
    <Page>
      <PageHeader
        title={t.keys.title}
        description={t.keys.description}
        actions={
          <Button variant="primary" onClick={() => setEditing('new')}>
            <Plus aria-hidden />
            {t.keys.add}
          </Button>
        }
      />
      <Card>
        <QueryState query={keys} loading={<TableSkeleton />}>
          {(rows) => (
            <DataTable
              rows={rows}
              rowKey={(row) => row.id}
              empty={
                <Empty icon={KeyRound} title={t.keys.emptyTitle} description={t.keys.emptyHint} />
              }
              columns={[
                {
                  key: 'label',
                  header: t.common.label,
                  className: 'font-medium',
                  cell: (row) => row.label,
                },
                {
                  key: 'prefix',
                  header: t.keys.prefix,
                  className: 'font-mono text-xs text-muted',
                  cell: (row) => `${row.prefix}…`,
                },
                {
                  key: 'status',
                  header: t.keys.status,
                  cell: (row) =>
                    row.revoked ? (
                      <Badge tone="danger">{t.keys.revoked}</Badge>
                    ) : (
                      <EnabledBadge enabled={row.enabled} />
                    ),
                },
                {
                  key: 'rpm',
                  header: t.keys.rpm,
                  className: 'num',
                  cell: (row) => formatLimit(row.requests_per_minute, t.common.unlimited),
                },
                {
                  key: 'concurrency',
                  header: t.keys.concurrency,
                  className: 'num',
                  cell: (row) => formatLimit(row.concurrency, t.common.unlimited),
                },
                {
                  key: 'lastUsed',
                  header: t.keys.lastUsed,
                  className: 'text-muted',
                  cell: (row) =>
                    row.last_used_at ? formatDate(row.last_used_at, locale) : t.keys.never,
                },
                {
                  key: 'created',
                  header: t.common.createdAt,
                  className: 'text-muted',
                  cell: (row) =>
                    row.created_at ? formatDate(row.created_at, locale) : placeholder,
                },
                {
                  key: 'actions',
                  header: t.common.actions,
                  className: 'text-right',
                  cell: (row) =>
                    !row.revoked && (
                      <div className="flex justify-end gap-1">
                        <Button size="sm" variant="ghost" onClick={() => setEditing(row)}>
                          <Pencil aria-hidden />
                          {t.common.edit}
                        </Button>
                        <Button
                          size="sm"
                          variant="ghost"
                          className="text-danger"
                          onClick={() => setRevoking(row)}
                        >
                          {t.keys.revoke}
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
        <KeyDialog
          agentKey={editing === 'new' ? null : editing}
          pending={mutations.create.isPending || mutations.update.isPending}
          onClose={() => setEditing(null)}
          onSubmit={(input) =>
            editing === 'new'
              ? mutations.create.mutate(input, {
                  onSuccess: (created) => {
                    setEditing(null);
                    setSecret(created.secret ?? null);
                  },
                })
              : mutations.update.mutate(
                  { keyId: editing.id, input },
                  { onSuccess: () => setEditing(null) },
                )
          }
        />
      )}
      <ConfirmDialog
        open={revoking !== null}
        onOpenChange={(open) => !open && setRevoking(null)}
        title={interpolate(t.keys.revokeTitle, { label: revoking?.label ?? '' })}
        description={t.keys.revokeHint}
        confirmLabel={t.keys.revoke}
        pending={mutations.revoke.isPending}
        onConfirm={() =>
          revoking && mutations.revoke.mutate(revoking.id, { onSuccess: () => setRevoking(null) })
        }
      />
      <SecretDialog
        secret={secret}
        title={t.keys.secretTitle}
        description={t.keys.secretHint}
        onClose={() => setSecret(null)}
      />
    </Page>
  );
}
