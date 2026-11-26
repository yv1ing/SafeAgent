import { Plus, Users } from 'lucide-react';
import { useState } from 'react';
import type { ConsoleMemberView } from '../../api/generated/models';
import { useListMembers } from '../../api/generated/members/members';
import { useCurrentUser, useProjectId } from '../../app/context';
import { ConfirmDialog } from '../../components/form/ConfirmDialog';
import { DataTable, TableSkeleton } from '../../components/data/DataTable';
import { Page } from '../../components/layout/Page';
import { PageHeader } from '../../components/layout/PageHeader';
import { QueryState } from '../../components/feedback/QueryState';
import { Badge } from '../../components/ui/badge';
import { EnabledBadge } from '../../components/feedback/Badges';
import { Button } from '../../components/ui/button';
import { Card } from '../../components/ui/card';
import { Empty } from '../../components/ui/empty';
import { interpolate, useLocale } from '../../i18n';
import { AddMemberDialog } from './components/AddMemberDialog';
import { useMemberMutations } from './hooks';

export function MembersPage() {
  const { t } = useLocale();
  const projectId = useProjectId();
  const manager = useCurrentUser().administrator;
  const members = useListMembers(projectId);
  const mutations = useMemberMutations(projectId);
  const [adding, setAdding] = useState(false);
  const [removing, setRemoving] = useState<ConsoleMemberView | null>(null);
  return (
    <Page>
      <PageHeader
        title={t.members.title}
        description={t.members.description}
        actions={
          manager && (
            <Button variant="primary" onClick={() => setAdding(true)}>
              <Plus aria-hidden />
              {t.members.add}
            </Button>
          )
        }
      />
      <Card>
        <QueryState query={members} loading={<TableSkeleton />}>
          {(rows) => (
            <DataTable
              rows={rows}
              rowKey={(row) => row.user_id}
              empty={
                <Empty
                  icon={Users}
                  title={t.members.emptyTitle}
                  description={t.members.emptyHint}
                />
              }
              columns={[
                {
                  key: 'username',
                  header: t.members.user,
                  className: 'font-medium',
                  cell: (row) => row.username,
                },
                {
                  key: 'role',
                  header: t.members.role,
                  cell: (row) =>
                    row.administrator ? (
                      <Badge tone="accent">{t.members.administrator}</Badge>
                    ) : (
                      t.members.member
                    ),
                },
                {
                  key: 'status',
                  header: t.users.status,
                  cell: (row) =>
                    row.disabled ? (
                      <Badge>{t.common.disabled}</Badge>
                    ) : (
                      <EnabledBadge enabled label={t.users.active} />
                    ),
                },
                ...(manager
                  ? [
                      {
                        key: 'actions',
                        header: t.common.actions,
                        className: 'text-right',
                        cell: (row: ConsoleMemberView) => (
                          <Button
                            size="sm"
                            variant="ghost"
                            className="text-danger"
                            onClick={() => setRemoving(row)}
                          >
                            {t.members.remove}
                          </Button>
                        ),
                      },
                    ]
                  : []),
              ]}
            />
          )}
        </QueryState>
      </Card>
      {adding && (
        <AddMemberDialog
          members={members.data ?? []}
          pending={mutations.add.isPending}
          onClose={() => setAdding(false)}
          onSubmit={(userId) => mutations.add.mutate(userId, { onSuccess: () => setAdding(false) })}
        />
      )}
      <ConfirmDialog
        open={removing !== null}
        onOpenChange={(open) => !open && setRemoving(null)}
        title={interpolate(t.members.removeTitle, { username: removing?.username ?? '' })}
        description={t.members.removeHint}
        confirmLabel={t.members.remove}
        pending={mutations.remove.isPending}
        onConfirm={() =>
          removing &&
          mutations.remove.mutate(removing.user_id, { onSuccess: () => setRemoving(null) })
        }
      />
    </Page>
  );
}
