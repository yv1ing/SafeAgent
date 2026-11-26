import { Plus, UserCog } from 'lucide-react';
import { useState } from 'react';
import type { ConsoleUserView } from '../../api/generated/models';
import { useListUsers } from '../../api/generated/users/users';
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
import { formatDate } from '../../lib/format';
import { UserActions } from './components/UserActions';
import { UserDialog } from './components/UserDialog';
import { useUserMutations } from './hooks';

export function UsersPage() {
  const { t, locale } = useLocale();
  const users = useListUsers();
  const mutations = useUserMutations();
  const [creating, setCreating] = useState(false);
  const [resetting, setResetting] = useState<ConsoleUserView | null>(null);
  const [issued, setIssued] = useState<ConsoleUserView | null>(null);
  const showPassword = (user: ConsoleUserView) => setIssued(user);
  return (
    <Page>
      <PageHeader
        title={t.users.title}
        description={t.users.description}
        actions={
          <Button variant="primary" onClick={() => setCreating(true)}>
            <Plus aria-hidden />
            {t.users.add}
          </Button>
        }
      />
      <Card>
        <QueryState query={users} loading={<TableSkeleton />}>
          {(rows) => (
            <DataTable
              rows={rows}
              rowKey={(row) => row.id}
              empty={
                <Empty icon={UserCog} title={t.users.emptyTitle} description={t.users.emptyHint} />
              }
              columns={[
                {
                  key: 'username',
                  header: t.users.username,
                  className: 'font-medium',
                  cell: (row) => row.username,
                },
                {
                  key: 'role',
                  header: t.users.role,
                  cell: (row) =>
                    row.administrator ? (
                      <Badge tone="accent">{t.users.administrator}</Badge>
                    ) : (
                      t.users.member
                    ),
                },
                {
                  key: 'status',
                  header: t.users.status,
                  cell: (row) => (
                    <div className="flex gap-1">
                      {row.disabled ? (
                        <Badge>{t.common.disabled}</Badge>
                      ) : (
                        <EnabledBadge enabled label={t.users.active} />
                      )}
                      {row.must_change_password && (
                        <Badge tone="warning">{t.users.mustChange}</Badge>
                      )}
                    </div>
                  ),
                },
                {
                  key: 'created',
                  header: t.common.createdAt,
                  className: 'text-muted',
                  cell: (row) => formatDate(row.created_at, locale),
                },
                {
                  key: 'actions',
                  header: t.common.actions,
                  className: 'text-right',
                  cell: (row) => (
                    <UserActions
                      user={row}
                      onUpdate={(input) => mutations.update.mutate({ userId: row.id, input })}
                      onResetPassword={() => setResetting(row)}
                    />
                  ),
                },
              ]}
            />
          )}
        </QueryState>
      </Card>
      {creating && (
        <UserDialog
          pending={mutations.create.isPending}
          onClose={() => setCreating(false)}
          onSubmit={(input) =>
            mutations.create.mutate(input, {
              onSuccess: (user) => {
                setCreating(false);
                showPassword(user);
              },
            })
          }
        />
      )}
      <ConfirmDialog
        open={resetting !== null}
        onOpenChange={(open) => !open && setResetting(null)}
        title={interpolate(t.users.resetTitle, { username: resetting?.username ?? '' })}
        description={t.users.resetHint}
        confirmLabel={t.users.resetPassword}
        pending={mutations.resetPassword.isPending}
        onConfirm={() =>
          resetting &&
          mutations.resetPassword.mutate(resetting.id, {
            onSuccess: (user) => {
              setResetting(null);
              showPassword(user);
            },
          })
        }
      />
      <SecretDialog
        secret={issued?.initial_password ?? null}
        title={interpolate(t.users.passwordTitle, { username: issued?.username ?? '' })}
        description={t.users.passwordHint}
        onClose={() => setIssued(null)}
      />
    </Page>
  );
}
