import { Link } from '@tanstack/react-router';
import { Activity, AlertTriangle, Gauge, ShieldAlert } from 'lucide-react';
import { useState } from 'react';
import type { GetMonitorWindow } from '../../api/generated/models';
import { useGetMonitor } from '../../api/generated/monitor/monitor';
import { ChartCard } from '../../components/charts/ChartCard';
import { TimeChart } from '../../components/charts/TimeChart';
import { DataTable } from '../../components/data/DataTable';
import { StatTile } from '../../components/data/StatTile';
import { QueryState } from '../../components/feedback/QueryState';
import { Segmented } from '../../components/feedback/Segmented';
import { Page } from '../../components/layout/Page';
import { PageHeader } from '../../components/layout/PageHeader';
import { Button } from '../../components/ui/button';
import { Card, CardHeader } from '../../components/ui/card';
import { Empty } from '../../components/ui/empty';
import { Skeleton } from '../../components/ui/skeleton';
import { interpolate, useLocale } from '../../i18n';
import { formatBucket, formatDate, formatNumber } from '../../lib/format';
import { usePollingInterval } from '../../hooks/usePolling';

const windows: GetMonitorWindow[] = ['24h', '7d', '30d'];

export function MonitorPage() {
  const { t, locale } = useLocale();
  const [timeWindow, setTimeWindow] = useState<GetMonitorWindow>('24h');
  const [paused, setPaused] = useState(false);
  const monitor = useGetMonitor(
    { window: timeWindow },
    { query: { refetchInterval: usePollingInterval(10_000, paused) } },
  );

  return (
    <Page>
      <PageHeader
        title={t.monitor.title}
        description={t.monitor.description}
        actions={
          <>
            <span className="text-xs text-muted">
              {paused ? t.monitor.paused : t.monitor.refreshing}
            </span>
            <Button size="sm" variant="secondary" onClick={() => setPaused((value) => !value)}>
              {paused ? t.monitor.resume : t.monitor.pause}
            </Button>
            <Segmented
              label={t.monitor.window}
              value={timeWindow}
              options={windows.map((value) => ({ value, label: t.monitor.windows[value] }))}
              onChange={setTimeWindow}
            />
          </>
        }
      />
      <QueryState query={monitor} loading={<Skeleton className="h-96" />}>
        {(snapshot) => {
          const rows = snapshot.series.map((bucket) => ({
            time: formatBucket(bucket.start, locale, timeWindow !== '24h'),
            requests: bucket.requests,
            errors: bucket.errors,
            blocked: bucket.blocked,
          }));
          const series = [
            { key: 'requests', label: t.monitor.requests, color: 'var(--chart-1)' },
            { key: 'errors', label: t.monitor.errors, color: 'var(--danger)' },
            { key: 'blocked', label: t.monitor.blocked, color: 'var(--chart-3)' },
          ];
          return (
            <div className="flex flex-col gap-4">
              <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
                <Card>
                  <StatTile
                    label={t.monitor.calls}
                    value={formatNumber(snapshot.totals.calls)}
                    icon={Activity}
                  />
                </Card>
                <Card>
                  <StatTile
                    label={t.monitor.failures}
                    value={formatNumber(snapshot.totals.failures)}
                    icon={AlertTriangle}
                    alert
                  />
                </Card>
                <Card>
                  <StatTile
                    label={t.monitor.blocked}
                    value={formatNumber(snapshot.totals.blocked)}
                    icon={ShieldAlert}
                  />
                </Card>
                <Card>
                  <StatTile
                    label={t.monitor.highFindings}
                    value={formatNumber(snapshot.totals.high_findings)}
                    icon={Gauge}
                    alert
                    detail={interpolate(
                      snapshot.totals.projects === 1
                        ? t.monitor.projectCountOne
                        : t.monitor.projectCount,
                      { count: formatNumber(snapshot.totals.projects) },
                    )}
                  />
                </Card>
              </div>
              <p className="text-[11px] text-faint">{t.monitor.mainLaneNote}</p>
              <ChartCard
                title={t.monitor.traffic}
                legend={series}
                table={{
                  columns: [t.table.time, ...series.map((entry) => entry.label)],
                  rows: rows.map((row) => [row.time, row.requests, row.errors, row.blocked]),
                }}
              >
                {rows.length === 0 ? (
                  <Empty icon={Activity} title={t.monitor.noTraffic} />
                ) : (
                  <TimeChart rows={rows} series={series} formatValue={formatNumber} />
                )}
              </ChartCard>
              <div className="grid items-start gap-4 xl:grid-cols-2">
                <Card>
                  <CardHeader title={t.monitor.projectsTable} />
                  <DataTable
                    rows={snapshot.projects}
                    rowKey={(project) => project.project_id}
                    empty={<p className="px-3 py-6 text-sm text-muted">{t.monitor.noTraffic}</p>}
                    columns={[
                      {
                        key: 'name',
                        header: t.monitor.name,
                        cell: (project) => (
                          <Link
                            to="/projects/$projectId/trajectories"
                            params={{ projectId: project.project_id }}
                            className="font-medium hover:text-accent"
                          >
                            {project.name}
                          </Link>
                        ),
                      },
                      {
                        key: 'calls',
                        header: t.monitor.calls,
                        align: 'right',
                        className: 'num',
                        cell: (project) => formatNumber(project.calls),
                      },
                      {
                        key: 'blocked',
                        header: t.monitor.blocked,
                        align: 'right',
                        className: 'num',
                        cell: (project) => formatNumber(project.blocked),
                      },
                      {
                        key: 'high',
                        header: t.monitor.highFindings,
                        align: 'right',
                        className: 'num text-danger',
                        cell: (project) => formatNumber(project.high_findings),
                      },
                    ]}
                  />
                </Card>
                <Card>
                  <CardHeader title={t.monitor.agentsTable} />
                  <DataTable
                    rows={snapshot.agents}
                    rowKey={(agent) => `${agent.project_id}:${agent.agent_key_id}`}
                    empty={<p className="px-3 py-6 text-sm text-muted">{t.monitor.noTraffic}</p>}
                    columns={[
                      {
                        key: 'agent',
                        header: t.monitor.agent,
                        cell: (agent) => (
                          <Link
                            to="/projects/$projectId/trajectories"
                            params={{ projectId: agent.project_id }}
                            className="font-mono text-xs hover:text-accent"
                          >
                            {agent.label}
                          </Link>
                        ),
                      },
                      {
                        key: 'calls',
                        header: t.monitor.calls,
                        align: 'right',
                        className: 'num',
                        cell: (agent) => formatNumber(agent.calls),
                      },
                      {
                        key: 'high',
                        header: t.monitor.highFindings,
                        align: 'right',
                        className: 'num text-danger',
                        cell: (agent) => formatNumber(agent.high_findings),
                      },
                      {
                        key: 'last',
                        header: t.monitor.lastSeen,
                        align: 'right',
                        className: 'text-xs text-faint',
                        cell: (agent) => formatDate(agent.last_at, locale),
                      },
                    ]}
                  />
                </Card>
              </div>
              <Card>
                <CardHeader title={t.monitor.recent} />
                {snapshot.recent.length === 0 ? (
                  <p className="px-3 py-6 text-sm text-muted">{t.monitor.noHighRisk}</p>
                ) : (
                  <ul className="divide-y">
                    {snapshot.recent.map((call) => (
                      <li
                        key={call.call_id}
                        className="flex flex-wrap items-center gap-3 px-3 py-2 text-sm"
                      >
                        <Link
                          to="/projects/$projectId/calls"
                          params={{ projectId: call.project_id }}
                          search={{ call: call.call_id }}
                          className="font-mono text-xs text-accent hover:underline"
                        >
                          {call.call_id}
                        </Link>
                        <span className="text-muted">{call.model}</span>
                        <span className="ml-auto text-xs text-faint">
                          {formatDate(call.created_at, locale)}
                        </span>
                      </li>
                    ))}
                  </ul>
                )}
              </Card>
            </div>
          );
        }}
      </QueryState>
    </Page>
  );
}
