import { getRouteApi, Link } from '@tanstack/react-router';
import { ChevronLeft, List } from 'lucide-react';
import { useGetConversation } from '../../api/generated/conversations/conversations';
import { TableSkeleton } from '../../components/data/DataTable';
import { Page } from '../../components/layout/Page';
import { PageHeader } from '../../components/layout/PageHeader';
import { QueryState } from '../../components/feedback/QueryState';
import { buttonClass } from '../../components/ui/button';
import { interpolate, useLocale } from '../../i18n';
import { shortId } from '../../lib/format';
import { TreeTimeline } from './components/TreeTimeline';
import { buildTree } from './tree';

const route = getRouteApi('/app/projects/$projectId/conversations/$sessionId');

export function ConversationPage() {
  const { t } = useLocale();
  const { projectId, sessionId } = route.useParams();
  const conversation = useGetConversation(projectId, sessionId);
  return (
    <Page>
      <Link
        to="/projects/$projectId/conversations"
        params={{ projectId }}
        className="mb-3 inline-flex items-center gap-1 text-sm text-muted hover:text-ink"
      >
        <ChevronLeft aria-hidden className="size-4" />
        {t.nav.conversations}
      </Link>
      <QueryState query={conversation} loading={<TableSkeleton rows={8} />}>
        {(tree) => (
          <div className="flex flex-col gap-5">
            <PageHeader
              title={interpolate(t.conversations.treeTitle, { id: shortId(sessionId) })}
              description={interpolate(
                tree.nodes.length === 1
                  ? t.conversations.treeDescriptionOne
                  : t.conversations.treeDescription,
                { count: tree.nodes.length },
              )}
              actions={
                <Link
                  to="/projects/$projectId/calls"
                  params={{ projectId }}
                  search={{ session_id: sessionId }}
                  className={buttonClass()}
                >
                  <List aria-hidden />
                  {t.conversations.viewCalls}
                </Link>
              }
            />
            {tree.truncated && (
              <p className="mb-3 text-xs text-faint">{t.conversations.truncated}</p>
            )}
            <TreeTimeline branches={buildTree(tree.nodes)} projectId={projectId} />
          </div>
        )}
      </QueryState>
    </Page>
  );
}
