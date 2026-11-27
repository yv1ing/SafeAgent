import { Link } from '@tanstack/react-router';
import { ListTree } from 'lucide-react';
import { useState } from 'react';
import { useListTrajectories } from '../../api/generated/trajectories/trajectories';
import type { ConsoleTrajectorySummaryViewState } from '../../api/generated/models';
import { useProjectId } from '../../app/context';
import { DataTable } from '../../components/data/DataTable';
import { QueryState } from '../../components/feedback/QueryState';
import { Page } from '../../components/layout/Page';
import { PageHeader } from '../../components/layout/PageHeader';
import { Badge } from '../../components/ui/badge';
import { Card } from '../../components/ui/card';
import { Empty } from '../../components/ui/empty';
import { Select } from '../../components/ui/select';
import { Skeleton } from '../../components/ui/skeleton';
import { useLocale } from '../../i18n';
import { formatDate, formatNumber, shortId } from '../../lib/format';

const states: ConsoleTrajectorySummaryViewState[] = ['open', 'ended', 'interrupted'];

export function TrajectoriesPage() {
  const { t, locale } = useLocale();
  const projectId = useProjectId();
  const [state, setState] = useState<ConsoleTrajectorySummaryViewState | ''>('');
  const query = useListTrajectories(projectId, state ? { state } : undefined);
  return (
    <Page>
      <PageHeader
        title={t.nav.trajectories}
        description={t.trajectory.description}
        actions={
          <Select
            aria-label={t.trajectory.state}
            value={state}
            onValueChange={(value) => setState(value as ConsoleTrajectorySummaryViewState | '')}
            options={[
              { value: '', label: t.trajectory.allStates },
              ...states.map((value) => ({ value, label: t.trajectory.states[value] })),
            ]}
          />
        }
      />
      <Card>
        <QueryState query={query} loading={<Skeleton className="m-4 h-64" />}>
          {(page) =>
            page.items.length === 0 ? (
              <Empty
                icon={ListTree}
                title={t.trajectory.emptyTitle}
                description={t.trajectory.emptyHint}
              />
            ) : (
              <DataTable
                rows={page.items}
                rowKey={(row) => row.id}
                columns={[
                  {
                    key: 'id',
                    header: t.trajectory.columnTrajectory,
                    cell: (row) => (
                      <Link
                        className="font-mono text-xs text-accent hover:underline"
                        to="/projects/$projectId/trajectories/$trajectoryId"
                        params={{ projectId, trajectoryId: row.id }}
                      >
                        {shortId(row.id)}
                      </Link>
                    ),
                  },
                  {
                    key: 'state',
                    header: t.trajectory.state,
                    cell: (row) => (
                      <Badge tone={row.state === 'open' ? 'accent' : 'neutral'}>
                        {t.trajectory.states[row.state]}
                      </Badge>
                    ),
                  },
                  {
                    key: 'coverage',
                    header: t.trajectory.columnCoverage,
                    align: 'right',
                    className: 'num',
                    cell: (row) => `${Math.round(row.coverage * 100)}%`,
                  },
                  {
                    key: 'risk',
                    header: t.trajectory.columnRisk,
                    align: 'right',
                    className: 'num',
                    cell: (row) => row.risk.toFixed(2),
                  },
                  {
                    key: 'calls',
                    header: t.trajectory.columnCalls,
                    align: 'right',
                    className: 'num',
                    cell: (row) => formatNumber(row.call_count),
                  },
                  {
                    key: 'findings',
                    header: t.trajectory.columnFindings,
                    align: 'right',
                    className: 'num',
                    cell: (row) => formatNumber(row.finding_count),
                  },
                  {
                    key: 'last',
                    header: t.trajectory.columnLast,
                    align: 'right',
                    className: 'text-xs text-faint',
                    cell: (row) => formatDate(row.last_event_at, locale),
                  },
                ]}
              />
            )
          }
        </QueryState>
      </Card>
    </Page>
  );
}
