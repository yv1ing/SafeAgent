import { Link } from '@tanstack/react-router';
import { MessagesSquare } from 'lucide-react';
import { useListConversations } from '../../api/generated/conversations/conversations';
import { useProjectId } from '../../app/context';
import { DataTable, TableSkeleton } from '../../components/data/DataTable';
import { PageHeader } from '../../components/layout/PageHeader';
import { Pager } from '../../components/data/Pager';
import { Page } from '../../components/layout/Page';
import { QueryState } from '../../components/feedback/QueryState';
import { SeverityBadge } from '../../components/feedback/Badges';
import { Card } from '../../components/ui/card';
import { Empty } from '../../components/ui/empty';
import { useLocale } from '../../i18n';
import { useCursorPages } from '../../lib/cursor';
import { formatDate, shortId } from '../../lib/format';

export function ConversationsPage() {
  const { t, locale } = useLocale();
  const projectId = useProjectId();
  const pages = useCursorPages(projectId);
  const conversations = useListConversations(projectId, pages.params);
  return (
    <Page>
      <PageHeader title={t.conversations.title} description={t.conversations.description} />
      <Card>
        <QueryState query={conversations} loading={<TableSkeleton rows={10} />}>
          {(page) => (
            <>
              <DataTable
                rows={page.items}
                rowKey={(row) => row.id}
                empty={
                  <Empty
                    icon={MessagesSquare}
                    title={t.conversations.emptyTitle}
                    description={t.conversations.emptyHint}
                  />
                }
                columns={[
                  {
                    key: 'session',
                    header: t.conversations.session,
                    cell: (row) => (
                      <Link
                        to="/projects/$projectId/conversations/$sessionId"
                        params={{ projectId, sessionId: row.id }}
                        className="font-mono text-xs hover:text-accent hover:underline"
                      >
                        {shortId(row.id)}
                      </Link>
                    ),
                  },
                  {
                    key: 'calls',
                    header: t.conversations.calls,
                    className: 'num',
                    cell: (row) => row.calls,
                  },
                  {
                    key: 'risk',
                    header: t.conversations.risk,
                    cell: (row) => <SeverityBadge severity={row.max_severity} />,
                  },
                  {
                    key: 'started',
                    header: t.conversations.started,
                    className: 'text-muted',
                    cell: (row) => formatDate(row.created_at, locale),
                  },
                  {
                    key: 'last',
                    header: t.conversations.lastActivity,
                    className: 'text-muted',
                    cell: (row) => formatDate(row.last_at, locale),
                  },
                ]}
              />
              <Pager
                page={pages.page}
                hasPrevious={pages.hasPrevious}
                hasNext={page.next !== undefined}
                onPrevious={pages.previous}
                onNext={() => page.next && pages.next(page.next)}
              />
            </>
          )}
        </QueryState>
      </Card>
    </Page>
  );
}
