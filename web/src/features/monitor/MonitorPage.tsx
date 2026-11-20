import { Link } from '@tanstack/react-router';
import { Activity, AlertTriangle, Gauge, ShieldAlert } from 'lucide-react';
import { useState } from 'react';
import type { GetMonitorWindow } from '../../api/generated/models';
import { useGetMonitor } from '../../api/generated/monitor/monitor';
import { Page } from '../../components/layout/Page';
import { usePollingInterval } from '../../hooks/usePolling';
import { PageHeader } from '../../components/layout/PageHeader';
import { QueryState } from '../../components/feedback/QueryState';
import { Segmented } from '../../components/feedback/Segmented';
import { Button } from '../../components/ui/button';
import { Card, CardHeader } from '../../components/ui/card';
import { Empty } from '../../components/ui/empty';
import { Skeleton } from '../../components/ui/skeleton';
import { ChartCard } from '../overview/components/ChartCard';
import { TimeChart } from '../overview/components/TimeChart';
import { formatBucket, formatNumber } from '../../lib/format';
import { interpolate, useLocale } from '../../i18n';

const windows: GetMonitorWindow[] = ['24h', '7d', '30d'];

function Tile({
  label,
  value,
  hint,
  icon: Icon,
  tone,
}: {
  label: string;
  value: string;
  hint?: string;
  icon: typeof Activity;
  tone?: 'danger';
}) {
  return (
    <Card className="flex items-start gap-3 p-3.5">
      <span
        className={`grid size-8 place-items-center rounded-md ${tone === 'danger' ? 'bg-danger-soft text-danger' : 'bg-accent-soft text-accent'}`}
      >
        <Icon className="size-4" aria-hidden />
      </span>
      <div className="min-w-0">
        <p className="text-xs text-muted">{label}</p>
        <p className="num text-xl font-semibold tracking-tight">{value}</p>
        {hint && <p className="text-[11px] text-faint">{hint}</p>}
      </div>
    </Card>
  );
}

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
                <Tile
                  label={t.monitor.calls}
                  value={formatNumber(snapshot.totals.calls)}
                  icon={Activity}
                />
                <Tile
                  label={t.monitor.failures}
                  value={formatNumber(snapshot.totals.failures)}
                  icon={AlertTriangle}
                  tone="danger"
                />
                <Tile
                  label={t.monitor.blocked}
                  value={formatNumber(snapshot.totals.blocked)}
                  icon={ShieldAlert}
                />
                <Tile
                  label={t.monitor.highFindings}
                  value={formatNumber(snapshot.totals.high_findings)}
                  icon={Gauge}
                  tone="danger"
                  hint={interpolate(
                    snapshot.totals.projects === 1
                      ? t.monitor.projectCountOne
                      : t.monitor.projectCount,
                    { count: formatNumber(snapshot.totals.projects) },
                  )}
                />
              </div>
              <ChartCard
                title={t.monitor.traffic}
                legend={series}
                table={{
                  columns: [t.overview.time, ...series.map((entry) => entry.label)],
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
                  <div className="overflow-x-auto">
                    <table className="w-full text-sm">
                      <thead>
                        <tr className="border-b text-left text-xs text-muted">
                          <th className="px-3 py-2 font-medium">{t.monitor.name}</th>
                          <th className="px-3 py-2 text-right font-medium">{t.monitor.calls}</th>
                          <th className="px-3 py-2 text-right font-medium">{t.monitor.blocked}</th>
                          <th className="px-3 py-2 text-right font-medium">
                            {t.monitor.highFindings}
                          </th>
                        </tr>
                      </thead>
                      <tbody>
                        {snapshot.projects.map((project) => (
                          <tr key={project.project_id} className="border-b last:border-0">
                            <td className="px-3 py-2">
                              <Link
                                to="/projects/$projectId/risk"
                                params={{ projectId: project.project_id }}
                                className="font-medium hover:text-accent"
                              >
                                {project.name}
                              </Link>
                            </td>
                            <td className="num px-3 py-2 text-right">
                              {formatNumber(project.calls)}
                            </td>
                            <td className="num px-3 py-2 text-right">
                              {formatNumber(project.blocked)}
                            </td>
                            <td className="num px-3 py-2 text-right text-danger">
                              {formatNumber(project.high_findings)}
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                </Card>
                <Card>
                  <CardHeader title={t.monitor.agentsTable} />
                  <div className="overflow-x-auto">
                    <table className="w-full text-sm">
                      <thead>
                        <tr className="border-b text-left text-xs text-muted">
                          <th className="px-3 py-2 font-medium">{t.monitor.agent}</th>
                          <th className="px-3 py-2 text-right font-medium">{t.monitor.calls}</th>
                          <th className="px-3 py-2 text-right font-medium">
                            {t.monitor.highFindings}
                          </th>
                          <th className="px-3 py-2 text-right font-medium">{t.monitor.lastSeen}</th>
                        </tr>
                      </thead>
                      <tbody>
                        {snapshot.agents.map((agent) => (
                          <tr
                            key={`${agent.project_id}:${agent.agent_key_id}`}
                            className="border-b last:border-0"
                          >
                            <td className="px-3 py-2">
                              <Link
                                to="/projects/$projectId/risk"
                                params={{ projectId: agent.project_id }}
                                className="font-mono text-xs hover:text-accent"
                              >
                                {agent.label}
                              </Link>
                            </td>
                            <td className="num px-3 py-2 text-right">
                              {formatNumber(agent.calls)}
                            </td>
                            <td className="num px-3 py-2 text-right text-danger">
                              {formatNumber(agent.high_findings)}
                            </td>
                            <td className="px-3 py-2 text-right text-xs text-faint">
                              {new Date(agent.last_at).toLocaleString(locale)}
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
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
                          {new Date(call.created_at).toLocaleString(locale)}
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
