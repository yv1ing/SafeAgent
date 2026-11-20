import { Activity } from 'lucide-react';
import { useState } from 'react';
import type { GetProjectStatsWindow } from '../../api/generated/models';
import { useGetProjectSeries } from '../../api/generated/series/series';
import { useGetProjectStats } from '../../api/generated/stats/stats';
import { useProjectId } from '../../app/context';
import { Page } from '../../components/layout/Page';
import { PageHeader } from '../../components/layout/PageHeader';
import { QueryState } from '../../components/feedback/QueryState';
import { Segmented } from '../../components/feedback/Segmented';
import { Card, CardHeader } from '../../components/ui/card';
import { Empty } from '../../components/ui/empty';
import { Skeleton } from '../../components/ui/skeleton';
import { useLocale } from '../../i18n';
import { ChannelTraffic } from './components/ChannelTraffic';
import { Breakdowns, TrafficCharts } from './components/Charts';
import { KpiTiles } from './components/KpiTiles';
import { RecentCalls } from './components/RecentCalls';
import { SeverityChart } from './components/SeverityChart';

const windows: GetProjectStatsWindow[] = ['24h', '7d', '30d'];

function OverviewSkeleton() {
  return (
    <div className="flex flex-col gap-4">
      <div className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">
        {Array.from({ length: 6 }, (_, index) => (
          <Skeleton key={index} className="h-28" />
        ))}
      </div>
      <div className="grid gap-4 xl:grid-cols-2">
        <Skeleton className="h-72" />
        <Skeleton className="h-72" />
      </div>
    </div>
  );
}

export function OverviewPage() {
  const { t } = useLocale();
  const projectId = useProjectId();
  const [timeWindow, setTimeWindow] = useState<GetProjectStatsWindow>('24h');
  const byModel = useGetProjectStats(projectId, {
    window: timeWindow,
    group: 'model',
  });
  const byKey = useGetProjectStats(projectId, {
    window: timeWindow,
    group: 'agent_key',
  });
  const series = useGetProjectSeries(projectId, { window: timeWindow });

  return (
    <Page>
      <PageHeader
        title={t.overview.title}
        description={t.overview.description}
        actions={
          <Segmented
            label={t.overview.window}
            value={timeWindow}
            options={windows.map((value) => ({ value, label: t.overview.windows[value] }))}
            onChange={setTimeWindow}
          />
        }
      />
      <QueryState query={byModel} loading={<OverviewSkeleton />}>
        {(stats) =>
          stats.totals.calls === 0 ? (
            <Card>
              <Empty icon={Activity} title={t.overview.noData} description={t.overview.emptyHint} />
            </Card>
          ) : (
            <div className="flex flex-col gap-4">
              <KpiTiles stats={stats} series={series.data} />
              <QueryState query={series} loading={<Skeleton className="h-72" />}>
                {(loaded) => (
                  <>
                    <TrafficCharts series={loaded} />
                    <div className="grid gap-4 xl:grid-cols-3">
                      <div className="flex min-w-0 flex-col gap-4 xl:col-span-2">
                        <QueryState query={byKey} loading={<Skeleton className="h-48" />}>
                          {(keyStats) => (
                            <Breakdowns byModel={stats} byKey={keyStats} series={loaded} />
                          )}
                        </QueryState>
                        <RecentCalls projectId={projectId} />
                      </div>
                      <div className="flex min-w-0 flex-col gap-4">
                        <Card className="overflow-visible">
                          <CardHeader title={t.overview.severityChart} />
                          <div className="p-4">
                            <SeverityChart severities={loaded.severities} />
                          </div>
                        </Card>
                        <ChannelTraffic series={loaded} />
                      </div>
                    </div>
                  </>
                )}
              </QueryState>
            </div>
          )
        }
      </QueryState>
    </Page>
  );
}
