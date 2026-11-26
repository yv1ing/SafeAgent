import { Link } from '@tanstack/react-router';
import { useListCalls } from '../../../api/generated/calls/calls';
import { TableSkeleton } from '../../../components/data/DataTable';
import { QueryState } from '../../../components/feedback/QueryState';
import { SeverityBadge, StateBadge } from '../../../components/feedback/Badges';
import { Card, CardHeader } from '../../../components/ui/card';
import { useLocale } from '../../../i18n';
import { formatDate, formatDuration } from '../../../lib/format';

const recentLimit = 8;

export function RecentCalls({ projectId }: { projectId: string }) {
  const { t, locale } = useLocale();
  const calls = useListCalls(projectId, { limit: recentLimit });
  return (
    <Card>
      <CardHeader
        title={t.overview.recentCalls}
        actions={
          <Link
            to="/projects/$projectId/calls"
            params={{ projectId }}
            className="text-xs text-accent hover:underline"
          >
            {t.overview.viewAll}
          </Link>
        }
      />
      <QueryState query={calls} loading={<TableSkeleton rows={4} />}>
        {(page) =>
          page.items.length === 0 ? (
            <p className="px-4 py-6 text-sm text-muted">{t.calls.emptyTitle}</p>
          ) : (
            <ul className="divide-y">
              {page.items.map((call) => (
                <li key={call.id}>
                  <Link
                    to="/projects/$projectId/calls"
                    params={{ projectId }}
                    search={{ call: call.id }}
                    className="flex items-center gap-3 px-4 py-2 text-sm hover:bg-sunken"
                  >
                    <StateBadge state={call.state} />
                    <span className="min-w-0 flex-1 truncate font-mono text-xs">
                      {call.public_model}
                    </span>
                    <SeverityBadge severity={call.max_severity} />
                    <span className="num text-xs text-muted">
                      {formatDuration(call.duration_ms)}
                    </span>
                    <span className="hidden text-xs text-faint sm:inline">
                      {formatDate(call.created_at, locale)}
                    </span>
                  </Link>
                </li>
              ))}
            </ul>
          )
        }
      </QueryState>
    </Card>
  );
}
